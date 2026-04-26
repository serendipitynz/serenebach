// Package feed builds RSS 2.0 and Atom 1.0 documents for the public site.
// The output intentionally targets modern specs (not SB3's shipped
// RSS 1.0 / Atom 0.3 templates) so mainstream readers and aggregators
// accept the feeds without falling back to legacy parsers.
package feed

import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
)

// DefaultEntryLimit caps how many entries ship in one feed. 20 is the
// informal convention — enough for reader catch-up, small enough to keep
// payloads under a few hundred KB.
const DefaultEntryLimit = 20

// MIME types exposed for HTTP handlers.
const (
	MIMERSS  = "application/rss+xml; charset=utf-8"
	MIMEAtom = "application/atom+xml; charset=utf-8"
)

// Options bundles what both builders need. Entries must already be sorted
// newest-first (the caller's responsibility — matches the public handler
// pattern).
type Options struct {
	Site    content.Site
	Entries []domain.Entry
	// Users is looked up by AuthorID to emit <author> / <dc:creator>.
	// Missing authors fall back to an empty name silently (historical SB
	// behaviour — partial data shouldn't break the feed).
	Users map[int64]domain.User
	// Categories is looked up by CategoryID to emit <category>. Missing
	// categories are simply omitted.
	Categories map[int64]domain.Category
	// Now is the feed's reference time (used as the channel-level updated
	// timestamp when the entry list is empty). Defaults to time.Now().
	Now time.Time
}

// BuildRSS renders an RSS 2.0 document with <content:encoded> carrying the
// rendered HTML body. Returns the XML including the declaration.
func BuildRSS(opts Options) ([]byte, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	channel := rssChannel{
		Title:         opts.Site.Weblog.Title,
		Link:          opts.Site.TopURL(),
		Description:   opts.Site.Weblog.Description,
		Language:      opts.Site.Weblog.Lang,
		LastBuildDate: now.UTC().Format(time.RFC1123Z),
		Generator:     "serenebach",
		AtomLink: &atomLinkSelf{
			Href: opts.Site.RSSURL(),
			Rel:  "self",
			Type: MIMERSS,
		},
	}

	for _, e := range limitEntries(opts.Entries) {
		desc, err := format.Render(e.Body, e.Format)
		if err != nil {
			return nil, fmt.Errorf("feed: render entry %d body: %w", e.ID, err)
		}
		permalink := opts.Site.EntryPermalink(e)
		item := rssItem{
			Title:       e.Title,
			Link:        permalink,
			GUID:        &rssGUID{Value: permalink, IsPermaLink: "true"},
			PubDate:     e.PostedAt.UTC().Format(time.RFC1123Z),
			Description: cdata{Value: desc},
			Creator:     authorName(opts.Users, e.AuthorID),
		}
		if c, ok := opts.Categories[e.CategoryID]; ok && c.Name != "" {
			item.Category = c.Name
		}
		channel.Items = append(channel.Items, item)
	}

	doc := rssDocument{
		Version:   "2.0",
		ContentNS: "http://purl.org/rss/1.0/modules/content/",
		DCNS:      "http://purl.org/dc/elements/1.1/",
		AtomNS:    "http://www.w3.org/2005/Atom",
		Channel:   channel,
	}
	return marshal(doc)
}

// BuildAtom renders an Atom 1.0 document (RFC 4287). Uses the feed's
// latest entry as <updated>; falls back to Now when empty.
func BuildAtom(opts Options) ([]byte, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	top := opts.Site.TopURL()
	feedID := top
	updated := now.UTC().Format(time.RFC3339)
	entries := limitEntries(opts.Entries)
	if len(entries) > 0 && !entries[0].UpdatedAt.IsZero() {
		updated = entries[0].UpdatedAt.UTC().Format(time.RFC3339)
	} else if len(entries) > 0 {
		updated = entries[0].PostedAt.UTC().Format(time.RFC3339)
	}

	doc := atomFeed{
		Xmlns:   "http://www.w3.org/2005/Atom",
		XMLLang: opts.Site.Weblog.Lang,
		Title:   opts.Site.Weblog.Title,
		Subtitle: &atomText{
			Type:  "text",
			Value: opts.Site.Weblog.Description,
		},
		Links: []atomLink{
			{Rel: "alternate", Type: "text/html", Href: top},
			{Rel: "self", Type: MIMEAtom, Href: opts.Site.AtomURL()},
		},
		ID:        feedID,
		Updated:   updated,
		Generator: &atomGenerator{Value: "serenebach"},
	}

	for _, e := range entries {
		desc, err := format.Render(e.Body, e.Format)
		if err != nil {
			return nil, fmt.Errorf("feed: render entry %d body: %w", e.ID, err)
		}
		permalink := opts.Site.EntryPermalink(e)
		entryUpdated := e.UpdatedAt
		if entryUpdated.IsZero() {
			entryUpdated = e.PostedAt
		}
		entry := atomEntry{
			Title:     e.Title,
			ID:        permalink,
			Links:     []atomLink{{Rel: "alternate", Type: "text/html", Href: permalink}},
			Published: e.PostedAt.UTC().Format(time.RFC3339),
			Updated:   entryUpdated.UTC().Format(time.RFC3339),
			Content: &atomContent{
				Type:  "html",
				Value: desc,
			},
			Author: &atomAuthor{Name: authorNameOrDefault(opts.Users, e.AuthorID, opts.Site.Weblog.Title)},
		}
		if c, ok := opts.Categories[e.CategoryID]; ok && c.Name != "" {
			entry.Categories = []atomCategory{{Term: c.Name}}
		}
		doc.Entries = append(doc.Entries, entry)
	}

	return marshal(doc)
}

