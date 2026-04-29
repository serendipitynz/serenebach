package content

import (
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestRenderTemplateCSSExpandsSiteParts(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		ID:  42,
		CSS: "body { background: url({site_parts}sb_body_bg.gif); }\n",
	}
	out := RenderTemplateCSS(NewSite(domain.Weblog{Lang: "ja"}), tmpl)
	if !strings.Contains(out, "url(/template/42/sb_body_bg.gif)") {
		t.Errorf("site_parts not expanded; out:\n%s", out)
	}
	if strings.Contains(out, "{site_parts}") {
		t.Errorf("raw {site_parts} marker leaked; out:\n%s", out)
	}
}

func TestRenderTemplateCSSExpandsSiteEncoding(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{ID: 1, CSS: `@charset "{site_encoding}";` + "\n"}
	out := RenderTemplateCSS(NewSite(domain.Weblog{Lang: "ja"}), tmpl)
	if !strings.Contains(out, `@charset "utf-8";`) {
		t.Errorf("site_encoding not expanded; out:\n%s", out)
	}
}

func TestRenderTemplateCSSLeavesUnknownTagsAlone(t *testing.T) {
	t.Parallel()

	// Unknown {tag} patterns must NOT be silently blanked out — that
	// would corrupt CSS that legitimately uses `{}` for declaration
	// blocks (though unlikely to look exactly like `{word}`).
	tmpl := &domain.Template{ID: 1, CSS: ".a { color: red } .b { color: {something_else}; }\n"}
	out := RenderTemplateCSS(NewSite(domain.Weblog{}), tmpl)
	if !strings.Contains(out, "{something_else}") {
		t.Errorf("unknown tag must be preserved; out:\n%s", out)
	}
}

func TestRenderTemplateCSSEmptyInputs(t *testing.T) {
	t.Parallel()

	if got := RenderTemplateCSS(NewSite(domain.Weblog{}), nil); got != "" {
		t.Errorf("nil tmpl should yield \"\", got %q", got)
	}
	if got := RenderTemplateCSS(NewSite(domain.Weblog{}), &domain.Template{CSS: ""}); got != "" {
		t.Errorf("empty CSS should yield \"\", got %q", got)
	}
}

func TestRenderTemplateCSSSitePartsEmptyWhenNoTemplateID(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{ID: 0, CSS: "body { background: url({site_parts}x.gif); }\n"}
	out := RenderTemplateCSS(NewSite(domain.Weblog{}), tmpl)
	// PartsURL falls back to "" when TemplateID is 0; the marker
	// should be replaced with the empty string.
	if strings.Contains(out, "{site_parts}") {
		t.Errorf("site_parts should still be replaced even with id=0; out:\n%s", out)
	}
	if !strings.Contains(out, "url(x.gif)") {
		t.Errorf("expected bare url(x.gif); out:\n%s", out)
	}
}
