//go:build windows

package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/windows"
)

func configureCmdForPlatform(cmd *exec.Cmd) {}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.Process == nil {
		return os.ErrProcessDone
	}

	pid := strconv.Itoa(cmd.Process.Pid)
	taskkill := exec.Command("taskkill", "/T", "/F", "/PID", pid)
	if err := taskkill.Run(); err == nil {
		return nil
	}
	if exited, err := processExited(cmd.Process.Pid); err == nil && exited {
		return os.ErrProcessDone
	}

	killErr := cmd.Process.Kill()
	if killErr == nil {
		return nil
	}
	if errors.Is(killErr, os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	if exited, err := processExited(cmd.Process.Pid); err == nil && exited {
		return os.ErrProcessDone
	}
	return killErr
}

func processExited(pid int) (bool, error) {
	access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION) | uint32(windows.SYNCHRONIZE)
	handle, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return true, nil
		}
		return false, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	waitResult, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	if waitResult == uint32(windows.WAIT_OBJECT_0) {
		return true, nil
	}
	if waitResult == uint32(windows.WAIT_TIMEOUT) {
		return false, nil
	}

	return false, fmt.Errorf("unexpected wait result: %d", waitResult)
}
