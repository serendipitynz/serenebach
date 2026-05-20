package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedListWebhook(t *testing.T, h *Handler, url string) int64 {
	t.Helper()
	id, err := h.Store.CreateWebhook(context.Background(), domain.Webhook{
		WID:    1,
		URL:    url,
		Active: true,
		Events: []string{"comment.approved"},
	})
	if err != nil {
		t.Fatalf("CreateWebhook(%q): %v", url, err)
	}
	return id
}

func TestWebhookList_DefaultRenders(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListWebhook(t, h, "https://alpha.example")
	seedListWebhook(t, h, "https://beta.example")

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/settings/webhooks", nil))
	rec := httptest.NewRecorder()
	h.webhookList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "alpha.example") || !strings.Contains(body, "beta.example") {
		t.Error("body should list seeded webhook URLs")
	}
}

func TestWebhookList_SortByURLToggles(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListWebhook(t, h, "https://alpha.example")

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/settings/webhooks?sort=url&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.webhookList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `sort-link active asc`) {
		t.Error(`url column should render with "active asc" class`)
	}
	if !strings.Contains(body, `sort=url&amp;dir=desc`) && !strings.Contains(body, `sort=url&dir=desc`) {
		t.Error("active url column should toggle to desc on next click")
	}
}
