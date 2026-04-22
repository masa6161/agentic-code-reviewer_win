package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCodexAgent(t *testing.T) {
	agent := NewCodexAgent("")
	if agent == nil {
		t.Fatal("NewCodexAgent() returned nil")
	}
}

func TestCodexAgent_Name(t *testing.T) {
	agent := NewCodexAgent("")
	got := agent.Name()
	want := "codex"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestCodexAgent_IsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		prepareMockCLI(t, "codex", "args")
		agent := NewCodexAgent("")
		if err := agent.IsAvailable(); err != nil {
			t.Errorf("IsAvailable() unexpected error = %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", "")
		agent := NewCodexAgent("")
		err := agent.IsAvailable()
		if err == nil {
			t.Error("IsAvailable() should return error when codex is not in PATH")
			return
		}
		if !strings.Contains(err.Error(), "codex CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'codex CLI not found'", err)
		}
	})
}

func TestCodexAgent_ExecuteReview_CodexNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure codex is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewCodexAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "main",
		WorkDir: ".",
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteReview() should return error when codex is not available")
	}

	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'codex CLI not found'", err)
	}
}

func TestAgentInterface(t *testing.T) {
	var _ Agent = (*CodexAgent)(nil)
}

func TestCodexAgent_ExecuteSummary_CodexNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure codex is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewCodexAgent("")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "test prompt", []byte(`{"findings":[]}`))
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when codex is not available")
	}

	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'codex CLI not found'", err)
	}
}

func TestCodexAgent_ExecuteReview_ArgsWithoutGuidance(t *testing.T) {
	prepareMockCLI(t, "codex", "args")

	agent := NewCodexAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "main",
		WorkDir: t.TempDir(),
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	expected := []string{"exec", "--json", "--color", "never", "review", "--base", "main"}
	if len(args) != len(expected) {
		t.Fatalf("got %d args %v, want %d args %v", len(args), args, len(expected), expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestCodexAgent_ExecuteReview_ArgsWithGuidance(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a git repo so executeDiffBasedReview can fetch a diff
	for _, cmd := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v failed: %v\n%s", cmd, err, out)
		}
	}

	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit %v failed: %v\n%s", cmd, err, out)
		}
	}

	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prepareMockCLI(t, "codex", "args-prefix-stdin")

	agent := NewCodexAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:  "HEAD",
		WorkDir:  tmpDir,
		Guidance: "Focus on security issues",
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)

	// With guidance, should use diff-based review (no "review" or "--base" args)
	if strings.Contains(outputStr, "ARG:review") {
		t.Errorf("with guidance, should not use built-in 'review' subcommand, got:\n%s", outputStr)
	}
	if strings.Contains(outputStr, "ARG:--base") {
		t.Errorf("with guidance, should not use --base flag, got:\n%s", outputStr)
	}
	// Should use stdin mode
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - flag (stdin mode) in args, got:\n%s", outputStr)
	}
	// Should include guidance in the rendered prompt
	if !strings.Contains(outputStr, "Focus on security issues") {
		t.Errorf("expected guidance in stdin prompt, got:\n%s", outputStr)
	}
}

func TestCodexAgent_ExecuteReview_TargetFilesRouting(t *testing.T) {
	// Test that when TargetFiles is set (but no Guidance/Phase),
	// the diff-based path is used instead of the built-in review path.
	tmpDir := t.TempDir()

	// Set up a git repo so executeDiffBasedReview can fetch a diff
	for _, cmd := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v failed: %v\n%s", cmd, err, out)
		}
	}

	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit %v failed: %v\n%s", cmd, err, out)
		}
	}

	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prepareMockCLI(t, "codex", "args-prefix-stdin")

	agent := NewCodexAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:     "HEAD",
		WorkDir:     tmpDir,
		TargetFiles: []string{"test.go"},
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)

	// With TargetFiles set, should use diff-based review (no "review" or "--base" args)
	if strings.Contains(outputStr, "ARG:review") {
		t.Errorf("with TargetFiles, should not use built-in 'review' subcommand, got:\n%s", outputStr)
	}
	if strings.Contains(outputStr, "ARG:--base") {
		t.Errorf("with TargetFiles, should not use --base flag, got:\n%s", outputStr)
	}
	// Should use stdin mode
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - flag (stdin mode) in args, got:\n%s", outputStr)
	}
}

func TestCodexAgent_ExecuteSummary_Args(t *testing.T) {
	prepareMockCLI(t, "codex", "args")

	agent := NewCodexAgent("")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize this", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	expected := []string{"exec", "--json", "--color", "never", "-"}
	if len(args) != len(expected) {
		t.Fatalf("got %d args %v, want %d args %v", len(args), args, len(expected), expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestCodexReasoningEffortArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"low", []string{"-c", "model_reasoning_effort=low"}},
		{"LOW", []string{"-c", "model_reasoning_effort=low"}},
		{"Medium", []string{"-c", "model_reasoning_effort=medium"}},
		{"high", []string{"-c", "model_reasoning_effort=high"}},
		{"unknown", nil},
		{"8000", nil},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := codexReasoningEffortArgs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestCodexAgent_ExecuteReview_WithEffort(t *testing.T) {
	prepareMockCLI(t, "codex", "args")

	agent := NewCodexAgentWithOptions(AgentOptions{Effort: "high"})
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "main",
		WorkDir: t.TempDir(),
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// Regression guard for F#2 (Round-7): -c flag must precede the 'exec'
	// subcommand for codex CLI to recognize the model_reasoning_effort
	// override. Without ordering verification, an append-style bug like the
	// claude.go --effort one (Round-8 #4) could silently regress.
	args := parseHelperArgs(string(output))
	iC := argIndex(args, "-c")
	iValue := argIndex(args, "model_reasoning_effort=high")
	iExec := argIndex(args, "exec")
	if iC < 0 || iValue < 0 {
		t.Fatalf("expected -c and model_reasoning_effort=high in args, got: %v", args)
	}
	if iValue != iC+1 {
		t.Errorf("expected 'model_reasoning_effort=high' immediately after -c, got args=%v", args)
	}
	if iExec < 0 || iC > iExec {
		t.Errorf("-c (idx=%d) must precede 'exec' subcommand (idx=%d), got args=%v", iC, iExec, args)
	}
}

func TestCodexAgent_ExecuteSummary_WithEffort(t *testing.T) {
	prepareMockCLI(t, "codex", "args")

	agent := NewCodexAgentWithOptions(AgentOptions{Effort: "medium"})
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize this", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// Regression guard (see TestCodexAgent_ExecuteReview_WithEffort):
	// -c must precede 'exec' AND the positional '-' stdin marker.
	args := parseHelperArgs(string(output))
	iC := argIndex(args, "-c")
	iValue := argIndex(args, "model_reasoning_effort=medium")
	iExec := argIndex(args, "exec")
	iDash := argIndex(args, "-")
	if iC < 0 || iValue < 0 {
		t.Fatalf("expected -c and model_reasoning_effort=medium in args, got: %v", args)
	}
	if iValue != iC+1 {
		t.Errorf("expected 'model_reasoning_effort=medium' immediately after -c, got args=%v", args)
	}
	if iExec < 0 || iC > iExec {
		t.Errorf("-c (idx=%d) must precede 'exec' subcommand (idx=%d), got args=%v", iC, iExec, args)
	}
	if iDash < 0 || iC > iDash {
		t.Errorf("-c (idx=%d) must precede positional '-' stdin marker (idx=%d), got args=%v", iC, iDash, args)
	}
}
