//go:build !windows

package utils

import "os/exec"

// ConfigureBackgroundCommand is a no-op outside Windows.
func ConfigureBackgroundCommand(*exec.Cmd) {}
