package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/modelconfig"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// cliOrLegacy returns (cliModel, legacyModel) based on whether the value came
// from a CLI flag or env var (highest priority) vs config file (lowest priority).
func cliOrLegacy(value string, fromCLI bool) (cli, legacy string) {
	if fromCLI {
		return value, ""
	}
	return "", value
}

func executeReview(ctx context.Context, opts ReviewOpts, logger *terminal.Logger) domain.ExitCode {
	if err := agent.ValidateAgentNames(opts.ReviewerAgents); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	// Resolve the base ref once before launching parallel reviewers.
	// This ensures all reviewers compare against the same ref, avoiding
	// inconsistent results if network conditions vary during parallel execution.
	// Hoisted above agent construction so diff-size classification can feed the
	// size-aware model resolver (models.sizes.<size>.<role>).
	resolvedBaseRef := opts.Base
	if opts.Fetch {
		// Update current branch from remote (fast-forward only).
		// Skip when the base ref is relative to HEAD (e.g., HEAD~3) since
		// fast-forwarding would change what those refs resolve to.
		if !git.IsRelativeRef(opts.Base) {
			branchResult := git.UpdateCurrentBranch(ctx, opts.WorkDir)
			if branchResult.Updated && opts.Verbose {
				logger.Logf(terminal.StyleDim, "Updated branch %s from origin", branchResult.BranchName)
			}
			if branchResult.Error != nil {
				logger.Logf(terminal.StyleWarning, "Could not update %s from origin: %v (reviewing local state)", branchResult.BranchName, branchResult.Error)
			}
		}

		// Fetch base ref
		result := git.FetchRemoteRef(ctx, opts.Base, opts.WorkDir)
		resolvedBaseRef = result.ResolvedRef
		if result.FetchAttempted && !result.RefResolved {
			logger.Logf(terminal.StyleWarning, "Failed to fetch %s from origin, comparing against local %s (may be stale)", opts.Base, resolvedBaseRef)
		} else if opts.Verbose && result.FetchAttempted && result.RefResolved {
			logger.Logf(terminal.StyleDim, "Comparing against %s (fetched from origin)", resolvedBaseRef)
		}
	}

	// Classify diff size once up-front so the size-aware model resolver
	// (models.sizes.<size>.<role>) can pick per-role model/effort for every
	// agent constructed below. ClassifyDiffSize uses `git diff --stat` and is
	// cheap compared to GetDiff. On classification error we pass sizeStr=""
	// and the resolver falls through to defaults + legacy fields.
	var sizeStr string
	var diffSize git.DiffSize
	var diffFileCount, diffLineCount int
	var diffSizeClassified bool
	var classifyErr error
	diffSize, diffFileCount, diffLineCount, classifyErr = git.ClassifyDiffSize(ctx, resolvedBaseRef, opts.WorkDir)
	if classifyErr == nil {
		sizeStr = diffSize.String()
		diffSizeClassified = true
	} else if opts.Verbose {
		logger.Logf(terminal.StyleWarning, "Diff size classification failed: %v (model resolver will skip size layer)", classifyErr)
	}

	// Resolve generic (phase="") reviewer specs with per-agent-name effort/model
	// via the size-aware phase-aware model config resolver. This covers the flat
	// review path (auto-phase OFF / size=small / explicit `--phase diff`); the
	// grouped and phased paths below re-resolve per-phase from the same config
	// using phase="arch"/"diff".
	reviewerSpecs := make([]agent.AgentSpec, 0, len(opts.ReviewerAgents))
	for _, name := range opts.ReviewerAgents {
		cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, "" /* flat */, name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		reviewerSpecs = append(reviewerSpecs, agent.AgentSpec{
			Name:    name,
			Options: agent.AgentOptions{Model: spec.Model, Effort: spec.Effort},
		})
	}
	reviewAgents, err := agent.CreateAgentsFromSpecs(reviewerSpecs)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	// rebindSpecAgent re-resolves the reviewer model/effort for a given
	// (phase, agent name) pair and constructs a fresh Agent instance for the
	// spec. Captures surrounding scope (opts, sizeStr) so downstream phase
	// wiring stays concise. Falls back to the original Agent on error.
	rebindSpecAgent := func(origAgent agent.Agent, phase string) agent.Agent {
		if origAgent == nil {
			return origAgent
		}
		name := origAgent.Name()
		cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, phase, name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		a, err := agent.NewAgentWithOptions(name, agent.AgentOptions{Model: spec.Model, Effort: spec.Effort})
		if err != nil {
			return origAgent
		}
		return a
	}

	cliSumModel, legacySumModel := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
	summSpec := modelconfig.Resolve(
		opts.Models, sizeStr, modelconfig.RoleSummarizer, opts.SummarizerAgent,
		cliSumModel, "",
		legacySumModel, "",
	)
	summarizerAgent, err := agent.NewAgentWithOptions(opts.SummarizerAgent, agent.AgentOptions{Model: summSpec.Model, Effort: summSpec.Effort})
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return domain.ExitError
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", opts.SummarizerAgent, err)
		return domain.ExitError
	}

	// Resolve and validate cross-check agents (fail-fast).
	ccAgentNames, ccModels, ccErr := resolveCrossCheckAgents(opts)
	if ccErr != nil {
		logger.Logf(terminal.StyleError, "%v", ccErr)
		return domain.ExitError
	}
	// Per-agent model is positionally paired with the agent name.
	// CrossCheckModel is now required, so no SummarizerModel fallback path remains.
	ccSpecs := make([]summarizer.CrossCheckAgentSpec, 0, len(ccAgentNames))
	for i, name := range ccAgentNames {
		cliCCModel, legacyCCModel := cliOrLegacy(ccModels[i], opts.CrossCheckModelFromCLI)
		perSpec := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleCrossCheck, name,
			cliCCModel, "",
			legacyCCModel, "",
		)
		ccSpecs = append(ccSpecs, summarizer.CrossCheckAgentSpec{
			Name:    name,
			Options: summarizer.CrossCheckOptions{Model: perSpec.Model, Effort: perSpec.Effort},
		})
	}
	// CLI availability check for cross-check agents is deferred to the
	// grouped path below (see `if useGroupedSpecs && opts.CrossCheckEnabled`):
	// small diff / explicit `--phase` / `--no-auto-phase` / `arch,diff` paths
	// never invoke cross-check, so requiring those CLIs to be installed for
	// every review run is unnecessary friction. Contract integrity (agent name
	// validity, model 1:1 pairing) is enforced by resolveCrossCheckAgents above
	// plus ResolvedConfig.ValidateAll (cross_check.enabled=true requires model).

	// Verbose: log the effective model/effort matrix for all roles once, up-front.
	// fp_filter and pr_feedback specs are resolved later in the flow, so we
	// re-invoke Resolve here (pure, cheap) solely for display purposes.
	if opts.Verbose {
		cliSumModelLog, legacySumModelLog := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
		fpSpecLog := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleFPFilter, opts.SummarizerAgent,
			cliSumModelLog, "",
			legacySumModelLog, "",
		)
		prFeedbackAgentName := opts.PRFeedbackAgent
		if prFeedbackAgentName == "" {
			prFeedbackAgentName = opts.SummarizerAgent
		}
		prFeedbackSpecLog := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RolePRFeedback, prFeedbackAgentName,
			cliSumModelLog, "",
			legacySumModelLog, "",
		)
		logger.Logf(terminal.StyleDim, "Effective model matrix (size=%s):", formatSizeStr(sizeStr))
		for _, s := range reviewerSpecs {
			logger.Logf(terminal.StyleDim, "  reviewer[%s]       : %s", s.Name, formatSpec(agent.AgentOptions{Model: s.Options.Model, Effort: s.Options.Effort}))
		}
		// arch_reviewer / diff_reviewer rows are meaningful only when
		// auto-phase is enabled AND diff-size classification succeeded: they
		// show how the multi-phase run will resolve per-phase model/effort
		// (phase-specific roles fall back to the generic reviewer at the SAME
		// cascade layer via ResolveReviewer). Without a classified size, these
		// rows would be misleading since auto-phase will skip per-phase routing.
		if opts.AutoPhase && opts.Phase == "" && diffSizeClassified && diffSize != git.DiffSizeSmall {
			cliRevModelLog, legacyRevModelLog := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
			// Arch: single agent — explicit override or first reviewer agent.
			archNames := []string{opts.ReviewerAgents[0]}
			if opts.ArchReviewerAgent != "" {
				archNames = []string{opts.ArchReviewerAgent}
			}
			for _, name := range archNames {
				archSpec := modelconfig.ResolveReviewer(
					opts.Models, sizeStr, "arch", name,
					cliRevModelLog, "",
					legacyRevModelLog, "",
				)
				logger.Logf(terminal.StyleDim, "  arch_reviewer[%s]  : %s", name, formatSpec(agent.AgentOptions{Model: archSpec.Model, Effort: archSpec.Effort}))
			}
			// Diff: all diff-phase agents — explicit override or all reviewer agents.
			diffNames := opts.ReviewerAgents
			if len(opts.DiffReviewerAgents) > 0 {
				diffNames = opts.DiffReviewerAgents
			}
			for _, name := range diffNames {
				diffSpec := modelconfig.ResolveReviewer(
					opts.Models, sizeStr, "diff", name,
					cliRevModelLog, "",
					legacyRevModelLog, "",
				)
				logger.Logf(terminal.StyleDim, "  diff_reviewer[%s]  : %s", name, formatSpec(agent.AgentOptions{Model: diffSpec.Model, Effort: diffSpec.Effort}))
			}
		}
		logger.Logf(terminal.StyleDim, "  summarizer        : %s", formatSpec(agent.AgentOptions{Model: summSpec.Model, Effort: summSpec.Effort}))
		if opts.CrossCheckEnabled {
			for i, name := range ccAgentNames {
				cliCCModelLog, legacyCCModelLog := cliOrLegacy(ccModels[i], opts.CrossCheckModelFromCLI)
				perSpec := modelconfig.Resolve(
					opts.Models, sizeStr, modelconfig.RoleCrossCheck, name,
					cliCCModelLog, "",
					legacyCCModelLog, "",
				)
				logger.Logf(terminal.StyleDim, "  cross_check[%s]    : %s", name, formatSpec(agent.AgentOptions{Model: perSpec.Model, Effort: perSpec.Effort}))
			}
		}
		logger.Logf(terminal.StyleDim, "  fp_filter         : %s", formatSpec(agent.AgentOptions{Model: fpSpecLog.Model, Effort: fpSpecLog.Effort}))
		logger.Logf(terminal.StyleDim, "  pr_feedback       : %s", formatSpec(agent.AgentOptions{Model: prFeedbackSpecLog.Model, Effort: prFeedbackSpecLog.Effort}))
	}

	// Show agent distribution if multiple agents
	if len(reviewAgents) > 1 {
		distribution := agent.FormatDistribution(reviewAgents, opts.Reviewers)
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distribution, terminal.Color(terminal.Reset))
	} else if opts.Verbose && len(opts.ReviewerAgents) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), opts.ReviewerAgents[0], terminal.Color(terminal.Reset))
	}

	// Pre-compute the git diff once and share it across all reviewers.
	// Always compute (even for codex-only) so we can short-circuit empty diffs.
	diff, err := git.GetDiff(ctx, resolvedBaseRef, opts.WorkDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to get diff: %v", err)
		return domain.ExitError
	}

	// Short-circuit: no changes means nothing to review
	if diff == "" {
		logger.Logf(terminal.StyleSuccess, "No changes detected between HEAD and %s. Nothing to review.", resolvedBaseRef)
		return domain.ExitNoFindings
	}

	if opts.Verbose {
		logger.Logf(terminal.StyleDim, "Diff size: %d bytes", len(diff))
	}

	// Pass precomputed diff to agents that need it (Claude, Gemini).
	// Codex ignores it (built-in diff via --base).
	diffPrecomputed := agent.AgentsNeedDiff(reviewAgents)

	if opts.Verbose && opts.UseRefFile {
		logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
	}

	// Resolve review phases. phaseFromAuto tracks whether the final phase
	// configuration came from auto-phase (true) vs CLI `--phase` (false).
	// Only auto-phase-derived multi-phase runs consult arch_reviewer /
	// diff_reviewer; explicit `--phase` flags use the generic reviewer role.
	phaseStr := opts.Phase
	phaseFromAuto := false
	var useGroupedSpecs bool
	var groupedSpecs []runner.ReviewerSpec
	// mediumDiffCount > 0 → auto-phase chose arch,diff and the diff phase
	// reviewer count must come from medium_diff_reviewers (not opts.Reviewers).
	mediumDiffCount := 0
	if shouldUseAutoPhase(opts) {
		if !diffSizeClassified {
			if opts.Verbose {
				logger.Logf(terminal.StyleWarning, "Auto-phase: diff size classification failed earlier, using default phase")
			}
		} else {
			size := diffSize
			fileCount := diffFileCount
			lineCount := diffLineCount
			if opts.Verbose {
				logger.Logf(terminal.StyleDim, "Auto-phase: diff is %s (%d files, %d lines)", size, fileCount, lineCount)
			}
			// Resolve per-phase agent backends for the grouped path.
			// arch_reviewer_agent / diff_reviewer_agents overrides take effect
			// here; unset overrides fall back to ReviewerAgents (matching the
			// pre-Round-9 behaviour where every phase used the same cohort).
			archAgentForPhase, diffAgentsForPhase, agentBuildErr := buildPhaseAgents(opts, sizeStr)
			if agentBuildErr != nil {
				logger.Logf(terminal.StyleError, "Failed to build per-phase reviewer agents: %v", agentBuildErr)
				return domain.ExitError
			}
			apr := resolveAutoPhase(size, diff, opts.Guidance, diffPrecomputed,
				archAgentForPhase, diffAgentsForPhase,
				opts.DiffGroups, opts.MediumDiffReviewers)
			phaseStr = apr.PhaseStr
			groupedSpecs = apr.GroupedSpecs
			useGroupedSpecs = apr.UseGrouped
			mediumDiffCount = apr.MediumDiffCount
			// Enable per-phase role rebinding (arch_reviewer/diff_reviewer) only
			// for medium/large diffs. Small diffs use flat diff review with the
			// generic reviewer role.
			phaseFromAuto = size != git.DiffSizeSmall
			if opts.Verbose {
				switch {
				case apr.UseGrouped:
					logger.Logf(terminal.StyleInfo, "Auto-phase: grouped (diff_groups=%d, arch+%d diff groups)",
						opts.DiffGroups, len(groupedSpecs)-1)
				case apr.MediumDiffCount > 0 && apr.FallbackReason != "":
					logger.Logf(terminal.StyleWarning,
						"Auto-phase: arch,diff (medium_diff_reviewers=%d) — fallback: %s",
						apr.MediumDiffCount, apr.FallbackReason)
				case apr.MediumDiffCount > 0:
					logger.Logf(terminal.StyleInfo,
						"Auto-phase: arch,diff (medium_diff_reviewers=%d)", apr.MediumDiffCount)
				case apr.FallbackReason != "":
					logger.Logf(terminal.StyleWarning, "Auto-phase: falling back to arch,diff (%s)", apr.FallbackReason)
				}
			}
		}
	}

	// Build runner: phase-based (NewWithSpecs) or legacy (New)
	runnerConfig := runner.Config{
		Reviewers:       opts.Reviewers,
		Concurrency:     opts.Concurrency,
		BaseRef:         resolvedBaseRef,
		Timeout:         opts.Timeout,
		Retries:         opts.Retries,
		Verbose:         opts.Verbose,
		WorkDir:         opts.WorkDir,
		Guidance:        opts.Guidance,
		UseRefFile:      opts.UseRefFile,
		Diff:            diff,
		DiffPrecomputed: diffPrecomputed,
	}

	var r *runner.Runner
	actualReviewers := opts.Reviewers
	if useGroupedSpecs {
		// Auto-phase grouped path: per-phase model/effort rebind using
		// ResolveReviewer with spec.Phase ("arch"/"diff"). arch_reviewer /
		// diff_reviewer overrides take effect here.
		for i := range groupedSpecs {
			groupedSpecs[i].Agent = rebindSpecAgent(groupedSpecs[i].Agent, groupedSpecs[i].Phase)
		}
		actualReviewers = len(groupedSpecs)
		r, err = runner.NewWithSpecs(runnerConfig, groupedSpecs, logger)
	} else if phaseStr != "" {
		// Auto-phase medium/large fallback paths supply MediumDiffCount so the
		// diff phase reviewer count comes from medium_diff_reviewers (NOT
		// --reviewers, which is reserved for flat / explicit --phase paths).
		totalForParse := opts.Reviewers
		if mediumDiffCount > 0 {
			totalForParse = mediumDiffCount + 1 // +1 for arch
		}
		phases, phaseErr := parsePhases(phaseStr, totalForParse)
		if phaseErr != nil {
			logger.Logf(terminal.StyleError, "Invalid --phase: %v", phaseErr)
			return domain.ExitError
		}
		if opts.Verbose {
			for _, p := range phases {
				logger.Logf(terminal.StyleDim, "Phase %q: %d reviewer(s)", p.Phase, p.ReviewerCount)
			}
		}
		specs, specErr := runner.BuildReviewerSpecs(phases, reviewAgents, opts.Guidance, diff, diffPrecomputed)
		if specErr != nil {
			logger.Logf(terminal.StyleError, "Phase config error: %v", specErr)
			return domain.ExitError
		}
		// Per-phase rebind: only when phases were produced by auto-phase.
		// Explicit `--phase` flags use the generic reviewer role per the
		// documented contract (flat review path).
		if phaseFromAuto {
			for i := range specs {
				specs[i].Agent = rebindSpecAgent(specs[i].Agent, specs[i].Phase)
			}
		}
		actualReviewers = len(specs)
		r, err = runner.NewWithSpecs(runnerConfig, specs, logger)
	} else {
		r, err = runner.New(runnerConfig, reviewAgents, logger)
	}
	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
	}

	if opts.Concurrency < actualReviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), actualReviewers, opts.Concurrency, opts.Base, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), actualReviewers, opts.Base, terminal.Color(terminal.Reset))
	}

	// Start PR feedback summarizer in parallel with reviewers (if enabled, reviewing a PR, and FP filter is on)
	// Skip if FP filter is disabled since the feedback summary is only consumed by the FP filter
	var priorFeedback string
	var feedbackWg sync.WaitGroup
	if opts.PRFeedbackEnabled && opts.DetectedPR != "" && opts.FPFilterEnabled {
		logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
			opts.DetectedPR, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		feedbackWg.Add(1)
		go func() {
			defer feedbackWg.Done()

			// Determine which agent to use for feedback summarization
			feedbackAgentName := opts.PRFeedbackAgent
			if feedbackAgentName == "" {
				feedbackAgentName = opts.SummarizerAgent
			}

			cliSumModelPR, legacySumModelPR := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
			prSpec := modelconfig.Resolve(
				opts.Models, sizeStr, modelconfig.RolePRFeedback, feedbackAgentName,
				cliSumModelPR, "",
				legacySumModelPR, "",
			)
			summarizer := feedback.NewSummarizer(feedbackAgentName, prSpec.Model, prSpec.Effort, opts.Verbose, logger)
			feedbackCtx, feedbackCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
			defer feedbackCancel()

			summary, err := summarizer.Summarize(feedbackCtx, opts.DetectedPR)
			if err != nil {
				// Distinguish feedback-specific timeout from parent context cancellation
				if ctx.Err() != nil {
					// Parent context was canceled (e.g., user interrupt) — don't log as timeout
					return
				}
				if feedbackCtx.Err() == context.DeadlineExceeded {
					logger.Logf(terminal.StyleWarning, "PR feedback summarizer timed out after %s", opts.SummarizerTimeout)
					return
				}
				logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: %v", err)
				return
			}
			if summary != "" {
				logger.Log("PR feedback summarized", terminal.StyleSuccess)
			} else {
				logger.Log("No relevant PR feedback found", terminal.StyleDim)
			}
			priorFeedback = summary
		}()
	}

	results, wallClock, err := r.Run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	// Build statistics
	stats := runner.BuildStats(results, actualReviewers, wallClock)

	// Check if all reviewers failed
	if stats.AllFailed() {
		logger.Log("All reviewers failed", terminal.StyleError)
		return domain.ExitError
	}

	// Aggregate and summarize findings
	allFindings := runner.CollectFindings(results)
	aggregated := domain.AggregateFindings(allFindings)

	// Cross-check: run only for grouped diff reviews with >=2 groups.
	var ccResult *summarizer.CrossCheckResult
	if useGroupedSpecs && opts.CrossCheckEnabled && ctx.Err() == nil {
		// Lazy CLI availability check: only verify cross-check agent CLIs are
		// installed when we are about to actually run cross-check. Non-grouped
		// paths (small diff / explicit --phase / --no-auto-phase / arch,diff)
		// skip this entirely so users without the cross-check CLI can still
		// run those review modes.
		if err := agent.ValidateAgentNames(ccAgentNames); err != nil {
			logger.Logf(terminal.StyleError, "Invalid cross-check agent: %v", err)
			return domain.ExitError
		}
		for _, spec := range ccSpecs {
			ccAg, err := agent.NewAgentWithOptions(spec.Name, agent.AgentOptions{Model: spec.Options.Model, Effort: spec.Options.Effort})
			if err != nil {
				logger.Logf(terminal.StyleError, "Invalid cross-check agent %q: %v", spec.Name, err)
				return domain.ExitError
			}
			if err := ccAg.IsAvailable(); err != nil {
				logger.Logf(terminal.StyleError, "%s CLI not found (cross-check): %v", spec.Name, err)
				return domain.ExitError
			}
		}

		ccCtx := buildCrossCheckContext(aggregated, groupedSpecs, results)
		if len(ccCtx.Groups) >= 2 {
			ccSpinner := terminal.NewPhaseSpinner("Cross-checking groups")
			ccSpinnerCtx, ccSpinnerCancel := context.WithCancel(ctx)
			ccSpinnerDone := make(chan struct{})
			go func() {
				ccSpinner.Run(ccSpinnerCtx)
				close(ccSpinnerDone)
			}()

			ccRunCtx, ccRunCancel := context.WithTimeout(ctx, opts.CrossCheckTimeout)
			ccResult = summarizer.CrossCheck(ccRunCtx, ccSpecs, ccCtx, opts.Verbose, logger)
			ccRunCancel()
			ccSpinnerCancel()
			<-ccSpinnerDone
			stats.CrossCheckDuration = ccResult.Duration

			if ccResult.Skipped && ctx.Err() == nil {
				if ccResult.SkipReason != "" {
					logger.Logf(terminal.StyleWarning, "Cross-check skipped (%s)", ccResult.SkipReason)
				}
			} else if ccResult.SkipReason != "" && ctx.Err() == nil {
				// Partial failure: some agents succeeded
				logger.Logf(terminal.StyleWarning, "Cross-check partial (%s)", ccResult.SkipReason)
			}
		}
	}

	// Run summarizer with spinner
	phaseSpinner := terminal.NewPhaseSpinner("Summarizing")
	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		phaseSpinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	summarizerCtx, summarizerCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
	defer summarizerCancel()

	summaryResult, err := summarizer.Summarize(summarizerCtx, opts.SummarizerAgent, summarizer.SummarizeOptions{Model: summSpec.Model, Effort: summSpec.Effort}, aggregated, ccResult, opts.Verbose, logger)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		if summarizerCtx.Err() == context.DeadlineExceeded {
			logger.Logf(terminal.StyleError, "Summarizer timed out after %s", opts.SummarizerTimeout)
		} else {
			logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		}
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Wait for PR feedback summarizer to complete
	feedbackWg.Wait()

	var fpFilteredCount int
	var fpRemoved []domain.FPRemovedInfo
	if opts.FPFilterEnabled && summaryResult.ExitCode == 0 && len(summaryResult.Grouped.Findings) > 0 && ctx.Err() == nil {
		fpSpinner := terminal.NewPhaseSpinner("Filtering false positives")
		fpSpinnerCtx, fpSpinnerCancel := context.WithCancel(ctx)
		fpSpinnerDone := make(chan struct{})
		go func() {
			fpSpinner.Run(fpSpinnerCtx)
			close(fpSpinnerDone)
		}()

		fpCtx, fpCancel := context.WithTimeout(ctx, opts.FPFilterTimeout)
		defer fpCancel()
		cliSumModelFP, legacySumModelFP := cliOrLegacy(opts.SummarizerModel, opts.SummarizerModelFromCLI)
		fpSpec := modelconfig.Resolve(
			opts.Models, sizeStr, modelconfig.RoleFPFilter, opts.SummarizerAgent,
			cliSumModelFP, "",
			legacySumModelFP, "",
		)
		fpFilter := fpfilter.New(opts.SummarizerAgent, fpSpec.Model, fpSpec.Effort, opts.FPThreshold, opts.Verbose, logger)
		fpResult := fpFilter.Apply(fpCtx, summaryResult.Grouped, priorFeedback, stats.SuccessfulReviewers)
		fpSpinnerCancel()
		<-fpSpinnerDone

		if fpResult != nil && fpResult.Skipped && ctx.Err() == nil {
			logger.Logf(terminal.StyleWarning, "FP filter skipped (%s): showing all findings", fpResult.SkipReason)
		}
		if fpResult != nil {
			summaryResult.Grouped = fpResult.Grouped
			fpFilteredCount = fpResult.RemovedCount
			stats.FPFilterDuration = fpResult.Duration

			for _, r := range fpResult.Removed {
				fpRemoved = append(fpRemoved, domain.FPRemovedInfo{
					Sources:   r.Finding.Sources,
					FPScore:   r.FPScore,
					Reasoning: r.Reasoning,
					Title:     r.Finding.Title,
				})
			}
		}
	}
	stats.FPFilteredCount = fpFilteredCount

	if ctx.Err() != nil {
		return domain.ExitInterrupted
	}

	var excludeFiltered []domain.FindingGroup
	if len(opts.ExcludePatterns) > 0 {
		f, err := filter.New(opts.ExcludePatterns)
		if err != nil {
			logger.Logf(terminal.StyleError, "Invalid exclude pattern: %v", err)
			return domain.ExitError
		}
		preExclude := summaryResult.Grouped.Findings
		summaryResult.Grouped = f.Apply(summaryResult.Grouped)
		excludeFiltered = diffFindingGroups(preExclude, summaryResult.Grouped.Findings)
	}

	// Build disposition map for LGTM annotation
	dispositions := domain.BuildDispositions(
		len(aggregated),
		summaryResult.Grouped.Info,
		fpRemoved,
		excludeFiltered,
		summaryResult.Grouped.Findings,
	)
	// Severity reconcile now lives in summarizer.backfillSeverity (3 rules:
	// empty Sources → blocking; any aggregated source blocking → blocking;
	// otherwise default to advisory). Keeping a second pass here would
	// double-apply and conflict with the single source of truth.

	// Compute overall Verdict (and Ok) from post-filter state.
	// Cross-check degradation (Partial or non-structural Skipped) is folded
	// into ccAdvisory so a clean grouped run cannot LGTM while the cross-check
	// layer silently lost coverage (split-brain prevention). Keeping domain
	// ComputeVerdict pure, degradation interpretation lives at this boundary.
	ccBlocking := ccResult.HasBlockingFindings()
	ccAdvisory := ccResult != nil && !ccBlocking && (ccResult.HasAdvisoryFindings() || ccResult.IsDegraded())
	summaryResult.Grouped.ComputeVerdict(ccBlocking, ccAdvisory)

	// Render and print report
	if opts.Format == "json" {
		jsonBytes, jsonErr := runner.RenderJSON(&summaryResult.Grouped, ccResult)
		if jsonErr != nil {
			logger.Logf(terminal.StyleError, "JSON rendering failed: %v", jsonErr)
			return domain.ExitError
		}
		fmt.Println(string(jsonBytes))
	} else {
		report := runner.RenderReport(summaryResult.Grouped, summaryResult, stats, ccResult)
		fmt.Println(report)
	}

	if summaryResult.ExitCode != 0 {
		return domain.ExitError
	}

	// Handle PR actions based on overall verdict.
	// "ok"        → LGTM path, exit 0
	// "advisory"  → findings path, exit 0 (unless --strict, then 1)
	// "blocking"  → findings path, exit 1
	verdict := summaryResult.Grouped.Verdict
	if verdict == "ok" {
		return handleLGTM(ctx, opts, allFindings, aggregated, dispositions, stats, logger)
	}

	findingsCode, finalVerdict := handleFindings(ctx, opts, summaryResult.Grouped, aggregated, ccResult, ccBlocking, ccAdvisory, verdict, opts.Strict, stats, logger)
	return applyVerdictExitPolicy(finalVerdict, opts.Strict, findingsCode)
}

