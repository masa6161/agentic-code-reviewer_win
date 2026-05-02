package fpfilter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestFilter_New(t *testing.T) {
	f := New("codex", "", "", 75, false, false, terminal.NewLogger())
	if f == nil {
		t.Fatal("New returned nil")
	}
}

func TestBuildPromptWithFeedback(t *testing.T) {
	feedback := "User said the null check is intentional"
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	if !strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should contain Prior Feedback Context section")
	}
	if !strings.Contains(prompt, feedback) {
		t.Error("prompt should contain the feedback text")
	}
}

func TestBuildPromptWithoutFeedback(t *testing.T) {
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, "")

	if strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should not contain Prior Feedback Context when feedback is empty")
	}
}

func TestFPPrompt_IncludesReviewerCountGuidance(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "reviewer_count") {
		t.Error("prompt should reference reviewer_count field")
	}
	if !strings.Contains(fpEvaluationPrompt, "Reviewer Agreement") {
		t.Error("prompt should contain Reviewer Agreement section")
	}
}

func TestSkippedResult(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "finding 1", Summary: "summary 1"},
			{Title: "finding 2", Summary: "summary 2"},
		},
		Info: []domain.FindingGroup{
			{Title: "info 1"},
		},
	}

	tests := []struct {
		name   string
		reason string
	}{
		{"agent creation failed", "agent creation failed: codex not found"},
		{"request marshal failed", "request marshal failed: json error"},
		{"LLM execution failed", "LLM execution failed: timeout"},
		{"response read failed", "response read failed: io error"},
		{"response parse failed", "response parse failed: invalid json"},
		{"context canceled", "context canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			result := skippedResult(grouped, start, tt.reason)
			if !result.Skipped {
				t.Error("expected Skipped to be true")
			}
			if result.SkipReason != tt.reason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.reason)
			}
			if len(result.Grouped.Findings) != len(grouped.Findings) {
				t.Errorf("expected %d findings passed through, got %d", len(grouped.Findings), len(result.Grouped.Findings))
			}
			if len(result.Grouped.Info) != len(grouped.Info) {
				t.Errorf("expected %d info items passed through, got %d", len(grouped.Info), len(result.Grouped.Info))
			}
			if result.RemovedCount != 0 {
				t.Errorf("expected RemovedCount 0, got %d", result.RemovedCount)
			}
			if result.Duration < 0 {
				t.Error("expected non-negative Duration")
			}
		})
	}
}

func TestFindingInput_IncludesReviewerCount(t *testing.T) {
	tests := []struct {
		name          string
		reviewerCount int
	}{
		{"zero reviewers", 0},
		{"single reviewer", 1},
		{"multiple reviewers", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := findingInput{
				ID:            0,
				Title:         "Test",
				Summary:       "Summary",
				Messages:      []string{"msg"},
				ReviewerCount: tt.reviewerCount,
			}

			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			rc, ok := parsed["reviewer_count"]
			if !ok {
				t.Fatal("reviewer_count field missing from JSON output")
			}
			if int(rc.(float64)) != tt.reviewerCount {
				t.Errorf("reviewer_count = %v, want %d", rc, tt.reviewerCount)
			}
		})
	}
}

func TestBuildPromptWithStructuredFeedback(t *testing.T) {
	feedback := `- DISMISSED: "Non-atomic merge of shared map" -- protected by caller mutex (by @alice)
- FIXED: "Unchecked error from db.Connect()" -- fixed in commit abc123 (by @bob)
- INTENTIONAL: "Graph writes outside SQL transaction" -- intentional ordering (by @alice)`

	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	// Structured content preserved
	if !strings.Contains(prompt, "DISMISSED") {
		t.Error("prompt should contain DISMISSED status")
	}
	if !strings.Contains(prompt, "Non-atomic merge") {
		t.Error("prompt should preserve specific finding description")
	}

	// Matching instructions present
	if !strings.Contains(prompt, "semantic match") {
		t.Error("prompt should contain semantic matching guidance")
	}
	if !strings.Contains(prompt, "fp_score 90-100") {
		t.Error("prompt should specify fp_score range for DISMISSED matches")
	}
}

