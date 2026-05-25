package backup

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/sqlite"
)

func TestIntegration_backupAndRestore(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// Build a small data tree.
	dbPath := filepath.Join(tmp, "sb.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	imgDir := filepath.Join(tmp, "img")
	_ = os.MkdirAll(filepath.Join(imgDir, "2026", "05"), 0o755)
	_ = os.WriteFile(filepath.Join(imgDir, "2026", "05", "a.jpg"), []byte("fake-image"), 0o644)

	tmplDir := filepath.Join(tmp, "templates")
	_ = os.MkdirAll(tmplDir, 0o755)
	_ = os.WriteFile(filepath.Join(tmplDir, "index.html"), []byte("<html></html>"), 0o644)

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:      dbPath,
		ImageDir:    imgDir,
		TemplateDir: tmplDir,
		OutPath:     outPath,
		Quiet:       true,
		Now:         time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}
	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Unzip to a fresh directory.
	restoreDir := filepath.Join(tmp, "restore")
	_ = os.MkdirAll(restoreDir, 0o755)

	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		dst := filepath.Join(restoreDir, f.Name)
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", f.Name, err)
		}
	}

	// Re-open the extracted DB and run migrations to confirm it is valid.
	restoredDBPath := filepath.Join(restoreDir, "db", "serenebach.db")
	restoredDB, err := sqlite.Open(restoredDBPath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer restoredDB.Close()

	if err := storage.Up(restoredDB); err != nil {
		t.Fatalf("migrate restored db: %v", err)
	}

	// Confirm image file was restored.
	restoredImg := filepath.Join(restoreDir, "img", "2026", "05", "a.jpg")
	if _, err := os.Stat(restoredImg); err != nil {
		t.Fatalf("restored image missing: %v", err)
	}
}
