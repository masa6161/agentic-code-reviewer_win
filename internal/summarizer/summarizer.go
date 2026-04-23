// Package summarizer provides finding summarization via LLM.
package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const groupPrompt = `# Code Review Summarizer

You are grouping results from repeated code review runs.

Input: a JSON array of objects, each with "id" (input identifier), "text" (the finding),
"reviewers" (list of reviewer IDs that found it), and "severity"
("blocking" | "advisory") indicating the raw reviewer-assigned severity.

Task:
- Cluster messages that describe the same underlying issue.
- Create a short, precise title per group.
- Keep groups distinct; do not merge different issues.
- If something is unique, keep it as its own group.
- Sum up unique reviewer IDs across clustered messages for reviewer_count.
- Track which input ids are represented in each group via "sources".

Output format (JSON only, no extra prose):
{
  "findings": [
    {
      "title": "Short issue title",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [0, 2],
      "severity": "blocking"
    }
  ],
  "info": [
    {
      "title": "Informational note",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [1]
    }
  ]
}

Rules:
- Return ONLY valid JSON.
- Keep excerpts under ~200 characters each.
- Preserve file paths, line numbers, flags, branch names, and commands in excerpts when present.
- If a message includes a file path with line numbers, keep that exact location text in the excerpt.
- "sources" must include all input ids represented in each group.
- reviewer_count = number of unique reviewers that reported any message in this cluster.
- "severity" must be "blocking" or "advisory". Use "blocking" when any clustered reviewer input is severity=blocking; otherwise "advisory". If unsure, default to "advisory".
- Put non-actionable outcomes (e.g., "no diffs", "no changes to review") in "info".
- If the input is empty, return: {"findings": [], "info": []}`

// buildGroupPrompt returns the summarizer prompt, optionally augmented with
// cross-group context extracted from ccResult. The augmentation is informational
// only: the summarizer still clusters by shared issue, not by cross-check links.
func buildGroupPrompt(ccResult *CrossCheckResult) string {
	if ccResult == nil || len(ccResult.Findings) == 0 {
		return groupPrompt
	}
	var b strings.Builder
	b.WriteString(groupPrompt)
	b.WriteString("\n\n## Cross-Group Context\n\n")
	b.WriteString("An upstream cross-check pass identified the following relationships across review groups.\n")
	b.WriteString("Use this context to inform clustering and summaries; do NOT copy these items into findings.\n\n")
	for i, f := range ccResult.Findings {
		title := strings.TrimSpace(f.Title)
		if title == "" {
			title = "(untitled)"
		}
		b.WriteString(fmt.Sprintf("- [%s/%s] %s — groups=%v, related_ids=%v\n",
			strings.ToLower(f.Type), strings.ToLower(f.Severity), title, f.InvolvedGroups, f.RelatedIDs))
		if i >= 19 {
			b.WriteString("- ...\n")
			break
		}
	}
	return b.String()
}

// Result contains the output from the summarizer.
type Result struct {
	Grouped  domain.GroupedFindings
	ExitCode int
	Stderr   string
	RawOut   string
	Duration time.Duration
}

// inputItem represents a single finding for the summarizer input payload.
type inputItem struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	Reviewers []int  `json:"reviewers"`
	Severity  string `json:"severity,omitempty"`
}

// SummarizeOptions bundles model/effort resolution for the summarizer role.
// Empty fields fall back to the agent's built-in defaults.
type SummarizeOptions struct {
	Model  string
	Effort string
}