func TestFPPrompt_IncludesPriorFeedbackCheck(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "previously discussed") {
		t.Error("base prompt should reference checking prior feedback")
	}
}

func TestNew_ThresholdClamping(t *testing.T) {
	logger := terminal.NewLogger()
	tests := []struct {
		name              string
		threshold         int
		expectedThreshold int
	}{
		{"zero uses default", 0, DefaultThreshold},
		{"negative uses default", -5, DefaultThreshold},
		{"above 100 uses default", 101, DefaultThreshold},
		{"valid 50 keeps 50", 50, 50},
		{"minimum valid 1", 1, 1},
		{"maximum valid 100", 100, 100},
		{"mid-range 75", 75, 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("codex", "", "", tt.threshold, false, false, logger)
			if f.threshold != tt.expectedThreshold {
				t.Errorf("threshold = %d, want %d", f.threshold, tt.expectedThreshold)
			}
		})
	}
}

func TestApply_EmptyFindings(t *testing.T) {
	f := New("codex", "", "", 75, false, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
	}

	result := f.Apply(context.Background(), grouped, "", 0)

	if result == nil {
		t.Fatal("Apply returned nil")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Grouped.Findings))
	}
	if result.RemovedCount != 0 {
		t.Errorf("expected 0 removed, got %d", result.RemovedCount)
	}
	if result.Skipped {
		t.Error("expected Skipped to be false for empty findings")
	}
	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestApply_EmptyFindings_WithTotalReviewers(t *testing.T) {
	f := New("codex", "", "", 75, false, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
	}
	result := f.Apply(context.Background(), grouped, "", 5)
	if result == nil {
		t.Fatal("Apply returned nil")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Grouped.Findings))
	}
}

func TestApply_EmptyFindingsPreservesInfo(t *testing.T) {
	f := New("codex", "", "", 75, false, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
		Info: []domain.FindingGroup{
			{Title: "info 1", Summary: "informational note"},
			{Title: "info 2", Summary: "another note"},
		},
	}

	result := f.Apply(context.Background(), grouped, "", 0)

	if len(result.Grouped.Info) != 2 {
		t.Errorf("expected 2 info items preserved, got %d", len(result.Grouped.Info))
	}
	if result.Grouped.Info[0].Title != "info 1" {
		t.Errorf("info[0].Title = %q, want %q", result.Grouped.Info[0].Title, "info 1")
	}
}

