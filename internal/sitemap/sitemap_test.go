package sitemap

import (
	"encoding/xml"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestBuildFromInput_emptyWeblog(t *testing.T) {
	in := Input{
		Weblog: &domain.Weblog{BaseURL: "https://blog.example.com"},
	}
	body, lastMod, err := BuildFromInput(in)
	if err != nil {
		t.Fatalf("BuildFromInput: %v", err)
	}
	if !lastMod.IsZero() {
		t.Error("expected zero lastMod for empty weblog")
	}
	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		t.Fatalf("xml unmarshal: %v", err)
	}
	if len(us.URLs) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(us.URLs))
	}
	if us.URLs[0].Loc != "https://blog.example.com/" {
		t.Errorf("top URL = %q, want %q", us.URLs[0].Loc, "https://blog.example.com/")
	}
}

func TestBuildFromInput_mixedEntries(t *testing.T) {
	now := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	in := Input{
		Weblog: &domain.Weblog{BaseURL: "https://blog.example.com"},
		Entries: []domain.Entry{
			{ID: 1, Slug: "hello", PostedAt: now, UpdatedAt: now},
		},
		Categories: []domain.Category{
			{ID: 1, Slug: "tech"},
		},
		Tags: []domain.Tag{
			{ID: 1, Slug: "go"},
		},
		Pages: []domain.Page{
			{Slug: "/about", CreatedAt: now, UpdatedAt: now},
		},
		CategoryLastMods: map[int64]time.Time{1: now},
		TagLastMods:      map[int64]time.Time{1: now},
	}
	body, _, err := BuildFromInput(in)
	if err != nil {
		t.Fatalf("BuildFromInput: %v", err)
	}
	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		t.Fatalf("xml unmarshal: %v", err)
	}
	locs := make([]string, len(us.URLs))
	for i, u := range us.URLs {
		locs[i] = u.Loc
	}
	want := []string{
		"https://blog.example.com/",
		"https://blog.example.com/about/",
		"https://blog.example.com/category/tech/",
		"https://blog.example.com/entry/hello/",
		"https://blog.example.com/tag/go/",
	}
	if len(locs) != len(want) {
		t.Fatalf("URLs = %v, want %v", locs, want)
	}
	for i := range want {
		if locs[i] != want[i] {
			t.Errorf("URL[%d] = %q, want %q", i, locs[i], want[i])
		}
	}
}

func TestBuildFromInput_skipsNoIndexEntries(t *testing.T) {
	older := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	in := Input{
		Weblog: &domain.Weblog{BaseURL: "https://blog.example.com"},
		Entries: []domain.Entry{
			{ID: 1, Slug: "visible", PostedAt: older, UpdatedAt: older},
			// noindex entry is also the most recently updated — it must
			// drop out of the <url> list but still drive the top-page
			// lastmod (it stays published on home / list, just unindexed).
			{ID: 2, Slug: "hidden-from-index", NoIndex: true, PostedAt: newer, UpdatedAt: newer},
		},
	}
	body, _, err := BuildFromInput(in)
	if err != nil {
		t.Fatalf("BuildFromInput: %v", err)
	}
	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		t.Fatalf("xml unmarshal: %v", err)
	}

	var topLastMod string
	for _, u := range us.URLs {
		if u.Loc == "https://blog.example.com/entry/hidden-from-index/" {
			t.Error("noindex entry should not appear in the urlset")
		}
		if u.Loc == "https://blog.example.com/" {
			topLastMod = u.LastMod
		}
	}
	if !containsLoc(us.URLs, "https://blog.example.com/entry/visible/") {
		t.Error("indexed entry should still appear")
	}
	// noindex ≠ unpublished: the top-page lastmod must still reflect the
	// newer (noindex) entry's date.
	if topLastMod != "2026-05-20" {
		t.Errorf("top-page lastmod = %q, want 2026-05-20 (noindex entry kept in lastmod calc)", topLastMod)
	}
}

func containsLoc(urls []urlEntry, loc string) bool {
	for _, u := range urls {
		if u.Loc == loc {
			return true
		}
	}
	return false
}

func TestBuildFromInput_doesNotLeakExcluded(t *testing.T) {
	now := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	in := Input{
		Weblog: &domain.Weblog{BaseURL: "https://blog.example.com"},
		Entries: []domain.Entry{
			{ID: 1, CategoryID: 10, Slug: "visible", PostedAt: now, UpdatedAt: now},
		},
		Categories: []domain.Category{
			{ID: 10, Slug: "public-cat", Hidden: false},
		},
		CategoryLastMods: map[int64]time.Time{10: now},
	}
	body, _, err := BuildFromInput(in)
	if err != nil {
		t.Fatalf("BuildFromInput: %v", err)
	}
	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		t.Fatalf("xml unmarshal: %v", err)
	}
	for _, u := range us.URLs {
		if u.Loc == "https://blog.example.com/entry/hidden-cat/" {
			t.Error("excluded entry should not appear")
		}
		if u.Loc == "https://blog.example.com/category/secret-cat/" {
			t.Error("excluded category should not appear")
		}
	}
}

func TestBuildFromInput_noDuplicates(t *testing.T) {
	now := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	in := Input{
		Weblog: &domain.Weblog{BaseURL: "https://blog.example.com"},
		Entries: []domain.Entry{
			{ID: 1, Slug: "dup", PostedAt: now, UpdatedAt: now},
		},
		Pages: []domain.Page{
			{Slug: "/entry/dup/", CreatedAt: now}, // would collide without dedup
		},
	}
	body, _, err := BuildFromInput(in)
	if err != nil {
		t.Fatalf("BuildFromInput: %v", err)
	}
	var us urlSet
	if err := xml.Unmarshal(body, &us); err != nil {
		t.Fatalf("xml unmarshal: %v", err)
	}
	seen := make(map[string]int)
	for _, u := range us.URLs {
		seen[u.Loc]++
	}
	for loc, n := range seen {
		if n > 1 {
			t.Errorf("duplicate URL %q (count %d)", loc, n)
		}
	}
}

func TestRobotsTxt(t *testing.T) {
	w := &domain.Weblog{BaseURL: "https://blog.example.com"}
	got := RobotsTxt(w)
	want := "User-agent: *\nAllow: /\n\nSitemap: https://blog.example.com/sitemap.xml\n"
	if got != want {
		t.Errorf("RobotsTxt = %q, want %q", got, want)
	}
}

func TestRobotsTxt_trailingSlash(t *testing.T) {
	w := &domain.Weblog{BaseURL: "https://blog.example.com/"}
	got := RobotsTxt(w)
	want := "User-agent: *\nAllow: /\n\nSitemap: https://blog.example.com/sitemap.xml\n"
	if got != want {
		t.Errorf("RobotsTxt = %q, want %q", got, want)
	}
}