// Summarize summarizes the aggregated findings using an LLM.
// The agentName parameter specifies which agent to use for summarization.
// The opts parameter carries model and effort overrides (empty = agent defaults).
// If verbose is true, non-fatal errors (like Close failures) are logged.
// If ccResult is non-nil and contains findings, its cross-group context is
// injected into the prompt so the summarizer can reason about related items.
func Summarize(ctx context.Context, agentName string, opts SummarizeOptions, aggregated []domain.AggregatedFinding, ccResult *CrossCheckResult, verbose bool, logger *terminal.Logger) (*Result, error) {
	start := time.Now()

	if len(aggregated) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			Duration: time.Since(start),
		}, nil
	}

	// Create agent
	ag, err := agent.NewAgentWithOptions(agentName, agent.AgentOptions{Model: opts.Model, Effort: opts.Effort})
	if err != nil {
		return nil, err
	}

	// Build input payload
	items := make([]inputItem, len(aggregated))
	for i, a := range aggregated {
		items[i] = inputItem{
			ID:        i,
			Text:      a.Text,
			Reviewers: a.Reviewers,
			Severity:  a.Severity,
		}
	}

	payload, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}

	prompt := buildGroupPrompt(ccResult)

	// Check if context is already canceled
	if ctx.Err() != nil {
		return &Result{
			ExitCode: -1,
			Stderr:   "context canceled",
			Duration: time.Since(start),
		}, nil
	}

	// Execute summary via agent
	execResult, err := ag.ExecuteSummary(ctx, prompt, payload)
	if err != nil {
		// Handle context cancellation
		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}
	// Close errors are non-fatal; defer ensures cleanup on all exit paths.
	// The explicit Close() below handles the primary close; this is a safety net.
	defer func() {
		if err := execResult.Close(); err != nil && verbose {
			logger.Logf(terminal.StyleDim, "summarizer close error (non-fatal): %v", err)
		}
	}()

	// Read all output
	output, err := io.ReadAll(execResult)
	if err != nil {
		// Handle context cancellation
		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	// Close to get exit code and stderr (defer will be a no-op due to sync.Once).
	// Close errors are non-fatal; they only occur on process cleanup issues.
	if err := execResult.Close(); err != nil && verbose {
		logger.Logf(terminal.StyleDim, "summarizer close error (non-fatal): %v", err)
	}
	exitCode := execResult.ExitCode()
	stderr := execResult.Stderr()
	duration := time.Since(start)

	if len(output) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: exitCode,
			Stderr:   stderr,
			Duration: duration,
		}, nil
	}

	// Create parser for this agent's output format
	parser, err := agent.NewSummaryParser(agentName)
	if err != nil {
		return nil, err
	}

	// Parse the output
	grouped, err := parser.Parse(output)
	if err != nil {
		parseErr := "failed to parse summarizer output: " + err.Error()
		if stderr != "" {
			parseErr = stderr + "\n" + parseErr
		}
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: 1,
			Stderr:   parseErr,
			RawOut:   string(output),
			Duration: duration,
		}, nil
	}

	// Backfill GroupKey from source AggregatedFindings into FindingGroups.
	// The LLM doesn't see GroupKey, so we propagate it via Sources indices.
	backfillGroupKeys(grouped, aggregated)

	// Reconcile Severity: if the LLM skipped it, derive from aggregated source
	// severities. Blocking wins on collision.
	backfillSeverity(grouped, aggregated)

	return &Result{
		Grouped:  *grouped,
		ExitCode: exitCode,
		Stderr:   stderr,
		RawOut:   string(output),
		Duration: duration,
	}, nil
}

// backfillGroupKeys populates GroupKey on each FindingGroup by collecting
// unique GroupKeys from the source AggregatedFindings referenced by Sources.
func backfillGroupKeys(grouped *domain.GroupedFindings, aggregated []domain.AggregatedFinding) {
	fill := func(groups []domain.FindingGroup) {
		for i := range groups {
			keys := make(map[string]struct{})
			for _, srcIdx := range groups[i].Sources {
				if srcIdx >= 0 && srcIdx < len(aggregated) {
					if gk := aggregated[srcIdx].GroupKey; gk != "" {
						for _, k := range strings.Split(gk, ",") {
							keys[k] = struct{}{}
						}
					}
				}
			}
			if len(keys) > 0 {
				sorted := make([]string, 0, len(keys))
				for k := range keys {
					sorted = append(sorted, k)
				}
				slices.Sort(sorted)
				groups[i].GroupKey = strings.Join(sorted, ",")
			}
		}
	}
	fill(grouped.Findings)
	fill(grouped.Info)
}

// backfillSeverity is the sole authoritative severity reconciler.
// Rules:
//   - Rule C (allow-list): unknown severity values normalize to "advisory".
//   - Rule A (upgrade): any valid source with Severity=="blocking" → group Severity="blocking".
//   - Rule B (downgrade): group Severity=="blocking" with valid sources but no blocking source
//     → downgrade to "advisory" (LLM-claimed blocking without source citation is downgraded).
//   - Rule B preserves Severity when no valid sources exist (empty Sources or all out-of-range):
//     treated as information loss, not evidence of non-blocking.
//   - Default: empty Severity → "advisory".
//
// Info groups are not mutated (informational by nature).
func backfillSeverity(grouped *domain.GroupedFindings, aggregated []domain.AggregatedFinding) {
	for i := range grouped.Findings {
		fg := &grouped.Findings[i]

		// Rule C: allow-list normalization.
		if fg.Severity != "blocking" && fg.Severity != "advisory" && fg.Severity != "" {
			fg.Severity = "advisory"
		}

		// Inspect sources: count valid index entries and detect any blocking.
		hasBlocking := false
		validSources := 0
		for _, srcIdx := range fg.Sources {
			if srcIdx < 0 || srcIdx >= len(aggregated) {
				continue
			}
			validSources++
			if aggregated[srcIdx].Severity == "blocking" {
				hasBlocking = true
			}
		}

		// Rule A: upgrade on any blocking source.
		if hasBlocking {
			fg.Severity = "blocking"
			continue
		}

		// Rule B: downgrade only when we have valid sources that confirm no blocking.
		// If Sources is empty or all out-of-range, preserve LLM severity (information loss).
		if fg.Severity == "blocking" && validSources > 0 {
			fg.Severity = "advisory"
		}

		// Default: empty → advisory.
		if fg.Severity == "" {
			fg.Severity = "advisory"
		}
	}
}
