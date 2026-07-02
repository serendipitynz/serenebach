package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/dateformat"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/template/lint"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// mountTemplatesDesign registers the `/admin/templates/*` routes — the
// "デザイン" area (historically "デザイン設定" / SB3 wording). RequireUser
// wraps the outer group already; requireDesign further blocks
// regular-tier users — they can't touch templates or design settings.
func (h *Handler) mountTemplatesDesign(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/templates", h.templatesList)
		gr.Get("/templates/active/edit", h.templatesActiveShortcut)
		gr.Get("/templates/settings", h.templatesSettingsForm)
		gr.Post("/templates/settings", h.templatesSettingsSave)
		gr.Get("/templates/og", h.templatesOGForm)
		gr.Post("/templates/og", h.templatesOGSave)
		gr.Get("/templates/custom-tags", h.customTagList)
		gr.Post("/templates/custom-tags", h.customTagCreate)
		gr.Post("/templates/custom-tags/{id}/update", h.customTagUpdate)
		gr.Post("/templates/custom-tags/{id}/delete", h.customTagDelete)
		gr.Get("/templates/{id}/edit", h.templatesEditForm)
		gr.Post("/templates/{id}/edit", h.templatesSave)
		gr.Post("/templates/{id}/recheck", h.templatesRecheck)
		gr.Post("/templates/{id}/save-as", h.templatesSaveAs)
		gr.Post("/templates/{id}/rename", h.templatesRename)
		gr.Post("/templates/{id}/activate", h.templatesActivate)
		gr.Post("/templates/{id}/delete", h.templatesDelete)
		gr.Post("/templates/reorder", h.templatesReorder)
	})
}

// ---- design settings tab -----------------------------------------------

type templateSettingsPageData struct {
	pageBase
	Weblog    domain.Weblog
	Templates []domain.Template
	// DateFormatDefaults surfaces the dateformat package constants so the
	// template can render them as `placeholder="..."` on the 5 date
	// inputs — empty field then shows the fallback the public site will
	// actually use.
	DateFormatDefaults struct {
		Entry   string
		Time    string
		Comment string
		List    string
		Archive string
	}
	// DateFormatPreview is a sample timestamp (the current server time)
	// pre-rendered against the stored format strings. Used by the live
	// preview JS in admin.js as the "starting value" + as a no-JS
	// fallback for users who have JS disabled.
	DateFormatPreview struct {
		Entry   string
		Time    string
		Comment string
		List    string
		Archive string
	}
	Flash string
	Error string
}

func (h *Handler) templatesSettingsForm(w http.ResponseWriter, r *http.Request) {
	h.renderTemplateSettings(w, r, "", r.URL.Query().Get("ok"))
}

func (h *Handler) templatesSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderTemplateSettings(w, r, tr(r, "flash.formParseError"), "")
		return
	}
	archiveID, err := parseOptionalTemplateID(r.PostFormValue("archive_template_id"))
	if err != nil {
		h.renderTemplateSettings(w, r, tr(r, "templates.settings.error.archiveInvalid"), "")
		return
	}
	profileID, err := parseOptionalTemplateID(r.PostFormValue("profile_template_id"))
	if err != nil {
		h.renderTemplateSettings(w, r, tr(r, "templates.settings.error.profileInvalid"), "")
		return
	}
	if err := h.Store.UpdateWeblogDesign(r.Context(), h.wid(), archiveID, profileID); err != nil {
		log.Printf("admin.templatesSettingsSave: %v", err)
		h.renderTemplateSettings(w, r, tr(r, "flash.saveFailed"), "")
		return
	}
	// Date-format strings live in the same form on the same page; save
	// them alongside the template pins. Empty values are stored as-is
	// and resolved to package defaults at render time. Leading and
	// trailing whitespace is preserved verbatim — SB3 ships
	// conf_dateinlist as " (%Mon%/%Day%)" (leading space + parens) so
	// authors typing the same shape into the form must round-trip
	// without the renderer eating the space.
	if err := h.Store.UpdateWeblogDateFormats(r.Context(), h.wid(),
		r.PostFormValue("date_format_entry"),
		r.PostFormValue("time_format_entry"),
		r.PostFormValue("date_format_comment"),
		r.PostFormValue("date_format_list"),
		r.PostFormValue("date_format_archive"),
	); err != nil {
		log.Printf("admin.templatesSettingsSave: date formats: %v", err)
		h.renderTemplateSettings(w, r, tr(r, "templates.settings.error.dateFormatSaveFailed"), "")
		return
	}
	// Display counts + sort order. Page size is clamped into a
	// sensible range so a typo doesn't 0-length the home page or ask
	// for 10000 entries in one render pass.
	pageSize, _ := strconv.Atoi(postFormValue(r, "entries_per_page"))
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	entrySort := normalisedSortOrder(r.PostFormValue("entry_sort_order"), "desc")
	commentSort := normalisedSortOrder(r.PostFormValue("comment_sort_order"), "asc")
	if err := h.Store.UpdateWeblogDisplaySettings(r.Context(), h.wid(), pageSize, entrySort, commentSort); err != nil {
		log.Printf("admin.templatesSettingsSave: display settings: %v", err)
		h.renderTemplateSettings(w, r, tr(r, "templates.settings.error.displayCountSaveFailed"), "")
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates/settings?ok=saved", http.StatusFound)
}

