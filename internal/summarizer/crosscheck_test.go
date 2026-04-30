package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// TestCrossCheckResult_NilSafety locks the contract that all signal predicates
// on *CrossCheckResult accept a nil receiver. Round-4 review flagged that only
// IsDegraded had a visible nil guard; this table-driven test makes the contract
// explicit across HasBlockingFindings / HasAdvisoryFindings / IsDegraded so
// regressions are caught at unit-test time rather than production panic.
func TestCrossCheckResult_NilSafety(t *testing.T) {
	var r *CrossCheckResult

	cases := []struct {
		name string
		got  bool
	}{
		{"HasBlockingFindings", r.HasBlockingFindings()},
		{"HasAdvisoryFindings", r.HasAdvisoryFindings()},
		{"IsDegraded", r.IsDegraded()},
	}
	for _, tc := range cases {
		if tc.got {
			t.Errorf("%s on nil receiver = true, want false", tc.name)
		}
	}
}

// TestCrossCheckPayload_UsesAggregatedIDs verifies that when two raw findings
// with the same text are aggregated into one entry before building the payload,
// the payload contains exactly one finding with ID 0 — not two entries with IDs
// 0 and 1 as would happen if raw pre-aggregation findings were used.
func TestCrossCheckPayload_UsesAggregatedIDs(t *testing.T) {
	// Two raw findings with identical text (simulating two reviewers reporting the
	// same issue). After aggregation this collapses to one AggregatedFinding.
	raw := []domain.Finding{
		{Text: "nil pointer dereference", ReviewerID: 1, GroupKey: "g01"},
		{Text: "nil pointer dereference", ReviewerID: 2, GroupKey: "g02"},
	}
	aggregated := domain.AggregateFindings(raw)
	if len(aggregated) != 1 {
		t.Fatalf("expected aggregation to collapse to 1 finding, got %d", len(aggregated))
	}

	ccCtx := CrossCheckContext{
		Findings: aggregated,
		Groups: []GroupInfo{
			{GroupKey: "g01", Phase: domain.PhaseDiff},
			{GroupKey: "g02", Phase: domain.PhaseDiff},
		},
		Outcomes: []GroupOutcome{
			{GroupKey: "g01", Succeeded: true, FindingCount: 1},
			{GroupKey: "g02", Succeeded: true, FindingCount: 1},
		},
	}

	payload := buildCrossCheckPayload(ccCtx)

	if len(payload.Findings) != 1 {
		t.Fatalf("expected 1 finding in payload, got %d (pre-aggregation would give 2)", len(payload.Findings))
	}
	if payload.Findings[0].ID != 0 {
		t.Errorf("expected payload finding ID=0, got %d", payload.Findings[0].ID)
	}
	if payload.Findings[0].Text != "nil pointer dereference" {
		t.Errorf("unexpected text %q", payload.Findings[0].Text)
	}
}

func TestBuildCrossCheckPayload_TruncatesLongText(t *testing.T) {
	longText := strings.Repeat("A", 1000)
	ccCtx := CrossCheckContext{
		Findings: []domain.AggregatedFinding{
			{Text: longText, GroupKey: "g01", Severity: "blocking"},
		},
		Groups: []GroupInfo{
			{GroupKey: "g01", Phase: domain.PhaseDiff},
		},
		Outcomes: []GroupOutcome{
			{GroupKey: "g01", Succeeded: true, FindingCount: 1},
		},
	}
	payload := buildCrossCheckPayload(ccCtx)
	if len(payload.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(payload.Findings))
	}
	// truncation now uses rune count; ASCII text truncated to maxFindingTextLen runes
	// plus the "…" ellipsis = maxFindingTextLen+1 runes total.
	gotRunes := utf8.RuneCountInString(payload.Findings[0].Text)
	wantRunes := maxFindingTextLen + 1 // maxFindingTextLen runes + "…"
	if gotRunes != wantRunes {
		t.Errorf("expected truncation to %d runes (including ellipsis), got %d", wantRunes, gotRunes)
	}
}

