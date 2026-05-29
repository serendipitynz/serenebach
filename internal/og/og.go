// Package og renders per-entry Open Graph card images (1200×630 PNG).
// Pure Go: stdlib `image` + `image/png` plus `golang.org/x/image/font/
// opentype` so the single-binary distribution stays cgo-free. The JP
// font (Noto Sans JP Medium) and the default background image are
// embedded into the binary — deployment is still one file.
package og

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"unicode"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Noto Sans JP (Medium) is (c) 2014, 2015 Adobe Systems Incorporated and is
// redistributed unmodified under the SIL Open Font License 1.1. The full
// license text ships next to the font in assets/NotoSansJP-LICENSE.txt and
// in every release archive.
//
//go:embed assets/NotoSansJP-Medium.otf
var jpFontBytes []byte

//go:embed assets/sb_opengraph_bg.png
var defaultBGBytes []byte

// Card dimensions lined up with Facebook / Twitter recommendations.
const (
	CardWidth  = 1200
	CardHeight = 630
)

// Layout tunables — kept as constants so the rendered result is
// deterministic given the same title + site name, which in turn means
// static rebuild can hash the output for caching if we ever add that.
const (
	titleSize    = 52
	siteSize     = 26
	titlePadding = 80 // left + right gutter for the title block
	titleMaxLine = 4  // hard cap on wrapped lines
)

// Renderer is the thing the admin wires up at startup so the parsed
// font + decoded background stay in memory for every generation call.
// Safe for concurrent use — all fields are read-only after construction.
type Renderer struct {
	jpFont *opentype.Font
	bg     image.Image
}

// New builds a Renderer with the embedded defaults. Callers that want
// to override the background (per-template OG backgrounds in a later
// phase) can use NewWithBackground.
func New() (*Renderer, error) {
	f, err := opentype.Parse(jpFontBytes)
	if err != nil {
		return nil, fmt.Errorf("og: parse font: %w", err)
	}
	bg, _, err := image.Decode(bytes.NewReader(defaultBGBytes))
	if err != nil {
		return nil, fmt.Errorf("og: decode default bg: %w", err)
	}
	return &Renderer{jpFont: f, bg: bg}, nil
}

// Options bundles per-render overrides for the OG card. Empty fields
// collapse to the renderer's built-in defaults (SB logo bg, two-tone
// text). Use this instead of overloading Render's signature so future
// knobs (shadow, gradient overlay, ...) slot in without breaking
// existing call sites.
type Options struct {
	// BGPath points at a custom background image on disk. Any decode
	// failure silently falls back to the embedded default.
	BGPath string
	// TextColor is a hex literal ("#RRGGBB" opaque, "#RRGGBBAA" with
	// alpha). Empty preserves the default two-tone (entry #475569,
	// site #94a3b8). When set the same colour applies to both strings
	// — the "one colour knob" simplification the admin settings ship.
	TextColor string
}

// Render composes the card and writes a PNG to w. title is the entry
// title; siteName is rendered in smaller type at the bottom.
func (r *Renderer) Render(w io.Writer, title, siteName string) error {
	return r.RenderCard(w, title, siteName, Options{})
}

// RenderWithBG is the pre-Options shim — callers that only care about
// the background path can still pass it directly.
func (r *Renderer) RenderWithBG(w io.Writer, title, siteName, bgPath string) error {
	return r.RenderCard(w, title, siteName, Options{BGPath: bgPath})
}

