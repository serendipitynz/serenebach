package app

import (
	"net/http"
	"os"
	"path"
	"strings"
)

// rootedFileServer returns an http.Handler that serves files under
// rootDir while refusing path traversal — including symlinks that
// point outside the root. urlPrefix is stripped from r.URL.Path
// before lookup and is expected to end with "/".
//
// This is the symlink-safe replacement for
//
//	http.StripPrefix(urlPrefix, http.FileServer(http.Dir(rootDir)))
//
// Directory listings are intentionally disabled (404), matching the
// read-only-asset shape we want for /img and /template.
func rootedFileServer(rootDir, urlPrefix string) (http.Handler, error) {
	// Ensure the root exists so OpenRoot succeeds even on a fresh
	// install where the operator has not uploaded anything yet.
	// http.Dir tolerated a missing directory by returning 404 per
	// request; preserve that ergonomics by creating it up front.
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, urlPrefix)
		// path.Clean keeps a leading slash; trim it so OpenRoot sees
		// a relative path. Anything that resolves to "" or "." is the
		// directory itself — we don't list, so 404.
		rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
		if rel == "" || rel == "." {
			http.NotFound(w, r)
			return
		}
		f, err := root.Open(rel)
		if err != nil {
			// Covers ENOENT, EACCES, traversal attempts, and any
			// symlink that escapes the root. Collapse all of them
			// to 404 so we don't leak filesystem layout.
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
	}), nil
}
