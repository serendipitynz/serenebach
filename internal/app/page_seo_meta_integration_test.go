package app_test

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultTemplateSEOMetaHead is a regression guard (PR #129 review):
// a flat page's Summary must reach <meta name="description"> /
// og:description in the *shipped* default template, not only in custom
// templates. The per-item head meta lives in the single_meta block
// (entry permalink + flat page); the entry-only sequel block no longer
// gates it. List pages must still omit it (where {entry_excerpt} would
// otherwise resolve to the first entry's excerpt).
func TestDefaultTemplateSEOMetaHead(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	if _, err := a.DB.Exec(`INSERT INTO pages
		(wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, summary, canonical_url, noindex, created_at, updated_at)
		VALUES (1, 1, 'About', '<p>about body</p>', 'html', '/about', 0, 0, 1, '', 'page summary here', '', 0, ?, ?)`,
		now, now); err != nil {
		t.Fatalf("seed page: %v", err)
	}

	t.Run("flat page emits description + og from summary", func(t *testing.T) {
		body := httpGet(t, a.Handler(), "/about")
		for _, want := range []string{
			`<meta name="description" content="page summary here">`,
			`<meta property="og:description" content="page summary here">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("flat page head missing %q\nfull output:\n%s", want, body)
			}
		}
	})

	t.Run("entry permalink still emits description", func(t *testing.T) {
		body := httpGet(t, a.Handler(), "/entry/1/")
		if !strings.Contains(body, `<meta name="description"`) {
			t.Errorf("entry permalink head should still emit a description meta:\n%s", body)
		}
	})

	t.Run("home list omits per-item description", func(t *testing.T) {
		body := httpGet(t, a.Handler(), "/")
		if strings.Contains(body, `<meta name="description"`) {
			t.Errorf("home/list head must not emit a per-item description meta:\n%s", body)
		}
	})
}
