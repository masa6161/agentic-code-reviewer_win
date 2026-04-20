package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
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
