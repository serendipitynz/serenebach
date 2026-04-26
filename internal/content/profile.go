package content

import (
	"fmt"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// ProfileView renders the public `/profile/{id}/` page — SB3's
// `?pid=N` mode=user route. The user-detail content lives in the
// `profile_area` block; everything else (entry loop, comment_area,
// sequel, option) is stripped to 0 because this page is not about a
// specific entry.
type ProfileView struct {
	Site         Site
	Template     *domain.Template
	User         domain.User
	ProfileUsers []domain.User
	Sidebar      SidebarData
	CSRFToken    string
}

func (v ProfileView) Render() (string, error) {
	if v.Template == nil || v.Template.MainBody == "" {
		return "", fmt.Errorf("content.ProfileView: no template main body")
	}

	tmpl, err := sbtemplate.Parse(v.Template.MainBody, sbtemplate.DefaultCallback)
	if err != nil {
		return "", fmt.Errorf("content.ProfileView: parse: %w", err)
	}
	c := tmpl.New()

	v.Site.
		WithTemplate(v.Template).
		WithMode("user", strconv.FormatInt(v.User.ID, 10)).
		WithPageSuffix(displayName(v.User)).
		Apply(c)
	c.Tag("csrf_token", v.CSRFToken)

	// The header `title` block still shows; every entry-flavoured
	// block is stripped because there's no entry content on a
	// profile page.
	c.Block("title", 1)
	for _, blk := range []string{"entry", "option", "sequel", "comment_area", "comment", "trackback_area", "recent_trackback", "page"} {
		if tmpl.HasBlock(blk) {
			c.Block(blk, 0)
		}
	}

	applyProfileAreaBlock(c, tmpl, v.User)
	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)

	return c.Render(), nil
}
