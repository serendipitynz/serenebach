package templatepack

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// SB3 template.txt samples used as parser fixtures. These files are
// not bundled with the repository; set SB_TEST_TEMPLATE_DIR to a
// directory that contains `default/template.txt` and
// `gray/template.txt` (e.g. an extracted SB3 distribution) to run
// these tests. They skip cleanly otherwise so CI stays green.
func sampleTemplatePath(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("SB_TEST_TEMPLATE_DIR")
	if dir == "" {
		t.Skip("SB_TEST_TEMPLATE_DIR not set; no SB3 template fixtures available")
	}
	return dir + "/" + name + "/template.txt"
}

func TestParseDefaultSample(t *testing.T) {
	raw, err := os.ReadFile(sampleTemplatePath(t, "default"))
	if err != nil {
		t.Skipf("sample missing: %v", err)
	}
	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pack.Name != "default" {
		t.Errorf("Name = %q, want %q", pack.Name, "default")
	}
	if pack.Author != "takkyun" {
		t.Errorf("Author = %q, want takkyun", pack.Author)
	}
	if !strings.Contains(pack.MainBody, "{site_title}") {
		t.Errorf("MainBody should contain {site_title}; got %q", pack.MainBody[:200])
	}
	if !strings.Contains(pack.CSS, "body") {
		t.Errorf("CSS should contain a body selector; got: %q", pack.CSS[:200])
	}
	if len(pack.Assets) != 0 {
		t.Errorf("default sample has no image assets; got %d", len(pack.Assets))
	}
}

func TestParseGraySampleIncludesImageAssets(t *testing.T) {
	raw, err := os.ReadFile(sampleTemplatePath(t, "gray"))
	if err != nil {
		t.Skipf("sample missing: %v", err)
	}
	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pack.Name != "gray" {
		t.Errorf("Name = %q, want gray", pack.Name)
	}
	// The gray sample bundles four images.
	if len(pack.Assets) != 4 {
		t.Errorf("asset count = %d, want 4", len(pack.Assets))
	}
	var foundJPG, foundGIF bool
	for _, a := range pack.Assets {
		if strings.HasSuffix(a.Filename, ".jpg") {
			foundJPG = true
			// Each asset body should decode into actual image bytes
			// (non-empty, starts with JPEG SOI marker FF D8).
			if len(a.Data) < 4 || a.Data[0] != 0xFF || a.Data[1] != 0xD8 {
				t.Errorf("jpeg asset %q bytes look wrong: %x", a.Filename, a.Data[:4])
			}
		}
		if strings.HasSuffix(a.Filename, ".gif") {
			foundGIF = true
			if !bytes.HasPrefix(a.Data, []byte("GIF8")) {
				t.Errorf("gif asset %q missing GIF8 header: %x", a.Filename, a.Data[:4])
			}
		}
	}
	if !foundJPG || !foundGIF {
		t.Errorf("expected at least one jpg and one gif asset")
	}
}

func TestRoundTripThroughWriter(t *testing.T) {
	original := &Pack{
		Name:      "round-trip",
		Author:    "me",
		Address:   "https://example.com/",
		Version:   "0.01",
		Info:      "Name: round-trip\nAuthor: me\nAddress: https://example.com/\nVersion: 0.01\n=====\ndescription body\n",
		MainBody:  "<html>{site_title}</html>",
		CSS:       "body { color: red; }",
		EntryBody: "<article>{entry_title}</article>",
		Assets: []Asset{
			{Filename: "logo.png", MimeType: "image/png", Data: []byte("\x89PNG\r\n\x1a\nbinary-body")},
		},
	}
	var buf bytes.Buffer
	if err := Write(&buf, original, time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("write: %v", err)
	}

	parsed, err := Parse(&buf)
	if err != nil {
		t.Fatalf("parse after write: %v", err)
	}
	if parsed.Name != original.Name {
		t.Errorf("Name lost: %q vs %q", parsed.Name, original.Name)
	}
	if parsed.Author != original.Author {
		t.Errorf("Author lost: %q vs %q", parsed.Author, original.Author)
	}
	if parsed.MainBody != original.MainBody {
		t.Errorf("MainBody lost:\n got %q\nwant %q", parsed.MainBody, original.MainBody)
	}
	if parsed.CSS != original.CSS {
		t.Errorf("CSS lost: %q", parsed.CSS)
	}
	if parsed.EntryBody != original.EntryBody {
		t.Errorf("EntryBody lost: %q", parsed.EntryBody)
	}
	if len(parsed.Assets) != 1 {
		t.Fatalf("asset count = %d, want 1", len(parsed.Assets))
	}
	if !bytes.Equal(parsed.Assets[0].Data, original.Assets[0].Data) {
		t.Errorf("asset bytes round-trip mismatch:\n got %q\nwant %q",
			parsed.Assets[0].Data, original.Assets[0].Data)
	}
}

