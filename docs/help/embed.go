// Package helpdocs ships the admin help system's Markdown source as
// an embedded filesystem so the rendered /admin/help routes serve
// bytes directly from the binary — no separate deployment step.
//
// Docs are organised by locale: `ja/*.md` is the source-of-truth
// (the original author writes in Japanese), `en/*.md` and any
// future locale dirs hold translations. A missing translation for a
// given slug falls through to the default locale, and the handler
// surfaces a "not translated yet" banner to the reader.
//
// Each Markdown file carries a small YAML-ish frontmatter block
// (title / slug / order) that drives the sidebar. The loader parses
// every file eagerly at init; render uses goldmark on the body
// below the frontmatter delimiter.
package helpdocs

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed ja/*.md en/*.md
var files embed.FS

// DefaultLocale is the fall-through language when a slug isn't
// translated into the requested locale. Japanese is the source lang
// because the author writes the original prose there.
const DefaultLocale = "ja"

// Page is one parsed help document. Body is the raw Markdown after
// the frontmatter block; Render produces the HTML.
type Page struct {
	Locale string
	Slug   string
	Title  string
	Order  int
	Body   string
}

var (
	// pages[locale][slug] — locale catalogues keyed by slug.
	pages = map[string]map[string]*Page{}
	// ordered[locale] — sidebar-order list per locale.
	ordered = map[string][]*Page{}

	renderer goldmark.Markdown
)

func init() {
	locales := []string{DefaultLocale, "en"}
	for _, loc := range locales {
		pages[loc] = map[string]*Page{}
		entries, err := files.ReadDir(loc)
		if err != nil {
			// Empty / missing locale dir is not fatal — an operator can
			// ship the binary with only the default lang populated.
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := loc + "/" + e.Name()
			raw, err := files.ReadFile(path)
			if err != nil {
				panic("helpdocs: read " + path + ": " + err.Error())
			}
			p, err := parsePage(raw)
			if err != nil {
				panic("helpdocs: parse " + path + ": " + err.Error())
			}
			if p.Slug == "" {
				panic("helpdocs: " + path + ": missing slug frontmatter")
			}
			if _, dup := pages[loc][p.Slug]; dup {
				panic("helpdocs: duplicate slug " + p.Slug + " in " + loc)
			}
			p.Locale = loc
			pages[loc][p.Slug] = p
			ordered[loc] = append(ordered[loc], p)
		}
		sort.Slice(ordered[loc], func(i, j int) bool {
			a, b := ordered[loc][i], ordered[loc][j]
			if a.Order != b.Order {
				return a.Order < b.Order
			}
			return a.Slug < b.Slug
		})
	}
	renderer = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
}

// Lookup resolves slug in locale. Returns (page, fellBack) where
// fellBack=true means the caller requested a locale that didn't
// ship a translation for this slug, and the default locale's page
// is being served as a stand-in. (page, _) is nil when even the
// default locale is missing the slug — callers serve 404.
func Lookup(slug, locale string) (*Page, bool) {
	if cat, ok := pages[locale]; ok {
		if p, ok := cat[slug]; ok {
			return p, false
		}
	}
	if locale == DefaultLocale {
		return nil, false
	}
	if p, ok := pages[DefaultLocale][slug]; ok {
		return p, true
	}
	return nil, false
}

// Index returns the sidebar-ordered page list for locale. Pages
// missing a translation in the requested locale are substituted by
// the default locale's entry so the sidebar stays complete — that
// way a reader browsing English still sees the full catalogue and
// can click through to fall-back content. Each returned page's
// Locale field tells the caller whether it's the requested locale
// or the default.
func Index(locale string) []*Page {
	if locale == DefaultLocale {
		return ordered[DefaultLocale]
	}
	// Merge: start with the default locale's order so no slug is
	// missed, but swap in the requested locale's page when available.
	localeCat := pages[locale]
	out := make([]*Page, 0, len(ordered[DefaultLocale]))
	for _, p := range ordered[DefaultLocale] {
		if loc, ok := localeCat[p.Slug]; ok {
			out = append(out, loc)
		} else {
			out = append(out, p)
		}
	}
	return out
}

// Render produces the HTML form of the page body. Safe to call
// concurrently — goldmark's parser/renderer is stateless after
// construction.
func (p *Page) Render() (string, error) {
	var buf bytes.Buffer
	if err := renderer.Convert([]byte(p.Body), &buf); err != nil {
		return "", fmt.Errorf("helpdocs: render %s/%s: %w", p.Locale, p.Slug, err)
	}
	return buf.String(), nil
}

// parsePage pulls the YAML-ish frontmatter block off the head of the
// Markdown source. Kept deliberately minimal — three keys, always
// author-controlled — so a tiny line-based parser beats pulling in a
// full YAML dependency.
func parsePage(raw []byte) (*Page, error) {
	const fence = "---\n"
	if !bytes.HasPrefix(raw, []byte(fence)) {
		return nil, fmt.Errorf("missing frontmatter fence")
	}
	rest := raw[len(fence):]
	end := bytes.Index(rest, []byte("\n"+fence))
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := rest[:end]
	body := rest[end+len("\n"+fence):]
	p := &Page{Body: string(body)}
	for _, line := range strings.Split(string(fm), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			return nil, fmt.Errorf("malformed frontmatter line %q", line)
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "title":
			p.Title = val
		case "slug":
			p.Slug = val
		case "order":
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("order: %w", err)
			}
			p.Order = n
		}
	}
	return p, nil
}
