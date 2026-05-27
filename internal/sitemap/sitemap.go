// Package sitemap builds sitemap.xml and robots.txt for the public site.
// Both the dynamic HTTP route and the static rebuild share BuildFromInput
// so the output is identical.
package sitemap

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// Input captures everything the builders need. Callers populate this from
// repo queries; the sitemap package itself never touches the DB so it stays
// trivial to test.
type Input struct {
	Weblog           *domain.Weblog
	Entries          []domain.Entry
	Categories       []domain.Category
	Tags             []domain.Tag
	Pages            []domain.Page
	CategoryLastMods map[int64]time.Time
	TagLastMods      map[int64]time.Time
}

// urlEntry matches the sitemap protocol 0.9 <url> element.
type urlEntry struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// urlSet is the root element of a sitemap document.
type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	Xmlns   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

// Build fetches data from the store and assembles the sitemap. Used by the
// dynamic HTTP handler.
func Build(ctx context.Context, store *repo.Store, weblog *domain.Weblog) ([]byte, time.Time, error) {
	in, err := loadInput(ctx, store, weblog)
	if err != nil {
		return nil, time.Time{}, err
	}
	return BuildFromInput(in)
}

// BuildFromInput assembles the sitemap from a pre-populated Input. Used by
// the static rebuild so it can reuse the same datasets already in memory.
func BuildFromInput(in Input) ([]byte, time.Time, error) {
	if in.Weblog == nil || in.Weblog.BaseURL == "" {
		return nil, time.Time{}, fmt.Errorf("sitemap: base_url is required")
	}

	base := in.Weblog.BaseURL
	if base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}

	urls := assembleURLs(base, in)

	us := urlSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	out, err := xml.MarshalIndent(us, "", "  ")
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("sitemap: marshal: %w", err)
	}
	// Prepend XML declaration manually; encoding/xml does not emit it for us.
	body := append([]byte(xml.Header), out...)

	// lastmod for the whole document = newest lastmod among all URLs
	var docLastMod time.Time
	for _, u := range urls {
		if u.LastMod != "" {
			if t, err := time.Parse("2006-01-02", u.LastMod); err == nil && t.After(docLastMod) {
				docLastMod = t
			}
		}
	}

	return body, docLastMod, nil
}

func assembleURLs(base string, in Input) []urlEntry {
	urls := make([]urlEntry, 0, 1+len(in.Entries)+len(in.Categories)+len(in.Tags)+len(in.Pages))
	seen := make(map[string]struct{}, cap(urls))

	// Top page
	urls = append(urls, urlEntry{
		Loc:        base + "/",
		LastMod:    lastModStr(in.Entries),
		ChangeFreq: "daily",
		Priority:   "1.0",
	})
	seen[base+"/"] = struct{}{}

	// Entries (published, hidden-category already excluded by caller)
	for _, e := range in.Entries {
		if e.NoIndex {
			// noindex entries stay published on home / list but must
			// not advertise their URL to crawlers. Skipping here (not
			// in in.Entries) keeps the top-page lastmod calc intact.
			continue
		}
		loc := base + permalink(e)
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}
		urls = append(urls, urlEntry{
			Loc:        loc,
			LastMod:    entryLastModStr(e),
			ChangeFreq: "monthly",
			Priority:   "0.7",
		})
	}

	// Categories (non-hidden)
	for _, c := range in.Categories {
		loc := base + categoryPermalink(c)
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}
		urls = append(urls, urlEntry{
			Loc:        loc,
			LastMod:    modStr(in.CategoryLastMods[c.ID]),
			ChangeFreq: "weekly",
			Priority:   "0.5",
		})
	}

	// Tags
	for _, t := range in.Tags {
		loc := base + "/tag/" + t.Slug + "/"
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}
		urls = append(urls, urlEntry{
			Loc:        loc,
			LastMod:    modStr(in.TagLastMods[t.ID]),
			ChangeFreq: "weekly",
			Priority:   "0.4",
		})
	}

	// Flat pages (published)
	for _, p := range in.Pages {
		if p.NoIndex {
			continue // noindex pages stay published but unindexed (same as entries)
		}
		loc := base + p.Slug
		if loc[len(loc)-1] != '/' {
			loc += "/"
		}
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}
		urls = append(urls, urlEntry{
			Loc:        loc,
			LastMod:    pageLastModStr(p),
			ChangeFreq: "monthly",
			Priority:   "0.6",
		})
	}

	// Deterministic ordering makes tests stable and diffs readable.
	sort.Slice(urls, func(i, j int) bool {
		return urls[i].Loc < urls[j].Loc
	})
	return urls
}

