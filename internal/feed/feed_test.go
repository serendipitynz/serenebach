package feed

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
)

func sampleOpts() Options {
	site := content.NewSite(domain.Weblog{
		ID:          1,
		Title:       "Example Blog",
		Description: "An example",
		BaseURL:     "https://example.com/",
		Lang:        "ja",
	})
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	entries := []domain.Entry{
		{
			ID: 2, AuthorID: 10, CategoryID: 100,
			Title:     "Second post <special>",
			Body:      "Hello **world**",
			Format:    "markdown",
			Status:    domain.EntryPublished,
			PostedAt:  now.Add(-1 * time.Hour),
			UpdatedAt: now.Add(-1 * time.Hour),
		},
		{
			ID: 1, AuthorID: 10, CategoryID: 100,
			Title:     "First",
			Body:      "<p>Raw HTML &amp; CDATA</p>",
			Format:    "html",
			Status:    domain.EntryPublished,
			PostedAt:  now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-24 * time.Hour),
		},
	}
	users := map[int64]domain.User{
		10: {ID: 10, Name: "ootani", DisplayName: "Takuya"},
	}
	cats := map[int64]domain.Category{
		100: {ID: 100, Name: "diary"},
	}
	return Options{Site: site, Entries: entries, Users: users, Categories: cats, Now: now}
}

// TestBuildRSSWellFormed — the output must be a parseable XML document
// with the expected channel / item shape. If any reader is ever going to
// trust our feed, it has to at least xml.Unmarshal cleanly.
func TestBuildRSSWellFormed(t *testing.T) {
	out, err := BuildRSS(sampleOpts())
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	if !strings.HasPrefix(string(out), xml.Header) {
		t.Errorf("missing XML declaration prefix")
	}
	// Parse back. encoding/xml doesn't enforce RSS semantics but will
	// reject malformed XML — the common mistake we want to catch.
	var doc struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("round-trip unmarshal: %v\n--- document:\n%s", err, out)
	}
	if doc.Version != "2.0" {
		t.Errorf("rss version = %q, want 2.0", doc.Version)
	}
	if doc.Channel.Title != "Example Blog" {
		t.Errorf("channel title = %q", doc.Channel.Title)
	}
	if len(doc.Channel.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(doc.Channel.Items))
	}
	// Newest first — matches the input order (entries slice is
	// caller-sorted), not some accidental reshuffle.
	if !strings.Contains(doc.Channel.Items[0].Title, "Second post") {
		t.Errorf("items out of order: first = %q", doc.Channel.Items[0].Title)
	}
	if doc.Channel.Items[0].Link != "https://example.com/entry/2/" {
		t.Errorf("item link = %q", doc.Channel.Items[0].Link)
	}
}

// TestBuildRSSContainsMarkdownRendered confirms format.Render() is applied
// — if we shipped the raw markdown source, readers would render `**` as
// asterisks instead of emphasis.
func TestBuildRSSContainsMarkdownRendered(t *testing.T) {
	out, err := BuildRSS(sampleOpts())
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "<strong>world</strong>") {
		t.Errorf("markdown body not rendered to HTML\n%s", s)
	}
	// Rendered HTML must land inside a CDATA section so the reader
	// doesn't see <strong> as escaped entities.
	if !strings.Contains(s, "<![CDATA[") {
		t.Errorf("description CDATA section missing")
	}
}

// TestBuildAtomWellFormed mirrors the RSS check for Atom 1.0.
func TestBuildAtomWellFormed(t *testing.T) {
	out, err := BuildAtom(sampleOpts())
	if err != nil {
		t.Fatalf("BuildAtom: %v", err)
	}
	var doc struct {
		XMLName xml.Name `xml:"feed"`
		Xmlns   string   `xml:"xmlns,attr"`
		Title   string   `xml:"title"`
		ID      string   `xml:"id"`
		Entries []struct {
			Title string `xml:"title"`
			ID    string `xml:"id"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("round-trip unmarshal: %v\n--- document:\n%s", err, out)
	}
	if doc.Xmlns != "http://www.w3.org/2005/Atom" {
		t.Errorf("atom xmlns = %q", doc.Xmlns)
	}
	if doc.ID != "https://example.com/" {
		t.Errorf("feed id = %q", doc.ID)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(doc.Entries))
	}
	if doc.Entries[0].ID != "https://example.com/entry/2/" {
		t.Errorf("entry id = %q", doc.Entries[0].ID)
	}
}

// TestBuildRSSEscapesTitleSpecials confirms XML-unsafe characters in
// titles don't break parsing — the stdlib marshaller handles this for
// element text, but the test guards the promise.
func TestBuildRSSEscapesTitleSpecials(t *testing.T) {
	out, err := BuildRSS(sampleOpts())
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	if strings.Contains(string(out), "<special>") {
		t.Errorf("unescaped <special> leaked into title\n%s", out)
	}
	if !strings.Contains(string(out), "&lt;special&gt;") {
		t.Errorf("title specials not escaped as entities\n%s", out)
	}
}

// TestLimitEntriesCapsAt20 — feeds should never balloon past the cap,
// even when a blog has hundreds of entries.
func TestLimitEntriesCapsAt20(t *testing.T) {
	opts := sampleOpts()
	opts.Entries = nil
	for i := 0; i < 50; i++ {
		opts.Entries = append(opts.Entries, domain.Entry{
			ID:       int64(i + 1),
			Title:    "e",
			Format:   "html",
			Status:   domain.EntryPublished,
			PostedAt: time.Unix(int64(1_700_000_000-i*60), 0),
		})
	}
	out, err := BuildRSS(opts)
	if err != nil {
		t.Fatalf("BuildRSS: %v", err)
	}
	if n := strings.Count(string(out), "<item>"); n != DefaultEntryLimit {
		t.Errorf("item count = %d, want %d", n, DefaultEntryLimit)
	}
}

// TestEmptyEntriesProducesValidFeed — a freshly-installed blog with no
// posts still needs to serve a parseable (if empty) feed, otherwise
// pointing readers at the URL 404s the whole experience.
func TestEmptyEntriesProducesValidFeed(t *testing.T) {
	opts := sampleOpts()
	opts.Entries = nil
	for _, f := range []func(Options) ([]byte, error){BuildRSS, BuildAtom} {
		out, err := f(opts)
		if err != nil {
			t.Errorf("build empty: %v", err)
			continue
		}
		if err := xml.Unmarshal(out, new(interface{})); err != nil {
			t.Errorf("empty feed not well-formed: %v", err)
		}
	}
}