func TestParseRejectsNonMultipart(t *testing.T) {
	// A plain text/plain body should be rejected — the template bundle
	// format requires multipart.
	raw := "Content-Type: text/plain\r\n\r\nhello"
	if _, err := Parse(strings.NewReader(raw)); err == nil {
		t.Errorf("expected error for non-multipart body")
	}
}

// ---- charset hardening tests -------------------------------------------

// encodeTo runs s through the given transformer and returns the encoded
// bytes. Used to synthesise legacy-encoded fixture bodies.
func encodeTo(t *testing.T, enc transform.Transformer, s string) []byte {
	t.Helper()
	b, _, err := transform.Bytes(enc, []byte(s))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// bundleBuilder is a tiny helper for composing hand-rolled MIME multipart
// bundles in tests. The real Write function always emits UTF-8 + the
// "correct" Content-Type, but these tests cover *reading* legacy
// bundles where info parts advertise charset=ISO-2022-JP and html/css
// parts omit charset entirely, so tests build bundles by hand.
type bundleBuilder struct {
	boundary string
	buf      bytes.Buffer
}

func newBundle(boundary string) *bundleBuilder {
	b := &bundleBuilder{boundary: boundary}
	fmt.Fprintf(&b.buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
	b.buf.WriteString("This is a multi-part message in MIME format.\r\n")
	return b
}

// info7bit writes a 7bit-encoded info part. charset may be empty.
func (b *bundleBuilder) info7bit(charset string, body []byte) *bundleBuilder {
	fmt.Fprintf(&b.buf, "--%s\r\n", b.boundary)
	if charset != "" {
		fmt.Fprintf(&b.buf, "Content-Type: text/plain; charset=%s\r\n", charset)
	} else {
		b.buf.WriteString("Content-Type: text/plain\r\n")
	}
	b.buf.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	b.buf.Write(body)
	b.buf.WriteString("\r\n")
	return b
}

// partBase64 writes a base64-encoded named part (base.html / style.css /
// entry.html / asset). charset may be empty.
func (b *bundleBuilder) partBase64(filename, mimeType, charset string, body []byte) *bundleBuilder {
	fmt.Fprintf(&b.buf, "--%s\r\n", b.boundary)
	if charset != "" {
		fmt.Fprintf(&b.buf, "Content-Type: %s; name=\"%s\"; charset=%s\r\n", mimeType, filename, charset)
	} else {
		fmt.Fprintf(&b.buf, "Content-Type: %s; name=\"%s\"\r\n", mimeType, filename)
	}
	b.buf.WriteString("Content-Transfer-Encoding: Base64\r\n")
	fmt.Fprintf(&b.buf, "Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", filename)
	b.buf.WriteString(base64.StdEncoding.EncodeToString(body))
	b.buf.WriteString("\r\n")
	return b
}

func (b *bundleBuilder) close() []byte {
	fmt.Fprintf(&b.buf, "--%s--\r\n", b.boundary)
	return b.buf.Bytes()
}

func TestParseConvertsISO2022JPInfo(t *testing.T) {
	jpInfo := "Name: テストテンプレート\nAuthor: 太郎\n=====\nこれはテスト用の説明文です。\n"
	encoded := encodeTo(t, japanese.ISO2022JP.NewEncoder(), jpInfo)
	raw := newBundle("===t===").
		info7bit("ISO-2022-JP", encoded).
		partBase64("base.html", "text/html", "", []byte("<html>{site_title}</html>")).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pack.Name != "テストテンプレート" {
		t.Errorf("Name = %q, want テストテンプレート", pack.Name)
	}
	if pack.Author != "太郎" {
		t.Errorf("Author = %q, want 太郎", pack.Author)
	}
	if !strings.Contains(pack.Info, "これはテスト用の説明文です。") {
		t.Errorf("Info free-form body not converted: %q", pack.Info)
	}
}

func TestParseConvertsTypoContetTypeCharset(t *testing.T) {
	// gray sample uses `Contet-Type` (sic). Verify mergeTypoHeader lets
	// charset extraction work through the typo path.
	jpInfo := "Name: typo-test\n=====\n日本語の説明。\n"
	encoded := encodeTo(t, japanese.ISO2022JP.NewEncoder(), jpInfo)

	var buf bytes.Buffer
	boundary := "===typo==="
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
	buf.WriteString("preamble\r\n")
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	// Intentionally use the typo header.
	buf.WriteString("Contet-Type: text/plain; charset=ISO-2022-JP\r\n")
	buf.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	buf.Write(encoded)
	buf.WriteString("\r\n")
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	pack, err := Parse(&buf)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pack.Info, "日本語の説明。") {
		t.Errorf("typo-header charset lost: %q", pack.Info)
	}
}

func TestParseDetectsShiftJISInHTMLWithoutDeclaration(t *testing.T) {
	// No <meta charset>, no MIME charset — detection must fall through
	// to content-based scoring and pick Shift_JIS.
	html := "<html><body>こんにちは世界</body></html>"
	encoded := encodeTo(t, japanese.ShiftJIS.NewEncoder(), html)
	raw := newBundle("===sjis===").
		info7bit("", []byte("Name: sjis-test\n")).
		partBase64("base.html", "text/html", "", encoded).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pack.MainBody, "こんにちは世界") {
		t.Errorf("Shift_JIS HTML not detected; got %q", pack.MainBody)
	}
}