// applyVerdictExitPolicy maps a verdict + strict flag + handleFindings exit code
// to the final exit code per Part C policy:
//
//	verdict=="ok"       → findingsCode unchanged (caller handles LGTM branch)
//	verdict=="advisory" → 0 unless strict; strict keeps findingsCode
//	verdict=="blocking" → findingsCode unchanged (1 on findings)
//	propagates non-ExitFindings codes (e.g. interrupted/error) unchanged
func applyVerdictExitPolicy(verdict string, strict bool, findingsCode domain.ExitCode) domain.ExitCode {
	if verdict == "advisory" && !strict && findingsCode == domain.ExitFindings {
		return domain.ExitNoFindings
	}
	return findingsCode
}

// isLGTM reports whether the review result qualifies for LGTM:
// shouldUseAutoPhase returns true when auto-phase is enabled and no explicit
// --phase override was provided. Explicit --phase always takes precedence.
func shouldUseAutoPhase(opts ReviewOpts) bool {
	return opts.AutoPhase && opts.Phase == ""
}

// no grouped findings AND no blocking cross-check findings.
// ccResult may be nil (nil-safe via CrossCheckResult.HasBlockingFindings).
func isLGTM(grouped domain.GroupedFindings, cc *summarizer.CrossCheckResult) bool {
	return !grouped.HasFindings() && !cc.HasBlockingFindings()
}

