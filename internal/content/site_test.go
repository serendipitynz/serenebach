package content

import (
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

func TestSiteApplyCustomTags(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<html>{custom_hello}{custom_html}</html>`,
	}
	site := NewSite(domain.Weblog{Lang: "ja"}).WithCustomTags([]domain.CustomTag{
		{Name: "custom_hello", Value: "world"},
		{Name: "custom_html", Value: "<b>bold</b>"},
	})

	parsed, err := sbtemplate.Parse(tmpl.MainBody, sbtemplate.DefaultCallback)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := parsed.New()
	site.Apply(c)
	out := c.Render()

	if !strings.Contains(out, "world") {
		t.Errorf("missing custom_hello expansion: %q", out)
	}
	if !strings.Contains(out, "<b>bold</b>") {
		t.Errorf("missing custom_html expansion (raw HTML): %q", out)
	}
}

func TestSiteApplyCustomTagsDoNotEscapeHTML(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<html>{custom_script}</html>`,
	}
	site := NewSite(domain.Weblog{Lang: "ja"}).WithCustomTags([]domain.CustomTag{
		{Name: "custom_script", Value: `<script>alert(1)</script>`},
	})

	parsed, err := sbtemplate.Parse(tmpl.MainBody, sbtemplate.DefaultCallback)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := parsed.New()
	site.Apply(c)
	out := c.Render()

	// Raw HTML must pass through unescaped.
	if !strings.Contains(out, `<script>alert(1)</script>`) {
		t.Errorf("expected raw script tag in output, got: %q", out)
	}
	if strings.Contains(out, `&lt;script&gt;`) {
		t.Errorf("custom tag value was escaped; got: %q", out)
	}
}
