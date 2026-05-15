package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masa6161/arc-cli/internal/config"
)

// chdir changes to dir and returns a cleanup function to restore the original directory.
func chdir(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("failed to restore working directory to %s: %v", origDir, err)
		}
	})
}

// initGitRepo initializes a git repo in dir.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
}

func TestConfigInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, config.ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected .acr.yaml to be created")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .acr.yaml: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "summarizer_timeout: 5m") {
		t.Fatal("expected starter config to include summarizer_timeout")
	}
	if !strings.Contains(text, "fp_filter_timeout: 5m") {
		t.Fatal("expected starter config to include fp_filter_timeout")
	}
}

func TestConfigInit_FailsIfExists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Create the file first
	configPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when file already exists")
	}
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Round-9: cross_check defaults to enabled and now requires a model.
	// Supply via env so this test exercises the "valid" path it claims to
	// test, not the cross-check guard.
	t.Setenv("ACR_CROSS_CHECK_MODEL", "test-cc-model")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected valid config to succeed, got: %v", err)
	}
}

func TestConfigValidate_DetectsInvalidEnvVars(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Set semantically invalid env var (parses fine, but fails validation)
	t.Setenv("ACR_REVIEWERS", "0")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_REVIEWERS=0, got nil")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Errorf("expected error message to mention errors, got: %v", err)
	}
}

func TestConfigValidate_DetectsInvalidAgent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_REVIEWER_AGENT", "unsupported")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_REVIEWER_AGENT=unsupported, got nil")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Errorf("expected error message to mention errors, got: %v", err)
	}
}

func TestConfigValidate_DetectsNegativeRetries(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_RETRIES", "-1")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_RETRIES=-1, got nil")
	}
}

func TestConfigValidate_MalformedEnvVarIsError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_REVIEWERS", "not-a-number")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for malformed ACR_REVIEWERS, got nil")
	}
}

func TestConfigValidate_InvalidGuidanceFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_GUIDANCE_FILE", "/nonexistent/guidance.md")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for nonexistent guidance file, got nil")
	}
}

func TestConfigValidate_ValidGuidanceFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	guidancePath := filepath.Join(dir, "guidance.md")
	if err := os.WriteFile(guidancePath, []byte("review carefully"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACR_GUIDANCE_FILE", guidancePath)
	// Round-9: cross-check guard requires a model when enabled (default on).
	t.Setenv("ACR_CROSS_CHECK_MODEL", "test-cc-model")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("expected valid guidance file to pass, got: %v", err)
	}
}

// TestConfigValidate_SemanticErrorDoesNotMaskRuntimeError verifies that when
// .acr.yaml has semantic errors (e.g. reviewers: 0), ValidateRuntime still
// runs so env-driven cross_check runtime issues are reported.
//
// Round-12 conflated "cfg syntax error" and "cfg semantic error" under a single
// configFileError flag that skipped ValidateRuntime in BOTH cases, masking
// cross_check issues whenever a user had any unrelated cfg semantic error.
// Round-13 F#2 split the two: ValidateRuntime is skipped only on true syntax /
// regex / IO errors where cfg is unparseable.
func TestConfigValidate_SemanticErrorDoesNotMaskRuntimeError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Semantic error in cfg: reviewers must be >= 1.
	configPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("reviewers: 0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Leave ACR_CROSS_CHECK_MODEL unset so ValidateRuntime fires the
	// cross_check.model-required error on top of the cfg semantic error.

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error when config has semantic issues AND cross_check.model is missing")
	}
	// Before F#2 fix: count == 1 (only the lump "config file: ..." line;
	// ValidateRuntime was skipped entirely).
	// After fix: count >= 2 (lump + cross_check runtime error).
	var n int
	if _, scanErr := fmt.Sscanf(err.Error(), "configuration has %d error(s)", &n); scanErr != nil {
		t.Fatalf("could not parse error count from %q: %v", err.Error(), scanErr)
	}
	if n < 2 {
		t.Errorf("expected at least 2 errors (semantic cfg + cross_check runtime), got %d: %q", n, err.Error())
	}
}

