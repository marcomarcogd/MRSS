package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"MRSS/internal/updatehelper"
	appUtils "MRSS/internal/utils"
)

func TestUpdaterHelperInvocationDetection(t *testing.T) {
	args := []string{"MRSS-update-helper.exe", "--mrss-apply-update", "installer", `C:\Temp\MRSS-Setup.exe`, "1234", `C:\Program Files\MRSS\MRSS.exe`}
	if !updatehelper.IsHelperInvocation(args) {
		t.Fatal("valid updater helper invocation was not detected")
	}
	if updatehelper.IsHelperInvocation(args[:5]) {
		t.Fatal("incomplete updater helper invocation must be rejected")
	}
}

func TestWindowsGUICommandKeepsInstallerVisible(t *testing.T) {
	cmd := exec.Command(`C:\Temp\MRSS-Setup.exe`)
	appUtils.ConfigureGUICommand(cmd)

	// On Windows, the GUI installer must not inherit the background-process
	// flags used for scripts, otherwise NSIS starts with its window hidden.
	if cmd.SysProcAttr != nil {
		attr := reflect.ValueOf(cmd.SysProcAttr).Elem()
		if hideWindow := attr.FieldByName("HideWindow"); hideWindow.IsValid() && hideWindow.Bool() {
			t.Fatal("Windows installer command must keep its GUI window visible")
		}
		if creationFlags := attr.FieldByName("CreationFlags"); creationFlags.IsValid() {
			const createNoWindow = uint64(0x08000000)
			if creationFlags.Uint()&createNoWindow != 0 {
				t.Fatal("Windows installer command must not use CREATE_NO_WINDOW")
			}
		}
	}
	if !reflect.DeepEqual(cmd.Args, []string{`C:\Temp\MRSS-Setup.exe`}) {
		t.Fatalf("GUI command unexpectedly uses a shell: %#v", cmd.Args)
	}
}

// Test copyFile function
func TestCopyFile(t *testing.T) {
	// Create a temporary source file
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	// Write test content
	testContent := []byte("test content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test copy
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("Content mismatch: got %s, want %s", content, testContent)
	}
}