// parsePhases converts a comma-separated phase string into PhaseConfigs.
// "arch" → 1 arch reviewer; "diff" → N diff reviewers; "arch,diff" → 1 arch + remaining diff.
// Returns an error for unknown phase tokens, duplicates, or insufficient reviewer budget.
func parsePhases(phaseStr string, totalReviewers int) ([]runner.PhaseConfig, error) {
	if totalReviewers < 1 {
		return nil, fmt.Errorf("totalReviewers must be >= 1, got %d", totalReviewers)
	}

	var phases []runner.PhaseConfig
	remaining := totalReviewers
	seen := map[string]bool{}

	parts := strings.Split(phaseStr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if seen[p] {
			return nil, fmt.Errorf("duplicate phase %q", p)
		}
		switch p {
		case "arch":
			if remaining < 1 {
				return nil, fmt.Errorf("not enough reviewers for phase %q (need 1, have %d)", p, remaining)
			}
			phases = append(phases, runner.PhaseConfig{
				Phase:         "arch",
				ReviewerCount: 1,
			})
			remaining--
			seen[p] = true
		case "diff":
			if remaining < 1 {
				return nil, fmt.Errorf("not enough reviewers for phase %q (need 1, have %d)", p, remaining)
			}
			phases = append(phases, runner.PhaseConfig{
				Phase:         "diff",
				ReviewerCount: remaining,
			})
			remaining = 0
			seen[p] = true
		default:
			return nil, fmt.Errorf("unknown phase %q (valid: arch, diff)", p)
		}
	}
	return phases, nil
}

