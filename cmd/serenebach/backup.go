package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/serendipitynz/serenebach/internal/backup"
	"github.com/serendipitynz/serenebach/internal/config"
)

func runBackup(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("out", "", "output ZIP path (default backup-YYYY-MM-DD-HHMMSS.zip, '-' for stdout)")
	includeAnalytics := fs.Bool("include-analytics", false, "include analytics and MCP audit DBs")
	includePublic := fs.Bool("include-public", false, "include static rebuild output")
	exclude := fs.String("exclude", "", "comma-separated list of assets to omit: images, templates")
	quiet := fs.Bool("quiet", false, "suppress progress output")
	_ = fs.Parse(args)

	// In CGI mode the working directory is unpredictable, so require
	// an explicit --out to avoid writing into the web server's root.
	if inCGI && (*out == "" || *out == "-") {
		fatal("backup: --out is required when running under CGI")
	}

	var excluded []string
	if *exclude != "" {
		for _, s := range strings.Split(*exclude, ",") {
			if v := strings.TrimSpace(s); v != "" {
				excluded = append(excluded, v)
			}
		}
	}

	opts := backup.Options{
		DBPath:           cfg.DBPath,
		AnalyticsDBPath:  cfg.AnalyticsDBPath,
		MCPAuditDBPath:   cfg.MCPAuditDBPath,
		ImageDir:         cfg.ImageDir,
		TemplateDir:      cfg.TemplateDir,
		RebuildOutDir:    cfg.RebuildOutDir,
		OutPath:          *out,
		IncludeAnalytics: *includeAnalytics,
		IncludePublic:    *includePublic,
		Excluded:         excluded,
		Quiet:            *quiet,
	}

	report, err := backup.Run(context.Background(), opts)
	if err != nil {
		exitCode := 1
		switch {
		case strings.Contains(err.Error(), "vacuum into"):
			exitCode = 2
		case strings.Contains(err.Error(), "collect ") || strings.Contains(err.Error(), "open root"):
			exitCode = 3
		case strings.Contains(err.Error(), "zip") || strings.Contains(err.Error(), "create zip"):
			exitCode = 4
		}
		log.Printf("backup: %v", err)
		os.Exit(exitCode)
	}

	if !*quiet {
		if report.OutPath == "-" {
			fmt.Fprintln(os.Stderr, "backup: ok")
		} else {
			fmt.Fprintf(os.Stderr, "backup: ok\n")
		}
	}
}