// ---- OG card defaults tab ---------------------------------------------

// templateOGPageData backs the "OG カード" tab. Only the two defaults
// (background image path + text colour) are editable here; per-entry
// overrides stay on the entry form. Flash/Error piggyback on the same
// ?ok=saved / inline-error pattern as template_settings.
type templateOGPageData struct {
	pageBase
	Weblog domain.Weblog
	Flash  string
	Error  string
}

func (h *Handler) templatesOGForm(w http.ResponseWriter, r *http.Request) {
	h.renderTemplateOG(w, r, "", r.URL.Query().Get("ok"))
}

func (h *Handler) templatesOGSave(w http.ResponseWriter, r *http.Request) {
	current, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.templatesOGSave: load: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderTemplateOG(w, r, tr(r, "flash.formParseError"), "")
		return
	}
	updated := *current
	updated.OGBGImagePath = postFormValue(r, "og_bg_image_path")
	// og_text_color resolution: the 透明 checkbox wins (hides text via
	// #00000000). Otherwise honour the hex from the color picker, but
	// only when the unset-flag is absent — an empty field means "use
	// two-tone defaults". Malformed values fall through to empty so
	// the renderer silently reverts to defaults.
	switch {
	case r.PostFormValue("og_text_transparent") == "1":
		updated.OGTextColor = "#00000000"
	case r.PostFormValue("og_text_color_unset") == "1":
		updated.OGTextColor = ""
	default:
		raw := postFormValue(r, "og_text_color")
		if looksLikeHexColor(raw) {
			updated.OGTextColor = strings.ToLower(raw)
		} else {
			updated.OGTextColor = ""
		}
	}
	if err := h.Store.UpdateWeblog(r.Context(), updated); err != nil {
		log.Printf("admin.templatesOGSave: save: %v", err)
		h.renderTemplateOG(w, r, tr(r, "flash.saveFailed"), "")
		return
	}
	// When either OG default changed, every entry that doesn't carry
	// its own override needs a fresh card. Best-effort — per-entry
	// errors are logged, the save still succeeds.
	if updated.OGBGImagePath != current.OGBGImagePath || updated.OGTextColor != current.OGTextColor {
		// Detach from the request context: ServeHTTP returns as soon as the
		// redirect below is written, which cancels r.Context() and would
		// abort the goroutine's first DB call (ListEntriesForAdmin). Keep
		// request-scoped values but drop cancellation. Mirrors webhook.Dispatch.
		go h.regenerateAllOGCards(context.WithoutCancel(r.Context()))
	}
	http.Redirect(w, r, root(r)+"/admin/templates/og?ok=saved", http.StatusFound)
}

