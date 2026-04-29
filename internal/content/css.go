package content

import (
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// RenderTemplateCSS expands the only two sbtemplate tags that the CSS
// surface is documented to support — `{site_parts}` (the per-template
// asset URL prefix) and `{site_encoding}` — and returns the rewritten
// stylesheet body. Any other `{tag}`-shaped pattern in the source is
// left untouched so legitimate CSS that happens to use brace text
// (e.g. content selectors) survives intact.
//
// The rewrite is intentionally a plain string replacement rather than
// a full sbtemplate parse: SB-format CSS only uses these two tags in
// practice, has no `<!-- BEGIN -->` blocks, and we don't want the
// parser's blank-on-unknown-tag behaviour to silently delete content.
//
// `tmpl` is required to resolve `{site_parts}` to the right
// `/template/<id>/` prefix; passing a tmpl with ID 0 falls through to
// an empty PartsURL, which mirrors the behaviour of CSSURL when no
// template is pinned.
func RenderTemplateCSS(site Site, tmpl *domain.Template) string {
	if tmpl == nil || tmpl.CSS == "" {
		return ""
	}
	s := site.WithTemplate(tmpl)
	out := strings.ReplaceAll(tmpl.CSS, "{site_parts}", s.PartsURL())
	out = strings.ReplaceAll(out, "{site_encoding}", s.Encoding)
	return out
}
