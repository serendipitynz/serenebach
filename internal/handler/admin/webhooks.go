package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/webhook"
)

// maxWebhooks caps the rows per weblog. The dispatcher already filters
// in Go; the cap keeps the list view scannable and is the same
// rationale as custom_tags.
const maxWebhooks = 50

// mountWebhooks registers the /admin/settings/webhooks/* routes. All
// routes sit under requireDesign (role ≥ power_user) so regular-tier
// authors don't get a knob that can leak entry metadata off-platform.
func (h *Handler) mountWebhooks(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/settings/webhooks", h.webhookList)
		gr.Get("/settings/webhooks/new", h.webhookNewForm)
		gr.Post("/settings/webhooks", h.webhookCreate)
		gr.Get("/settings/webhooks/{id}/edit", h.webhookEditForm)
		gr.Post("/settings/webhooks/{id}", h.webhookUpdate)
		gr.Post("/settings/webhooks/{id}/delete", h.webhookDelete)
		gr.Post("/settings/webhooks/{id}/toggle", h.webhookToggle)
		gr.Post("/settings/webhooks/{id}/test", h.webhookTest)
		gr.Get("/settings/webhooks/{id}/deliveries", h.webhookDeliveries)
	})
}

// webhookRow couples a webhook with its latest delivery so the list view
// can show "最終配信" + "最終ステータス" without per-row look-ups in the
// template.
type webhookRow struct {
	Webhook        domain.Webhook
	URLShort       string
	HasSecret      bool
	LastStatusCode int
	LastError      bool
	LastAt         time.Time
}

type webhookListPageData struct {
	pageBase
	Rows  []webhookRow
	Flash string
	Error string
}

func (h *Handler) webhookList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hooks, err := h.Store.ListWebhooks(ctx, h.wid())
	if err != nil {
		log.Printf("admin.webhookList: %v", err)
		http.Error(w, "failed to list webhooks", http.StatusInternalServerError)
		return
	}
	rows := make([]webhookRow, 0, len(hooks))
	for _, hw := range hooks {
		row := webhookRow{
			Webhook:   hw,
			URLShort:  shortenURL(hw.URL, 60),
			HasSecret: hw.Secret != "",
		}
		latest, _ := h.Store.LatestWebhookDelivery(ctx, hw.ID)
		if latest != nil {
			if latest.StatusCode != nil {
				row.LastStatusCode = *latest.StatusCode
			}
			row.LastError = latest.Error != ""
			if latest.DeliveredAt != nil {
				row.LastAt = *latest.DeliveredAt
			} else {
				row.LastAt = latest.CreatedAt
			}
		}
		rows = append(rows, row)
	}

	data := webhookListPageData{
		pageBase: pageBase{
			Title:      tr(r, "webhooks.title"),
			ActiveMenu: "settings",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Rows:  rows,
		Flash: r.URL.Query().Get("ok"),
		Error: r.URL.Query().Get("err"),
	}
	renderMain(w, r, pageWebhooksList, data)
}

type webhookFormPageData struct {
	pageBase
	Action          string
	Webhook         domain.Webhook
	EventOptions    []webhookEventOption
	IsEdit          bool
	Error           string
	HasSecretStored bool // edit mode: true when the row already has a non-empty secret
}

type webhookEventOption struct {
	ID      string
	Checked bool
	LabelK  string // i18n key
}

func (h *Handler) webhookNewForm(w http.ResponseWriter, r *http.Request) {
	h.renderWebhookForm(w, r, domain.Webhook{Active: true}, "", false)
}

func (h *Handler) webhookEditForm(w http.ResponseWriter, r *http.Request) {
	hw, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	h.renderWebhookForm(w, r, *hw, "", true)
}

