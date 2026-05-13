package admin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/ai"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
)

// aiTimeoutMaxSeconds caps the per-user AI request timeout override
// at 10 minutes. Anything longer keeps a transport-stuck request
// holding handler state past the point of usefulness; if a local
// model genuinely needs >600 s per call the workflow is broken
// regardless of timeout.
const aiTimeoutMaxSeconds = 600

// settingsAIPageData backs /admin/settings/ai.
//
// Two panels live on this tab:
//   - AI 執筆補助 (all users) — per-user provider config. Hidden
//     entirely when SB_AI_SECRET is unset, per the "env-missing =>
//     no AI UI" rule the user asked for.
//   - MCP サーバ (admin only) — bearer tokens + audit log, moved off
//     the old /admin/settings/ops tab.
//
// The template branches on CanManageUsers + AISecretConfigured to
// decide which panels render; that keeps the data struct single-
// shaped regardless of role.
type settingsAIPageData struct {
	settingsPageBase
	Target             domain.User
	AISecretConfigured bool
	AISecretIsDefault  bool
	AIHasKey           bool
	AIFlash            string
	AIError            string

	// MCP panel (admin-only). Empty slice when the signed-in user
	// isn't an admin; the template gates rendering on .CanManageUsers.
	Tokens      []mcpTokenRow
	Users       []domain.User
	Audit       []mcpAuditRow
	NewRawToken string
	NewTokenID  int64

	// Ops reflects the env-var snapshot for the MCP-audit state
	// chip rendered next to the panel header when the admin cares
	// where the audit rows live.
	Ops OpsInfo
}

func (h *Handler) settingsAIForm(w http.ResponseWriter, r *http.Request) {
	h.renderSettingsAI(w, r, "", 0, "")
}

// renderSettingsAI does the heavy lifting for the AI settings tab.
// newRaw / newID carry the raw-token display state after a
// successful POST /admin/settings/mcp/new — same pattern the old ops
// page used. errMsg is surfaced as the MCP-panel error slot.
func (h *Handler) renderSettingsAI(w http.ResponseWriter, r *http.Request, newRaw string, newID int64, errMsg string) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		http.Redirect(w, r, root(r)+"/admin/login", http.StatusFound)
		return
	}
	// Reload the logged-in user so the AI panel reflects the latest
	// saved row (Edit + reload mirrors /admin/profile's pattern).
	fresh, err := h.Store.UserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("admin.settingsAI: reload user: %v", err)
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	aiFlash, aiError := splitAIFlag(q.Get("ok")), splitAIFlag(q.Get("err"))

	data := settingsAIPageData{
		settingsPageBase:   h.newSettingsBase(r, tr(r, "settings.tab.ai"), "ai"),
		Target:             *fresh,
		AISecretConfigured: ai.SecretConfigured(),
		AISecretIsDefault:  ai.SecretIsExampleDefault(),
		AIHasKey:           fresh.AIAPIKeyEnc != "",
		AIFlash:            aiFlash,
		AIError:            aiError,
		Ops:                h.opsInfo(),
	}

	// Admin-only MCP block: fetch tokens, users, and the audit log.
	if actor.CanManageUsers() {
		if err := h.fillMCPBlock(r.Context(), &data, newRaw, newID, errMsg); err != nil {
			log.Printf("admin.settingsAI: mcp block: %v", err)
			http.Error(w, "failed to load MCP block", http.StatusInternalServerError)
			return
		}
	}

	renderMain(w, r, pageSettingsAI, data)
}