func TestBuildCrossCheckPayload_OutcomesMergedByKey(t *testing.T) {
	ccCtx := CrossCheckContext{
		Groups: []GroupInfo{
			{GroupKey: domain.PhaseArch, Phase: domain.PhaseArch, FullDiff: true},
			{GroupKey: "g01", Phase: domain.PhaseDiff, TargetFiles: []string{"a.go"}},
		},
		Outcomes: []GroupOutcome{
			{GroupKey: domain.PhaseArch, Succeeded: true, FindingCount: 3},
			{GroupKey: "g01", Succeeded: false, TimedOut: true},
		},
	}
	payload := buildCrossCheckPayload(ccCtx)
	if len(payload.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(payload.Groups))
	}
	if !payload.Groups[0].Succeeded || payload.Groups[0].FindingCount != 3 {
		t.Errorf("arch outcome not merged: %+v", payload.Groups[0])
	}
	if !payload.Groups[1].TimedOut || payload.Groups[1].Succeeded {
		t.Errorf("g01 outcome not merged: %+v", payload.Groups[1])
	}
	if !payload.Groups[0].FullDiff || payload.Groups[1].FullDiff {
		t.Errorf("FullDiff flags wrong: %+v vs %+v", payload.Groups[0], payload.Groups[1])
	}
}

func TestCrossCheck_SkippedForSingleGroup(t *testing.T) {
	ccCtx := CrossCheckContext{
		Groups: []GroupInfo{{GroupKey: domain.PhaseArch}},
	}
	result := CrossCheck(context.Background(), []CrossCheckAgentSpec{{Name: "codex"}}, ccCtx, false, terminal.NewLogger())
	if !result.Skipped {
		t.Error("expected Skipped=true for single group")
	}
	if !strings.Contains(result.SkipReason, "single group") {
		t.Errorf("expected skip reason to mention 'single group', got %q", result.SkipReason)
	}
}

func TestCrossCheck_SkippedForNoAgents(t *testing.T) {
	ccCtx := CrossCheckContext{
		Groups: []GroupInfo{{GroupKey: domain.PhaseArch}, {GroupKey: "g01"}},
	}
	result := CrossCheck(context.Background(), nil, ccCtx, false, terminal.NewLogger())
	if !result.Skipped {
		t.Error("expected Skipped=true for no agents")
	}
}

