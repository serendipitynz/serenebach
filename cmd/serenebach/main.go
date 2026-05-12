package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/importer"
	"github.com/serendipitynz/serenebach/internal/mcp"
	"github.com/serendipitynz/serenebach/internal/rebuild"
	"github.com/serendipitynz/serenebach/internal/version"
	admintpl "github.com/serendipitynz/serenebach/web/templates/admin"
)

// inCGI is set once at startup: true when GATEWAY_INTERFACE is present,
// meaning Apache (or any other CGI host) is running us. Used by fatal()
// to guarantee that Apache always receives valid HTTP headers even when
// the process has to abort before cgi.Serve can write them.
var inCGI = os.Getenv("GATEWAY_INTERFACE") != ""

// fatal logs the message to stderr and, in CGI mode, writes a minimal
// HTTP 500 response to stdout before exiting so Apache never shows
// "End of script output before headers". Use this in place of
// log.Fatalf for all failure paths that run before cgi.Serve is called.
func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	if inCGI {
		fmt.Fprintf(os.Stdout,
			"Status: 500 Internal Server Error\r\n"+
				"Content-Type: text/plain; charset=utf-8\r\n\r\n"+
				"Internal server error\n\n%s\n", msg)
	}
	os.Exit(1)
}

// newApp wraps app.New with panic recovery so that unexpected panics
// during initialization are converted to errors and routed through
// fatal() rather than crashing silently in CGI mode.
func newApp(cfg *config.Config) (a *app.App, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return app.New(cfg)
}

func main() {
	// CGI writes responses to stdout, so every log line must go to stderr.
	log.SetOutput(os.Stderr)

	cfg, subcmd, subArgs, err := config.Load(os.Args[1:])
	if err != nil {
		fatal("config: %v", err)
	}

	// extract-assets is a pure file-extraction command: it never
	// touches the database, so dispatch it before newApp to avoid
	// creating / migrating a DB that the operator doesn't need.
	if subcmd == "extract-assets" {
		runExtractAssets(subArgs)
		return
	}

	a, err := newApp(cfg)
	if err != nil {
		fatal("app: %v", err)
	}
	defer func() {
		if err := a.Close(); err != nil {
			log.Printf("app close: %v", err)
		}
	}()

	switch subcmd {
	case "", "serve":
		if err := serve(a, cfg); err != nil {
			fatal("serve: %v", err)
		}
	case "seed":
		spec := app.DefaultSeed()
		if name := os.Getenv("SB_ADMIN_NAME"); name != "" {
			spec.AdminName = name
		}
		if pw := os.Getenv("SB_ADMIN_PASSWORD"); pw != "" {
			spec.AdminPassword = pw
		}
		if email := os.Getenv("SB_ADMIN_EMAIL"); email != "" {
			spec.AdminEmail = email
		}
		// SB_SEED_NO_SAMPLES=1 creates only the admin user + default template,
		// skipping the demo entries. Useful right before `./serenebach import`.
		if os.Getenv("SB_SEED_NO_SAMPLES") == "1" {
			spec.SampleEntries = false
		}
		if err := a.Seed(context.Background(), spec); err != nil {
			// An admin already existing is the expected outcome of
			// re-running seed against a populated DB with a new admin
			// name — surface it as informational, not fatal, so CLI
			// re-runs stay ergonomic.
			if errors.Is(err, app.ErrAdminAlreadyExists) {
				fmt.Fprintln(os.Stderr, "seed: admin already exists, skipping")
				return
			}
			log.Fatalf("seed: %v", err)
		}
		fmt.Fprintln(os.Stderr, "seed: ok")
	case "migrate":
		// app.New already ran migrations — nothing else to do.
		fmt.Fprintln(os.Stderr, "migrate: ok")
	case "import":
		runImport(a, subArgs)
	case "build":
		runBuild(a, subArgs)
	case "mcp":
		if len(subArgs) < 1 || subArgs[0] != "serve" {
			log.Fatalf("mcp: usage: serenebach mcp serve")
		}
		runMCPServe(a)
	default:
		log.Fatalf("unknown subcommand: %q (want: serve | seed | migrate | import | build | extract-assets)", subcmd)
	}
}

