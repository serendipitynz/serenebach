package admin

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/ai"
	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/version"
	admintpl "github.com/serendipitynz/serenebach/web/templates/admin"
)

// pageBase carries the fields every dashboard template expects. Page-
// specific data structs embed this so fields like .Title, .User, and
// .CSRFToken are accessible without plumbing them through each template.
type pageBase struct {
	Title      string
	ActiveMenu string
	CSRFToken  string
	User       *domain.User
}

// tmplFuncs are shared by every admin template so formatting stays
// consistent. Nothing here is per-request — keep it pure.
var tmplFuncs = template.FuncMap{
	"fmtDate": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("2006-01-02 15:04")
	},
	"humanTime": func(t time.Time) string {
		if t.IsZero() {
			return "まだ再構築されていません"
		}
		return t.Format("2006-01-02 15:04 MST")
	},
	"percent": func(part, whole int64) string {
		if whole == 0 {
			return "—"
		}
		return strconv.FormatInt(100*part/whole, 10) + "%"
	},
	"statusLabel": func(s any) string {
		switch v := s.(type) {
		case domain.EntryStatus:
			return entryStatusLabel(v)
		case domain.MessageStatus:
			return messageStatusLabel(v)
		case int:
			return entryStatusLabel(domain.EntryStatus(v))
		}
		return "?"
	},
	"appVersion": func() string { return version.Full() },
	// iconTrash returns the shared trash-can SVG used on every
	// destructive row button. Shipped as a template func rather than a
	// {{template}} include so page templates can drop it inline in
	// button text without further plumbing.
	"iconTrash": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M7 3h6v1h4v2H3V4h4V3zm-2 5h10l-1 10H6L5 8zm3 2v6m4-6v6" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
	},
	"iconInfo": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M10 9v5m0-8.5v.01" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`)
	},
	"iconCopy": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><rect x="6" y="6" width="10" height="12" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M4 14V4.5A1.5 1.5 0 0 1 5.5 3H13" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>`)
	},
	"iconCheck": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M4 10.5l4 4L16 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
	},
	"iconFolder": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M2.5 5.5A1.5 1.5 0 0 1 4 4h3.5l1.75 2H16A1.5 1.5 0 0 1 17.5 7.5v7A1.5 1.5 0 0 1 16 16H4a1.5 1.5 0 0 1-1.5-1.5v-9z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>`)
	},
	"iconEye": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M1.5 10S4.5 4.5 10 4.5 18.5 10 18.5 10 15.5 15.5 10 15.5 1.5 10 1.5 10z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><circle cx="10" cy="10" r="2.5" fill="none" stroke="currentColor" stroke-width="1.5"/></svg>`)
	},
	"iconEdit": func() template.HTML {
		return template.HTML(`<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M13.5 2.5l4 4-10 10H3.5v-4l10-10z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
	},
	// T / Tf / THTML / Locale — stubs registered at parse time so
	// templates can reference {{ T "key" }} without tripping the
	// strict parse check. Real implementations are bound per-render
	// in renderMain / renderLogin after cloning the parsed template.
	// THTML returns template.HTML so catalogue values that embed
	// already-trusted markup (e.g. Markdown-style <code> in hint
	// copy) render literally instead of getting escaped.
	"T":        func(key string) string { return key },
	"Tf":       func(key string, args ...any) string { return key },
	"THTML":    func(key string) template.HTML { return template.HTML(key) },
	"THTMLf":   func(key string, args ...any) template.HTML { return template.HTML(key) },
	"JSBundle": func() template.JS { return template.JS(`{}`) },
	"Locale":   func() string { return "" },
	// Root returns the deployment base path (e.g. "/sb4"). Stub only;
	// real implementation bound per-request in localeFuncs.
	"Root": func() string { return "" },
	// aiEnabled reports whether the writing-assist features should
	// surface their UI affordances. True only when both the server
	// has SB_AI_SECRET configured AND the given user has picked a
	// provider on /admin/settings/ai. Used to hide the Ace toolbar ✨
	// buttons and the entry-form ✨ inputs when the user turned AI
	// off — otherwise clicking would fail with "unconfigured" and the
	// buttons look broken.
	"aiEnabled": func(u *domain.User) bool {
		if u == nil || !ai.SecretConfigured() {
			return false
		}
		return u.AIKind != ""
	},
	"statusClass": func(s any) string {
		switch v := s.(type) {
		case domain.EntryStatus:
			return entryStatusClass(v)
		case domain.MessageStatus:
			return messageStatusClass(v)
		case int:
			return entryStatusClass(domain.EntryStatus(v))
		}
		return ""
	},
	// lintCtx packs the per-body lint summary into a single map the
	// template_form lintBlock partial can iterate over. Keeps the
	// template body free of multi-arg `with` chains. fromRecheck
	// gates the green ✅ banner so it only fires when the operator
	// pressed the "テンプレートをチェックする" button — initial
	// page loads stay quiet for clean templates.
	"lintCtx": func(body string, sum templateLintSummary, ran, fromRecheck bool) map[string]any {
		return map[string]any{
			"Body":            body,
			"Lint":            sum,
			"LintRan":         ran,
			"LintFromRecheck": fromRecheck,
		}
	},
	// joinInts renders a []int as "12, 34, 56" — used by the lint
	// status panel to list the source line numbers a finding appears
	// on. Empty slice returns "" so the surrounding label can hide
	// itself with an `if .Lines` check.
	"joinInts": func(xs []int) string {
		if len(xs) == 0 {
			return ""
		}
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			out = append(out, strconv.Itoa(x))
		}
		return strings.Join(out, ", ")
	},
	"trimPrefix": func(prefix, s string) string {
		return strings.TrimPrefix(s, prefix)
	},
}

func entryStatusLabel(s domain.EntryStatus) string {
	switch s {
	case domain.EntryPublished:
		return "公開"
	case domain.EntryDraft:
		return "下書き"
	case domain.EntryClosed:
		return "非公開"
	}
	return "?"
}

func entryStatusClass(s domain.EntryStatus) string {
	switch s {
	case domain.EntryPublished:
		return "published"
	case domain.EntryDraft:
		return "draft"
	case domain.EntryClosed:
		return "closed"
	}
	return ""
}

func messageStatusLabel(s domain.MessageStatus) string {
	switch s {
	case domain.MessageApproved:
		return "承認済"
	case domain.MessageWaiting:
		return "承認待ち"
	case domain.MessageHidden:
		return "非公開"
	}
	return "?"
}

func messageStatusClass(s domain.MessageStatus) string {
	switch s {
	case domain.MessageApproved:
		return "approved"
	case domain.MessageWaiting:
		return "waiting"
	case domain.MessageHidden:
		return "hidden"
	}
	return ""
}

// Page template names. Each dashboard page is a layout + content pair;
// login is a standalone template without the shell.
const (
	pageHome             = "home"
	pageEntriesList      = "entries_list"
	pageEntryForm        = "entry_form"
	pagePagesList        = "pages_list"
	pagePageForm         = "page_form"
	pageImages           = "images"
	pageCategoriesList   = "categories_list"
	pageCategoryForm     = "category_form"
	pageLinksList        = "links_list"
	pageLinkForm         = "link_form"
	pageTagsList         = "tags_list"
	pageUsersList        = "users_list"
	pageUserForm         = "user_form"
	pageProfileForm      = "profile_form"
	pageCommentsList     = "comments_list"
	pageCommentSettings  = "comment_settings"
	pageAnalytics        = "analytics"
	pageRebuild          = "rebuild"
	pageSettings         = "settings"
	pageSettingsBasic    = "settings_basic"
	pageSettingsAI       = "settings_ai"
	pageTemplatesList    = "templates_list"
	pageTemplateForm     = "template_form"
	pageTemplateImport   = "template_import"
	pageTemplateSettings = "template_settings"
	pageTemplateOG       = "template_og"
	pageCustomTags       = "custom_tags"
	pageHelp             = "help"
)

// DevMode disables template and i18n caching. When true, every request
// re-parses templates and catalogues from disk. Set this during local
// development (e.g. SB_DEV=1) so edits are reflected without restarting.
var DevMode bool

var (
	mainTemplates = loadMainTemplates()
	loginTemplate = loadLoginTemplate()
	setupTemplate = loadSetupTemplate()
	i18nBundle    = loadI18nBundle()
)

func getMainTemplates() map[string]*template.Template {
	if DevMode {
		return loadMainTemplates()
	}
	return mainTemplates
}

func getLoginTemplate() *template.Template {
	if DevMode {
		return loadLoginTemplate()
	}
	return loginTemplate
}

func getSetupTemplate() *template.Template {
	if DevMode {
		return loadSetupTemplate()
	}
	return setupTemplate
}

func getI18nBundle() *i18n.Bundle {
	if DevMode {
		return loadI18nBundle()
	}
	return i18nBundle
}

// loadI18nBundle parses every JSON file under admintpl's embedded
// i18n/ directory into an i18n.Bundle. "ja" is the source-of-truth
// default; missing keys in other locales fall back to it. A startup
// failure here is a build-time problem (bad JSON shipped) — better to
// crash than render key-literal fallbacks for every string.
func loadI18nBundle() *i18n.Bundle {
	raw, err := admintpl.I18nCatalogues()
	if err != nil {
		panic("admin: load i18n catalogues: " + err.Error())
	}
	b, err := i18n.LoadBundle("ja", raw)
	if err != nil {
		panic("admin: i18n bundle: " + err.Error())
	}
	return b
}

func loadMainTemplates() map[string]*template.Template {
	out := map[string]*template.Template{}
	for _, p := range []string{pageHome, pageEntriesList, pageEntryForm, pagePagesList, pagePageForm, pageImages, pageCategoriesList, pageCategoryForm, pageLinksList, pageLinkForm, pageTagsList, pageUsersList, pageUserForm, pageProfileForm, pageCommentsList, pageCommentSettings, pageAnalytics, pageRebuild, pageSettings, pageSettingsBasic, pageSettingsAI, pageTemplatesList, pageTemplateForm, pageTemplateImport, pageTemplateSettings, pageTemplateOG, pageCustomTags, pageHelp} {
		t, err := template.New("").Funcs(tmplFuncs).ParseFS(admintpl.FS(), "layout.html", p+".html")
		if err != nil {
			panic("admin: parse " + p + ": " + err.Error())
		}
		out[p] = t
	}
	return out
}

func loadLoginTemplate() *template.Template {
	t, err := template.New("").Funcs(tmplFuncs).ParseFS(admintpl.FS(), "login.html")
	if err != nil {
		panic("admin: parse login: " + err.Error())
	}
	return t
}

func loadSetupTemplate() *template.Template {
	t, err := template.New("").Funcs(tmplFuncs).ParseFS(admintpl.FS(), "setup.html")
	if err != nil {
		panic("admin: parse setup: " + err.Error())
	}
	return t
}

// renderMain writes one of the dashboard pages through the shared
// layout. A per-request clone rebinds the T / Tf / Locale funcs to
// the request locale — `tmpl` itself stays shared; Clone is cheap
// (shallow copy of parse trees) and safe for concurrent use.
func renderMain(w http.ResponseWriter, r *http.Request, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	base, ok := getMainTemplates()[page]
	if !ok {
		http.Error(w, "admin: unknown template "+page, http.StatusInternalServerError)
		return
	}
	tmpl, err := base.Clone()
	if err != nil {
		http.Error(w, "admin: clone template: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Funcs(localeFuncs(r))
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		// Not http.Error — headers already written. Server-side log only.
	}
}

// tr resolves an i18n key for the request's active locale. Shared by
// handlers that need to build localized flash / error strings before
// handing them off to a template (login errors, for example, where
// the field is just a plain Error string on the data struct).
func tr(r *http.Request, key string) string {
	bundle := getI18nBundle()
	locale := i18n.LocaleFrom(r.Context())
	if locale == "" {
		locale = bundle.Resolve(r)
	}
	return bundle.T(locale, key)
}

// trf is the formatted variant of tr — feeds the resolved string into
// fmt.Sprintf with the given args. Mirrors i18n.Bundle.Tf so callers
// don't have to plumb the locale themselves.
func trf(r *http.Request, key string, args ...any) string {
	bundle := getI18nBundle()
	locale := i18n.LocaleFrom(r.Context())
	if locale == "" {
		locale = bundle.Resolve(r)
	}
	return bundle.Tf(locale, key, args...)
}

// localeFuncs returns T / Tf / THTML / Locale closures bound to the
// locale resolved off the request. Shared between renderMain and
// renderLogin so every admin surface picks up the same rules. Reads
// context first (middleware path) but falls back to resolving off
// the raw request so admin routes don't need a separate middleware
// wiring — the bundle's Resolve handles cookie + Accept-Language on
// every call.
func localeFuncs(r *http.Request) template.FuncMap {
	bundle := getI18nBundle()
	locale := i18n.LocaleFrom(r.Context())
	if locale == "" {
		locale = bundle.Resolve(r)
	}
	return template.FuncMap{
		"T":  func(key string) string { return bundle.T(locale, key) },
		"Tf": func(key string, args ...any) string { return bundle.Tf(locale, key, args...) },
		"THTML": func(key string) template.HTML {
			return template.HTML(bundle.T(locale, key))
		},
		"THTMLf": func(key string, args ...any) template.HTML {
			return template.HTML(bundle.Tf(locale, key, args...))
		},
		// JSBundle emits a pre-resolved JSON object carrying every
		// `js.*` key for the active locale. layout.html drops this
		// into `window.__sbI18n` so admin.js can localize toast /
		// modal copy without shipping the whole catalogue to the
		// browser. Returned as template.JS so the JSON renders
		// inline without escaping.
		"JSBundle": func() template.JS {
			return template.JS(bundle.JSCatalogueJSON(locale, "js."))
		},
		"Locale": func() string { return locale },
		"Root":   func() string { return basepath.FromContext(r.Context()) },
		// statusLabel / humanTime are registered as static stubs in
		// tmplFuncs (so parse-time strict checks pass). Re-bind them
		// here so the rendered text picks up the active locale rather
		// than the JP source-of-truth fallback.
		"statusLabel": func(s any) string {
			return localizedStatusLabel(locale, s)
		},
		"humanTime": func(t time.Time) string {
			if t.IsZero() {
				return bundle.T(locale, "humanTime.never")
			}
			return t.Format("2006-01-02 15:04 MST")
		},
	}
}

// localizedStatusLabel maps a domain status value to its localized
// badge text via the i18n bundle. Mirrors the type-switch in the
// static `statusLabel` stub so tests and templates get identical
// behaviour, just with locale awareness.
func localizedStatusLabel(locale string, s any) string {
	bundle := getI18nBundle()
	unknown := bundle.T(locale, "status.unknown")
	switch v := s.(type) {
	case domain.EntryStatus:
		return entryStatusKey(locale, v, unknown)
	case domain.MessageStatus:
		return messageStatusKey(locale, v, unknown)
	case int:
		return entryStatusKey(locale, domain.EntryStatus(v), unknown)
	}
	return unknown
}

func entryStatusKey(locale string, s domain.EntryStatus, fallback string) string {
	bundle := getI18nBundle()
	switch s {
	case domain.EntryPublished:
		return bundle.T(locale, "status.published")
	case domain.EntryDraft:
		return bundle.T(locale, "status.draft")
	case domain.EntryClosed:
		return bundle.T(locale, "status.closed")
	}
	return fallback
}

func messageStatusKey(locale string, s domain.MessageStatus, fallback string) string {
	bundle := getI18nBundle()
	switch s {
	case domain.MessageApproved:
		return bundle.T(locale, "status.message.approved")
	case domain.MessageWaiting:
		return bundle.T(locale, "status.message.waiting")
	case domain.MessageHidden:
		return bundle.T(locale, "status.message.hidden")
	}
	return fallback
}

// renderLogin writes the standalone login page (no sidebar / topbar).
func renderLogin(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := getLoginTemplate().Clone()
	if err != nil {
		http.Error(w, "admin: clone login template: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Funcs(localeFuncs(r))
	_ = tmpl.ExecuteTemplate(w, "login", data)
}

// renderSetup writes the first-run setup page. Like renderLogin it skips
// the layout chrome — there is no session / sidebar yet.
func renderSetup(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := getSetupTemplate().Clone()
	if err != nil {
		http.Error(w, "admin: clone setup template: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Funcs(localeFuncs(r))
	_ = tmpl.ExecuteTemplate(w, "setup", data)
}
