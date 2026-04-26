// Package version centralises the visible app version + build metadata.
// Rendered in the admin footer and available to settings / about pages.
package version

import (
	"runtime/debug"
	"strings"
)

// Public is the human-facing version shown on every admin page. Bump it
// manually on notable releases — kept as a string so date-suffix schemes
// (e.g. "4.0-preview") are free to land without code changes.
const Public = "4.0"

// Build returns a short commit-hash-like build identifier pulled from
// the Go toolchain's embedded VCS stamp. Empty when the binary was
// built outside a VCS or with `-buildvcs=false` — in that case the
// footer simply omits the build segment.
func Build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return ""
	}
	// 7-char short hash is the git convention and what GitHub / most UIs
	// surface. Append a "+dirty" tag when the working tree had uncommitted
	// changes at build time so the admin can tell dev builds apart at a
	// glance.
	short := revision
	if len(short) > 7 {
		short = short[:7]
	}
	if strings.EqualFold(modified, "true") {
		short += "+dirty"
	}
	return short
}

// Full returns "vX.Y (build abcdef1)" — the human-readable string the
// admin footer renders.
func Full() string {
	var b strings.Builder
	b.WriteString("v")
	b.WriteString(Public)
	if bld := Build(); bld != "" {
		b.WriteString(" (build ")
		b.WriteString(bld)
		b.WriteString(")")
	}
	return b.String()
}
