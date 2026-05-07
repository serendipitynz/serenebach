package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestPageViewRenderPrefersEntryBody(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody:  "MAIN",
		EntryBody: "ENTRY",
	}
	v := PageView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.TrimSpace(out) != "ENTRY" {
		t.Errorf("expected EntryBody to win; got %q", out)
	}
}

func TestPageViewRenderFallsBackToMainBody(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody:  "MAIN",
		EntryBody: "",
	}
	v := PageView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.TrimSpace(out) != "MAIN" {
		t.Errorf("expected MainBody fallback; got %q", out)
	}
}

func TestPageViewRenderSetsEntryModeToPage(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: "{entry_mode}",
	}
	v := PageView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.TrimSpace(out) != "page" {
		t.Errorf("entry_mode = %q, want page", out)
	}
}

func TestPageViewRenderStripsEntryOnlyBlocks(t *testing.T) {
	t.Parallel()

	// Per sbtemplate parser rules, BEGIN/END must each sit on their own line.
	tmpl := &domain.Template{
		MainBody: `<!-- BEGIN entry -->
<!-- BEGIN sequel -->
sequel
<!-- END sequel -->
<!-- BEGIN comment_area -->
comment_area
<!-- END comment_area -->
<!-- BEGIN trackback_area -->
trackback_area
<!-- END trackback_area -->
<!-- BEGIN recent_trackback -->
recent_trackback
<!-- END recent_trackback -->
<!-- BEGIN profile_area -->
profile_area
<!-- END profile_area -->
<!-- BEGIN dedicated_page -->
dedicated_page
<!-- END dedicated_page -->
<!-- END entry -->`,
	}
	v := PageView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, unwanted := range []string{"sequel", "comment_area", "trackback_area", "recent_trackback", "profile_area"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output should not contain %q; got:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "dedicated_page") {
		t.Errorf("output should contain dedicated_page block; got:\n%s", out)
	}
}

func TestPageViewRenderPagePermalinks(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: "{entry_permalink}|{permalink}|{entry_og_image}",
	}
	v := PageView{
		Site:     NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com/", Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 42, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "https://example.com/about|https://example.com/about|https://example.com/img/og/page_42.png"
	if strings.TrimSpace(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestPageViewRenderFormatsBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format string
		body   string
		want   string
	}{
		{"html", "<p>hello</p>", "<p>hello</p>"},
		{"markdown", "# hello", "<h1>hello</h1>"},
	}

	for _, tc := range tests {
		tmpl := &domain.Template{
			MainBody: "{entry_description}",
		}
		v := PageView{
			Site:     NewSite(domain.Weblog{Lang: "ja"}),
			Template: tmpl,
			Page:     domain.Page{ID: 1, Title: "T", Body: tc.body, Format: tc.format, Slug: "/x"},
		}
		out, err := v.Render()
		if err != nil {
			t.Fatalf("Render(%s): %v", tc.format, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("format=%s: missing %q in %q", tc.format, tc.want, out)
		}
	}
}

func TestPageViewRenderWithDate(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: "{entry_date}|{entry_time}|{entry_disp_time}",
	}
	posted := time.Date(2026, 5, 8, 14, 30, 0, 0, time.UTC)
	v := PageView{
		Site:     NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com/", Lang: "ja"}),
		Template: tmpl,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about", CreatedAt: posted},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "2026") {
		t.Errorf("expected year in output; got %q", out)
	}
}

func TestPageViewRenderNoTemplate(t *testing.T) {
	t.Parallel()

	v := PageView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: nil,
		Page:     domain.Page{ID: 1, Title: "T", Body: "B", Format: "html", Slug: "/about"},
	}
	_, err := v.Render()
	if err == nil {
		t.Fatal("expected error for nil template")
	}
}