// TestConfigValidate_SyntaxErrorSkipsRuntime verifies the complementary case:
// when .acr.yaml fails to parse (syntax / regex / IO error), ValidateRuntime
// is still skipped because cfg is unusable and running ValidateRuntime against
// Defaults would false-positive on cross_check.model (Defaults leaves it
// empty while cross_check.enabled=true). The user's intent is hidden behind
// the broken YAML.
func TestConfigValidate_SyntaxErrorSkipsCrossCheckRuntimeFalsePositive(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Syntax error: malformed YAML.
	configPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("reviewers: [not a number\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error on YAML syntax error")
	}
	var n int
	if _, scanErr := fmt.Sscanf(err.Error(), "configuration has %d error(s)", &n); scanErr != nil {
		t.Fatalf("could not parse error count from %q: %v", err.Error(), scanErr)
	}
	// Syntax-error path should yield exactly 1 error (the config-file lump).
	// If ValidateRuntime were incorrectly running, it would synthesize a
	// cross_check false-positive and bump the count to 2.
	if n != 1 {
		t.Errorf("expected exactly 1 error (config file syntax only) on syntax error, got %d: %q", n, err.Error())
	}
}

func TestConfigShow_DisplaysAllFields(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	expectedKeys := []string{
		"reviewers:", "concurrency:", "base:", "timeout:", "retries:", "fetch:",
		"auto_phase:", "strict:",
		"reviewer_agents:", "arch_reviewer_agent:", "diff_reviewer_agents:", "summarizer_agent:",
		"reviewer_model:", "summarizer_model:",
		"large_diff_reviewers:", "medium_diff_reviewers:", "small_diff_reviewers:",
		"summarizer_timeout:", "fp_filter_timeout:", "cross_check_timeout:",
		"fp_filter.enabled:", "fp_filter.threshold:",
		"pr_feedback.enabled:", "pr_feedback.agent:",
		"cross_check.enabled:", "cross_check.agent:", "cross_check.model:",
		"guidance_file:",
		"models.defaults.reviewer:", "models.defaults.arch_reviewer:",
		"models.defaults.diff_reviewer:", "models.defaults.summarizer:",
		"models.defaults.fp_filter:", "models.defaults.cross_check:",
		"models.defaults.pr_feedback:",
		"models.sizes:", "models.agents:",
	}

	for _, key := range expectedKeys {
		if !strings.Contains(output, key) {
			t.Errorf("expected output to contain %q, but it did not.\nOutput:\n%s", key, output)
		}
	}
}

func TestConfigShow_EnvOverrideReflected(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_STRICT", "true")
	t.Setenv("ACR_LARGE_DIFF_REVIEWERS", "8")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "strict:                          true") {
		t.Errorf("expected output to contain 'strict: true' reflecting ACR_STRICT=true.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "large_diff_reviewers:") || !strings.Contains(output, "8") {
		t.Errorf("expected output to reflect ACR_LARGE_DIFF_REVIEWERS=8.\nOutput:\n%s", output)
	}
}

func TestConfigShow_FallbackDisplay(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// arch_reviewer_agent fallback
	if !strings.Contains(output, "(first of reviewer_agents)") {
		t.Errorf("expected arch_reviewer_agent fallback text.\nOutput:\n%s", output)
	}
	// diff_reviewer_agents fallback
	if !strings.Contains(output, "(reviewer_agents)") {
		t.Errorf("expected diff_reviewer_agents fallback text.\nOutput:\n%s", output)
	}
	// reviewer_model and summarizer_model fallback
	if !strings.Contains(output, "(agent default)") {
		t.Errorf("expected model fallback text.\nOutput:\n%s", output)
	}
	// pr_feedback.agent and cross_check.agent fallback
	if !strings.Contains(output, "(same as summarizer_agent)") {
		t.Errorf("expected agent fallback text.\nOutput:\n%s", output)
	}
	// guidance_file and models.defaults.* fallback
	if !strings.Contains(output, "(not set)") {
		t.Errorf("expected '(not set)' fallback text.\nOutput:\n%s", output)
	}
	// models.sizes and models.agents fallback
	if !strings.Contains(output, "(none)") {
		t.Errorf("expected '(none)' fallback text.\nOutput:\n%s", output)
	}
	// concurrency: 0 fallback
	if !strings.Contains(output, "(auto)") {
		t.Errorf("expected concurrency fallback text.\nOutput:\n%s", output)
	}
}