func TestParseDetectsShiftJISViaMetaTag(t *testing.T) {
	html := `<html><head><meta http-equiv="Content-Type" content="text/html; charset=Shift_JIS"></head><body>あいうえお</body></html>`
	encoded := encodeTo(t, japanese.ShiftJIS.NewEncoder(), html)
	raw := newBundle("===meta===").
		info7bit("", []byte("Name: meta-test\n")).
		partBase64("base.html", "text/html", "", encoded).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pack.MainBody, "あいうえお") {
		t.Errorf("meta-tag Shift_JIS not applied; got %q", pack.MainBody)
	}
}

func TestParseIgnoresTemplatePlaceholderMeta(t *testing.T) {
	// SB3 templates embed `charset={site_encoding}` as a template tag,
	// not a literal encoding. Detection must ignore the placeholder and
	// fall back to content-based scoring.
	html := `<html><head><meta http-equiv="Content-Type" content="text/html; charset={site_encoding}"></head><body>さくらんぼ</body></html>`
	encoded := encodeTo(t, japanese.ShiftJIS.NewEncoder(), html)
	raw := newBundle("===placeholder===").
		info7bit("", []byte("Name: ph-test\n")).
		partBase64("base.html", "text/html", "", encoded).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pack.MainBody, "さくらんぼ") {
		t.Errorf("placeholder meta should fall back to content detection; got %q", pack.MainBody)
	}
}

func TestParseDetectsEUCJPInCSS(t *testing.T) {
	css := `@charset "EUC-JP";
/* 背景色 */
body { color: red; }
`
	encoded := encodeTo(t, japanese.EUCJP.NewEncoder(), css)
	raw := newBundle("===euc===").
		info7bit("", []byte("Name: euc-test\n")).
		partBase64("style.css", "text/css", "", encoded).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pack.CSS, "背景色") {
		t.Errorf("EUC-JP CSS not decoded via @charset; got %q", pack.CSS)
	}
}

func TestParseKeepsUTF8Unchanged(t *testing.T) {
	html := "<html><body>already utf-8 日本語</body></html>"
	raw := newBundle("===utf===").
		info7bit("UTF-8", []byte("Name: utf-test\n")).
		partBase64("base.html", "text/html", "", []byte(html)).
		close()

	pack, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pack.MainBody != html {
		t.Errorf("UTF-8 body mutated:\n got %q\nwant %q", pack.MainBody, html)
	}
}
