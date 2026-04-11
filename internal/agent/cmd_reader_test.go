package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCmdReader_Close(t *testing.T) {
	cmd := newHelperCommand(t, "exit")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}

	// Read all output
	_, _ = io.ReadAll(reader)

	// Close should not error
	err = reader.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestCmdReader_Close_NilCommand(t *testing.T) {
	reader := &cmdReader{
		Reader: strings.NewReader("test"),
		cmd:    nil,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close() with nil cmd error = %v, want nil", err)
	}
}

func TestCmdReader_Close_CommandNotStarted(t *testing.T) {
	cmd := newHelperCommand(t, "exit")

	reader := &cmdReader{
		Reader: strings.NewReader("test"),
		cmd:    cmd,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close() with non-started command error = %v, want nil", err)
	}
}

func TestCmdReader_ExitCode_Success(t *testing.T) {
	cmd := newHelperCommand(t, "exit")
	cmd.Env = append(cmd.Env, agentTestHelperExitEnv+"=0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}

	_, _ = io.ReadAll(reader)
	_ = reader.Close()

	if got := reader.ExitCode(); got != 0 {
		t.Errorf("ExitCode() = %d, want 0", got)
	}
}

func TestCmdReader_ExitCode_Failure(t *testing.T) {
	cmd := newHelperCommand(t, "exit")
	cmd.Env = append(cmd.Env, agentTestHelperExitEnv+"=3")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}

	_, _ = io.ReadAll(reader)
	_ = reader.Close()

	if got := reader.ExitCode(); got == 0 {
		t.Error("ExitCode() = 0, want non-zero for failed command")
	}
}

func TestCmdReader_Close_Idempotent(t *testing.T) {
	cmd := newHelperCommand(t, "exit")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}

	_, _ = io.ReadAll(reader)

	// Close multiple times should not error
	if err := reader.Close(); err != nil {
		t.Errorf("First Close() error = %v, want nil", err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Second Close() error = %v, want nil", err)
	}
}

func TestCmdReader_CloseWithNilProcess(t *testing.T) {
	// cmdReader with cmd set but Process is nil (Start() was never called)
	cmd := newHelperCommand(t, "exit") // Don't start it

	reader := &cmdReader{
		Reader: strings.NewReader(""),
		cmd:    cmd,
		ctx:    context.Background(),
	}

	// Should not panic
	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdReader_CloseWithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := newHelperCommand(t, "sleep")
	cmd.Env = append(cmd.Env, agentTestHelperSleepEnv+"=30s")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start helper command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}

	cancel()

	// Should kill the helper process and not panic
	err = reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdReader_CloseAfterCancelOnCompletedProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, helperBinaryPath(t))
	cmd.Env = append(os.Environ(),
		agentTestHelperEnv+"=1",
		agentTestHelperModeEnv+"=exit",
	)
	configureCmdForPlatform(cmd)
	cmd.Cancel = func() error {
		return terminateProcessTree(cmd)
	}
	cmd.WaitDelay = cmdWaitDelay

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start helper command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}

	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	cancel()

	if err := reader.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if got := reader.ExitCode(); got != 0 {
		t.Fatalf("ExitCode() = %d, want 0", got)
	}
}

func TestTerminateProcessTree_ReturnsErrProcessDoneForCompletedProcess(t *testing.T) {
	cmd := newHelperCommand(t, "exit")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start helper command: %v", err)
	}

	if _, err := io.ReadAll(stdout); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v, want nil", err)
	}

	err = terminateProcessTree(cmd)
	if !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("terminateProcessTree() error = %v, want os.ErrProcessDone", err)
	}
}
