package dateformat

import (
	"testing"
	"time"
)

// TestExpandTokens covers every documented SB3 token plus a mix of
// locale-dependent ones. When a token regresses the failing case points
// directly at the offending branch in token().
func TestExpandTokens(t *testing.T) {
	// Thursday, 2026-04-02 09:05:07 +0900. Picked so 1-digit / 2-digit
	// and ordinal distinctions ("2nd") both exercise.
	loc, _ := time.LoadLocation("Asia/Tokyo")
	ts := time.Date(2026, 4, 2, 9, 5, 7, 0, loc)

	cases := []struct {
		pattern string
		lang    string
		want    string
	}{
		{"%Year%", "en", "2026"},
		{"%YearShort%", "en", "26"},
		{"%Mon%", "en", "04"},
		{"%MonNum%", "en", "4"},
		{"%MonShort%", "en", "Apr."},
		{"%MonLong%", "en", "April"},
		{"%Day%", "en", "02"},
		{"%DayShort%", "en", "2"},
		{"%DayOrd%", "en", "2nd"},
		{"%Week%", "en", "Thu"},
		{"%WeekLong%", "en", "Thursday"},
		{"%Hour%", "en", "09"},
		{"%Hour24%", "en", "9"},
		{"%Hour11%", "en", "09"},
		{"%Hour12%", "en", "09"},
		{"%HourAP%", "en", "AM"},
		{"%Min%", "en", "05"},
		{"%Sec%", "en", "07"},
		{"%Zone%", "en", "+0900"},
		// Composite pattern used as the SB3 default entry-date form.
		{"%Year%-%Mon%-%Day% (%Week%)", "en", "2026-04-02 (Thu)"},
		// Literal text untouched.
		{"posted: %Year%", "en", "posted: 2026"},
		// Unknown token passes through untouched.
		{"%Bogus%", "en", "%Bogus%"},
		// Locale swap on month/weekday names.
		{"%MonLong% %DayOrd%", "ja", "4月 2日"},
		{"%WeekLong%", "ja", "木曜日"},
	}
	for _, tc := range cases {
		got := Expand(tc.pattern, ts, tc.lang)
		if got != tc.want {
			t.Errorf("Expand(%q, lang=%q) = %q, want %q", tc.pattern, tc.lang, got, tc.want)
		}
	}
}

// TestExpandOrdinalEdgeCases covers the -teen exception in English
// ordinals (11th / 12th / 13th) — every other day in the month goes
// through the ones-digit branch.
func TestExpandOrdinalEdgeCases(t *testing.T) {
	cases := map[int]string{
		1: "1st", 2: "2nd", 3: "3rd", 4: "4th",
		11: "11th", 12: "12th", 13: "13th",
		21: "21st", 22: "22nd", 23: "23rd", 30: "30th",
	}
	for day, want := range cases {
		ts := time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)
		got := Expand("%DayOrd%", ts, "en")
		if got != want {
			t.Errorf("day %d ordinal = %q, want %q", day, got, want)
		}
	}
}

// TestExpand12HourMidnightNoon pins down the common "12:00 AM / 12:00 PM"
// edge case that %Hour12% gets wrong if you write `hour%12` without the
// zero→12 fallback.
func TestExpand12HourMidnightNoon(t *testing.T) {
	midnight := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	noon := time.Date(2026, 1, 1, 12, 5, 0, 0, time.UTC)
	if got := Expand("%Hour12%:%Min% %HourAP%", midnight, "en"); got != "12:05 AM" {
		t.Errorf("midnight 12h = %q", got)
	}
	if got := Expand("%Hour12%:%Min% %HourAP%", noon, "en"); got != "12:05 PM" {
		t.Errorf("noon 12h = %q", got)
	}
}

// TestExpandEmptyOrZero returns empty, consistent with the handler
// contract: blank format string = "caller falls back to its default".
func TestExpandEmptyOrZero(t *testing.T) {
	if Expand("", time.Now(), "en") != "" {
		t.Errorf("empty pattern should produce empty output")
	}
	if Expand("%Year%", time.Time{}, "en") != "" {
		t.Errorf("zero time should produce empty output")
	}
}
