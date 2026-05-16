package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// fakeRepo implements the Repository surface in memory. Captures every
// CreateWebhookDelivery + UpdateWebhookDeliveryResult call so the
// tests can assert on the persisted outcome without spinning up SQLite.
type fakeRepo struct {
	mu         sync.Mutex
	hooks      []domain.Webhook
	created    []domain.WebhookDelivery
	updated    map[int64]updateRecord
	pruneCalls atomic.Int32
}

type updateRecord struct {
	status int
	errMsg string
}

func newFakeRepo(hooks ...domain.Webhook) *fakeRepo {
	return &fakeRepo{hooks: hooks, updated: map[int64]updateRecord{}}
}

func (f *fakeRepo) ActiveWebhooksForEvent(_ context.Context, _ int64, event string) ([]domain.Webhook, error) {
	var out []domain.Webhook
	for _, h := range f.hooks {
		if !h.Active {
			continue
		}
		for _, e := range h.Events {
			if e == event {
				out = append(out, h)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) CreateWebhookDelivery(_ context.Context, d domain.WebhookDelivery) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.ID = int64(len(f.created) + 1)
	f.created = append(f.created, d)
	return d.ID, nil
}

func (f *fakeRepo) UpdateWebhookDeliveryResult(_ context.Context, id int64, status int, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated[id] = updateRecord{status: status, errMsg: errMsg}
	return nil
}

func (f *fakeRepo) PruneWebhookDeliveries(_ context.Context, _ int64, _ int) error {
	f.pruneCalls.Add(1)
	return nil
}

func TestDispatchSyncRecordsSuccess(t *testing.T) {
	var (
		gotBody  []byte
		gotEvent string
		gotSig   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotEvent = r.Header.Get(headerEvent)
		gotSig = r.Header.Get(headerSignature)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	repo := newFakeRepo(domain.Webhook{
		ID:     1,
		WID:    1,
		URL:    srv.URL,
		Secret: "shh",
		Events: []string{EventEntryPublished},
		Active: true,
	})
	svc := New(repo, true /*cgi=sync*/, false)
	svc.AllowLoopback = true
	payload := Payload{
		ID:        "delivery-1",
		Event:     EventEntryPublished,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      map[string]any{"hello": "world"},
	}
	if err := svc.Dispatch(context.Background(), 1, EventEntryPublished, payload); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 delivery row, got %d", len(repo.created))
	}
	rec, ok := repo.updated[1]
	if !ok {
		t.Fatalf("no update recorded for delivery 1")
	}
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.status)
	}
	if rec.errMsg != "" {
		t.Errorf("unexpected error message: %q", rec.errMsg)
	}
	if gotEvent != EventEntryPublished {
		t.Errorf("X-SB-Event header = %q, want %q", gotEvent, EventEntryPublished)
	}
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Errorf("X-SB-Signature header = %q, want sha256= prefix", gotSig)
	}
	if !Verify("shh", gotSig, gotBody) {
		t.Errorf("signature does not verify against payload body")
	}
	if repo.pruneCalls.Load() == 0 {
		t.Errorf("expected pruneCalls > 0, got 0")
	}
}

func TestDispatchSkipsWhenNoSubscriber(t *testing.T) {
	repo := newFakeRepo() // no hooks
	svc := New(repo, true, false)
	if err := svc.Dispatch(context.Background(), 1, EventEntryPublished, Payload{ID: "x", Event: EventEntryPublished}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("expected no deliveries created, got %d", len(repo.created))
	}
}

func TestDispatchRejectsUnknownEvent(t *testing.T) {
	svc := New(newFakeRepo(), true, false)
	if err := svc.Dispatch(context.Background(), 1, "nope.event", Payload{ID: "x", Event: "nope.event"}); err == nil {
		t.Errorf("expected error for unknown event, got nil")
	}
}

func TestDispatchRecordsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	repo := newFakeRepo(domain.Webhook{
		ID: 1, WID: 1, URL: srv.URL, Events: []string{EventEntryPublished}, Active: true,
	})
	svc := New(repo, true, false)
	svc.AllowLoopback = true
	if err := svc.Dispatch(context.Background(), 1, EventEntryPublished, Payload{ID: "d2", Event: EventEntryPublished}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	rec, ok := repo.updated[1]
	if !ok {
		t.Fatalf("no update recorded")
	}
	if rec.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.status)
	}
	if rec.errMsg == "" {
		t.Errorf("expected error message for non-2xx, got empty")
	}
}

func TestDispatchDisabledIsNoop(t *testing.T) {
	repo := newFakeRepo(domain.Webhook{ID: 1, WID: 1, URL: "https://example.com", Events: []string{EventEntryPublished}, Active: true})
	svc := New(repo, true, true /*disabled*/)
	if err := svc.Dispatch(context.Background(), 1, EventEntryPublished, Payload{ID: "x", Event: EventEntryPublished}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("disabled service must not create delivery rows, got %d", len(repo.created))
	}
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://hooks.example.com/sb", false},
		{"http://example.org/path", false},
		{"", true},
		{"ftp://example.com/", true},
		{"https://127.0.0.1/", true},
		{"https://10.0.0.1/", true},
		{"https://192.168.1.5/", true},
		{"http://localhost:8080/", true},
		{"http://[::1]/", true},
		{"http://[fe80::1]/", true},
		{"https:///nopath", true},
	}
	for _, tc := range cases {
		err := ValidateURL(tc.raw)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateURL(%q) = nil, want error", tc.raw)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", tc.raw, err)
		}
	}
}
