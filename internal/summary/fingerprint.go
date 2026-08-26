package summary

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// CacheFingerprint identifies the article material and AI configuration that
// produced a cached summary. It contains no credentials or custom headers.
func CacheFingerprint(content, length, profile, endpoint, model string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(content),
		strings.TrimSpace(length),
		strings.TrimSpace(profile),
		strings.TrimSpace(endpoint),
		strings.TrimSpace(model),
	}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", digest[:])
}

// ContentFingerprint changes only when the source material changes, allowing
// compatible AI summaries to be reused across article and digest entrypoints.
func ContentFingerprint(content string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("%x", digest[:])
}
