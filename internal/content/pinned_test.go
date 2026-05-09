package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// entryPageTmpl is a minimal template with both {entry_pinned} tag and
// pinned_entry sub-block, used by single-entry page tests.
var entryPageTmpl = &domain.Template{
	MainBody: `
<!-- BEGIN entry -->
<article class="{entry_pinned}">
<!-- BEGIN pinned_entry -->
[PINNED]
<!-- END pinned_entry -->
{entry_title}
</article>
<!-- END entry -->
`,
}

func renderEntryView(pinned bool) (string, error) {
	v := EntryView{
		Template: entryPageTmpl,
		Site:     NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"}),
		Entry: domain.Entry{
			ID: 1, Title: "Post", Body: "b", Format: "html",
			Status: domain.EntryPublished, PostedAt: time.Now(), Pinned: pinned,
		},
	}
	return v.Render()
}

// Single-entry page: pinned_entry block IS per-entry (only one entry rendered).
func TestEntryViewPinnedEntryBlock(t *testing.T) {
	t.Parallel()

	out, err := renderEntryView(true)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "[PINNED]") {
		t.Errorf("pinned_entry block should render on single-entry page when pinned; got:\n%s", out)
	}
	if !strings.Contains(out, `class="pinned"`) {
		t.Errorf("{entry_pinned} should yield \"pinned\" on single-entry page; got:\n%s", out)
	}
}

func TestEntryViewPinnedEntryBlockHiddenWhenNotPinned(t *testing.T) {
	t.Parallel()

	out, err := renderEntryView(false)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "[PINNED]") {
		t.Errorf("pinned_entry block must not render when entry is not pinned; got:\n%s", out)
	}
	if strings.Contains(out, `class="pinned"`) {
		t.Errorf("{entry_pinned} should yield \"\" when not pinned; got:\n%s", out)
	}
}

func pinnedListView(pinned bool) ListView {
	tmpl := &domain.Template{
		Name: "t",
		MainBody: `
<!-- BEGIN entry -->
<article class="{entry_pinned}">
<!-- BEGIN pinned_entry -->
[PIN]
<!-- END pinned_entry -->
{entry_title}
</article>
<!-- END entry -->
`,
	}
	return ListView{
		Template: tmpl,
		Site:     NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"}),
		Entries: []domain.Entry{
			{ID: 1, Title: "Pinned post", Body: "b", Format: "html",
				Status: domain.EntryPublished, PostedAt: time.Now(), Pinned: pinned},
		},
	}
}

func TestListViewEntryPinnedTag(t *testing.T) {
	t.Parallel()

	out, err := pinnedListView(true).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, `class="pinned"`) {
		t.Errorf("{entry_pinned} should yield \"pinned\" for a pinned entry; got:\n%s", out)
	}
}

func TestListViewEntryPinnedTagEmpty(t *testing.T) {
	t.Parallel()

	out, err := pinnedListView(false).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, `class="pinned"`) {
		t.Errorf("{entry_pinned} should yield \"\" for a non-pinned entry; got:\n%s", out)
	}
}

// TestListViewPinnedEntryBlockAlways0Striped verifies that pinned_entry is
// 0-striped on list pages. sbtemplate block counts are global (not per-
// iteration), so a sub-block inside entry cannot change value per iteration.
// Use {entry_pinned} tag for per-entry conditional styling on list pages.
func TestListViewPinnedEntryBlockAlways0Striped(t *testing.T) {
	t.Parallel()

	// Even a single pinned entry must NOT render pinned_entry block on list.
	out, err := pinnedListView(true).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "[PIN]") {
		t.Errorf("pinned_entry block must be 0-striped on list pages; got:\n%s", out)
	}
}
