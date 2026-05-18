package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/ai"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/session"
)

// aiComposeTimeout is the default budget for a text-only completion.
// Sized for reasoning/thinking local models (qwen3, llm-jp-thinking)
// that may emit several thousand reasoning tokens before the final
// answer; non-thinking models finish well under this and exit early.
// Per-user override lives on user.AITimeoutSeconds.
const aiComposeTimeout = 120 * time.Second

// resolveAITimeout picks the effective timeout for one AI call.
// User override (1..aiTimeoutMaxSeconds) wins; otherwise the per-
// feature code default fires.
func resolveAITimeout(user domain.User, fallback time.Duration) time.Duration {
	if user.AITimeoutSeconds > 0 {
		return time.Duration(user.AITimeoutSeconds) * time.Second
	}
	return fallback
}

// composeRequest captures the JSON body the JS side POSTs. Every
// field is optional at the JSON level; per-action validation happens
// inside the handler so the client can use one shape for all
// variants.
type composeRequest struct {
	Action   string `json:"action"`             // "rewrite" | "continue" | "summarise" | "title" | "tags" | "keywords"
	Text     string `json:"text,omitempty"`     // selection or the passage to work on
	Context  string `json:"context,omitempty"`  // preceding/surrounding context for "continue"
	Format   string `json:"format,omitempty"`   // "markdown" | "html" (prompt tuning)
	Language string `json:"language,omitempty"` // "ja" | "en" — optional hint; defaults to the admin display language resolved off the request
}

