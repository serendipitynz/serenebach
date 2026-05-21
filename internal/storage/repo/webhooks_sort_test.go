package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedWebhook(t *testing.T, s *Store, url string, active bool) int64 {
	t.Helper()
	id, err := s.CreateWebhook(context.Background(), domain.Webhook{
		WID:    1,
		URL:    url,
		Active: active,
		Events: []string{"comment.approved"},
	})
	if err != nil {
		t.Fatalf("CreateWebhook(%q): %v", url, err)
	}
	return id
}

func recordDelivery(t *testing.T, s *Store, hookID int64, statusCode int) {
	t.Helper()
	delID, err := s.CreateWebhookDelivery(context.Background(), domain.WebhookDelivery{
		WebhookID:  hookID,
		Event:      "comment.approved",
		DeliveryID: "test-id",
		Payload:    "{}",
	})
	if err != nil {
		t.Fatalf("CreateWebhookDelivery: %v", err)
	}
	if err := s.UpdateWebhookDeliveryResult(context.Background(), delID, statusCode, ""); err != nil {
		t.Fatalf("UpdateWebhookDeliveryResult: %v", err)
	}
}

func TestListWebhooks_DefaultIDDesc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedWebhook(t, s, "https://a.example", true)
	b := seedWebhook(t, s, "https://b.example", true)
	c := seedWebhook(t, s, "https://c.example", true)

	got, err := s.ListWebhooks(ctx, 1, ListWebhooksQuery{})
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(got) != 3 || got[0].ID != c || got[1].ID != b || got[2].ID != a {
		t.Errorf("default order: got %v, want %d/%d/%d", []int64{got[0].ID, got[1].ID, got[2].ID}, c, b, a)
	}
}

func TestListWebhooks_SortByURLAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	z := seedWebhook(t, s, "https://zebra.example", true)
	a := seedWebhook(t, s, "https://alpha.example", true)
	m := seedWebhook(t, s, "https://mango.example", true)

	got, err := s.ListWebhooks(ctx, 1, ListWebhooksQuery{SortBy: WebhookSortURL, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if got[0].ID != a || got[1].ID != m || got[2].ID != z {
		t.Errorf("url ASC: got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestListWebhooks_SortByLastAtNullsAlwaysLast(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	never := seedWebhook(t, s, "https://never.example", true)
	delivered := seedWebhook(t, s, "https://delivered.example", true)
	recordDelivery(t, s, delivered, 200)

	// DESC: delivered first, never-delivered at the bottom.
	got, err := s.ListWebhooks(ctx, 1, ListWebhooksQuery{SortBy: WebhookSortLastAt, SortDir: SortDesc})
	if err != nil {
		t.Fatalf("ListWebhooks DESC: %v", err)
	}
	if got[0].ID != delivered || got[len(got)-1].ID != never {
		t.Errorf("lastAt DESC: got %v, want delivered first, never last", []int64{got[0].ID, got[1].ID})
	}

	// ASC: the NULLS-LAST trick keeps never-delivered at the bottom
	// even with ASC, so the user doesn't have to scroll past silent
	// hooks to find ones that actually fired.
	got, err = s.ListWebhooks(ctx, 1, ListWebhooksQuery{SortBy: WebhookSortLastAt, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListWebhooks ASC: %v", err)
	}
	if got[len(got)-1].ID != never {
		t.Errorf("lastAt ASC: never-delivered should still sort last, got %d", got[len(got)-1].ID)
	}
}

func TestListWebhooks_SortByLastStatusNullsLast(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	never := seedWebhook(t, s, "https://never.example", true)
	ok := seedWebhook(t, s, "https://ok.example", true)
	recordDelivery(t, s, ok, 200)

	got, err := s.ListWebhooks(ctx, 1, ListWebhooksQuery{SortBy: WebhookSortLastStatus, SortDir: SortDesc})
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if got[len(got)-1].ID != never {
		t.Errorf("lastStatus DESC: never-delivered should sort last, got %d", got[len(got)-1].ID)
	}
}

func TestParseWebhookSortKey(t *testing.T) {
	cases := []struct {
		in   string
		want WebhookSortKey
	}{
		{"", WebhookSortCreatedAt},
		{"garbage", WebhookSortCreatedAt},
		{"url", WebhookSortURL},
		{"active", WebhookSortActive},
		{"format", WebhookSortFormat},
		{"lastAt", WebhookSortLastAt},
		{"lastStatus", WebhookSortLastStatus},
	}
	for _, tc := range cases {
		if got := ParseWebhookSortKey(tc.in); got != tc.want {
			t.Errorf("ParseWebhookSortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
