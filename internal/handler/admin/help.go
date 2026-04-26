package admin

import (
	"html/template"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	helpdocs "github.com/serendipitynz/serenebach/docs/help"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/session"
)

// mountHelp registers /admin/help and /admin/help/{slug}. Help pages
// require a logged-in user (the mount is inside MountProtected), not
// any specific role — every admin should be able to read the manual.
func (h *Handler) mountHelp(r chi.Router) {
	r.Get("/help", h.helpIndex)
	r.Get("/help/{slug}", h.helpShow)
}

// helpNavItem is one entry in the sidebar on the help pages.
type helpNavItem struct {
	Slug   string
	Title  string
	Active bool
	// Fallback reports whether this entry is being served from the
	// default locale because the requested locale has no translation
	// yet. The template uses it to show a small "(未訳)" marker.
	Fallback bool
}

type helpPageData struct {
	pageBase
	Nav            []helpNavItem
	Current        helpNavItem
	Body           template.HTML
	FallbackNotice bool // page body is the default locale's because no translation exists
}

func (h *Handler) helpIndex(w http.ResponseWriter, r *http.Request) {
	h.renderHelpPage(w, r, "index")
}

func (h *Handler) helpShow(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		slug = "index"
	}
	locale := resolveHelpLocale(r)
	if page, _ := helpdocs.Lookup(slug, locale); page == nil {
		http.NotFound(w, r)
		return
	}
	h.renderHelpPage(w, r, slug)
}

func (h *Handler) renderHelpPage(w http.ResponseWriter, r *http.Request, slug string) {
	locale := resolveHelpLocale(r)
	page, fellBack := helpdocs.Lookup(slug, locale)
	if page == nil {
		http.NotFound(w, r)
		return
	}
	body, err := page.Render()
	if err != nil {
		log.Printf("admin.help: render %s: %v", slug, err)
		http.Error(w, "failed to render help page", http.StatusInternalServerError)
		return
	}
	index := helpdocs.Index(locale)
	nav := make([]helpNavItem, 0, len(index))
	for _, p := range index {
		nav = append(nav, helpNavItem{
			Slug:     p.Slug,
			Title:    p.Title,
			Active:   p.Slug == slug,
			Fallback: p.Locale != locale,
		})
	}
	renderMain(w, r, pageHelp, helpPageData{
		pageBase: pageBase{
			Title:      page.Title,
			ActiveMenu: "help",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Nav:            nav,
		Current:        helpNavItem{Slug: page.Slug, Title: page.Title},
		Body:           template.HTML(body),
		FallbackNotice: fellBack,
	})
}

// resolveHelpLocale mirrors tr() / localeFuncs in templates.go:
// context first (middleware path), then a fresh resolve off the
// request. Keeps the help surface lined up with the rest of the
// admin UI — the operator's language choice on the admin sidebar
// drives the help language too.
func resolveHelpLocale(r *http.Request) string {
	locale := i18n.LocaleFrom(r.Context())
	if locale == "" {
		locale = i18nBundle.Resolve(r)
	}
	return locale
}
