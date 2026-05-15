// Package runner provides the review execution engine.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/masa6161/arc-cli/internal/agent"
	"github.com/masa6161/arc-cli/internal/domain"
	"github.com/masa6161/arc-cli/internal/terminal"
)

// maxFindingPreviewLength is the maximum characters shown for a finding in
// verbose output. Longer findings are truncated with "..." to prevent
// excessive terminal output while preserving enough context for debugging.
const maxFindingPreviewLength = 120

// Config holds the runner configuration.
type Config struct {
	Reviewers       int
	Concurrency     int
	BaseRef         string
	Timeout         time.Duration
	Retries         int
	Verbose         bool
	WorkDir         string
	Guidance        string
	UseRefFile      bool
	Diff            string        // Pre-computed git diff (generated once, shared across reviewers)
	DiffPrecomputed bool          // Whether Diff was pre-computed (true even if Diff is empty)
	Phases          []PhaseConfig // Phase-typed review configuration (empty = legacy mode)
	RolePrompts     bool          // Enable role-specific prompts for auto-phase mode
	HasArchReviewer bool          // Arch-phase reviewer exists in this run
}

// Runner executes parallel code reviews.
type Runner struct {
	config    Config
	agents    []agent.Agent  // Backward compat (New() callers)
	specs     []ReviewerSpec // Per-reviewer specs (Phase 0)
	logger    *terminal.Logger
	completed *atomic.Int32
}

// New creates a new runner with one or more agents for round-robin assignment.
// Returns an error if agents slice is empty.
func New(config Config, agents []agent.Agent, logger *terminal.Logger) (*Runner, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("at least one agent is required")
	}
	r := &Runner{
		config:    config,
		agents:    agents,
		logger:    logger,
		completed: &atomic.Int32{},
	}
	r.specs = buildSpecsFromAgents(agents, config)
	return r, nil
}

// NewWithSpecs creates a Runner with explicit per-reviewer specs.
// Used when callers need per-reviewer differentiation (phase-typed reviews).
// Specs with ReviewerID==0 are auto-assigned IDs starting from 1 in slice order.
// Specs with explicit ReviewerID values are preserved. Duplicate IDs return an error.
func NewWithSpecs(config Config, specs []ReviewerSpec, logger *terminal.Logger) (*Runner, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("at least one reviewer spec is required")
	}
	assigned, err := assignReviewerIDs(specs)
	if err != nil {
		return nil, err
	}
	// Ensure reviewer count matches spec count to prevent phantom failures
	config.Reviewers = len(assigned)
	return &Runner{
		config:    config,
		specs:     assigned,
		logger:    logger,
		completed: &atomic.Int32{},
	}, nil
}

// assignReviewerIDs fills in ReviewerID for any spec where it is 0 (using
// index+1 for those slots) and verifies that no two specs share the same ID.
// It returns a new slice so the caller's original slice is not mutated.
func assignReviewerIDs(specs []ReviewerSpec) ([]ReviewerSpec, error) {
	out := make([]ReviewerSpec, len(specs))
	copy(out, specs)

	// First pass: assign IDs to zero-value slots.
	for i := range out {
		if out[i].ReviewerID == 0 {
			out[i].ReviewerID = i + 1
		}
	}

	// Second pass: verify uniqueness.
	seen := make(map[int]bool, len(out))
	for _, s := range out {
		if seen[s.ReviewerID] {
			return nil, fmt.Errorf("duplicate ReviewerID %d in specs", s.ReviewerID)
		}
		seen[s.ReviewerID] = true
	}

	return out, nil
}

// buildSpecsFromAgents converts a round-robin agent list into ReviewerSpecs
// for backward compatibility with the existing New() constructor.
// IDs are assigned explicitly (1..N) so runReviewer can address them directly.
func buildSpecsFromAgents(agents []agent.Agent, config Config) []ReviewerSpec {
	specs := make([]ReviewerSpec, config.Reviewers)
	for i := range specs {
		specs[i] = ReviewerSpec{
			ReviewerID:      i + 1,
			Agent:           agent.AgentForReviewer(agents, i+1),
			Guidance:        config.Guidance,
			Diff:            config.Diff,
			DiffPrecomputed: config.DiffPrecomputed,
		}
	}
	return specs
}

