package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// webhookColumns is the canonical column list for the webhooks table.
// Order must match the Scan call sites below.
const webhookColumns = `id, wid, url, secret, events, active, payload_format, created_at, updated_at`

// webhookDeliveryColumns is the canonical column list for
// webhook_deliveries. Same contract as the webhooks list.
const webhookDeliveryColumns = `id, webhook_id, event, delivery_id, payload, status_code, error, delivered_at, created_at`

// WebhookSortKey is a typed enum of the columns the admin webhook
// list can sort by. Default is created_at DESC — most-recently-
// registered first, the same shape as the MCP token list.
type WebhookSortKey int

const (
	WebhookSortCreatedAt WebhookSortKey = iota // default
	WebhookSortURL
	WebhookSortActive
	WebhookSortFormat
	WebhookSortLastAt
	WebhookSortLastStatus
)

// String returns the URL-form name of the sort key.
func (k WebhookSortKey) String() string {
	switch k {
	case WebhookSortURL:
		return "url"
	case WebhookSortActive:
		return "active"
	case WebhookSortFormat:
		return "format"
	case WebhookSortLastAt:
		return "lastAt"
	case WebhookSortLastStatus:
		return "lastStatus"
	default:
		return ""
	}
}

// ParseWebhookSortKey maps a ?sort= query value to the enum.
func ParseWebhookSortKey(s string) WebhookSortKey {
	switch s {
	case "url":
		return WebhookSortURL
	case "active":
		return WebhookSortActive
	case "format":
		return WebhookSortFormat
	case "lastAt":
		return WebhookSortLastAt
	case "lastStatus":
		return WebhookSortLastStatus
	default:
		return WebhookSortCreatedAt
	}
}

// ListWebhooksQuery bundles the admin webhook list's sort parameters.
// No search / paging — the list is capped at maxWebhooks (50) in the
// admin form, so paging would be permanently single-page anyway.
type ListWebhooksQuery struct {
	SortBy  WebhookSortKey
	SortDir SortDir
}

// webhookLatestDeliveredAtExpr / webhookLatestStatusCodeExpr are the
// correlated subqueries the ORDER BY clauses use for the "last
// delivery" columns. Inlined because each call site needs the same
// expression twice (once for the NULL-handling CASE, once for the
// actual ordering).
const webhookLatestDeliveredAtExpr = `(SELECT delivered_at FROM webhook_deliveries WHERE webhook_id = webhooks.id ORDER BY id DESC LIMIT 1)`
const webhookLatestStatusCodeExpr = `(SELECT status_code FROM webhook_deliveries WHERE webhook_id = webhooks.id ORDER BY id DESC LIMIT 1)`

// orderClause writes the ORDER BY fragment for `k` directly to b,
// including direction and any NULLS-LAST handling. lastAt /
// lastStatus join through a correlated subquery and need an explicit
// "NULL sorts last" pass because SQLite would otherwise put never-
// delivered webhooks on top in ASC order.
func (k WebhookSortKey) writeOrderBy(b *strings.Builder, d SortDir) {
	switch k {
	case WebhookSortURL:
		b.WriteString(`url `)
		b.WriteString(d.String())
	case WebhookSortActive:
		b.WriteString(`active `)
		b.WriteString(d.String())
	case WebhookSortFormat:
		b.WriteString(`payload_format `)
		b.WriteString(d.String())
	case WebhookSortLastAt:
		fmt.Fprintf(b, `CASE WHEN %s IS NULL THEN 1 ELSE 0 END, %s %s`,
			webhookLatestDeliveredAtExpr, webhookLatestDeliveredAtExpr, d.String())
	case WebhookSortLastStatus:
		fmt.Fprintf(b, `CASE WHEN %s IS NULL THEN 1 ELSE 0 END, %s %s`,
			webhookLatestStatusCodeExpr, webhookLatestStatusCodeExpr, d.String())
	default:
		b.WriteString(`created_at `)
		b.WriteString(d.String())
	}
}

