package runner

import (
	"context"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

// mockPhaseAgent implements agent.Agent for phase testing.
type mockPhaseAgent struct {
	name string
}

func (m *mockPhaseAgent) Name() string { return m.name }
func (m *mockPhaseAgent) IsAvailable() error { return nil }
func (m *mockPhaseAgent) ExecuteReview(_ context.Context, _ *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	return nil, nil
}
func (m *mockPhaseAgent) ExecuteSummary(_ context.Context, _ string, _ []byte) (*agent.ExecutionResult, error) {
	return nil, nil
}

func TestBuildReviewerSpecs_ArchAndDiff(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	phases := []PhaseConfig{
		{Phase: "arch", ReviewerCount: 1},
		{Phase: "diff", ReviewerCount: 2},
	}

	specs, err := BuildReviewerSpecs(phases, agents, "global guidance", "diff content", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs, want 3", len(specs))
	}

	// First spec should be arch phase
	if specs[0].Phase != "arch" {
		t.Errorf("specs[0].Phase = %q, want %q", specs[0].Phase, "arch")
	}
	// Arch phase guidance should be globalGuidance (prompt selection happens in execution path)
	if specs[0].Guidance != "global guidance" {
		t.Errorf("specs[0].Guidance = %q, want %q", specs[0].Guidance, "global guidance")
	}

	// Remaining specs should be diff phase
	if specs[1].Phase != "diff" {
		t.Errorf("specs[1].Phase = %q, want %q", specs[1].Phase, "diff")
	}
	if specs[2].Phase != "diff" {
		t.Errorf("specs[2].Phase = %q, want %q", specs[2].Phase, "diff")
	}

	// All specs should have DiffPrecomputed set
	for i, s := range specs {
		if !s.DiffPrecomputed {
			t.Errorf("specs[%d].DiffPrecomputed = false, want true", i)
		}
		if s.Diff != "diff content" {
			t.Errorf("specs[%d].Diff = %q, want %q", i, s.Diff, "diff content")
		}
	}
}

func TestBuildReviewerSpecs_DefaultPrompt(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	phases := []PhaseConfig{
		{Phase: "arch", ReviewerCount: 1},
	}

	specs, err := BuildReviewerSpecs(phases, agents, "fallback", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Arch phase should use globalGuidance (prompt template selection happens in execution path)
	if specs[0].Guidance != "fallback" {
		t.Errorf("expected globalGuidance %q for arch phase, got %q", "fallback", specs[0].Guidance)
	}
}

func TestBuildReviewerSpecs_CustomPrompt(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	phases := []PhaseConfig{
		{Phase: "arch", ReviewerCount: 1, Prompt: "custom arch prompt"},
	}

	specs, err := BuildReviewerSpecs(phases, agents, "fallback", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if specs[0].Guidance != "custom arch prompt" {
		t.Errorf("expected custom prompt, got %q", specs[0].Guidance)
	}
}

func TestBuildReviewerSpecs_DiffPhaseUsesGlobalGuidance(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	phases := []PhaseConfig{
		{Phase: "diff", ReviewerCount: 1},
	}

	specs, err := BuildReviewerSpecs(phases, agents, "global guidance", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// diff phase has no default prompt, should fall back to globalGuidance
	if specs[0].Guidance != "global guidance" {
		t.Errorf("expected global guidance for diff phase, got %q", specs[0].Guidance)
	}
}

func TestBuildReviewerSpecs_EmptyPhasesError(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	_, err := BuildReviewerSpecs(nil, agents, "", "", false)
	if err == nil {
		t.Error("expected error for empty phases, got nil")
	}
}

func TestBuildReviewerSpecs_ZeroReviewerCount(t *testing.T) {
	agents := []agent.Agent{&mockPhaseAgent{name: "codex"}}
	phases := []PhaseConfig{
		{Phase: "arch", ReviewerCount: 0},
	}
	_, err := BuildReviewerSpecs(phases, agents, "", "", false)
	if err == nil {
		t.Error("expected error for zero reviewer count, got nil")
	}
}

func TestDefaultPromptForPhase(t *testing.T) {
	tests := []struct {
		phase     string
		wantEmpty bool
	}{
		{"arch", false},
		{"diff", true},
		{"", true},
		{"unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			got := defaultPromptForPhase(tt.phase)
			if tt.wantEmpty && got != "" {
				t.Errorf("defaultPromptForPhase(%q) = %q, want empty", tt.phase, got)
			}
			if !tt.wantEmpty && got == "" {
				t.Errorf("defaultPromptForPhase(%q) = empty, want non-empty", tt.phase)
			}
		})
	}
}
