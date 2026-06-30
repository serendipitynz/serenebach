package jacharset

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// All Japanese fixtures are dummy strings built in-test by encoding a
// known UTF-8 sample into the target legacy encoding. No real data /
// PII. The detector's documented order is:
//
//	Content-Type hint → HTML <meta> / CSS @charset → ISO-2022-JP escape
//	sniff → UTF-8 validity → Shift_JIS/EUC-JP byte-distribution score
//
// Each test walks one rung of that ladder so a regression in detection
// ordering is caught precisely, not just "decode broke somewhere".

// sampleJP is intentionally long enough that the SJIS/EUC distribution
// score is unambiguous on the hint-less paths.
const sampleJP = "日本語のテスト文字列です。ひらがなカタカナ漢字。"

func encode(t *testing.T, enc transform.Transformer, s string) []byte {
	t.Helper()
	out, _, err := transform.Bytes(enc, []byte(s))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}

func TestDecodeASCIIUnchanged(t *testing.T) {
	const ascii = "hello world 123"
	got, name := DecodeToUTF8([]byte(ascii), "", KindPlain)
	if got != ascii {
		t.Errorf("decoded = %q, want unchanged %q", got, ascii)
	}
	if name != "utf-8" {
		t.Errorf("encodingName = %q, want utf-8", name)
	}
}

func TestDecodeAlreadyUTF8Unchanged(t *testing.T) {
	got, name := DecodeToUTF8([]byte(sampleJP), "", KindPlain)
	if got != sampleJP {
		t.Errorf("decoded = %q, want unchanged UTF-8 input", got)
	}
	if name != "utf-8" {
		t.Errorf("encodingName = %q, want utf-8", name)
	}
}

func TestHintTakesPrecedenceOverContentDetection(t *testing.T) {
	// Plain ASCII decodes cleanly under any of these decoders, so the
	// returned name proves the hint was honoured even though content
	// detection alone would call this "utf-8".
	cases := []struct {
		hint     string
		wantName string
	}{
		{"shift_jis", "shift_jis"},
		{"Shift_JIS", "shift_jis"},
		{"sjis", "shift_jis"},
		{"x-sjis", "shift_jis"},
		{"windows-31j", "shift_jis"},
		{"euc-jp", "euc-jp"},
		{"eucjp", "euc-jp"},
		{"EUC_JP", "euc-jp"},
		{"iso-2022-jp", "iso-2022-jp"},
		{"utf-8", "utf-8"},
		{"us-ascii", "utf-8"},
	}
	for _, tc := range cases {
		t.Run(tc.hint, func(t *testing.T) {
			got, name := DecodeToUTF8([]byte("plain ascii body"), tc.hint, KindPlain)
			if got != "plain ascii body" {
				t.Errorf("decoded = %q, want unchanged ASCII", got)
			}
			if name != tc.wantName {
				t.Errorf("hint %q: encodingName = %q, want %q", tc.hint, name, tc.wantName)
			}
		})
	}
}

func TestDecodeShiftJISByDistribution(t *testing.T) {
	body := encode(t, japanese.ShiftJIS.NewEncoder(), sampleJP)
	got, name := DecodeToUTF8(body, "", KindPlain)
	if got != sampleJP {
		t.Errorf("decoded = %q, want %q", got, sampleJP)
	}
	if name != "shift_jis" {
		t.Errorf("encodingName = %q, want shift_jis", name)
	}
}

func TestDecodeEUCJPByDistribution(t *testing.T) {
	body := encode(t, japanese.EUCJP.NewEncoder(), sampleJP)
	got, name := DecodeToUTF8(body, "", KindPlain)
	if got != sampleJP {
		t.Errorf("decoded = %q, want %q", got, sampleJP)
	}
	if name != "euc-jp" {
		t.Errorf("encodingName = %q, want euc-jp", name)
	}
}

func TestDecodeISO2022JPByEscapeSniff(t *testing.T) {
	// ISO-2022-JP is 7-bit clean, so it is also "valid UTF-8". This case
	// locks the ordering: the ESC-sequence sniff must fire BEFORE the
	// utf8.Valid check, otherwise the body would be mistaken for UTF-8
	// and the JIS escapes would leak through as garbage.
	body := encode(t, japanese.ISO2022JP.NewEncoder(), sampleJP)
	got, name := DecodeToUTF8(body, "", KindPlain)
	if got != sampleJP {
		t.Errorf("decoded = %q, want %q", got, sampleJP)
	}
	if name != "iso-2022-jp" {
		t.Errorf("encodingName = %q, want iso-2022-jp", name)
	}
}

