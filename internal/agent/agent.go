package agent

import (
	"context"
)

// AgentOptions configures an Agent instance at construction time.
// Model overrides the agent's default model (empty = agent default).
// Effort is agent-specific thinking/reasoning configuration:
//   - codex: low|medium|high maps to `-c model_reasoning_effort=<value>`
//   - claude: low|medium|high|xhigh|max maps to --effort; other values are
//     silently dropped (session-scoped; available levels depend on the model)
//   - gemini: unsupported, ignored
//
// Empty Effort means "use agent default" (no flag passed).
type AgentOptions struct {
	Model  string
	Effort string
}

// Agent represents a backend that can execute code reviews and summarizations.
// Implementations include CodexAgent, ClaudeAgent, GeminiAgent.
type Agent interface {
	// Name returns the agent's identifier (e.g., "codex", "claude", "gemini").
	Name() string

	// IsAvailable checks if the agent's backend CLI is installed and accessible.
	// Returns an error if the agent cannot be used.
	IsAvailable() error

	// ExecuteReview runs a code review with the given configuration.
	// Returns an ExecutionResult for streaming output and an error if execution fails.
	// The caller MUST call Close() on the result to ensure proper resource cleanup.
	// After Close(), ExitCode() and Stderr() return valid values.
	ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error)

	// ExecuteSummary runs a summarization task with the given prompt and input data.
	// The prompt contains the summarization instructions.
	// The input contains the data to summarize (typically JSON-encoded aggregated findings).
	// Returns an ExecutionResult for streaming output.
	// The caller MUST call Close() on the result to ensure proper resource cleanup.
	// After Close(), ExitCode() and Stderr() return valid values.
	ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error)
}
