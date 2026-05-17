package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestWebhookCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateWebhook(ctx, domain.Webhook{
		WID:    1,
		URL:    "https://hooks.example.com/sb",
		Secret: "shh",
		Events: []string{"entry.published", "comment.received"},
		Active: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if id <= 0 {
		t.Fatalf("CreateWebhook id = %d", id)
	}

	verifyWebhookCRUDInitialRead(t, ctx, s, id)
	verifyWebhookCRUDUpdate(t, ctx, s, id)
	verifyWebhookCRUDSetActive(t, ctx, s, id)
	verifyWebhookCRUDDelete(t, ctx, s, id)
}

func verifyWebhookCRUDInitialRead(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	hw, err := s.WebhookByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("WebhookByID: %v", err)
	}
	if hw.URL != "https://hooks.example.com/sb" {
		t.Errorf("URL = %q", hw.URL)
	}
	if !hw.Active {
		t.Errorf("Active = false, want true")
	}
	if len(hw.Events) != 2 || hw.Events[0] != "entry.published" {
		t.Errorf("Events = %v", hw.Events)
	}
}

func verifyWebhookCRUDUpdate(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.UpdateWebhook(ctx, domain.Webhook{
		ID:     id,
		WID:    1,
		URL:    "https://hooks.example.com/v2",
		Secret: "newsecret",
		Events: []string{"entry.published"},
		Active: false,
	}); err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	hw, _ := s.WebhookByID(ctx, 1, id)
	if hw.URL != "https://hooks.example.com/v2" || hw.Active {
		t.Errorf("update did not apply: %+v", hw)
	}
	if len(hw.Events) != 1 || hw.Events[0] != "entry.published" {
		t.Errorf("Events after update = %v", hw.Events)
	}
}

func verifyWebhookCRUDSetActive(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.SetWebhookActive(ctx, 1, id, true); err != nil {
		t.Fatalf("SetWebhookActive: %v", err)
	}
	hw, _ := s.WebhookByID(ctx, 1, id)
	if !hw.Active {
		t.Errorf("Active flag did not flip back")
	}
}

func verifyWebhookCRUDDelete(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.DeleteWebhook(ctx, 1, id); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if _, err := s.WebhookByID(ctx, 1, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("WebhookByID after delete = %v, want ErrNotFound", err)
	}
}

func TestActiveWebhooksForEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	mustCreate := func(events []string, active bool) {
		t.Helper()
		_, err := s.CreateWebhook(ctx, domain.Webhook{
			WID: 1, URL: "https://h.example.com/a", Events: events, Active: active,
		})
		if err != nil {
			t.Fatalf("CreateWebhook: %v", err)
		}
	}
	mustCreate([]string{"entry.published"}, true)
	mustCreate([]string{"comment.received"}, true)
	mustCreate([]string{"entry.published"}, false) // inactive — must not show
	mustCreate([]string{"entry.published", "entry.updated"}, true)

	got, err := s.ActiveWebhooksForEvent(ctx, 1, "entry.published")
	if err != nil {
		t.Fatalf("ActiveWebhooksForEvent: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("active subscribers for entry.published = %d, want 2", len(got))
	}
	got, _ = s.ActiveWebhooksForEvent(ctx, 1, "comment.received")
	if len(got) != 1 {
		t.Errorf("active subscribers for comment.received = %d, want 1", len(got))
	}
}

func TestWebhookDeliveriesLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateWebhook(ctx, domain.Webhook{
		WID: 1, URL: "https://h.example.com/", Events: []string{"entry.published"}, Active: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	rowID, err := s.CreateWebhookDelivery(ctx, domain.WebhookDelivery{
		WebhookID:  id,
		Event:      "entry.published",
		DeliveryID: "abc123",
		Payload:    `{"id":"abc123"}`,
	})
	if err != nil {
		t.Fatalf("CreateWebhookDelivery: %v", err)
	}

	if err := s.UpdateWebhookDeliveryResult(ctx, rowID, 200, ""); err != nil {
		t.Fatalf("UpdateWebhookDeliveryResult: %v", err)
	}

	deliveries, err := s.ListWebhookDeliveries(ctx, id, 10)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("len(deliveries) = %d, want 1", len(deliveries))
	}
	if deliveries[0].StatusCode == nil || *deliveries[0].StatusCode != 200 {
		t.Errorf("StatusCode = %v, want 200", deliveries[0].StatusCode)
	}

	latest, err := s.LatestWebhookDelivery(ctx, id)
	if err != nil {
		t.Fatalf("LatestWebhookDelivery: %v", err)
	}
	if latest == nil || latest.ID != rowID {
		t.Errorf("latest = %+v, want id=%d", latest, rowID)
	}

	// Pruning to keep=1 with one row in place should be a no-op.
	if err := s.PruneWebhookDeliveries(ctx, id, 1); err != nil {
		t.Fatalf("PruneWebhookDeliveries: %v", err)
	}
	// Deletes succeed; FK cascade on webhook_deliveries is enabled in
	// production (sqlite.Open enables foreign_keys), but the bare
	// sql.Open used by newTestStore does not, so we only verify that
	// the webhook row is gone without asserting cascade behaviour here.
	if err := s.DeleteWebhook(ctx, 1, id); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if _, err := s.WebhookByID(ctx, 1, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("WebhookByID after delete = %v, want ErrNotFound", err)
	}
}

func TestPruneWebhookDeliveriesKeepsNewest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateWebhook(ctx, domain.Webhook{
		WID: 1, URL: "https://h.example.com/", Events: []string{"entry.published"}, Active: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	// Insert five attempts.
	for i := 0; i < 5; i++ {
		_, err := s.CreateWebhookDelivery(ctx, domain.WebhookDelivery{
			WebhookID:  id,
			Event:      "entry.published",
			DeliveryID: "d",
			Payload:    `{}`,
		})
		if err != nil {
			t.Fatalf("CreateWebhookDelivery: %v", err)
		}
	}
	if err := s.PruneWebhookDeliveries(ctx, id, 2); err != nil {
		t.Fatalf("PruneWebhookDeliveries: %v", err)
	}
	deliveries, err := s.ListWebhookDeliveries(ctx, id, 100)
	if err != nil {
		t.Fatalf("ListWebhookDeliveries: %v", err)
	}
	if len(deliveries) != 2 {
		t.Errorf("len(deliveries) = %d, want 2", len(deliveries))
	}
}
