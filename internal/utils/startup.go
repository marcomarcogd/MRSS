package utils

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"MRSS/internal/utils/fileutil"
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
		return removeStartupFile(filepath.Join(".config", "autostart", "mrss.desktop"))
	case "darwin":
		return removeStartupFile(filepath.Join("Library", "LaunchAgents", "com.mrrss.app.plist"))
	default:
		return nil
	}
}

// Linux implementation using .desktop file in autostart
func enableStartupLinux(executable string) error {
	// APPIMAGE identifies the persistent outer file, not the transient mount.
	if appImage := os.Getenv("APPIMAGE"); appImage != "" {
		executable = appImage
	}
	execValue, err := desktopExec(executable)
	if err != nil {
		return err
	}
	execValue += " --start-minimized"
	if dir := fileutil.CustomDataDir(); dir != "" && !fileutil.DesktopStorageManaged() {
		quotedDir, err := desktopExec(dir)
		if err != nil {
			return err
		}
		execValue += " --data-dir " + quotedDir
	}
	autostartDir, err := linuxAutostartDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(autostartDir, 0755); err != nil {
		return fmt.Errorf("failed to create autostart directory: %w", err)
	}

	desktopFile := filepath.Join(autostartDir, "mrrss.desktop")
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=MRSS
Exec=%s
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
`, execValue)

	if err := os.WriteFile(desktopFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write desktop file: %w", err)
	}

	log.Printf("Startup enabled for Linux: %s", desktopFile)
	return nil
}

func disableStartupLinux() error {
	autostartDir, err := linuxAutostartDir()
	if err != nil {
		return err
	}

	desktopFile := filepath.Join(autostartDir, "mrrss.desktop")
	if err := os.Remove(desktopFile); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove desktop file: %w", err)
		}
	}
	log.Println("Startup disabled for Linux")
	return nil
}

func linuxAutostartDir() (string, error) {
	if configDir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(configDir) {
		return filepath.Join(configDir, "autostart"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "autostart"), nil
}

// desktopExec quotes one executable according to the Desktop Entry Exec grammar.
// Escape argument quoting first, then the desktop file's string escaping layer.
func desktopExec(executable string) (string, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\x00\r\n") {
		return "", fmt.Errorf("startup executable must be an absolute path without line breaks")
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`, "%", "%%").Replace(executable)
	escaped = strings.NewReplacer(`\`, `\\`, "\t", `\t`).Replace(escaped)
	return `"` + escaped + `"`, nil
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
		%s
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, startupDarwinArguments(executable))

	if err := os.WriteFile(plistFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	log.Printf("Startup enabled for macOS: %s", plistFile)
	return nil
}

func startupDarwinArguments(executable string) string {
	args := "<string>" + html.EscapeString(executable) + "</string>\n\t\t<string>--start-minimized</string>"
	if dir := fileutil.CustomDataDir(); dir != "" && !fileutil.DesktopStorageManaged() {
		args += "\n\t\t<string>--data-dir</string>\n\t\t<string>" + html.EscapeString(dir) + "</string>"
	}
	return args
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