func TestKindHTMLReadsMetaCharset(t *testing.T) {
	sjis := encode(t, japanese.ShiftJIS.NewEncoder(), sampleJP)
	cases := []struct {
		name string
		head string
	}{
		{"meta charset", `<meta charset="shift_jis">` + "\n"},
		{"meta http-equiv", `<meta http-equiv="Content-Type" content="text/html; charset=Shift_JIS">` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte(tc.head), sjis...)
			got, encName := DecodeToUTF8(body, "", KindHTML)
			if encName != "shift_jis" {
				t.Errorf("encodingName = %q, want shift_jis (from %s)", encName, tc.name)
			}
			// The hiragana/kanji portion must come back intact.
			if !strings.Contains(got, sampleJP) {
				t.Errorf("decoded body %q missing %q", got, sampleJP)
			}
		})
	}
}

func TestKindHTMLIgnoresTemplatePlaceholderCharset(t *testing.T) {
	// SB templates carry {feed_site_encoding}-style placeholders before
	// they are rendered; a "charset={...}" must not be treated as a real
	// declaration. With a UTF-8 body the detector should fall through to
	// content sniffing and report utf-8.
	body := []byte(`<meta charset="{site_charset}">` + "\n" + sampleJP)
	got, name := DecodeToUTF8(body, "", KindHTML)
	if name != "utf-8" {
		t.Errorf("encodingName = %q, want utf-8 (placeholder charset ignored)", name)
	}
	if !strings.Contains(got, sampleJP) {
		t.Errorf("decoded body %q missing %q", got, sampleJP)
	}
}

func TestKindCSSReadsAtCharset(t *testing.T) {
	sjis := encode(t, japanese.ShiftJIS.NewEncoder(), sampleJP)
	body := append([]byte(`@charset "shift_jis";`+"\n"), sjis...)
	got, name := DecodeToUTF8(body, "", KindCSS)
	if name != "shift_jis" {
		t.Errorf("encodingName = %q, want shift_jis (from @charset)", name)
	}
	if !strings.Contains(got, sampleJP) {
		t.Errorf("decoded body %q missing %q", got, sampleJP)
	}
}

func TestKindPlainIgnoresInlineDeclarations(t *testing.T) {
	// A KindPlain body that happens to contain a <meta charset> string
	// must NOT be parsed for it — plain content has no declaration step,
	// so detection proceeds straight to the content sniff. The body is
	// EUC-JP, so it must be detected as euc-jp, not as whatever the
	// embedded meta claims.
	euc := encode(t, japanese.EUCJP.NewEncoder(), `<meta charset="shift_jis"> `+sampleJP)
	got, name := DecodeToUTF8(euc, "", KindPlain)
	if name != "euc-jp" {
		t.Errorf("encodingName = %q, want euc-jp (inline meta must be ignored for KindPlain)", name)
	}
	if !strings.Contains(got, sampleJP) {
		t.Errorf("decoded body %q missing %q", got, sampleJP)
	}
}

func TestHintBeatsHTMLMeta(t *testing.T) {
	// Detection order puts the Content-Type hint above the inline <meta>.
	// ASCII body keeps the hint decoder from erroring, isolating ordering.
	body := []byte(`<meta charset="euc-jp">` + "\nplain ascii")
	_, name := DecodeToUTF8(body, "shift_jis", KindHTML)
	if name != "shift_jis" {
		t.Errorf("encodingName = %q, want shift_jis (hint must beat <meta>)", name)
	}
}

func TestDecodeEmptyInputDoesNotPanic(t *testing.T) {
	got, name := DecodeToUTF8([]byte{}, "", KindPlain)
	if got != "" {
		t.Errorf("decoded = %q, want empty", got)
	}
	if name != "utf-8" {
		t.Errorf("encodingName = %q, want utf-8", name)
	}
}

func TestDecodeInvalidBytesDoNotPanic(t *testing.T) {
	// Lone high bytes that are neither valid UTF-8 nor a clean legacy
	// run. The contract is "never panic" and "return one of the known
	// encoding labels"; the exact label is implementation-defined, so we
	// only assert non-panic + a sane label here.
	got, name := DecodeToUTF8([]byte{0xFF, 0xFE, 0x80, 0x81}, "", KindPlain)
	_ = got
	switch name {
	case "utf-8", "shift_jis", "euc-jp", "iso-2022-jp":
		// acceptable
	default:
		t.Errorf("encodingName = %q, want one of the known labels", name)
	}
}

func TestDistributionGoldenLocksLabels(t *testing.T) {
	// Regression golden: the same dummy Japanese sample encoded two ways
	// must keep resolving to its source encoding. This is the net that
	// catches an accidental change to scoreShiftJISvsEUCJP from silently
	// flipping SJIS↔EUC detection on real imports.
	if _, name := DecodeToUTF8(encode(t, japanese.ShiftJIS.NewEncoder(), sampleJP), "", KindPlain); name != "shift_jis" {
		t.Errorf("SJIS sample golden = %q, want shift_jis", name)
	}
	if _, name := DecodeToUTF8(encode(t, japanese.EUCJP.NewEncoder(), sampleJP), "", KindPlain); name != "euc-jp" {
		t.Errorf("EUC sample golden = %q, want euc-jp", name)
	}
}