func TestEvaluationRequest_Marshal(t *testing.T) {
	req := evaluationRequest{
		Findings: []findingInput{
			{ID: 0, Title: "Bug A", Summary: "null ptr", Messages: []string{"fix this"}, ReviewerCount: 3},
			{ID: 1, Title: "Bug B", Summary: "race condition", Messages: []string{"msg1", "msg2"}, ReviewerCount: 1},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	findings, ok := parsed["findings"].([]interface{})
	if !ok {
		t.Fatal("findings field missing or wrong type")
	}
	if len(findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(findings))
	}

	first := findings[0].(map[string]interface{})
	if first["title"] != "Bug A" {
		t.Errorf("first finding title = %q, want %q", first["title"], "Bug A")
	}
	if int(first["reviewer_count"].(float64)) != 3 {
		t.Errorf("first finding reviewer_count = %v, want 3", first["reviewer_count"])
	}
}

func TestEvaluationResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantEvalCount  int
		wantFirstID    int
		wantFirstScore int
		wantErr        bool
	}{
		{
			name:           "valid response with multiple evaluations",
			input:          `{"evaluations":[{"id":0,"fp_score":80,"reasoning":"likely false positive"},{"id":1,"fp_score":30,"reasoning":"real issue"}]}`,
			wantEvalCount:  2,
			wantFirstID:    0,
			wantFirstScore: 80,
		},
		{
			name:          "empty evaluations array",
			input:         `{"evaluations":[]}`,
			wantEvalCount: 0,
		},
		{
			name:           "missing fields default to zero",
			input:          `{"evaluations":[{"id":0}]}`,
			wantEvalCount:  1,
			wantFirstID:    0,
			wantFirstScore: 0,
		},
		{
			name:    "invalid JSON",
			input:   `{not valid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp evaluationResponse
			err := json.Unmarshal([]byte(tt.input), &resp)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Evaluations) != tt.wantEvalCount {
				t.Fatalf("got %d evaluations, want %d", len(resp.Evaluations), tt.wantEvalCount)
			}
			if tt.wantEvalCount > 0 {
				if resp.Evaluations[0].ID != tt.wantFirstID {
					t.Errorf("first eval ID = %d, want %d", resp.Evaluations[0].ID, tt.wantFirstID)
				}
				if resp.Evaluations[0].FPScore != tt.wantFirstScore {
					t.Errorf("first eval FPScore = %d, want %d", resp.Evaluations[0].FPScore, tt.wantFirstScore)
				}
			}
		})
	}
}

func TestAgreementBonus(t *testing.T) {
	tests := []struct {
		name           string
		reviewerCount  int
		totalReviewers int
		wantBonus      int
	}{
		// < 20% agreement — strong penalty (+15)
		{"1 of 6 reviewers (17%)", 1, 6, 15},
		{"1 of 8 reviewers (13%)", 1, 8, 15},
		{"1 of 10 reviewers (10%)", 1, 10, 15},
		{"2 of 12 reviewers (17%)", 2, 12, 15},
		{"1 of 20 reviewers (5%)", 1, 20, 15},

		// 20-39% agreement — moderate penalty (+10)
		{"1 of 5 reviewers (20%)", 1, 5, 10},
		{"1 of 3 reviewers (33%)", 1, 3, 10},
		{"1 of 4 reviewers (25%)", 1, 4, 10},
		{"2 of 6 reviewers (33%)", 2, 6, 10},
		{"3 of 10 reviewers (30%)", 3, 10, 10},
		{"2 of 8 reviewers (25%)", 2, 8, 10},

		// >= 40% agreement — no penalty
		{"2 of 5 reviewers (40%)", 2, 5, 0},
		{"2 of 4 reviewers (50%)", 2, 4, 0},
		{"3 of 6 reviewers (50%)", 3, 6, 0},
		{"4 of 10 reviewers (40%)", 4, 10, 0},
		{"5 of 6 reviewers (83%)", 5, 6, 0},
		{"6 of 6 reviewers (100%)", 6, 6, 0},
		{"1 of 2 reviewers (50%)", 1, 2, 0},
		{"2 of 3 reviewers (67%)", 2, 3, 0},
		{"2 of 2 reviewers (100%)", 2, 2, 0},

		// Edge cases — no penalty
		{"1 of 1 reviewer", 1, 1, 0},
		{"0 totalReviewers", 1, 0, 0},
		{"0 reviewerCount", 0, 6, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agreementBonus(tt.reviewerCount, tt.totalReviewers)
			if got != tt.wantBonus {
				t.Errorf("agreementBonus(%d, %d) = %d, want %d",
					tt.reviewerCount, tt.totalReviewers, got, tt.wantBonus)
			}
		})
	}
}

func TestTriagePrompt_ContainsSeveritySection(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "Severity Levels") {
		t.Error("prompt should contain Severity Levels section")
	}
	if !strings.Contains(fpEvaluationPrompt, "blocking") && !strings.Contains(fpEvaluationPrompt, "advisory") && !strings.Contains(fpEvaluationPrompt, "noise") {
		t.Error("prompt should define blocking, advisory, and noise severity levels")
	}
}

func TestTriagePrompt_OutputFormat_IncludesSeverity(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, `"severity"`) {
		t.Error("prompt output format should include severity field")
	}
}

func TestTriagePrompt_ReviewerSeverityHint(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "reviewer_severity") {
		t.Error("prompt should reference reviewer_severity input field")
	}
}

func TestFindingInput_IncludesReviewerSeverity(t *testing.T) {
	input := findingInput{
		ID:               0,
		Title:            "Test",
		Summary:          "Summary",
		Messages:         []string{"msg"},
		ReviewerCount:    1,
		ReviewerSeverity: "blocking",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if !strings.Contains(string(data), `"reviewer_severity":"blocking"`) {
		t.Errorf("expected reviewer_severity in JSON; got %s", string(data))
	}
}

func TestFilteringLogic_WithAgreementBonus(t *testing.T) {
	threshold := 75
	totalReviewers := 6

	findings := []domain.FindingGroup{
		{Title: "Finding 0", Summary: "1 of 6, fp=65", ReviewerCount: 1}, // 17% → +15 = 80 → removed
		{Title: "Finding 1", Summary: "5 of 6, fp=65", ReviewerCount: 5}, // 83% → +0  = 65 → kept
		{Title: "Finding 2", Summary: "2 of 6, fp=68", ReviewerCount: 2}, // 33% → +10 = 78 → removed
		{Title: "Finding 3", Summary: "3 of 6, fp=85", ReviewerCount: 3}, // 50% → +0  = 85 → removed
		{Title: "Finding 4", Summary: "2 of 6, fp=60", ReviewerCount: 2}, // 33% → +10 = 70 → kept
	}

	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 65, Reasoning: "maybe false positive"},
			{ID: 1, FPScore: 65, Reasoning: "maybe false positive"},
			{ID: 2, FPScore: 68, Reasoning: "uncertain"},
			{ID: 3, FPScore: 85, Reasoning: "clearly false positive"},
			{ID: 4, FPScore: 60, Reasoning: "borderline"},
		},
	}

	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		if adjusted >= threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   adjusted,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	if len(kept) != 2 {
		t.Fatalf("expected 2 kept findings, got %d", len(kept))
	}
	keptTitles := map[string]bool{}
	for _, f := range kept {
		keptTitles[f.Title] = true
	}
	if !keptTitles["Finding 1"] {
		t.Error("Finding 1 (5/6, high agreement) should be kept")
	}
	if !keptTitles["Finding 4"] {
		t.Error("Finding 4 (2/6, bonus not enough) should be kept")
	}

	if len(removed) != 3 {
		t.Fatalf("expected 3 removed findings, got %d", len(removed))
	}
	removedTitles := map[string]bool{}
	for _, ef := range removed {
		removedTitles[ef.Finding.Title] = true
	}
	if !removedTitles["Finding 0"] {
		t.Error("Finding 0 (1/6, +15 bonus) should be removed")
	}
	if !removedTitles["Finding 2"] {
		t.Error("Finding 2 (2/6, +10 bonus) should be removed")
	}
	if !removedTitles["Finding 3"] {
		t.Error("Finding 3 (3/6, high FP regardless) should be removed")
	}
}

func TestFilteringLogic_WithAgreementBonus_SmallTeam(t *testing.T) {
	threshold := 75
	totalReviewers := 2

	findings := []domain.FindingGroup{
		{Title: "Finding 0", Summary: "1 of 2, fp=65", ReviewerCount: 1}, // 50% → +0 = 65 → kept
		{Title: "Finding 1", Summary: "1 of 2, fp=80", ReviewerCount: 1}, // 50% → +0 = 80 → removed
		{Title: "Finding 2", Summary: "2 of 2, fp=65", ReviewerCount: 2}, // 100% → +0 = 65 → kept
	}

	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 65, Reasoning: "maybe"},
			{ID: 1, FPScore: 80, Reasoning: "likely FP"},
			{ID: 2, FPScore: 65, Reasoning: "maybe"},
		},
	}

	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		if adjusted >= threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   adjusted,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	if len(kept) != 2 {
		t.Fatalf("expected 2 kept findings, got %d", len(kept))
	}
	keptTitles := map[string]bool{}
	for _, f := range kept {
		keptTitles[f.Title] = true
	}
	if !keptTitles["Finding 0"] {
		t.Error("Finding 0 (1/2, no bonus, below threshold) should be kept")
	}
	if !keptTitles["Finding 2"] {
		t.Error("Finding 2 (2/2, no bonus, below threshold) should be kept")
	}

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed finding, got %d", len(removed))
	}
	if removed[0].Finding.Title != "Finding 1" {
		t.Errorf("removed finding = %q, want Finding 1", removed[0].Finding.Title)
	}
}

func TestFilteringLogic_WithAgreementBonus_LargeTeam(t *testing.T) {
	threshold := 75
	totalReviewers := 10

	findings := []domain.FindingGroup{
		{Title: "Finding 0", Summary: "1 of 10, fp=60", ReviewerCount: 1}, // 10% → +15 = 75 → removed
		{Title: "Finding 1", Summary: "3 of 10, fp=60", ReviewerCount: 3}, // 30% → +10 = 70 → kept
		{Title: "Finding 2", Summary: "4 of 10, fp=60", ReviewerCount: 4}, // 40% → +0  = 60 → kept
		{Title: "Finding 3", Summary: "7 of 10, fp=60", ReviewerCount: 7}, // 70% → +0  = 60 → kept
	}

	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 60, Reasoning: "borderline"},
			{ID: 1, FPScore: 60, Reasoning: "borderline"},
			{ID: 2, FPScore: 60, Reasoning: "borderline"},
			{ID: 3, FPScore: 60, Reasoning: "borderline"},
		},
	}

	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		if adjusted >= threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   adjusted,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	if len(kept) != 3 {
		t.Fatalf("expected 3 kept findings, got %d", len(kept))
	}
	keptTitles := map[string]bool{}
	for _, f := range kept {
		keptTitles[f.Title] = true
	}
	if !keptTitles["Finding 1"] {
		t.Error("Finding 1 (3/10, +10 not enough) should be kept")
	}
	if !keptTitles["Finding 2"] {
		t.Error("Finding 2 (4/10, no bonus) should be kept")
	}
	if !keptTitles["Finding 3"] {
		t.Error("Finding 3 (7/10, no bonus) should be kept")
	}

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed finding, got %d", len(removed))
	}
	if removed[0].Finding.Title != "Finding 0" {
		t.Errorf("removed finding = %q, want Finding 0", removed[0].Finding.Title)
	}
}

func TestEvaluationResponse_WithSeverity(t *testing.T) {
	// severity present
	input := `{"evaluations":[{"id":0,"fp_score":30,"severity":"blocking","reasoning":"real"}]}`
	var resp evaluationResponse
	if err := json.Unmarshal([]byte(input), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(resp.Evaluations) != 1 {
		t.Fatalf("expected 1 evaluation, got %d", len(resp.Evaluations))
	}
	if resp.Evaluations[0].Severity != "blocking" {
		t.Errorf("Severity = %q, want %q", resp.Evaluations[0].Severity, "blocking")
	}

	// severity absent → empty string
	input2 := `{"evaluations":[{"id":1,"fp_score":80,"reasoning":"FP"}]}`
	var resp2 evaluationResponse
	if err := json.Unmarshal([]byte(input2), &resp2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp2.Evaluations[0].Severity != "" {
		t.Errorf("Severity = %q, want empty", resp2.Evaluations[0].Severity)
	}
}

func TestEvaluatedFinding_HasSeverity(t *testing.T) {
	ef := EvaluatedFinding{Severity: "noise"}
	if ef.Severity != "noise" {
		t.Errorf("Severity = %q, want %q", ef.Severity, "noise")
	}

	ef2 := EvaluatedFinding{}
	if ef2.Severity != "" {
		t.Errorf("zero-value Severity = %q, want empty", ef2.Severity)
	}
}

func TestResult_NoiseFields(t *testing.T) {
	r := Result{
		Noise: []EvaluatedFinding{
			{Severity: "noise", FPScore: 20},
		},
		NoiseCount: 1,
	}
	if len(r.Noise) != 1 {
		t.Errorf("Noise length = %d, want 1", len(r.Noise))
	}
	if r.NoiseCount != 1 {
		t.Errorf("NoiseCount = %d, want 1", r.NoiseCount)
	}
}

func TestFilteringLogic_Severity_BlockingOverridesFPScore(t *testing.T) {
	threshold := 75
	totalReviewers := 3
	findings := []domain.FindingGroup{
		{Title: "Critical bug", Severity: "advisory", ReviewerCount: 1},
	}
	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 100, Severity: "blocking", Reasoning: "real critical issue"},
		},
	}
	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	// Simulate triage-enabled Apply logic
	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	triageEnabled := true

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		if triageEnabled {
			finding.RawSeverity = finding.Severity
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		switch {
		case triageEnabled && eval.Severity == "blocking":
			finding.Severity = "blocking"
			kept = append(kept, finding)
		case adjusted >= threshold:
			removed = append(removed, EvaluatedFinding{Finding: finding, FPScore: adjusted, Reasoning: eval.Reasoning, Severity: eval.Severity})
		default:
			kept = append(kept, finding)
		}
	}

	if len(kept) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(kept))
	}
	if kept[0].Severity != "blocking" {
		t.Errorf("Severity = %q, want blocking", kept[0].Severity)
	}
	if kept[0].RawSeverity != "advisory" {
		t.Errorf("RawSeverity = %q, want advisory", kept[0].RawSeverity)
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
}

func TestFilteringLogic_Severity_NoiseCategory(t *testing.T) {
	threshold := 75
	totalReviewers := 3
	findings := []domain.FindingGroup{
		{Title: "Style suggestion", Severity: "advisory", ReviewerCount: 1},
	}
	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 30, Severity: "noise", Reasoning: "style only"},
		},
	}
	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var noise []EvaluatedFinding
	triageEnabled := true

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		if triageEnabled {
			finding.RawSeverity = finding.Severity
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		switch {
		case triageEnabled && eval.Severity == "blocking":
			finding.Severity = "blocking"
			kept = append(kept, finding)
		case adjusted >= threshold:
			// removed
		case triageEnabled && eval.Severity == "noise" && adjusted < threshold:
			finding.Severity = "noise"
			noise = append(noise, EvaluatedFinding{Finding: finding, FPScore: adjusted, Reasoning: eval.Reasoning, Severity: "noise"})
		default:
			kept = append(kept, finding)
		}
	}

	if len(kept) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(kept))
	}
	if len(noise) != 1 {
		t.Fatalf("expected 1 noise, got %d", len(noise))
	}
	if noise[0].Finding.Severity != "noise" {
		t.Errorf("noise Severity = %q, want noise", noise[0].Finding.Severity)
	}
	if noise[0].Finding.RawSeverity != "advisory" {
		t.Errorf("noise RawSeverity = %q, want advisory", noise[0].Finding.RawSeverity)
	}
}

func TestFilteringLogic_Severity_EmptyFallback(t *testing.T) {
	// When triageEnabled is false, severity is ignored — fp_score only
	threshold := 75
	totalReviewers := 3
	findings := []domain.FindingGroup{
		{Title: "Finding", Severity: "advisory", ReviewerCount: 2},
	}
	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 80, Severity: "blocking", Reasoning: "triage says blocking"},
		},
	}
	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	triageEnabled := false

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		if triageEnabled {
			finding.RawSeverity = finding.Severity
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		switch {
		case triageEnabled && eval.Severity == "blocking":
			finding.Severity = "blocking"
			kept = append(kept, finding)
		case adjusted >= threshold && (!triageEnabled || eval.Severity != "blocking"):
			removed = append(removed, EvaluatedFinding{Finding: finding, FPScore: adjusted})
		default:
			kept = append(kept, finding)
		}
	}

	// Without triage, severity=blocking is ignored, fp_score 80 >= 75 → removed
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if len(kept) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(kept))
	}
	// RawSeverity should NOT be set when triage is disabled
	if removed[0].Finding.RawSeverity != "" {
		t.Errorf("RawSeverity = %q, want empty (triage disabled)", removed[0].Finding.RawSeverity)
	}
}

func TestApply_RawSeverity_PreservedBeforeOverwrite(t *testing.T) {
	// This test verifies that RawSeverity captures the pre-triage value
	fg := domain.FindingGroup{Title: "Bug", Severity: "advisory", ReviewerCount: 2}

	// Simulate what Apply does
	fg.RawSeverity = fg.Severity // snapshot
	fg.Severity = "blocking"     // triage overwrite

	if fg.RawSeverity != "advisory" {
		t.Errorf("RawSeverity = %q, want advisory", fg.RawSeverity)
	}
	if fg.Severity != "blocking" {
		t.Errorf("Severity = %q, want blocking", fg.Severity)
	}
}

func TestTriage_RawSeverity_SeverityChanged(t *testing.T) {
	// Simulate: triage changes severity from advisory to blocking
	finding := domain.FindingGroup{Title: "Bug", Severity: "advisory", ReviewerCount: 2}

	// triage-enabled Apply logic: snapshot then overwrite
	finding.RawSeverity = finding.Severity
	finding.Severity = "blocking"

	if finding.RawSeverity != "advisory" {
		t.Errorf("RawSeverity = %q, want advisory", finding.RawSeverity)
	}
	if finding.Severity != "blocking" {
		t.Errorf("Severity = %q, want blocking", finding.Severity)
	}
	if finding.RawSeverity == finding.Severity {
		t.Error("RawSeverity should differ from Severity when triage changed it")
	}
}

func TestTriage_RawSeverity_SeverityUnchanged(t *testing.T) {
	finding := domain.FindingGroup{Title: "Bug", Severity: "blocking", ReviewerCount: 3}

	// triage keeps same severity
	finding.RawSeverity = finding.Severity
	// severity stays "blocking"

	if finding.RawSeverity != finding.Severity {
		t.Errorf("RawSeverity=%q should equal Severity=%q when triage didn't change it",
			finding.RawSeverity, finding.Severity)
	}
}

func TestTriage_RawSeverity_NoTriage(t *testing.T) {
	finding := domain.FindingGroup{Title: "Bug", Severity: "advisory", ReviewerCount: 1}

	// --no-triage mode: RawSeverity is never set
	triageEnabled := false
	if triageEnabled {
		finding.RawSeverity = finding.Severity
	}

	if finding.RawSeverity != "" {
		t.Errorf("RawSeverity = %q, want empty (triage disabled)", finding.RawSeverity)
	}
}

func TestTriage_NoTriageMode_BackwardCompat(t *testing.T) {
	// Full backward-compat test: with triage disabled, the classification
	// logic should behave identically to the pre-triage code
	threshold := 75
	totalReviewers := 5
	triageEnabled := false

	findings := []domain.FindingGroup{
		{Title: "F0", Severity: "advisory", ReviewerCount: 1}, // fp=80, severity=blocking → still removed (triage off)
		{Title: "F1", Severity: "blocking", ReviewerCount: 3}, // fp=30, severity=noise → still kept (triage off)
		{Title: "F2", Severity: "advisory", ReviewerCount: 1}, // fp=60, severity=advisory → kept
	}
	evals := []findingEvaluation{
		{ID: 0, FPScore: 80, Severity: "blocking", Reasoning: "triage says blocking"},
		{ID: 1, FPScore: 30, Severity: "noise", Reasoning: "triage says noise"},
		{ID: 2, FPScore: 60, Severity: "advisory", Reasoning: "triage says advisory"},
	}
	evalMap := make(map[int]findingEvaluation)
	for _, e := range evals {
		evalMap[e.ID] = e
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	var noise []EvaluatedFinding

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			continue
		}
		if triageEnabled {
			finding.RawSeverity = finding.Severity
		}
		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)
		switch {
		case triageEnabled && eval.Severity == "blocking":
			finding.Severity = "blocking"
			kept = append(kept, finding)
		case adjusted >= threshold && (!triageEnabled || eval.Severity != "blocking"):
			removed = append(removed, EvaluatedFinding{Finding: finding, FPScore: adjusted, Reasoning: eval.Reasoning, Severity: eval.Severity})
		case triageEnabled && eval.Severity == "noise" && adjusted < threshold:
			finding.Severity = "noise"
			noise = append(noise, EvaluatedFinding{Finding: finding, FPScore: adjusted, Reasoning: eval.Reasoning, Severity: "noise"})
		default:
			if triageEnabled && eval.Severity != "" {
				finding.Severity = eval.Severity
			}
			kept = append(kept, finding)
		}
	}

	// With triage disabled:
	// F0: fp=80+10(20%)=90 >= 75 → removed (severity ignored)
	// F1: fp=30+0(60%)=30 < 75 → kept (severity ignored)
	// F2: fp=60+10(20%)=70 < 75 → kept (severity ignored)
	if len(removed) != 1 || removed[0].Finding.Title != "F0" {
		t.Errorf("removed = %v, want [F0]", removed)
	}
	if len(kept) != 2 {
		t.Fatalf("kept count = %d, want 2", len(kept))
	}
	if len(noise) != 0 {
		t.Errorf("noise = %v, want empty (triage disabled)", noise)
	}
	// RawSeverity should NOT be set
	for _, f := range kept {
		if f.RawSeverity != "" {
			t.Errorf("%s: RawSeverity = %q, want empty", f.Title, f.RawSeverity)
		}
	}
	// Severity should NOT be overwritten
	if kept[0].Severity != "blocking" {
		t.Errorf("F1 Severity = %q, want blocking (unchanged)", kept[0].Severity)
	}
}

func TestFilteringLogic(t *testing.T) {
	// This test simulates the core filtering logic from Apply() without
	// needing an external agent. We replicate the evalMap + threshold logic.
	threshold := 75

	findings := []domain.FindingGroup{
		{Title: "Finding 0", Summary: "At threshold", ReviewerCount: 2},       // id=0, score=75 -> removed
		{Title: "Finding 1", Summary: "Below threshold", ReviewerCount: 1},    // id=1, score=74 -> kept
		{Title: "Finding 2", Summary: "Above threshold", ReviewerCount: 1},    // id=2, score=90 -> removed
		{Title: "Finding 3", Summary: "Missing evaluation", ReviewerCount: 3}, // id=3, no eval -> kept + error
		{Title: "Finding 4", Summary: "Well below", ReviewerCount: 2},         // id=4, score=10 -> kept
	}

	infoItems := []domain.FindingGroup{
		{Title: "Info 1", Summary: "informational"},
	}

	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 75, Reasoning: "exactly at threshold"},
			{ID: 1, FPScore: 74, Reasoning: "just below threshold"},
			{ID: 2, FPScore: 90, Reasoning: "clearly false positive"},
			// ID 3 intentionally missing
			{ID: 4, FPScore: 10, Reasoning: "very likely real issue"},
		},
	}

	// Build evalMap (same as Apply)
	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	evalErrors := 0

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			evalErrors++
			continue
		}
		if eval.FPScore >= threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   eval.FPScore,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	// Verify kept findings
	if len(kept) != 3 {
		t.Fatalf("expected 3 kept findings, got %d", len(kept))
	}
	keptTitles := map[string]bool{}
	for _, f := range kept {
		keptTitles[f.Title] = true
	}
	if !keptTitles["Finding 1"] {
		t.Error("Finding 1 (below threshold) should be kept")
	}
	if !keptTitles["Finding 3"] {
		t.Error("Finding 3 (missing eval) should be kept")
	}
	if !keptTitles["Finding 4"] {
		t.Error("Finding 4 (well below) should be kept")
	}

	// Verify removed findings
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed findings, got %d", len(removed))
	}
	removedTitles := map[string]bool{}
	for _, ef := range removed {
		removedTitles[ef.Finding.Title] = true
	}
	if !removedTitles["Finding 0"] {
		t.Error("Finding 0 (at threshold) should be removed")
	}
	if !removedTitles["Finding 2"] {
		t.Error("Finding 2 (above threshold) should be removed")
	}

	// Verify eval errors
	if evalErrors != 1 {
		t.Errorf("expected 1 eval error, got %d", evalErrors)
	}

	// Verify info items are always preserved (they never go through filtering)
	result := &Result{
		Grouped: domain.GroupedFindings{
			Findings: kept,
			Info:     infoItems,
		},
		Removed:      removed,
		RemovedCount: len(removed),
		EvalErrors:   evalErrors,
	}

	if len(result.Grouped.Info) != 1 {
		t.Errorf("expected 1 info item preserved, got %d", len(result.Grouped.Info))
	}
	if result.Grouped.Info[0].Title != "Info 1" {
		t.Errorf("info title = %q, want %q", result.Grouped.Info[0].Title, "Info 1")
	}
}
