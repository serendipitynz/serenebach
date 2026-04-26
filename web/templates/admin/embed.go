// Package admintpl ships the admin UI assets (templates + css + js) as
// an embedded filesystem so a single binary can serve the entire admin
// surface without extra files on disk.
package admintpl

import (
	"embed"
	"io/fs"
)

//go:embed *.html *.css *.js assets i18n
var files embed.FS

// I18nCatalogues returns the admin-side string catalogues as
// locale-code → JSON bytes, ready to hand to i18n.LoadBundle. Kept
// on the template package because the catalogues live next to the
// templates they serve.
func I18nCatalogues() (map[string][]byte, error) {
	out := map[string][]byte{}
	entries, err := files.ReadDir("i18n")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := files.ReadFile("i18n/" + e.Name())
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
func FS() fs.FS { return files }

// Raw returns the bytes of one embedded asset. Convenient for the
// admin.css / admin.js endpoints so they don't need a generic file
// server — we know exactly which two files are public.
func Raw(name string) ([]byte, error) {
	return files.ReadFile(name)
}
