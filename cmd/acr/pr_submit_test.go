package main

import (
	"context"
	"strings"
	"testing"

	"github.com/masa6161/arc-cli/internal/domain"
	"github.com/masa6161/arc-cli/internal/github"
	"github.com/masa6161/arc-cli/internal/summarizer"
	"github.com/masa6161/arc-cli/internal/terminal"
)

func TestPrContext_Defaults(t *testing.T) {
	pc := prContext{}
	if pc.number != "" {
		t.Errorf("default number = %q, want empty", pc.number)
	}
	if pc.isSelfReview {
		t.Error("default isSelfReview = true, want false")
	}
	if pc.err != nil {
		t.Errorf("default err = %v, want nil", pc.err)
	}
}

func TestPrContext_WithAuthError(t *testing.T) {
	pc := prContext{err: github.ErrAuthFailed}
	if pc.err != github.ErrAuthFailed {
		t.Errorf("err = %v, want ErrAuthFailed", pc.err)
	}
}

func TestPrContext_WithNoPRError(t *testing.T) {
	pc := prContext{err: github.ErrNoPRFound}
	if pc.err != github.ErrNoPRFound {
		t.Errorf("err = %v, want ErrNoPRFound", pc.err)
	}
}

func TestPrependUserNote(t *testing.T) {
	body := "## Review\nSome findings here."

	t.Run("prepends note with separator", func(t *testing.T) {
		got := prependUserNote(body, "1 is low priority, 2 looks good")
		want := "**Reviewer's note:** 1 is low priority, 2 looks good\n\n---\n\n## Review\nSome findings here."
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("empty note still wraps body with prefix", func(t *testing.T) {
		// prependUserNote is only called with non-empty notes,
		// but verify the format is still valid
		got := prependUserNote(body, "")
		if got == body {
			t.Error("expected formatted output even with empty note")
		}
	})
}

func TestLgtmAction_Constants(t *testing.T) {
	if actionApprove == actionComment {
		t.Error("actionApprove should not equal actionComment")
	}
	if actionApprove == actionSkip {
		t.Error("actionApprove should not equal actionSkip")
	}
	if actionComment == actionSkip {
		t.Error("actionComment should not equal actionSkip")
	}
}

// --- reviewActionForVerdict tests ---

func TestReviewAction_BlockingVerdict_RequestsChanges(t *testing.T) {
	got := reviewActionForVerdict("blocking", false, nil)
	if got != "request_changes" {
		t.Errorf("blocking/non-strict: got %q, want %q", got, "request_changes")
	}
}

func TestReviewAction_AdvisoryVerdict_NoStrict_Comments(t *testing.T) {
	got := reviewActionForVerdict("advisory", false, nil)
	if got != "comment" {
		t.Errorf("advisory/non-strict: got %q, want %q", got, "comment")
	}
}

func TestReviewAction_AdvisoryVerdict_Strict_RequestsChanges(t *testing.T) {
	got := reviewActionForVerdict("advisory", true, nil)
	if got != "request_changes" {
		t.Errorf("advisory/strict: got %q, want %q", got, "request_changes")
	}
}

func TestReviewAction_UnknownVerdict_FallsBackToRequestChanges(t *testing.T) {
	// Unknown verdict must not panic with a nil logger and must fall back
	// to the safer "request_changes" default.
	got := reviewActionForVerdict("weird", false, nil)
	if got != "request_changes" {
		t.Errorf("unknown verdict: got %q, want %q", got, "request_changes")
	}
}

func TestReviewAction_EmptyVerdict_FallsBackToRequestChanges(t *testing.T) {
	got := reviewActionForVerdict("", false, nil)
	if got != "request_changes" {
		t.Errorf("empty verdict: got %q, want %q", got, "request_changes")
	}
}

// --- buildReviewPromptLabel tests ---

func TestBuildReviewPromptLabel_Comment_DefaultIsComment(t *testing.T) {
	got := buildReviewPromptLabel("comment")
	if !strings.Contains(got, "[C]omment (default)") {
		t.Errorf("comment default: got %q, want substring %q", got, "[C]omment (default)")
	}
	if strings.Contains(got, "[R]equest changes (default)") {
		t.Errorf("comment default: label must not mark request_changes as default; got %q", got)
	}
}

func TestBuildReviewPromptLabel_RequestChanges_DefaultIsRequestChanges(t *testing.T) {
	got := buildReviewPromptLabel("request_changes")
	if !strings.Contains(got, "[R]equest changes (default)") {
		t.Errorf("request_changes default: got %q, want substring %q", got, "[R]equest changes (default)")
	}
	if strings.Contains(got, "[C]omment (default)") {
		t.Errorf("request_changes default: label must not mark comment as default; got %q", got)
	}
}

// --- buildReviewBody edge-case tests ---

func TestBuildReviewBody_EmptyGroupedWithCC_UsesCCOnly(t *testing.T) {
	grouped := domain.GroupedFindings{}
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{Title: "CC only", Summary: "s", Type: "t", Severity: "advisory"},
		},
	}

	body := buildReviewBody(grouped, domain.ReviewStats{}, nil, cc, "test")

	if body == "" {
		t.Fatal("expected cross-check-only body, got empty string")
	}
	if strings.HasPrefix(body, "\n") {
		t.Errorf("cross-check-only body must not start with blank line; got %q", body)
	}
	if !strings.Contains(body, "Cross-Group Findings") {
		t.Errorf("missing cross-check header; got:\n%s", body)
	}
	if strings.Contains(body, "## Findings") {
		t.Errorf("empty grouped must not emit '## Findings' header; got:\n%s", body)
	}
}

