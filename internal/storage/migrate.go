// Package storage owns database setup and goose-driven migrations.
// Migrations run automatically at process start so tests and production
// share the same path. Sub-packages hold the queries (repo) and the
// pure-Go SQLite driver setup (sqlite).
package storage

import (
	"database/sql"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"

	"github.com/serendipitynz/serenebach/migrations"
)

var gooseInitOnce sync.Once
var gooseInitErr error

// embeddedMaxVersion caches the highest migration version number found
// in the embedded migrations.FS. Computed once because the FS is
// immutable for the lifetime of the binary.
var embeddedMaxVersionOnce sync.Once
var embeddedMaxVersionVal int64

func embeddedMaxVersion() int64 {
	embeddedMaxVersionOnce.Do(func() {
		entries, err := migrations.FS.ReadDir(".")
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			base := path.Base(e.Name())
			// File names look like "0001_init.sql" — take the leading digits.
			idx := 0
			for idx < len(base) && base[idx] >= '0' && base[idx] <= '9' {
				idx++
			}
			if idx == 0 {
				continue
			}
			v, err := strconv.ParseInt(base[:idx], 10, 64)
			if err != nil {
				continue
			}
			if v > embeddedMaxVersionVal {
				embeddedMaxVersionVal = v
			}
		}
	})
	return embeddedMaxVersionVal
}

func appliedMaxVersion(db *sql.DB) (int64, error) {
	var v sql.NullInt64
	row := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`)
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Int64, nil
}

// Up applies every pending migration bundled into the binary.
// Keeping this in-process means a CGI deployment has nothing external to run.
//
// Fast-path: if the embedded max version equals the applied max version,
// we skip goose's full integrity scan, saving ~50 ms per CGI request.
func Up(db *sql.DB) error {
	gooseInitOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		gooseInitErr = goose.SetDialect("sqlite3")
	})
	if gooseInitErr != nil {
		return fmt.Errorf("storage: set dialect: %w", gooseInitErr)
	}

	applied, err := appliedMaxVersion(db)
	if err == nil && applied == embeddedMaxVersion() && applied > 0 {
		return nil
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("storage: migrate up: %w", err)
	}
	return nil
}
