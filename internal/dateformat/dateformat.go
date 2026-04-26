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

func token(name string, t time.Time, lang string) (string, bool) {
	switch name {
	case "Year":
		return fmt.Sprintf("%04d", t.Year()), true
	case "YearShort":
		return fmt.Sprintf("%02d", t.Year()%100), true
	case "Mon":
		return fmt.Sprintf("%02d", int(t.Month())), true
	case "MonNum":
		return fmt.Sprintf("%d", int(t.Month())), true
	case "MonShort":
		return monthShort(t.Month(), lang), true
	case "MonLong":
		return monthLong(t.Month(), lang), true
	case "Day":
		return fmt.Sprintf("%02d", t.Day()), true
	case "DayShort":
		return fmt.Sprintf("%d", t.Day()), true
	case "DayOrd":
		return dayOrdinal(t.Day(), lang), true
	case "Week":
		return weekShort(t.Weekday(), lang), true
	case "WeekLong":
		return weekLong(t.Weekday(), lang), true
	case "Hour":
		return fmt.Sprintf("%02d", t.Hour()), true
	case "Hour24":
		return fmt.Sprintf("%d", t.Hour()), true
	case "Hour11":
		h := t.Hour() % 12
		return fmt.Sprintf("%02d", h), true
	case "Hour12":
		h := t.Hour() % 12
		if h == 0 {
			h = 12
		}
		return fmt.Sprintf("%02d", h), true
	case "HourAP":
		if t.Hour() < 12 {
			return "AM", true
		}
		return "PM", true
	case "Min":
		return fmt.Sprintf("%02d", t.Minute()), true
	case "Sec":
		return fmt.Sprintf("%02d", t.Second()), true
	case "Zone":
		// Go's %z equivalent: "-0700". time.Time.Format understands
		// "-0700" directly.
		return t.Format("-0700"), true
	}
	return "", false
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
	DefaultListDate    = "%Mon%/%Day%"
	DefaultArchiveDate = "%Year%-%Mon%"
)
