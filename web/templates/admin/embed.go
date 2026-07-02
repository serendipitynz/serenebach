// Package admintpl ships the admin UI assets (templates + css + js) as
// an embedded filesystem so a single binary can serve the entire admin
// surface without extra files on disk.
package admintpl

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// DevRoot, when non-empty, causes FS(), Raw(), and I18nCatalogues() to
// read from disk instead of the embedded FS. Set this during local
// development so template edits are reflected without rebuilding.
var DevRoot string

//go:embed *.html *.js modules assets i18n css
var files embed.FS

// adminCSSSources is the fixed concatenation order for the admin
// stylesheet, which is authored per-component under css/ but served as a
// single /admin/static/admin.css bundle. The cascade order IS this
// order — never derive it from fs.WalkDir / directory sorting, and keep
// dark-theme in its original position (right after tokens) so the
// cascade matches the pre-split single file.
var adminCSSSources = []string{
	"css/00-tokens.css",
	"css/10-theme.css",
	"css/20-layout.css",
	"css/30-components.css",
	"css/40-features.css",
	"css/90-responsive.css",
}

// AdminCSSBundle concatenates the per-component stylesheet sources in
// adminCSSSources order and returns the combined bytes. Both the
// /admin/static/admin.css route and `extract-assets` go through this, so
// the served and extracted stylesheets stay identical. Reads honour
// DevRoot via Raw, so the bundle works from the embedded FS and from
// disk during development. The sources are concatenated verbatim; each
// ends with a newline so no rule glues onto the next file's first rule.
func AdminCSSBundle() ([]byte, error) {
	var buf bytes.Buffer
	for _, name := range adminCSSSources {
		b, err := Raw(name)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}

// I18nCatalogues returns the admin-side string catalogues as
// locale-code → JSON bytes, ready to hand to i18n.LoadBundle. Kept
// on the template package because the catalogues live next to the
// templates they serve.
func I18nCatalogues() (map[string][]byte, error) {
	out := map[string][]byte{}
	var entries []fs.DirEntry
	var err error
	if DevRoot != "" {
		entries, err = os.ReadDir(filepath.Join(DevRoot, "i18n"))
	} else {
		entries, err = files.ReadDir("i18n")
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var data []byte
		if DevRoot != "" {
			data, err = os.ReadFile(filepath.Join(DevRoot, "i18n", e.Name()))
		} else {
			data, err = files.ReadFile("i18n/" + e.Name())
		}
		if err != nil {
			return nil, err
		}
		code := e.Name()
		if idx := lastDot(code); idx >= 0 {
			code = code[:idx]
		}
		out[code] = data
	}
	return out, nil
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// FS returns the embedded filesystem so handler code can parse templates
// and static handlers can serve assets out of it.
func FS() fs.FS {
	if DevRoot != "" {
		return os.DirFS(DevRoot)
	}
	return files
}

// Raw returns the bytes of one embedded asset. Convenient for the
// admin.css / admin.js endpoints so they don't need a generic file
// server — we know exactly which two files are public.
func Raw(name string) ([]byte, error) {
	if DevRoot != "" {
		return os.ReadFile(filepath.Join(DevRoot, name))
	}
	return files.ReadFile(name)
}
