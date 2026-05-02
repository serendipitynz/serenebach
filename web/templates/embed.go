// Package templates ships the built-in default template assets that seed
// bundles into a freshly initialised database so a site renders out of the
// box without any admin setup.
package templates

import _ "embed"

//go:embed default/main.html
var DefaultMain string

//go:embed default/style.css
var DefaultCSS string

// DefaultInfo carries the template.txt-style metadata block (Name /
// Author / Address / Version + free-form description) that gets stored
// in templates.info, so the bundled default round-trips through the
// admin UI and template-pack export the same way an imported pack would.
//
//go:embed default/info.txt
var DefaultInfo string
