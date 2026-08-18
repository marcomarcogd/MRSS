//go:build windows

package utils

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ConfigureBackgroundCommand prevents console-based child processes from
// creating a visible window while preserving their stdout and stderr pipes.
func ConfigureBackgroundCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

// ConfigureGUICommand starts a GUI child process without inheriting a console
// while keeping the child window visible and independent from the parent.
func ConfigureGUICommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = false
	cmd.SysProcAttr.CreationFlags &^= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.CreationFlags |= windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
}
