package repo

import (
	"context"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedAdminListMessage(t *testing.T, s *Store, m domain.Message, ageMinutes int) int64 {
	t.Helper()
	m.WID = 1
	if m.EntryID == 0 {
		m.EntryID = 1
	}
	m.PostedAt = time.Now().Add(-time.Duration(ageMinutes) * time.Minute)
	id, err := s.CreateMessage(context.Background(), m)
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	return id
}

func TestListMessagesForAdmin_DefaultsToPostedDesc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedAdminListMessage(t, s, domain.Message{AuthorName: "a", Body: "ba", Status: domain.MessageApproved}, 30)
	b := seedAdminListMessage(t, s, domain.Message{AuthorName: "b", Body: "bb", Status: domain.MessageApproved}, 20)
	c := seedAdminListMessage(t, s, domain.Message{AuthorName: "c", Body: "bc", Status: domain.MessageApproved}, 10)

	got, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin: %v", err)
	}
	if len(got) != 3 || got[0].ID != c || got[1].ID != b || got[2].ID != a {
		t.Errorf("default order should be posted DESC; got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestListMessagesForAdmin_StatusFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	w := seedAdminListMessage(t, s, domain.Message{AuthorName: "w", Body: "waiting", Status: domain.MessageWaiting}, 30)
	_ = seedAdminListMessage(t, s, domain.Message{AuthorName: "a", Body: "approved", Status: domain.MessageApproved}, 20)
	_ = seedAdminListMessage(t, s, domain.Message{AuthorName: "h", Body: "hidden", Status: domain.MessageHidden}, 10)

	waiting := domain.MessageWaiting
	got, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{Filter: &waiting})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin: %v", err)
	}
	if len(got) != 1 || got[0].ID != w {
		t.Errorf("waiting filter: got %v, want only id %d", got, w)
	}

	all, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("nil filter: got %d rows, want 3", len(all))
	}
}

func TestListMessagesForAdmin_SearchHitsAuthorBodyEmailIP(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	authorHit := seedAdminListMessage(t, s, domain.Message{AuthorName: "needle author", Body: "x", Status: domain.MessageApproved}, 40)
	bodyHit := seedAdminListMessage(t, s, domain.Message{AuthorName: "x", Body: "needle in body", Status: domain.MessageApproved}, 30)
	emailHit := seedAdminListMessage(t, s, domain.Message{AuthorName: "x", Body: "x", AuthorEmail: "needle@example.com", Status: domain.MessageApproved}, 20)
	ipHit := seedAdminListMessage(t, s, domain.Message{AuthorName: "x", Body: "x", IPAddress: "10.0.needle.1", Status: domain.MessageApproved}, 10)
	_ = seedAdminListMessage(t, s, domain.Message{AuthorName: "miss", Body: "miss", Status: domain.MessageApproved}, 5)

	got, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin: %v", err)
	}
	want := map[int64]bool{authorHit: true, bodyHit: true, emailHit: true, ipHit: true}
	if len(got) != 4 {
		t.Fatalf("search needle: got %d rows, want 4", len(got))
	}
	for _, m := range got {
		if !want[m.ID] {
			t.Errorf("unexpected hit id=%d body=%q", m.ID, m.Body)
		}
	}
}

func TestListMessagesForAdmin_SortByAuthorAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	c := seedAdminListMessage(t, s, domain.Message{AuthorName: "carol", Body: "c", Status: domain.MessageApproved}, 30)
	a := seedAdminListMessage(t, s, domain.Message{AuthorName: "alice", Body: "a", Status: domain.MessageApproved}, 20)
	b := seedAdminListMessage(t, s, domain.Message{AuthorName: "bob", Body: "b", Status: domain.MessageApproved}, 10)

	got, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{SortBy: MessageSortAuthor, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin: %v", err)
	}
	if got[0].ID != a || got[1].ID != b || got[2].ID != c {
		t.Errorf("author ASC: got %d,%d,%d; want %d,%d,%d", got[0].ID, got[1].ID, got[2].ID, a, b, c)
	}
}

func TestListMessagesForAdmin_LimitOffset(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		seedAdminListMessage(t, s, domain.Message{AuthorName: "x", Body: "x", Status: domain.MessageApproved}, 50-i)
	}
	got, err := s.ListMessagesForAdmin(ctx, 1, ListMessagesQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListMessagesForAdmin: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len: got %d, want 2", len(got))
	}
}

func TestCountMessagesForAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedAdminListMessage(t, s, domain.Message{AuthorName: "alpha", Body: "x", Status: domain.MessageApproved}, 30)
	seedAdminListMessage(t, s, domain.Message{AuthorName: "needle", Body: "x", Status: domain.MessageWaiting}, 20)
	seedAdminListMessage(t, s, domain.Message{AuthorName: "needle two", Body: "x", Status: domain.MessageApproved}, 10)

	all, err := s.CountMessagesForAdmin(ctx, 1, ListMessagesQuery{})
	if err != nil {
		t.Fatalf("CountMessagesForAdmin: %v", err)
	}
	if all != 3 {
		t.Errorf("unfiltered count: got %d, want 3", all)
	}
	hits, err := s.CountMessagesForAdmin(ctx, 1, ListMessagesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("CountMessagesForAdmin needle: %v", err)
	}
	if hits != 2 {
		t.Errorf("needle count: got %d, want 2", hits)
	}
	waiting := domain.MessageWaiting
	filtered, err := s.CountMessagesForAdmin(ctx, 1, ListMessagesQuery{Filter: &waiting})
	if err != nil {
		t.Fatalf("CountMessagesForAdmin waiting: %v", err)
	}
	if filtered != 1 {
		t.Errorf("waiting count: got %d, want 1", filtered)
	}
}

func TestParseMessageSortKey(t *testing.T) {
	cases := []struct {
		in   string
		want MessageSortKey
	}{
		{"", MessageSortPostedAt},
		{"unknown", MessageSortPostedAt},
		{"posted", MessageSortPostedAt},
		{"id", MessageSortID},
		{"author", MessageSortAuthor},
		{"status", MessageSortStatus},
		{"entry", MessageSortEntry},
		{"body", MessageSortBody},
	}
	for _, tc := range cases {
		if got := ParseMessageSortKey(tc.in); got != tc.want {
			t.Errorf("ParseMessageSortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
