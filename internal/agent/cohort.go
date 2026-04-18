// Package agent provides LLM agent abstractions.
package agent

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ParseAgentNames splits a comma-separated agent string into a slice of names.
// Handles whitespace trimming. Returns default agent if input is empty.
func ParseAgentNames(input string) []string {
	if input == "" {
		return []string{DefaultAgent}
	}

	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			result = append(result, name)
		}
	}

	if len(result) == 0 {
		return []string{DefaultAgent}
	}

	return result
}

// ValidateAgentNames checks that all agent names are supported.
// Returns an error listing unsupported agents.
func ValidateAgentNames(names []string) error {
	var invalid []string
	for _, name := range names {
		if !slices.Contains(SupportedAgents, name) {
			invalid = append(invalid, name)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("unsupported agent(s): %s (supported: %v)",
			strings.Join(invalid, ", "), SupportedAgents)
	}

	return nil
}

// CreateAgents creates Agent instances for the given names with the default model.
// Validates all agent CLIs are available (fail fast).
func CreateAgents(names []string) ([]Agent, error) {
	return CreateAgentsWithModel(names, "")
}

// CreateAgentsWithModel creates Agent instances for the given names with an optional model override.
// If model is empty, agents use their default models.
// Validates all agent CLIs are available (fail fast).
//
// Deprecated: prefer CreateAgentsFromSpecs for new call sites so per-agent
// effort and model can be resolved independently.
func CreateAgentsWithModel(names []string, model string) ([]Agent, error) {
	specs := make([]AgentSpec, len(names))
	for i, n := range names {
		specs[i] = AgentSpec{Name: n, Options: AgentOptions{Model: model}}
	}
	return CreateAgentsFromSpecs(specs)
}

// AgentSpec pairs an agent name with its construction options. Used by
// CreateAgentsFromSpecs to build a reviewer cohort where each agent instance
// may have a distinct model/effort resolved from the (size, role, agent) tuple.
type AgentSpec struct {
	Name    string
	Options AgentOptions
}

// CreateAgentsFromSpecs creates Agent instances for the given specs.
// Agents are deduplicated by (Name, Options) so callers can pass repeated
// entries (e.g., for reviewer round-robin) without constructing duplicate
// backends. Validates all agent CLIs are available (fail fast).
func CreateAgentsFromSpecs(specs []AgentSpec) ([]Agent, error) {
	agents := make([]Agent, 0, len(specs))
	seen := make(map[AgentSpec]Agent)

	for _, spec := range specs {
		// Reuse existing agent instance if same (name, options) tuple appears multiple times
		if existing, ok := seen[spec]; ok {
			agents = append(agents, existing)
			continue
		}

		a, err := NewAgentWithOptions(spec.Name, spec.Options)
		if err != nil {
			return nil, err
		}

		if err := a.IsAvailable(); err != nil {
			return nil, fmt.Errorf("%s CLI not found: %w", spec.Name, err)
		}

		seen[spec] = a
		agents = append(agents, a)
	}

	return agents, nil
}

// AgentsNeedDiff returns true if any agent in the list requires a pre-computed diff.
// Codex has built-in diff via --base and doesn't need one.
func AgentsNeedDiff(agents []Agent) bool {
	for _, a := range agents {
		if a.Name() != "codex" {
			return true
		}
	}
	return false
}

// FormatDistribution returns a human-readable distribution summary.
// Example: "2×codex, 2×claude, 1×gemini" for 5 reviewers with 3 agent types.
func FormatDistribution(agents []Agent, totalReviewers int) string {
	if len(agents) == 0 {
		return ""
	}

	if len(agents) == 1 {
		return agents[0].Name()
	}

	// Count how many times each agent will be used
	counts := make(map[string]int)
	for i := range totalReviewers {
		agent := agents[i%len(agents)]
		counts[agent.Name()]++
	}

	// Collect unique agent names and sort for consistent output
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Format as "N×agent"
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d×%s", counts[name], name))
	}

	return strings.Join(parts, ", ")
}

// AgentForReviewer returns the agent for a given reviewer ID using round-robin.
// Reviewer IDs are 1-based. Returns nil if agents slice is empty or reviewerID < 1.
func AgentForReviewer(agents []Agent, reviewerID int) Agent {
	if len(agents) == 0 || reviewerID < 1 {
		return nil
	}
	return agents[(reviewerID-1)%len(agents)]
}
