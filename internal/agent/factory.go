package agent

import (
	"fmt"
	"slices"
)

// agentRegistry holds the factory functions for a single agent.
type agentRegistry struct {
	newAgent         func(opts AgentOptions) Agent
	newReviewParser  func(reviewerID int) ReviewParser
	newSummaryParser func() SummaryParser
}

// registry maps agent names to their factory functions.
// To add a new agent, add an entry here - no other changes needed.
var registry = map[string]agentRegistry{
	"codex": {
		newAgent:         func(opts AgentOptions) Agent { return NewCodexAgentWithOptions(opts) },
		newReviewParser:  func(id int) ReviewParser { return NewCodexOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewCodexSummaryParser() },
	},
	"claude": {
		newAgent:         func(opts AgentOptions) Agent { return NewClaudeAgentWithOptions(opts) },
		newReviewParser:  func(id int) ReviewParser { return NewClaudeOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewClaudeSummaryParser() },
	},
	"gemini": {
		newAgent:         func(opts AgentOptions) Agent { return NewGeminiAgentWithOptions(opts) },
		newReviewParser:  func(id int) ReviewParser { return NewGeminiOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewGeminiSummaryParser() },
	},
}

// SupportedAgents lists all valid agent names.
// Derived from the registry to stay in sync automatically.
var SupportedAgents = func() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}()

// DefaultAgent is the default agent used for reviews when none is specified.
const DefaultAgent = "codex"

// DefaultSummarizerAgent is the default agent used for summarization when none is specified.
const DefaultSummarizerAgent = "codex"

// NewAgent creates an Agent by name with the default model.
// Supported agents: codex, claude, gemini
func NewAgent(name string) (Agent, error) {
	return NewAgentWithOptions(name, AgentOptions{})
}

// NewAgentWithModel creates an Agent by name with an optional model override.
// If model is empty, the agent uses its default model.
// Deprecated: prefer NewAgentWithOptions for new call sites.
func NewAgentWithModel(name, model string) (Agent, error) {
	return NewAgentWithOptions(name, AgentOptions{Model: model})
}

// NewAgentWithOptions creates an Agent by name with the given options.
// opts.Model overrides the agent's default model (empty = agent default).
// opts.Effort configures agent-specific reasoning effort (empty = agent default).
func NewAgentWithOptions(name string, opts AgentOptions) (Agent, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, supported: %v", name, SupportedAgents)
	}
	return reg.newAgent(opts), nil
}

// NewReviewParser creates a ReviewParser for the given agent name.
// The parser matches the output format of the corresponding agent.
func NewReviewParser(agentName string, reviewerID int) (ReviewParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no parser available", agentName)
	}
	return reg.newReviewParser(reviewerID), nil
}

// NewSummaryParser creates a SummaryParser for the given agent name.
// The parser matches the summary output format of the corresponding agent.
func NewSummaryParser(agentName string) (SummaryParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no summary parser available", agentName)
	}
	return reg.newSummaryParser(), nil
}
