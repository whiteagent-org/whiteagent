package util

import (
	"fmt"
	"time"
)

// FormatSize returns a human-readable file size string.
// Thresholds: <1024 = "N B", <1MB = "N KB", <1GB = "N.N MB", else "N.N GB".
func FormatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%d KB", bytes/kb)
	case bytes < gb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	default:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	}
}

// FormatTimestampUTC returns an ISO 8601 / RFC3339 UTC timestamp string.
// Returns "" for the zero time.
func FormatTimestampUTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
