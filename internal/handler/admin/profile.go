package admin

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/auth"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountProfile wires /admin/profile — the self-edit screen available to
// every logged-in user regardless of role. No requireAdmin /
// requireDesign here; RequireUser in the outer group is enough. The
// route is focused on name / password / description; AI writing-assist
// config lives separately under /admin/settings/ai.
func (h *Handler) mountProfile(r chi.Router) {
	r.Get("/profile", h.profileForm)
	r.Post("/profile", h.profileSave)
}

type profileFormPageData struct {
	pageBase
	Target domain.User
	Error  string
	Flash  string
}

func (h *Handler) profileForm(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	// Reload from DB so a freshly-saved description / name shows up
	// even if the session snapshot is stale.
	fresh, err := h.Store.UserByID(r.Context(), u.ID)
	if err != nil {
		log.Printf("admin.profileForm: reload: %v", err)
		http.Error(w, "failed to load profile", http.StatusInternalServerError)
		return
	}
	h.renderProfileForm(w, r, *fresh, "", r.URL.Query().Get("ok"))
}

func (h *Handler) renderProfileForm(w http.ResponseWriter, r *http.Request, u domain.User, errMsg, flash string) {
	renderMain(w, r, pageProfileForm, profileFormPageData{
		pageBase: pageBase{
			Title:      tr(r, "profile.form.title"),
			ActiveMenu: "profile",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Target: u,
		Error:  errMsg,
		Flash:  flash,
	})
}

func (h *Handler) profileSave(w http.ResponseWriter, r *http.Request) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		http.Redirect(w, r, root(r)+"/admin/login", http.StatusFound)
		return
	}
	existing, err := h.Store.UserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("admin.profileSave: load: %v", err)
		http.Error(w, "failed to load profile", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderProfileForm(w, r, *existing, tr(r, "flash.formParseError"), "")
		return
	}

	updated := *existing
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" || !repo.IsValidUserName(name) {
		h.renderProfileForm(w, r, updated, tr(r, "profile.form.error.nameInvalid"), "")
		return
	}
	updated.Name = name
	updated.DisplayName = strings.TrimSpace(r.PostFormValue("display_name"))
	if updated.DisplayName == "" {
		updated.DisplayName = updated.Name
	}
	updated.Email = strings.TrimSpace(r.PostFormValue("email"))
	updated.Description = r.PostFormValue("description")
	updated.DescriptionFormat = normaliseDescriptionFormat(r.PostFormValue("description_format"))
	updated.ListVisible = r.PostFormValue("list_visible") == "on"
	// Role is never read from the profile form — users can't
	// self-promote or self-demote. The existing row's role stays
	// put across the save.

	if err := h.Store.UpdateUser(r.Context(), updated); err != nil {
		if errors.Is(err, repo.ErrUserNameInUse) {
			h.renderProfileForm(w, r, updated, tr(r, "profile.form.error.nameInUse"), "")
			return
		}
		log.Printf("admin.profileSave: %v", err)
		http.Error(w, "failed to save profile", http.StatusInternalServerError)
		return
	}

	// Password change: blank fields mean "keep current" — same
	// convention as /admin/users/{id}/edit.
	newPW := r.PostFormValue("password")
	if newPW != "" {
		if newPW != r.PostFormValue("password_confirm") {
			h.renderProfileForm(w, r, updated, tr(r, "profile.form.error.passwordMismatchKept"), "")
			return
		}
		hash, err := auth.HashPassword(newPW)
		if err != nil {
			log.Printf("admin.profileSave: hash: %v", err)
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		if err := h.Store.UpdateUserPassword(r.Context(), actor.ID, hash); err != nil {
			log.Printf("admin.profileSave: password: %v", err)
			http.Error(w, "failed to update password", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, root(r)+"/admin/profile?ok=saved", http.StatusFound)
}
