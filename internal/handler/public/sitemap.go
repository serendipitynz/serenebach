package public

import (
	"net/http"

	"github.com/serendipitynz/serenebach/internal/sitemap"
)

func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wlog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	if !wlog.SitemapEnabled || wlog.BaseURL == "" {
		http.NotFound(w, r)
		return
	}

	body, lastMod, err := sitemap.Build(ctx, h.Store, wlog)
	if err != nil {
		http.Error(w, "failed to build sitemap", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=1800")
	if !lastMod.IsZero() {
		w.Header().Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	_, _ = w.Write(body)
}

func (h *Handler) robotsTxt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wlog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	if !wlog.RobotsEnabled || wlog.BaseURL == "" {
		http.NotFound(w, r)
		return
	}

	body := sitemap.RobotsTxt(wlog)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(body))
}
