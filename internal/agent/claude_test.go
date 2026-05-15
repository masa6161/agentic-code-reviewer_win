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

func TestNewClaudeAgent(t *testing.T) {
	agent := NewClaudeAgent("")
	if agent == nil {
		t.Fatal("NewClaudeAgent() returned nil")
	}
}

func TestClaudeAgent_Name(t *testing.T) {
	agent := NewClaudeAgent("")
	got := agent.Name()
	want := "claude"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestClaudeAgent_IsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		prepareMockCLI(t, "claude", "args")
		agent := NewClaudeAgent("")
		if err := agent.IsAvailable(); err != nil {
			t.Errorf("IsAvailable() unexpected error = %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", "")
		agent := NewClaudeAgent("")
		err := agent.IsAvailable()
		if err == nil {
			t.Error("IsAvailable() should return error when claude is not in PATH")
			return
		}
		if !strings.Contains(err.Error(), "claude CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'claude CLI not found'", err)
		}
	})
}

func TestClaudeAgent_ExecuteReview_ClaudeNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure claude is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewClaudeAgent("")
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
		t.Error("ExecuteReview() should return error when claude is not available")
	}

	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'claude CLI not found'", err)
	}
}

func TestClaudeAgentInterface(t *testing.T) {
	var _ Agent = (*ClaudeAgent)(nil)
}

func TestClaudeAgent_ExecuteSummary_ClaudeNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure claude is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewClaudeAgent("")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "test prompt", []byte(`{"findings":[]}`))
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when claude is not available")
	}

	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'claude CLI not found'", err)
	}
}

func TestClaudeAgent_ExecuteReview_Args(t *testing.T) {
	tmpDir := t.TempDir()

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

	prepareMockCLI(t, "claude", "args-prefix-stdin")

	agent := NewClaudeAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
		WorkDir: tmpDir,
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
	if !strings.Contains(outputStr, "ARG:--print") {
		t.Errorf("expected --print flag in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - flag (stdin mode) in args, got:\n%s", outputStr)
	}
}

func TestClaudeAgent_ExecuteReview_RefFileMode(t *testing.T) {
	tmpDir := t.TempDir()

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

	bigContent := strings.Repeat("// line of code\n", RefFileSizeThreshold/16+1)
	if err := os.WriteFile(testFile, []byte(bigContent), 0644); err != nil {
		t.Fatal(err)
	}

	prepareMockCLI(t, "claude", "stdin")

	agent := NewClaudeAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
		WorkDir: tmpDir,
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
	if !strings.Contains(outputStr, ".arc-diff-") {
		t.Errorf("expected ref-file path in prompt for large diff, got:\n%s", outputStr[:min(200, len(outputStr))])
	}
	if !strings.Contains(outputStr, "Read tool") {
		t.Errorf("expected 'Read tool' instruction in ref-file prompt, got:\n%s", outputStr[:min(200, len(outputStr))])
	}

	result.Close()
	matches, _ := filepath.Glob(filepath.Join(tmpDir, ".arc-diff-*"))
	if len(matches) > 0 {
		t.Errorf("temp diff file not cleaned up: %v", matches)
	}
}

