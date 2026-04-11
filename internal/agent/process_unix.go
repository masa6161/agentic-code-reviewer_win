//go:build !windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCmdForPlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Process == nil {
		return os.ErrProcessDone
	}

	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	target := -pid
	if pgid != pid {
		target = pid
	}

	err = syscall.Kill(target, syscall.SIGKILL)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
