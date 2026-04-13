package domain

import (
	"encoding/json"
	"testing"
)

func TestFinding_ZeroValueSeverity(t *testing.T) {
	f := Finding{Text: "some issue", ReviewerID: 1}
	if f.Severity != "" {
		t.Errorf("expected zero-value Severity to be empty string, got %q", f.Severity)
	}
}

func TestAggregateFindings_PreservesSeverity(t *testing.T) {
	findings := []Finding{
		{Text: "Issue A", ReviewerID: 1, Severity: "blocking"},
		{Text: "Issue B", ReviewerID: 2, Severity: "advisory"},
	}

	result := AggregateFindings(findings)

	if len(result) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result))
	}

	for _, af := range result {
		switch af.Text {
		case "Issue A":
			if af.Severity != "blocking" {
				t.Errorf("Issue A: expected Severity %q, got %q", "blocking", af.Severity)
			}
		case "Issue B":
			if af.Severity != "advisory" {
				t.Errorf("Issue B: expected Severity %q, got %q", "advisory", af.Severity)
			}
		default:
			t.Errorf("unexpected finding text: %q", af.Text)
		}
	}
}

func TestAggregateFindings_FirstSeenSeverityWins(t *testing.T) {
	findings := []Finding{
		{Text: "Issue", ReviewerID: 1, Severity: "blocking"},
		{Text: "Issue", ReviewerID: 2, Severity: "advisory"}, // duplicate — first-seen wins
	}

	result := AggregateFindings(findings)

	if len(result) != 1 {
		t.Fatalf("expected 1 aggregated finding, got %d", len(result))
	}
	if result[0].Severity != "blocking" {
		t.Errorf("expected first-seen Severity %q, got %q", "blocking", result[0].Severity)
	}
}

func TestGroupedFindings_ComputeOk_NoBlockingFindings(t *testing.T) {
	g := &GroupedFindings{}
	g.ComputeOk(nil)
	if !g.Ok {
		t.Error("expected Ok=true when findings list is nil")
	}
}

func TestGroupedFindings_ComputeOk_WithBlockingFinding(t *testing.T) {
	findings := []Finding{
		{Text: "advisory issue", ReviewerID: 1, Severity: "advisory"},
		{Text: "blocking issue", ReviewerID: 2, Severity: "blocking"},
	}
	g := &GroupedFindings{}
	g.ComputeOk(findings)
	if g.Ok {
		t.Error("expected Ok=false when a blocking finding exists")
	}
}

func TestGroupedFindings_ComputeOk_AdvisoryOnly(t *testing.T) {
	findings := []Finding{
		{Text: "advisory issue 1", ReviewerID: 1, Severity: "advisory"},
		{Text: "advisory issue 2", ReviewerID: 2, Severity: "advisory"},
	}
	g := &GroupedFindings{}
	g.ComputeOk(findings)
	if !g.Ok {
		t.Error("expected Ok=true with advisory-only findings")
	}
}

func TestGroupedFindings_JSONRoundTrip(t *testing.T) {
	original := GroupedFindings{
		Findings: []FindingGroup{
			{Title: "Critical Bug", Summary: "desc", ReviewerCount: 2, Sources: []int{0, 1}},
		},
		Info:               []FindingGroup{{Title: "Info note", Summary: "info"}},
		Ok:                 false,
		NotesForNextReview: "check auth logic next time",
		SkippedFiles:       []string{"vendor/lib.go", "generated.go"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var restored GroupedFindings
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if restored.Ok != original.Ok {
		t.Errorf("Ok: expected %v, got %v", original.Ok, restored.Ok)
	}
	if restored.NotesForNextReview != original.NotesForNextReview {
		t.Errorf("NotesForNextReview: expected %q, got %q", original.NotesForNextReview, restored.NotesForNextReview)
	}
	if len(restored.SkippedFiles) != len(original.SkippedFiles) {
		t.Fatalf("SkippedFiles length: expected %d, got %d", len(original.SkippedFiles), len(restored.SkippedFiles))
	}
	for i, f := range original.SkippedFiles {
		if restored.SkippedFiles[i] != f {
			t.Errorf("SkippedFiles[%d]: expected %q, got %q", i, f, restored.SkippedFiles[i])
		}
	}
	if len(restored.Findings) != 1 || restored.Findings[0].Title != "Critical Bug" {
		t.Errorf("Findings not preserved correctly after JSON round-trip")
	}
}
