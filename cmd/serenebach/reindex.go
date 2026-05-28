package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
)

// runReindex rebuilds the entries_fts trigram index from scratch.
// Normal INSERT / UPDATE / DELETE keep the index in sync via triggers,
// so this is a repair CLI for the rare case where an operator
// suspects drift (manual DB edits, a stripped trigger, or wanting to
// pick up a tokenizer change after a migration).
func runReindex(a *app.App, _ *config.Config, _ []string) {
	ctx := context.Background()
	if err := a.Store.RebuildFTSIndex(ctx); err != nil {
		log.Fatalf("reindex: %v", err)
	}
	fmt.Fprintln(os.Stderr, "reindex: ok")
}
