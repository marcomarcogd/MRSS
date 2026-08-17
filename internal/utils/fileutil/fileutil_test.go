package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolateTestDataHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	isServerMode = false

	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", root)
		t.Setenv("USERPROFILE", "")
		return root
	case "darwin":
		t.Setenv("HOME", root)
		return filepath.Join(root, "Library", "Application Support")
	case "linux":
		t.Setenv("XDG_DATA_HOME", root)
		t.Setenv("HOME", root)
		return root
	default:
		t.Setenv("HOME", root)
		return filepath.Join(root, ".config")
	}
}

func TestGetDataDir(t *testing.T) {
	isolateTestDataHome(t)
	// Test that GetDataDir returns a valid path
	dir, err := GetDataDir()
	if err != nil {
		t.Fatalf("GetDataDir failed: %v", err)
	}

	if dir == "" {
		t.Error("GetDataDir returned empty string")
	}

	// Check that directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("Data directory does not exist: %s", dir)
	}

	// Check that it ends with MRSS (in normal mode) or data (in portable mode)
	if !IsPortableMode() && !strings.HasSuffix(dir, "MRSS") {
		t.Errorf("Expected path to end with MRSS in normal mode, got: %s", dir)
	}
	if IsPortableMode() && !strings.HasSuffix(dir, "data") {
		t.Errorf("Expected path to end with data in portable mode, got: %s", dir)
	}
}

func TestGetDBPath(t *testing.T) {
	isolateTestDataHome(t)
	path, err := GetDBPath()
	if err != nil {
		t.Fatalf("GetDBPath failed: %v", err)
	}

	if path == "" {
		t.Error("GetDBPath returned empty string")
	}

	// Check that it ends with rss.db
	if !strings.HasSuffix(path, "rss.db") {
		t.Errorf("Expected path to end with rss.db, got: %s", path)
	}

	// Check that parent directory exists
	parentDir := filepath.Dir(path)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		t.Errorf("Parent directory does not exist: %s", parentDir)
	}
}

func TestGetLogPath(t *testing.T) {
	isolateTestDataHome(t)
	path, err := GetLogPath()
	if err != nil {
		t.Fatalf("GetLogPath failed: %v", err)
	}

	if path == "" {
		t.Error("GetLogPath returned empty string")
	}

	// Check that it ends with debug.log
	if !strings.HasSuffix(path, "debug.log") {
		t.Errorf("Expected path to end with debug.log, got: %s", path)
	}
}

func TestGetDataDir_PlatformSpecific(t *testing.T) {
	root := t.TempDir()
	isServerMode = false
	// Test platform-specific behavior
	switch runtime.GOOS {
	case "windows":
		// On Windows, should use APPDATA or USERPROFILE
		originalAppData := os.Getenv("APPDATA")
		originalUserProfile := os.Getenv("USERPROFILE")
		defer func() {
			os.Setenv("APPDATA", originalAppData)
			os.Setenv("USERPROFILE", originalUserProfile)
		}()

		// Test with APPDATA set
		os.Setenv("APPDATA", root)
		dir, err := GetDataDir()
		if err != nil {
			t.Fatalf("GetDataDir failed: %v", err)
		}
		if !strings.HasPrefix(dir, root) {
			t.Errorf("Expected path to start with %s, got: %s", root, dir)
		}

	case "darwin":
		// On macOS, should use HOME/Library/Application Support
		originalHome := os.Getenv("HOME")
		defer os.Setenv("HOME", originalHome)

		// Use tmp to avoid permission issues
		os.Setenv("HOME", root)
		dir, err := GetDataDir()
		if err != nil {
			t.Fatalf("GetDataDir failed: %v", err)
		}
		expected := filepath.Join(root, "Library", "Application Support", "MRSS")
		if dir != expected {
			t.Errorf("Expected %s, got %s", expected, dir)
		}

	case "linux":
		// On Linux, should use XDG_DATA_HOME or HOME/.local/share
		originalXDG := os.Getenv("XDG_DATA_HOME")
		originalHome := os.Getenv("HOME")
		defer func() {
			os.Setenv("XDG_DATA_HOME", originalXDG)
			os.Setenv("HOME", originalHome)
		}()

		os.Setenv("XDG_DATA_HOME", root)
		dir, err := GetDataDir()
		if err != nil {
			t.Fatalf("GetDataDir failed: %v", err)
		}
		if !strings.HasPrefix(dir, root) {
			t.Errorf("Expected path to start with %s, got: %s", root, dir)
		}
	}
}

