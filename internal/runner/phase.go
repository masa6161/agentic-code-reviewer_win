package runner

import (
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

// PhaseConfig defines a review phase and its parameters.
type PhaseConfig struct {
	Phase         string // "arch" | "diff"
	ReviewerCount int    // Number of reviewers for this phase
	AgentName     string // Which agent to use (empty = default from caller)
	Model         string // Model override (empty = agent default)
	Prompt        string // Per-phase guidance override (empty = use global guidance; phase prompt template is selected by Phase)
}

// BuildReviewerSpecs creates ReviewerSpecs from PhaseConfigs.
// Each PhaseConfig generates ReviewerCount specs with the phase's settings.
// defaultAgents are used when PhaseConfig.AgentName is empty.
// globalDiff is the pre-computed diff shared across all reviewers.
// diffPrecomputed indicates whether globalDiff was pre-computed.
func BuildReviewerSpecs(phases []PhaseConfig, defaultAgents []agent.Agent, globalGuidance, globalDiff string, diffPrecomputed bool) ([]ReviewerSpec, error) {
	if len(phases) == 0 {
		return nil, fmt.Errorf("at least one phase config is required")
	}

	var specs []ReviewerSpec
	reviewerIdx := 0
	for _, pc := range phases {
		if pc.ReviewerCount <= 0 {
			continue
		}

		for i := 0; i < pc.ReviewerCount; i++ {
			reviewerIdx++

			// Select agent: use PhaseConfig.AgentName if set, otherwise round-robin from defaultAgents
			var a agent.Agent
			if pc.AgentName != "" {
				var err error
				a, err = agent.NewAgentWithModel(pc.AgentName, pc.Model)
				if err != nil {
					return nil, fmt.Errorf("phase %q agent %q: %w", pc.Phase, pc.AgentName, err)
				}
			} else {
				a = agent.AgentForReviewer(defaultAgents, reviewerIdx)
			}

			// Guidance carries user-provided steering context, not the phase prompt template.
			// Phase-specific prompt selection happens in the agent execution path
			// (e.g., diff_review.go checks config.Phase and selects the appropriate template).
			guidance := pc.Prompt
			if guidance == "" {
				guidance = globalGuidance
			}

			specs = append(specs, ReviewerSpec{
				ReviewerID:      reviewerIdx,
				Agent:           a,
				Model:           pc.Model,
				Phase:           pc.Phase,
				Guidance:        guidance,
				Diff:            globalDiff,
				DiffPrecomputed: diffPrecomputed,
			})
		}
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no reviewers generated from phase configs")
	}

	return specs, nil
}

// defaultPromptForPhase returns the default prompt for a given phase.
// Returns empty string for unknown phases (caller should fall back to global guidance).
func defaultPromptForPhase(phase string) string {
	switch phase {
	case "arch":
		return agent.DefaultArchPrompt
	default:
		return ""
	}
}