func serve(a *app.App, cfg *config.Config) error {
	if cfg.Mode == config.ModeCGI {
		// Apache may set REQUEST_URI to the rewritten URI (e.g.
		// /serenebach.cgi/admin/login) rather than the original path.
		// Go's net/http/cgi uses REQUEST_URI first, which causes the
		// chi router to see /serenebach.cgi/... and return 404.
		// PATH_INFO always holds the correct path per the CGI spec.
		log.Printf("cgi: method=%s path=%s content-length=%s",
			os.Getenv("REQUEST_METHOD"),
			os.Getenv("PATH_INFO"),
			os.Getenv("CONTENT_LENGTH"),
		)
		if pathInfo := os.Getenv("PATH_INFO"); pathInfo != "" {
			uri := pathInfo
			if qs := os.Getenv("QUERY_STRING"); qs != "" {
				uri += "?" + qs
			}
			if err := os.Setenv("REQUEST_URI", uri); err != nil {
				log.Printf("cgi: setenv REQUEST_URI: %v", err)
			}
		}
		return cgi.Serve(a.Handler())
	}
	log.Printf("serenebach: listening on %s (db=%s)", cfg.Addr, cfg.DBPath)
	h := a.Handler()
	if cfg.BasePath != "" {
		// Strip the base path prefix before the router sees the request so
		// routes registered as /admin/... also match /sb4/admin/... etc.
		h = http.StripPrefix(cfg.BasePath, h)
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	// signal.NotifyContext lets the listener loop and the signal-watch
	// path share one cancellation token; calling stop() in defer guarantees
	// the signal handler is removed even on a Listen error.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		log.Printf("serenebach: shutdown signal received, draining (timeout=%s)", cfg.ShutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		// Drain the listen goroutine's terminal error so it cannot leak.
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// runExtractAssets writes the embedded admin static files (admin.css,
// admin.js, logos, favicon, and the Ace editor bundle) to disk so
// Apache can serve them directly in CGI deployments. This is opt-in —
// operators on memory-constrained shared hosting (e.g. Sakura) pair
// this with an .htaccess RewriteRule that forwards /admin/static/*
// to the extracted directory. Other deployments don't need to run it;
// the embedded path keeps working as a fallback.
func runExtractAssets(args []string) {
	fset := flag.NewFlagSet("extract-assets", flag.ExitOnError)
	out := fset.String("out", "./admin-static", "directory to write the embedded admin assets to")
	_ = fset.Parse(args)

	files := []struct {
		name   string // path within the admin template FS
		outRel string // path relative to --out
	}{
		{"admin.css", "admin.css"},
		{"admin.js", "admin.js"},
		{"assets/sb_logo_dark.svg", "sb_logo_dark.svg"},
		{"assets/sb_logo_light.svg", "sb_logo_light.svg"},
		{"assets/sb_logo_gray.svg", "sb_logo_gray.svg"},
		{"assets/favicon.png", "favicon.png"},
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("extract-assets: mkdir: %v", err)
	}
	for _, f := range files {
		body, err := admintpl.Raw(f.name)
		if err != nil {
			log.Fatalf("extract-assets: %s: %v", f.name, err)
		}
		full := filepath.Join(*out, f.outRel)
		if err := os.WriteFile(full, body, 0o644); err != nil {
			log.Fatalf("extract-assets: write %s: %v", full, err)
		}
		fmt.Fprintf(os.Stderr, "extract-assets: wrote %s (%d bytes)\n", full, len(body))
	}

	// Recursively extract assets/ace/ (Ace editor bundle) so the
	// template editor's lazy-loaded mode/theme files are available
	// when Apache serves /admin/static/ace/* directly.
	aceRoot := "assets/ace"
	entries, err := fs.ReadDir(admintpl.FS(), aceRoot)
	if err != nil {
		log.Fatalf("extract-assets: readdir %s: %v", aceRoot, err)
	}
	var walk func(dir string, entries []fs.DirEntry)
	walk = func(dir string, entries []fs.DirEntry) {
		for _, e := range entries {
			pathInFS := dir + "/" + e.Name()
			outRel := strings.TrimPrefix(pathInFS, "assets/")
			full := filepath.Join(*out, outRel)
			if e.IsDir() {
				if err := os.MkdirAll(full, 0o755); err != nil {
					log.Fatalf("extract-assets: mkdir %s: %v", full, err)
				}
				sub, err := fs.ReadDir(admintpl.FS(), pathInFS)
				if err != nil {
					log.Fatalf("extract-assets: readdir %s: %v", pathInFS, err)
				}
				walk(pathInFS, sub)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				log.Fatalf("extract-assets: mkdir %s: %v", filepath.Dir(full), err)
			}
			body, err := admintpl.Raw(pathInFS)
			if err != nil {
				log.Fatalf("extract-assets: %s: %v", pathInFS, err)
			}
			if err := os.WriteFile(full, body, 0o644); err != nil {
				log.Fatalf("extract-assets: write %s: %v", full, err)
			}
			fmt.Fprintf(os.Stderr, "extract-assets: wrote %s (%d bytes)\n", full, len(body))
		}
	}
	walk(aceRoot, entries)

	// Write a MANIFEST so operators can verify version alignment after
	// a binary upgrade.
	manifest := fmt.Sprintf("serenebach %s\nextracted: %s\n",
		version.Full(), time.Now().UTC().Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(*out, "MANIFEST"), []byte(manifest), 0o644)
	fmt.Fprintln(os.Stderr, "extract-assets: ok")
}

func runBuild(a *app.App, subArgs []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "./data/public", "directory to write the static site to")
	limit := fs.Int("limit", rebuild.DefaultEntryListSize, "max entries per list page")
	_ = fs.Parse(subArgs)

	rep, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{
		OutDir:         *out,
		WID:            app.DefaultWID,
		EntryListLimit: *limit,
		BasePath:       a.Config.BasePath,
		ImageDir:       a.Config.ImageDir,
		TemplateDir:    a.Config.TemplateDir,
		TZ:             a.Config.TZ,
	})
	if err != nil {
		log.Fatalf("build: %v", err)
	}
	fmt.Fprintf(os.Stderr,
		"build: out=%s home=%t entries=%d categories=%d archive-year=%d archive-month=%d css=%t images=%d template-assets=%d\n",
		rep.OutDir, rep.Home, rep.Entries, rep.Categories, rep.ArchiveYear, rep.ArchiveMonth, rep.CSSWritten, rep.ImagesCopied, rep.TemplateAssetsCopied)
}

