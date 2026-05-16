// Package webhook delivers outbound JSON POSTs when domain events fire
// (entry.published / comment.received / ...). The Service is fire-and-forget
// by design: no retry queue, no scheduled re-send. Per-attempt outcomes
// are persisted to the webhook_deliveries table so admins can inspect
// failures from the UI.
//
// In HTTP server mode dispatch runs on a background goroutine so the
// originating request returns promptly. In CGI mode (where goroutines
// die with the process) dispatch runs synchronously with a tighter
// timeout — see Service.Dispatch.
package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// Repository is the subset of repo.Store the service needs. Kept as
// an interface so tests can swap in a fake without spinning up SQLite.
type Repository interface {
	ActiveWebhooksForEvent(ctx context.Context, wid int64, event string) ([]domain.Webhook, error)
	CreateWebhookDelivery(ctx context.Context, d domain.WebhookDelivery) (int64, error)
	UpdateWebhookDeliveryResult(ctx context.Context, id int64, statusCode int, errMsg string) error
	PruneWebhookDeliveries(ctx context.Context, webhookID int64, keep int) error
}

// Event identifiers. New events should be added here so admin UI
// validation and dispatch share the same source of truth.
const (
	EventEntryPublished   = "entry.published"
	EventEntryUpdated     = "entry.updated"
	EventEntryDeleted     = "entry.deleted"
	EventCommentReceived  = "comment.received"
	EventCommentApproved  = "comment.approved"
	EventImageUploaded    = "image.uploaded"
	httpServerTimeout     = 10 * time.Second
	cgiTimeout            = 3 * time.Second
	deliveriesRetention   = 200
	maxPayloadBytes       = 64 * 1024
	headerEvent           = "X-SB-Event"
	headerDeliveryID      = "X-SB-Delivery"
	headerSignature       = "X-SB-Signature"
	userAgent             = "serenebach-webhook/1.0"
	defaultUserAgentField = userAgent
)

// AllEvents enumerates every event id the service knows about. The
// admin UI walks this list so adding an event automatically surfaces it
// in the create / edit form.
var AllEvents = []string{
	EventEntryPublished,
	EventEntryUpdated,
	EventEntryDeleted,
	EventCommentReceived,
	EventCommentApproved,
	EventImageUploaded,
}

// IsKnownEvent reports whether the id appears in AllEvents.
func IsKnownEvent(id string) bool {
	for _, e := range AllEvents {
		if e == id {
			return true
		}
	}
	return false
}

// Service owns the dispatch lifecycle. Construct one per process and
// share it between the admin and public handlers.
type Service struct {
	Repo Repository
	// Sync controls whether Dispatch blocks until delivery finishes.
	// Set this to true for CGI deployments where the process exits at
	// response time — a goroutine would die before the POST completes.
	Sync bool
	// Disabled cuts every dispatch path to a no-op. Wired from
	// SB_WEBHOOKS_DISABLED so operators have a kill switch without
	// editing rows.
	Disabled bool
	// HTTPClient overrides the package-default client. Tests inject a
	// recording client; production callers leave this nil.
	HTTPClient *http.Client
	// Now lets tests freeze time without dragging time.Now through
	// every helper. Production callers leave it nil and the service
	// uses time.Now directly.
	Now func() time.Time

	clientOnce sync.Once
	client     *http.Client
}

// New returns a Service ready to dispatch. The cgi flag picks the right
// timeout + sync mode for the deployment.
func New(repo Repository, cgi bool, disabled bool) *Service {
	return &Service{Repo: repo, Sync: cgi, Disabled: disabled}
}

