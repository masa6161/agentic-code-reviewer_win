package runner

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

func TestFormatDistributionFromSpecs_Empty(t *testing.T) {
	if got := FormatDistributionFromSpecs(nil); got != "" {
		t.Errorf("nil specs: expected empty string, got %q", got)
	}
	if got := FormatDistributionFromSpecs([]ReviewerSpec{}); got != "" {
		t.Errorf("empty specs: expected empty string, got %q", got)
	}
}

func TestFormatDistributionFromSpecs_SingleAgentType(t *testing.T) {
	a, err := agent.NewAgent("codex")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	specs := []ReviewerSpec{
		{Agent: a},
		{Agent: a},
		{Agent: a},
	}
	if got := FormatDistributionFromSpecs(specs); got != "" {
		t.Errorf("single agent type: expected empty string, got %q", got)
	}
}

func TestFormatDistributionFromSpecs_MultiAgentTypes(t *testing.T) {
	codex, err := agent.NewAgent("codex")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	claude, err := agent.NewAgent("claude")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	specs := []ReviewerSpec{
		{Agent: codex},
		{Agent: claude},
		{Agent: codex},
	}
	got := FormatDistributionFromSpecs(specs)
	expected := "1×claude, 2×codex"
	if got != expected {
		t.Errorf("multi agent: expected %q, got %q", expected, got)
	}
}

func TestFormatDistributionFromSpecs_ThreeAgentTypes(t *testing.T) {
	codex, err := agent.NewAgent("codex")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	claude, err := agent.NewAgent("claude")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	gemini, err := agent.NewAgent("gemini")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	specs := []ReviewerSpec{
		{Agent: codex},
		{Agent: claude},
		{Agent: gemini},
		{Agent: codex},
		{Agent: claude},
	}
	got := FormatDistributionFromSpecs(specs)
	expected := "2×claude, 2×codex, 1×gemini"
	if got != expected {
		t.Errorf("three agents: expected %q, got %q", expected, got)
	}
}

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
