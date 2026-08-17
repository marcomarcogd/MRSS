package utils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// EnableStartup enables the application to start on system boot
func EnableStartup() error {
	if err := CleanupLegacyStartupRegistration(); err != nil {
		log.Printf("Warning: Failed to clean legacy startup registration: %v", err)
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		return enableStartupWindows(executable)
	case "linux":
		return enableStartupLinux(executable)
	case "darwin":
		return enableStartupDarwin(executable)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// DisableStartup disables the application from starting on system boot
func DisableStartup() error {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = disableStartupWindows()
	case "linux":
		err = disableStartupLinux()
	case "darwin":
		err = disableStartupDarwin()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	if err != nil {
		return err
	}
	return CleanupLegacyStartupRegistration()
}

// CleanupLegacyStartupRegistration removes startup entries created by releases
// that used the old application name and identifiers.
func CleanupLegacyStartupRegistration() error {
	switch runtime.GOOS {
	case "windows":
		return deleteStartupWindowsValue("MrRSS")
	case "linux":
		return removeStartupFile(filepath.Join(".config", "autostart", "mrrss.desktop"))
	case "darwin":
		return removeStartupFile(filepath.Join("Library", "LaunchAgents", "com.mrrss.app.plist"))
	default:
		return nil
	}
}

// Windows implementation using registry
func enableStartupWindows(executable string) error {
	// Use reg.exe to add registry entry
	cmd := exec.Command("reg", "add",
		"HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"/v", "MRSS",
		"/t", "REG_SZ",
		"/d", fmt.Sprintf("\"%s\"", executable),
		"/f")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add registry entry: %v, output: %s", err, output)
	}

	log.Printf("Startup enabled for Windows: %s", executable)
	return nil
}

func disableStartupWindows() error {
	return deleteStartupWindowsValue("MRSS")
}

func deleteStartupWindowsValue(valueName string) error {
	// Use reg.exe to remove registry entry
	cmd := exec.Command("reg", "delete",
		"HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"/v", valueName,
		"/f")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is because the key doesn't exist (exit code 1)
		// If so, we can ignore it since our goal is to have the key not present
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			log.Println("Startup registry key was not present (already disabled)")
			return nil
		}
		return fmt.Errorf("failed to remove registry entry: %v, output: %s", err, output)
	}

	log.Printf("Startup registry value removed for Windows: %s", valueName)
	return nil
}

// Linux implementation using .desktop file in autostart
func enableStartupLinux(executable string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	autostartDir := filepath.Join(homeDir, ".config", "autostart")
	if err := os.MkdirAll(autostartDir, 0755); err != nil {
		return fmt.Errorf("failed to create autostart directory: %w", err)
	}

	desktopFile := filepath.Join(autostartDir, "mrss.desktop")
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=MRSS
Exec=%s
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
`, executable)

	if err := os.WriteFile(desktopFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write desktop file: %w", err)
	}

	log.Printf("Startup enabled for Linux: %s", desktopFile)
	return nil
}

func disableStartupLinux() error {
	if err := removeStartupFile(filepath.Join(".config", "autostart", "mrss.desktop")); err != nil {
		return err
	}
	log.Println("Startup disabled for Linux")
	return nil
}

// macOS implementation using LaunchAgents plist
func enableStartupDarwin(executable string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	plistFile := filepath.Join(launchAgentsDir, "io.github.marcomarcogd.mrss.plist")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.github.marcomarcogd.mrss</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, executable)

	if err := os.WriteFile(plistFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	log.Printf("Startup enabled for macOS: %s", plistFile)
	return nil
}

func disableStartupDarwin() error {
	if err := removeStartupFile(filepath.Join("Library", "LaunchAgents", "io.github.marcomarcogd.mrss.plist")); err != nil {
		return err
	}
	log.Println("Startup disabled for macOS")
	return nil
}

func removeStartupFile(relativePath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	path := filepath.Join(homeDir, relativePath)
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove startup file %s: %w", path, err)
		}
	}
	return nil
}
