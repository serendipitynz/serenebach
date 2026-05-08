package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fetch issues a request through h and returns status + body.
func fetch(t *testing.T, h http.Handler, method, target string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestRootedFileServer_ServesRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, err := rootedFileServer(dir, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}

	code, body := fetch(t, h, http.MethodGet, "/img/hello.txt")
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%q", code, body)
	}
	if body != "hi" {
		t.Fatalf("body=%q want %q", body, "hi")
	}

	// HEAD should report 200 with empty body but Content-Length.
	req := httptest.NewRequest(http.MethodHead, "/img/hello.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body should be empty, got %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "2" {
		t.Fatalf("HEAD Content-Length=%q want 2", rec.Header().Get("Content-Length"))
	}
}

func TestRootedFileServer_ServesNestedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "x.txt"), []byte("deep"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, err := rootedFileServer(dir, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	code, body := fetch(t, h, http.MethodGet, "/img/sub/deep/x.txt")
	if code != http.StatusOK || body != "deep" {
		t.Fatalf("status=%d body=%q", code, body)
	}
}

func TestRootedFileServer_RejectsDotDot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Place a "secret" sibling that traversal would expose.
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	h, err := rootedFileServer(root, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}

	// Even raw, the cleaning should normalise this away. Encode both
	// forms to make sure neither slips past cleaning + os.Root.
	for _, p := range []string{"/img/../secret.txt", "/img/sub/../../secret.txt"} {
		code, body := fetch(t, h, http.MethodGet, p)
		if code != http.StatusNotFound {
			t.Fatalf("%s: status=%d body=%q want 404", p, code, body)
		}
		if strings.Contains(body, "nope") {
			t.Fatalf("%s: leaked secret content", p)
		}
	}
}

func TestRootedFileServer_RejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on Windows test runners")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("escape"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	// Plant a symlink inside the root pointing outside it. os.Root
	// should refuse to follow links that resolve outside the root.
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	h, err := rootedFileServer(root, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	code, body := fetch(t, h, http.MethodGet, "/img/leak")
	if code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q want 404", code, body)
	}
	if strings.Contains(body, "escape") {
		t.Fatalf("leaked secret content via symlink")
	}
}

func TestRootedFileServer_AllowsInternalSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on Windows test runners")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Symlink that resolves *within* the root — this is legitimate
	// (e.g. operator deduplicates an asset). Use a relative target so
	// path resolution stays anchored to the root rather than chasing
	// macOS's /var → /private/var canonicalisation.
	if err := os.Symlink("target.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	h, err := rootedFileServer(root, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	code, body := fetch(t, h, http.MethodGet, "/img/alias.txt")
	if code != http.StatusOK || body != "inside" {
		t.Fatalf("status=%d body=%q want 200 inside", code, body)
	}
}

func TestRootedFileServer_DirectoryReturns404(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "subdir", "index.html"), []byte("<html/>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, err := rootedFileServer(root, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	for _, p := range []string{"/img/", "/img/subdir", "/img/subdir/"} {
		code, _ := fetch(t, h, http.MethodGet, p)
		if code != http.StatusNotFound {
			t.Fatalf("%s: status=%d want 404", p, code)
		}
	}
}

func TestRootedFileServer_MissingFileReturns404(t *testing.T) {
	root := t.TempDir()
	h, err := rootedFileServer(root, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	code, _ := fetch(t, h, http.MethodGet, "/img/missing.txt")
	if code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", code)
	}
}

func TestRootedFileServer_CreatesMissingRoot(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "not-yet-created", "img")
	h, err := rootedFileServer(target, "/img/")
	if err != nil {
		t.Fatalf("rootedFileServer: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("root dir not created: %v", err)
	}
	code, _ := fetch(t, h, http.MethodGet, "/img/anything.txt")
	if code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", code)
	}
}
