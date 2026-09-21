//go:build windows

package utils

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"MRSS/internal/utils/fileutil"
	"golang.org/x/sys/windows/registry"
)

const windowsStartupRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func enableStartupWindows(executable string) error {
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		windowsStartupRegistryPath,
		registry.SET_VALUE,
	)
	if err != nil {
		return fmt.Errorf("failed to open startup registry key: %w", err)
	}
	defer key.Close()

	command := `"` + executable + `" --start-minimized`
	if dir := fileutil.CustomDataDir(); dir != "" && !fileutil.DesktopStorageManaged() {
		// Windows paths cannot contain quotes; double trailing slashes before the
		// closing quote, notably when the chosen directory is a drive root.
		trimmed := strings.TrimRight(dir, `\`)
		command += ` --data-dir "` + trimmed + strings.Repeat(`\`, 2*(len(dir)-len(trimmed))) + `"`
	}
	if err := key.SetStringValue("MRSS", command); err != nil {
		return fmt.Errorf("failed to set startup registry value: %w", err)
	}

	log.Printf("Startup enabled for Windows: %s", executable)
	return nil
}

func disableStartupWindows() error {
	return deleteStartupWindowsValue("MRSS")
}

func deleteStartupWindowsValue(valueName string) error {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		windowsStartupRegistryPath,
		registry.SET_VALUE,
	)
	if errors.Is(err, registry.ErrNotExist) {
		log.Printf("Startup registry value was not present (already disabled): %s", valueName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to open startup registry key: %w", err)
	}
	defer key.Close()

	if err := key.DeleteValue(valueName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			log.Printf("Startup registry value was not present (already disabled): %s", valueName)
			return nil
		}
		return fmt.Errorf("failed to remove startup registry value %s: %w", valueName, err)
	}

	log.Printf("Startup registry value removed for Windows: %s", valueName)
	return nil
}
