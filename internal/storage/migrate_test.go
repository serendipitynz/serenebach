package storage

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/serendipitynz/serenebach/migrations"

	_ "modernc.org/sqlite"
)

func TestUpSkipsMigrationWhenAlreadyAtLatest(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// First call: applies all migrations normally.
	if err := Up(db); err != nil {
		t.Fatalf("first Up: %v", err)
	}

	// Second call: should hit the fast-path and return without error.
	if err := Up(db); err != nil {
		t.Fatalf("second Up (fast-path): %v", err)
	}
}

func TestEmbeddedMaxVersionMatchesFS(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	var maxFromFS int64
	for _, e := range entries {
		name := e.Name()
		if len(name) < 4 {
			continue
		}
		// Leading digits are the version.
		var digits int
		for digits < len(name) && name[digits] >= '0' && name[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			continue
		}
		v := parseInt64(t, name[:digits])
		if v > maxFromFS {
			maxFromFS = v
		}
	}

	if maxFromFS == 0 {
		t.Fatal("no migration files found")
	}

	got := embeddedMaxVersion()
	if got != maxFromFS {
		t.Errorf("embeddedMaxVersion() = %d, want %d", got, maxFromFS)
	}
}

func parseInt64(t *testing.T, s string) int64 {
	t.Helper()
	var v int64
	for i := 0; i < len(s); i++ {
		v = v*10 + int64(s[i]-'0')
	}
	return v
}

// Sessions written before 0053 hold the cookie value itself, which the
// digest lookup can never match; the migration drops them (everyone signs
// in once) rather than leaving usable cookies in the database.
func TestSessionDigestMigrationDropsPlaintextSessions(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := Up(db); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := goose.DownTo(db, ".", 52); err != nil {
		t.Fatalf("DownTo 52: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at, created_at)
		VALUES ('plaintext-cookie', 1, 9999999999, 0)`); err != nil {
		t.Fatalf("seed pre-0053 session: %v", err)
	}

	if err := Up(db); err != nil {
		t.Fatalf("Up to 0053: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("sessions = %d after 0053, want the plaintext rows dropped", n)
	}
	if _, err := db.Exec(`SELECT token_hash FROM sessions`); err != nil {
		t.Errorf("token_hash column missing after 0053: %v", err)
	}
}
