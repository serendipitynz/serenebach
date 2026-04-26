package admin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/analytics"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// analyticsWindowDays is the fixed list of selectable spans on the
// analytics page. Labels are resolved per-request via the i18n bundle
// so a switch to en doesn't leak the JP labels.
var analyticsWindowDays = []int{7, 30, 90}

type analyticsWindow struct {
	Label string
	Days  int
}

// localizedAnalyticsWindows resolves the standard window list against
// the active locale so the template can iterate { .Label, .Days }
// without knowing about i18n keys.
func localizedAnalyticsWindows(r *http.Request) []analyticsWindow {
	out := make([]analyticsWindow, 0, len(analyticsWindowDays))
	for _, d := range analyticsWindowDays {
		out = append(out, analyticsWindow{
			Label: tr(r, fmt.Sprintf("analytics.window.%d", d)),
			Days:  d,
		})
	}
	return out
}

// topRow is analytics.EntryHit decorated with the entry title so the
// dashboard can surface something friendlier than "entry 123".
type topRow struct {
	analytics.EntryHit
	Title string
}

type analyticsPageData struct {
	pageBase
	Disabled    bool
	CurrentDays int
	CurrentSort analytics.TopEntrySort
	Windows     []analyticsWindow
	Summary     *analytics.Summary
	Top         []topRow
	Daily       []analytics.DayPoint
}

func (h *Handler) mountAnalytics(r chi.Router) {
	r.Get("/analytics", h.analyticsHome)
}

func (h *Handler) analyticsHome(w http.ResponseWriter, r *http.Request) {
	data := analyticsPageData{
		pageBase: pageBase{
			Title:      tr(r, "analytics.title"),
			ActiveMenu: "analytics",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		CurrentDays: 30,
		Windows:     localizedAnalyticsWindows(r),
	}
	if h.Analytics == nil {
		data.Disabled = true
		renderMain(w, r, pageAnalytics, data)
		return
	}
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 365 {
			data.CurrentDays = n
		}
	}
	data.CurrentSort = parseTopSort(r.URL.Query().Get("sort"))

	since := time.Now().Add(-time.Duration(data.CurrentDays) * 24 * time.Hour)

	sum, err := h.Analytics.Summarise(r.Context(), since)
	if err != nil {
		log.Printf("admin.analytics: summary: %v", err)
		http.Error(w, "failed to load analytics", http.StatusInternalServerError)
		return
	}
	data.Summary = sum

	hits, err := h.Analytics.TopEntries(r.Context(), h.Store.DB(), since, 10, data.CurrentSort)
	if err != nil {
		log.Printf("admin.analytics: top: %v", err)
	}
	data.Top = h.enrichTopRows(r, hits)

	if daily, err := h.Analytics.DailyViews(r.Context(), since); err != nil {
		log.Printf("admin.analytics: daily: %v", err)
	} else {
		data.Daily = daily
	}

	renderMain(w, r, pageAnalytics, data)
}

func parseTopSort(raw string) analytics.TopEntrySort {
	switch analytics.TopEntrySort(raw) {
	case analytics.SortByLikes:
		return analytics.SortByLikes
	case analytics.SortByStamps:
		return analytics.SortByStamps
	}
	return analytics.SortByViews
}

// enrichTopRows turns analytics.EntryHit rows into topRow values with
// a human-readable entry title attached. Missing entries (deleted /
// wrong wid) fall back to the "entry <id>" shorthand so the table
// never shows blank cells.
func (h *Handler) enrichTopRows(r *http.Request, hits []analytics.EntryHit) []topRow {
	out := make([]topRow, 0, len(hits))
	for _, hit := range hits {
		e, err := h.Store.EntryByID(r.Context(), h.wid(), hit.EntryID)
		title := "entry " + strconv.FormatInt(hit.EntryID, 10)
		if err == nil && e.Title != "" {
			title = e.Title
		} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
			log.Printf("admin.analytics: enrich %d: %v", hit.EntryID, err)
		}
		out = append(out, topRow{EntryHit: hit, Title: title})
	}
	return out
}
