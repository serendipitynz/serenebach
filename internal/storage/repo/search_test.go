package repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestToFTSQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"foo", `"foo"`},
		{"foo bar", `"foo" "bar"`},
		{"machine learning", `"machine" "learning"`},
		{"機械学習", `"機械学習"`},
		// 2-character Japanese token dropped (handled by LIKE).
		{"東京", ""},
		// Mixed: long ja phrase + short tokens; only >=3 char tokens kept.
		{"東京 タワー", `"タワー"`},
		// Operators safely embedded in phrases.
		{"max-age", `"max-age"`},
		{"cats AND dogs", `"cats" "AND" "dogs"`},
		{"node:fs", `"node:fs"`},
		{"foo(bar)", `"foo(bar)"`},
		{"A*B", `"A*B"`},
		// `"` is doubled per FTS5 phrase-escape rules.
		{`foo"bar`, `"foo""bar"`},
		// Metacharacters-only — long enough to MATCH but won't hit anything.
		{"***", `"***"`},
		// Single short token disappears.
		{"a", ""},
	}
	for _, tc := range cases {
		if got := ToFTSQuery(tc.in); got != tc.want {
			t.Errorf("ToFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLikeNeedles(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"机器学习", nil},        // >=3 chars → MATCH path
		{"machine", nil},      // >=3 chars → MATCH path
		{"東京", []string{`%東京%`}},
		// `_` and `%` escaped, `:` left literal.
		{"a%", []string{`%a\%%`}},
		{`a_b`, nil}, // 3 chars → MATCH path, not LIKE
		// Two short tokens: each gets its own needle.
		{"東京 大阪", []string{`%東京%`, `%大阪%`}},
		// Mixed: long token is omitted; short token is captured.
		{"東京 タワー", []string{`%東京%`}},
	}
	for _, tc := range cases {
		got := LikeNeedles(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("LikeNeedles(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("LikeNeedles(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestHasSearchTerms(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"foo", true}, // MATCH path
		{"a", true},   // 1-char token still captured via LIKE
		{"東京", true},  // LIKE path
		{"foo bar", true},      // MATCH
		{"foo 東京", true},       // MATCH AND LIKE
		{"***", true},          // 3+ chars, treated as MATCH (will yield 0 rows)
	}
	for _, tc := range cases {
		if got := HasSearchTerms(tc.in); got != tc.want {
			t.Errorf("HasSearchTerms(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// seedSearchEntry inserts an entry with the given fields and returns
// its id. The Status field is taken as-is — callers needing the
// default "published" pass domain.EntryPublished explicitly because
// EntryDraft's underlying value is 0 (the Go zero value), so we can't
// distinguish "unset" from "explicit draft" here.
func seedSearchEntry(t *testing.T, s *Store, e domain.Entry) int64 {
	t.Helper()
	if e.WID == 0 {
		e.WID = 1
	}
	if e.AuthorID == 0 {
		e.AuthorID = 1
	}
	if e.Format == "" {
		e.Format = "html"
	}
	if e.PostedAt.IsZero() {
		e.PostedAt = time.Now()
	}
	id, err := s.CreateEntry(context.Background(), e)
	if err != nil {
		t.Fatalf("CreateEntry(%q): %v", e.Title, err)
	}
	return id
}

// seedHiddenCategory creates a category flagged hidden and returns its
// id. The category's wid is fixed to 1 to match the rest of the test
// fixtures.
func seedHiddenCategory(t *testing.T, s *Store) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := s.CreateCategory(ctx, domain.Category{
		WID:    1,
		Name:   "hidden",
		Slug:   "hidden",
		Hidden: true,
	}, 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	return id
}

func TestSearchEntriesPublic_MatchTitleBodyMoreKeywords(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tid := seedSearchEntry(t, s, domain.Entry{Title: "alphabet", Body: "irrelevant", Status: domain.EntryPublished, PostedAt: time.Now()})
	bid := seedSearchEntry(t, s, domain.Entry{Title: "x", Body: "beta carotene", Status: domain.EntryPublished, PostedAt: time.Now().Add(-time.Hour)})
	mid := seedSearchEntry(t, s, domain.Entry{Title: "y", Body: "y", More: "delta force", Status: domain.EntryPublished, PostedAt: time.Now().Add(-2 * time.Hour)})
	kid := seedSearchEntry(t, s, domain.Entry{Title: "z", Body: "z", Keywords: "epsilon", Status: domain.EntryPublished, PostedAt: time.Now().Add(-3 * time.Hour)})

	for _, c := range []struct {
		query   string
		wantIDs []int64
	}{
		{"alphabet", []int64{tid}},
		{"beta", []int64{bid}},
		{"delta", []int64{mid}},
		{"epsilon", []int64{kid}},
	} {
		got, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: c.query, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("SearchEntriesPublic(%q): %v", c.query, err)
		}
		if len(got.Entries) != len(c.wantIDs) {
			t.Fatalf("SearchEntriesPublic(%q) returned %d, want %d", c.query, len(got.Entries), len(c.wantIDs))
		}
		for i, id := range c.wantIDs {
			if got.Entries[i].ID != id {
				t.Errorf("SearchEntriesPublic(%q)[%d] = %d, want %d", c.query, i, got.Entries[i].ID, id)
			}
		}
	}
}

func TestSearchEntriesPublic_ExcludesDraftAndHidden(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hiddenID := seedHiddenCategory(t, s)
	_ = seedSearchEntry(t, s, domain.Entry{Title: "lambda 1", Body: "x", Status: domain.EntryDraft})
	_ = seedSearchEntry(t, s, domain.Entry{Title: "lambda 2", Body: "x", Status: domain.EntryPublished, CategoryID: hiddenID})
	visible := seedSearchEntry(t, s, domain.Entry{Title: "lambda 3", Body: "x", Status: domain.EntryPublished})

	res, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "lambda", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchEntriesPublic: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].ID != visible {
		t.Fatalf("want only visible id %d, got %+v", visible, res.Entries)
	}
}

func TestSearchEntriesPublic_PaginationAndZeroHits(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		seedSearchEntry(t, s, domain.Entry{Title: "kappa entry", Body: "kappa", Status: domain.EntryPublished, PostedAt: time.Now().Add(-time.Duration(i) * time.Hour)})
	}
	res, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "kappa", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("SearchEntriesPublic: %v", err)
	}
	if res.Total != 5 || len(res.Entries) != 2 {
		t.Fatalf("expected total=5 page-1 len=2, got total=%d len=%d", res.Total, len(res.Entries))
	}
	res, err = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "kappa", Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("SearchEntriesPublic page 3: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 3, got %d", len(res.Entries))
	}
	res, err = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "nothingmatches", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchEntriesPublic no-hit: %v", err)
	}
	if res.Total != 0 || len(res.Entries) != 0 {
		t.Fatalf("expected zero hits, got %+v", res)
	}
}

func TestSearchEntriesPublic_MixedTokenAndLongClause(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	want := seedSearchEntry(t, s, domain.Entry{Title: "東京タワー", Body: "東京 タワー", Status: domain.EntryPublished})
	_ = seedSearchEntry(t, s, domain.Entry{Title: "タワー記事", Body: "本文", Status: domain.EntryPublished}) // タワー but no 東京

	// Mixed query "東京 タワー" — タワー goes through MATCH, 東京 through LIKE.
	res, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "東京 タワー", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchEntriesPublic: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].ID != want {
		t.Fatalf("want id %d, got %+v", want, res.Entries)
	}
}

func TestSearchEntriesPublic_TwoCharTokensOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	want := seedSearchEntry(t, s, domain.Entry{Title: "東京と大阪", Body: "x", Status: domain.EntryPublished})
	_ = seedSearchEntry(t, s, domain.Entry{Title: "東京", Body: "x", Status: domain.EntryPublished}) // missing 大阪

	// Both tokens are 2-char → LIKE-only path.
	res, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "東京 大阪", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchEntriesPublic: %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].ID != want {
		t.Fatalf("want id %d, got %+v", want, res.Entries)
	}
}

