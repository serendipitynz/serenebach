package storage

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/pressly/goose/v3"

	"github.com/serendipitynz/serenebach/migrations"
)

var gooseInitOnce sync.Once
var gooseInitErr error

// Up applies every pending migration bundled into the binary.
// Keeping this in-process means a CGI deployment has nothing external to run.
func Up(db *sql.DB) error {
	gooseInitOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		gooseInitErr = goose.SetDialect("sqlite3")
	})
	if gooseInitErr != nil {
		return fmt.Errorf("storage: set dialect: %w", gooseInitErr)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("storage: migrate up: %w", err)
	}
	return nil
}
