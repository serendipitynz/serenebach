// Package templatepack reads and writes SB3's multipart/mixed template
// bundle format (template.txt). A bundle carries the base HTML template,
// the CSS template, an optional entry HTML template, every binary asset
// the template references, and a free-form info part with metadata
// (Name / Author / Address / Version).
//
// The format is an RFC 822 message with headers at the top
// (From / To / Date / Subject / X-Version / Content-Type: multipart/mixed)
// and one MIME body part per chunk. Each chunk carries:
//
//   - `Content-Type: text/html; name="base.html"` (or similar)
//   - `Content-Transfer-Encoding: Base64` (binary chunks) or `7bit` (info)
//   - `Content-Disposition: attachment; filename="<name>"`
//
// Writer output round-trips through Parse unchanged so exports from the
// admin UI can be imported back — by this tool or the legacy SB3
// template-import screen.
package templatepack

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

// Pack is the decoded contents of a template.txt. Only Name and one of
// MainBody / CSS are strictly required to be a useful bundle.
type Pack struct {
	// Metadata from the info part. Empty fields simply round-trip as "".
	Name    string
	Author  string
	Address string
	Version string
	// Info is the raw body of the info part (including metadata lines
	// and the free-form description after the `=====` separator). Stored
	// verbatim so round-trip export reproduces the author's original.
	Info string

	MainBody  string // base.html
	CSS       string // style.css
	EntryBody string // entry.html (optional)

	// Assets holds every non-template binary chunk (images, fonts, etc.).
	// Known filenames base.html / style.css / entry.html are pulled into
	// the fields above instead of appearing here.
	Assets []Asset
}

// Asset is one binary chunk — usually an image referenced from the HTML
// or CSS via the {site_parts} tag.
type Asset struct {
	Filename string
	MimeType string
	Data     []byte
}

// Well-known filenames that land in typed Pack fields instead of Assets.
const (
	FilenameMain      = "base.html"
	FilenameCSS       = "style.css"
	FilenameEntry     = "entry.html"
	FilenameEntryBase = "entry_base.html" // some SB3 themes use this legacy name
)

// Parse reads a template.txt from r. The header block is standard RFC
// 822, and the body is multipart/mixed with a boundary declared in
// Content-Type. Unrecognised chunks are stored under Assets so nothing
// silently disappears on import.
func Parse(r io.Reader) (*Pack, error) {
	msg, err := mail.ReadMessage(bufio.NewReader(r))
	if err != nil {
		return nil, fmt.Errorf("templatepack: read message: %w", err)
	}
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("templatepack: parse content-type: %w", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, fmt.Errorf("templatepack: expected multipart/*, got %q", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, fmt.Errorf("templatepack: missing boundary")
	}

	pack := &Pack{}
	mr := multipart.NewReader(msg.Body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("templatepack: next part: %w", err)
		}
		if err := readPart(pack, part); err != nil {
			return nil, err
		}
	}
	return pack, nil
}

// readPart dispatches one MIME part to either the info-text path or the
// binary-asset path based on Content-Disposition + headers.
func readPart(pack *Pack, part *multipart.Part) error {
	// Some SB3 exports mistype the header as "Contet-Type" — be
	// forgiving: MIMEHeader lookup is case-insensitive but the typo
	// wins. Merge any misspelled twin before reading.
	hdr := mergeTypoHeader(part.Header)
	filename := extractFilename(hdr)
	enc := strings.ToLower(strings.TrimSpace(hdr.Get("Content-Transfer-Encoding")))
	charsetHint := extractCharset(hdr)

	body, err := io.ReadAll(part)
	if err != nil {
		return fmt.Errorf("templatepack: read part body: %w", err)
	}
	if enc == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(stripBase64Whitespace(string(body)))
		if err != nil {
			return fmt.Errorf("templatepack: base64 decode %q: %w", filename, err)
		}
		body = decoded
	}

	// Info part: no filename attached; carries the Name/Author metadata.
	if filename == "" {
		text, _ := decodeToUTF8(body, charsetHint, kindPlain)
		pack.Info = text
		parseInfoFields(pack)
		return nil
	}

	switch filename {
	case FilenameMain:
		pack.MainBody, _ = decodeToUTF8(body, charsetHint, kindHTML)
	case FilenameCSS:
		pack.CSS, _ = decodeToUTF8(body, charsetHint, kindCSS)
	case FilenameEntry, FilenameEntryBase:
		pack.EntryBody, _ = decodeToUTF8(body, charsetHint, kindHTML)
	default:
		// Anything else — treat as an asset. Trust the declared
		// Content-Type over guessing from the extension; the admin UI
		// lets the operator correct mime types per-asset later if needed.
		mt := hdr.Get("Content-Type")
		if idx := strings.IndexByte(mt, ';'); idx >= 0 {
			mt = strings.TrimSpace(mt[:idx])
		}
		pack.Assets = append(pack.Assets, Asset{
			Filename: filename,
			MimeType: mt,
			Data:     body,
		})
	}
	return nil
}

// mergeTypoHeader looks for the common "Contet-Type" misspelling (present
// in sample exports) and copies it onto the canonical header so the
// rest of the parser doesn't need to know about the typo.
func mergeTypoHeader(h textproto.MIMEHeader) textproto.MIMEHeader {
	if h.Get("Content-Type") != "" {
		return h
	}
	for key, vals := range h {
		if strings.EqualFold(key, "Contet-Type") {
			h["Content-Type"] = vals
			break
		}
	}
	return h
}

