package runner

import "testing"

func TestReviewerSpec_ZeroValue(t *testing.T) {
	var spec ReviewerSpec
	if spec.Agent != nil {
		t.Error("zero-value Agent should be nil")
	}
	if spec.Model != "" {
		t.Error("zero-value Model should be empty")
	}
	if spec.Phase != "" {
		t.Error("zero-value Phase should be empty")
	}
	if spec.Guidance != "" {
		t.Error("zero-value Guidance should be empty")
	}
	if spec.Diff != "" {
		t.Error("zero-value Diff should be empty")
	}
	if spec.TargetFiles != nil {
		t.Error("zero-value TargetFiles should be nil")
	}
}