// loadInput fetches datasets from the store and assembles an Input.
func loadInput(ctx context.Context, store *repo.Store, weblog *domain.Weblog) (Input, error) {
	wid := weblog.ID
	entries, err := store.AllPublishedEntries(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load entries: %w", err)
	}

	cats, err := store.AllCategories(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load categories: %w", err)
	}
	visibleCats := make([]domain.Category, 0, len(cats))
	for _, c := range cats {
		if !c.Hidden {
			visibleCats = append(visibleCats, c)
		}
	}

	// Filter out entries whose category is hidden
	filteredEntries := make([]domain.Entry, 0, len(entries))
	for _, e := range entries {
		if e.CategoryID == domain.Uncategorized {
			filteredEntries = append(filteredEntries, e)
			continue
		}
		cat, ok := findCategory(cats, e.CategoryID)
		if ok && !cat.Hidden {
			filteredEntries = append(filteredEntries, e)
		}
	}

	tags, err := store.AllTags(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load tags: %w", err)
	}

	pages, err := store.PublishedPages(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load pages: %w", err)
	}

	catLastMods, err := store.SitemapCategoryLastMods(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load category lastmods: %w", err)
	}

	tagLastMods, err := store.SitemapTagLastMods(ctx, wid)
	if err != nil {
		return Input{}, fmt.Errorf("sitemap: load tag lastmods: %w", err)
	}

	return Input{
		Weblog:           weblog,
		Entries:          filteredEntries,
		Categories:       visibleCats,
		Tags:             tags,
		Pages:            pages,
		CategoryLastMods: catLastMods,
		TagLastMods:      tagLastMods,
	}, nil
}

func findCategory(cats []domain.Category, id int64) (domain.Category, bool) {
	for _, c := range cats {
		if c.ID == id {
			return c, true
		}
	}
	return domain.Category{}, false
}

func permalink(e domain.Entry) string {
	if e.Slug != "" {
		return "/entry/" + e.Slug + "/"
	}
	return fmt.Sprintf("/entry/%d/", e.ID)
}

func categoryPermalink(c domain.Category) string {
	if c.Slug != "" {
		return "/category/" + c.Slug + "/"
	}
	return fmt.Sprintf("/category/%d/", c.ID)
}

func entryLastModStr(e domain.Entry) string {
	if !e.UpdatedAt.IsZero() {
		return e.UpdatedAt.Format("2006-01-02")
	}
	if !e.PostedAt.IsZero() {
		return e.PostedAt.Format("2006-01-02")
	}
	return ""
}

func pageLastModStr(p domain.Page) string {
	if !p.UpdatedAt.IsZero() {
		return p.UpdatedAt.Format("2006-01-02")
	}
	if !p.CreatedAt.IsZero() {
		return p.CreatedAt.Format("2006-01-02")
	}
	return ""
}

func modStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// lastModStr returns the newest updated_at among entries, or empty if none.
func lastModStr(entries []domain.Entry) string {
	var newest time.Time
	for _, e := range entries {
		var t time.Time
		if !e.UpdatedAt.IsZero() {
			t = e.UpdatedAt
		} else if !e.PostedAt.IsZero() {
			t = e.PostedAt
		}
		if t.After(newest) {
			newest = t
		}
	}
	return modStr(newest)
}
