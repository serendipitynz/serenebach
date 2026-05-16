package webhook

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// TestDispatchCapturesResponseBodyOnNon2xx ensures the diagnostic
// payload returned by services like Slack ("invalid_payload") makes
// it into the recorded delivery row, not just the bare status code.
func TestDispatchCapturesResponseBodyOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()
	repo := newFakeRepo(domain.Webhook{
		ID: 1, WID: 1, URL: srv.URL, Events: []string{EventEntryPublished}, Active: true,
	})
	svc := New(repo, true, false)
	svc.AllowLoopback = true
	if err := svc.Dispatch(context.Background(), 1, EventEntryPublished, Payload{ID: "d3", Event: EventEntryPublished}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	rec, ok := repo.updated[1]
	if !ok {
		t.Fatalf("no update recorded")
	}
	if rec.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.status)
	}
	if !strings.Contains(rec.errMsg, "400") {
		t.Errorf("error should mention status code: %q", rec.errMsg)
	}
	if !strings.Contains(rec.errMsg, "invalid_payload") {
		t.Errorf("error should include response body excerpt, got %q", rec.errMsg)
	}
}

// TestDispatchNotesEmptyBodyOnNon2xx documents the empty-body branch
// so future readers know we deliberately surface that distinction
// instead of silently dropping the body entirely.
func TestDispatchNotesEmptyBodyOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	repo := newFakeRepo(domain.Webhook{
		ID: 1, WID: 1, URL: srv.URL, Events: []string{EventEntryPublished}, Active: true,
	})
	svc := New(repo, true, false)
	svc.AllowLoopback = true
	_ = svc.Dispatch(context.Background(), 1, EventEntryPublished, Payload{ID: "d4", Event: EventEntryPublished})
	rec := repo.updated[1]
	if !strings.Contains(rec.errMsg, "empty body") {
		t.Errorf("error should note empty body, got %q", rec.errMsg)
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

// TestDialContextRejectsResolvedLoopback covers the SSRF
// "public hostname resolving to private IP" path from PR #88 review.
// ValidateURL only inspects the literal URL string, so the second-
// layer defence lives in the transport's DialContext. We exercise it
// directly with an IP-literal addr — the same code path runs for
// resolved hostnames once net.DefaultResolver returns.
func TestDialContextRejectsBlockedIPs(t *testing.T) {
	svc := New(newFakeRepo(), true, false)
	dial := svc.makeDialContext(&net.Dialer{Timeout: 1 * time.Second})
	for _, addr := range []string{
		"127.0.0.1:9", // loopback
		"10.0.0.5:9",  // RFC1918
		"169.254.0.1:9",
		"192.168.1.1:9",
		"[::1]:9",
	} {
		_, err := dial(context.Background(), "tcp", addr)
		if err == nil {
			t.Errorf("dial(%q) returned no error, want blocked", addr)
			continue
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("dial(%q) error = %v, want \"blocked\" substring", addr, err)
		}
	}
}

// TestDialIPsFallsBackOnConnectionFailure regression-tests PR #88
// follow-up review (dual-stack fallback): a hostname whose AAAA and A
// records both pass the SSRF block check should still deliver when
// the first resolved address is unreachable. We simulate this by
// pointing dialIPs at [::1, 127.0.0.1] for an httptest listener
// (which httptest.NewServer binds to 127.0.0.1 only — nothing
// answers on the same port via ::1), and assert the dial succeeds
// via the IPv4 fallback rather than failing fast on the IPv6 attempt.
func TestDialIPsFallsBackOnConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split URL: %v", err)
	}
	if host != "127.0.0.1" {
		t.Skipf("httptest bound to %q, dual-stack fallback test assumes 127.0.0.1", host)
	}

	svc := New(newFakeRepo(), true, false)
	svc.AllowLoopback = true // both ::1 and 127.0.0.1 pass the block check
	dialer := &net.Dialer{Timeout: 2 * time.Second}

	ips := []net.IPAddr{
		{IP: net.ParseIP("::1")},       // no listener on this port — should fail
		{IP: net.ParseIP("127.0.0.1")}, // httptest listener — should succeed
	}
	conn, err := svc.dialIPs(context.Background(), dialer, "tcp", ips, port)
	if err != nil {
		t.Fatalf("dialIPs should fall back to IPv4, got error: %v", err)
	}
	_ = conn.Close()
}

// TestDialIPsReturnsLastErrorWhenAllFail keeps the failure mode
// well-defined: if every IP in the list errors, dialIPs surfaces the
// last underlying error (callers see a real dial error rather than a
// generic "no dialable addresses" if one was available).
func TestDialIPsReturnsLastErrorWhenAllFail(t *testing.T) {
	svc := New(newFakeRepo(), true, false)
	svc.AllowLoopback = true
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
	// Reserve an OS-allocated port that is closed by the time we dial.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	_ = l.Close() // ensure subsequent dials are refused
	port := strconv.Itoa(addr.Port)

	ips := []net.IPAddr{
		{IP: net.ParseIP("127.0.0.1")},
		{IP: net.ParseIP("127.0.0.1")},
	}
	conn, err := svc.dialIPs(context.Background(), dialer, "tcp", ips, port)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected dial failure, got connection")
	}
}

// TestDialContextAllowLoopbackBypass confirms the test-only flag does
// what it says: an explicitly loopback addr dials through (and the
// connection refusal from a dead port surfaces, not a guard error).
func TestDialContextAllowLoopbackBypass(t *testing.T) {
	svc := New(newFakeRepo(), true, false)
	svc.AllowLoopback = true
	dial := svc.makeDialContext(&net.Dialer{Timeout: 1 * time.Second})
	_, err := dial(context.Background(), "tcp", "127.0.0.1:1") // port 1 is closed
	if err == nil {
		t.Fatalf("expected dial failure on closed port, got nil")
	}
	if strings.Contains(err.Error(), "blocked") {
		t.Errorf("AllowLoopback should bypass guard, but got: %v", err)
	}
}

// TestTransportIgnoresProxyEnv documents that the webhook client must
// not honour HTTP_PROXY / HTTPS_PROXY. If it did, the proxy would
// resolve the destination on our behalf and the DialContext SSRF
// guard would only see the proxy address — letting a public-looking
// hostname that secretly resolves to an internal address slip through
// via the proxy's resolver. See PR #88 follow-up review.
func TestTransportIgnoresProxyEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://203.0.113.1:8080")
	t.Setenv("HTTPS_PROXY", "http://203.0.113.1:8080")
	svc := New(newFakeRepo(), true, false)
	client := svc.httpClient()
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport = %T, want *http.Transport", client.Transport)
	}
	if tr.Proxy != nil {
		// Anything non-nil here means we are honouring some proxy
		// source, which bypasses our DialContext IP guard.
		t.Errorf("transport.Proxy is set; expected nil to keep DialContext as the SSRF authority")
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
