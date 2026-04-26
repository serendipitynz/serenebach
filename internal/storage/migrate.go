package storage

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/serendipitynz/serenebach/migrations"
)

// Up applies every pending migration bundled into the binary.
// Keeping this in-process means a CGI deployment has nothing external to run.
func Up(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("storage: set dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("storage: migrate up: %w", err)
	}
	return nil
}
