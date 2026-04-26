package og

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// parseHexColor accepts 7- and 9-char hex literals; anything else —
// empty, wrong length, missing #, junk digits — must report !ok so the
// renderer silently falls back to the default two-tone.
func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in      string
		ok      bool
		r, g, b uint8
		a       uint8
	}{
		{"#ffffff", true, 0xff, 0xff, 0xff, 0xff},
		{"#000000", true, 0x00, 0x00, 0x00, 0xff},
		{"#475569", true, 0x47, 0x55, 0x69, 0xff},
		{"#00000000", true, 0x00, 0x00, 0x00, 0x00},
		{"#112233aa", true, 0x11, 0x22, 0x33, 0xaa},
		{"#AABBCC", true, 0xAA, 0xBB, 0xCC, 0xff},
		{"", false, 0, 0, 0, 0},
		{"abc", false, 0, 0, 0, 0},
		{"#abc", false, 0, 0, 0, 0},
		{"#GGGGGG", false, 0, 0, 0, 0},
		{"#1234567", false, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		got, ok := parseHexColor(tc.in)
		if ok != tc.ok {
			t.Errorf("%q ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		nrgba, _ := got.(color.NRGBA)
		if nrgba.R != tc.r || nrgba.G != tc.g || nrgba.B != tc.b || nrgba.A != tc.a {
			t.Errorf("%q => %+v, want (%x,%x,%x,%x)", tc.in, nrgba, tc.r, tc.g, tc.b, tc.a)
		}
	}
}

// coverRect must centre-crop to the target aspect regardless of the
// input's aspect so off-ratio uploads render without distortion.
func TestCoverRectMatchesTargetAspect(t *testing.T) {
	cases := []struct {
		name               string
		src                image.Rectangle
		dstW, dstH         int
		wantW, wantH       int
		wantMinX, wantMinY int
	}{
		{"wider-source", image.Rect(0, 0, 2000, 500), 1200, 630,
			// 2000x500 vs 1200x630 → source wider, crop left+right
			// target ratio 1200/630 ≈ 1.904, newW = 500 * 1200/630 = 952
			952, 500, (2000 - 952) / 2, 0},
		{"taller-source", image.Rect(0, 0, 600, 900), 1200, 630,
			// 600x900 vs 1200x630 → source taller, crop top+bottom
			// newH = 600 * 630/1200 = 315
			600, 315, 0, (900 - 315) / 2},
		{"exact-aspect", image.Rect(0, 0, 1200, 630), 1200, 630,
			// already matches; no crop.
			1200, 630, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coverRect(tc.src, tc.dstW, tc.dstH)
			if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
				t.Errorf("size = %dx%d, want %dx%d", got.Dx(), got.Dy(), tc.wantW, tc.wantH)
			}
			if got.Min.X != tc.wantMinX || got.Min.Y != tc.wantMinY {
				t.Errorf("origin = (%d,%d), want (%d,%d)",
					got.Min.X, got.Min.Y, tc.wantMinX, tc.wantMinY)
			}
		})
	}
}

func TestRendererNewSucceeds(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r == nil {
		t.Fatalf("nil renderer")
	}
}

func TestRenderProducesValidPNGAtCardSize(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := r.Render(&buf, "hello world", "Serene Bach"); err != nil {
		t.Fatalf("Render: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != CardWidth || b.Dy() != CardHeight {
		t.Errorf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), CardWidth, CardHeight)
	}
}

func TestRenderHandlesLongJapaneseTitle(t *testing.T) {
	// Long CJK title with no whitespace — wrapping must happen at
	// character boundaries, else the render function would refuse to
	// break and produce a single-line overflowing image.
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	title := strings.Repeat("テンプレート編集画面の再設計", 4) // ~56 chars
	var buf bytes.Buffer
	if err := r.Render(&buf, title, "Serene Bach"); err != nil {
		t.Fatalf("Render long title: %v", err)
	}
	// Decodable output implies the internal pipeline didn't panic /
	// produce corrupt bytes.
	if _, err := png.Decode(&buf); err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
}

func TestRenderHandlesEmptyTitle(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := r.Render(&buf, "", "Serene Bach"); err != nil {
		t.Fatalf("empty title Render: %v", err)
	}
	if _, err := png.Decode(&buf); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestWrapJapaneseSplitsAtCharBoundary(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(r.jpFont, &opentype.FaceOptions{
		Size:    48,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	// CJK title with no whitespace; expect wrapping at character
	// boundaries rather than a single overflowing line.
	lines := wrapJapanese(face, "はるかむかしのむかしがありました", 600, 4)
	if len(lines) < 2 {
		t.Errorf("expected at least 2 lines; got %d: %v", len(lines), lines)
	}
}
