package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountLinks registers /admin/links/* routes — SB3 sb_link table port.
// Behind requireDesign like categories: regular-tier users (role=3)
// can't edit the blogroll.
func (h *Handler) mountLinks(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/links", h.linkList)
		gr.Get("/links/new", h.linkNewForm)
		gr.Post("/links/new", h.linkCreate)
		gr.Get("/links/{id}/edit", h.linkEditForm)
		gr.Post("/links/{id}/edit", h.linkUpdate)
		gr.Post("/links/{id}/delete", h.linkDelete)
		gr.Post("/links/reorder", h.linkReorder)
	})
}

// ---- reorder -----------------------------------------------------------

func (h *Handler) linkReorder(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Store.ReorderLinks(r.Context(), h.wid(), payload.IDs); err != nil {
		log.Printf("admin.linkReorder: %v", err)
		http.Error(w, "failed to reorder", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ---- list --------------------------------------------------------------

// linkRow decorates domain.Link with the derived fields the list page
// shows. ChildCount is populated only for group rows (so the list
// column can read "リンク数: N"). The main list hides group members —
// they're managed from their parent group's edit page instead.
type linkRow struct {
	domain.Link
	ChildCount int64
}

type linksListPageData struct {
	pageBase
	Links []linkRow
	Flash string
}

func (h *Handler) linkList(w http.ResponseWriter, r *http.Request) {
	all, err := h.Store.AllLinks(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.linkList: %v", err)
		http.Error(w, "failed to list links", http.StatusInternalServerError)
		return
	}
	// The main list only surfaces top-level rows: groups + ungrouped
	// links. Group members are accessible from the group's edit page
	// so duplicating them here would just double-render every grouped
	// row (and tangle the drag-reorder semantics).
	rows := make([]linkRow, 0, len(all))
	for _, l := range all {
		if !l.IsGroup() && l.ParentID != 0 {
			continue
		}
		row := linkRow{Link: l}
		if l.IsGroup() {
			n, err := h.Store.CountLinksInGroup(r.Context(), h.wid(), l.ID)
			if err != nil {
				log.Printf("admin.linkList: count: %v", err)
			}
			row.ChildCount = n
		}
		rows = append(rows, row)
	}
	renderMain(w, r, pageLinksList, linksListPageData{
		pageBase: pageBase{
			Title:      tr(r, "links.title"),
			ActiveMenu: "links",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Links: rows,
		Flash: r.URL.Query().Get("ok"),
	})
}

// ---- new / edit shared form -------------------------------------------

// linkFormPageData wraps the link being edited with the select-option
// datasets the form needs. Groups is the candidate group list for the
// 所属グループ selector. Members is populated only when editing an
// existing group, so the form can show the group's current children
// inline with a "新規リンク→" header.
type linkFormPageData struct {
	pageBase
	Action  string
	Link    domain.Link
	Groups  []domain.Link // candidate parent groups (only for link-kind rows)
	Members []domain.Link // children belonging to this group (edit-group only)
	// IsNew signals the type selector and parent preset behaviour.
	IsNew bool
	// LockKind = true when editing an existing row — the type picker is
	// hidden so a group can't flip to a link (children would orphan) and
	// a link can't flip to a group (it might already be a child).
	LockKind bool
	// BackURL is the target for the "一覧に戻る" / "グループに戻る"
	// link in the form-actions row. Points at the parent group's edit
	// page when the row is scoped to one, `/admin/links` otherwise.
	BackURL   string
	BackLabel string
	Error     string
}

// linkNewForm renders the create form. ?parent=<id> pre-scopes the new
// row to an existing group (hides the type selector, fixes parent to
// the group). Without the query param it's a root-level new-item form
// showing the リンク vs グループ selector.
func (h *Handler) linkNewForm(w http.ResponseWriter, r *http.Request) {
	base := domain.Link{WID: h.wid(), Kind: domain.LinkKindLink}
	parent := parseOptionalID(r.URL.Query().Get("parent"))
	if parent > 0 {
		// Guarantee the parent exists + is actually a group.
		g, err := h.Store.LinkByID(r.Context(), h.wid(), parent)
		if err == nil && g.IsGroup() {
			base.ParentID = parent
		}
	}
	h.renderLinkForm(w, r, "/admin/links/new", base, true, base.ParentID > 0, "", tr(r, "links.form.titleNewLink"), "link-new")
}

func (h *Handler) linkEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	l, err := h.Store.LinkByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.linkEditForm: %v", err)
		http.Error(w, "failed to load link", http.StatusInternalServerError)
		return
	}
	title := tr(r, "links.form.titleEdit.link")
	if l.IsGroup() {
		title = tr(r, "links.form.titleEdit.group")
	}
	h.renderLinkForm(w, r, fmt.Sprintf("/admin/links/%d/edit", id), *l, false, true, "", title, "links")
}

func (h *Handler) renderLinkForm(
	w http.ResponseWriter, r *http.Request,
	action string, link domain.Link, isNew, lockKind bool,
	errMsg, title, activeMenu string,
) {
	all, err := h.Store.AllLinks(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderLinkForm: all: %v", err)
	}
	var groups []domain.Link
	var members []domain.Link
	for _, l := range all {
		if l.IsGroup() && l.ID != link.ID {
			groups = append(groups, l)
		}
		if !isNew && link.IsGroup() && l.ParentID == link.ID {
			members = append(members, l)
		}
	}
	// Back link: if the row is scoped to a parent group (non-group
	// row with ParentID set), the natural "back" destination is that
	// group's edit page — both for new-under-group and edit-child
	// flows, so the admin can open a child, tweak, save, then return
	// to the group's member list with one click.
	backURL := "/admin/links"
	backLabel := tr(r, "action.back")
	if !link.IsGroup() && link.ParentID > 0 {
		backURL = fmt.Sprintf("/admin/links/%d/edit", link.ParentID)
		backLabel = tr(r, "links.form.backToGroup")
	}
	renderMain(w, r, pageLinkForm, linkFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: activeMenu,
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:    action,
		Link:      link,
		Groups:    groups,
		Members:   members,
		IsNew:     isNew,
		LockKind:  lockKind,
		BackURL:   backURL,
		BackLabel: backLabel,
		Error:     errMsg,
	})
}

func parseOptionalID(raw string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// ---- create / update --------------------------------------------------

// parseLinkForm reads the submitted form values onto the base Link.
// existing is non-nil on update — Kind stays frozen and parent must
// still be a valid group. Returns a non-empty errMsg for user-facing
// validation failures; the handler re-renders the form with that text.
func parseLinkForm(r *http.Request, base domain.Link, existing *domain.Link) (domain.Link, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}
	base.Name = strings.TrimSpace(r.PostFormValue("name"))
	if base.Name == "" {
		return base, tr(r, "links.form.error.nameRequired")
	}
	base.Description = strings.TrimSpace(r.PostFormValue("description"))

	// Kind: only editable on create. On update we trust the existing row.
	if existing == nil {
		kind := strings.TrimSpace(r.PostFormValue("kind"))
		if kind != domain.LinkKindLink && kind != domain.LinkKindGroup {
			kind = domain.LinkKindLink
		}
		base.Kind = kind
	} else {
		base.Kind = existing.Kind
	}

	if base.IsGroup() {
		// Groups carry no URL / target / parent / disp — but keep the
		// values already on the row so the DB doesn't end up with
		// surprising drift. parent_id for groups is always 0 (one
		// level, groups can't nest).
		base.URL = ""
		base.Target = ""
		base.ParentID = 0
		base.Disp = 0
		return base, ""
	}

	// Link-kind fields.
	base.URL = strings.TrimSpace(r.PostFormValue("url"))
	if base.URL == "" {
		return base, tr(r, "links.form.error.urlRequired")
	}
	base.Target = strings.TrimSpace(r.PostFormValue("target"))

	base.ParentID = parseOptionalID(r.PostFormValue("parent_id"))

	switch strings.TrimSpace(r.PostFormValue("disp")) {
	case "hidden", "1":
		base.Disp = 1
	default:
		base.Disp = 0
	}
	return base, ""
}

func (h *Handler) linkCreate(w http.ResponseWriter, r *http.Request) {
	base := domain.Link{WID: h.wid()}
	link, errMsg := parseLinkForm(r, base, nil)
	if errMsg != "" {
		h.renderLinkForm(w, r, "/admin/links/new", link, true, link.ParentID > 0, errMsg, tr(r, "links.form.titleNewLink"), "link-new")
		return
	}
	// If a link declares a parent group, validate it still exists + is a
	// group; silently drop otherwise (UI shouldn't let this happen, but
	// a stale tab could).
	if link.ParentID > 0 {
		g, err := h.Store.LinkByID(r.Context(), h.wid(), link.ParentID)
		if err != nil || !g.IsGroup() {
			link.ParentID = 0
		}
	}
	if _, err := h.Store.CreateLink(r.Context(), link); err != nil {
		log.Printf("admin.linkCreate: %v", err)
		http.Error(w, "failed to create link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/links?ok=saved", http.StatusFound)
}

func (h *Handler) linkUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	existing, err := h.Store.LinkByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.linkUpdate: load: %v", err)
		http.Error(w, "failed to load link", http.StatusInternalServerError)
		return
	}
	link, errMsg := parseLinkForm(r, *existing, existing)
	if errMsg != "" {
		title := tr(r, "links.form.titleEdit.link")
		if link.IsGroup() {
			title = tr(r, "links.form.titleEdit.group")
		}
		h.renderLinkForm(w, r, fmt.Sprintf("/admin/links/%d/edit", id), link, false, true, errMsg, title, "links")
		return
	}
	if link.ParentID > 0 {
		g, err := h.Store.LinkByID(r.Context(), h.wid(), link.ParentID)
		if err != nil || !g.IsGroup() {
			link.ParentID = 0
		}
	}
	if err := h.Store.UpdateLink(r.Context(), link); err != nil {
		log.Printf("admin.linkUpdate: save: %v", err)
		http.Error(w, "failed to save link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/links?ok=saved", http.StatusFound)
}

func (h *Handler) linkDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteLink(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.linkDelete: %v", err)
		http.Error(w, "failed to delete link", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/links?ok=deleted", http.StatusFound)
}