func TestSearchEntriesPublic_EmptyQuery(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedSearchEntry(t, s, domain.Entry{Title: "alpha", Body: "x", Status: domain.EntryPublished})

	res, err := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchEntriesPublic empty: %v", err)
	}
	if res == nil || res.Total != 0 || len(res.Entries) != 0 {
		t.Fatalf("empty query should yield no rows, got %+v", res)
	}
}

func TestSearchPublishedEntriesMCP_NoHiddenExclusion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	hiddenID := seedHiddenCategory(t, s)
	want := seedSearchEntry(t, s, domain.Entry{Title: "mu entry", Body: "x", Status: domain.EntryPublished, CategoryID: hiddenID})

	got, err := s.SearchPublishedEntries(ctx, 1, "mu entry", 10)
	if err != nil {
		t.Fatalf("SearchPublishedEntries: %v", err)
	}
	if len(got) != 1 || got[0].ID != want {
		t.Fatalf("MCP search should return hidden-category entries; got %+v", got)
	}
}

func TestSearchPublishedEntriesMCP_PublishedOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = seedSearchEntry(t, s, domain.Entry{Title: "nu draft", Body: "x", Status: domain.EntryDraft})
	want := seedSearchEntry(t, s, domain.Entry{Title: "nu published", Body: "x", Status: domain.EntryPublished})

	got, err := s.SearchPublishedEntries(ctx, 1, "nu", 10)
	if err != nil {
		t.Fatalf("SearchPublishedEntries: %v", err)
	}
	if len(got) != 1 || got[0].ID != want {
		t.Fatalf("MCP search should drop drafts; got %+v", got)
	}
}