// resolveCrossCheckAgents returns the resolved cross-check agent names and
// per-agent models. CrossCheckModel is REQUIRED when cross-check is enabled
// and must be a comma-separated list whose count matches CrossCheckAgent
// (after fallback to SummarizerAgent when CrossCheckAgent is unset). Agents
// and models are paired positionally: agents[i] uses models[i].
//
// Returns (nil, nil, nil) when cross-check is disabled.
//
// NOTE: agent.ParseAgentNames("") returns []string{DefaultAgent} ("codex"),
// so we must check the raw string BEFORE calling ParseAgentNames to detect
// a truly-unset CrossCheckAgent and fall back to SummarizerAgent. This is
// belt-and-suspenders even if config.Resolve already copies the value,
// because the fallback here is the canonical resolution point.
func resolveCrossCheckAgents(opts ReviewOpts) ([]string, []string, error) {
	if !opts.CrossCheckEnabled {
		return nil, nil, nil
	}
	if strings.TrimSpace(opts.CrossCheckModel) == "" {
		return nil, nil, fmt.Errorf("--cross-check-model is required when cross-check is enabled (env: ACR_CROSS_CHECK_MODEL); supply a comma-separated model list paired 1:1 with --cross-check-agent")
	}
	rawModels := strings.Split(opts.CrossCheckModel, ",")
	models := make([]string, 0, len(rawModels))
	for _, m := range rawModels {
		m = strings.TrimSpace(m)
		if m == "" {
			return nil, nil, fmt.Errorf("--cross-check-model contains an empty entry; check for leading/trailing/duplicate commas")
		}
		models = append(models, m)
	}
	agentSpec := opts.CrossCheckAgent
	if strings.TrimSpace(agentSpec) == "" {
		agentSpec = opts.SummarizerAgent
	}
	names := agent.ParseAgentNames(agentSpec)
	if len(names) != len(models) {
		return nil, nil, fmt.Errorf("--cross-check-agent (%d agents) and --cross-check-model (%d models) must have same count; agents and models are paired by position", len(names), len(models))
	}
	return names, models, nil
}