func TestBuildReviewBody_EmptyBoth_ReturnsEmpty(t *testing.T) {
	grouped := domain.GroupedFindings{}
	got := buildReviewBody(grouped, domain.ReviewStats{}, nil, nil, "test")
	if got != "" {
		t.Errorf("expected empty body, got %q", got)
	}
}

func TestBuildReviewBody_GroupedOnly_NoCC_ReturnsFindingsBody(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Some Issue", Summary: "Details"},
		},
	}
	got := buildReviewBody(grouped, domain.ReviewStats{TotalReviewers: 1}, nil, nil, "test")
	if !strings.Contains(got, "## Findings") {
		t.Errorf("expected '## Findings' header; got:\n%s", got)
	}
	if strings.Contains(got, "Cross-Group Findings") {
		t.Errorf("nil cross-check must not produce cross-check section; got:\n%s", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("body must not have trailing double newline from empty cc section; got %q", got)
	}
}

// --- formatCrossCheckForPR tests ---

func TestFormatCrossCheckForPR_Empty(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		got := formatCrossCheckForPR(nil)
		if got != "" {
			t.Errorf("nil: got %q, want empty", got)
		}
	})
	t.Run("no findings and not partial returns empty", func(t *testing.T) {
		got := formatCrossCheckForPR(&summarizer.CrossCheckResult{})
		if got != "" {
			t.Errorf("empty non-partial: got %q, want empty", got)
		}
	})
}

func TestFormatCrossCheckForPR_Basic(t *testing.T) {
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{
				Title:          "Missing auth check",
				Summary:        "Auth is not validated across groups.",
				Type:           "security",
				Severity:       "blocking",
				InvolvedGroups: []string{"g01", "g02"},
			},
			{
				Title:          "Inconsistent error handling",
				Summary:        "Error codes differ between components.",
				Type:           "correctness",
				Severity:       "advisory",
				InvolvedGroups: []string{"g02", "g03"},
			},
		},
	}

	got := formatCrossCheckForPR(cc)

	if !strings.Contains(got, "## Cross-Group Findings (2)") {
		t.Errorf("missing header; got:\n%s", got)
	}
	if !strings.Contains(got, "**[security/blocking]** Missing auth check") {
		t.Errorf("missing finding 1; got:\n%s", got)
	}
	if !strings.Contains(got, "**[correctness/advisory]** Inconsistent error handling") {
		t.Errorf("missing finding 2; got:\n%s", got)
	}
	if !strings.Contains(got, "groups: [g01 g02]") {
		t.Errorf("missing groups for finding 1; got:\n%s", got)
	}
	if !strings.Contains(got, "groups: [g02 g03]") {
		t.Errorf("missing groups for finding 2; got:\n%s", got)
	}
	if !strings.Contains(got, "Auth is not validated across groups.") {
		t.Errorf("missing summary for finding 1; got:\n%s", got)
	}
}