func TestSearchEntriesPublic_TriggerSyncOnInsertUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// INSERT: trigger should make the entry searchable without reindex.
	id := seedSearchEntry(t, s, domain.Entry{Title: "omicron", Body: "first", Status: domain.EntryPublished})
	res, _ := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "omicron", Page: 1, PageSize: 10})
	if len(res.Entries) != 1 {
		t.Fatalf("expected INSERT trigger to make entry searchable, got %d", len(res.Entries))
	}

	// UPDATE: change title; old token should disappear, new should appear.
	e, err := s.EntryByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("EntryByID: %v", err)
	}
	e.Title = "rebranded"
	if err := s.UpdateEntry(ctx, *e); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	res, _ = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "omicron", Page: 1, PageSize: 10})
	if len(res.Entries) != 0 {
		t.Fatalf("expected old token to drop from index after UPDATE")
	}
	res, _ = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "rebranded", Page: 1, PageSize: 10})
	if len(res.Entries) != 1 {
		t.Fatalf("expected new token after UPDATE, got %d", len(res.Entries))
	}

	// DELETE: row disappears from index.
	if err := s.DeleteEntry(ctx, 1, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	res, _ = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "rebranded", Page: 1, PageSize: 10})
	if len(res.Entries) != 0 {
		t.Fatalf("expected entry to drop from index after DELETE")
	}
}

func TestRebuildFTSIndex(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := seedSearchEntry(t, s, domain.Entry{Title: "pi entry", Body: "x", Status: domain.EntryPublished})

	// Drop the row out of the index manually (simulating drift).
	if _, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts WHERE rowid = ?`, id); err != nil {
		t.Fatalf("manual delete from fts: %v", err)
	}
	res, _ := s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "pi entry", Page: 1, PageSize: 10})
	if len(res.Entries) != 0 {
		t.Fatalf("precondition: index should be empty after manual desync")
	}

	if err := s.RebuildFTSIndex(ctx); err != nil {
		t.Fatalf("RebuildFTSIndex: %v", err)
	}
	res, _ = s.SearchEntriesPublic(ctx, SearchPublicOptions{WID: 1, Query: "pi entry", Page: 1, PageSize: 10})
	if len(res.Entries) != 1 || res.Entries[0].ID != id {
		t.Fatalf("expected RebuildFTSIndex to restore entry, got %+v", res.Entries)
	}
}

func TestSearchAdminListUsesFTS(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	want := seedSearchEntry(t, s, domain.Entry{Title: "sigma admin", Body: "x", Status: domain.EntryPublished})
	other := seedSearchEntry(t, s, domain.Entry{Title: "unrelated", Body: "y", Status: domain.EntryPublished})

	out, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "sigma"})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(out) != 1 || out[0].ID != want {
		t.Fatalf("expected one hit %d, got %+v", want, out)
	}
	// Sanity: the non-matching entry isn't returned.
	if len(out) > 0 && out[0].ID == other {
		t.Fatalf("non-matching entry leaked through filter")
	}
}

func TestSearchAdminListEmptyMatchedTokens(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	seedSearchEntry(t, s, domain.Entry{Title: "tau", Body: "x", Status: domain.EntryPublished})

	// A query that normalises non-empty but produces no usable tokens
	// would yield AND 1=0 in the admin SQL. Here we use a single
	// 1-character token: ToFTSQuery returns "" and LikeNeedles returns
	// the LIKE needle for that char, so it still routes. Use a single
	// metacharacter that's at least 3 chars to lock the test.
	out, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "**!"})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("metacharacter-only query should yield zero rows; got %d", len(out))
	}
}

func TestSearchAdminListOwnerRestriction(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	mine := seedSearchEntry(t, s, domain.Entry{Title: "upsilon entry", AuthorID: 1, Body: "x", Status: domain.EntryPublished})
	yours := seedSearchEntry(t, s, domain.Entry{Title: "upsilon entry", AuthorID: 2, Body: "x", Status: domain.EntryPublished})

	owner := int64(1)
	out, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "upsilon", OwnerID: &owner})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(out) != 1 || out[0].ID != mine {
		t.Fatalf("owner-restricted search should isolate owner's hit %d (yours=%d); got %+v", mine, yours, out)
	}
}

// TestToFTSQueryLongUnsegmentedClause documents the known limitation
// (handoff §4.4): a single long, unspaced query becomes one big MATCH
// phrase and matches only entries containing that exact substring.
func TestToFTSQueryLongUnsegmentedClause(t *testing.T) {
	q := "機械学習による画像認識の最新手法"
	got := ToFTSQuery(q)
	want := `"` + q + `"`
	if got != want {
		t.Fatalf("ToFTSQuery long clause = %q, want %q", got, want)
	}
	if strings.Count(got, " ") != 0 {
		t.Errorf("unsegmented input should produce a single phrase, not multiple")
	}
}