func TestResolveNormalDataDirMigratesLegacyDirectory(t *testing.T) {
	baseDir := t.TempDir()
	legacyDir := filepath.Join(baseDir, legacyDataDirName)
	if err := os.MkdirAll(filepath.Join(legacyDir, "media_cache"), 0755); err != nil {
		t.Fatal(err)
	}
	databaseContents := []byte("sqlite-database-snapshot")
	if err := os.WriteFile(filepath.Join(legacyDir, "rss.db"), databaseContents, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "media_cache", "cached.bin"), []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, legacyMonitorIDFileName), []byte("legacy-device-id"), 0644); err != nil {
		t.Fatal(err)
	}

	selectedDir := resolveNormalDataDir(baseDir)
	newDir := filepath.Join(baseDir, dataDirName)
	if selectedDir != newDir {
		t.Fatalf("expected migrated directory %s, got %s", newDir, selectedDir)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy directory should have been atomically renamed, stat error: %v", err)
	}
	gotDatabase, err := os.ReadFile(filepath.Join(newDir, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotDatabase) != string(databaseContents) {
		t.Fatalf("database contents changed during migration")
	}
	if _, err := os.Stat(filepath.Join(newDir, "media_cache", "cached.bin")); err != nil {
		t.Fatalf("nested cache was not migrated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, legacyMonitorIDFileName)); !os.IsNotExist(err) {
		t.Fatalf("legacy analytics identifier should be removed, stat error: %v", err)
	}
}

func TestResolveNormalDataDirFallsBackWhenRenameFails(t *testing.T) {
	baseDir := t.TempDir()
	legacyDir := filepath.Join(baseDir, legacyDataDirName)
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "rss.db"), []byte("preserved"), 0644); err != nil {
		t.Fatal(err)
	}

	originalRename := renameDataDir
	renameDataDir = func(string, string) error { return errors.New("simulated rename failure") }
	t.Cleanup(func() { renameDataDir = originalRename })

	selectedDir := resolveNormalDataDir(baseDir)
	if selectedDir != legacyDir {
		t.Fatalf("expected fallback to %s, got %s", legacyDir, selectedDir)
	}
	contents, err := os.ReadFile(filepath.Join(legacyDir, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "preserved" {
		t.Fatalf("legacy database changed after failed migration")
	}
}

func TestResolveNormalDataDirPrefersNewDatabaseWhenBothExist(t *testing.T) {
	baseDir := t.TempDir()
	newDir := filepath.Join(baseDir, dataDirName)
	legacyDir := filepath.Join(baseDir, legacyDataDirName)
	for _, dir := range []string{newDir, legacyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(newDir, "rss.db"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "rss.db"), []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, legacyMonitorIDFileName), []byte("leave-legacy-dir-unchanged"), 0644); err != nil {
		t.Fatal(err)
	}

	if selectedDir := resolveNormalDataDir(baseDir); selectedDir != newDir {
		t.Fatalf("expected new directory %s, got %s", newDir, selectedDir)
	}
	legacyContents, err := os.ReadFile(filepath.Join(legacyDir, "rss.db"))
	if err != nil || string(legacyContents) != "legacy" {
		t.Fatalf("legacy database should remain unchanged: contents=%q err=%v", legacyContents, err)
	}
	if _, err := os.Stat(filepath.Join(legacyDir, legacyMonitorIDFileName)); err != nil {
		t.Fatalf("legacy directory should remain untouched when both databases exist: %v", err)
	}
}
