package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/cgi"
	"os"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/importer"
	"github.com/serendipitynz/serenebach/internal/mcp"
	"github.com/serendipitynz/serenebach/internal/rebuild"
)

func main() {
	// CGI writes responses to stdout, so every log line must go to stderr.
	log.SetOutput(os.Stderr)

	cfg, subcmd, subArgs, err := config.Load(os.Args[1:])
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("app: %v", err)
	}
	defer a.Close()

	switch subcmd {
	case "", "serve":
		if err := serve(a, cfg); err != nil {
			log.Fatalf("serve: %v", err)
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
		if len(subArgs) < 1 {
			log.Fatalf("import: usage: serenebach import <sb3-sqlite-path>")
		}
		runImport(a, subArgs[0])
	case "build":
		runBuild(a, subArgs)
	case "mcp":
		if len(subArgs) < 1 || subArgs[0] != "serve" {
			log.Fatalf("mcp: usage: serenebach mcp serve")
		}
		runMCPServe(a)
	default:
		log.Fatalf("unknown subcommand: %q (want: serve | seed | migrate | import | build)", subcmd)
	}
}

func serve(a *app.App, cfg *config.Config) error {
	if cfg.Mode == config.ModeCGI {
		return cgi.Serve(a.Handler())
	}
	log.Printf("serenebach: listening on %s (db=%s)", cfg.Addr, cfg.DBPath)
	srv := &http.Server{Addr: cfg.Addr, Handler: a.Handler()}
	return srv.ListenAndServe()
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
		ImageDir:       a.Config.ImageDir,
		TemplateDir:    a.Config.TemplateDir,
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

func runImport(a *app.App, sourcePath string) {
	report, err := importer.Import(context.Background(), a.DB, sourcePath, importer.Options{
		TargetWID:     app.DefaultWID,
		AuthorID:      1,
		OnlyPublished: true,
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
