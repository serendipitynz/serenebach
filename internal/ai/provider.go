// Package ai owns the Provider abstraction that the admin-side writing
// assists (rewrite / continue / title suggest / tag suggest / image
// alt-text) dispatch through. Each concrete provider (Claude native,
// OpenAI chat-completions compatible) is one file in this package.
// Callers only talk to Provider — no provider-specific types leak out.
//
// Two concrete providers ship today:
//   - providerOpenAICompat covers OpenAI, LM Studio, Ollama (OpenAI-
//     compat mode). The `base_url` field decides which one.
//   - providerClaude talks the Anthropic Messages API directly.
//
// Gemini / native Ollama can slot in as a third file later; the
// interface is deliberately small so adding one is a ~100-line drop-in.
package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Kind identifies which concrete provider to spin up. Kept as a
// typed string so the settings form can validate on submit and the
// DB column stays human-readable.
type Kind string

const (
	KindOpenAICompat Kind = "openai-compat" // OpenAI / LM Studio / Ollama
	KindClaude       Kind = "claude"        // Anthropic native Messages API
)

// Valid returns true when k is a known provider kind. Settings-form
// validation and migrations lean on this.
func (k Kind) Valid() bool {
	switch k {
	case KindOpenAICompat, KindClaude:
		return true
	}
	return false
}

// Config is the per-user settings row rendered from the users table
// (decrypted API key included). Fields kept flat so persistence is
// straightforward.
type Config struct {
	Kind    Kind
	BaseURL string // OpenAI: "https://api.openai.com/v1" etc; LM Studio: "http://127.0.0.1:1234/v1"; Ollama: "http://127.0.0.1:11434/v1"
	Model   string // provider-specific model id
	APIKey  string // decrypted; may be empty for local endpoints
}

// Request is the feature-agnostic call shape. A text-only feature
// leaves Image* zero; the alt-text feature sets Image + ImageMIME.
type Request struct {
	System      string // system prompt / instructions
	Prompt      string // user prompt
	Image       []byte // optional; when non-empty the provider MUST send it as a vision input
	ImageMIME   string // "image/jpeg" | "image/png" | "image/gif" | "image/webp" — required iff Image is set
	MaxTokens   int
	Temperature float64
}

// Response carries the provider's text output. Usage is best-effort —
// local backends (LM Studio, Ollama) don't always populate it, and
// the admin UI should gracefully render 0 as "unknown" rather than
// surface a surprise $0.00 cost estimate.
//
// FinishReason and Sanitized are diagnostic fields the audit log
// surfaces on a per-call basis; they have no effect on the returned
// Text. FinishReason is the provider-native string ("stop", "length",
// "end_turn", …) — kept verbatim because each backend differs.
type Response struct {
	Text         string
	Usage        Usage
	FinishReason string
	Sanitized    SanitizeInfo
}

// Usage mirrors the OpenAI-shape usage block. Fields are int because
// the underlying APIs emit integer counts; no floats.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Provider is the single surface the admin handlers call. Concrete
// types live in sibling files; New() is the factory callers use.
type Provider interface {
	// Complete runs one request/response round-trip. Non-nil error
	// indicates the call didn't yield usable text — the caller's
	// toast UI surfaces err.Error() verbatim, so messages must stay
	// short and user-actionable.
	Complete(ctx context.Context, req Request) (Response, error)
	// Kind returns the provider's type so audit / logging code can
	// label calls without caring which concrete struct is behind the
	// interface.
	Kind() Kind
	// SupportsVision reports whether the provider can accept an
	// image in Request. The alt-text feature calls this before
	// dispatch so it can fail with a clear message when the user's
	// configured model isn't multimodal.
	SupportsVision() bool
}

// Shared error sentinels. Handlers translate these into localised
// toast copy so the interface layer never hard-codes Japanese text.
var (
	ErrUnconfigured      = errors.New("ai: no provider configured for this user")
	ErrUnauthorized      = errors.New("ai: provider rejected the API key (401)")
	ErrRateLimited       = errors.New("ai: provider rate-limited the request (429)")
	ErrTimeout           = errors.New("ai: request timed out")
	ErrEmptyResponse     = errors.New("ai: provider returned an empty response")
	ErrVisionUnsupported = errors.New("ai: the configured provider/model doesn't accept images")
	// ErrReasoningExhausted is returned when a reasoning/thinking
	// model used the entire token budget on the hidden chain-of-
	// thought (reasoning_content) without producing a final answer
	// (content). Surfaces in the admin UI as a "raise max_tokens"
	// hint rather than the generic empty-response toast.
	ErrReasoningExhausted = errors.New("ai: reasoning model exhausted token budget before final answer")
)

// DefaultTimeout is the ceiling for a single provider round-trip.
// Local endpoints (LM Studio on CPU) can be slow enough that the
// OpenAI default of 10 s is too aggressive; 60 s clears most
// reasonable local-model configurations without stalling the admin
// UI indefinitely.
const DefaultTimeout = 60 * time.Second

// New returns a Provider for the given Config, ready to Complete.
// The HTTP client is injectable so tests can swap in httptest
// transports; pass nil for the stdlib default with a DefaultTimeout
// ceiling.
func New(cfg Config, client *http.Client) (Provider, error) {
	if !cfg.Kind.Valid() {
		return nil, fmt.Errorf("ai: unknown provider kind %q", cfg.Kind)
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	switch cfg.Kind {
	case KindOpenAICompat:
		if baseURL == "" {
			return nil, errors.New("ai: openai-compat requires base_url")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, errors.New("ai: openai-compat requires model")
		}
		return &openAICompatProvider{
			baseURL: baseURL,
			model:   cfg.Model,
			apiKey:  cfg.APIKey,
			client:  client,
		}, nil
	case KindClaude:
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, errors.New("ai: claude requires model")
		}
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil, errors.New("ai: claude requires api_key")
		}
		return &claudeProvider{
			baseURL: baseURL,
			model:   cfg.Model,
			apiKey:  cfg.APIKey,
			client:  client,
		}, nil
	}
	return nil, fmt.Errorf("ai: unreachable kind %q", cfg.Kind)
}

// DefaultBaseURL maps a provider Kind to the URL admins most often
// want pre-filled on the settings form. Ollama / LM Studio share
// KindOpenAICompat so the form suggests LM Studio's port as a
// pragmatic default; the admin can paste their own on save.
func DefaultBaseURL(kind Kind) string {
	switch kind {
	case KindOpenAICompat:
		return "http://127.0.0.1:1234/v1"
	case KindClaude:
		return "https://api.anthropic.com"
	}
	return ""
}
