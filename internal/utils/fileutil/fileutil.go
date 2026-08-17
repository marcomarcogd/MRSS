// Package fileutil provides file system utilities including path management,
// directory operations, and platform-specific path handling.
package fileutil

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	isPortableMode   bool
	portableModeOnce sync.Once
	isServerMode     bool
	renameDataDir    = os.Rename
)

const (
	dataDirName             = "MRSS"
	legacyDataDirName       = "MrRSS"
	legacyMonitorIDFileName = "monitor_device_id.txt"
)

// SetServerMode sets the server mode flag.
func SetServerMode(v bool) {
	isServerMode = v
}

// IsServerMode returns true if running in server mode.
func IsServerMode() bool {
	return isServerMode
}

// IsPortableMode checks if the application is running in portable mode.
func IsPortableMode() bool {
	portableModeOnce.Do(func() {
		exePath, err := os.Executable()
		if err != nil {
			isPortableMode = false
			return
		}

		exeDir := filepath.Dir(exePath)
		portableMarker := filepath.Join(exeDir, "portable.txt")

		_, err = os.Stat(portableMarker)
		isPortableMode = err == nil
	})
	return isPortableMode
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func cleanupLegacyMonitorID(dataDir string) {
	path := filepath.Join(dataDir, legacyMonitorIDFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: Failed to remove legacy analytics identifier %s: %v", path, err)
	}
}

func resolveNormalDataDir(baseDir string) string {
	newDir := filepath.Join(baseDir, dataDirName)
	legacyDir := filepath.Join(baseDir, legacyDataDirName)
	newDBExists := isRegularFile(filepath.Join(newDir, "rss.db"))
	legacyDBExists := isRegularFile(filepath.Join(legacyDir, "rss.db"))

	switch {
	case newDBExists && legacyDBExists:
		log.Printf("Warning: Both MRSS and legacy MrRSS data directories contain rss.db; using %s and leaving %s unchanged", newDir, legacyDir)
		cleanupLegacyMonitorID(newDir)
		return newDir
	case newDBExists:
		cleanupLegacyMonitorID(newDir)
		return newDir
	case legacyDBExists:
		if err := renameDataDir(legacyDir, newDir); err == nil {
			log.Printf("Migrated legacy MrRSS data directory to %s", newDir)
			cleanupLegacyMonitorID(newDir)
			return newDir
		} else {
			log.Printf("Warning: Failed to migrate legacy MrRSS data directory to %s: %v; continuing with %s", newDir, err, legacyDir)
			cleanupLegacyMonitorID(legacyDir)
			return legacyDir
		}
	default:
		cleanupLegacyMonitorID(newDir)
		return newDir
	}
}

// GetDataDir returns the platform-specific user data directory for MRSS.
func GetDataDir() (string, error) {
	var dataDir string
	var err error

	if IsServerMode() {
		return "./data", nil
	}

	if IsPortableMode() {
		exePath, err := os.Executable()
		if err != nil {
			return "", err
		}
		exeDir := filepath.Dir(exePath)
		dataDir = filepath.Join(exeDir, "data")
	} else {
		var baseDir string

		switch runtime.GOOS {
		case "windows":
			baseDir = os.Getenv("APPDATA")
			if baseDir == "" {
				baseDir = os.Getenv("USERPROFILE")
				if baseDir != "" {
					baseDir = filepath.Join(baseDir, "AppData", "Roaming")
				}
			}
		case "darwin":
			baseDir = os.Getenv("HOME")
			if baseDir != "" {
				baseDir = filepath.Join(baseDir, "Library", "Application Support")
			}
		case "linux":
			baseDir = os.Getenv("XDG_DATA_HOME")
			if baseDir == "" {
				homeDir := os.Getenv("HOME")
				if homeDir != "" {
					baseDir = filepath.Join(homeDir, ".local", "share")
				}
			}
		default:
			baseDir = os.Getenv("HOME")
			if baseDir != "" {
				baseDir = filepath.Join(baseDir, ".config")
			}
		}

		if baseDir == "" {
			baseDir, err = os.Getwd()
			if err != nil {
				return "", err
			}
		}

		dataDir = resolveNormalDataDir(baseDir)
	}

	err = os.MkdirAll(dataDir, 0755)
	if err != nil {
		return "", err
	}

	return dataDir, nil
}

// GetDBPath returns the full path to the database file.
func GetDBPath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "rss.db"), nil
}

// GetLogPath returns the full path to the debug log file.
func GetLogPath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}

	logsDir := filepath.Join(dataDir, "logs")
	err = os.MkdirAll(logsDir, 0755)
	if err != nil {
		return "", err
	}

	return filepath.Join(logsDir, "debug.log"), nil
}

// GetMediaCacheDir returns the full path to the media cache directory.
func GetMediaCacheDir() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dataDir, "media_cache")
	err = os.MkdirAll(cacheDir, 0755)
	if err != nil {
		return "", err
	}
	return cacheDir, nil
}

// GetScriptsDir returns the path to the scripts directory.
func GetScriptsDir() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	scriptsDir := filepath.Join(dataDir, "scripts")
	err = os.MkdirAll(scriptsDir, 0755)
	if err != nil {
		return "", err
	}
	return scriptsDir, nil
}

// ValidateScriptPath validates that a script path is within the scripts directory.
func ValidateScriptPath(scriptPath string) (string, error) {
	scriptsDir, err := GetScriptsDir()
	if err != nil {
		return "", err
	}

	absScriptPath := filepath.Join(scriptsDir, scriptPath)
	absScriptPath = filepath.Clean(absScriptPath)

	cleanScriptsDir := filepath.Clean(scriptsDir) + string(filepath.Separator)

	if !strings.HasPrefix(absScriptPath+string(filepath.Separator), cleanScriptsDir) &&
		!strings.HasPrefix(absScriptPath, cleanScriptsDir) {
		return "", os.ErrPermission
	}

	if _, err := os.Stat(absScriptPath); os.IsNotExist(err) {
		return "", os.ErrNotExist
	}

	return absScriptPath, nil
}

// IsWindows returns true if the current platform is Windows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsMacOS returns true if the current platform is MacOS (Darwin).
func IsMacOS() bool {
	return runtime.GOOS == "darwin"
}

// IsLinux returns true if the current platform is Linux.
func IsLinux() bool {
	return runtime.GOOS == "linux"
}