// ListWebhooks returns every webhook subscription for the given weblog
// ordered by q. Zero-value query reproduces the legacy "ORDER BY id
// DESC" behaviour via the WebhookSortCreatedAt default + the id
// tie-breaker (id correlates with created_at for any monotonic-id
// table).
func (s *Store) ListWebhooks(ctx context.Context, wid int64, q ListWebhooksQuery) ([]domain.Webhook, error) {
	var b strings.Builder
	b.WriteString(`SELECT ` + webhookColumns + ` FROM webhooks WHERE wid = ? ORDER BY `)
	q.SortBy.writeOrderBy(&b, q.SortDir)
	// Stable tie-breaker. For the default sort this also degrades the
	// SQL back into "ORDER BY created_at DESC, id DESC" — i.e. the
	// pre-refactor order.
	b.WriteString(`, id DESC`)
	rows, err := s.db.QueryContext(ctx, b.String(), wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListWebhooks: %w", err)
	}
	defer rows.Close()

	var out []domain.Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WebhookByID fetches one webhook row. ErrNotFound on miss.
func (s *Store) WebhookByID(ctx context.Context, wid, id int64) (*domain.Webhook, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+webhookColumns+`
		FROM webhooks
		WHERE wid = ? AND id = ?`, wid, id)
	w, err := scanWebhook(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: WebhookByID: %w", err)
	}
	return &w, nil
}

