package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/auth"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountUsers wires /admin/users/* behind the RequireAdmin middleware —
// every route in this group rejects non-admin sessions before the
// handler runs.
func (h *Handler) mountUsers(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireAdmin)
		gr.Get("/users", h.userList)
		gr.Get("/users/new", h.userNewForm)
		gr.Post("/users/new", h.userCreate)
		gr.Get("/users/{id}/edit", h.userEditForm)
		gr.Post("/users/{id}/edit", h.userUpdate)
		gr.Post("/users/{id}/delete", h.userDelete)
		gr.Post("/users/reorder", h.userReorder)
	})
}

// requireAdmin rejects the request with 403 when the session user
// isn't a site administrator. Used by the users-management group —
// menu-visibility alone isn't enough since a malicious admin-lite
// user could still craft the URL directly.
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := session.UserFrom(r.Context())
		if u == nil || !u.CanManageUsers() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireDesign blocks regular-tier users from routes that touch
// site-structural config (categories, tags, templates, design
// settings). Power + Admin pass through. See domain.User.CanManageDesign.
func (h *Handler) requireDesign(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := session.UserFrom(r.Context())
		if u == nil || !u.CanManageDesign() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- list --------------------------------------------------------------

type usersListPageData struct {
	pageBase
	Users []domain.User
	Flash string
}

func (h *Handler) userList(w http.ResponseWriter, r *http.Request) {
	users, err := h.Store.ListUsers(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.userList: %v", err)
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	renderMain(w, r, pageUsersList, usersListPageData{
		pageBase: pageBase{
			Title:      tr(r, "users.list.title"),
			ActiveMenu: "users",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Users: users,
		Flash: r.URL.Query().Get("ok"),
	})
}

// ---- new form ----------------------------------------------------------

func (h *Handler) userNewForm(w http.ResponseWriter, r *http.Request) {
	h.renderUserForm(w, r, domain.User{
		WID: h.wid(), Role: domain.RoleRegular,
		ListVisible: true, DescriptionFormat: "html",
	}, "", "")
}

// parseNewUserForm pulls everything the top-of-page create form submits
// — the edit form uses its own parser since it doesn't collect a
// password confirmation on a blank field.
func parseNewUserForm(r *http.Request, wid int64) (domain.User, string, string) {
	if err := r.ParseForm(); err != nil {
		return domain.User{}, "", tr(r, "flash.formParseError")
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" || !repo.IsValidUserName(name) {
		return domain.User{WID: wid}, "", tr(r, "users.form.error.nameInvalid")
	}
	pw := r.PostFormValue("password")
	pwConfirm := r.PostFormValue("password_confirm")
	if pw == "" {
		return domain.User{WID: wid, Name: name}, "", tr(r, "users.form.error.passwordRequired")
	}
	if pw != pwConfirm {
		return domain.User{WID: wid, Name: name}, "", tr(r, "users.form.error.passwordMismatch")
	}
	role, ok := parseRole(r.PostFormValue("role"))
	if !ok {
		return domain.User{WID: wid, Name: name}, "", tr(r, "users.form.error.roleInvalid")
	}
	u := domain.User{
		WID:               wid,
		Name:              name,
		DisplayName:       strings.TrimSpace(r.PostFormValue("display_name")),
		Email:             strings.TrimSpace(r.PostFormValue("email")),
		Role:              role,
		ListVisible:       true,
		DescriptionFormat: "html",
	}
	if u.DisplayName == "" {
		u.DisplayName = u.Name
	}
	return u, pw, ""
}

// normaliseDescriptionFormat clamps a form value to one of the two
// supported values ("html" / "markdown"). Anything else collapses to
// "html" so a tampered submit can't land an arbitrary string in the
// DB. Shared between user / profile / category handlers.
func normaliseDescriptionFormat(raw string) string {
	switch strings.TrimSpace(raw) {
	case "markdown":
		return "markdown"
	case "html":
		return "html"
	}
	return "html"
}

func parseRole(raw string) (int, bool) {
	switch strings.TrimSpace(raw) {
	case strconv.Itoa(domain.RoleAdmin):
		return domain.RoleAdmin, true
	case strconv.Itoa(domain.RolePower):
		return domain.RolePower, true
	case strconv.Itoa(domain.RoleRegular):
		return domain.RoleRegular, true
	}
	return 0, false
}

func (h *Handler) userCreate(w http.ResponseWriter, r *http.Request) {
	u, password, errMsg := parseNewUserForm(r, h.wid())
	if errMsg != "" {
		h.renderUserForm(w, r, u, errMsg, "")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Printf("admin.userCreate: hash: %v", err)
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}
	if _, err := h.Store.CreateUser(r.Context(), u, hash); err != nil {
		if errors.Is(err, repo.ErrUserNameInUse) {
			h.renderUserForm(w, r, u, tr(r, "users.form.error.nameInUse"), "")
			return
		}
		log.Printf("admin.userCreate: %v", err)
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/users?ok=created", http.StatusFound)
}

// ---- edit ---------------------------------------------------------------

type userFormPageData struct {
	pageBase
	Target domain.User
	// Action is the form's POST target — /admin/users/new for a create
	// and /admin/users/{id}/edit for an update. IsNew mirrors the same
	// distinction for the template's conditional branches (password
	// required vs. "leave blank to keep", no role self-guard on create).
	Action string
	IsNew  bool
	Error  string
	Flash  string
}

func (h *Handler) userEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	u, err := h.Store.UserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.userEditForm: %v", err)
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}
	h.renderUserForm(w, r, *u, "", r.URL.Query().Get("ok"))
}

func (h *Handler) renderUserForm(w http.ResponseWriter, r *http.Request, u domain.User, errMsg, flash string) {
	isNew := u.ID == 0
	action := root(r) + "/admin/users/new"
	title := tr(r, "users.form.titleNewPlain")
	if !isNew {
		action = root(r) + "/admin/users/" + strconv.FormatInt(u.ID, 10) + "/edit"
		title = trf(r, "users.form.titleEditPlain", u.Name)
	}
	renderMain(w, r, pageUserForm, userFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: "users",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Target: u,
		Action: action,
		IsNew:  isNew,
		Error:  errMsg,
		Flash:  flash,
	})
}

func (h *Handler) userUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	existing, err := h.Store.UserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.userUpdate: load: %v", err)
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderUserForm(w, r, *existing, tr(r, "flash.formParseError"), "")
		return
	}

	updated := *existing
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" || !repo.IsValidUserName(name) {
		h.renderUserForm(w, r, updated, tr(r, "users.form.error.nameInvalid"), "")
		return
	}
	updated.Name = name
	applyUserProfileFields(&updated, r)

	role, ok := parseRole(r.PostFormValue("role"))
	if !ok {
		h.renderUserForm(w, r, updated, tr(r, "users.form.error.roleInvalid"), "")
		return
	}
	role, roleErrKey := h.resolveUserRoleUpdate(r.Context(), existing, role, session.UserFrom(r.Context()))
	if roleErrKey != "" {
		h.renderUserForm(w, r, updated, tr(r, roleErrKey), "")
		return
	}
	updated.Role = role

	if err := h.Store.UpdateUser(r.Context(), updated); err != nil {
		if errors.Is(err, repo.ErrUserNameInUse) {
			h.renderUserForm(w, r, updated, tr(r, "users.form.error.nameInUse"), "")
			return
		}
		log.Printf("admin.userUpdate: %v", err)
		http.Error(w, "failed to save user", http.StatusInternalServerError)
		return
	}

	if !h.updateUserPasswordIfChanged(w, r, id, updated) {
		return
	}

	http.Redirect(w, r, root(r)+"/admin/users/"+strconv.FormatInt(id, 10)+"/edit?ok=saved", http.StatusFound)
}

