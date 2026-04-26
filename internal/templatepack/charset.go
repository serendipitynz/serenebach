package templatepack

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// Legacy SB3 template bundles predate the UTF-8 era — info parts advertise
// charset=ISO-2022-JP in the MIME header, while base.html / style.css have
// no MIME-level charset and were authored in whatever encoding the user's
// editor used (commonly Shift_JIS or EUC-JP on Japanese systems of the era).
// The helpers below normalise everything to UTF-8 at import time so the
// rest of the Go port can stay single-encoding.

type contentKind int

const (
	kindPlain contentKind = iota
	kindHTML
	kindCSS
)

// decodeToUTF8 converts body to UTF-8, using hint (the Content-Type
// charset parameter if any) as the first source of truth. When hint is
// empty it falls back to content-based detection — HTML <meta charset>
// and CSS @charset declarations, ISO-2022-JP escape sequences, then a
// byte-distribution score between Shift_JIS and EUC-JP.
//
// The second return is the detected encoding name, useful for logs.
// ASCII-only and already-UTF-8 inputs are returned unchanged.
func decodeToUTF8(body []byte, hint string, kind contentKind) (string, string) {
	name := canonicalCharsetName(hint)
	if name == "" {
		name = detectCharset(body, kind)
	}
	if name == "" || name == "utf-8" {
		return string(body), "utf-8"
	}
	dec := decoderFor(name)
	if dec == nil {
		return string(body), "utf-8"
	}
	out, _, err := transform.Bytes(dec, body)
	if err != nil {
		// Partial conversion; rather than surface garbled mid-stream
		// output, keep the raw bytes so the operator can still see the
		// corruption and re-export.
		return string(body), "utf-8"
	}
	return string(out), name
}

// canonicalCharsetName maps the common aliases we see in the wild to the
// small set of names decoderFor understands. Empty hint returns "".
func canonicalCharsetName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return "utf-8"
	case "shift-jis", "sjis", "x-sjis", "windows-31j", "cp932", "ms932":
		return "shift_jis"
	case "euc-jp", "eucjp":
		return "euc-jp"
	case "iso-2022-jp", "iso-2022-jp-1":
		return "iso-2022-jp"
	}
	return s
}

func decoderFor(name string) transform.Transformer {
	switch name {
	case "shift_jis":
		return japanese.ShiftJIS.NewDecoder()
	case "euc-jp":
		return japanese.EUCJP.NewDecoder()
	case "iso-2022-jp":
		return japanese.ISO2022JP.NewDecoder()
	}
	return nil
}

var (
	// <meta charset="X"> or <meta http-equiv="Content-Type" content="...; charset=X">.
	// Matches any charset= attribute, and the caller discards values that
	// look like template placeholders (starting with "{").
	reHTMLMetaCharset = regexp.MustCompile(`(?i)<meta[^>]+charset\s*=\s*["']?\s*([A-Za-z0-9_\-:]+)`)
	// @charset "X"; — CSS 2.1 charset rule, which must appear at the very
	// top of the stylesheet.
	reCSSAtCharset = regexp.MustCompile(`(?i)^\s*@charset\s+"([^"]+)"`)
)

func detectCharset(body []byte, kind contentKind) string {
	switch kind {
	case kindHTML:
		if n := extractDeclaredCharset(body, reHTMLMetaCharset); n != "" {
			return n
		}
	case kindCSS:
		if n := extractDeclaredCharset(body, reCSSAtCharset); n != "" {
			return n
		}
	}
	// ISO-2022-JP starts every JIS run with ESC $ B (kanji) or ESC ( B / J
	// (return to ASCII). Spotting any of these is a near-certain signal.
	if bytes.Contains(body, []byte{0x1B, 0x24, 0x42}) ||
		bytes.Contains(body, []byte{0x1B, 0x28, 0x42}) ||
		bytes.Contains(body, []byte{0x1B, 0x28, 0x4A}) {
		return "iso-2022-jp"
	}
	if utf8.Valid(body) {
		return "utf-8"
	}
	return scoreShiftJISvsEUCJP(body)
}

// extractDeclaredCharset returns the canonical name if the regex finds a
// charset and it doesn't look like an unresolved template placeholder.
func extractDeclaredCharset(body []byte, re *regexp.Regexp) string {
	m := re.FindSubmatch(body)
	if m == nil {
		return ""
	}
	val := strings.TrimSpace(string(m[1]))
	if val == "" || strings.HasPrefix(val, "{") {
		return ""
	}
	return canonicalCharsetName(val)
}

// scoreShiftJISvsEUCJP runs body through both decoders and picks whichever
// produced more Japanese-looking runes. When both decode cleanly and tie,
// prefer Shift_JIS — the more common encoding for SB3-era Windows exports.
func scoreShiftJISvsEUCJP(body []byte) string {
	sjisOut, _, _ := transform.Bytes(japanese.ShiftJIS.NewDecoder(), body)
	eucOut, _, _ := transform.Bytes(japanese.EUCJP.NewDecoder(), body)
	if scoreJapanese(eucOut) > scoreJapanese(sjisOut) {
		return "euc-jp"
	}
	return "shift_jis"
}

func scoreJapanese(b []byte) int {
	score := 0
	for _, r := range string(b) {
		switch {
		case r >= 0x3040 && r <= 0x309F: // Hiragana
			score += 2
		case r >= 0x30A0 && r <= 0x30FF: // Katakana
			score += 2
		case r >= 0x4E00 && r <= 0x9FFF: // CJK unified
			score += 2
		case r == utf8.RuneError:
			score -= 3
		}
	}
	return score
}
