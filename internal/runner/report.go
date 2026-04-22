package runner

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxRawOutputLines = 10

// RenderReport renders a terminal report for the review results.
// ccResult may be nil when cross-check was not run.
func RenderReport(
	grouped domain.GroupedFindings,
	summaryResult *summarizer.Result,
	stats domain.ReviewStats,
	ccResult *summarizer.CrossCheckResult,
) string {
	width := terminal.ReportWidth()

	var lines []string

	// Handle summarizer errors
	if summaryResult.ExitCode != 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s✗ Summarizer Error%s", terminal.Color(terminal.Red), terminal.Color(terminal.Reset)))
		lines = append(lines, terminal.Ruler(width, "─"))
		lines = append(lines, fmt.Sprintf("  Exit code: %d", summaryResult.ExitCode))
		if summaryResult.Stderr != "" {
			lines = append(lines, fmt.Sprintf("  Stderr: %s", summaryResult.Stderr))
		}
		if summaryResult.RawOut != "" {
			lines = append(lines, fmt.Sprintf("\n  %sRaw output:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
			rawLines := strings.Split(summaryResult.RawOut, "\n")
			for i, line := range rawLines {
				if i >= maxRawOutputLines {
					break
				}
				lines = append(lines, fmt.Sprintf("  %s%s%s", terminal.Color(terminal.Dim), line, terminal.Color(terminal.Reset)))
			}
		}
		return strings.Join(lines, "\n")
	}

	// Warnings
	var warnings []string
	if stats.ParseErrors > 0 {
		warnings = append(warnings, fmt.Sprintf("JSONL parse errors: %d", stats.ParseErrors))
	}
	if len(stats.FailedReviewers) > 0 {
		warnings = append(warnings, fmt.Sprintf("Failed reviewers: %s", formatReviewersWithAgents(stats.FailedReviewers, stats.ReviewerAgentNames)))
	}
	if len(stats.TimedOutReviewers) > 0 {
		warnings = append(warnings, fmt.Sprintf("Timed out reviewers: %s", formatReviewersWithAgents(stats.TimedOutReviewers, stats.ReviewerAgentNames)))
	}
	if len(stats.AuthFailedReviewers) > 0 {
		warnings = append(warnings, fmt.Sprintf("Auth failed reviewers: %s",
			formatReviewersWithAgents(stats.AuthFailedReviewers, stats.ReviewerAgentNames)))
		seen := make(map[string]bool)
		for _, id := range stats.AuthFailedReviewers {
			agentName := stats.ReviewerAgentNames[id]
			if agentName != "" && !seen[agentName] {
				seen[agentName] = true
				warnings = append(warnings, fmt.Sprintf("  Hint (%s): %s", agentName, agent.AuthHint(agentName)))
			}
		}
	}

	if len(warnings) > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s⚠ Warnings%s", terminal.Color(terminal.Yellow), terminal.Color(terminal.Reset)))
		lines = append(lines, terminal.Ruler(width, "─"))
		for _, w := range warnings {
			lines = append(lines, fmt.Sprintf("  %s•%s %s", terminal.Color(terminal.Yellow), terminal.Color(terminal.Reset), w))
		}
		lines = append(lines, "")
	}

	// Grouped findings section (only when there are findings)
	if grouped.HasFindings() {
		findingWord := "finding"
		if len(grouped.Findings) != 1 {
			findingWord = "findings"
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s%s📋 %d %s%s",
			terminal.Color(terminal.Cyan), terminal.Color(terminal.Bold), len(grouped.Findings), findingWord, terminal.Color(terminal.Reset)))
		lines = append(lines, terminal.Ruler(width, "━"))

		// Render each finding
		for idx, finding := range grouped.Findings {
			title := finding.Title
			if title == "" {
				title = "Untitled"
			}

			lines = append(lines, "")
			confidence := ""
			if stats.TotalReviewers > 0 && finding.ReviewerCount > 0 {
				confidence = fmt.Sprintf(" %s(%d/%d reviewers)%s",
					terminal.Color(terminal.Dim), finding.ReviewerCount, stats.TotalReviewers, terminal.Color(terminal.Reset))
			}
			severityLabel := formatSeverityLabel(finding.Severity)
			lines = append(lines, fmt.Sprintf("%s%s%d.%s %s%s%s%s%s",
				terminal.Color(terminal.Yellow), terminal.Color(terminal.Bold), idx+1, terminal.Color(terminal.Reset),
				severityLabel,
				terminal.Color(terminal.Bold), title, terminal.Color(terminal.Reset), confidence))
			lines = append(lines, terminal.Ruler(width, "─"))

			if finding.Summary != "" {
				wrapped := terminal.WrapText(finding.Summary, width-3, "   ")
				lines = append(lines, wrapped)
			}

			if len(finding.Messages) > 0 {
				lines = append(lines, "")
				lines = append(lines, fmt.Sprintf("   %sEvidence:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
				for _, msg := range finding.Messages {
					if msg != "" {
						wrapped := terminal.WrapText(msg, width-5, fmt.Sprintf("   %s•%s ", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
						lines = append(lines, wrapped)
					}
				}
			}
		}

		lines = append(lines, "")
		lines = append(lines, terminal.Ruler(width, "━"))
	}

	// Cross-check section: show when ccResult is non-nil and has findings, partial, or skipped
	if ccResult != nil && (len(ccResult.Findings) > 0 || ccResult.Partial || ccResult.Skipped) {
		lines = append(lines, "")
		if ccResult.Partial {
			lines = append(lines, fmt.Sprintf("%s⚠ Cross-check ran partially (failed agents: %s) — coverage reduced%s",
				terminal.Color(terminal.Dim), strings.Join(ccResult.FailedAgents, ", "), terminal.Color(terminal.Reset)))
		}
		if ccResult.Skipped && ccResult.SkipReason != "" {
			lines = append(lines, fmt.Sprintf("%s⚠ Cross-check skipped: %s%s",
				terminal.Color(terminal.Dim), ccResult.SkipReason, terminal.Color(terminal.Reset)))
		}
		if len(ccResult.Findings) > 0 {
			lines = append(lines, fmt.Sprintf("%s%s🔗 Cross-Group Findings (%d)%s",
				terminal.Color(terminal.Cyan), terminal.Color(terminal.Bold), len(ccResult.Findings), terminal.Color(terminal.Reset)))
			lines = append(lines, terminal.Ruler(width, "─"))
			for i, cf := range ccResult.Findings {
				title := cf.Title
				if title == "" {
					title = "Untitled"
				}
				sev := cf.Severity
				if sev == "" {
					sev = "advisory"
				}
				lines = append(lines, fmt.Sprintf("  %s%d.%s [%s/%s] %s",
					terminal.Color(terminal.Dim), i+1, terminal.Color(terminal.Reset),
					cf.Type, sev, title))
				if cf.Summary != "" {
					wrapped := terminal.WrapText(cf.Summary, width-5, "     ")
					lines = append(lines, wrapped)
				}
				if len(cf.InvolvedGroups) > 0 {
					lines = append(lines, fmt.Sprintf("     %sgroups: %v%s",
						terminal.Color(terminal.Dim), cf.InvolvedGroups, terminal.Color(terminal.Reset)))
				}
			}
		}
	}

	if stats.FPFilteredCount > 0 {
		findingWord := "finding"
		positiveWord := "positive"
		if stats.FPFilteredCount != 1 {
			findingWord = "findings"
			positiveWord = "positives"
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%sℹ %d %s filtered as likely false %s%s",
			terminal.Color(terminal.Dim), stats.FPFilteredCount, findingWord, positiveWord, terminal.Color(terminal.Reset)))
	}

	if stats.WallClockDuration > 0 || len(stats.ReviewerDurations) > 0 || summaryResult.Duration > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%sTiming:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))

		if stats.WallClockDuration > 0 {
			lines = append(lines, fmt.Sprintf("  %sreviewers: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(stats.WallClockDuration), terminal.Color(terminal.Reset)))
		}

		if len(stats.ReviewerDurations) > 0 {
			durations := make([]float64, 0, len(stats.ReviewerDurations))
			for _, d := range stats.ReviewerDurations {
				durations = append(durations, d.Seconds())
			}
			slices.Sort(durations)

			var sum float64
			for _, d := range durations {
				sum += d
			}
			avg := sum / float64(len(durations))
			min := durations[0]
			max := durations[len(durations)-1]

			lines = append(lines, fmt.Sprintf("  %s  min %.1fs / avg %.1fs / max %.1fs%s",
				terminal.Color(terminal.Dim), min, avg, max, terminal.Color(terminal.Reset)))
		}

		if summaryResult.Duration > 0 {
			lines = append(lines, fmt.Sprintf("  %ssummarizer: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(summaryResult.Duration), terminal.Color(terminal.Reset)))
		}

		if stats.CrossCheckDuration > 0 {
			lines = append(lines, fmt.Sprintf("  %scross-check: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(stats.CrossCheckDuration), terminal.Color(terminal.Reset)))
		}

		if stats.FPFilterDuration > 0 {
			lines = append(lines, fmt.Sprintf("  %sfp-filter: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(stats.FPFilterDuration), terminal.Color(terminal.Reset)))
		}

		if stats.WallClockDuration > 0 && summaryResult.Duration > 0 {
			total := stats.WallClockDuration + summaryResult.Duration + stats.CrossCheckDuration + stats.FPFilterDuration
			lines = append(lines, fmt.Sprintf("  %stotal: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(total), terminal.Color(terminal.Reset)))
		}
	}

	if line := formatVerdictLine(grouped.Verdict); line != "" {
		lines = append(lines, "")
		lines = append(lines, line)
	}

	// LGTM banner: only when grouped is empty AND cross-check is fully clean.
	// Structural skips (e.g., "single-group") are treated as quiet because there
	// was simply not enough material to cross-check — not an error condition.
	ccQuiet := ccResult == nil || (len(ccResult.Findings) == 0 && !ccResult.Partial &&
		(!ccResult.Skipped || summarizer.IsStructuralSkipReason(ccResult.SkipReason)))
	if !grouped.HasFindings() && ccQuiet {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s✓%s %s%sLGTM%s %s(%d/%d reviewers)%s",
			terminal.Color(terminal.Green), terminal.Color(terminal.Reset),
			terminal.Color(terminal.Green), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset),
			terminal.Color(terminal.Dim), stats.SuccessfulReviewers, stats.TotalReviewers, terminal.Color(terminal.Reset)))
	}

	return strings.Join(lines, "\n")
}

// formatSeverityLabel returns a colored "[severity] " prefix suitable for
// prepending a finding title. Empty severity returns an empty string so the
// call site can drop the prefix entirely.
func formatSeverityLabel(severity string) string {
	switch severity {
	case "blocking":
		return fmt.Sprintf("%s[blocking]%s ", terminal.Color(terminal.Red), terminal.Color(terminal.Reset))
	case "advisory":
		return fmt.Sprintf("%s[advisory]%s ", terminal.Color(terminal.Yellow), terminal.Color(terminal.Reset))
	default:
		return ""
	}
}

// formatVerdictLine returns a colored one-liner summarizing the overall verdict.
// Returns an empty string when verdict is empty (no computation happened yet).
func formatVerdictLine(verdict string) string {
	switch verdict {
	case "ok":
		return fmt.Sprintf("%s%s✓ Verdict: ok%s",
			terminal.Color(terminal.Green), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
	case "advisory":
		return fmt.Sprintf("%s%s⚠ Verdict: advisory%s",
			terminal.Color(terminal.Yellow), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
	case "blocking":
		return fmt.Sprintf("%s%s✗ Verdict: blocking%s",
			terminal.Color(terminal.Red), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
	default:
		return ""
	}
}

// RenderCommentMarkdown renders GitHub comment markdown for findings.
// Returns an empty string when there are no findings so callers can
// compose cross-check sections independently.
func RenderCommentMarkdown(
	grouped domain.GroupedFindings,
	totalReviewers int,
	aggregated []domain.AggregatedFinding,
	version string,
) string {
	if len(grouped.Findings) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "## Findings")

	for idx, finding := range grouped.Findings {
		title := finding.Title
		if title == "" {
			title = "Untitled"
		}

		confidence := ""
		if finding.ReviewerCount > 0 {
			confidence = fmt.Sprintf(" (%d/%d reviewers)", finding.ReviewerCount, totalReviewers)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%d. **%s**%s", idx+1, title, confidence))

		if finding.Summary != "" {
			lines = append(lines, "")
			lines = append(lines, finding.Summary)
		}

		if len(finding.Messages) > 0 {
			lines = append(lines, "")
			lines = append(lines, "Evidence:")
			for _, msg := range finding.Messages {
				if msg != "" {
					lines = append(lines, fmt.Sprintf("- %s", msg))
				}
			}
		}
	}

	// Raw findings section
	rawIndices := collectSourceIndices(grouped.Findings)
	rawSection := formatRawFindings(aggregated, rawIndices, totalReviewers)
	if rawSection != "" {
		lines = append(lines, "")
		lines = append(lines, "_Expand for verbatim findings._")
		lines = append(lines, "<details>")
		lines = append(lines, "<summary>Raw findings (verbatim)</summary>")
		lines = append(lines, "")
		lines = append(lines, rawSection)
		lines = append(lines, "</details>")
	}

	lines = append(lines, "")
	lines = append(lines, renderFooter(version))

	return strings.Join(lines, "\n")
}

// AnnotatedComment pairs a reviewer's raw finding text with its pipeline disposition.
type AnnotatedComment struct {
	Text        string
	Disposition domain.Disposition
}

// RenderLGTMMarkdown renders approval comment markdown.
// annotatedComments maps reviewer ID to their findings with disposition annotations.
// If nil, falls back to unannotated rendering.
func RenderLGTMMarkdown(totalReviewers, successfulReviewers int, annotatedComments map[int][]AnnotatedComment, version string) string {
	var lines []string
	lines = append(lines, "## LGTM :white_check_mark:")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("**%d of %d reviewers found no issues.**", successfulReviewers, totalReviewers))

	if len(annotatedComments) > 0 {
		lines = append(lines, "")
		lines = append(lines, "<details>")
		lines = append(lines, "<summary>Reviewer comments</summary>")
		lines = append(lines, "")

		keys := make([]int, 0, len(annotatedComments))
		for k := range annotatedComments {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, id := range keys {
			for _, ac := range annotatedComments[id] {
				annotation := formatDisposition(ac.Disposition)
				if annotation != "" {
					lines = append(lines, fmt.Sprintf("- **Reviewer %d:** %s\n  _%s_", id, ac.Text, annotation))
				} else {
					lines = append(lines, fmt.Sprintf("- **Reviewer %d:** %s", id, ac.Text))
				}
			}
		}
		lines = append(lines, "")
		lines = append(lines, "</details>")
	}

	lines = append(lines, "")
	lines = append(lines, renderFooter(version))

	return strings.Join(lines, "\n")
}

func formatDisposition(d domain.Disposition) string {
	switch d.Kind {
	case domain.DispositionInfo:
		return "Categorized as informational during summarization"
	case domain.DispositionFilteredFP:
		return fmt.Sprintf("Filtered as likely false positive (score %d)", d.FPScore)
	case domain.DispositionFilteredExclude:
		return "Filtered by exclude pattern"
	case domain.DispositionSurvived:
		return "Survived all filters (posted as finding)"
	default:
		return ""
	}
}

// RenderDismissedLGTMMarkdown renders LGTM markdown for when a user dismisses all findings.
func RenderDismissedLGTMMarkdown(findings []domain.FindingGroup, stats domain.ReviewStats, version string) string {
	var lines []string
	lines = append(lines, "## LGTM :white_check_mark:")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("**%d of %d reviewers completed review. All findings dismissed after human review.**",
		stats.SuccessfulReviewers, stats.TotalReviewers))
	lines = append(lines, "")

	count := len(findings)
	if count == 1 {
		lines = append(lines, "1 finding was reviewed and dismissed.")
	} else {
		lines = append(lines, fmt.Sprintf("%d findings were reviewed and dismissed.", count))
	}

	lines = append(lines, "")
	lines = append(lines, "<details>")
	lines = append(lines, "<summary>Dismissed findings</summary>")
	lines = append(lines, "")
	for _, f := range findings {
		title := f.Title
		if title == "" {
			title = "Untitled"
		}
		lines = append(lines, fmt.Sprintf("- **%s**", title))
	}
	lines = append(lines, "")
	lines = append(lines, "</details>")

	lines = append(lines, "")
	lines = append(lines, renderFooter(version))

	return strings.Join(lines, "\n")
}

// RenderJSON marshals the grouped findings as JSON with indentation.
// Ok is expected to be already computed before calling this function.
// If ccResult is non-nil and carries any signal — findings, Partial, or
// Skipped — a "cross_check" section is embedded alongside the grouped
// findings so that machine consumers can distinguish a clean cross-check
// run from a degraded one (partial agent failure or non-structural skip).
func RenderJSON(grouped *domain.GroupedFindings, ccResult *summarizer.CrossCheckResult) ([]byte, error) {
	if ccResult == nil {
		return json.MarshalIndent(grouped, "", "  ")
	}
	if len(ccResult.Findings) == 0 && !ccResult.Partial && !ccResult.Skipped {
		return json.MarshalIndent(grouped, "", "  ")
	}
	type crossCheckOut struct {
		Findings     []summarizer.CrossCheckFinding `json:"findings"`
		Skipped      bool                           `json:"skipped,omitempty"`
		Partial      bool                           `json:"partial,omitempty"`
		FailedAgents []string                       `json:"failed_agents,omitempty"`
		SkipReason   string                         `json:"skip_reason,omitempty"`
	}
	type wrapped struct {
		*domain.GroupedFindings
		CrossCheck crossCheckOut `json:"cross_check"`
	}
	return json.MarshalIndent(wrapped{
		GroupedFindings: grouped,
		CrossCheck: crossCheckOut{
			Findings:     ccResult.Findings,
			Skipped:      ccResult.Skipped,
			Partial:      ccResult.Partial,
			FailedAgents: ccResult.FailedAgents,
			SkipReason:   ccResult.SkipReason,
		},
	}, "", "  ")
}

// renderFooter returns a small attribution line for GitHub comments.
func renderFooter(version string) string {
	if version == "" {
		return "_Posted by [acr](https://github.com/richhaase/agentic-code-reviewer)_"
	}
	return fmt.Sprintf("_Posted by [acr](https://github.com/richhaase/agentic-code-reviewer) %s_", version)
}

func collectSourceIndices(groups []domain.FindingGroup) []int {
	seen := make(map[int]bool)
	var indices []int
	for _, g := range groups {
		for _, src := range g.Sources {
			if !seen[src] {
				seen[src] = true
				indices = append(indices, src)
			}
		}
	}
	return indices
}

func formatRawFindings(aggregated []domain.AggregatedFinding, indices []int, totalReviewers int) string {
	if len(indices) == 0 {
		return ""
	}

	var lines []string
	for idx, src := range indices {
		if src < 0 || src >= len(aggregated) {
			continue
		}
		entry := aggregated[src]
		reviewerCount := len(entry.Reviewers)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%d. (%d/%d reviewers)", idx+1, reviewerCount, totalReviewers))
		lines = append(lines, "```")
		lines = append(lines, strings.TrimRight(entry.Text, " \n"))
		lines = append(lines, "```")
	}

	return strings.Join(lines, "\n")
}

// formatReviewersWithAgents formats reviewer IDs with their agent names.
// Example: "#1 (codex), #3 (claude)"
func formatReviewersWithAgents(reviewerIDs []int, agentNames map[int]string) string {
	strs := make([]string, len(reviewerIDs))
	for i, id := range reviewerIDs {
		if name, ok := agentNames[id]; ok && name != "" {
			strs[i] = fmt.Sprintf("#%d (%s)", id, name)
		} else {
			strs[i] = fmt.Sprintf("#%d", id)
		}
	}
	return strings.Join(strs, ", ")
}
