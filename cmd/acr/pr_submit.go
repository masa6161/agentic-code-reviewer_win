package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxDisplayedCIChecks = 5

// prContext holds PR number and self-review status for GitHub operations.
type prContext struct {
	number       string
	isSelfReview bool
	err          error // non-nil if PR lookup failed (distinguishes auth errors from "no PR")
}

// getPRContext retrieves PR number and self-review status for the current branch.
// If --pr flag was used, uses that PR number directly instead of looking it up.
func getPRContext(ctx context.Context, opts ReviewOpts) prContext {
	if opts.Local || !github.IsGHAvailable() {
		return prContext{}
	}

	// If --pr flag was used, we already have the PR number
	// This is important for detached worktrees where branch lookup would fail
	if opts.PRNumber != "" {
		return prContext{
			number:       opts.PRNumber,
			isSelfReview: github.IsSelfReview(ctx, opts.PRNumber),
		}
	}

	// Otherwise, look up PR from branch
	foundPR, err := github.GetCurrentPRNumber(ctx, opts.WorktreeBranch)
	if err != nil {
		return prContext{err: err}
	}
	return prContext{
		number:       foundPR,
		isSelfReview: github.IsSelfReview(ctx, foundPR),
	}
}

// checkPRAvailable verifies gh CLI is available and PR exists.
// Returns error if gh CLI unavailable or auth failed, true if PR exists, false if no PR found.
func checkPRAvailable(pr prContext, opts ReviewOpts, logger *terminal.Logger) (bool, error) {
	if err := github.CheckGHAvailable(); err != nil {
		return false, err
	}

	if pr.err != nil {
		if errors.Is(pr.err, github.ErrAuthFailed) {
			logger.Logf(terminal.StyleError, "GitHub authentication failed. Run 'gh auth login' to authenticate.")
			return false, pr.err
		}
		if errors.Is(pr.err, github.ErrNoPRFound) {
			branchDesc := "current branch"
			if opts.WorktreeBranch != "" {
				branchDesc = fmt.Sprintf("branch '%s'", opts.WorktreeBranch)
			}
			logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
			return false, nil
		}
		logger.Logf(terminal.StyleError, "Failed to check PR: %v", pr.err)
		return false, pr.err
	}

	if pr.number == "" {
		branchDesc := "current branch"
		if opts.WorktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", opts.WorktreeBranch)
		}
		logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
		return false, nil
	}
	return true, nil
}

// stdinReader is a shared buffered reader for all interactive stdin prompts.
// Using a single reader avoids data loss when multiple prompts read from stdin
// in the same session.
var stdinReader = bufio.NewReader(os.Stdin)

// readUserInput reads a line from stdin, returning empty string on error.
func readUserInput() string {
	response, err := stdinReader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(response)) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(response))
}

// promptOptionalMessage prompts for an optional reviewer note to prepend to the review.
func promptOptionalMessage() string {
	fmt.Print(formatPrompt("Add a note to the review?", "(press Enter to skip):"))
	msg, err := stdinReader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(msg)) == 0 {
		return ""
	}
	return strings.TrimSpace(msg)
}

// prependUserNote prepends a reviewer note to the review body.
func prependUserNote(body, note string) string {
	return fmt.Sprintf("**Reviewer's note:** %s\n\n---\n\n%s", note, body)
}

// formatPrompt creates a colored prompt string for user input.
func formatPrompt(question, options string) string {
	return fmt.Sprintf("%s?%s %s %s%s%s ",
		terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
		question,
		terminal.Color(terminal.Dim), options, terminal.Color(terminal.Reset))
}

// formatPRRef creates a bold PR reference like "#123".
func formatPRRef(prNumber string) string {
	return fmt.Sprintf("%s#%s%s", terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))
}