// extractFilename pulls the asset filename off Content-Disposition or
// (fallback) the name parameter on Content-Type. Returns "" for the info
// part and any part lacking a filename.
func extractFilename(h textproto.MIMEHeader) string {
	if cd := h.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := params["filename"]; name != "" {
				return name
			}
		}
	}
	if ct := h.Get("Content-Type"); ct != "" {
		if _, params, err := mime.ParseMediaType(ct); err == nil {
			if name := params["name"]; name != "" {
				return name
			}
		}
	}
	return ""
}

// extractCharset returns the charset parameter on the part's Content-Type
// header, or "" when no charset was declared. The value is returned raw;
// canonicalisation happens in the charset helpers.
func extractCharset(h textproto.MIMEHeader) string {
	ct := h.Get("Content-Type")
	if ct == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return ""
	}
	return params["charset"]
}

// infoKeyRe extracts the simple `key: value` metadata at the top of the
// info part. We stop as soon as we see an empty line or the `=====`
// separator — anything after that is free-form description.
var infoKeyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*):\s*(.*)$`)

func parseInfoFields(p *Pack) {
	for _, line := range strings.Split(p.Info, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" {
			return
		}
		if strings.HasPrefix(trimmed, "=====") {
			return
		}
		m := infoKeyRe.FindStringSubmatch(trimmed)
		if m == nil {
			return
		}
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "name":
			p.Name = val
		case "author":
			p.Author = val
		case "address":
			p.Address = val
		case "version":
			p.Version = val
		}
	}
}

func stripBase64Whitespace(s string) string {
	// Base64 body may contain CR/LF line wraps — strip them to a compact
	// form StdEncoding.DecodeString accepts.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ---- writer ------------------------------------------------------------

// Write produces a template.txt-formatted bundle on w that Parse accepts
// as input. Uses a deterministic boundary built off the current time so
// consecutive exports are distinguishable but the producer doesn't need
// a crypto-random source. Passed `now` so tests stay deterministic.
func Write(w io.Writer, pack *Pack, now time.Time) error {
	boundary := fmt.Sprintf("===sbTempPack%d===", now.UnixNano())

	headers := []string{
		"From: serenebach <noreply@example.com>",
		"To: sb users",
		"Date: " + now.UTC().Format(time.RFC1123),
		"Subject: " + pack.Name,
		"X-Version: " + pack.Version,
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"",
		"", // blank line terminates headers
		"This is a multi-part message in MIME format.",
	}
	if _, err := io.WriteString(w, strings.Join(headers, "\r\n")+"\r\n"); err != nil {
		return err
	}

	// Info part
	info := buildInfoBody(pack)
	if err := writeInfoPart(w, boundary, info); err != nil {
		return err
	}
	// Template parts
	if pack.MainBody != "" {
		if err := writeBase64Part(w, boundary, FilenameMain, "text/html", []byte(pack.MainBody)); err != nil {
			return err
		}
	}
	if pack.CSS != "" {
		if err := writeBase64Part(w, boundary, FilenameCSS, "text/css", []byte(pack.CSS)); err != nil {
			return err
		}
	}
	if pack.EntryBody != "" {
		if err := writeBase64Part(w, boundary, FilenameEntry, "text/html", []byte(pack.EntryBody)); err != nil {
			return err
		}
	}
	// Asset parts
	for _, a := range pack.Assets {
		mt := a.MimeType
		if mt == "" {
			mt = "application/octet-stream"
		}
		if err := writeBase64Part(w, boundary, a.Filename, mt, a.Data); err != nil {
			return err
		}
	}

	// Closing boundary
	if _, err := fmt.Fprintf(w, "--%s--\r\n", boundary); err != nil {
		return err
	}
	return nil
}

func buildInfoBody(p *Pack) string {
	if p.Info != "" {
		return p.Info
	}
	// Build a minimal info body from the typed metadata fields so the
	// bundle is still valid when a user never edited the info column.
	var b strings.Builder
	if p.Name != "" {
		fmt.Fprintf(&b, "Name: %s\n", p.Name)
	}
	if p.Author != "" {
		fmt.Fprintf(&b, "Author: %s\n", p.Author)
	}
	if p.Address != "" {
		fmt.Fprintf(&b, "Address: %s\n", p.Address)
	}
	if p.Version != "" {
		fmt.Fprintf(&b, "Version: %s\n", p.Version)
	}
	return b.String()
}

func writeInfoPart(w io.Writer, boundary, body string) error {
	_, err := fmt.Fprintf(w,
		"--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 7bit\r\n\r\n%s\r\n",
		boundary, body)
	return err
}

func writeBase64Part(w io.Writer, boundary, filename, mimeType string, data []byte) error {
	header := fmt.Sprintf(
		"--%s\r\nContent-Type: %s; name=\"%s\"\r\nContent-Transfer-Encoding: Base64\r\nContent-Disposition: attachment; filename=\"%s\"\r\n\r\n",
		boundary, mimeType, filename, filename)
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return writeWrapped(w, encoded, 76)
}

// writeWrapped outputs encoded with a CRLF every `width` characters to
// match RFC 2045 style wrapping (which is what SB3 exports use).
func writeWrapped(w io.Writer, encoded string, width int) error {
	var buf bytes.Buffer
	for i := 0; i < len(encoded); i += width {
		end := i + width
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
	_, err := w.Write(buf.Bytes())
	return err
}
