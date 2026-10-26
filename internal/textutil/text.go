// Package textutil cleans external command output and formats plain text.
package textutil

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiSequence = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-_])`)

// Clean removes ANSI escapes and control characters, preserving tabs, newlines
// and printable Unicode. Terminal markup must still be escaped by the caller.
func Clean(value string) string {
	value = ansiSequence.ReplaceAllString(value, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 && !(r >= 128 && r <= 159) {
			return r
		}
		return -1
	}, value)
}

// JoinNote concatenates the non-empty diagnostics with the status separator.
func JoinNote(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, strings.TrimSpace(part))
		}
	}
	return strings.Join(kept, " · ")
}

// SkippedNote describes tolerated parser rows; "" when nothing was skipped.
func SkippedNote(skipped int, what string) string {
	if skipped <= 0 {
		return ""
	}
	return fmt.Sprintf("%d unrecognised %s skipped", skipped, what)
}

// Argv quotes arguments for display, returning a dash when none were reported.
func Argv(args []string) string {
	if len(args) == 0 {
		return "—"
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%q", arg)
	}
	return strings.Join(parts, " ")
}
