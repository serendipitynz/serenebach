// Package migrations ships every .sql migration bundled into the serenebach
// binary so that a CGI deployment has nothing external to carry alongside.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
