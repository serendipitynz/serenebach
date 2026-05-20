package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedSortToken(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	id, err := s.CreateMCPToken(context.Background(), 1, name, "raw-"+name,
		domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken(%q): %v", name, err)
	}
	return id
}

func TestListMCPTokens_DefaultCreatedDesc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedSortToken(t, s, "a")
	b := seedSortToken(t, s, "b")
	c := seedSortToken(t, s, "c")

	got, err := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if err != nil {
		t.Fatalf("ListMCPTokens: %v", err)
	}
	// Created at second-resolution means a/b/c may share a timestamp,
	// so the tie-breaker id DESC takes over: c (newest id) → b → a.
	if len(got) != 3 || got[0].ID != c || got[1].ID != b || got[2].ID != a {
		t.Errorf("default order: got %v, want %d/%d/%d", []int64{got[0].ID, got[1].ID, got[2].ID}, c, b, a)
	}
}

func TestListMCPTokens_SortByNameAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	c := seedSortToken(t, s, "cherry")
	a := seedSortToken(t, s, "apple")
	b := seedSortToken(t, s, "banana")

	got, err := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{SortBy: MCPTokenSortName, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListMCPTokens: %v", err)
	}
	if got[0].ID != a || got[1].ID != b || got[2].ID != c {
		t.Errorf("name ASC: got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestListMCPTokens_LastUsedZeroSortsLast(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Three tokens; touch only two to give them non-zero last_used_at.
	zero := seedSortToken(t, s, "never-used")
	used1 := seedSortToken(t, s, "used-once")
	used2 := seedSortToken(t, s, "used-twice")
	// TouchMCPToken bumps last_used_at to now.
	if err := s.TouchMCPToken(ctx, used1); err != nil {
		t.Fatalf("touch used1: %v", err)
	}
	if err := s.TouchMCPToken(ctx, used2); err != nil {
		t.Fatalf("touch used2: %v", err)
	}

	// DESC: most-recently-used first, never-used at the bottom.
	got, err := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{SortBy: MCPTokenSortLastUsed, SortDir: SortDesc})
	if err != nil {
		t.Fatalf("ListMCPTokens: %v", err)
	}
	if got[len(got)-1].ID != zero {
		t.Errorf("DESC: zero-last_used should sort last, got id %d", got[len(got)-1].ID)
	}
	if got[0].ID != used2 && got[0].ID != used1 {
		t.Errorf("DESC: most recent should be first, got id %d", got[0].ID)
	}

	// ASC: oldest-used first, never-used still at the bottom (the zero-
	// last trick keeps it there both ways).
	got, err = s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{SortBy: MCPTokenSortLastUsed, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListMCPTokens ASC: %v", err)
	}
	if got[len(got)-1].ID != zero {
		t.Errorf("ASC: zero-last_used should still sort last, got id %d", got[len(got)-1].ID)
	}
}

func TestParseMCPTokenSortKey(t *testing.T) {
	cases := []struct {
		in   string
		want MCPTokenSortKey
	}{
		{"", MCPTokenSortCreatedAt},
		{"garbage", MCPTokenSortCreatedAt},
		{"name", MCPTokenSortName},
		{"scope", MCPTokenSortScope},
		{"author", MCPTokenSortAuthor},
		{"lastUsed", MCPTokenSortLastUsed},
		{"revoked", MCPTokenSortRevoked},
	}
	for _, tc := range cases {
		if got := ParseMCPTokenSortKey(tc.in); got != tc.want {
			t.Errorf("ParseMCPTokenSortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
