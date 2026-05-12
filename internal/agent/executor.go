package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// maxStderrSize is the maximum bytes captured from agent subprocess stderr.
// Prevents unbounded memory growth from misbehaving CLI tools.
const maxStderrSize = 1 << 20 // 1MB

// cmdWaitDelay bounds how long Wait may block after cancellation before the
// standard library closes pipes and force-kills the root process.
const cmdWaitDelay = 5 * time.Second

// stderrBuffer is the interface for stderr capture buffers.
type stderrBuffer interface {
	io.Writer
	String() string
}

// cappedBuffer is a bytes.Buffer that stops writing after a size limit.
// Once the limit is reached, further writes are silently discarded.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {
		// Silently discard — report full length to avoid io.ErrShortWrite
		return len(p), nil
	}
	if len(p) > remaining {
		// Write what we can, but report full length consumed
		c.buf.Write(p[:remaining])
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) Bytes() []byte {
	return c.buf.Bytes()
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}

// executeOptions configures command execution for agent CLI invocations.
type executeOptions struct {
	// Command is the CLI executable name (e.g., "codex", "claude", "gemini").
	Command string
	// Args are the command-line arguments.
	Args []string
	// Stdin provides input to the command (typically the prompt).
	Stdin io.Reader
	// WorkDir sets the working directory for the command.
	WorkDir string
	// Env adds or overrides environment variables for this command.
	Env map[string]string
	// TempFilePath is a temp file to clean up on Close (used by ref-file pattern).
	TempFilePath string
}

// executeCommand runs a CLI command with platform-specific process handling.
// This is the shared implementation used by all agent ExecuteReview/ExecuteSummary methods.
//
// It handles:
//   - Applying platform-specific process configuration
//   - Installing cancellation hooks for process tree cleanup
//   - Capturing stderr for error diagnostics
//   - Creating stdout pipe for streaming output
//   - Starting the command and returning a managed ExecutionResult
//   - Cleaning up temp files on error or when the result is closed
func executeCommand(ctx context.Context, opts executeOptions) (*ExecutionResult, error) {
	// #nosec G204 - Command is always one of the known agent CLIs (codex, claude, gemini)
	// passed from trusted code in the agent implementations, not user input.
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)
	if len(opts.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), opts.Env)
	}

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	configureCmdForPlatform(cmd)
	cmd.Cancel = func() error {
		return terminateProcessTree(cmd)
	}
	cmd.WaitDelay = cmdWaitDelay

	// Capture stderr for error diagnostics (capped to prevent unbounded memory)
	stderr := newCappedBuffer(maxStderrSize)
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to start %s: %w", opts.Command, err)
	}

	reader := &cmdReader{
		Reader:       stdout,
		cmd:          cmd,
		ctx:          ctx,
		stderr:       stderr,
		tempFilePath: opts.TempFilePath,
	}

	return reader.ToExecutionResult(), nil
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]int, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		norm := normalizeEnvKey(key)
		seen[norm] = len(out)
		out = append(out, entry)
	}

	for key, value := range overrides {
		if key == "" {
			continue
		}
		entry := key + "=" + value
		norm := normalizeEnvKey(key)
		if idx, ok := seen[norm]; ok {
			out[idx] = entry
			continue
		}
		seen[norm] = len(out)
		out = append(out, entry)
	}
	return out
}

func normalizeEnvKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}
