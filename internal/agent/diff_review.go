package agent

import (
	"bytes"
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// diffReviewConfig holds the agent-specific parameters for a diff-based review.
type diffReviewConfig struct {
	// Command is the CLI command name (e.g., "claude", "gemini").
	Command string
	// Args are the CLI arguments (e.g., ["--print", "-"] for claude).
	Args []string
	// DefaultPrompt is the standard review prompt template for this agent.
	DefaultPrompt string
	// RefFilePrompt is the review prompt template used when diff is in a file (must have %s for path).
	RefFilePrompt string
}

// executeDiffBasedReview is the shared review implementation for agents that receive
// a git diff (Claude, Gemini). It handles diff retrieval (using pre-computed or fetching),
// ref-file branching, prompt rendering, and command execution.
// resolvePrompts selects the appropriate prompt based on Phase, RolePrompts, and HasArchReviewer.
// When RolePrompts is true, Phase is set, and an arch reviewer exists, role-specific prompts are used.
// When HasArchReviewer is false, RolePrompts is not applied so that diff reviewers do not
// suppress cross-file findings. Legacy behavior (arch prompt override only) is preserved otherwise.
func resolvePrompts(config *ReviewConfig, dc *diffReviewConfig) {
	if config.Phase != "" && config.RolePrompts && config.HasArchReviewer {
		switch config.Phase {
		case "arch":
			dc.DefaultPrompt = AutoPhaseArchPrompt
			dc.RefFilePrompt = AutoPhaseArchRefFilePrompt
		default:
			dc.DefaultPrompt = AutoPhaseDiffPrompt
			dc.RefFilePrompt = AutoPhaseDiffRefFilePrompt
		}
	} else if config.Phase == "arch" {
		dc.DefaultPrompt = DefaultArchPrompt
	}
}

func executeDiffBasedReview(ctx context.Context, config *ReviewConfig, dc diffReviewConfig) (*ExecutionResult, error) {
	resolvePrompts(config, &dc)

	// Use pre-computed diff if available, otherwise fetch it
	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}
	}

	// Determine if we should use ref-file mode
	useRefFile := config.UseRefFile || len(diff) > RefFileSizeThreshold

	var prompt string
	var tempFilePath string

	if useRefFile && diff != "" {
		// Write diff to a temp file in the working directory
		absPath, err := WriteDiffToTempFile(config.WorkDir, diff)
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		prompt = fmt.Sprintf(dc.RefFilePrompt, absPath)
		prompt = RenderPrompt(prompt, config.Guidance)
	} else {
		// Use standard prompt with embedded diff
		prompt = RenderPrompt(dc.DefaultPrompt, config.Guidance)
		prompt = BuildPromptWithDiff(prompt, diff)
	}

	stdin := bytes.NewReader([]byte(prompt))

	return executeCommand(ctx, executeOptions{
		Command:      dc.Command,
		Args:         dc.Args,
		Stdin:        stdin,
		WorkDir:      config.WorkDir,
		TempFilePath: tempFilePath,
	})
}