// applyUserProfileFields copies the non-credential profile fields from
// the form onto updated. Name validation happens in the caller because
// the empty-name path needs a tailored flash; everything here is "take
// whatever the form gave us" with light normalisation.
func applyUserProfileFields(updated *domain.User, r *http.Request) {
	updated.DisplayName = strings.TrimSpace(r.PostFormValue("display_name"))
	if updated.DisplayName == "" {
		updated.DisplayName = updated.Name
	}
	updated.Email = strings.TrimSpace(r.PostFormValue("email"))
	updated.Description = r.PostFormValue("description")
	updated.DescriptionFormat = normaliseDescriptionFormat(r.PostFormValue("description_format"))
	updated.ListVisible = r.PostFormValue("list_visible") == "on"
}

// resolveUserRoleUpdate enforces the two role-change invariants and
// returns the effective role plus an i18n error key (empty when the
// update is allowed):
//
//  1. Admins editing themselves can never demote. A tampered form is
//     silently pinned back to RoleAdmin rather than rejected so a
//     hostile submit can't slip through.
//  2. The lone-admin guard rejects any path that would leave zero
//     admins (e.g. a second admin demoting the only other one).
func (h *Handler) resolveUserRoleUpdate(ctx context.Context, existing *domain.User, submitted int, actor *domain.User) (int, string) {
	role := submitted
	if actor != nil && actor.ID == existing.ID && existing.Role == domain.RoleAdmin {
		role = domain.RoleAdmin
	}
	if existing.Role == domain.RoleAdmin && role != domain.RoleAdmin {
		count, err := h.Store.CountAdmins(ctx, h.wid())
		if err != nil {
			log.Printf("admin.userUpdate: count admins: %v", err)
		}
		if count <= 1 {
			return role, "users.form.error.lastAdmin"
		}
	}
	return role, ""
}

