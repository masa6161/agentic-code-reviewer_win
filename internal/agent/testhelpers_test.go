package agent

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	agentTestHelperEnv      = "ACR_AGENT_TEST_HELPER"
	agentTestHelperModeEnv  = "ACR_AGENT_TEST_HELPER_MODE"
	agentTestHelperExitEnv  = "ACR_AGENT_TEST_HELPER_EXIT_CODE"
	agentTestHelperSleepEnv = "ACR_AGENT_TEST_HELPER_SLEEP"
)

func TestMain(m *testing.M) {
	if os.Getenv(agentTestHelperEnv) == "1" {
		os.Exit(runAgentTestHelper())
	}
	os.Exit(m.Run())
}

func runAgentTestHelper() int {
	mode := os.Getenv(agentTestHelperModeEnv)

	switch mode {
	case "args":
		return emitArgs(false, false)
	case "args-prefix-stdin":
		return emitArgs(true, true)
	case "env":
		return emitEnv("CODEX_HOME", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "LC_ALL")
	case "stdin":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		return 0
	case "sleep":
		sleepFor := 10 * time.Second
		if v := os.Getenv(agentTestHelperSleepEnv); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				sleepFor = d
			}
		}
		time.Sleep(sleepFor)
		return 0
	case "exit":
		if v := os.Getenv(agentTestHelperExitEnv); v != "" {
			if code, err := strconv.Atoi(v); err == nil {
				_, _ = io.Copy(io.Discard, os.Stdin)
				return code
			}
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	default:
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
}

func emitEnv(keys ...string) int {
	for _, key := range keys {
		fmt.Fprintf(os.Stdout, "%s=%s\n", key, os.Getenv(key))
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

func emitArgs(prefix bool, copyStdin bool) int {
	for _, arg := range os.Args[1:] {
		if prefix {
			fmt.Fprintf(os.Stdout, "ARG:%s\n", arg)
			continue
		}
		fmt.Fprintln(os.Stdout, arg)
	}
	if copyStdin {
		_, _ = io.Copy(os.Stdout, os.Stdin)
	} else {
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
	return 0
}

func helperBinaryPath(t *testing.T) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to determine test executable path: %v", err)
	}
	return exe
}

func copyHelperBinary(t *testing.T, dir, name string) string {
	t.Helper()

	src := helperBinaryPath(t)
	dst := filepath.Join(dir, helperBinaryName(name))

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

func helperBinaryName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func prependPath(t *testing.T, dir string) {
	t.Helper()

	orig := os.Getenv("PATH")
	if orig == "" {
		t.Setenv("PATH", dir)
		return
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
}

func newHelperCommand(t *testing.T, mode string, args ...string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(helperBinaryPath(t), args...)
	configureCmdForPlatform(cmd)
	cmd.Env = append(os.Environ(),
		agentTestHelperEnv+"=1",
		agentTestHelperModeEnv+"="+mode,
	)
	return cmd
}

func prepareMockCLI(t *testing.T, name, mode string) {
	t.Helper()

	dir := t.TempDir()
	copyHelperBinary(t, dir, name)
	prependPath(t, dir)
	t.Setenv(agentTestHelperEnv, "1")
	t.Setenv(agentTestHelperModeEnv, mode)
}

// parseHelperArgs extracts the argv received by the mock CLI helper.
// It accepts both "args" mode (raw newline-separated) and "args-prefix-stdin"
// mode (each arg prefixed with "ARG:"). Lines that do not look like an arg
// (e.g., copied stdin payload) are skipped. The returned slice preserves the
// order in which the helper saw the arguments.
func parseHelperArgs(output string) []string {
	var args []string
	prefix := "ARG:"
	hasPrefix := strings.Contains(output, prefix)
	for _, line := range strings.Split(output, "\n") {
		if hasPrefix {
			if strings.HasPrefix(line, prefix) {
				args = append(args, strings.TrimPrefix(line, prefix))
			}
			continue
		}
		// Plain "args" mode: every non-empty line up until the first blank
		// line is an arg. Stop at the first blank to avoid pulling in copied
		// stdin payload (when the helper is in args-prefix-stdin variants).
		if line == "" {
			break
		}
		args = append(args, line)
	}
	return args
}

// argIndex returns the index of target in args, or -1 if missing.
func argIndex(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}