// ActiveWebhooksForEvent returns every active webhook for the weblog
// whose `events` array contains the given event id. Dispatch consults
// this from the request path; we filter in Go rather than in SQL
// because the events column is a JSON array stored as TEXT and the
// total row count is bounded by admin UI.
func (s *Store) ActiveWebhooksForEvent(ctx context.Context, wid int64, event string) ([]domain.Webhook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+webhookColumns+`
		FROM webhooks
		WHERE wid = ? AND active = 1`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ActiveWebhooksForEvent: %w", err)
	}
	defer rows.Close()

	var out []domain.Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		if containsEvent(w.Events, event) {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

// CreateWebhook inserts a new webhook subscription and returns its id.
func (s *Store) CreateWebhook(ctx context.Context, w domain.Webhook) (int64, error) {
	eventsJSON, err := encodeEvents(w.Events)
	if err != nil {
		return 0, err
	}
	active := 0
	if w.Active {
		active = 1
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO webhooks (wid, url, secret, events, active, payload_format, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.WID, w.URL, w.Secret, eventsJSON, active, normaliseFormat(w.PayloadFormat), now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateWebhook: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateWebhook lastid: %w", err)
	}
	return id, nil
}

// UpdateWebhook replaces every editable field on the row. ErrNotFound
// when no row matches (wid, id).
func (s *Store) UpdateWebhook(ctx context.Context, w domain.Webhook) error {
	eventsJSON, err := encodeEvents(w.Events)
	if err != nil {
		return err
	}
	active := 0
	if w.Active {
		active = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhooks SET
			url = ?, secret = ?, events = ?, active = ?, payload_format = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		w.URL, w.Secret, eventsJSON, active, normaliseFormat(w.PayloadFormat), time.Now().Unix(), w.WID, w.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateWebhook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetWebhookActive flips the active flag without rewriting the rest of
// the row. Used by the toggle button on the list view.
func (s *Store) SetWebhookActive(ctx context.Context, wid, id int64, active bool) error {
	flag := 0
	if active {
		flag = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE webhooks SET active = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		flag, time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: SetWebhookActive: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWebhook removes a subscription and (via FK cascade) every
// delivery row that referenced it.
func (s *Store) DeleteWebhook(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteWebhook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- deliveries --------------------------------------------------------

// CreateWebhookDelivery inserts a pending delivery row and returns its id.
// status_code / delivered_at stay NULL until UpdateWebhookDeliveryResult
// commits the outcome.
func (s *Store) CreateWebhookDelivery(ctx context.Context, d domain.WebhookDelivery) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (webhook_id, event, delivery_id, payload, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		d.WebhookID, d.Event, d.DeliveryID, d.Payload, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("repo: CreateWebhookDelivery: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateWebhookDelivery lastid: %w", err)
	}
	return id, nil
}

// UpdateWebhookDeliveryResult records the outcome of a single attempt.
// statusCode may be 0 when the request never produced a response (DNS,
// connect, timeout) — the error string carries the diagnostic in that
// case.
func (s *Store) UpdateWebhookDeliveryResult(ctx context.Context, id int64, statusCode int, errMsg string) error {
	now := time.Now().Unix()
	var sc any
	if statusCode > 0 {
		sc = statusCode
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE webhook_deliveries SET
			status_code = ?, error = ?, delivered_at = ?
		WHERE id = ?`,
		sc, errMsg, now, id)
	if err != nil {
		return fmt.Errorf("repo: UpdateWebhookDeliveryResult: %w", err)
	}
	return nil
}

// ListWebhookDeliveries returns recent attempts for one subscription,
// newest first. Limit caps the rows fetched; the admin UI uses 50.
func (s *Store) ListWebhookDeliveries(ctx context.Context, webhookID int64, limit int) ([]domain.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+webhookDeliveryColumns+`
		FROM webhook_deliveries
		WHERE webhook_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, webhookID, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: ListWebhookDeliveries: %w", err)
	}
	defer rows.Close()

	var out []domain.WebhookDelivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestWebhookDelivery returns the most recent attempt for a webhook,
// or (nil, nil) when none exist. Used by the list view to show
// "最終ステータス" without joining in SQL.
func (s *Store) LatestWebhookDelivery(ctx context.Context, webhookID int64) (*domain.WebhookDelivery, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+webhookDeliveryColumns+`
		FROM webhook_deliveries
		WHERE webhook_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, webhookID)
	d, err := scanWebhookDelivery(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil //nolint:nilnil // "no rows" is the documented sentinel.
		}
		return nil, fmt.Errorf("repo: LatestWebhookDelivery: %w", err)
	}
	return &d, nil
}

// PruneWebhookDeliveries keeps only the newest `keep` rows for the
// webhook. Older attempts are removed in a single DELETE so the
// admin UI never has to paginate through historical noise.
func (s *Store) PruneWebhookDeliveries(ctx context.Context, webhookID int64, keep int) error {
	if keep <= 0 {
		keep = 200
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM webhook_deliveries
		WHERE webhook_id = ?
		  AND id NOT IN (
			SELECT id FROM webhook_deliveries
			WHERE webhook_id = ?
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		  )`, webhookID, webhookID, keep)
	if err != nil {
		return fmt.Errorf("repo: PruneWebhookDeliveries: %w", err)
	}
	return nil
}

// ---- scanners + helpers ------------------------------------------------

// rowScanner is the minimal Scan surface both *sql.Rows and *sql.Row
// satisfy, so scanWebhook works for single-row and multi-row queries.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanWebhook(sc rowScanner) (domain.Webhook, error) {
	var (
		w                    domain.Webhook
		eventsJSON           string
		activeInt            int64
		payloadFormat        string
		createdAt, updatedAt int64
	)
	if err := sc.Scan(&w.ID, &w.WID, &w.URL, &w.Secret, &eventsJSON, &activeInt, &payloadFormat, &createdAt, &updatedAt); err != nil {
		return domain.Webhook{}, err
	}
	events, err := decodeEvents(eventsJSON)
	if err != nil {
		return domain.Webhook{}, fmt.Errorf("repo: scan webhook events: %w", err)
	}
	w.Events = events
	w.Active = activeInt != 0
	w.PayloadFormat = normaliseFormat(payloadFormat)
	w.CreatedAt = time.Unix(createdAt, 0)
	w.UpdatedAt = time.Unix(updatedAt, 0)
	return w, nil
}

// normaliseFormat collapses unknown / empty payload_format values back
// to the safe default. The DB constraint is loose (just TEXT) so a
// future code path that introduces a third format never crashes older
// readers — they treat it as "envelope" and the operator can re-pick
// from the admin UI.
func normaliseFormat(s string) string {
	switch s {
	case "envelope", "flat":
		return s
	default:
		return "envelope"
	}
}

func scanWebhookDelivery(sc rowScanner) (domain.WebhookDelivery, error) {
	var (
		d           domain.WebhookDelivery
		statusCode  sql.NullInt64
		deliveredAt sql.NullInt64
		createdAt   int64
	)
	if err := sc.Scan(&d.ID, &d.WebhookID, &d.Event, &d.DeliveryID, &d.Payload, &statusCode, &d.Error, &deliveredAt, &createdAt); err != nil {
		return domain.WebhookDelivery{}, err
	}
	if statusCode.Valid {
		sc := int(statusCode.Int64)
		d.StatusCode = &sc
	}
	if deliveredAt.Valid {
		t := time.Unix(deliveredAt.Int64, 0)
		d.DeliveredAt = &t
	}
	d.CreatedAt = time.Unix(createdAt, 0)
	return d, nil
}

// encodeEvents serialises the events slice as a JSON array. An empty
// slice becomes "[]" so the column never holds NULL or a JSON null.
func encodeEvents(events []string) (string, error) {
	if len(events) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(events)
	if err != nil {
		return "", fmt.Errorf("repo: encode events: %w", err)
	}
	return string(b), nil
}

// decodeEvents parses the JSON array back into a slice. An empty or
// malformed value collapses to nil so callers never panic; a bad payload
// in DB is reported via the returned error so the admin can fix it.
func decodeEvents(s string) ([]string, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func containsEvent(events []string, want string) bool {
	for _, e := range events {
		if e == want {
			return true
		}
	}
	return false
}
