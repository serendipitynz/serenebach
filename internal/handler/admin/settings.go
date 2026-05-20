package admin

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountSettings registers the /admin/settings/* routes. Called from
// MountProtected so RequireUser already wraps the group.
//
// The settings page carries a three-tab nav for admins
// ([基本設定][AI設定][画面設定]) and a two-tab nav for regular users
// ([AI設定][画面設定]). The handlers below back each tab with a
// dedicated GET; POST endpoints that save state are gated by the
// appropriate role middleware.
//
//   - /settings                  — sidebar entry point. Redirects to
//     /settings/basic for users who can
//     manage design (the blog's primary
//     configuration surface), otherwise
//     renders the personal 画面設定 tab.
//   - /settings/basic (基本設定) — CanManageDesign only (blog info +
//     env-var snapshot).
//   - /settings/ai (AI 設定)     — every logged-in user. The AI
//     writing-assist panel is personal;
//     the MCP section shown within the
//     same tab is admin-only.
//   - /settings/ops → 301        — legacy redirect for bookmarks.
func (h *Handler) mountSettings(r chi.Router) {
	r.Get("/settings", h.settingsRoot)
	r.Get("/settings/screen", h.settingsScreenForm)
	r.Post("/settings/language", h.settingsLanguageSubmit)
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/settings/basic", h.settingsBasicForm)
		gr.Post("/settings/basic", h.settingsBasicSubmit)
	})
	r.Get("/settings/ai", h.settingsAIForm)
	r.Post("/settings/ai", h.settingsAISave)
	r.Post("/settings/ai/test", h.settingsAITest)

	// Legacy /settings/ops bookmarks fall through to the merged
	// /settings/ai tab so admins who had the ops page pinned still
	// land on a useful view.
	r.Get("/settings/ops", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, root(r)+"/admin/settings/ai", http.StatusMovedPermanently)
	})
}

// OpsInfo surfaces the env-driven operational settings that the UI
// cannot edit (secrets + deployment paths). Rendered read-only on
// the 基本設定 tab so the admin can see the current state without
// shelling into the server.
type OpsInfo struct {
	TurnstileEnabled bool
	UploadMaxMB      int64
	ImageDir         string
	RebuildOutDir    string
	AnalyticsState   string // "disabled" | "main-db" | "external:<path>"
	MCPAuditState    string // "main-db" | "external:<path>"
}

// mcpAuditRow is the view-model row rendered in the audit panel on
// the AI settings tab. Labels are pre-resolved so the template stays
// free of map lookups.
type mcpAuditRow struct {
	ID           int64
	Tool         string
	TargetID     int64
	TokenLabel   string
	AuthorLabel  string
	Extra        string
	CreatedAtFmt string
}

// settingsPageBase carries the fields every settings tab needs to
// render its tabbar + shared flash slots. Each per-tab struct embeds
// this so templates can resolve `.ActiveTab` uniformly.
type settingsPageBase struct {
	pageBase
	ActiveTab       string // "screen" | "basic" | "ai" | "webhooks"
	CanManageDesign bool   // drives visibility of the 基本設定 tab
	CanManageUsers  bool   // drives visibility of MCP panel on the AI tab
}

// newSettingsBase pre-fills the shared fields. Callers only need to
// set ActiveTab + Title.
func (h *Handler) newSettingsBase(r *http.Request, title, activeTab string) settingsPageBase {
	u := session.UserFrom(r.Context())
	canDesign, canUsers := false, false
	if u != nil {
		canDesign = u.CanManageDesign()
		canUsers = u.CanManageUsers()
	}
	return settingsPageBase{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: "settings",
			CSRFToken:  csrf.Token(r.Context()),
			User:       u,
		},
		ActiveTab:       activeTab,
		CanManageDesign: canDesign,
		CanManageUsers:  canUsers,
	}
}

// --- 画面設定 tab (all users) -----------------------------------------

type settingsScreenPageData struct {
	settingsPageBase
}

// settingsRoot is the entry point /admin/settings (the sidebar
// "設定" link). For design-capable users it 302s to /settings/basic
// — the blog-wide configuration tab is the primary thing they came
// here for. Regular users without that permission fall through to
// the personal 画面設定 tab so they aren't redirected to a 403.
func (h *Handler) settingsRoot(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	if u != nil && u.CanManageDesign() {
		http.Redirect(w, r, root(r)+"/admin/settings/basic", http.StatusFound)
		return
	}
	h.settingsScreenForm(w, r)
}

func (h *Handler) settingsScreenForm(w http.ResponseWriter, r *http.Request) {
	renderMain(w, r, pageSettings, settingsScreenPageData{
		settingsPageBase: h.newSettingsBase(r, tr(r, "settings.tab.screen"), "screen"),
	})
}

// settingsLanguageSubmit persists the operator's admin UI language
// choice in a server-issued cookie. Browser-side document.cookie
// writes don't survive Sakura's "ENC_" cookie protection layer, so
// the language preference must round-trip through Set-Cookie like
// any other server-managed cookie (sb_csrf / sb_session).
func (h *Handler) settingsLanguageSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	lang := r.FormValue("lang")
	if !slices.Contains(i18nBundle.Supported(), lang) {
		http.Error(w, "unsupported lang", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     i18n.LangCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   31536000,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		// HttpOnly: false — admin.js still wants to read the value
		// for select-state restoration on reload (see (3)).
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- 基本設定 tab (CanManageDesign) -----------------------------------

type settingsBasicPageData struct {
	settingsPageBase
	Weblog       domain.Weblog
	Ops          OpsInfo
	FlashSuccess string
	Error        string
}

func (h *Handler) settingsBasicForm(w http.ResponseWriter, r *http.Request) {
	h.renderSettingsBasic(w, r, "", r.URL.Query().Get("ok") != "")
}

func (h *Handler) renderSettingsBasic(w http.ResponseWriter, r *http.Request, errMsg string, success bool) {
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.settingsBasicForm: load: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	h.renderSettingsBasicWith(w, r, *weblog, errMsg, success)
}

func (h *Handler) renderSettingsBasicWith(w http.ResponseWriter, r *http.Request, weblog domain.Weblog, errMsg string, success bool) {
	data := settingsBasicPageData{
		settingsPageBase: h.newSettingsBase(r, tr(r, "settings.tab.basic"), "basic"),
		Weblog:           weblog,
		Ops:              h.opsInfo(),
		Error:            errMsg,
	}
	if success {
		data.FlashSuccess = tr(r, "flash.saved")
	}
	renderMain(w, r, pageSettingsBasic, data)
}

func (h *Handler) settingsBasicSubmit(w http.ResponseWriter, r *http.Request) {
	current, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.settingsBasicSubmit: load: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	updated, errMsg := parseSettingsForm(r, *current)
	if errMsg != "" {
		h.renderSettingsBasicWith(w, r, updated, errMsg, false)
		return
	}
	if err := h.Store.UpdateWeblog(r.Context(), updated); err != nil {
		log.Printf("admin.settingsBasicSubmit: save: %v", err)
		h.renderSettingsBasicWith(w, r, updated, tr(r, "flash.saveFailed"), false)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/basic?ok=1", http.StatusFound)
}

// regenerateAllOGCards iterates every entry for the weblog and
// rebuilds the OG card. Runs in a goroutine from settingsBasicSubmit
// so a blog with hundreds of entries doesn't stall the request; each
// card's errors are logged individually.
func (h *Handler) regenerateAllOGCards(ctx context.Context) {
	// Pass a big limit — ListEntriesForAdmin uses it as LIMIT in SQL;
	// typical blogs sit well under 10k entries. Missing a few tail
	// cards is acceptable given the goroutine is best-effort anyway.
	entries, err := h.Store.ListEntriesForAdmin(ctx, h.wid(), repo.ListEntriesQuery{Limit: 10000})
	if err != nil {
		log.Printf("admin.regenerateAllOGCards: list: %v", err)
		return
	}
	for _, e := range entries {
		h.regenerateOGCard(ctx, e)
	}
}

// parseSettingsForm pulls each editable field off the submitted form
// with a small validation pass. Returns (updated, "") on success or
// (best-effort, message) on failure so the form re-renders with the
// user's in-flight values instead of silently reverting.
func parseSettingsForm(r *http.Request, base domain.Weblog) (domain.Weblog, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}

	base.Title = strings.TrimSpace(r.PostFormValue("title"))
	if base.Title == "" {
		return base, tr(r, "settings.basic.error.titleRequired")
	}
	base.Description = strings.TrimSpace(r.PostFormValue("description"))
	base.Lang = strings.TrimSpace(r.PostFormValue("lang"))
	if base.Lang == "" {
		base.Lang = "ja"
	}

	baseURL := strings.TrimSpace(r.PostFormValue("base_url"))
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return base, tr(r, "settings.basic.error.baseUrlInvalid")
		}
	}
	base.BaseURL = baseURL

	base.LLMSEnabled = strings.TrimSpace(r.PostFormValue("llms_enabled")) == "1"
	base.AutoRebuildOnPublish = strings.TrimSpace(r.PostFormValue("auto_rebuild_on_publish")) == "1"
	// OG background / text-colour are edited on /admin/templates/og.
	// Those fields are not posted to this handler and the corresponding
	// base.OGBGImagePath / base.OGTextColor values flow through
	// untouched from the current weblog row.

	// comment_mode / spam_words live on /admin/comments/settings.
	// Preserve whatever's already stored by leaving base.CommentMode
	// and base.SpamWords untouched.
	return base, ""
}

// looksLikeHexColor reports whether s is a "#RRGGBB" or "#RRGGBBAA"
// literal. Lets the settings handler reject garbage from a raw form
// submission before storing — the og package re-validates at render
// time but surfacing the bad value to the DB just churns later
// regenerations.
func looksLikeHexColor(s string) bool {
	if len(s) != 7 && len(s) != 9 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// opsInfo snapshots the env-driven side of the configuration so the
// settings page can render it read-only. The admin sees what's in
// effect without needing SSH.
func (h *Handler) opsInfo() OpsInfo {
	info := OpsInfo{
		ImageDir:         h.ImageDir,
		UploadMaxMB:      h.uploadMaxBytes() >> 20,
		TurnstileEnabled: h.TurnstileConfigured,
	}
	if h.Rebuilder != nil {
		info.RebuildOutDir = h.Rebuilder.OutDir
	}
	switch {
	case h.Analytics == nil:
		info.AnalyticsState = "disabled"
	case h.AnalyticsDBPath != "":
		info.AnalyticsState = "external:" + h.AnalyticsDBPath
	default:
		info.AnalyticsState = "main-db"
	}
	switch {
	case h.Audit == nil:
		info.MCPAuditState = "disabled"
	case h.MCPAuditDBPath != "":
		info.MCPAuditState = "external:" + h.MCPAuditDBPath
	default:
		info.MCPAuditState = "main-db"
	}
	return info
}
