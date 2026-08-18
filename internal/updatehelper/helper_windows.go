//go:build windows

package updatehelper

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	appUtils "MRSS/internal/utils"
	"MRSS/internal/utils/fileutil"

	"golang.org/x/sys/windows"
)

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	waitForUpdateTimeout  = 15 * time.Minute
)

var shellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

func launchHelper(mode, archivePath string) error {
	currentExecutable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get current executable: %w", err)
	}

	if err := validateUpdateArchive(archivePath, mode); err != nil {
		return err
	}
	cleanupStaleHelpers()

	helperDir, err := os.MkdirTemp("", "mrss-update-helper-")
	if err != nil {
		return fmt.Errorf("create updater helper directory: %w", err)
	}
	helperPath := filepath.Join(helperDir, "MRSS-update-helper.exe")
	if err := copyFile(currentExecutable, helperPath); err != nil {
		_ = os.RemoveAll(helperDir)
		return fmt.Errorf("prepare updater helper: %w", err)
	}

	cmd := exec.Command(helperPath, helperArguments(mode, archivePath, currentExecutable)...)
	appUtils.ConfigureGUICommand(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(helperDir)
		return fmt.Errorf("start updater helper: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		writeUpdateLog("Failed to release updater helper process handle: %v", err)
	}
	return nil
}

func runHelper(req helperRequest) error {
	configureUpdateLog()
	writeUpdateLog("Updater helper started in %s mode", req.mode)
	defer cleanupDownloadedArchive(req.archivePath)

	if err := waitForProcess(req.parentPID); err != nil {
		return fmt.Errorf("wait for MRSS to exit: %w", err)
	}

	var err error
	switch req.mode {
	case "installer":
		err = runElevatedInstaller(req.archivePath)
	case "portable":
		err = applyPortableUpdate(req.archivePath, req.restartBinary)
	default:
		err = fmt.Errorf("unsupported update mode %q", req.mode)
	}
	if err != nil {
		return err
	}

	if err := startApplication(req.restartBinary); err != nil {
		return fmt.Errorf("restart updated MRSS: %w", err)
	}
	writeUpdateLog("Update completed and MRSS restarted successfully")
	return nil
}

func waitForProcess(pid int) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	result, err := windows.WaitForSingleObject(handle, uint32(waitForUpdateTimeout/time.Millisecond))
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("timed out waiting for process %d", pid)
	}
	return nil
}

func runElevatedInstaller(installerPath string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(installerPath)
	if err != nil {
		return fmt.Errorf("encode installer path: %w", err)
	}
	parameters, _ := windows.UTF16PtrFromString("/S")

	info := shellExecuteInfo{
		// ShellExecuteEx must finish creating the elevated process before it
		// returns because the helper immediately waits on the process handle.
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		nShow:        windows.SW_SHOWNORMAL,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	result, _, callErr := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return fmt.Errorf("launch elevated installer: %w", callErr)
	}
	if info.hProcess == 0 {
		return fmt.Errorf("installer process handle was not returned")
	}
	defer windows.CloseHandle(info.hProcess)

	waitResult, err := windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait for installer: %w", err)
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected installer wait result: %d", waitResult)
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &exitCode); err != nil {
		return fmt.Errorf("read installer exit code: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("installer exited with code %d", exitCode)
	}
	return nil
}

func applyPortableUpdate(archivePath, targetExecutable string) error {
	extractDir, err := os.MkdirTemp("", "mrss-portable-update-")
	if err != nil {
		return fmt.Errorf("create portable extraction directory: %w", err)
	}
	defer os.RemoveAll(extractDir)

	newExecutable, err := extractPortableExecutable(archivePath, extractDir, filepath.Base(targetExecutable))
	if err != nil {
		return err
	}

	backupPath := targetExecutable + ".backup"
	_ = os.Remove(backupPath)
	if err := os.Rename(targetExecutable, backupPath); err != nil {
		return fmt.Errorf("back up portable executable: %w", err)
	}
	if err := copyFile(newExecutable, targetExecutable); err != nil {
		if restoreErr := os.Rename(backupPath, targetExecutable); restoreErr != nil {
			return fmt.Errorf("replace portable executable: %w (restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace portable executable: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		writeUpdateLog("Failed to remove portable backup: %v", err)
	}
	return nil
}

func extractPortableExecutable(archivePath, destination, executableName string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open portable archive: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Base(file.Name), executableName) {
			continue
		}
		targetPath := filepath.Join(destination, executableName)
		input, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open portable executable: %w", err)
		}
		output, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			input.Close()
			return "", fmt.Errorf("create staged portable executable: %w", err)
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		input.Close()
		if copyErr != nil {
			return "", fmt.Errorf("stage portable executable: %w", copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close staged portable executable: %w", closeErr)
		}
		return targetPath, nil
	}
	return "", fmt.Errorf("portable archive does not contain %s", executableName)
}

func startApplication(path string) error {
	cmd := exec.Command(path)
	appUtils.ConfigureGUICommand(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func validateUpdateArchive(path, mode string) error {
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(os.TempDir(), cleanPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("update archive is outside the temporary directory")
	}
	wantedExtension := ".exe"
	if mode == "portable" {
		wantedExtension = ".zip"
	}
	if !strings.HasSuffix(strings.ToLower(cleanPath), wantedExtension) {
		return fmt.Errorf("invalid %s update archive type", mode)
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Errorf("stat update archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("update archive is not a regular file")
	}
	return nil
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	return target.Close()
}

func cleanupDownloadedArchive(path string) {
	directory := filepath.Dir(path)
	if strings.HasPrefix(filepath.Base(directory), "mrss-update-") {
		if err := os.RemoveAll(directory); err != nil {
			writeUpdateLog("Failed to remove update download directory: %v", err)
		}
	}
}

func cleanupStaleHelpers() {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "mrss-update-helper-*"))
	cutoff := time.Now().Add(-time.Hour)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(match)
		}
	}
}

func configureUpdateLog() {
	appLogPath, err := fileutil.GetLogPath()
	if err != nil {
		return
	}
	updateLogPath := filepath.Join(filepath.Dir(appLogPath), "update.log")
	if err := os.MkdirAll(filepath.Dir(updateLogPath), 0755); err != nil {
		return
	}
	file, err := os.OpenFile(updateLogPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(file)
	}
}

func writeUpdateLog(format string, args ...interface{}) {
	log.Printf("[Updater] "+format, args...)
}
