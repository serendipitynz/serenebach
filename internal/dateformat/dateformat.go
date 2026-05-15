// Package dateformat expands SB3-style date format strings — the
// %Year%.%Mon%.%Day% %WeekLong% notation weblog authors configure in
// the design-settings screen — into rendered text.
//
// Token set and semantics track the SB3 template_ja.html reference
// table as closely as feasible given Go's time.Time API. Locale-dependent
// tokens (month / weekday names, day ordinals) honour the weblog's
// Lang field: "ja" for Japanese, everything else falls through to
// English (the SB3 default).
package dateformat

import (
	"fmt"
	"strings"
	"time"
)

// Expand replaces every %Token% occurrence in pattern with the matching
// rendering of t. Unknown tokens are left in place (literal "%Xxx%")
// so a typo in the template is visible rather than silently dropped.
// An empty pattern returns an empty string — callers treat that as
// "use the default" in their own layer.
func Expand(pattern string, t time.Time, lang string) string {
	if pattern == "" || t.IsZero() {
		return ""
	}
	var b strings.Builder
	b.Grow(len(pattern))
	i := 0
	for i < len(pattern) {
		// Token form: %Alnum+%. Anything else is a literal %.
		if pattern[i] == '%' {
			end := indexTokenEnd(pattern, i+1)
			if end > i+1 && pattern[end] == '%' {
				name := pattern[i+1 : end]
				if val, ok := token(name, t, lang); ok {
					b.WriteString(val)
					i = end + 1
					continue
				}
				// Unknown — keep the raw "%name%" in output.
			}
		}
		b.WriteByte(pattern[i])
		i++
	}
	return b.String()
}

// indexTokenEnd scans forward from start expecting [A-Za-z0-9]+, returns
// the index of the terminating '%' or the first non-matching byte.
func indexTokenEnd(s string, start int) int {
	j := start
	for j < len(s) {
		c := s[j]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			j++
			continue
		}
		break
	}
	return j
}

// tokenHandler renders a single %Token% body. lang is forwarded for
// locale-dependent tokens (month / weekday names, ordinals); tokens
// that ignore it just accept the parameter.
type tokenHandler func(t time.Time, lang string) string

// tokenHandlers dispatches %Token% names to their renderers. Adding a
// new SB3 token is a single map entry — no extra switch arm to grow.
var tokenHandlers = map[string]tokenHandler{
	"Year":      func(t time.Time, _ string) string { return fmt.Sprintf("%04d", t.Year()) },
	"YearShort": func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Year()%100) },
	"Mon":       func(t time.Time, _ string) string { return fmt.Sprintf("%02d", int(t.Month())) },
	"MonNum":    func(t time.Time, _ string) string { return fmt.Sprintf("%d", int(t.Month())) },
	"MonShort":  func(t time.Time, lang string) string { return monthShort(t.Month(), lang) },
	"MonLong":   func(t time.Time, lang string) string { return monthLong(t.Month(), lang) },
	"Day":       func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Day()) },
	"DayShort":  func(t time.Time, _ string) string { return fmt.Sprintf("%d", t.Day()) },
	"DayOrd":    func(t time.Time, lang string) string { return dayOrdinal(t.Day(), lang) },
	"Week":      func(t time.Time, lang string) string { return weekShort(t.Weekday(), lang) },
	"WeekLong":  func(t time.Time, lang string) string { return weekLong(t.Weekday(), lang) },
	"Hour":      func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Hour()) },
	"Hour24":    func(t time.Time, _ string) string { return fmt.Sprintf("%d", t.Hour()) },
	"Hour11":    func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Hour()%12) },
	"Hour12":    func(t time.Time, _ string) string { return fmt.Sprintf("%02d", hour12(t)) },
	"HourAP":    func(t time.Time, _ string) string { return hourAMPM(t) },
	"Min":       func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Minute()) },
	"Sec":       func(t time.Time, _ string) string { return fmt.Sprintf("%02d", t.Second()) },
	// Go's %z equivalent: "-0700". time.Time.Format understands it directly.
	"Zone": func(t time.Time, _ string) string { return t.Format("-0700") },
}

func token(name string, t time.Time, lang string) (string, bool) {
	h, ok := tokenHandlers[name]
	if !ok {
		return "", false
	}
	return h(t, lang), true
}

// hour12 maps a 24-hour clock to 12-hour form with the midnight/noon
// "12" fallback that %Hour12% requires. Extracted so the dispatch entry
// stays a one-liner.
func hour12(t time.Time) int {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return h
}

func hourAMPM(t time.Time) string {
	if t.Hour() < 12 {
		return "AM"
	}
	return "PM"
}

var (
	monthLongEN  = [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	monthShortEN = [...]string{"", "Jan.", "Feb.", "Mar.", "Apr.", "May.", "Jun.", "Jul.", "Aug.", "Sep.", "Oct.", "Nov.", "Dec."}
	weekLongEN   = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	weekShortEN  = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	// Japanese month names use the numeric form "1月" … "12月" which
	// matches SB3's default when lang=ja.
	weekLongJA  = [...]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"}
	weekShortJA = [...]string{"日", "月", "火", "水", "木", "金", "土"}
)

func monthLong(m time.Month, lang string) string {
	if lang == "ja" {
		return fmt.Sprintf("%d月", int(m))
	}
	if int(m) < 1 || int(m) > 12 {
		return ""
	}
	return monthLongEN[int(m)]
}

func monthShort(m time.Month, lang string) string {
	if lang == "ja" {
		return fmt.Sprintf("%d月", int(m))
	}
	if int(m) < 1 || int(m) > 12 {
		return ""
	}
	return monthShortEN[int(m)]
}

func weekLong(d time.Weekday, lang string) string {
	if lang == "ja" {
		return weekLongJA[int(d)]
	}
	return weekLongEN[int(d)]
}

func weekShort(d time.Weekday, lang string) string {
	if lang == "ja" {
		return weekShortJA[int(d)]
	}
	return weekShortEN[int(d)]
}

// dayOrdinal renders the day of month as an English ordinal ("3rd") or
// the Japanese "3日" form. Keeps the output locale-appropriate without
// adding a third format dimension.
func dayOrdinal(d int, lang string) string {
	if lang == "ja" {
		return fmt.Sprintf("%d日", d)
	}
	suffix := "th"
	switch {
	case d%100 >= 11 && d%100 <= 13:
		// 11th / 12th / 13th — the -teen exception to the general rule.
	case d%10 == 1:
		suffix = "st"
	case d%10 == 2:
		suffix = "nd"
	case d%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", d, suffix)
}

// DefaultEntryDate is the pattern stored for fresh weblogs when
// nothing has been customised. Matches the SB3 ja preset shape but
// uses ISO-style separators so it reads cleanly in any locale.
const (
	DefaultEntryDate   = "%Year%-%Mon%-%Day% (%Week%)"
	DefaultEntryTime   = "%Hour%:%Min%"
	DefaultCommentDate = "%Year%-%Mon%-%Day% %Hour%:%Min%"
	DefaultListDate    = " (%Mon%/%Day%)"
	DefaultArchiveDate = "%Year%-%Mon%"
)
