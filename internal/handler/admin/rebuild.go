package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/rebuild"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// Rebuilder pairs a static-build output directory with the concurrency
// guard that stops two admins clicking "Rebuild" at the same time.
type Rebuilder struct {
	OutDir string
	// BasePath is the URL prefix under which the static site will be served
	// (e.g. "/sb4"). Forwarded to rebuild.Options so content.Site generates
	// correct links when weblog.BaseURL is not configured.
	BasePath string
	// ImageDir is the source directory whose contents are mirrored into
	// OutDir/img during rebuild. Empty means "skip image copy".
	ImageDir string
	// TemplateDir is the source for per-template assets; mirrored into
	// OutDir/template during rebuild. Empty means "skip".
	TemplateDir string
	// TZ is forwarded to rebuild.Options so archive year/month
	// boundaries are bucketed in the configured timezone instead
	// of the host clock. Nil falls back to time.Local in
	// rebuild.Build, preserving the historical behaviour for
	// callers that haven't been updated.
	TZ *time.Location

	mu       sync.Mutex
	running  bool
	runningM sync.RWMutex
}

func NewRebuilder(outDir string) *Rebuilder {
	return &Rebuilder{OutDir: outDir}
}

// NewRebuilderWithImages is the variant app.New uses so the rebuilder knows
// where to find uploaded media. Kept separate so test callers can still use
// the positional constructor.
func NewRebuilderWithImages(outDir, imageDir, templateDir, basePath string) *Rebuilder {
	return &Rebuilder{OutDir: outDir, ImageDir: imageDir, TemplateDir: templateDir, BasePath: basePath}
}

var ErrRebuildBusy = errors.New("rebuild: another run in progress")

func (rb *Rebuilder) Run(ctx context.Context, store *repo.Store, wid int64) (*rebuild.Report, error) {
	if !rb.mu.TryLock() {
		return nil, ErrRebuildBusy
	}
	defer rb.mu.Unlock()

	rb.runningM.Lock()
	rb.running = true
	rb.runningM.Unlock()
	defer func() {
		rb.runningM.Lock()
		rb.running = false
		rb.runningM.Unlock()
	}()

	return rebuild.Build(ctx, store, rebuild.Options{
		OutDir:         rb.OutDir,
		WID:            wid,
		EntryListLimit: rebuild.DefaultEntryListSize,
		BasePath:       rb.BasePath,
		ImageDir:       rb.ImageDir,
		TemplateDir:    rb.TemplateDir,
		TZ:             rb.TZ,
	})
}

func (rb *Rebuilder) Running() bool {
	rb.runningM.RLock()
	defer rb.runningM.RUnlock()
	return rb.running
}

// ---- handler ------------------------------------------------------------

type rebuildPageData struct {
	pageBase
	OutDir      string
	LastBuiltAt time.Time
	Report      *rebuild.Report
	Running     bool
	Error       string
}

func (h *Handler) mountRebuild(r chi.Router) {
	r.Get("/rebuild", h.rebuildGet)
	r.Post("/rebuild", h.rebuildPost)
}

func (h *Handler) rebuildGet(w http.ResponseWriter, r *http.Request) {
	renderMain(w, r, pageRebuild, h.buildRebuildPageData(r))
}

func (h *Handler) rebuildPost(w http.ResponseWriter, r *http.Request) {
	if h.Rebuilder == nil {
		http.Error(w, "rebuild is not configured on this server", http.StatusInternalServerError)
		return
	}

	report, err := h.Rebuilder.Run(r.Context(), h.Store, h.wid())
	if errors.Is(err, ErrRebuildBusy) {
		http.Redirect(w, r, root(r)+"/admin/rebuild?status=busy", http.StatusSeeOther)
		return
	}
	if err != nil {
		log.Printf("admin.rebuildPost: %v", err)
		redirectToRebuild(w, r, "err", err.Error())
		return
	}

	q := url.Values{}
	q.Set("status", "ok")
	q.Set("home", strconv.FormatBool(report.Home))
	q.Set("entries", strconv.Itoa(report.Entries))
	q.Set("categories", strconv.Itoa(report.Categories))
	q.Set("archive_year", strconv.Itoa(report.ArchiveYear))
	q.Set("archive_month", strconv.Itoa(report.ArchiveMonth))
	q.Set("css", strconv.FormatBool(report.CSSWritten))
	http.Redirect(w, r, root(r)+"/admin/rebuild?"+q.Encode(), http.StatusSeeOther)
}

func (h *Handler) buildRebuildPageData(r *http.Request) rebuildPageData {
	data := rebuildPageData{
		pageBase: pageBase{
			Title:      tr(r, "rebuild.title"),
			ActiveMenu: "rebuild",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
	}
	if h.Rebuilder != nil {
		data.OutDir = h.Rebuilder.OutDir
		data.LastBuiltAt = lastBuildTime(h.Rebuilder.OutDir)
		data.Running = h.Rebuilder.Running()
	}
	q := r.URL.Query()
	switch q.Get("status") {
	case "ok":
		data.Report = &rebuild.Report{
			Home:         q.Get("home") == "true",
			Entries:      atoiOr(q.Get("entries"), 0),
			Categories:   atoiOr(q.Get("categories"), 0),
			ArchiveYear:  atoiOr(q.Get("archive_year"), 0),
			ArchiveMonth: atoiOr(q.Get("archive_month"), 0),
			CSSWritten:   q.Get("css") == "true",
			OutDir:       data.OutDir,
		}
	case "busy":
		data.Error = tr(r, "rebuild.error.busy")
	case "err":
		data.Error = trf(r, "rebuild.error.failed", q.Get("msg"))
	}
	return data
}

func redirectToRebuild(w http.ResponseWriter, r *http.Request, status, msg string) {
	q := url.Values{}
	q.Set("status", status)
	if msg != "" {
		q.Set("msg", msg)
	}
	http.Redirect(w, r, root(r)+"/admin/rebuild?"+q.Encode(), http.StatusSeeOther)
}

func lastBuildTime(outDir string) time.Time {
	info, err := os.Stat(filepath.Join(outDir, "index.html"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