func TestFormatCrossCheckForPR_Partial(t *testing.T) {
	cc := &summarizer.CrossCheckResult{
		Partial:      true,
		FailedAgents: []string{"codex"},
		Findings: []summarizer.CrossCheckFinding{
			{
				Title:          "Cross-boundary data leak",
				Summary:        "Data escapes group boundary.",
				Type:           "security",
				Severity:       "blocking",
				InvolvedGroups: []string{"g01", "g03"},
			},
			{
				Title:          "Duplicate logic",
				Summary:        "Same logic implemented twice.",
				Type:           "maintainability",
				Severity:       "advisory",
				InvolvedGroups: []string{"g02"},
			},
		},
	}

	got := formatCrossCheckForPR(cc)

	if !strings.Contains(got, "coverage reduced") {
		t.Errorf("missing coverage-reduced warning; got:\n%s", got)
	}
	if !strings.Contains(got, "failed agents: codex") {
		t.Errorf("missing failed agent name; got:\n%s", got)
	}
	// Warning should appear before the header
	warnIdx := strings.Index(got, "coverage reduced")
	headerIdx := strings.Index(got, "## Cross-Group Findings")
	if warnIdx > headerIdx {
		t.Errorf("warning should precede header; got:\n%s", got)
	}
}

// TestFormatCrossCheckForPR_EmitsSectionOnNonStructuralSkip verifies that a
// non-structural Skipped result (all agents failed, payload marshal failed,
// etc.) with no findings still produces a Markdown section so reviewers see
// the lost coverage. Structural skips (single group, no agents) stay silent.
func TestFormatCrossCheckForPR_EmitsSectionOnNonStructuralSkip(t *testing.T) {
	t.Run("all agents failed emits degraded advisory notice", func(t *testing.T) {
		cc := &summarizer.CrossCheckResult{
			Skipped:      true,
			SkipReason:   "all 3 agents failed: codex: timeout; claude: auth; gemini: parse",
			FailedAgents: []string{"codex", "claude", "gemini"},
		}
		got := formatCrossCheckForPR(cc)
		if got == "" {
			t.Fatal("expected non-empty Markdown for non-structural skip, got empty")
		}
		if !strings.Contains(got, "Cross-check degraded") {
			t.Errorf("missing degraded notice; got:\n%s", got)
		}
		if !strings.Contains(got, "treat as advisory") {
			t.Errorf("missing advisory wording; got:\n%s", got)
		}
		// Reason sanitizer must strip per-agent detail after the first colon.
		if !strings.Contains(got, "all 3 agents failed") {
			t.Errorf("missing sanitized agent-failure summary; got:\n%s", got)
		}
		if strings.Contains(got, "codex: timeout") || strings.Contains(got, "claude: auth") {
			t.Errorf("raw per-agent error detail must be sanitized out of PR body; got:\n%s", got)
		}
	})

	t.Run("structural single-group skip stays silent", func(t *testing.T) {
		cc := &summarizer.CrossCheckResult{
			Skipped:    true,
			SkipReason: summarizer.SkipReasonSingleGroup,
		}
		if got := formatCrossCheckForPR(cc); got != "" {
			t.Errorf("structural skip should stay silent, got:\n%s", got)
		}
	})

	t.Run("structural no-agents skip stays silent", func(t *testing.T) {
		cc := &summarizer.CrossCheckResult{
			Skipped:    true,
			SkipReason: summarizer.SkipReasonNoAgents,
		}
		if got := formatCrossCheckForPR(cc); got != "" {
			t.Errorf("no-agents is structural; expected silent, got:\n%s", got)
		}
	})

	t.Run("empty SkipReason stays silent (structural)", func(t *testing.T) {
		cc := &summarizer.CrossCheckResult{
			Skipped: true,
		}
		if got := formatCrossCheckForPR(cc); got != "" {
			t.Errorf("empty SkipReason is structural; expected silent, got:\n%s", got)
		}
	})
}

// --- buildReviewBody / PR body integration test ---

func TestPRBody_IncludesCrossCheckWhenGroupedEmpty(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: nil,
		Verdict:  "blocking",
	}
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{
				Title:          "Global state mutation",
				Summary:        "Shared state mutated without lock.",
				Type:           "concurrency",
				Severity:       "blocking",
				InvolvedGroups: []string{"g01", "g02"},
			},
		},
	}

	body := buildReviewBody(grouped, domain.ReviewStats{TotalReviewers: 2}, nil, cc, "test")

	if !strings.Contains(body, "Cross-Group Findings") {
		t.Errorf("PR body missing cross-check section; got:\n%s", body)
	}
	if !strings.Contains(body, "Global state mutation") {
		t.Errorf("PR body missing cross-check finding title; got:\n%s", body)
	}
}

