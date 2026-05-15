package summarizer

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	summarizerHelperEnv       = "ARC_SUMMARIZER_TEST_HELPER"
	summarizerHelperStdoutEnv = "ARC_SUMMARIZER_TEST_STDOUT"
	summarizerHelperStderrEnv = "ARC_SUMMARIZER_TEST_STDERR"
	summarizerHelperExitEnv   = "ARC_SUMMARIZER_TEST_EXIT_CODE"
)

func TestMain(m *testing.M) {
	if os.Getenv(summarizerHelperEnv) == "1" {
		os.Exit(runSummarizerHelper())
	}
	os.Exit(m.Run())
}

func runSummarizerHelper() int {
	_, _ = io.Copy(io.Discard, os.Stdin)
	if stderr := os.Getenv(summarizerHelperStderrEnv); stderr != "" {
		_, _ = io.WriteString(os.Stderr, stderr)
	}
	if stdout := os.Getenv(summarizerHelperStdoutEnv); stdout != "" {
		_, _ = io.WriteString(os.Stdout, stdout)
	}
	if exitCode := os.Getenv(summarizerHelperExitEnv); exitCode != "" {
		if code, err := strconv.Atoi(exitCode); err == nil {
			return code
		}
	}
	return 0
}

func prepareMockCodex(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()

	dir := t.TempDir()
	path := copySummarizerHelperBinary(t, dir, "codex")

	origPath := os.Getenv("PATH")
	if origPath == "" {
		t.Setenv("PATH", dir)
	} else {
		t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
	}
	t.Setenv(summarizerHelperEnv, "1")
	t.Setenv(summarizerHelperStdoutEnv, stdout)
	t.Setenv(summarizerHelperStderrEnv, stderr)
	t.Setenv(summarizerHelperExitEnv, strconv.Itoa(exitCode))

	return path
}

func copySummarizerHelperBinary(t *testing.T, dir, name string) string {
	t.Helper()

	src, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to determine test executable path: %v", err)
	}

	dst := filepath.Join(dir, summarizerHelperBinaryName(name))
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

func summarizerHelperBinaryName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}