// fillMCPBlock populates the admin-only MCP panel (tokens / users /
// audit log) on settingsAIPageData. The caller has already gated on
// CanManageUsers, so any error from the underlying store calls is a
// hard failure and is returned for the caller to log + 500.
func (h *Handler) fillMCPBlock(ctx context.Context, data *settingsAIPageData, newRaw string, newID int64, errMsg string) error {
	tokens, err := h.Store.ListMCPTokens(ctx, h.wid())
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	users, err := h.Store.ListUsers(ctx, h.wid())
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	byID := make(map[int64]domain.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	rows := make([]mcpTokenRow, 0, len(tokens))
	tokenName := make(map[int64]string, len(tokens))
	for _, t := range tokens {
		rows = append(rows, mcpTokenRow{
			MCPToken:      t,
			CreatedAtFmt:  fmtUnix(t.CreatedAt),
			LastUsedAtFmt: fmtUnixOrDash(t.LastUsedAt),
			RevokedAtFmt:  fmtUnixOrDash(t.RevokedAt),
			AuthorLabel:   userDisplayLabel(byID, t.AuthorID),
		})
		tokenName[t.ID] = t.Name
	}
	data.Tokens = rows
	data.Users = users
	data.Audit = h.buildMCPAuditRows(ctx, byID, tokenName)
	data.NewRawToken = newRaw
	data.NewTokenID = newID
	if errMsg != "" {
		data.AIError = "mcp:" + errMsg // leave a breadcrumb; template picks up raw errMsg
	}
	return nil
}

// buildMCPAuditRows returns the recent-audit table rows. A nil Audit
// store or an error from Recent is non-fatal — the panel renders with
// an empty audit list and the error (if any) is logged so operators
// can see why.
func (h *Handler) buildMCPAuditRows(ctx context.Context, byID map[int64]domain.User, tokenName map[int64]string) []mcpAuditRow {
	out := make([]mcpAuditRow, 0)
	if h.Audit == nil {
		return out
	}
	entries, err := h.Audit.Recent(ctx, h.wid(), 100)
	if err != nil {
		log.Printf("admin.settingsAI: audit recent: %v", err)
		return out
	}
	for _, e := range entries {
		out = append(out, mcpAuditRow{
			ID:           e.ID,
			Tool:         e.Tool,
			TargetID:     e.TargetID,
			TokenLabel:   mcpTokenLabel(e.TokenID, tokenName),
			AuthorLabel:  userDisplayLabel(byID, e.AuthorID),
			Extra:        e.Extra,
			CreatedAtFmt: fmtUnix(e.CreatedAt.Unix()),
		})
	}
	return out
}

// userDisplayLabel resolves a user id to the dashboard's display string:
// the user's DisplayName when set, else the login Name, falling back to
// an em-dash placeholder when the user row is unavailable.
func userDisplayLabel(byID map[int64]domain.User, id int64) string {
	u, ok := byID[id]
	if !ok {
		return "—"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

// mcpTokenLabel maps an audit-log token id to the column display value.
// Zero/negative ids come from the stdio transport (no token row); a
// missing tokenName entry means the row references a deleted token and
// gets rendered as "#<id>" so the audit history stays intelligible.
func mcpTokenLabel(tokenID int64, tokenName map[int64]string) string {
	if tokenID <= 0 {
		return "stdio"
	}
	if n, ok := tokenName[tokenID]; ok {
		return n
	}
	return "#" + strconv.FormatInt(tokenID, 10)
}

// --- AI config save -------------------------------------------------

// settingsAISave persists the signed-in user's AI writing-assist
// config. Formerly /admin/profile/ai — lives under /admin/settings/ai
// now that the AI UI has moved to the settings page. The "leave
// blank to keep" API-key convention mirrors the password field.
func (h *Handler) settingsAISave(w http.ResponseWriter, r *http.Request) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		http.Redirect(w, r, root(r)+"/admin/login", http.StatusFound)
		return
	}
	if !ai.SecretConfigured() {
		aiFlashRedirect(w, r, "err", "ai_unconfigured")
		return
	}
	existing, err := h.Store.UserByID(r.Context(), actor.ID)
	if err != nil {
		log.Printf("admin.settingsAISave: load: %v", err)
		http.Error(w, "failed to load profile", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		aiFlashRedirect(w, r, "err", "ai_parse")
		return
	}

	kindRaw := strings.TrimSpace(r.PostFormValue("ai_kind"))
	if !aiEnabledByForm(r, kindRaw) {
		h.disableUserAIConfig(w, r, actor.ID, *existing)
		return
	}

	kind := ai.Kind(kindRaw)
	if !kind.Valid() {
		aiFlashRedirect(w, r, "err", "ai_invalid_kind")
		return
	}

	updated := *existing
	updated.AIKind = string(kind)
	updated.AIBaseURL = strings.TrimSpace(r.PostFormValue("ai_base_url"))
	updated.AIModel = strings.TrimSpace(r.PostFormValue("ai_model"))
	updated.AIAutoAlt = r.PostFormValue("ai_auto_alt") == "on"
	updated.AITimeoutSeconds = parseAITimeoutSeconds(r.PostFormValue("ai_timeout_seconds"))

	if errKey := h.applyAIAPIKey(r, &updated); errKey != "" {
		aiFlashRedirect(w, r, "err", errKey)
		return
	}
	if errKey := validateAIProviderConfig(kind, updated); errKey != "" {
		aiFlashRedirect(w, r, "err", errKey)
		return
	}

	if err := h.Store.UpdateUserAIConfig(r.Context(), actor.ID, updated); err != nil {
		log.Printf("admin.settingsAISave: %v", err)
		http.Error(w, "failed to save AI config", http.StatusInternalServerError)
		return
	}
	aiFlashRedirect(w, r, "ok", "ai_saved")
}

// aiEnabledByForm reports whether the form submission represents an
// enable request — both the explicit checkbox and a non-empty provider
// kind must be set.
func aiEnabledByForm(r *http.Request, kindRaw string) bool {
	return r.PostFormValue("ai_enabled") == "on" && kindRaw != ""
}

// disableUserAIConfig clears every AI field on the user's row and
// persists the result. The handler writes the flash + redirect (or a
// 500) before returning so the caller just stops.
func (h *Handler) disableUserAIConfig(w http.ResponseWriter, r *http.Request, userID int64, base domain.User) {
	updated := base
	updated.AIKind = ""
	updated.AIBaseURL = ""
	updated.AIModel = ""
	updated.AIAPIKeyEnc = ""
	updated.AIAutoAlt = false
	if err := h.Store.UpdateUserAIConfig(r.Context(), userID, updated); err != nil {
		log.Printf("admin.settingsAISave: disable: %v", err)
		http.Error(w, "failed to save AI config", http.StatusInternalServerError)
		return
	}
	aiFlashRedirect(w, r, "ok", "ai_disabled")
}

// parseAITimeoutSeconds interprets the optional timeout override. 0
// (or empty / malformed / out-of-range input) signals "use code
// defaults"; any other value is clamped to [1, aiTimeoutMaxSeconds].
// The field is a tuning knob, not a primary required input, so bad
// input falls back silently rather than rejecting the form.
func parseAITimeoutSeconds(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 || v > aiTimeoutMaxSeconds {
		return 0
	}
	return v
}

// applyAIAPIKey replaces the encrypted key on updated when the form
// supplied a new one. Blank input leaves the saved value alone (the
// "leave blank to keep" convention mirrors the password field).
// Returns an i18n error key when encryption fails so the caller can
// surface it as a flash.
func (h *Handler) applyAIAPIKey(r *http.Request, updated *domain.User) string {
	newKey := strings.TrimSpace(r.PostFormValue("ai_api_key"))
	if newKey == "" {
		return ""
	}
	enc, err := ai.Encrypt(newKey)
	if err != nil {
		log.Printf("admin.settingsAISave: encrypt: %v", err)
		return "ai_encrypt"
	}
	updated.AIAPIKeyEnc = enc
	return ""
}

// validateAIProviderConfig enforces the kind-specific requirements:
// Claude needs an API key (no anonymous Anthropic), openai_compat
// needs an explicit base URL (it covers self-hosted endpoints), and
// every kind needs a model name. Returns "" when the config is valid.
func validateAIProviderConfig(kind ai.Kind, updated domain.User) string {
	if kind == ai.KindClaude && updated.AIAPIKeyEnc == "" {
		return "ai_key_required"
	}
	if kind == ai.KindOpenAICompat && updated.AIBaseURL == "" {
		return "ai_base_url_required"
	}
	if updated.AIModel == "" {
		return "ai_model_required"
	}
	return ""
}

// settingsAITest sends a one-line prompt to the caller's configured
// provider and returns the response inline as JSON, so the 疎通テ
// スト button can render the outcome without a full page reload.
func (h *Handler) settingsAITest(w http.ResponseWriter, r *http.Request) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := h.Store.UserByID(r.Context(), actor.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "load: " + err.Error()})
		return
	}
	provider, err := providerForUser(*user)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), resolveAITimeout(*user, 30*time.Second))
	defer cancel()
	resp, err := provider.Complete(ctx, ai.Request{
		System:      "You are a one-line smoke test. Reply with exactly OK.",
		Prompt:      "Respond with OK.",
		MaxTokens:   10,
		Temperature: 0,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"text": resp.Text,
		"usage": map[string]int{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
	})
}