// Run executes the review process and returns the results.
func (r *Runner) Run(ctx context.Context) ([]domain.ReviewerResult, time.Duration, error) {
	spinner := terminal.NewSpinner(r.config.Reviewers)
	r.completed = spinner.Completed()

	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		spinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	start := time.Now()

	// Create result channel
	resultCh := make(chan domain.ReviewerResult, r.config.Reviewers)

	// Determine concurrency limit (default to reviewers if not set)
	concurrency := r.config.Concurrency
	if concurrency <= 0 {
		concurrency = r.config.Reviewers
	}

	// Create semaphore to limit concurrent reviewers
	sem := make(chan struct{}, concurrency)

	// Launch reviewers — iterate specs to use authoritative ReviewerIDs.
	for _, spec := range r.specs {
		go func(id int) {
			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				resultCh <- domain.ReviewerResult{
					ReviewerID: id,
					ExitCode:   -1,
				}
				return
			}

			result := r.runReviewerWithRetry(ctx, id)

			// Release semaphore
			<-sem

			r.completed.Add(1)
			resultCh <- result
		}(spec.ReviewerID)
	}

	// Collect results
	results := make([]domain.ReviewerResult, 0, r.config.Reviewers)
	for i := 0; i < r.config.Reviewers; i++ {
		select {
		case result := <-resultCh:
			results = append(results, result)
		case <-ctx.Done():
			spinnerCancel()
			<-spinnerDone
			return nil, time.Since(start), ctx.Err()
		}
	}

	spinnerCancel()
	<-spinnerDone

	return results, time.Since(start), nil
}

func (r *Runner) runReviewerWithRetry(ctx context.Context, reviewerID int) domain.ReviewerResult {
	var result domain.ReviewerResult

	for attempt := 0; attempt <= r.config.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return domain.ReviewerResult{
				ReviewerID: reviewerID,
				ExitCode:   -1,
			}
		default:
		}

		result = r.runReviewer(ctx, reviewerID)

		if result.ExitCode == 0 {
			return result
		}

		// Skip retries for auth failures — retrying won't help
		if result.AuthFailed {
			r.logger.Logf(terminal.StyleError, "Reviewer #%d (%s) authentication failed: %s",
				reviewerID, result.AgentName, agent.AuthHint(result.AgentName))
			return result
		}

		if attempt < r.config.Retries {
			base := time.Duration(1<<attempt) * time.Second
			jitter := time.Duration(rand.Int64N(int64(base / 2)))
			delay := base + jitter
			reason := "failed"
			if result.TimedOut {
				reason = "timed out"
			}
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d %s (exit %d), retry %d/%d in %v",
				reviewerID, reason, result.ExitCode, attempt+1, r.config.Retries, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return result
			}
		}
	}

	return result
}

