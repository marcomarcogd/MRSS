//go:build !windows

package utils

import "errors"

func enableStartupWindows(string) error {
	return errors.New("Windows startup registration is unavailable on this platform")
}

func disableStartupWindows() error {
	return errors.New("Windows startup registration is unavailable on this platform")
}

func deleteStartupWindowsValue(string) error {
	return errors.New("Windows startup registration is unavailable on this platform")
}
