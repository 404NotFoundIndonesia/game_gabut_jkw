package sanitize

import (
	"strings"
	"unicode"
)

// String trims leading/trailing whitespace and strips all control characters
// (including null bytes) from s. The result is safe for storage and display.
func String(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Keep printable characters and common whitespace (space, tab, newline).
		// Reject everything else in the control character range (U+0000–U+001F, U+007F).
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Truncate caps s to maxLen runes. If s exceeds maxLen it is sliced at the
// rune boundary and trailing whitespace is trimmed from the result.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return strings.TrimSpace(string(runes[:maxLen]))
}

// DisplayName sanitizes and length-caps a player display name (max 100 chars).
func DisplayName(s string) string {
	return Truncate(String(s), 100)
}

// Reason sanitizes a free-text reason string (max 500 chars).
func Reason(s string) string {
	return Truncate(String(s), 500)
}

// Word sanitizes a word submitted by a player (max 200 chars, single-line).
func Word(s string) string {
	// Words must be single tokens — strip internal newlines/tabs too.
	cleaned := String(s)
	// Remove embedded newlines/tabs that survived String().
	cleaned = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, cleaned)
	return Truncate(cleaned, 200)
}
