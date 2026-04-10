//go:build !windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestTerminateProcessTree_KillsPlainCommandWithoutProcessGroup(t *testing.T) {
	cmd := exec.Command(helperBinaryPath(t))
	cmd.Env = append(os.Environ(),
		agentTestHelperEnv+"=1",
		agentTestHelperModeEnv+"=sleep",
		agentTestHelperSleepEnv+"=30s",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid(%d) error = %v", cmd.Process.Pid, err)
	}
	if pgid == cmd.Process.Pid {
		t.Fatalf("expected helper process to inherit parent process group, got pgid == pid == %d", pgid)
	}

	if err := terminateProcessTree(cmd); err != nil {
		t.Fatalf("terminateProcessTree() error = %v, want nil", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("Wait() error = %v, want *exec.ExitError after SIGKILL", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() timed out; helper process was not terminated")
	}
}