func (h *Handler) renderTemplateOG(w http.ResponseWriter, r *http.Request, errMsg, flash string) {
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderTemplateOG: load weblog: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	renderMain(w, r, pageTemplateOG, templateOGPageData{
		pageBase: pageBase{
			Title:      tr(r, "templates.og.title"),
			ActiveMenu: "templates",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Weblog: *weblog,
		Error:  errMsg,
		Flash:  flash,
	})
}

// normalisedSortOrder clamps a form value to one of the two allowed
// strings ("asc" / "desc"). Anything unexpected collapses to the
// supplied default so a tampered form can't inject arbitrary text
// into the weblog row.
func normalisedSortOrder(raw, fallback string) string {
	switch strings.TrimSpace(raw) {
	case "asc":
		return "asc"
	case "desc":
		return "desc"
	}
	return fallback
}

func (h *Handler) renderTemplateSettings(w http.ResponseWriter, r *http.Request, errMsg, flash string) {
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderTemplateSettings: load weblog: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	templates, err := h.Store.ListTemplatesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderTemplateSettings: list templates: %v", err)
	}
	data := templateSettingsPageData{
		pageBase: pageBase{
			Title:      tr(r, "templates.settings.title"),
			ActiveMenu: "templates",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Weblog:    *weblog,
		Templates: templates,
		Error:     errMsg,
		Flash:     flash,
	}
	data.DateFormatDefaults.Entry = dateformat.DefaultEntryDate
	data.DateFormatDefaults.Time = dateformat.DefaultEntryTime
	data.DateFormatDefaults.Comment = dateformat.DefaultCommentDate
	data.DateFormatDefaults.List = dateformat.DefaultListDate
	data.DateFormatDefaults.Archive = dateformat.DefaultArchiveDate

	// Server-rendered preview so the 時刻表記 section shows what each
	// pattern produces without depending on JS. The live preview in
	// admin.js overrides these values as the author edits.
	previewTime := time.Now()
	pick := func(stored, def string) string {
		if stored != "" {
			return stored
		}
		return def
	}
	data.DateFormatPreview.Entry = dateformat.Expand(pick(weblog.DateFormatEntry, dateformat.DefaultEntryDate), previewTime, weblog.Lang)
	data.DateFormatPreview.Time = dateformat.Expand(pick(weblog.TimeFormatEntry, dateformat.DefaultEntryTime), previewTime, weblog.Lang)
	data.DateFormatPreview.Comment = dateformat.Expand(pick(weblog.DateFormatComment, dateformat.DefaultCommentDate), previewTime, weblog.Lang)
	data.DateFormatPreview.List = dateformat.Expand(pick(weblog.DateFormatList, dateformat.DefaultListDate), previewTime, weblog.Lang)
	data.DateFormatPreview.Archive = dateformat.Expand(pick(weblog.DateFormatArchive, dateformat.DefaultArchiveDate), previewTime, weblog.Lang)

	renderMain(w, r, pageTemplateSettings, data)
}

// parseOptionalTemplateID accepts "" or "0" (both = "use active") and any
// positive integer, and rejects anything else.
func parseOptionalTemplateID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid template id %q", raw)
	}
	return v, nil
}

// ---- active-template shortcut ------------------------------------------

