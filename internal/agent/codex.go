package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// codexReasoningEffortArgs maps Spec.Effort to codex CLI's -c config override.
// codex does not have a --reasoning-effort flag; the equivalent is
//
//	-c model_reasoning_effort=<value>
//
// which sets the value in ~/.codex/config.toml at runtime. See `codex exec --help`
// for the -c syntax. Unknown values are silently ignored — validation lives in
// the config layer.
func codexReasoningEffortArgs(effort string) []string {
	switch strings.ToLower(effort) {
	case "low", "medium", "high":
		return []string{"-c", "model_reasoning_effort=" + strings.ToLower(effort)}
	default:
		return nil
	}
}

// Compile-time interface check
var _ Agent = (*CodexAgent)(nil)

// CodexAgent implements the Agent interface for the Codex CLI backend.
type CodexAgent struct {
	model     string
	effort    string
	codexHome string
}

// NewCodexAgent creates a new CodexAgent instance.
// If model is non-empty, it overrides the default model via --model.
func NewCodexAgent(model string) *CodexAgent {
	return &CodexAgent{model: model}
}

// NewCodexAgentWithOptions creates a new CodexAgent instance with the given options.
func NewCodexAgentWithOptions(opts AgentOptions) *CodexAgent {
	return &CodexAgent{model: opts.Model, effort: opts.Effort, codexHome: opts.CodexHome}
}

// Name returns the agent's identifier.
func (c *CodexAgent) Name() string {
	return "codex"
}

// Options returns the AgentOptions the agent was constructed with.
func (c *CodexAgent) Options() AgentOptions {
	return AgentOptions{Model: c.model, Effort: c.effort, CodexHome: c.codexHome}
}

// IsAvailable checks if the codex CLI is installed and accessible.
func (c *CodexAgent) IsAvailable() error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the codex CLI.
// Returns an ExecutionResult for streaming the JSONL output.
//
// Without guidance, uses 'codex exec review --base X' for the built-in review behavior.
// With guidance, falls back to the diff-based review path because codex's --base flag
// and stdin prompt (-) are mutually exclusive (see #170).
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Per-reviewer model override
	model := c.model
	if config.Model != "" {
		model = config.Model
	}

	// Use diff-based review path when guidance, phase, or target files is set.
	// Codex's built-in "review --base" path ignores ReviewConfig.Phase/TargetFiles,
	// so we must route through the diff-based path for those features.
	// Note: DiffPrecomputed alone does NOT trigger this path — in mixed-agent runs
	// (codex+claude), DiffPrecomputed is globally true for Claude's benefit,
	// but Codex should still use its built-in review when no guidance/phase is set.
	if config.Guidance != "" || config.Phase != "" || len(config.TargetFiles) > 0 {
		effortArgs := codexReasoningEffortArgs(c.effort)
		args := append(effortArgs, []string{"exec", "--json", "--color", "never", "-"}...)
		if model != "" {
			args = append([]string{"--model", model}, args...)
		}
		return executeDiffBasedReview(ctx, config, diffReviewConfig{
			Command:       "codex",
			Args:          args,
			Env:           c.codexEnv(),
			DefaultPrompt: DefaultCodexPrompt,
			RefFilePrompt: DefaultCodexRefFilePrompt,
		})
	}

	effortArgs := codexReasoningEffortArgs(c.effort)
	args := append(effortArgs, []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}...)
	if model != "" {
		args = append([]string{"--model", model}, args...)
	}

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
		Env:     c.codexEnv(),
		WorkDir: config.WorkDir,
	})
}

// ExecuteSummary runs a summarization task using the codex CLI.
// Uses 'codex exec --color never -' with the prompt and input piped to stdin.
//
// Note: While Codex can read files within its working directory, this function
// embeds the input directly in the prompt for simplicity. Very large inputs
// (>100KB) may hit prompt length limits, but summary inputs are typically
// much smaller since they contain aggregated findings rather than raw diffs.
func (c *CodexAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	effortArgs := codexReasoningEffortArgs(c.effort)
	args := append(effortArgs, []string{"exec", "--json", "--color", "never", "-"}...)
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}
	// Use MultiReader to avoid copying large input byte slice
	stdin := io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
		Stdin:   stdin,
		Env:     c.codexEnv(),
	})
}

func (c *CodexAgent) codexEnv() map[string]string {
	env := make(map[string]string)

	codexHome := strings.TrimSpace(c.codexHome)
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv("ARC_CODEX_HOME"))
	}
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if codexHome == "" {
		codexHome = defaultCodexHome()
	}
	if codexHome != "" {
		env["CODEX_HOME"] = codexHome
	}

	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	home := strings.TrimSpace(os.Getenv("HOME"))

	if userProfile == "" && home != "" {
		env["USERPROFILE"] = home
	}
	if home == "" && userProfile != "" {
		env["HOME"] = userProfile
	}
	env["LC_ALL"] = "C"

	return env
}

func defaultCodexHome() string {
	if userProfile := strings.TrimSpace(os.Getenv("USERPROFILE")); userProfile != "" {
		return filepath.Join(userProfile, ".codex")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".codex")
	}
	return ""
}