// aiCompose dispatches one AI writing-assist action. Returns JSON:
//
//	{ ok: true, text: "..." }          on success
//	{ ok: false, error: "<code>" }     on handled errors
//
// Error codes match the i18n catalog (unauthorized, rate_limited,
// timeout, vision_unsupported, unconfigured) so the JS side can
// localise without parsing English messages.
func (h *Handler) aiCompose(w http.ResponseWriter, r *http.Request) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req composeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "parse"})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "action_required"})
		return
	}
	user, err := h.Store.UserByID(r.Context(), actor.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "load_user"})
		return
	}
	provider, err := providerForUser(*user)
	if err != nil {
		if errors.Is(err, ai.ErrUnconfigured) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unconfigured"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	prompt, system, err := buildComposePrompt(action, req, resolveComposeLocale(r))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), resolveAITimeout(*user, aiComposeTimeout))
	defer cancel()
	startedAt := time.Now()
	resp, err := provider.Complete(ctx, ai.Request{
		System:      system,
		Prompt:      prompt,
		MaxTokens:   composeMaxTokens(action),
		Temperature: composeTemperature(action),
	})
	latencyMS := time.Since(startedAt).Milliseconds()
	requestBytes := len(system) + len(prompt)
	if err != nil {
		code := classifyAIError(err)
		h.auditAI(r.Context(), *user, "ai."+action, 0, aiCallRecord{
			Status:       "err",
			ErrCode:      code,
			Model:        user.AIModel,
			FinishReason: resp.FinishReason,
			Usage:        resp.Usage,
			Sanitized:    resp.Sanitized,
			LatencyMS:    latencyMS,
			RequestBytes: requestBytes,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": code, "detail": err.Error()})
		return
	}

	h.auditAI(r.Context(), *user, "ai."+action, 0, aiCallRecord{
		Status:       "ok",
		Model:        user.AIModel,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
		Sanitized:    resp.Sanitized,
		LatencyMS:    latencyMS,
		RequestBytes: requestBytes,
	})

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

// classifyAIError turns a provider-returned error into a stable
// string code the client can key i18n off. Keeps the toast copy
// localised even though the internal error messages are English.
func classifyAIError(err error) string {
	switch {
	case errors.Is(err, ai.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ai.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, ai.ErrTimeout):
		return "timeout"
	case errors.Is(err, ai.ErrVisionUnsupported):
		return "vision_unsupported"
	case errors.Is(err, ai.ErrEmptyResponse):
		return "empty_response"
	case errors.Is(err, ai.ErrReasoningExhausted):
		return "reasoning_exhausted"
	}
	return "provider_error"
}

// composeAction is a single entry in the dispatch table — one function
// per logical compose verb. Each handler receives the resolved lang +
// format pair and returns the (prompt, system) tuple plus a
// validation error like "text_required" / "selection_required" /
// "context_required" when its input wasn't there.
type composeAction func(req composeRequest, lang, format string) (prompt, system string, err error)

// composeActions dispatches /admin/ai/compose by the form's action
// keyword. Adding a new compose verb is a single map entry; the HTTP
// handler doesn't need to grow.
var composeActions = map[string]composeAction{
	"rewrite":   composeRewriteAction,
	"continue":  composeContinueAction,
	"summarise": composeSummariseAction,
	"summarize": composeSummariseAction,
	"title":     composeTitleAction,
	"tags":      composeTagsAction,
	"keywords":  composeKeywordsAction,
}

// buildComposePrompt maps a logical action to the system + user
// prompt pair the provider gets. Centralised so prompt tweaks don't
// require touching the HTTP handler; also makes it trivial to unit
// test the prompt contents.
//
// fallbackLang is the locale to use when the client did not send an
// explicit language hint — wired to the admin's resolved display
// language so a non-empty value always reaches the provider prompt
// (never silently coerced to ja).
func buildComposePrompt(action string, req composeRequest, fallbackLang string) (prompt, system string, err error) {
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang == "" {
		lang = strings.ToLower(strings.TrimSpace(fallbackLang))
	}
	if lang == "" {
		lang = "ja"
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "html"
	}
	handler, ok := composeActions[action]
	if !ok {
		return "", "", fmt.Errorf("unknown_action")
	}
	return handler(req, lang, format)
}

// resolveComposeLocale mirrors resolveHelpLocale: context first
// (locale middleware path), then a fresh Resolve off the request so
// admin POSTs that bypass the locale middleware still pick up the
// operator's language choice (sb_admin_lang cookie → Accept-Language).
func resolveComposeLocale(r *http.Request) string {
	locale := i18n.LocaleFrom(r.Context())
	if locale == "" {
		locale = getI18nBundle().Resolve(r)
	}
	return locale
}

// resolveComposePrompt looks up the system prompt for action and
// substitutes the per-request {format} / {lang} placeholders. Keeps
// every per-action handler down to "validate input → return prompt"
// so the prompt wording itself lives in ai_compose_prompts.jsonc.
//
// Returns an "unknown_action" error when the catalogue has no entry
// for action. Startup validation makes this unreachable for the
// wired-in verbs, but propagating the error here guarantees an
// empty system prompt can never silently reach the provider if the
// invariant ever drifts at runtime.
func resolveComposePrompt(action, lang, format string) (string, error) {
	system, ok := composePromptSystem(action, format, langName(lang))
	if !ok {
		return "", fmt.Errorf("unknown_action")
	}
	return system, nil
}

func composeRewriteAction(req composeRequest, lang, format string) (string, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", "", fmt.Errorf("selection_required")
	}
	system, err := resolveComposePrompt("rewrite", lang, format)
	if err != nil {
		return "", "", err
	}
	return req.Text, system, nil
}

func composeContinueAction(req composeRequest, lang, format string) (string, string, error) {
	ctxText := strings.TrimSpace(req.Context)
	if ctxText == "" {
		ctxText = strings.TrimSpace(req.Text)
	}
	if ctxText == "" {
		return "", "", fmt.Errorf("context_required")
	}
	system, err := resolveComposePrompt("continue", lang, format)
	if err != nil {
		return "", "", err
	}
	return ctxText, system, nil
}

func composeSummariseAction(req composeRequest, lang, format string) (string, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", "", fmt.Errorf("text_required")
	}
	system, err := resolveComposePrompt("summarise", lang, format)
	if err != nil {
		return "", "", err
	}
	return req.Text, system, nil
}

func composeTitleAction(req composeRequest, lang, format string) (string, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", "", fmt.Errorf("text_required")
	}
	system, err := resolveComposePrompt("title", lang, format)
	if err != nil {
		return "", "", err
	}
	return req.Text, system, nil
}

func composeTagsAction(req composeRequest, lang, format string) (string, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", "", fmt.Errorf("text_required")
	}
	system, err := resolveComposePrompt("tags", lang, format)
	if err != nil {
		return "", "", err
	}
	return req.Text, system, nil
}

func composeKeywordsAction(req composeRequest, lang, format string) (string, string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", "", fmt.Errorf("text_required")
	}
	system, err := resolveComposePrompt("keywords", lang, format)
	if err != nil {
		return "", "", err
	}
	return req.Text, system, nil
}

func langName(lang string) string {
	switch lang {
	case "en":
		return "English"
	case "ja":
		return "Japanese"
	}
	return lang
}