func TestClaudeAgent_ExecuteReview_ExplicitRefFile(t *testing.T) {
	tmpDir := t.TempDir()

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

	if err := os.WriteFile(testFile, []byte("package main\n\n// small change\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prepareMockCLI(t, "claude", "stdin")

	agent := NewClaudeAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:    "HEAD",
		WorkDir:    tmpDir,
		UseRefFile: true,
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
	if !strings.Contains(outputStr, ".arc-diff-") {
		t.Errorf("UseRefFile=true should trigger ref-file mode, got:\n%s", outputStr[:min(200, len(outputStr))])
	}
}

func TestClaudeAgent_ExecuteSummary_Args(t *testing.T) {
	prepareMockCLI(t, "claude", "args-prefix-stdin")

	agent := NewClaudeAgent("")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "ARG:--print") {
		t.Errorf("expected --print in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:--output-format") {
		t.Errorf("expected --output-format in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:json") {
		t.Errorf("expected json in args, got:\n%s", outputStr)
	}
	// Verify --json-schema is NOT used (it constrains all ExecuteSummary callers to one schema)
	if strings.Contains(outputStr, "ARG:--json-schema") {
		t.Errorf("unexpected --json-schema in args — ExecuteSummary must not constrain output format")
	}
}

func TestClaudeAgent_ExecuteReview_WithEffort(t *testing.T) {
	tmpDir := t.TempDir()

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

	prepareMockCLI(t, "claude", "args-prefix-stdin")

	agent := NewClaudeAgentWithOptions(AgentOptions{Effort: "high"})
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
		WorkDir: tmpDir,
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
	args := parseHelperArgs(outputStr)
	iEffort := argIndex(args, "--effort")
	iValue := argIndex(args, "high")
	iPrint := argIndex(args, "--print")
	iDash := argIndex(args, "-")
	if iEffort < 0 || iValue < 0 {
		t.Fatalf("expected --effort and high in args, got: %v", args)
	}
	if iValue != iEffort+1 {
		t.Errorf("expected 'high' immediately after --effort, got args=%v", args)
	}
	// Position guard: claude CLI requires --effort to precede the positional
	// stdin marker '-' and the --print flag. Without this ordering claude
	// silently drops the flag (the original Round-8 bug).
	if iPrint < 0 || iEffort > iPrint {
		t.Errorf("--effort (idx=%d) must precede --print (idx=%d), got args=%v", iEffort, iPrint, args)
	}
	if iDash < 0 || iEffort > iDash {
		t.Errorf("--effort (idx=%d) must precede positional '-' (idx=%d), got args=%v", iEffort, iDash, args)
	}
}

func TestClaudeAgent_ExecuteSummary_WithEffort(t *testing.T) {
	prepareMockCLI(t, "claude", "args-prefix-stdin")

	agent := NewClaudeAgentWithOptions(AgentOptions{Effort: "medium"})
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)
	args := parseHelperArgs(outputStr)
	iEffort := argIndex(args, "--effort")
	iValue := argIndex(args, "medium")
	iPrint := argIndex(args, "--print")
	iFmt := argIndex(args, "--output-format")
	iDash := argIndex(args, "-")
	if iEffort < 0 || iValue < 0 {
		t.Fatalf("expected --effort and medium in args, got: %v", args)
	}
	if iValue != iEffort+1 {
		t.Errorf("expected 'medium' immediately after --effort, got args=%v", args)
	}
	// Position guard: same constraint as ExecuteReview — --effort must
	// precede --print, --output-format, and the positional '-' stdin marker.
	if iPrint < 0 || iEffort > iPrint {
		t.Errorf("--effort (idx=%d) must precede --print (idx=%d), got args=%v", iEffort, iPrint, args)
	}
	if iFmt < 0 || iEffort > iFmt {
		t.Errorf("--effort (idx=%d) must precede --output-format (idx=%d), got args=%v", iEffort, iFmt, args)
	}
	if iDash < 0 || iEffort > iDash {
		t.Errorf("--effort (idx=%d) must precede positional '-' (idx=%d), got args=%v", iEffort, iDash, args)
	}
}

func TestClaudeEffortArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"low", []string{"--effort", "low"}},
		{"Low", []string{"--effort", "low"}},
		{"medium", []string{"--effort", "medium"}},
		{"High", []string{"--effort", "high"}},
		{"xhigh", []string{"--effort", "xhigh"}},
		{"max", []string{"--effort", "max"}},
		{"MAX", []string{"--effort", "max"}},
		{"8000", nil},
		{"unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := claudeEffortArgs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("claudeEffortArgs(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("claudeEffortArgs(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