// templatesActiveShortcut 302s to the currently-active template's edit
// page so the sidebar "テンプレート編集" link works as a one-click jump
// without pushing per-request state through every page's pageBase.
func (h *Handler) templatesActiveShortcut(w http.ResponseWriter, r *http.Request) {
	t, err := h.Store.ActiveTemplate(r.Context(), h.wid())
	if err != nil {
		// No active template yet → send the user to the list so they can
		// pick / create one. The list page handles the empty state.
		http.Redirect(w, r, root(r)+"/admin/templates", http.StatusFound)
		return
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit", t.ID), http.StatusFound)
}

// ---- edit form ---------------------------------------------------------

type templateFormPageData struct {
	pageBase
	Template domain.Template
	Assets   []domain.TemplateAsset
	Error    string
	Flash    string
	// Lint results for the current state of each body. Populated on
	// first load with the stored template, and refreshed by the
	// re-check button from the live editor contents. nil slices mean
	// "parse failed / empty body" — the template renders a neutral
	// "no issues" message instead of hiding the panel.
	MainLint  templateLintSummary
	EntryLint templateLintSummary
	// LintRan flips true once the server has evaluated the submitted
	// bodies — either on initial GET render or after a recheck POST.
	// The form uses it to decide whether to show "not yet checked".
	LintRan bool
	// LintFromRecheck is true only when the operator just clicked
	// the "テンプレートをチェックする" button. The status panel
	// uses this to decide whether to show the green ✅ all-clear
	// banner: on initial page load we silently lint (so unsupported
	// tags surface a ⚠️ badge on the body section) but suppress the
	// noisy "everything's fine" success card.
	LintFromRecheck bool
	// CustomTags are the registered user-defined tags surfaced as a
	// quick-reference list so the template author can copy-paste
	// {custom_xxx} placeholders into the editor.
	CustomTags []domain.CustomTag
}

// templateLintSummary packages per-body lint output for the admin
// edit form. IsEmpty reports no-issue so the template can render the
// ✅ state succinctly; HasUnsupported drives the ⚠️ badge on the
// section header (the differs-severity findings stay listed inside
// the status panel but don't trigger the badge, per the product
// decision not to flag every SB3-mobile-tag-using template).
type templateLintSummary struct {
	Findings       []lint.Finding
	HasUnsupported bool
	HasDiffers     bool
}

func newTemplateLintSummary(body string) templateLintSummary {
	if strings.TrimSpace(body) == "" {
		return templateLintSummary{}
	}
	findings := lint.AnalyzeSource(body)
	s := templateLintSummary{Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case lint.SevUnsupported:
			s.HasUnsupported = true
		case lint.SevDiffers:
			s.HasDiffers = true
		}
	}
	return s
}

func (h *Handler) templatesEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	t, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templatesEditForm: %v", err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}
	h.renderTemplateForm(w, r, *t, "", r.URL.Query().Get("ok"))
}

func (h *Handler) renderTemplateForm(w http.ResponseWriter, r *http.Request, t domain.Template, errMsg, flash string) {
	var assets []domain.TemplateAsset
	if t.ID != 0 {
		var err error
		assets, err = h.Store.ListTemplateAssets(r.Context(), t.ID)
		if err != nil {
			log.Printf("admin.renderTemplateForm: list assets: %v", err)
		}
	}
	customTags, err := h.Store.ListCustomTags(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderTemplateForm: list custom tags: %v", err)
	}
	renderMain(w, r, pageTemplateForm, templateFormPageData{
		pageBase: pageBase{
			Title:      trf(r, "templates.form.titleEditPlain", t.Name),
			ActiveMenu: "template-edit",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Template:        t,
		Assets:          assets,
		Error:           errMsg,
		Flash:           flash,
		MainLint:        newTemplateLintSummary(t.MainBody),
		EntryLint:       newTemplateLintSummary(t.EntryBody),
		LintRan:         true,
		LintFromRecheck: flash == "rechecked",
		CustomTags:      customTags,
	})
}

// templatesRecheck re-runs the template-compat lint against the
// bodies the operator has typed into the editor — including any
// unsaved changes — and re-renders the same edit page with fresh
// findings. Nothing is persisted: the editor stays marked clean /
// dirty based on what the browser already tracked, and the save
// button is still the only way to commit.
func (h *Handler) templatesRecheck(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	current, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templatesRecheck: load: %v", err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Lint the submitted bodies, not the stored ones. Other fields
	// (name, info, is_active) aren't accepted from the recheck form
	// so the page round-trips without re-reading them.
	current.MainBody = r.PostFormValue("main_body")
	current.EntryBody = r.PostFormValue("entry_body")
	current.CSS = r.PostFormValue("css")
	h.renderTemplateForm(w, r, *current, "", "rechecked")
}

// ---- save (in-place) ---------------------------------------------------

// parseTemplateForm pulls the editable fields + runs a syntax check on
// the base HTML body so a malformed template can't be saved and silently
// break rendering. Returns ("", "") on success, with the updated struct.
// Note: name + info are intentionally NOT read from the form — the
// template edit page hides both fields. Callers preserve them via
// `base` (load-then-overlay) so a plain save is content-only.
func parseTemplateForm(r *http.Request, base domain.Template) (domain.Template, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}
	base.MainBody = r.PostFormValue("main_body")
	base.EntryBody = r.PostFormValue("entry_body")
	base.CSS = r.PostFormValue("css")

	// Syntax check: main body is required, and BEGIN/END blocks must
	// balance. The sbtemplate parser itself is permissive and won't
	// error on an unmatched BEGIN — we catch it here so the admin
	// doesn't ship a template that silently swallows half the page.
	if strings.TrimSpace(base.MainBody) == "" {
		return base, tr(r, "templates.form.error.mainBodyEmpty")
	}
	if err := validateTemplateSource(r, base.MainBody); err != nil {
		return base, trf(r, "templates.form.error.mainBodyParse", err)
	}
	if strings.TrimSpace(base.EntryBody) != "" {
		if err := validateTemplateSource(r, base.EntryBody); err != nil {
			return base, trf(r, "templates.form.error.entryBodyParse", err)
		}
	}
	return base, ""
}