// RenderCard is the new full-control entrypoint. Options zero-value
// gives the original behaviour; any non-empty field overrides that
// slot. Decode / parse failures on the inputs fall back to the
// defaults so a typo in the settings panel can't 500 the pipeline —
// worst case the card still renders, just without the custom touch.
func (r *Renderer) RenderCard(w io.Writer, title, siteName string, opts Options) error {
	img := image.NewRGBA(image.Rect(0, 0, CardWidth, CardHeight))

	// Background fill: scale the (custom or embedded) bg to the card
	// size. CatmullRom keeps text legible over photographic assets.
	bg := r.bg
	if opts.BGPath != "" {
		if custom, err := loadBG(opts.BGPath); err == nil {
			bg = custom
		} else {
			log.Printf("og: custom bg %q decode failed, using default: %v", opts.BGPath, err)
		}
	}
	// Aspect-preserving cover crop: compute the largest centred
	// sub-rect of the source that matches the card's 1200×630 aspect
	// ratio, then scale that into the full canvas. Matches CSS
	// `object-fit: cover` so an author uploading an off-ratio photo
	// gets a cleanly cropped card instead of a stretched one.
	crop := coverRect(bg.Bounds(), CardWidth, CardHeight)
	xdraw.CatmullRom.Scale(img, img.Bounds(), bg, crop, xdraw.Over, nil)

	// Title: wrap to available width, center vertically.
	titleFace, err := opentype.NewFace(r.jpFont, &opentype.FaceOptions{
		Size:    titleSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("og: title face: %w", err)
	}
	defer titleFace.Close()

	// Text colour resolution: a parsed opts.TextColor wins for both
	// strings, otherwise each falls back to its original hue. Parse
	// failures (empty string / bad hex) degrade to the defaults so a
	// typo'd colour never breaks rendering.
	var titleColor color.Color = color.NRGBA{0x47, 0x55, 0x69, 0xff}
	var siteColor color.Color = color.NRGBA{0x94, 0xa3, 0xb8, 0xff}
	if c, ok := parseHexColor(opts.TextColor); ok {
		titleColor = c
		siteColor = c
	}
	lines := wrapJapanese(titleFace, title, CardWidth-titlePadding*2, titleMaxLine)

	// Vertical layout: stack lines around the card centre, leaving
	// room for the site name strip at the bottom.
	metrics := titleFace.Metrics()
	lineHeight := metrics.Height.Ceil() + 10
	totalHeight := lineHeight * len(lines)
	startY := (CardHeight-totalHeight)/2 + metrics.Ascent.Ceil()

	for i, line := range lines {
		lineWidth := font.MeasureString(titleFace, line).Ceil()
		x := (CardWidth - lineWidth) / 2
		y := startY + i*lineHeight
		drawString(img, line, titleFace, titleColor, x, y)
	}

	// Site name: smaller, bottom-right, subdued.
	if siteName != "" {
		siteFace, err := opentype.NewFace(r.jpFont, &opentype.FaceOptions{
			Size:    siteSize,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			return fmt.Errorf("og: site face: %w", err)
		}
		defer siteFace.Close()

		siteWidth := font.MeasureString(siteFace, siteName).Ceil()
		x := CardWidth - titlePadding - siteWidth
		y := CardHeight - 60
		drawString(img, siteName, siteFace, siteColor, x, y)
	}

	return png.Encode(w, img)
}

// parseHexColor accepts "#RRGGBB" or "#RRGGBBAA" hex literals and
// returns the non-premultiplied RGBA they encode. Any other shape
// (empty, missing #, wrong length, non-hex digits) returns ok=false
// so the caller falls back to defaults rather than drawing
// uninitialised state. Uppercase / lowercase hex both accepted.
func parseHexColor(s string) (color.Color, bool) {
	if len(s) != 7 && len(s) != 9 {
		return nil, false
	}
	if s[0] != '#' {
		return nil, false
	}
	var c color.NRGBA
	c.A = 0xff
	for i, pair := range [][2]int{{1, 2}, {3, 4}, {5, 6}} {
		v, ok := hexPair(s[pair[0]], s[pair[1]])
		if !ok {
			return nil, false
		}
		switch i {
		case 0:
			c.R = v
		case 1:
			c.G = v
		case 2:
			c.B = v
		}
	}
	if len(s) == 9 {
		v, ok := hexPair(s[7], s[8])
		if !ok {
			return nil, false
		}
		c.A = v
	}
	return c, true
}

func hexPair(hi, lo byte) (uint8, bool) {
	h, ok := hexNibble(hi)
	if !ok {
		return 0, false
	}
	l, ok := hexNibble(lo)
	if !ok {
		return 0, false
	}
	return h<<4 | l, true
}

func hexNibble(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// coverRect returns the largest sub-rectangle of src that matches
// the dstW/dstH aspect ratio, centred on src. Mirrors CSS
// `object-fit: cover` — authors upload whatever size they like and
// the card shows an aspect-correct centre crop rather than a
// stretched thumbnail.
func coverRect(src image.Rectangle, dstW, dstH int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 || dstW <= 0 || dstH <= 0 {
		return src
	}
	// Compare sw/sh vs dstW/dstH using cross-multiplication to avoid
	// the float round-trip on small integer ratios.
	if sw*dstH > sh*dstW {
		// Source is wider than target — crop left+right.
		newW := sh * dstW / dstH
		off := (sw - newW) / 2
		return image.Rect(src.Min.X+off, src.Min.Y, src.Min.X+off+newW, src.Max.Y)
	}
	// Source is taller (or same aspect) — crop top+bottom.
	newH := sw * dstH / dstW
	off := (sh - newH) / 2
	return image.Rect(src.Min.X, src.Min.Y+off, src.Max.X, src.Min.Y+off+newH)
}

// loadBG decodes the image at path into an image.Image, honouring the
// PNG + JPEG formats the image uploader accepts. Callers treat errors
// as "fall back to the default bg" rather than hard failures.
func loadBG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// drawString lays text onto img using a font.Drawer — straight copy of
// the common idiom from x/image/font docs. Kept inline so callers stay
// one function call away from the pixel output.
func drawString(img draw.Image, text string, face font.Face, col color.Color, x, y int) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(text)
}

// wrapJapanese breaks text into lines that fit within maxWidth pixels
// under the given face. Handles both ASCII (break on whitespace) and
// Japanese / CJK (break at any character boundary) — the latter has no
// natural word separator, so naive ASCII word-wrap would collapse a
// whole title into one un-wrappable token.
func wrapJapanese(face font.Face, text string, maxWidth, maxLines int) []string {
	if text == "" {
		return []string{""}
	}
	// Fast path: short strings just return as-is.
	if font.MeasureString(face, text).Ceil() <= maxWidth {
		return []string{text}
	}

	var lines []string
	var current []rune
	for _, r := range text {
		candidate := append([]rune{}, current...)
		candidate = append(candidate, r)
		w := font.MeasureString(face, string(candidate)).Ceil()
		if w <= maxWidth {
			current = candidate
			continue
		}
		// Overflow — commit the last safe break. Prefer ASCII word
		// boundaries (space before `r`) when available; fall back to
		// breaking at the current position for CJK runs.
		if sp := lastWordBreak(current); sp > 0 {
			lines = append(lines, string(current[:sp]))
			current = append([]rune{}, current[sp+1:]...)
			// then add r to the new line
		} else {
			lines = append(lines, string(current))
			current = current[:0]
		}
		current = append(current, r)
		if len(lines) == maxLines-1 {
			break // last line will absorb the remainder with an ellipsis
		}
	}
	if len(current) > 0 {
		lines = append(lines, string(current))
	}

	// Ellipsise overflow: if we capped at maxLines and still have text
	// in `current`, append an ellipsis to the final line.
	if len(lines) == maxLines && font.MeasureString(face, lines[len(lines)-1]).Ceil() > maxWidth {
		lines[len(lines)-1] = clipWithEllipsis(face, lines[len(lines)-1], maxWidth)
	}
	return lines
}

// lastWordBreak returns the index of the most recent space in the
// buffer, or -1 when no ASCII space exists (CJK-only content).
func lastWordBreak(buf []rune) int {
	for i := len(buf) - 1; i >= 0; i-- {
		if unicode.IsSpace(buf[i]) {
			return i
		}
	}
	return -1
}

// clipWithEllipsis trims trailing runes off s until s + "…" fits
// within maxWidth, preserving the leading content. Safe on empty.
func clipWithEllipsis(face font.Face, s string, maxWidth int) string {
	ellipsis := "…"
	for utf8.RuneCountInString(s) > 0 {
		if font.MeasureString(face, s+ellipsis).Ceil() <= maxWidth {
			return s + ellipsis
		}
		// Drop the last rune.
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return ellipsis
}