// --- selector dismissed-all verdict recompute tests ---
//
// These mirror the in-place recompute inside handleFindings when the
// interactive selector leaves zero findings. We simulate the slice that
// handleFindings builds at that point (empty grouped + original cc signals)
// and assert the verdict gate determines dismissed-LGTM vs fall-through.

func TestHandleFindings_DismissedAll_CCBlocking_VerdictBlocking(t *testing.T) {
	grouped := domain.GroupedFindings{Findings: nil}
	// cc has a blocking finding → ccBlocking=true
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{Title: "cross blocker", Severity: "blocking"},
		},
	}
	ccBlocking := cc.HasBlockingFindings()
	ccAdvisory := !ccBlocking && (cc.HasAdvisoryFindings() || cc.IsDegraded())

	filtered := domain.GroupedFindings{Findings: nil, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "blocking" {
		t.Errorf("dismissed-all with cc-blocking must yield blocking verdict (no dismissed-LGTM), got %q", filtered.Verdict)
	}
}

func TestHandleFindings_DismissedAll_CCAdvisory_VerdictAdvisory(t *testing.T) {
	grouped := domain.GroupedFindings{Findings: nil}
	cc := &summarizer.CrossCheckResult{
		Findings: []summarizer.CrossCheckFinding{
			{Title: "cross nits", Severity: "advisory"},
		},
	}
	ccBlocking := cc.HasBlockingFindings()
	ccAdvisory := !ccBlocking && (cc.HasAdvisoryFindings() || cc.IsDegraded())

	filtered := domain.GroupedFindings{Findings: nil, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "advisory" {
		t.Errorf("dismissed-all with cc-advisory must yield advisory verdict, got %q", filtered.Verdict)
	}
}

func TestHandleFindings_DismissedAll_CCDegraded_VerdictAdvisory(t *testing.T) {
	// Regression for Round-4 §3: partial cross-check must suppress dismissed-LGTM.
	grouped := domain.GroupedFindings{Findings: nil}
	cc := &summarizer.CrossCheckResult{
		Partial:      true,
		FailedAgents: []string{"codex"},
	}
	ccBlocking := cc.HasBlockingFindings()
	ccAdvisory := !ccBlocking && (cc.HasAdvisoryFindings() || cc.IsDegraded())

	filtered := domain.GroupedFindings{Findings: nil, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "advisory" {
		t.Errorf("dismissed-all with degraded cc must force advisory (no dismissed-LGTM), got %q", filtered.Verdict)
	}
}

func TestHandleFindings_DismissedAll_CCClean_VerdictOk(t *testing.T) {
	grouped := domain.GroupedFindings{Findings: nil}
	// nil cc or a quiet cc → verdict must remain ok so dismissed-LGTM path runs.
	var cc *summarizer.CrossCheckResult
	ccBlocking := cc.HasBlockingFindings()
	ccAdvisory := !ccBlocking && (cc.HasAdvisoryFindings() || cc.IsDegraded())

	filtered := domain.GroupedFindings{Findings: nil, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "ok" {
		t.Errorf("dismissed-all with no cc must keep verdict=ok (dismissed-LGTM path), got %q", filtered.Verdict)
	}
}

func TestHandleFindings_Local_ReturnsVerdictUnchanged(t *testing.T) {
	// In local mode handleFindings skips the selector entirely and must return
	// the verdict it was passed (no recompute happens outside the selector path).
	opts := ReviewOpts{Local: true}
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{{Title: "x", Severity: "advisory"}},
		Verdict:  "advisory",
	}
	stats := domain.ReviewStats{TotalReviewers: 1}
	logger := terminal.NewLogger()
	code, finalVerdict := handleFindings(
		context.TODO(), opts, grouped, nil, nil,
		false, true, // ccBlocking, ccAdvisory
		"advisory", false, stats, logger,
	)
	if code != domain.ExitFindings {
		t.Errorf("local handleFindings expected ExitFindings, got %v", code)
	}
	if finalVerdict != "advisory" {
		t.Errorf("local handleFindings must preserve input verdict, got %q", finalVerdict)
	}
}

// --- §2: formatCrossCheckForPR degraded skip tests ---

func TestFormatCrossCheckForPR_DegradedSkip(t *testing.T) {
	cases := []struct {
		name       string
		cc         *summarizer.CrossCheckResult
		wantEmpty  bool
		wantSubstr []string // must all appear when !wantEmpty
		wantAbsent []string // must not appear when !wantEmpty
	}{
		{
			name: "structural skip single group -> empty",
			cc: &summarizer.CrossCheckResult{
				Skipped:    true,
				SkipReason: summarizer.SkipReasonSingleGroup,
			},
			wantEmpty: true,
		},
		{
			name: "structural skip no agents -> empty",
			cc: &summarizer.CrossCheckResult{
				Skipped:    true,
				SkipReason: summarizer.SkipReasonNoAgents,
			},
			wantEmpty: true,
		},
		{
			name: "all agents failed -> degraded notice",
			cc: &summarizer.CrossCheckResult{
				Skipped:      true,
				SkipReason:   "all 3 agents failed: codex: exec error; claude: timeout; gemini: auth failed",
				FailedAgents: []string{"codex", "claude", "gemini"},
			},
			wantEmpty:  false,
			wantSubstr: []string{"degraded", "coverage reduced", "all 3 agents failed"},
		},
		{
			name: "payload marshal failed -> degraded notice with generic message",
			cc: &summarizer.CrossCheckResult{
				Skipped:    true,
				SkipReason: "payload marshal failed: invalid type",
			},
			wantEmpty:  false,
			wantSubstr: []string{"degraded", "coverage reduced", "cross-check degraded (see local log for details)"},
		},
		{
			name: "all agents failed with details -> only summary line exposed",
			cc: &summarizer.CrossCheckResult{
				Skipped:      true,
				SkipReason:   "all 3 agents failed: codex: exec error; claude: timeout; gemini: auth failed",
				FailedAgents: []string{"codex", "claude", "gemini"},
			},
			wantEmpty:  false,
			wantSubstr: []string{"degraded", "coverage reduced", "all 3 agents failed"},
			wantAbsent: []string{"exec error", "timeout", "auth failed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatCrossCheckForPR(tc.cc)
			if tc.wantEmpty && got != "" {
				t.Errorf("expected empty, got %q", got)
			}
			if !tc.wantEmpty {
				if got == "" {
					t.Fatal("expected non-empty body")
				}
				for _, s := range tc.wantSubstr {
					if !strings.Contains(got, s) {
						t.Errorf("missing substring %q in:\n%s", s, got)
					}
				}
				for _, s := range tc.wantAbsent {
					if strings.Contains(got, s) {
						t.Errorf("unexpected substring %q in:\n%s", s, got)
					}
				}
			}
		})
	}
}

// --- §3: handleFindings partial dismiss verdict recompute tests ---
//
// These tests mirror the in-place recompute inside handleFindings when the
// interactive selector removes some (but not all) findings. We simulate the
// filtered slice that handleFindings builds and assert the verdict is correctly
// recomputed so applyVerdictExitPolicy receives the right signal.

// TestHandleFindings_PartialDismiss_BlockingRemoved_VerdictDowngrade verifies
// that removing the blocking finding while keeping an advisory one downgrades
// the verdict to "advisory". Without the §3 fix the input "blocking" verdict
// would persist, causing applyVerdictExitPolicy to request_changes / exit 1
// for what is effectively an advisory review.
func TestHandleFindings_PartialDismiss_BlockingRemoved_VerdictDowngrade(t *testing.T) {
	// Original grouped set: [blocking, advisory]
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "blocker", Severity: "blocking"},
			{Title: "nit", Severity: "advisory"},
		},
		Verdict: "blocking",
	}
	// Selector keeps only the advisory one (index 1).
	selected := []domain.FindingGroup{grouped.Findings[1]}
	// No cross-check signals.
	var ccBlocking, ccAdvisory bool

	filtered := domain.GroupedFindings{Findings: selected, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "advisory" {
		t.Errorf("partial dismiss keeping advisory must yield advisory verdict, got %q", filtered.Verdict)
	}
}

// TestHandleFindings_PartialDismiss_AdvisoryRemoved_VerdictBlocking verifies
// the reverse: keeping only the blocking finding must keep the verdict blocking.
func TestHandleFindings_PartialDismiss_AdvisoryRemoved_VerdictBlocking(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "blocker", Severity: "blocking"},
			{Title: "nit", Severity: "advisory"},
		},
		Verdict: "blocking",
	}
	selected := []domain.FindingGroup{grouped.Findings[0]}
	var ccBlocking, ccAdvisory bool

	filtered := domain.GroupedFindings{Findings: selected, Info: grouped.Info}
	filtered.ComputeVerdict(ccBlocking, ccAdvisory)

	if filtered.Verdict != "blocking" {
		t.Errorf("partial dismiss keeping blocking must yield blocking verdict, got %q", filtered.Verdict)
	}
}
