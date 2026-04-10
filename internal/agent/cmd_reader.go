package agent

import (
	"context"
	"io"
	"os/exec"
	"sync"
)

// Compile-time interface check
var _ io.Closer = (*cmdReader)(nil)

// cmdReader wraps an io.Reader and ensures the command is waited on when closed.
// It implements io.Closer and provides process exit code and stderr output
// after Close().
// This type is used by all agent implementations (codex, claude, gemini) to manage
// subprocess lifecycle.
type cmdReader struct {
	io.Reader
	cmd          *exec.Cmd
	ctx          context.Context
	stderr       stderrBuffer
	exitCode     int
	closeOnce    sync.Once
	tempFilePath string // temp file to clean up on Close (used by ref-file pattern)
}

// Close implements io.Closer and waits for the command to complete.
// After Close returns, ExitCode() will return the process exit code.
// If the context was canceled or timed out, exec.Cmd.Wait handles cancellation
// via the command's configured Cancel hook and WaitDelay.
// Close is safe for concurrent calls - only the first call performs cleanup.
func (r *cmdReader) Close() error {
	r.closeOnce.Do(func() {
		// Close the reader if it implements io.Closer
		if closer, ok := r.Reader.(io.Closer); ok {
			_ = closer.Close()
		}

		// Preserve cancellation for callers using plain exec.Command without a
		// custom Cancel hook. executeCommand installs cmd.Cancel, so this fallback
		// does not double-terminate normal reviewer processes.
		if r.cmd != nil && r.cmd.Process != nil {
			if r.ctx != nil && r.ctx.Err() != nil && r.cmd.Cancel == nil {
				_ = terminateProcessTree(r.cmd)
			}

			// Wait for command completion and capture exit status.
			err := r.cmd.Wait()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					r.exitCode = exitErr.ExitCode()
				} else {
					r.exitCode = -1
				}
			}
		}

		// Clean up temp file if one was created (ref-file pattern)
		CleanupTempFile(r.tempFilePath)
	})

	return nil
}

// ExitCode returns the process exit code.
// Only valid after Close() has been called. Returns 0 if process succeeded,
// -1 if process could not be waited on, or the actual exit code otherwise.
func (r *cmdReader) ExitCode() int {
	return r.exitCode
}

// Stderr returns captured stderr output.
// Only valid after Close() has been called. Returns empty string if no
// stderr was captured or if stderr buffer was not configured.
func (r *cmdReader) Stderr() string {
	if r.stderr == nil {
		return ""
	}
	return r.stderr.String()
}

// ToExecutionResult wraps this cmdReader in an ExecutionResult.
// This provides a clean API for callers without requiring type assertions.
func (r *cmdReader) ToExecutionResult() *ExecutionResult {
	return NewExecutionResult(
		r,
		func() int { return r.ExitCode() },
		func() string { return r.Stderr() },
	)
}
