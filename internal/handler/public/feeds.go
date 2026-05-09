package public

import (
	"html"
	"log"
	"net/http"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/feed"
)

func (h *Handler) rssFeed(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, feed.BuildRSS, feed.MIMERSS, "public.rss")
}

// rsdFeed serves an RSD 1.0 discovery document at /rsd.xml. The
// endpoint itself is mostly metadata — the advertised XML-RPC
// interface isn't implemented (the Go port has no blog-edit API
// yet), so editors like MarsEdit will fetch this, see no working
// apiLink, and fall back to their own UI. That's acceptable: the
// tag {site_rsd} now resolves to a real URL so imported SB3
// templates stop silently emitting an empty href.
func (h *Handler) rsdFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.rsd: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	site := content.NewSite(*weblog).WithBasePath(root(r))
	body := `<?xml version="1.0"?>
<rsd version="1.0" xmlns="http://archipelago.phrasewise.com/rsd">
 <service>
  <engineName>Serene Bach</engineName>
  <engineLink>https://github.com/serendipitynz/serenebach</engineLink>
  <homePageLink>` + html.EscapeString(site.TopURL()) + `</homePageLink>
  <apis>
   <api name="Atom" blogID="" preferred="true" apiLink="` + html.EscapeString(site.AtomURL()) + `" />
  </apis>
 </service>
</rsd>
`
	w.Header().Set("Content-Type", "application/rsd+xml; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (h *Handler) atomFeed(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, feed.BuildAtom, feed.MIMEAtom, "public.atom")
}

func (h *Handler) serveFeed(w http.ResponseWriter, r *http.Request, build func(feed.Options) ([]byte, error), mime, logTag string) {
	ctx := r.Context()
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("%s: load weblog: %v", logTag, err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	entries, err := h.Store.RecentPublishedEntries(ctx, h.WID, feed.DefaultEntryLimit)
	if err != nil {
		log.Printf("%s: load entries: %v", logTag, err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	cats, users := h.lookupRefs(ctx, entries, logTag)
	body, err := build(feed.Options{
		Site:       content.NewSite(*weblog).WithBasePath(root(r)),
		Entries:    entries,
		Users:      users,
		Categories: cats,
	})
	if err != nil {
		log.Printf("%s: build: %v", logTag, err)
		http.Error(w, "failed to build feed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	if _, err := w.Write(body); err != nil {
		log.Printf("%s: write: %v", logTag, err)
	}
}
