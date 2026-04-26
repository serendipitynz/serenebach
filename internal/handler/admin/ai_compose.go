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
	Language string `json:"language,omitempty"` // "ja" | "en" — optional hint; defaults to weblog lang
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

	prompt, system, err := buildComposePrompt(action, req)
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

// buildComposePrompt maps a logical action to the system + user
// prompt pair the provider gets. Centralised so prompt tweaks don't
// require touching the HTTP handler; also makes it trivial to unit
// test the prompt contents.
func buildComposePrompt(action string, req composeRequest) (prompt, system string, err error) {
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang == "" {
		lang = "ja"
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "html"
	}

	switch action {
	case "rewrite":
		if strings.TrimSpace(req.Text) == "" {
			return "", "", fmt.Errorf("selection_required")
		}
		system = "You are a concise writing assistant. Rewrite the passage the user sends so it reads more naturally while preserving meaning, tone, and any " + format + " markup. Return only the rewritten passage — no preamble, no commentary, no quotation marks. Reply in " + langName(lang) + "."
		prompt = req.Text
		return prompt, system, nil

	case "continue":
		ctxText := strings.TrimSpace(req.Context)
		if ctxText == "" {
			ctxText = strings.TrimSpace(req.Text)
		}
		if ctxText == "" {
			return "", "", fmt.Errorf("context_required")
		}
		system = "You are a concise writing assistant. Continue the passage the user sends with one or two additional paragraphs that match the existing voice and " + format + " markup. Return only the new text — do not repeat what was already written. Reply in " + langName(lang) + "."
		prompt = ctxText
		return prompt, system, nil

	case "summarise", "summarize":
		if strings.TrimSpace(req.Text) == "" {
			return "", "", fmt.Errorf("text_required")
		}
		system = "Summarise the passage in one short paragraph (under 120 words) in " + langName(lang) + ". No preamble."
		prompt = req.Text
		return prompt, system, nil

	case "title":
		if strings.TrimSpace(req.Text) == "" {
			return "", "", fmt.Errorf("text_required")
		}
		system = "Suggest a short, engaging title for the entry below. Reply with exactly one title, no quotation marks, no preamble, under 40 characters. Reply in " + langName(lang) + "."
		prompt = req.Text
		return prompt, system, nil

	case "tags":
		if strings.TrimSpace(req.Text) == "" {
			return "", "", fmt.Errorf("text_required")
		}
		system = "Suggest 3-6 short tags (1-3 words each) for the entry below. Reply with a single line of comma-separated tags. No preamble, no numbering, no quotation marks. Tags should be in " + langName(lang) + "."
		prompt = req.Text
		return prompt, system, nil

	case "keywords":
		if strings.TrimSpace(req.Text) == "" {
			return "", "", fmt.Errorf("text_required")
		}
		system = "Suggest SEO keywords for the entry below. Reply with a single line of comma-separated keywords (5-10 total). No preamble. Keywords should be in " + langName(lang) + "."
		prompt = req.Text
		return prompt, system, nil
	}
	return "", "", fmt.Errorf("unknown_action")
}

// composeMaxTokens caps output per action. Title / tag suggestions
// should be a single line; rewrite / continue / summarise can go
// longer but stay bounded so a runaway generation doesn't wedge the
// UI spinner.
//
// Headroom note: caps account for reasoning/thinking models (qwen3,
// llm-jp-thinking, deepseek-r1) that spend most of completion_tokens
// on hidden chain-of-thought before emitting the answer. Non-thinking
// models stop on `stop` well before these limits, so widening the
// ceiling has no cost on them but lets thinking models actually reach
// the final answer.
func composeMaxTokens(action string) int {
	switch action {
	case "title":
		return 200
	case "tags", "keywords":
		return 200
	case "summarise", "summarize":
		return 800
	case "rewrite":
		return 4096
	case "continue":
		return 2048
	}
	return 1024
}

// composeTemperature trades off determinism. Titles / tags benefit
// from a bit of variety; rewrite favours faithful reproduction so
// 0.2 keeps hallucinations rare.
func composeTemperature(action string) float64 {
	switch action {
	case "title", "tags", "keywords":
		return 0.7
	case "summarise", "summarize":
		return 0.3
	}
	return 0.2
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
