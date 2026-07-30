//go:build windows

// Package client provides functionality for the client subsystem.
package client

import (
	"os/exec"
	"strconv"
	"syscall"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return killPIDGroup(cmd.Process.Pid)
}

func killPIDGroup(pid int) error {
	killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	_ = killCmd.Run()
	return nil
}
