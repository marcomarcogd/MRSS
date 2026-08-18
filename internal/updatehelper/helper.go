package updatehelper

import (
	"fmt"
	"os"
	"strconv"
)

const helperFlag = "--mrss-apply-update"

type helperRequest struct {
	mode          string
	archivePath   string
	parentPID     int
	restartBinary string
}

// IsHelperInvocation reports whether args request the updater-helper mode.
// It is exported so the desktop entry point and existing update tests can
// distinguish the helper process from a normal application launch.
func IsHelperInvocation(args []string) bool {
	return len(args) == 6 && args[1] == helperFlag
}

// RunIfRequested executes updater-helper mode and returns true when the normal
// desktop application must not be started.
func RunIfRequested(args []string) bool {
	if !IsHelperInvocation(args) {
		return false
	}

	req, err := parseHelperRequest(args)
	if err != nil {
		writeUpdateLog("Invalid updater helper request: %v", err)
		return true
	}

	if err := runHelper(req); err != nil {
		writeUpdateLog("Update failed: %v", err)
		if restartErr := startApplication(req.restartBinary); restartErr != nil {
			writeUpdateLog("Failed to restart the existing application: %v", restartErr)
		}
	}
	return true
}

func parseHelperRequest(args []string) (helperRequest, error) {
	if !IsHelperInvocation(args) {
		return helperRequest{}, fmt.Errorf("invalid argument count or helper flag")
	}

	parentPID, err := strconv.Atoi(args[4])
	if err != nil || parentPID <= 0 {
		return helperRequest{}, fmt.Errorf("invalid parent process ID")
	}

	req := helperRequest{
		mode:          args[2],
		archivePath:   args[3],
		parentPID:     parentPID,
		restartBinary: args[5],
	}
	if req.mode != "installer" && req.mode != "portable" {
		return helperRequest{}, fmt.Errorf("unsupported update mode %q", req.mode)
	}
	if req.archivePath == "" || req.restartBinary == "" {
		return helperRequest{}, fmt.Errorf("update paths must not be empty")
	}
	return req, nil
}

// LaunchInstaller starts a detached helper that waits for MRSS to close,
// installs the NSIS package silently, and restarts the application.
func LaunchInstaller(installerPath string) error {
	return launchHelper("installer", installerPath)
}

// LaunchPortable starts the same helper for a Windows portable ZIP update.
func LaunchPortable(archivePath string) error {
	return launchHelper("portable", archivePath)
}

func helperArguments(mode, archivePath, restartBinary string) []string {
	return []string{helperFlag, mode, archivePath, strconv.Itoa(os.Getpid()), restartBinary}
}
