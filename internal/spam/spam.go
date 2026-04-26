// Package spam is a tiny, admin-editable banned-words check for public
// comment submissions. The goal is not to be a sophisticated classifier but
// to catch the obvious drive-by noise (cialis, viagra, casino, etc.) that
// hand-writing a list of patterns is good enough for.
package spam

import "strings"

// ParseWords turns a newline-separated list (as stored in weblogs.spam_words)
// into a slice of trimmed, lower-cased tokens, skipping empty lines and
// comments starting with "#" so the admin can annotate their list.
func ParseWords(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.ToLower(l))
	}
	return out
}

// MatchesAny reports whether any of the banned words appears as a case-
// insensitive substring of any of the fields. Empty word list == never match.
func MatchesAny(fields []string, words []string) bool {
	if len(words) == 0 {
		return false
	}
	joined := strings.ToLower(strings.Join(fields, "\n"))
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.Contains(joined, w) {
			return true
		}
	}
	return false
}
