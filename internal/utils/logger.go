package utils

import (
	"log"
	"os"
)

const (
	DebugEnv       = "MRSS_DEBUG"
	LegacyDebugEnv = "MRRSS_DEBUG"
)

// EnvValue returns the canonical environment variable value and falls back to
// the legacy name when upgrading from older MRSS releases.
func EnvValue(name, legacyName string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return os.Getenv(legacyName)
}

var debugLogging = EnvValue(DebugEnv, LegacyDebugEnv) != ""

func DebugLog(format string, args ...interface{}) {
	if debugLogging {
		log.Printf(format, args...)
	}
}
