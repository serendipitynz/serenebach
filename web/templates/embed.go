// Package templates ships the built-in default template assets that seed
// bundles into a freshly initialised database so a site renders out of the
// box without any admin setup.
package templates

import _ "embed"

//go:embed default/main.html
var DefaultMain string

//go:embed default/style.css
var DefaultCSS string
