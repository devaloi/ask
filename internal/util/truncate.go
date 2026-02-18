// Package util provides shared utility functions.
package util

import "strings"

// Truncate truncates s to maxLen characters, appending "..." if truncated.
// It also normalizes whitespace (collapses newlines and runs of spaces).
func Truncate(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen-3] + "..."
}