// buildCrossCheckContext assembles the cross-check input context from review
// specs, reviewer results, and aggregated findings.
func buildCrossCheckContext(findings []domain.AggregatedFinding, specs []runner.ReviewerSpec, results []domain.ReviewerResult) summarizer.CrossCheckContext {
	// Collect unique group keys from specs (preserving order of first appearance).
	groupInfos := make([]summarizer.GroupInfo, 0, len(specs))
	seenKey := make(map[string]bool, len(specs))
	for _, s := range specs {
		if s.GroupKey == "" || seenKey[s.GroupKey] {
			continue
		}
		seenKey[s.GroupKey] = true
		groupInfos = append(groupInfos, summarizer.GroupInfo{
			GroupKey:    s.GroupKey,
			Phase:       s.Phase,
			TargetFiles: s.TargetFiles,
			FullDiff:    s.Phase == "arch",
		})
	}

	// Aggregate outcomes by group key. A group succeeds if >=1 reviewer for it
	// returned without timeout/auth failure.
	outcomeByKey := make(map[string]*summarizer.GroupOutcome, len(groupInfos))
	for _, g := range groupInfos {
		outcomeByKey[g.GroupKey] = &summarizer.GroupOutcome{GroupKey: g.GroupKey}
	}
	// Map reviewer id -> group key via specs (uses authoritative ReviewerID field).
	idToKey := make(map[int]string, len(specs))
	for _, s := range specs {
		idToKey[s.ReviewerID] = s.GroupKey
	}
	for _, r := range results {
		key := idToKey[r.ReviewerID]
		o, ok := outcomeByKey[key]
		if !ok {
			continue
		}
		if r.TimedOut {
			o.TimedOut = true
		}
		if r.AuthFailed {
			o.AuthFailed = true
		}
		if !r.TimedOut && !r.AuthFailed && r.ExitCode == 0 {
			o.Succeeded = true
		}
		o.FindingCount += len(r.Findings)
	}

	outcomes := make([]summarizer.GroupOutcome, 0, len(groupInfos))
	for _, g := range groupInfos {
		outcomes = append(outcomes, *outcomeByKey[g.GroupKey])
	}

	return summarizer.CrossCheckContext{
		Findings: findings,
		Groups:   groupInfos,
		Outcomes: outcomes,
	}
}