// validateTemplateSource runs the source through the sbtemplate parser
// (catches the cases it *does* return errors for) and separately counts
// BEGIN / END directives so unmatched blocks get caught too. The
// request is used to localize the BEGIN/END mismatch message.
func validateTemplateSource(r *http.Request, src string) error {
	if _, err := sbtemplate.Parse(src, sbtemplate.DefaultCallback); err != nil {
		return err
	}
	begins := strings.Count(src, "<!-- BEGIN ")
	ends := strings.Count(src, "<!-- END ")
	if begins != ends {
		return fmt.Errorf(tr(r, "templates.form.error.beginEndMismatch"), begins, ends)
	}
	return nil
}

func (h *Handler) templatesSave(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	current, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templatesSave: load: %v", err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}
	updated, errMsg := parseTemplateForm(r, *current)
	if errMsg != "" {
		h.renderTemplateForm(w, r, updated, errMsg, "")
		return
	}
	if err := h.Store.UpdateTemplate(r.Context(), updated); err != nil {
		log.Printf("admin.templatesSave: save: %v", err)
		http.Error(w, "failed to save template", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit?ok=saved", id), http.StatusFound)
}

// templatesSaveAs clones the submitted payload into a new template row,
// leaving the original untouched. Useful for branching a layout — tweak,
// Save As "wide v2", activate when ready.
func (h *Handler) templatesSaveAs(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	current, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templatesSaveAs: load: %v", err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}
	updated, errMsg := parseTemplateForm(r, *current)
	if errMsg != "" {
		h.renderTemplateForm(w, r, updated, errMsg, "")
		return
	}
	// save-as takes its name from the dedicated modal input so the
	// original row stays untouched. Fall back to "<name> (コピー)" when
	// the modal is bypassed (no-JS fallback) so we never clone with the
	// same display name as the original.
	newName := postFormValue(r, "new_name")
	if newName == "" {
		newName = current.Name + tr(r, "templates.form.copySuffix")
	}
	// Force a fresh row, not an update of the existing one.
	updated.ID = 0
	updated.IsActive = false
	updated.Name = newName
	newID, err := h.Store.CreateTemplate(r.Context(), updated)
	if err != nil {
		log.Printf("admin.templatesSaveAs: create: %v", err)
		http.Error(w, "failed to save template", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit?ok=cloned", newID), http.StatusFound)
}

// ---- rename (name-only update) -----------------------------------------

// templatesRename updates only the template name. The edit page exposes
// an inline rename modal so authors can fix the display name without
// going through save-as (which clones) or worrying about the editor's
// unsaved-changes state — other editable fields are preserved by
// re-reading the row before writing back. Responds with JSON so the
// browser can update the page header in place.
func (h *Handler) templatesRename(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not_found"})
		return
	}
	current, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not_found"})
			return
		}
		log.Printf("admin.templatesRename: load: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "load_failed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_form"})
		return
	}
	newName := postFormValue(r, "name")
	if newName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": tr(r, "templates.form.error.nameEmpty")})
		return
	}
	if len([]rune(newName)) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": tr(r, "templates.form.error.nameTooLong")})
		return
	}
	if newName == current.Name {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": newName})
		return
	}
	updated := *current
	updated.Name = newName
	if err := h.Store.UpdateTemplate(r.Context(), updated); err != nil {
		log.Printf("admin.templatesRename: save: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "save_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": newName})
}

// ---- list --------------------------------------------------------------

// templateRow pairs the stored template with its parsed info-block
// metadata so the list page can render the i-icon modal without a
// second fetch per row.
type templateRow struct {
	domain.Template
	Meta templateMeta
}

type templateMeta struct {
	Name     string
	Author   string
	Address  string
	Version  string
	Memo     string
	MemoHTML string
}

type templatesListPageData struct {
	pageBase
	Templates    []templateRow
	FlashError   string
	FlashSuccess string
}

// parseTemplateInfo extracts Name/Author/Address/Version + freeform memo
// from the info text stored on a template row. The format mirrors what
// templatepack writes: key-value pairs at the top, then a `=====`
// separator before the free-form memo.
func parseTemplateInfo(info string) templateMeta {
	var m templateMeta
	lines := strings.Split(info, "\n")
	i := 0
	for ; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" || strings.HasPrefix(line, "=====") {
			if strings.HasPrefix(line, "=====") {
				i++ // consume the separator itself
			}
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			m.Name = val
		case "author":
			m.Author = val
		case "address":
			m.Address = val
		case "version":
			m.Version = val
		}
	}
	if i < len(lines) {
		m.Memo = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	}
	return m
}