func (h *Handler) renderWebhookForm(w http.ResponseWriter, r *http.Request, hw domain.Webhook, errMsg string, isEdit bool) {
	action := root(r) + "/admin/settings/webhooks"
	if isEdit {
		action = root(r) + "/admin/settings/webhooks/" + strconv.FormatInt(hw.ID, 10)
	}
	options := make([]webhookEventOption, 0, len(webhook.AllEvents))
	for _, ev := range webhook.AllEvents {
		options = append(options, webhookEventOption{
			ID:      ev,
			Checked: slices.Contains(hw.Events, ev),
			LabelK:  "webhooks.event." + ev,
		})
	}
	data := webhookFormPageData{
		pageBase: pageBase{
			Title:      tr(r, "webhooks.title"),
			ActiveMenu: "settings",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:          action,
		Webhook:         hw,
		EventOptions:    options,
		IsEdit:          isEdit,
		Error:           errMsg,
		HasSecretStored: hw.Secret != "",
	}
	renderMain(w, r, pageWebhookForm, data)
}

func (h *Handler) webhookCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	count, err := countWebhooks(r, h)
	if err != nil {
		log.Printf("admin.webhookCreate: count: %v", err)
		http.Error(w, "failed to count webhooks", http.StatusInternalServerError)
		return
	}
	if count >= maxWebhooks {
		http.Redirect(w, r, root(r)+"/admin/settings/webhooks?err=limit", http.StatusFound)
		return
	}
	hw, errMsg := parseWebhookForm(r, domain.Webhook{WID: h.wid(), Active: true})
	if errMsg != "" {
		h.renderWebhookForm(w, r, hw, errMsg, false)
		return
	}
	if _, err := h.Store.CreateWebhook(r.Context(), hw); err != nil {
		log.Printf("admin.webhookCreate: %v", err)
		h.renderWebhookForm(w, r, hw, tr(r, "webhooks.error.save"), false)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/webhooks?ok=created", http.StatusFound)
}

func (h *Handler) webhookUpdate(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	merged, errMsg := parseWebhookForm(r, *existing)
	if errMsg != "" {
		h.renderWebhookForm(w, r, merged, errMsg, true)
		return
	}
	if err := h.Store.UpdateWebhook(r.Context(), merged); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.webhookUpdate: %v", err)
		h.renderWebhookForm(w, r, merged, tr(r, "webhooks.error.save"), true)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/webhooks?ok=updated", http.StatusFound)
}

func (h *Handler) webhookDelete(w http.ResponseWriter, r *http.Request) {
	hw, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteWebhook(r.Context(), h.wid(), hw.ID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.webhookDelete: %v", err)
		http.Error(w, "failed to delete webhook", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/webhooks?ok=deleted", http.StatusFound)
}

func (h *Handler) webhookToggle(w http.ResponseWriter, r *http.Request) {
	hw, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	if err := h.Store.SetWebhookActive(r.Context(), h.wid(), hw.ID, !hw.Active); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.webhookToggle: %v", err)
		http.Error(w, "failed to toggle webhook", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/webhooks?ok=toggled", http.StatusSeeOther)
}

// webhookTest fires a synthetic ping payload at the subscription so the
// operator can verify connectivity from the admin UI. The event field
// is "ping" so receivers can filter it out of their main pipeline.
func (h *Handler) webhookTest(w http.ResponseWriter, r *http.Request) {
	hw, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	if h.Webhooks == nil {
		http.Error(w, "webhooks disabled", http.StatusServiceUnavailable)
		return
	}
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.webhookTest: weblog: %v", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	payload := webhook.Payload{
		ID:        webhook.NewDeliveryID(),
		Event:     "ping",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Weblog:    webhook.WeblogRef{ID: weblog.ID, Title: weblog.Title, URL: weblog.BaseURL},
		Data:      map[string]any{"message": "serenebach webhook test"},
	}
	// DispatchOne is always synchronous regardless of Service.Sync, so
	// we never need to mutate the shared service's Sync flag (which
	// would race against normal admin/public dispatches sharing the
	// same *Service). The redirect lands on the deliveries page where
	// the row has already been recorded with its final status.
	h.fireTestDelivery(r.Context(), hw, payload)
	http.Redirect(w, r, root(r)+"/admin/settings/webhooks/"+strconv.FormatInt(hw.ID, 10)+"/deliveries?ok=tested", http.StatusSeeOther)
}

type webhookDeliveriesPageData struct {
	pageBase
	Webhook    domain.Webhook
	URLShort   string
	Deliveries []webhookDeliveryRow
	Flash      string
}

type webhookDeliveryRow struct {
	domain.WebhookDelivery
	StatusLabel string
	WhenLabel   string
}

func (h *Handler) webhookDeliveries(w http.ResponseWriter, r *http.Request) {
	hw, ok := h.loadWebhook(w, r)
	if !ok {
		return
	}
	rows, err := h.Store.ListWebhookDeliveries(r.Context(), hw.ID, 50)
	if err != nil {
		log.Printf("admin.webhookDeliveries: %v", err)
		http.Error(w, "failed to list deliveries", http.StatusInternalServerError)
		return
	}
	view := make([]webhookDeliveryRow, 0, len(rows))
	for _, d := range rows {
		view = append(view, webhookDeliveryRow{
			WebhookDelivery: d,
			StatusLabel:     deliveryStatusLabel(d),
			WhenLabel:       deliveryWhenLabel(d, h.tz()),
		})
	}
	data := webhookDeliveriesPageData{
		pageBase: pageBase{
			Title:      tr(r, "webhooks.deliveries.title"),
			ActiveMenu: "settings",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Webhook:    *hw,
		URLShort:   shortenURL(hw.URL, 60),
		Deliveries: view,
		Flash:      r.URL.Query().Get("ok"),
	}
	renderMain(w, r, pageWebhookDeliveries, data)
}

// loadWebhook resolves the {id} URL param, returning the row and true
// on success. Failures (bad id, not found, fetch error) write the
// appropriate HTTP response so the caller only checks the boolean.
func (h *Handler) loadWebhook(w http.ResponseWriter, r *http.Request) (*domain.Webhook, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return nil, false
	}
	hw, err := h.Store.WebhookByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return nil, false
		}
		log.Printf("admin.loadWebhook: %v", err)
		http.Error(w, "failed to load webhook", http.StatusInternalServerError)
		return nil, false
	}
	return hw, true
}

// fireTestDelivery is the small wrapper webhookTest uses to push a
// synthetic payload through the service's dispatch pipeline without
// going through the active-events filter.
func (h *Handler) fireTestDelivery(ctx context.Context, hw *domain.Webhook, payload webhook.Payload) {
	if err := h.Webhooks.DispatchOne(ctx, *hw, payload.Event, payload); err != nil {
		log.Printf("admin.fireTestDelivery: %v", err)
	}
}

// parseWebhookForm merges the submitted form into an existing webhook
// row (or a zero value for create). Returns (best-effort row, "") on
// success or (best-effort row, error key) on validation failure.
func parseWebhookForm(r *http.Request, base domain.Webhook) (domain.Webhook, string) {
	urlVal := strings.TrimSpace(r.PostFormValue("url"))
	if urlVal == "" {
		return base, "webhooks.error.urlRequired"
	}
	if err := webhook.ValidateURL(urlVal); err != nil {
		return base, "webhooks.error.urlInvalid"
	}
	base.URL = urlVal

	// Secret rules:
	//   create: take whatever is typed (empty = no signing).
	//   edit:   typing a non-empty value replaces it; leaving it empty
	//           keeps the previous one; the explicit "clear" toggle
	//           wipes it.
	if r.PostFormValue("clear_secret") == "1" {
		base.Secret = ""
	} else if v := r.PostFormValue("secret"); strings.TrimSpace(v) != "" {
		base.Secret = strings.TrimSpace(v)
	}

	events := make([]string, 0)
	for _, ev := range webhook.AllEvents {
		if r.PostFormValue("event_"+ev) == "1" {
			events = append(events, ev)
		}
	}
	if len(events) == 0 {
		return base, "webhooks.error.eventsRequired"
	}
	base.Events = events

	base.Active = r.PostFormValue("active") == "1"
	return base, ""
}

// shortenURL truncates a URL to at most `maxLen` characters, suffixing
// "…" when it had to cut.
func shortenURL(raw string, maxLen int) string {
	if maxLen <= 0 || len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "…"
}

// deliveryStatusLabel renders the delivery row's outcome as a stable
// string for the table. "200" / "5xx" / "error" / "pending".
func deliveryStatusLabel(d domain.WebhookDelivery) string {
	if d.StatusCode == nil || *d.StatusCode == 0 {
		if d.Error != "" {
			return "error"
		}
		return "pending"
	}
	return strconv.Itoa(*d.StatusCode)
}

// deliveryWhenLabel formats either DeliveredAt or CreatedAt in the
// admin's TZ; deliveries that never completed fall back to creation
// time so the operator still has something to scan against.
func deliveryWhenLabel(d domain.WebhookDelivery, loc *time.Location) string {
	t := d.CreatedAt
	if d.DeliveredAt != nil {
		t = *d.DeliveredAt
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

// countWebhooks wraps the repo's List call for the admin form's
// "are we over the cap" check. Kept inline because adding a dedicated
// CountWebhooks repo method would clutter the API for a single caller.
func countWebhooks(r *http.Request, h *Handler) (int, error) {
	hooks, err := h.Store.ListWebhooks(r.Context(), h.wid())
	if err != nil {
		return 0, err
	}
	return len(hooks), nil
}