// autoPhaseResult holds the outcome of resolveAutoPhase.
type autoPhaseResult struct {
	PhaseStr        string                // non-empty → use legacy phase path
	GroupedSpecs    []runner.ReviewerSpec // non-nil → use grouped diff path
	UseGrouped      bool
	FallbackReason  string // non-empty when UseGrouped was downgraded to false
	MediumDiffCount int    // 0 = not an arch,diff fallback; otherwise the diff-phase reviewer count to use with parsePhases (arch=1 + this)
}

// resolveAutoPhase determines phase configuration based on diff size and the
// new auto-phase knobs.
//
//   - diffGroups: target group count for the grouped (large) path; capped by
//     the actual file count to keep at least 1 file per group.
//   - mediumDiffReviewers: diff-phase reviewer count for the medium path
//     (arch,diff) and for any large fallback to arch,diff.
//
// archAgent and diffAgents are passed through to buildGroupedDiffSpecs so the
// arch and diff phases can use distinct agent backends per
// arch_reviewer_agent / diff_reviewer_agents config.
//
// `reviewers` is intentionally NOT a parameter: --reviewers is now reserved
// for the flat path (auto-phase OFF / small / explicit --phase). Auto-phase
// medium/large paths derive their concurrency from these dedicated knobs.
func resolveAutoPhase(
	size git.DiffSize,
	diff, guidance string,
	diffPrecomputed bool,
	archAgent agent.Agent, diffAgents []agent.Agent,
	diffGroups int,
	mediumDiffReviewers int,
) autoPhaseResult {
	switch size {
	case git.DiffSizeSmall:
		// Small: flat diff path; reviewer count is taken from --reviewers by caller.
		return autoPhaseResult{PhaseStr: "diff"}
	case git.DiffSizeLarge:
		fileCount := len(git.ParseDiffSections(diff))
		// Sanity cap: 1 group must contain at least 1 file.
		effectiveGroups := diffGroups
		if effectiveGroups > fileCount {
			effectiveGroups = fileCount
		}
		if effectiveGroups < 2 {
			// Grouped path needs >=2 diff groups to be meaningful.
			return autoPhaseResult{
				PhaseStr:        "arch,diff",
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason: fmt.Sprintf(
					"only %d non-empty diff group(s) possible (file_count=%d, diff_groups=%d)",
					effectiveGroups, fileCount, diffGroups),
			}
		}
		specs, err := buildGroupedDiffSpecs(diff, guidance, diffPrecomputed, archAgent, diffAgents, effectiveGroups)
		if err != nil {
			return autoPhaseResult{
				PhaseStr:        "arch,diff",
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason:  fmt.Sprintf("buildGroupedDiffSpecs failed: %v", err),
			}
		}
		// specs[0] is always the arch spec; remaining entries are diff groups.
		diffGroupCount := len(specs) - 1
		if diffGroupCount < 2 {
			return autoPhaseResult{
				PhaseStr:        "arch,diff",
				MediumDiffCount: mediumDiffReviewers,
				FallbackReason:  fmt.Sprintf("only %d non-empty diff group(s) after building", diffGroupCount),
			}
		}
		return autoPhaseResult{
			GroupedSpecs: specs,
			UseGrouped:   true,
		}
	default: // medium
		// Medium: arch (1) + diff (mediumDiffReviewers).
		return autoPhaseResult{
			PhaseStr:        "arch,diff",
			MediumDiffCount: mediumDiffReviewers,
		}
	}
}