func handleLGTM(ctx context.Context, opts ReviewOpts, allFindings []domain.Finding, aggregated []domain.AggregatedFinding, dispositions map[int]domain.Disposition, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	// Build a text→aggregated index lookup for mapping raw findings to dispositions
	textToIndex := make(map[string]int, len(aggregated))
	for i, af := range aggregated {
		textToIndex[af.Text] = i
	}

	annotatedComments := make(map[int][]runner.AnnotatedComment)
	for _, f := range allFindings {
		if f.Text == "" {
			continue
		}
		ac := runner.AnnotatedComment{Text: f.Text}
		if idx, ok := textToIndex[f.Text]; ok {
			if d, ok := dispositions[idx]; ok {
				ac.Disposition = d
			}
		}
		annotatedComments[f.ReviewerID] = append(annotatedComments[f.ReviewerID], ac)
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, annotatedComments, version)
	pr := getPRContext(ctx, opts)

	if err := confirmAndSubmitLGTM(ctx, lgtmBody, pr, opts, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

// reviewActionForVerdict returns the GitHub review action ("request_changes" or "comment")
// based on verdict and strict flag. "ok" verdict should not reach this path (handleLGTM owns it).
//
//	verdict=="blocking"              → "request_changes"
//	verdict=="advisory" && strict    → "request_changes"
//	verdict=="advisory" && !strict   → "comment"
//
// Unknown or empty verdicts are logged as a warning and fall back to
// "request_changes" (the safer default for ambiguous review outcomes).
func reviewActionForVerdict(verdict string, strict bool, logger *terminal.Logger) string {
	switch verdict {
	case "blocking":
		return "request_changes"
	case "advisory":
		if strict {
			return "request_changes"
		}
		return "comment"
	default:
		if logger != nil {
			logger.Logf(terminal.StyleWarning, "Unknown verdict %q; defaulting to request_changes", verdict)
		}
		return "request_changes"
	}
}

// buildReviewPromptLabel returns the action selector label with the correct
// "(default)" marker based on the pre-computed default action. This keeps the
// interactive prompt honest when verdict=="advisory" and strict is off, where
// the default action is "comment" rather than "request_changes".
func buildReviewPromptLabel(defaultAction string) string {
	if defaultAction == "comment" {
		return "[R]equest changes / [C]omment (default) / [S]kip:"
	}
	return "[R]equest changes (default) / [C]omment / [S]kip:"
}

// formatCrossCheckForPR renders cross-check findings as a Markdown section for
// inclusion in a PR review body. Returns "" when cc is nil, or when cc has no
// findings and is neither Partial nor in a non-structural Skipped state —
// structural skips (single group, no agents configured) stay silent because
// they do not represent lost coverage.
func formatCrossCheckForPR(cc *summarizer.CrossCheckResult) string {
	if cc == nil {
		return ""
	}
	noFindings := len(cc.Findings) == 0
	if noFindings && !cc.Partial && !cc.Skipped {
		return ""
	}
	// Structural skips stay silent — cross-check was intentionally not run.
	if noFindings && cc.Skipped && summarizer.IsStructuralSkipReason(cc.SkipReason) {
		return ""
	}

	var sb strings.Builder

	if cc.Partial {
		agents := strings.Join(cc.FailedAgents, ", ")
		if agents == "" {
			agents = "unknown"
		}
		sb.WriteString(fmt.Sprintf("⚠ Cross-check ran partially (failed agents: %s) — coverage reduced\n\n", agents))
	}

	// Non-structural skip with no findings: emit a minimal section so reviewers
	// can see that cross-group coverage was lost (vs. silently dropping it).
	if noFindings && cc.Skipped {
		reason := cc.SkipReason
		if reason == "" {
			reason = "unknown reason"
		}
		sb.WriteString(fmt.Sprintf("⚠ Cross-check skipped: %s — cross-group coverage unavailable\n\n", reason))
	}

	if len(cc.Findings) > 0 {
		sb.WriteString(fmt.Sprintf("## Cross-Group Findings (%d)\n", len(cc.Findings)))
		for i, f := range cc.Findings {
			typ := f.Type
			if typ == "" {
				typ = "-"
			}
			sev := f.Severity
			if sev == "" {
				sev = "-"
			}
			title := f.Title
			if title == "" {
				title = "Untitled"
			}
			sb.WriteString(fmt.Sprintf("\n%d. **[%s/%s]** %s\n", i+1, typ, sev, title))
			if f.Summary != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", f.Summary))
			}
			if len(f.InvolvedGroups) > 0 {
				sb.WriteString(fmt.Sprintf("   groups: [%s]\n", strings.Join(f.InvolvedGroups, " ")))
			}
		}
	}

	return sb.String()
}

// buildReviewBody composes the full PR review body from grouped findings and
// optional cross-check results. RenderCommentMarkdown returns "" when there
// are no grouped findings, so this function handles the cross-check-only
// path cleanly without leaving stray leading/trailing blank sections.
func buildReviewBody(grouped domain.GroupedFindings, totalReviewers int, aggregated []domain.AggregatedFinding, cc *summarizer.CrossCheckResult, ver string) string {
	body := runner.RenderCommentMarkdown(grouped, totalReviewers, aggregated, ver)
	ccSection := formatCrossCheckForPR(cc)
	switch {
	case body == "" && ccSection == "":
		return ""
	case body == "":
		return ccSection
	case ccSection == "":
		return body
	default:
		return body + "\n\n" + ccSection
	}
}

// handleFindings drives the PR submission path after grouped findings and
// cross-check results have been finalized. It returns the final exit code and
// the final verdict — which may differ from the input verdict when the
// interactive selector dismisses all findings, triggering a re-computation
// against the remaining cross-check signals.
func handleFindings(ctx context.Context, opts ReviewOpts, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, ccResult *summarizer.CrossCheckResult, ccBlocking, ccAdvisory bool, verdict string, strict bool, stats domain.ReviewStats, logger *terminal.Logger) (domain.ExitCode, string) {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting (skip in local mode).
	// Only show selector when there are grouped findings to choose from.
	if !opts.Local && !opts.AutoYes && terminal.IsStdoutTTY() && len(grouped.Findings) > 0 {
		indices, canceled, err := terminal.RunSelector(grouped.Findings)
		if err != nil {
			logger.Logf(terminal.StyleError, "Selector error: %v", err)
			return domain.ExitError, verdict
		}
		if canceled {
			logger.Log("Skipped posting findings.", terminal.StyleDim)
			return domain.ExitFindings, verdict
		}
		selectedFindings = filterFindingsByIndices(grouped.Findings, indices)

		if len(selectedFindings) == 0 {
			logger.Log("No findings selected to post.", terminal.StyleDim)

			// Recompute verdict against the empty grouped slice plus the
			// original cross-check signals. When cross-check has its own
			// concerns (or is degraded), filtered.Verdict will not be "ok",
			// so we fall through to confirmAndSubmitReview with the CC-only
			// body and the updated verdict drives the review action. Only a
			// genuinely clean recompute may emit the dismissed-LGTM banner.
			filtered := domain.GroupedFindings{Findings: nil, Info: grouped.Info}
			filtered.ComputeVerdict(ccBlocking, ccAdvisory)
			verdict = filtered.Verdict

			if verdict == "ok" {
				lgtmBody := runner.RenderDismissedLGTMMarkdown(grouped.Findings, stats, version)
				pr := getPRContext(ctx, opts)
				// Best-effort: LGTM posting is optional when dismissing findings.
				// Auth/network errors should not fail the run.
				_ = confirmAndSubmitLGTM(ctx, lgtmBody, pr, opts, logger)
				return domain.ExitNoFindings, verdict
			}
		}
	}

	pr := getPRContext(ctx, opts)

	filteredGrouped := domain.GroupedFindings{
		Findings: selectedFindings,
		Info:     grouped.Info,
	}
	reviewBody := buildReviewBody(filteredGrouped, stats.TotalReviewers, aggregated, ccResult, version)

	if err := confirmAndSubmitReview(ctx, reviewBody, pr, verdict, strict, opts, logger); err != nil {
		return domain.ExitError, verdict
	}

	return domain.ExitFindings, verdict
}

func confirmAndSubmitReview(ctx context.Context, body string, pr prContext, verdict string, strict bool, opts ReviewOpts, logger *terminal.Logger) error {
	if opts.Local {
		logger.Log("Local mode enabled; skipping PR review.", terminal.StyleDim)
		return nil
	}

	// Safety guard: nothing to post (no grouped findings and no cross-check
	// section). This happens when selector dismissed everything and cross-check
	// also had nothing to say, but verdict was non-ok for some other reason.
	if strings.TrimSpace(body) == "" {
		logger.Log("No findings or cross-check content; skipping PR review.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, opts, logger)
	if err != nil {
		return err
	}
	if !available {
		return nil
	}

	// Determine the default action from verdict + strict.
	// Self-review always falls back to comment (cannot request changes on own PR).
	action := reviewActionForVerdict(verdict, strict, logger)
	if pr.isSelfReview {
		action = "comment"
	}

	if !opts.AutoYes {
		// Check if stdin is a TTY before prompting to avoid hanging in CI
		if !terminal.IsStdinTTY() {
			logger.Log("Non-interactive mode without --yes flag; skipping PR review.", terminal.StyleDim)
			return nil
		}

		fmt.Println()
		prRef := formatPRRef(pr.number)
		if pr.isSelfReview {
			fmt.Print(formatPrompt(
				"You cannot request changes on your own PR. Post review to PR "+prRef+"?",
				"[C]omment (default) / [S]kip:"))
		} else {
			fmt.Print(formatPrompt(
				"Post review to PR "+prRef+"?",
				buildReviewPromptLabel(action)))
		}

		response := readUserInput()

		if pr.isSelfReview {
			switch response {
			case "s", "n", "no":
				logger.Log("Skipped posting review.", terminal.StyleDim)
				return nil
			default:
				action = "comment"
			}
		} else {
			switch response {
			case "r":
				action = "request_changes"
			case "c":
				action = "comment"
			case "s", "n", "no":
				logger.Log("Skipped posting review.", terminal.StyleDim)
				return nil
			default:
				// keep verdict-derived action (request_changes for blocking/strict-advisory, comment for advisory)
			}
		}
	}

	if !opts.AutoYes && terminal.IsStdinTTY() {
		if note := promptOptionalMessage(); note != "" {
			body = prependUserNote(body, note)
		}
	}

	requestChanges := action == "request_changes"
	if err := github.SubmitPRReview(ctx, pr.number, body, requestChanges); err != nil {
		logger.Logf(terminal.StyleError, "Failed: %v", err)
		return err
	}

	reviewType := "request changes"
	if !requestChanges {
		reviewType = "comment"
	}
	logger.Logf(terminal.StyleSuccess, "Posted %s review to PR #%s.", reviewType, pr.number)
	return nil
}

// lgtmAction represents the action to take for an LGTM review.
type lgtmAction int

const (
	actionApprove lgtmAction = iota
	actionComment
	actionSkip
)

func confirmAndSubmitLGTM(ctx context.Context, body string, pr prContext, opts ReviewOpts, logger *terminal.Logger) error {
	if opts.Local {
		logger.Log("Local mode enabled; skipping PR approval.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, opts, logger)
	if err != nil {
		return err
	}
	if !available {
		return nil
	}

	action := actionApprove
	if pr.isSelfReview {
		action = actionComment
	}

	if !opts.AutoYes {
		if !terminal.IsStdinTTY() {
			logger.Log("Non-interactive mode without --yes flag; skipping LGTM.", terminal.StyleDim)
			return nil
		}

		action = promptLGTMAction(pr)
		if action == actionSkip {
			logger.Log("Skipped posting LGTM.", terminal.StyleDim)
			return nil
		}
	}

	// Check CI status before approving
	if action == actionApprove {
		var err error
		action, err = checkCIAndMaybeDowngrade(ctx, pr.number, action, opts, logger)
		if err != nil {
			return err
		}
		if action == actionSkip {
			return nil
		}
	}

	if !opts.AutoYes && terminal.IsStdinTTY() {
		if note := promptOptionalMessage(); note != "" {
			body = prependUserNote(body, note)
		}
	}

	return executeLGTMAction(ctx, action, pr.number, body, logger)
}

// promptLGTMAction prompts the user for LGTM action choice.
func promptLGTMAction(pr prContext) lgtmAction {
	fmt.Println()
	prRef := formatPRRef(pr.number)

	if pr.isSelfReview {
		fmt.Print(formatPrompt(
			"You cannot approve your own PR. Post LGTM review to PR "+prRef+"?",
			"[C]omment (default) / [S]kip:"))
	} else {
		fmt.Print(formatPrompt(
			"Post LGTM to PR "+prRef+"?",
			"[A]pprove (default) / [C]omment / [S]kip:"))
	}

	response := readUserInput()

	if pr.isSelfReview {
		if response == "s" || response == "n" || response == "no" {
			return actionSkip
		}
		return actionComment
	}

	switch response {
	case "c":
		return actionComment
	case "s", "n", "no":
		return actionSkip
	default:
		return actionApprove
	}
}

// logCIChecks logs a list of CI checks with truncation.
func logCIChecks(logger *terminal.Logger, checks []string) {
	for i, check := range checks {
		if i >= maxDisplayedCIChecks {
			logger.Logf(terminal.StyleDim, "  ... and %d more", len(checks)-maxDisplayedCIChecks)
			break
		}
		logger.Logf(terminal.StyleDim, "  * %s", check)
	}
}

// checkCIAndMaybeDowngrade checks CI status and downgrades to comment if CI is not green.
// Returns error if CI status check fails (network/auth issues).
func checkCIAndMaybeDowngrade(ctx context.Context, prNum string, action lgtmAction, opts ReviewOpts, logger *terminal.Logger) (lgtmAction, error) {
	ciStatus := github.CheckCIStatus(ctx, prNum)

	if ciStatus.Error != "" {
		logger.Logf(terminal.StyleError, "Failed to check CI status: %s", ciStatus.Error)
		return actionSkip, fmt.Errorf("CI check failed: %s", ciStatus.Error)
	}

	if ciStatus.AllPassed {
		return action, nil
	}

	if len(ciStatus.Failed) > 0 {
		logger.Logf(terminal.StyleError, "Cannot approve PR: %d CI check(s) failed", len(ciStatus.Failed))
		logCIChecks(logger, ciStatus.Failed)
	}
	if len(ciStatus.Pending) > 0 {
		logger.Logf(terminal.StyleWarning, "Cannot approve PR: %d CI check(s) pending", len(ciStatus.Pending))
		logCIChecks(logger, ciStatus.Pending)
	}

	if opts.AutoYes {
		logger.Log("CI not green; posting as comment instead of approval.", terminal.StyleDim)
		return actionComment, nil
	}

	if !terminal.IsStdinTTY() {
		logger.Log("CI not green and non-interactive; skipping LGTM.", terminal.StyleDim)
		return actionSkip, nil
	}

	fmt.Print(formatPrompt("Post as comment instead?", "[C]omment (default) / [S]kip:"))
	response := readUserInput()

	switch response {
	case "", "c", "y", "yes":
		return actionComment, nil
	default:
		logger.Log("Skipped posting LGTM.", terminal.StyleDim)
		return actionSkip, nil
	}
}

// executeLGTMAction executes the chosen LGTM action.
func executeLGTMAction(ctx context.Context, action lgtmAction, prNumber, body string, logger *terminal.Logger) error {
	switch action {
	case actionApprove:
		if err := github.ApprovePR(ctx, prNumber, body); err != nil {
			logger.Logf(terminal.StyleError, "Failed: %v", err)
			return err
		}
		logger.Logf(terminal.StyleSuccess, "Approved PR #%s.", prNumber)
	case actionComment:
		if err := github.SubmitPRReview(ctx, prNumber, body, false); err != nil {
			logger.Logf(terminal.StyleError, "Failed: %v", err)
			return err
		}
		logger.Logf(terminal.StyleSuccess, "Posted LGTM review to PR #%s.", prNumber)
	}
	return nil
}
