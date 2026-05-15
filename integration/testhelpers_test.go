package integration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const integrationMockEnv = "ARC_INTEGRATION_TEST_HELPER"
const integrationMockModeEnv = "ARC_INTEGRATION_TEST_HELPER_MODE"
const (
	integrationCodexReviewEnv   = "ARC_INTEGRATION_CODEX_REVIEW"
	integrationCodexSummaryEnv  = "ARC_INTEGRATION_CODEX_SUMMARY"
	integrationClaudeReviewEnv  = "ARC_INTEGRATION_CLAUDE_REVIEW"
	integrationClaudeSummaryEnv = "ARC_INTEGRATION_CLAUDE_SUMMARY"
	integrationGeminiReviewEnv  = "ARC_INTEGRATION_GEMINI_REVIEW"
	integrationGeminiSummaryEnv = "ARC_INTEGRATION_GEMINI_SUMMARY"
)

func TestMain(m *testing.M) {
	if os.Getenv(integrationMockEnv) == "1" {
		os.Exit(runIntegrationMockCLI())
	}
	os.Exit(m.Run())
}

func runIntegrationMockCLI() int {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0])))
	_, _ = io.Copy(io.Discard, os.Stdin)

	if os.Getenv(integrationMockModeEnv) == "missing" {
		return 127
	}

	switch name {
	case "codex":
		if hasArg("--json") {
			fmt.Fprintln(os.Stdout, envOrDefault(integrationCodexSummaryEnv, codexSummarizerResponse))
			return 0
		}
		fmt.Fprint(os.Stdout, envOrDefault(integrationCodexReviewEnv, codexReviewerResponse))
		return 0
	case "claude":
		if hasArg("json") {
			fmt.Fprintln(os.Stdout, envOrDefault(integrationClaudeSummaryEnv, claudeSummarizerJSON()))
			return 0
		}
		fmt.Fprint(os.Stdout, envOrDefault(integrationClaudeReviewEnv, claudeReviewerResponse))
		return 0
	case "gemini":
		if hasArg("json") {
			fmt.Fprintln(os.Stdout, envOrDefault(integrationGeminiSummaryEnv, geminiSummarizerJSON()))
			return 0
		}
		fmt.Fprint(os.Stdout, envOrDefault(integrationGeminiReviewEnv, geminiReviewerResponse))
		return 0
	case "gh":
		return 1
	default:
		return 0
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func hasArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if arg == target {
			return true
		}
	}
	return false
}

func copyIntegrationHelperBinary(t *testing.T, dir, name string) string {
	t.Helper()

	src, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to determine test executable path: %v", err)
	}

	dst := filepath.Join(dir, integrationHelperBinaryName(name))
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("failed to open test executable: %v", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("failed to create helper binary %s: %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("failed to copy helper binary to %s: %v", dst, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("failed to close helper binary %s: %v", dst, err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(dst, 0o755); err != nil {
			t.Fatalf("failed to mark helper binary executable: %v", err)
		}
	}

	return dst
}

func integrationHelperBinaryName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}
