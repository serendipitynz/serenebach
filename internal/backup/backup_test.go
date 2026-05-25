package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/sqlite"
)

func TestRun_basic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

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
	_ = os.MkdirAll(imgDir, 0o755)
	_ = os.WriteFile(filepath.Join(imgDir, "a.jpg"), []byte("fake-image"), 0o644)

	tmplDir := filepath.Join(tmp, "tmpl")
	_ = os.MkdirAll(tmplDir, 0o755)
	_ = os.WriteFile(filepath.Join(tmplDir, "style.css"), []byte("body{}"), 0o644)

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:      dbPath,
		ImageDir:    imgDir,
		TemplateDir: tmplDir,
		OutPath:     outPath,
		Now:         time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
		Quiet:       true,
	}

	report, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.OutPath != outPath {
		t.Fatalf("outPath = %q, want %q", report.OutPath, outPath)
	}
	if report.Size == 0 {
		t.Fatal("expected non-zero size")
	}

	// Read back ZIP.
	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	paths := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		paths = append(paths, f.Name)
	}

	want := []string{"db/serenebach.db", "img/a.jpg", "templates/style.css", "manifest.json"}
	for _, p := range want {
		if !slices.Contains(paths, p) {
			t.Errorf("missing path %q in zip", p)
		}
	}

	// Verify manifest has table counts.
	mfFile := findZipFile(zr, "manifest.json")
	if mfFile == nil {
		t.Fatal("manifest.json not found")
	}
	mfBody, err := mfFile.Open()
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer mfBody.Close()
	mfRaw, _ := io.ReadAll(mfBody)
	if !bytes.Contains(mfRaw, []byte(`"weblogs": 0`)) {
		t.Errorf("manifest missing weblog count")
	}
}

func TestRun_excludeImages(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

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
	_ = os.MkdirAll(imgDir, 0o755)
	_ = os.WriteFile(filepath.Join(imgDir, "a.jpg"), []byte("fake"), 0o644)

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:   dbPath,
		ImageDir: imgDir,
		OutPath:  outPath,
		Excluded: []string{"images"},
		Quiet:    true,
		Now:      time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}

	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "img/a.jpg" {
			t.Error("img/ should be excluded")
		}
	}
}

func TestRun_stdout(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	dbPath := filepath.Join(tmp, "sb.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	// Redirect stdout to capture ZIP bytes.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	opts := Options{
		DBPath:  dbPath,
		OutPath: "-",
		Quiet:   true,
		Now:     time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}
	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	w.Close()
	zipBytes := <-done
	os.Stdout = oldStdout

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("new zip reader: %v", err)
	}

	found := false
	for _, f := range zr.File {
		if f.Name == "db/serenebach.db" {
			found = true
		}
	}
	if !found {
		t.Fatal("db/serenebach.db not found in stdout zip")
	}
}

func TestRun_vacuumSnapshot(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	dbPath := filepath.Join(tmp, "sb.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:  dbPath,
		OutPath: outPath,
		Quiet:   true,
		Now:     time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}
	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	zf := findZipFile(zr, "db/serenebach.db")
	if zf == nil {
		t.Fatal("db/serenebach.db not found")
	}

	// Extract to temp and open to verify row counts.
	rc, err := zf.Open()
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer rc.Close()

	snapshotPath := filepath.Join(tmp, "snapshot.db")
	f, err := os.Create(snapshotPath)
	if err != nil {
		t.Fatalf("create snapshot file: %v", err)
	}
	_, err = io.Copy(f, rc)
	_ = f.Close()
	if err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	snapDB, err := sqlite.Open(snapshotPath)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer snapDB.Close()

	var count int
	if err := snapDB.QueryRowContext(ctx, "SELECT count(*) FROM weblogs").Scan(&count); err != nil {
		t.Fatalf("count weblogs: %v", err)
	}
	if count != 0 {
		t.Fatalf("weblogs count = %d, want 0", count)
	}
}

func TestRun_includeAnalytics(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	dbPath := filepath.Join(tmp, "sb.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	// Create a separate analytics DB.
	analyticsPath := filepath.Join(tmp, "analytics.db")
	adb, err := sqlite.Open(analyticsPath)
	if err != nil {
		t.Fatalf("open analytics db: %v", err)
	}
	if _, err := adb.ExecContext(ctx, "CREATE TABLE page_views (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create page_views: %v", err)
	}
	if _, err := adb.ExecContext(ctx, "INSERT INTO page_views (id) VALUES (1)"); err != nil {
		t.Fatalf("insert page_views: %v", err)
	}
	adb.Close()

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:           dbPath,
		AnalyticsDBPath:  analyticsPath,
		IncludeAnalytics: true,
		OutPath:          outPath,
		Quiet:            true,
		Now:              time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}
	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	if findZipFile(zr, "db/analytics.db") == nil {
		t.Fatal("db/analytics.db not found")
	}
}

func TestRun_analyticsEmptyPathNoOp(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	dbPath := filepath.Join(tmp, "sb.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	outPath := filepath.Join(tmp, "backup.zip")
	opts := Options{
		DBPath:           dbPath,
		AnalyticsDBPath:  "", // empty → no-op even with IncludeAnalytics
		IncludeAnalytics: true,
		OutPath:          outPath,
		Quiet:            true,
		Now:              time.Date(2026, 5, 23, 9, 30, 0, 0, time.UTC),
	}
	_, err = Run(ctx, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	zr, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "db/analytics.db" {
			t.Error("analytics.db should not be included when path is empty")
		}
	}
}

func findZipFile(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