// providerForUser decrypts the user's saved API key and builds a
// Provider ready for Complete(). Returns ErrUnconfigured when the
// row has no AIKind set so callers can distinguish "user disabled
// AI" from "AI failed".
func providerForUser(u domain.User) (ai.Provider, error) {
	if u.AIKind == "" {
		return nil, ai.ErrUnconfigured
	}
	if !ai.SecretConfigured() {
		return nil, errors.New("ai: SB_AI_SECRET not set on this server")
	}
	apiKey, err := ai.Decrypt(u.AIAPIKeyEnc)
	if err != nil {
		return nil, err
	}
	return ai.New(ai.Config{
		Kind:    ai.Kind(u.AIKind),
		BaseURL: u.AIBaseURL,
		Model:   u.AIModel,
		APIKey:  apiKey,
	}, nil)
}

// aiFlashRedirect rewrites the Location back to /admin/settings/ai
// with the given `ok=` or `err=` query so the page re-renders with
// the matching toast / alert.
func aiFlashRedirect(w http.ResponseWriter, r *http.Request, key, val string) {
	q := url.Values{}
	q.Set(key, val)
	http.Redirect(w, r, root(r)+"/admin/settings/ai?"+q.Encode(), http.StatusFound)
}

// splitAIFlag strips the "ai_" prefix from query flags that belong to
// the AI panel. Returns "" for empty / non-ai_ values so the
// template can rely on a simple equality check.
func splitAIFlag(v string) string {
	if v == "" || !strings.HasPrefix(v, "ai_") {
		return ""
	}
	return strings.TrimPrefix(v, "ai_")
}