func limitEntries(es []domain.Entry) []domain.Entry {
	if len(es) > DefaultEntryLimit {
		return es[:DefaultEntryLimit]
	}
	return es
}

func authorName(users map[int64]domain.User, id int64) string {
	if u, ok := users[id]; ok {
		if u.DisplayName != "" {
			return u.DisplayName
		}
		return u.Name
	}
	return ""
}

func authorNameOrDefault(users map[int64]domain.User, id int64, fallback string) string {
	if n := authorName(users, id); n != "" {
		return n
	}
	return fallback
}

func marshal(doc interface{}) ([]byte, error) {
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	// Prepend the XML declaration so aggregators that sniff the first
	// bytes (most of them) find it where they expect.
	buf := make([]byte, 0, len(out)+40)
	buf = append(buf, []byte(xml.Header)...)
	buf = append(buf, out...)
	buf = append(buf, '\n')
	return buf, nil
}

// ---- RSS 2.0 structs -------------------------------------------------------

type rssDocument struct {
	XMLName   xml.Name   `xml:"rss"`
	Version   string     `xml:"version,attr"`
	ContentNS string     `xml:"xmlns:content,attr"`
	DCNS      string     `xml:"xmlns:dc,attr"`
	AtomNS    string     `xml:"xmlns:atom,attr"`
	Channel   rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string        `xml:"title"`
	Link          string        `xml:"link"`
	Description   string        `xml:"description"`
	Language      string        `xml:"language,omitempty"`
	LastBuildDate string        `xml:"lastBuildDate,omitempty"`
	Generator     string        `xml:"generator,omitempty"`
	AtomLink      *atomLinkSelf `xml:"atom:link,omitempty"`
	Items         []rssItem     `xml:"item"`
}

// atomLinkSelf embeds an atom:link self-reference inside the RSS channel —
// a widely-supported extension that lets readers discover the feed's
// canonical URL independent of how they fetched it.
type atomLinkSelf struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        *rssGUID `xml:"guid,omitempty"`
	PubDate     string   `xml:"pubDate,omitempty"`
	Category    string   `xml:"category,omitempty"`
	Creator     string   `xml:"dc:creator,omitempty"`
	Description cdata    `xml:"description"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr"`
}

// cdata wraps a string so encoding/xml emits it inside a CDATA section —
// readers would otherwise see double-escaped HTML (the Body is already
// rendered to HTML by format.Render, so we need to ship it raw).
type cdata struct {
	Value string `xml:",cdata"`
}

// ---- Atom 1.0 structs ------------------------------------------------------

type atomFeed struct {
	XMLName   xml.Name       `xml:"feed"`
	Xmlns     string         `xml:"xmlns,attr"`
	XMLLang   string         `xml:"xml:lang,attr,omitempty"`
	Title     string         `xml:"title"`
	Subtitle  *atomText      `xml:"subtitle,omitempty"`
	Links     []atomLink     `xml:"link"`
	ID        string         `xml:"id"`
	Updated   string         `xml:"updated"`
	Generator *atomGenerator `xml:"generator,omitempty"`
	Entries   []atomEntry    `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomText struct {
	Type  string `xml:"type,attr,omitempty"`
	Value string `xml:",chardata"`
}

type atomGenerator struct {
	Value string `xml:",chardata"`
}

type atomEntry struct {
	Title      string         `xml:"title"`
	ID         string         `xml:"id"`
	Links      []atomLink     `xml:"link"`
	Published  string         `xml:"published"`
	Updated    string         `xml:"updated"`
	Author     *atomAuthor    `xml:"author,omitempty"`
	Categories []atomCategory `xml:"category,omitempty"`
	Content    *atomContent   `xml:"content,omitempty"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

// atomContent carries the rendered HTML body inside CDATA so reader
// parsers don't need to unescape double-encoded entities. The SB3 feed
// template did the same with mode="escaped" + CDATA; Atom 1.0 uses
// type="html" for this shape.
type atomContent struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",cdata"`
}