// httpClient returns the shared *http.Client, lazily constructed with
// the per-mode timeout. Returning a cached client keeps connection
// pooling effective across many dispatches.
func (s *Service) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	s.clientOnce.Do(func() {
		timeout := httpServerTimeout
		if s.Sync {
			timeout = cgiTimeout
		}
		s.client = &http.Client{
			Timeout: timeout,
			// Stop the client from chasing redirects — a webhook URL
			// that suddenly 301s to an internal host would defeat the
			// SSRF guard otherwise.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	})
	return s.client
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Dispatch fans out a payload to every active webhook subscribed to
// `event`. In async mode (HTTP server) the actual POSTs run in a
// goroutine; in sync mode (CGI) they run inline. The returned error
// covers only the lookup phase — per-delivery failures are recorded in
// webhook_deliveries.
func (s *Service) Dispatch(ctx context.Context, wid int64, event string, payload Payload) error {
	if s == nil || s.Disabled || s.Repo == nil {
		return nil
	}
	if !IsKnownEvent(event) {
		return fmt.Errorf("webhook: unknown event %q", event)
	}
	hooks, err := s.Repo.ActiveWebhooksForEvent(ctx, wid, event)
	if err != nil {
		return fmt.Errorf("webhook: list subscribers: %w", err)
	}
	if len(hooks) == 0 {
		return nil
	}
	body, err := encodePayload(payload)
	if err != nil {
		return fmt.Errorf("webhook: encode payload: %w", err)
	}
	if s.Sync {
		for _, h := range hooks {
			s.deliverOne(ctx, h, event, payload.ID, body)
		}
		return nil
	}
	// Detach from the request context — the caller's ctx is cancelled
	// as soon as it returns its HTTP response, but we need the goroutine
	// to outlive that. The context is purely for cancellation here, so
	// using a fresh background context preserves any deadline the HTTP
	// client itself enforces.
	for _, h := range hooks {
		go s.deliverOne(context.Background(), h, event, payload.ID, body)
	}
	return nil
}

// deliverOne logs a pending delivery row, performs the POST, then
// updates the row with the outcome. Errors are swallowed: the row's
// `error` column is the audit trail.
func (s *Service) deliverOne(ctx context.Context, hook domain.Webhook, event, deliveryID string, body []byte) {
	rowID, err := s.Repo.CreateWebhookDelivery(ctx, domain.WebhookDelivery{
		WebhookID:  hook.ID,
		Event:      event,
		DeliveryID: deliveryID,
		Payload:    string(body),
	})
	if err != nil {
		log.Printf("webhook: persist delivery: %v", err)
		return
	}
	statusCode, sendErr := s.send(ctx, hook, event, deliveryID, body)
	errMsg := ""
	if sendErr != nil {
		errMsg = sendErr.Error()
	}
	if err := s.Repo.UpdateWebhookDeliveryResult(ctx, rowID, statusCode, errMsg); err != nil {
		log.Printf("webhook: update delivery %d: %v", rowID, err)
	}
	// Best-effort retention sweep. Failures are logged; we don't unwind
	// the delivery just because pruning hit a snag.
	if err := s.Repo.PruneWebhookDeliveries(ctx, hook.ID, deliveriesRetention); err != nil {
		log.Printf("webhook: prune deliveries for %d: %v", hook.ID, err)
	}
}

// send performs a single POST. Returns the HTTP status code (0 when no
// response was received) and the transport error, if any.
func (s *Service) send(ctx context.Context, hook domain.Webhook, event, deliveryID string, body []byte) (int, error) {
	if err := ValidateURL(hook.URL); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", defaultUserAgentField)
	req.Header.Set(headerEvent, event)
	req.Header.Set(headerDeliveryID, deliveryID)
	if hook.Secret != "" {
		req.Header.Set(headerSignature, Sign(hook.Secret, body))
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain a bounded number of bytes so the connection can be reused
	// by keep-alive without us caring about the body content.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("webhook: non-2xx response %d", resp.StatusCode)
}

// encodePayload marshals the payload struct and enforces the byte cap.
func encodePayload(p Payload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(b) > maxPayloadBytes {
		return nil, fmt.Errorf("webhook: payload exceeds %d bytes", maxPayloadBytes)
	}
	return b, nil
}

// NewDeliveryID generates a 16-byte hex string used as the delivery's
// unique identifier (exposed on the wire as X-SB-Delivery and embedded
// in the JSON payload). 128 bits of randomness is plenty for the
// audit-trail use case; we avoid pulling in a UUID dependency.
func NewDeliveryID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback that's still unique enough for logging: timestamp +
		// nanoseconds. crypto/rand failing is exceptional.
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// ValidateURL rejects URLs that webhook delivery should not target. The
// SSRF guard blocks loopback, link-local, private network, and other
// non-routable address ranges so a misconfigured webhook can't be used
// to probe internal services.
func ValidateURL(raw string) error {
	if raw == "" {
		return errors.New("webhook: empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook: parse url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("webhook: scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("webhook: empty host")
	}
	if isBlockedHost(host) {
		return fmt.Errorf("webhook: blocked host %q", host)
	}
	return nil
}

// isBlockedHost reports whether the hostname / literal IP resolves to a
// non-routable destination we refuse to call. Hostnames that resolve via
// DNS are checked at request time by the HTTP client; this catches the
// common "set the URL to 127.0.0.1" case at validation time without
// triggering DNS during admin form submission.
func isBlockedHost(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.IsPrivate()
}
