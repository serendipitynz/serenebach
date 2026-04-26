// Package publici18n ships the reader-facing string catalogues the
// engine emits itself (comment submission errors, sidebar fallback
// labels). Admin-authored sbtemplate content is untouched — only
// the handful of engine-generated strings live here.
package publici18n

import "embed"

//go:embed *.json
var files embed.FS

// Catalogues returns locale-code → JSON bytes, ready for i18n.LoadBundle.
func Catalogues() (map[string][]byte, error) {
	out := map[string][]byte{}
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		code := name
		for i := len(code) - 1; i >= 0; i-- {
			if code[i] == '.' {
				code = code[:i]
				break
			}
		}
		out[code] = data
	}
	return out, nil
}
