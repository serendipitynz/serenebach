package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// SetupRequest carries the validated form input from the first-run
// setup screen up to whatever piece of code can actually run a Seed.
// Plumbing it through a struct (rather than positional args) keeps the
// callback signature stable as more fields land — multiblog might add
// e.g. WeblogBaseURL later.
type SetupRequest struct {
	AdminName     string
	AdminPassword string
	AdminEmail    string
	WeblogTitle   string
	SampleEntries bool
}

// SetupRunner runs the actual seed against the application. The admin
// handler does not depend on the app package directly — to avoid an
// import cycle, app.New constructs a closure that calls app.Seed and
// hands it to the handler at startup. ErrSetupAlreadyDone short-circuits
// to a 404 in the GET handler; any other error is logged and rendered
// as a generic "internal" message.
type SetupRunner func(ctx context.Context, req SetupRequest) error

// ErrSetupAlreadyDone signals that an admin already exists when the
// setup endpoint is hit. The handler turns this into a 404 so the
// route is invisible after first use.
var ErrSetupAlreadyDone = errors.New("admin: setup already completed")

// MountSetup wires the first-run /setup endpoint onto the supplied
// router. The caller is expected to mount this on the *root* router
// (not /admin), and to gate every other path through SetupGate so a
// fresh install lands here automatically.
func (h *Handler) MountSetup(r chi.Router) {
	if h.Setup == nil {
		return
	}
	r.Get("/setup", h.setupForm)
	r.Post("/setup", h.setupSubmit)
}

type setupData struct {
	Error         string
	Name          string
	Email         string
	WeblogTitle   string
	SampleEntries bool
	CSRFToken     string
}

func (h *Handler) setupForm(w http.ResponseWriter, r *http.Request) {
	if done, err := h.Store.HasAdminUser(r.Context()); err != nil {
		log.Printf("admin.setup: check admin: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if done {
		http.NotFound(w, r)
		return
	}
	renderSetup(w, r, setupData{
		WeblogTitle:   "Serene Bach",
		SampleEntries: true,
		CSRFToken:     csrf.Token(r.Context()),
	})
}

func (h *Handler) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if done, err := h.Store.HasAdminUser(r.Context()); err != nil {
		log.Printf("admin.setup: check admin: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else if done {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	pw := r.PostFormValue("password")
	pwConfirm := r.PostFormValue("password_confirm")
	weblogTitle := strings.TrimSpace(r.PostFormValue("weblog_title"))
	if weblogTitle == "" {
		weblogTitle = "Serene Bach"
	}
	samples := r.PostFormValue("sample_entries") == "1"
	token := csrf.Token(r.Context())

	echo := func(msg string) {
		renderSetup(w, r, setupData{
			Error:         msg,
			Name:          name,
			Email:         email,
			WeblogTitle:   weblogTitle,
			SampleEntries: samples,
			CSRFToken:     token,
		})
	}

	if name == "" || pw == "" {
		echo(tr(r, "setup.errorEmpty"))
		return
	}
	if !repo.IsValidUserName(name) {
		echo(tr(r, "setup.errorInvalidName"))
		return
	}
	if pw != pwConfirm {
		echo(tr(r, "setup.errorPasswordMismatch"))
		return
	}
	if len(pw) < 8 {
		echo(tr(r, "setup.errorPasswordShort"))
		return
	}

	err := h.Setup(r.Context(), SetupRequest{
		AdminName:     name,
		AdminPassword: pw,
		AdminEmail:    email,
		WeblogTitle:   weblogTitle,
		SampleEntries: samples,
	})
	if err != nil {
		if errors.Is(err, ErrSetupAlreadyDone) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.setup: run: %v", err)
		echo(tr(r, "setup.errorInternal"))
		return
	}
	http.Redirect(w, r, root(r)+"/admin/login", http.StatusFound)
}
