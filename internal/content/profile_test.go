package content

import (
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestProfileViewRendersProfileAreaBlock(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<!doctype html><html lang="{site_lang}">
<head><title>{site_title}</title></head>
<body>
<!-- BEGIN title -->
<h1>{blog_name_only}</h1>
<!-- END title -->
<!-- BEGIN entry -->
<article>should be stripped on profile</article>
<!-- END entry -->
<!-- BEGIN profile_area -->
<section class="profile"
  data-mode="{mode_name}"
  data-id="{mode_id}">
<h2>{profile_name}</h2>
<p class="login">{user_name}</p>
<div class="bio">{profile_description}</div>
</section>
<!-- END profile_area -->
</body></html>
`,
	}
	v := ProfileView{
		Site:     NewSite(domain.Weblog{ID: 1, Title: "Example", BaseURL: "https://example.com", Lang: "ja"}),
		Template: tmpl,
		User: domain.User{
			ID:                7,
			WID:               1,
			Name:              "jdoe",
			DisplayName:       "Jane Doe",
			Description:       "Hello world",
			DescriptionFormat: "html",
			ListVisible:       true,
		},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`data-mode="profile"`,
		`data-id="7"`,
		`<h2>Jane Doe</h2>`,
		`<p class="login">jdoe</p>`,
		`<div class="bio">Hello world</div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "should be stripped on profile") {
		t.Errorf("entry block should be stripped on /profile/:\n%s", out)
	}
}

func TestProfileViewEscapesDisplayName(t *testing.T) {
	t.Parallel()
	tmpl := &domain.Template{
		MainBody: "<!-- BEGIN profile_area -->\n{profile_name}\n<!-- END profile_area -->\n",
	}
	v := ProfileView{
		Site:     NewSite(domain.Weblog{Lang: "en"}),
		Template: tmpl,
		User:     domain.User{ID: 1, Name: "evil", DisplayName: "<script>bad</script>"},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("display name not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag:\n%s", out)
	}
}