// buildPhaseAgents constructs the arch-phase agent and diff-phase agent slice
// used by the auto-phase grouped diff path. It honours per-phase overrides
// (opts.ArchReviewerAgent, opts.DiffReviewerAgents) and falls back to
// opts.ReviewerAgents when an override is unset, preserving pre-Round-9
// behaviour. Each constructed agent picks its model/effort via
// modelconfig.ResolveReviewer with the matching phase ("arch" or "diff") so
// arch_reviewer / diff_reviewer model layers continue to apply.
func buildPhaseAgents(opts ReviewOpts, sizeStr string) (agent.Agent, []agent.Agent, error) {
	// Arch agent name: explicit override > first reviewer agent.
	archAgentName := opts.ArchReviewerAgent
	if archAgentName == "" {
		if len(opts.ReviewerAgents) == 0 {
			return nil, nil, fmt.Errorf("reviewer_agents is empty; cannot derive arch reviewer")
		}
		archAgentName = opts.ReviewerAgents[0]
	}
	cliRevModel, legacyRevModel := cliOrLegacy(opts.ReviewerModel, opts.ReviewerModelFromCLI)
	archSpec := modelconfig.ResolveReviewer(
		opts.Models, sizeStr, "arch", archAgentName,
		cliRevModel, "",
		legacyRevModel, "",
	)
	archAgent, err := agent.NewAgentWithOptions(archAgentName, agent.AgentOptions{Model: archSpec.Model, Effort: archSpec.Effort})
	if err != nil {
		return nil, nil, fmt.Errorf("arch reviewer %q: %w", archAgentName, err)
	}
	if err := archAgent.IsAvailable(); err != nil {
		return nil, nil, fmt.Errorf("arch reviewer %q unavailable: %w", archAgentName, err)
	}

	// Diff agent names: explicit override > all reviewer agents.
	diffAgentNames := opts.DiffReviewerAgents
	if len(diffAgentNames) == 0 {
		diffAgentNames = opts.ReviewerAgents
	}
	if len(diffAgentNames) == 0 {
		return nil, nil, fmt.Errorf("no diff-phase agents available (reviewer_agents and diff_reviewer_agents both empty)")
	}
	diffAgents := make([]agent.Agent, 0, len(diffAgentNames))
	for _, name := range diffAgentNames {
		spec := modelconfig.ResolveReviewer(
			opts.Models, sizeStr, "diff", name,
			cliRevModel, "",
			legacyRevModel, "",
		)
		a, err := agent.NewAgentWithOptions(name, agent.AgentOptions{Model: spec.Model, Effort: spec.Effort})
		if err != nil {
			return nil, nil, fmt.Errorf("diff reviewer %q: %w", name, err)
		}
		if err := a.IsAvailable(); err != nil {
			return nil, nil, fmt.Errorf("diff reviewer %q unavailable: %w", name, err)
		}
		diffAgents = append(diffAgents, a)
	}
	return archAgent, diffAgents, nil
}