// updateUserPasswordIfChanged persists a new password when both the
// `password` and `password_confirm` fields are filled and match. Blank
// fields are a no-op — matches SB3's profile / admin-edit UX where
// leaving the password field empty means "keep the existing hash".
//
// Returns false when the handler has already written the response (a
// flash on mismatch, or a 500 on hash / persist failure); callers must
// stop on a false return.
func (h *Handler) updateUserPasswordIfChanged(w http.ResponseWriter, r *http.Request, id int64, updated domain.User) bool {
	newPW := r.PostFormValue("password")
	if newPW == "" {
		return true
	}
	if newPW != r.PostFormValue("password_confirm") {
		h.renderUserForm(w, r, updated, tr(r, "users.form.error.passwordMismatchKept"), "")
		return false
	}
	hash, err := auth.HashPassword(newPW)
	if err != nil {
		log.Printf("admin.userUpdate: hash: %v", err)
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return false
	}
	if err := h.Store.UpdateUserPassword(r.Context(), id, hash); err != nil {
		log.Printf("admin.userUpdate: password: %v", err)
		http.Error(w, "failed to update password", http.StatusInternalServerError)
		return false
	}
	return true
}

// ---- delete -------------------------------------------------------------

func (h *Handler) userDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	actor := session.UserFrom(r.Context())
	if actor != nil && actor.ID == id {
		http.Redirect(w, r, root(r)+"/admin/users?ok=cannot-delete-self", http.StatusFound)
		return
	}
	// Never delete the last admin — even if the target is some other
	// admin, we'd leave zero admins if there's only one in total.
	target, err := h.Store.UserByID(r.Context(), id)
	if err == nil && target.Role == domain.RoleAdmin {
		count, _ := h.Store.CountAdmins(r.Context(), h.wid())
		if count <= 1 {
			http.Redirect(w, r, root(r)+"/admin/users?ok=cannot-delete-last-admin", http.StatusFound)
			return
		}
	}
	if err := h.Store.DeleteUser(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.userDelete: %v", err)
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/users?ok=deleted", http.StatusFound)
}

// ---- reorder ------------------------------------------------------------

func (h *Handler) userReorder(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Store.ReorderUsers(r.Context(), h.wid(), payload.IDs); err != nil {
		log.Printf("admin.userReorder: %v", err)
		http.Error(w, "failed to reorder", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
