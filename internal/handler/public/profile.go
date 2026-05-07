package public

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// profile renders the public /profile/{id}/ page — SB3's mode=user
// detail view. 404 when the user is missing, belongs to another
// weblog, or has list_visible=false (authors opt out of being
// listed, so their profile URL shouldn't resolve either).
func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	user, err := h.Store.UserByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.profile: load user %d: %v", id, err)
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}
	if user.WID != h.WID || !user.ListVisible {
		http.NotFound(w, r)
		return
	}

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.profile: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	tmpl, err := h.pickTemplate(ctx, weblog, weblog.ProfileTemplateID)
	if err != nil {
		log.Printf("public.profile: load template: %v", err)
		http.Error(w, "no active template", http.StatusInternalServerError)
		return
	}
	profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID)
	if err != nil {
		log.Printf("public.profile: load profile users: %v", err)
	}
	sidebar := h.loadSidebarData(ctx, "public.profile")

	view := content.ProfileView{
		Site:         h.buildSite(ctx, *weblog).WithBasePath(root(r)),
		Template:     tmpl,
		User:         *user,
		ProfileUsers: profileUsers,
		Sidebar:      sidebar,
		CSRFToken:    csrf.Token(r.Context()),
	}
	body, err := view.Render()
	if err != nil {
		log.Printf("public.profile: render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	writeHTML(w, body, "public.profile")
}
