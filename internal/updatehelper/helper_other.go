//go:build !windows

package updatehelper

import "fmt"

func launchHelper(string, string) error {
	return fmt.Errorf("Windows updater helper is unavailable on this platform")
}

func runHelper(helperRequest) error {
	return fmt.Errorf("Windows updater helper is unavailable on this platform")
}

func startApplication(string) error {
	return fmt.Errorf("Windows updater helper is unavailable on this platform")
}

func writeUpdateLog(string, ...interface{}) {}