func TestIsStructuralSkipReason(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   bool
	}{
		{"empty is structural", "", true},
		{"single group constant is structural", SkipReasonSingleGroup, true},
		{"single group literal is structural", "single group, cross-check unnecessary", true},
		{"no agents is structural (config choice, not runtime failure)", SkipReasonNoAgents, true},
		{"all agents failed is not structural", "all 3 agents failed: codex: timeout", false},
		{"payload marshal failed is not structural", "payload marshal failed: x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStructuralSkipReason(tc.reason); got != tc.want {
				t.Errorf("IsStructuralSkipReason(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}

func TestIsDegraded(t *testing.T) {
	cases := []struct {
		name string
		r    *CrossCheckResult
		want bool
	}{
		{"nil", nil, false},
		{"clean success", &CrossCheckResult{}, false},
		{"structural skip (single group)", &CrossCheckResult{Skipped: true, SkipReason: SkipReasonSingleGroup}, false},
		{"partial", &CrossCheckResult{Partial: true, FailedAgents: []string{"codex"}}, true},
		{"all agents failed (non-structural skip)", &CrossCheckResult{Skipped: true, SkipReason: "all 3 agents failed: codex: timeout"}, true},
		{"payload marshal failed", &CrossCheckResult{Skipped: true, SkipReason: "payload marshal failed: x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.IsDegraded(); got != tc.want {
				t.Errorf("IsDegraded() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasBlockingFindings(t *testing.T) {
	cases := []struct {
		name string
		in   *CrossCheckResult
		want bool
	}{
		{"nil", nil, false},
		{"empty", &CrossCheckResult{}, false},
		{"advisory only", &CrossCheckResult{Findings: []CrossCheckFinding{{Severity: "advisory"}}}, false},
		{"blocking", &CrossCheckResult{Findings: []CrossCheckFinding{{Severity: "advisory"}, {Severity: "blocking"}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.HasBlockingFindings(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDedupCrossCheckFindings_UnionMerge(t *testing.T) {
	in := []CrossCheckFinding{
		{Type: "gap", InvolvedGroups: []string{"g01"}, Severity: "advisory", RelatedIDs: []int{1, 2}},
		{Type: "gap", InvolvedGroups: []string{"g02"}, Severity: "blocking", RelatedIDs: []int{2, 1}}, // same key after sort
		{Type: "contradiction", InvolvedGroups: []string{"g01", "g02"}, Severity: "advisory", RelatedIDs: []int{3}},
	}
	out := dedupCrossCheckFindings(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 deduped findings, got %d", len(out))
	}
	// First one should be merged, severity upgraded to blocking
	if out[0].Severity != "blocking" {
		t.Errorf("expected severity upgrade to blocking, got %q", out[0].Severity)
	}
	// InvolvedGroups unioned
	groups := strings.Join(out[0].InvolvedGroups, ",")
	if groups != "g01,g02" {
		t.Errorf("expected unioned groups g01,g02; got %s", groups)
	}
}

func TestDedupCrossCheckFindings_EmptyKeyKeptSeparate(t *testing.T) {
	in := []CrossCheckFinding{
		{Title: "A"},
		{Title: "B"},
	}
	out := dedupCrossCheckFindings(in)
	if len(out) != 2 {
		t.Errorf("expected empty-key findings kept separate, got %d", len(out))
	}
}

func TestTruncateByRunes_ASCIIUnchangedWhenShort(t *testing.T) {
	s := "hello"
	got := truncateByRunes(s, 10)
	if got != s {
		t.Errorf("expected unchanged string %q, got %q", s, got)
	}
}

func TestTruncateByRunes_ASCIITruncated(t *testing.T) {
	s := strings.Repeat("a", 600)
	got := truncateByRunes(s, 500)
	// 500 runes + "…" = 501 runes total
	gotRunes := utf8.RuneCountInString(got)
	if gotRunes != 501 {
		t.Errorf("expected 501 runes (500 + ellipsis), got %d", gotRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected result to end with ellipsis, got %q", got[len(got)-5:])
	}
}

func TestTruncateByRunes_Multibyte(t *testing.T) {
	// 600 Japanese hiragana runes, each 3 bytes in UTF-8
	s := strings.Repeat("あ", 600)
	got := truncateByRunes(s, 500)
	if !utf8.ValidString(got) {
		t.Error("result is not valid UTF-8")
	}
	gotRunes := utf8.RuneCountInString(got)
	if gotRunes != 501 {
		t.Errorf("expected 501 runes (500 + ellipsis), got %d", gotRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected result to end with ellipsis")
	}
}

func TestTruncateByRunes_ZeroOrNegativeReturnsInput(t *testing.T) {
	s := strings.Repeat("x", 1000)
	if got := truncateByRunes(s, 0); got != s {
		t.Errorf("max=0: expected original string back, got length %d", len(got))
	}
	if got := truncateByRunes(s, -1); got != s {
		t.Errorf("max=-1: expected original string back, got length %d", len(got))
	}
}

func TestBuildCrossCheckPayload_TruncatesByRunes_NotBytes(t *testing.T) {
	// 600 Japanese runes; each is 3 bytes, so naive byte slice would corrupt UTF-8
	longJapanese := strings.Repeat("日", 600)
	ccCtx := CrossCheckContext{
		Findings: []domain.AggregatedFinding{
			{Text: longJapanese, GroupKey: "g01", Severity: "advisory"},
		},
		Groups: []GroupInfo{
			{GroupKey: "g01", Phase: domain.PhaseDiff},
		},
		Outcomes: []GroupOutcome{
			{GroupKey: "g01", Succeeded: true, FindingCount: 1},
		},
	}
	payload := buildCrossCheckPayload(ccCtx)
	if len(payload.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(payload.Findings))
	}
	text := payload.Findings[0].Text
	// Must produce valid UTF-8 (byte-slicing would not)
	if !utf8.ValidString(text) {
		t.Error("finding text is not valid UTF-8 after truncation")
	}
	// Marshal the whole payload to JSON; json.Valid confirms no broken sequences
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !json.Valid(b) {
		t.Error("marshaled payload is not valid JSON")
	}
}

// TestCrossCheck_PartialFailure_SetsPartialFlag verifies that when some (but not
// all) agents fail, CrossCheckResult has Partial=true, Skipped=false, the failed
// agent name in FailedAgents, and findings from the successful agent.
func TestCrossCheck_PartialFailure_SetsPartialFlag(t *testing.T) {
	successFindings := []CrossCheckFinding{
		{Title: "gap found", Type: "gap", Severity: "advisory", RelatedIDs: []int{0}},
	}
	agentResults := []AgentCrossCheckResult{
		{AgentName: "codex", Findings: successFindings, Err: nil},
		{AgentName: "claude", Err: fmt.Errorf("execute failed: some error")},
	}

	result := mergeAgentResults(agentResults, 2)

	if result.Skipped {
		t.Error("expected Skipped=false for partial failure")
	}
	if !result.Partial {
		t.Error("expected Partial=true when some agents failed")
	}
	if len(result.FailedAgents) != 1 || result.FailedAgents[0] != "claude" {
		t.Errorf("expected FailedAgents=[\"claude\"], got %v", result.FailedAgents)
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected 1 finding from successful agent, got %d", len(result.Findings))
	}
	if result.Findings[0].Title != "gap found" {
		t.Errorf("unexpected finding title: %q", result.Findings[0].Title)
	}
	if !strings.Contains(result.SkipReason, "1 of 2 agents failed") {
		t.Errorf("expected SkipReason to describe partial failure, got %q", result.SkipReason)
	}
}

// TestCrossCheck_AllFail_Skipped verifies that when all agents fail the result
// is Skipped=true with Partial=false.
func TestCrossCheck_AllFail_Skipped(t *testing.T) {
	agentResults := []AgentCrossCheckResult{
		{AgentName: "codex", Err: fmt.Errorf("timeout")},
		{AgentName: "claude", Err: fmt.Errorf("auth failed")},
	}

	result := mergeAgentResults(agentResults, 2)

	if !result.Skipped {
		t.Error("expected Skipped=true when all agents fail")
	}
	if result.Partial {
		t.Error("expected Partial=false when all agents fail")
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected no findings when all agents fail, got %d", len(result.Findings))
	}
	if len(result.FailedAgents) != 2 {
		t.Errorf("expected 2 failed agents, got %v", result.FailedAgents)
	}
	if !strings.Contains(result.SkipReason, "all 2 agents failed") {
		t.Errorf("expected SkipReason to mention all agents failed, got %q", result.SkipReason)
	}
}

// TestDedupCrossCheckFindings_PreservesDistinctTitles verifies that two
// findings with the same Type and RelatedIDs but different Title are NOT
// collapsed — they are kept as separate findings.
func TestDedupCrossCheckFindings_PreservesDistinctTitles(t *testing.T) {
	in := []CrossCheckFinding{
		{Type: "gap", Title: "Missing error handling in parser", Summary: "Parser does not handle EOF", RelatedIDs: []int{1}},
		{Type: "gap", Title: "Missing null check in formatter", Summary: "Formatter crashes on nil input", RelatedIDs: []int{1}},
	}
	out := dedupCrossCheckFindings(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 findings (distinct titles preserved), got %d: %+v", len(out), out)
	}
}

// TestDedupCrossCheckFindings_MergesSameTypeTitleRelated verifies that two
// findings with identical Type, RelatedIDs, and Title are merged into one,
// with InvolvedGroups unioned and Severity upgraded.
func TestDedupCrossCheckFindings_MergesSameTypeTitleRelated(t *testing.T) {
	in := []CrossCheckFinding{
		{Type: "gap", Title: "Missing error handling", InvolvedGroups: []string{"g01"}, Severity: "advisory", RelatedIDs: []int{1, 2}},
		{Type: "gap", Title: "Missing error handling", InvolvedGroups: []string{"g02"}, Severity: "blocking", RelatedIDs: []int{1, 2}},
	}
	out := dedupCrossCheckFindings(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(out))
	}
	if out[0].Severity != "blocking" {
		t.Errorf("expected severity upgraded to blocking, got %q", out[0].Severity)
	}
	if groups := strings.Join(out[0].InvolvedGroups, ","); groups != "g01,g02" {
		t.Errorf("expected InvolvedGroups=g01,g02, got %q", groups)
	}
}

// TestDedupCrossCheckFindings_SummaryUnion verifies that when two matching
// findings have distinct non-empty summaries, the merged summary joins them
// with "\n---\n" and the result is capped to 1000 runes.
func TestDedupCrossCheckFindings_SummaryUnion(t *testing.T) {
	sum1 := "First agent summary: potential nil pointer in handler"
	sum2 := "Second agent summary: nil dereference confirmed across groups"
	in := []CrossCheckFinding{
		{Type: "gap", Title: "Nil pointer issue", Summary: sum1, RelatedIDs: []int{1}},
		{Type: "gap", Title: "Nil pointer issue", Summary: sum2, RelatedIDs: []int{1}},
	}
	out := dedupCrossCheckFindings(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(out))
	}
	if !strings.Contains(out[0].Summary, "\n---\n") {
		t.Errorf("expected summaries joined with separator, got: %q", out[0].Summary)
	}
	if !strings.Contains(out[0].Summary, sum1) {
		t.Errorf("expected merged summary to contain first summary")
	}
	if !strings.Contains(out[0].Summary, sum2) {
		t.Errorf("expected merged summary to contain second summary")
	}

	// Verify rune cap: summaries totaling more than 1000 runes are truncated.
	longSum1 := strings.Repeat("A", 600)
	longSum2 := strings.Repeat("B", 600)
	inLong := []CrossCheckFinding{
		{Type: "gap", Title: "Long summary test", Summary: longSum1, RelatedIDs: []int{2}},
		{Type: "gap", Title: "Long summary test", Summary: longSum2, RelatedIDs: []int{2}},
	}
	outLong := dedupCrossCheckFindings(inLong)
	if len(outLong) != 1 {
		t.Fatalf("expected 1 merged finding for long summaries, got %d", len(outLong))
	}
	gotRunes := utf8.RuneCountInString(outLong[0].Summary)
	// truncateByRunes(_, 1000) gives at most 1001 runes (1000 + ellipsis)
	if gotRunes > 1001 {
		t.Errorf("expected merged summary capped at 1001 runes, got %d", gotRunes)
	}
}

// TestDedupCrossCheckFindings_TitleLengthPreference verifies that when two
// findings normalize to the same title key (e.g., whitespace differs only),
// the longer/more descriptive Title survives the merge.
func TestDedupCrossCheckFindings_TitleLengthPreference(t *testing.T) {
	// Shorter title first, then longer — exercises the replacement path.
	in := []CrossCheckFinding{
		{Type: "escalation", Title: "Race condition", RelatedIDs: []int{3}},
		{Type: "escalation", Title: "Race  condition", RelatedIDs: []int{3}}, // extra space → same normalized key but same rune count; use longer variant below
	}
	// Use a genuinely longer title that normalizes the same after whitespace collapse.
	in2 := []CrossCheckFinding{
		{Type: "escalation", Title: "null check", RelatedIDs: []int{4}},
		{Type: "escalation", Title: "null check across modules", RelatedIDs: []int{4}}, // same normalized prefix (80-rune truncation doesn't apply to short strings)
	}

	// For in2: "null check" normalizes to "null check" and "null check across modules"
	// normalizes to "null check across modules" — these are DIFFERENT keys, so they won't merge.
	// We need titles that share the same 80-rune normalized prefix.
	// Use titles that differ only in leading/trailing whitespace.
	in3 := []CrossCheckFinding{
		{Type: "gap", Title: "  missing auth check  ", Summary: "s", RelatedIDs: []int{5}},
		{Type: "gap", Title: "missing auth check", Summary: "s", RelatedIDs: []int{5}},
	}
	_ = in
	_ = in2

	out := dedupCrossCheckFindings(in3)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged finding for whitespace-only title diff, got %d: %+v", len(out), out)
	}
	// The longer raw title ("  missing auth check  ") has more runes than "missing auth check"
	// so it should win the length preference.
	if out[0].Title != "  missing auth check  " {
		t.Errorf("expected longer title to survive merge, got %q", out[0].Title)
	}
}

// TestCrossCheck_AllSucceed_NotPartial verifies that when all agents succeed
// Partial=false and FailedAgents is nil/empty.
func TestCrossCheck_AllSucceed_NotPartial(t *testing.T) {
	agentResults := []AgentCrossCheckResult{
		{AgentName: "codex", Findings: []CrossCheckFinding{
			{Title: "issue", Type: "gap", Severity: "advisory", RelatedIDs: []int{1}},
		}},
		{AgentName: "claude", Findings: []CrossCheckFinding{
			{Title: "issue2", Type: "contradiction", Severity: "blocking", RelatedIDs: []int{2}},
		}},
	}

	result := mergeAgentResults(agentResults, 2)

	if result.Skipped {
		t.Error("expected Skipped=false when all agents succeed")
	}
	if result.Partial {
		t.Error("expected Partial=false when all agents succeed")
	}
	if len(result.FailedAgents) != 0 {
		t.Errorf("expected FailedAgents to be empty, got %v", result.FailedAgents)
	}
	if len(result.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Findings))
	}
}