// runMCPServe wires the MCP Server to stdio and blocks until the
// client (IDE process) closes the pipe. Errors beyond EOF become
// non-zero exits so an IDE notices a misbehaving server.
func runMCPServe(a *app.App) {
	var imgStore *images.Store
	if a.Config.ImageDir != "" {
		imgStore = images.NewStore(a.Config.ImageDir)
	}
	srv := &mcp.Server{
		Store:      a.Store,
		Analytics:  a.Analytics,
		ImageStore: imgStore,
		Audit:      a.Audit,
		WID:        app.DefaultWID,
		In:         os.Stdin,
		Out:        os.Stdout,
	}
	log.Printf("serenebach: mcp serve (stdio, db=%s)", a.Config.DBPath)
	if err := srv.Serve(context.Background()); err != nil {
		log.Fatalf("mcp: %v", err)
	}
}

func runImport(a *app.App, args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	sbVersion := fs.Int("sb-version", 3, "source format: 2 (SB2 flat-file dir) or 3 (SB3 SQLite database)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: serenebach import [--sb-version 2|3] <path>")
		fmt.Fprintln(os.Stderr, "  SB3 (default): <path> is the data.db SQLite file")
		fmt.Fprintln(os.Stderr, "  SB2:           <path> is the data directory holding configure.cgi etc.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		log.Fatalf("import: %v", err)
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fs.Usage()
		log.Fatalf("import: missing source path")
	}
	report, err := importer.Import(context.Background(), a.DB, rest[0], importer.Options{
		TargetWID:     app.DefaultWID,
		AuthorID:      1,
		OnlyPublished: true,
		SBVersion:     *sbVersion,
	})
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	fmt.Fprintf(os.Stderr, "import: weblog updated=%t, templates=%d, categories=%d, entries=%d, skipped=%d\n",
		report.WeblogUpdated, report.Templates, report.Categories, report.Entries, report.SkippedEntries)
	for _, warn := range report.Warnings {
		fmt.Fprintf(os.Stderr, "import: warning: %s\n", warn)
	}
}