func (r *Runner) runReviewer(ctx context.Context, reviewerID int) domain.ReviewerResult {
	start := time.Now()

	// Look up spec by authoritative ReviewerID (not positional index).
	var spec ReviewerSpec
	found := false
	for _, s := range r.specs {
		if s.ReviewerID == reviewerID {
			spec = s
			found = true
			break
		}
	}
	if !found {
		return domain.ReviewerResult{
			ReviewerID: reviewerID,
			ExitCode:   -1,
			Duration:   time.Since(start),
		}
	}
	selectedAgent := spec.Agent
	if selectedAgent == nil {
		return domain.ReviewerResult{
			ReviewerID: reviewerID,
			ExitCode:   -1,
			Duration:   time.Since(start),
		}
	}

	result := domain.ReviewerResult{
		ReviewerID: reviewerID,
		AgentName:  selectedAgent.Name(),
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	// Create review configuration from spec + runner config
	reviewConfig := &agent.ReviewConfig{
		BaseRef:         r.config.BaseRef,
		Timeout:         r.config.Timeout,
		WorkDir:         r.config.WorkDir,
		Verbose:         r.config.Verbose,
		Guidance:        orDefault(spec.Guidance, r.config.Guidance),
		ReviewerID:      strconv.Itoa(reviewerID),
		UseRefFile:      r.config.UseRefFile,
		Diff:            orDefault(spec.Diff, r.config.Diff),
		DiffPrecomputed: spec.DiffPrecomputed || r.config.DiffPrecomputed,
		Model:           spec.Model,
		Phase:           spec.Phase,
		TargetFiles:     spec.TargetFiles,
		RolePrompts:     r.config.RolePrompts,
		HasArchReviewer: r.config.HasArchReviewer,
	}

	// Stamp phase early so that failed/timed-out reviewers are counted
	// in per-phase denominators by BuildStats.
	result.Phase = reviewConfig.Phase

	// Execute the review
	execResult, err := selectedAgent.ExecuteReview(timeoutCtx, reviewConfig)
	if err != nil {
		if r.verbose() {
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: execute error: %v", reviewerID, err)
		}
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}
	// Ensure cleanup on all exit paths
	defer func() {
		if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
		}
	}()

	// Create parser for this agent's output
	parser, err := agent.NewReviewParser(selectedAgent.Name(), reviewerID)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	// Configure scanner
	scanner := bufio.NewScanner(execResult)
	agent.ConfigureScanner(scanner)

	// Parse output
	for {
		// Check for timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			result.ParseErrors += parser.ParseErrors()
			result.TimedOut = true
			result.ExitCode = -1
			result.Duration = time.Since(start)
			return result
		}

		finding, err := parser.ReadFinding(scanner)
		if err != nil {
			if agent.IsRecoverable(err) {
				// Recoverable error - log and continue parsing
				// Parse error count tracked by parser.ParseErrors()
				if r.verbose() {
					r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: %v", reviewerID, err)
				}
				continue
			}
			// Fatal error - break to avoid infinite loop
			result.ParseErrors++
			break
		}

		if finding == nil {
			// End of stream
			break
		}

		result.Findings = append(result.Findings, *finding)

		if r.verbose() {
			text := finding.Text
			if len(text) > maxFindingPreviewLength {
				text = text[:maxFindingPreviewLength] + "..."
			}
			r.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
				terminal.Color(terminal.Dim), reviewerID, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), text, terminal.Color(terminal.Reset))
		}
	}

	// Stamp phase on all findings (parser has no access to ReviewConfig)
	if reviewConfig.Phase != "" {
		for i := range result.Findings {
			result.Findings[i].Phase = reviewConfig.Phase
		}
	}

	// Stamp group key on all findings
	if spec.GroupKey != "" {
		for i := range result.Findings {
			result.Findings[i].GroupKey = spec.GroupKey
		}
	}

	// Capture parse errors tracked by the parser
	result.ParseErrors += parser.ParseErrors()

	// Close to wait for process and get exit code
	// (defer will be a no-op due to sync.Once in ExecutionResult)
	if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
		r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
	}
	result.ExitCode = execResult.ExitCode()
	result.Stderr = strings.TrimSpace(execResult.Stderr())

	// Detect auth failure from exit code and stderr
	if result.ExitCode != 0 {
		if r.verbose() && result.Stderr != "" {
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d stderr:%s\n%s",
				reviewerID, terminal.Color(terminal.Reset), result.Stderr)
		}
		result.AuthFailed = agent.IsAuthFailure(selectedAgent.Name(), result.ExitCode, execResult.Stderr())
	}

	// Record duration after process fully exits
	result.Duration = time.Since(start)

	// Check for timeout after parsing — timeout takes precedence over auth failure
	if timeoutCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.AuthFailed = false
		result.ExitCode = -1
		return result
	}

	return result
}

func (r *Runner) verbose() bool {
	return r.config.Verbose
}

// orDefault returns value if non-empty, otherwise fallback.
func orDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// BuildStats builds review statistics from results.
func BuildStats(results []domain.ReviewerResult, totalReviewers int, wallClock time.Duration) domain.ReviewStats {
	stats := domain.ReviewStats{
		TotalReviewers:     totalReviewers,
		ReviewerDurations:  make(map[int]time.Duration),
		ReviewerAgentNames: make(map[int]string),
		WallClockDuration:  wallClock,
	}

	for _, r := range results {
		stats.ReviewerDurations[r.ReviewerID] = r.Duration
		stats.ReviewerAgentNames[r.ReviewerID] = r.AgentName
		stats.ParseErrors += r.ParseErrors

		if r.TimedOut {
			stats.TimedOutReviewers = append(stats.TimedOutReviewers, r.ReviewerID)
		} else if r.AuthFailed {
			stats.AuthFailedReviewers = append(stats.AuthFailedReviewers, r.ReviewerID)
		} else if r.ExitCode != 0 {
			stats.FailedReviewers = append(stats.FailedReviewers, r.ReviewerID)
		} else {
			stats.SuccessfulReviewers++
		}

		switch r.Phase {
		case domain.PhaseArch:
			stats.ArchReviewers++
			if !r.TimedOut && !r.AuthFailed && r.ExitCode == 0 {
				stats.SuccessfulArchReviewers++
			}
		case domain.PhaseDiff:
			stats.DiffReviewers++
			if !r.TimedOut && !r.AuthFailed && r.ExitCode == 0 {
				stats.SuccessfulDiffReviewers++
			}
		}
	}

	return stats
}

// CollectFindings collects all findings from results.
func CollectFindings(results []domain.ReviewerResult) []domain.Finding {
	var findings []domain.Finding
	for _, r := range results {
		findings = append(findings, r.Findings...)
	}
	return findings
}
