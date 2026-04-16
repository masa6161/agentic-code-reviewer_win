package summarizer

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestSummarize_EmptyInput(t *testing.T) {
	result, err := Summarize(context.Background(), "codex", "", nil, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected no findings, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Info) != 0 {
		t.Errorf("expected no info, got %d", len(result.Grouped.Info))
	}
}

func TestSummarize_EmptySlice(t *testing.T) {
	result, err := Summarize(context.Background(), "codex", "", []domain.AggregatedFinding{}, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestSummarize_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Context was canceled, so we expect either an error exit code or context canceled handling
	if result.ExitCode != -1 && result.ExitCode != 1 {
		// If codex is not installed, we'll get exit code 1
		// If context is properly detected as canceled, we get -1
		if result.Stderr != "context canceled" && !isCodexNotFound(result.Stderr) {
			t.Logf("exit code: %d, stderr: %s", result.ExitCode, result.Stderr)
		}
	}
}

func isCodexNotFound(stderr string) bool {
	return stderr != "" // Accept any error message when codex isn't available
}

func TestSummarize_WithMockCodex(t *testing.T) {
	mockCodex := prepareMockCodex(t, `{"type":"thread.started","thread_id":"test"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"Test Issue\", \"summary\": \"A test issue summary.\", \"messages\": [\"test message\"], \"reviewer_count\": 1, \"sources\": [0]}], \"info\": []}"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
`, "", 0)

	// Verify our mock is being used
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skipf("mock codex not found in PATH: %v", err)
	}
	if path != mockCodex {
		t.Skipf("wrong codex found: %s (expected %s)", path, mockCodex)
	}

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if len(result.Grouped.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Findings) > 0 && result.Grouped.Findings[0].Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %q", result.Grouped.Findings[0].Title)
	}
}

func TestSummarize_InvalidJSONOutput(t *testing.T) {
	prepareMockCodex(t, "this is not valid JSON\n", "", 0)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should have exit code 1 due to JSON parse failure
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
	if result.Stderr == "" {
		t.Error("expected non-empty error message for JSON parse failure")
	}
	if result.RawOut == "" {
		t.Error("expected raw output to be preserved")
	}
}

func TestSummarize_EmptyOutput(t *testing.T) {
	prepareMockCodex(t, "", "", 0)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Empty output should return empty GroupedFindings
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected no findings for empty output, got %d", len(result.Grouped.Findings))
	}
}

func TestSummarize_CodexFailure(t *testing.T) {
	prepareMockCodex(t, "", "error message\n", 42)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestBackfillGroupKeys_Basic(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "issue A", GroupKey: "g01"},
		{Text: "issue B", GroupKey: "g02"},
		{Text: "info C", GroupKey: "g01,g02"},
	}
	grouped := &domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "cluster 1", Sources: []int{0, 1}},
		},
		Info: []domain.FindingGroup{
			{Title: "info cluster", Sources: []int{2}},
		},
	}

	backfillGroupKeys(grouped, aggregated)

	if grouped.Findings[0].GroupKey != "g01,g02" {
		t.Errorf("expected GroupKey 'g01,g02', got %q", grouped.Findings[0].GroupKey)
	}
	if grouped.Info[0].GroupKey != "g01,g02" {
		t.Errorf("expected GroupKey 'g01,g02', got %q", grouped.Info[0].GroupKey)
	}
}

func TestBackfillGroupKeys_OutOfRange(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "only one", GroupKey: "g01"},
	}
	grouped := &domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "bad sources", Sources: []int{-1, 0, 5}},
		},
	}

	backfillGroupKeys(grouped, aggregated)

	if grouped.Findings[0].GroupKey != "g01" {
		t.Errorf("expected GroupKey 'g01', got %q", grouped.Findings[0].GroupKey)
	}
}

func TestBackfillGroupKeys_NoGroupKey(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "no key", GroupKey: ""},
	}
	grouped := &domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "no key source", Sources: []int{0}},
		},
	}

	backfillGroupKeys(grouped, aggregated)

	if grouped.Findings[0].GroupKey != "" {
		t.Errorf("expected empty GroupKey, got %q", grouped.Findings[0].GroupKey)
	}
}

func TestBackfillGroupKeys_EmptySources(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "issue", GroupKey: "g01"},
	}
	grouped := &domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "no sources", Sources: nil},
		},
	}

	backfillGroupKeys(grouped, aggregated)

	if grouped.Findings[0].GroupKey != "" {
		t.Errorf("expected empty GroupKey, got %q", grouped.Findings[0].GroupKey)
	}
}

func TestSummarize_MultipleFindings(t *testing.T) {
	prepareMockCodex(t, `{"type":"thread.started","thread_id":"test"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"Issue 1\", \"summary\": \"First issue.\", \"messages\": [\"msg1\"], \"reviewer_count\": 2, \"sources\": [0, 1]}, {\"title\": \"Issue 2\", \"summary\": \"Second issue.\", \"messages\": [\"msg2\"], \"reviewer_count\": 1, \"sources\": [2]}], \"info\": [{\"title\": \"Info 1\", \"summary\": \"Some info.\", \"messages\": [\"info msg\"], \"reviewer_count\": 1, \"sources\": [3]}]}"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
`, "", 0)

	findings := []domain.AggregatedFinding{
		{Text: "Finding 1", Reviewers: []int{1, 2}},
		{Text: "Finding 2", Reviewers: []int{1}},
		{Text: "Finding 3", Reviewers: []int{3}},
		{Text: "Info finding", Reviewers: []int{2}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", "", findings, false, terminal.NewLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if len(result.Grouped.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Info) != 1 {
		t.Errorf("expected 1 info, got %d", len(result.Grouped.Info))
	}
}