// buildGroupedDiffSpecs creates ReviewerSpecs for grouped diff review.
// It parses the precomputed diff into sections, groups them, and generates:
//   - 1 arch spec (full diff, GroupKey="arch") using archAgent
//   - N diff specs (per-group filtered diff, GroupKey="g01"..."gNN") with
//     diffAgents assigned round-robin per non-empty diff group
//
// maxDiffGroups is supplied by the caller (resolveAutoPhase) from the
// dedicated `diff_groups` knob, capped at the actual file count so that
// every group has at least 1 file.
//
// archAgent and diffAgents are independent so per-phase reviewer overrides
// (arch_reviewer_agent / diff_reviewer_agents) can route phases to different
// agent backends.
func buildGroupedDiffSpecs(
	fullDiff, guidance string,
	diffPrecomputed bool,
	archAgent agent.Agent,
	diffAgents []agent.Agent,
	maxDiffGroups int,
) ([]runner.ReviewerSpec, error) {
	sections := git.ParseDiffSections(fullDiff)
	if len(sections) == 0 {
		return nil, fmt.Errorf("no diff sections found in precomputed diff")
	}

	groups := git.GroupDiffSections(sections, 0, 0, maxDiffGroups)
	if len(groups) == 0 {
		return nil, fmt.Errorf("grouping produced no groups")
	}

	specs := make([]runner.ReviewerSpec, 0, 1+len(groups))

	// 1. Arch reviewer: full diff (ReviewerID=1)
	specs = append(specs, runner.ReviewerSpec{
		ReviewerID:      1,
		Agent:           archAgent,
		Phase:           "arch",
		GroupKey:        "arch",
		Guidance:        guidance,
		Diff:            fullDiff,
		DiffPrecomputed: diffPrecomputed,
	})

	// 2. Per-group diff reviewers (ReviewerID=2, 3, ...)
	// Compute ReviewerID once per non-skipped group so spec ordering and the
	// diff-phase agent round-robin stay in lockstep (empty groups skip both
	// counters together). diffReviewerIdx is 1-based per AgentForReviewer's
	// contract; the 1st surviving diff group always uses diffAgents[0].
	diffReviewerIdx := 0
	for _, group := range groups {
		groupDiff := git.JoinDiffSections(group.Sections)
		if groupDiff == "" {
			// Empty group diff after splitting precomputed diff is unexpected.
			// Skip (budget adjusts accordingly).
			continue
		}
		var targetFiles []string
		for _, s := range group.Sections {
			targetFiles = append(targetFiles, s.FilePath)
		}
		reviewerID := len(specs) + 1
		diffReviewerIdx++
		diffAgent := agent.AgentForReviewer(diffAgents, diffReviewerIdx)
		specs = append(specs, runner.ReviewerSpec{
			ReviewerID:      reviewerID,
			Agent:           diffAgent,
			Phase:           "diff",
			GroupKey:        group.Key,
			Guidance:        guidance,
			Diff:            groupDiff,
			DiffPrecomputed: true,
			TargetFiles:     targetFiles,
		})
	}

	return specs, nil
}

// diffFindingGroups returns groups present in before but not in after.
// Relies on filter.Apply preserving order, so after is an ordered subsequence.
func diffFindingGroups(before, after []domain.FindingGroup) []domain.FindingGroup {
	j := 0
	var removed []domain.FindingGroup
	for i := range before {
		if j < len(after) && slices.Equal(before[i].Sources, after[j].Sources) {
			j++
		} else {
			removed = append(removed, before[i])
		}
	}
	return removed
}

// formatSpec formats a model/effort pair for the verbose effective matrix log.
// Empty fields are shown as "(default)" to distinguish "not set" from a blank value.
func formatSpec(opts agent.AgentOptions) string {
	model := opts.Model
	if model == "" {
		model = "(default)"
	}
	effort := opts.Effort
	if effort == "" {
		effort = "(default)"
	}
	return fmt.Sprintf("%s [%s]", model, effort)
}

// formatSizeStr returns the size string for display, substituting "(unknown)"
// when classification was not available (empty string).
func formatSizeStr(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