func (h *Handler) templatesList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListTemplatesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.templatesList: %v", err)
		http.Error(w, "failed to list templates", http.StatusInternalServerError)
		return
	}
	rows := make([]templateRow, 0, len(items))
	for _, t := range items {
		m := parseTemplateInfo(t.Info)
		if m.Memo != "" {
			memoHTML, err := format.Render(m.Memo, "markdown")
			if err == nil {
				m.MemoHTML = memoHTML
			}
		}
		rows = append(rows, templateRow{Template: t, Meta: m})
	}
	q := r.URL.Query()
	renderMain(w, r, pageTemplatesList, templatesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "templates.title"),
			ActiveMenu: "templates",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Templates:    rows,
		FlashError:   q.Get("err"),
		FlashSuccess: q.Get("ok"),
	})
}

// ---- activate ----------------------------------------------------------

func (h *Handler) templatesActivate(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.ActivateTemplate(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templatesActivate: %v", err)
		http.Error(w, "failed to activate template", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates?ok=activated", http.StatusFound)
}

// ---- delete ------------------------------------------------------------

func (h *Handler) templatesDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := h.Store.DeleteTemplate(r.Context(), h.wid(), id)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, repo.ErrTemplateActive):
		// Active template can't be deleted — the site would have nothing
		// to render. Redirect back with a flash so the UI explains why.
		http.Redirect(w, r, root(r)+"/admin/templates?err=active-template-cannot-delete", http.StatusFound)
		return
	case err != nil:
		log.Printf("admin.templatesDelete: %v", err)
		http.Error(w, "failed to delete template", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/templates?ok=deleted", http.StatusFound)
}

// ---- reorder -----------------------------------------------------------

// templatesReorder accepts JSON `{"ids":[...]}` and rewrites sort_order to
// match, matching the category-reorder contract (CSRF via X-CSRF-Token
// header, JSON body, no form parsing).
func (h *Handler) templatesReorder(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if len(payload.IDs) == 0 {
		http.Error(w, "empty ids", http.StatusBadRequest)
		return
	}
	if err := h.Store.ReorderTemplates(r.Context(), h.wid(), payload.IDs); err != nil {
		log.Printf("admin.templatesReorder: %v", err)
		http.Error(w, "failed to reorder", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
