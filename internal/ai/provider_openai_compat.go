package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAICompatProvider speaks the OpenAI /v1/chat/completions contract
// well enough that the same struct serves OpenAI proper, LM Studio, and
// Ollama (compat mode). Streaming is off — calls are sync-only and
// the UI renders a spinner while waiting.
type openAICompatProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *openAICompatProvider) Kind() Kind { return KindOpenAICompat }

// SupportsVision is a capability hint, not a ground-truth check —
// whether the caller's *model* is multimodal is orthogonal to the
// endpoint. Returning true lets the handler dispatch; a non-vision
// model will surface its own error (usually 400) that we translate
// into ErrVisionUnsupported.
func (p *openAICompatProvider) SupportsVision() bool { return true }

// chatMessage mirrors OpenAI's message object. Content is either a
// plain string or an array of content parts (for vision calls) — we
// always emit the array form when Image is set and the string form
// otherwise, so the wire stays as narrow as possible.
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (p *openAICompatProvider) Complete(ctx context.Context, req Request) (Response, error) {
	payload, err := buildOpenAICompatPayload(p.model, req)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := p.newHTTPRequest(ctx, payload)
	if err != nil {
		return Response{}, err
	}
	raw, status, err := doProviderRequest(ctx, p.client, httpReq)
	if err != nil {
		return Response{}, err
	}
	return parseOpenAICompatResponse(raw, status)
}

// buildOpenAICompatPayload assembles the JSON body for a single sync
// chat completion. The vision/text split lives here so Complete only
// has to thread the bytes through HTTP.
func buildOpenAICompatPayload(model string, req Request) ([]byte, error) {
	messages, err := buildOpenAICompatMessages(req)
	if err != nil {
		return nil, err
	}
	body := chatRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}
	return payload, nil
}

func buildOpenAICompatMessages(req Request) ([]chatMessage, error) {
	messages := make([]chatMessage, 0, 2)
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.System})
	}
	if len(req.Image) == 0 {
		messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})
		return messages, nil
	}
	if strings.TrimSpace(req.ImageMIME) == "" {
		return nil, errors.New("ai: ImageMIME required when Image is set")
	}
	// Vision call: encode as a data URL in the image_url field.
	// OpenAI, LM Studio, and Ollama (compat) all accept this shape;
	// it's cheaper to bundle than force the caller to host images
	// for one-shot alt-text generation.
	dataURL := "data:" + req.ImageMIME + ";base64," + base64.StdEncoding.EncodeToString(req.Image)
	parts := []contentPart{
		{Type: "text", Text: req.Prompt},
		{Type: "image_url", ImageURL: &imageURLPart{URL: dataURL}},
	}
	messages = append(messages, chatMessage{Role: "user", Content: parts})
	return messages, nil
}

func (p *openAICompatProvider) newHTTPRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	url := p.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return httpReq, nil
}

func parseOpenAICompatResponse(raw []byte, status int) (Response, error) {
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("ai: decode response (status %d): %w", status, err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Response{}, mapOpenAICompatErrorMessage(parsed.Error.Message)
	}
	if status >= 400 {
		return Response{}, fmt.Errorf("ai: provider returned %d: %s", status, firstLine(string(raw)))
	}
	if len(parsed.Choices) == 0 {
		return Response{}, ErrEmptyResponse
	}
	return finalizeOpenAICompatChoice(parsed)
}

// mapOpenAICompatErrorMessage classifies a provider-supplied error
// message. Vision-mismatch 400s from LM Studio / Ollama come through
// here; surface them as ErrVisionUnsupported so the admin UI can show
// an actionable toast.
func mapOpenAICompatErrorMessage(msg string) error {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "image") || strings.Contains(lower, "vision") || strings.Contains(lower, "multimodal") {
		return fmt.Errorf("%w: %s", ErrVisionUnsupported, msg)
	}
	return fmt.Errorf("ai: %s", msg)
}

func finalizeOpenAICompatChoice(parsed chatResponse) (Response, error) {
	choice := parsed.Choices[0]
	usage := Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}
	clean, info := sanitizeAssistantText(choice.Message.Content)
	if clean != "" {
		return Response{
			Text:         clean,
			Usage:        usage,
			FinishReason: choice.FinishReason,
			Sanitized:    info,
		}, nil
	}
	// Empty content + non-empty reasoning_content + length-cut is
	// the canonical "the reasoning model used the whole token budget
	// on thinking" failure (qwen3-thinking on a small max_tokens,
	// etc.). Surface a distinct error so the toast can advise raising
	// max_tokens rather than blaming the model.
	err := ErrEmptyResponse
	if choice.FinishReason == "length" && strings.TrimSpace(choice.Message.ReasoningContent) != "" {
		err = ErrReasoningExhausted
	}
	return Response{
		Usage:        usage,
		FinishReason: choice.FinishReason,
		Sanitized:    info,
	}, err
}

// doProviderRequest executes the HTTP round-trip shared by all
// providers: client.Do, context-timeout coalescing, top-level status
// triage, and capped body read.
func doProviderRequest(ctx context.Context, client *http.Client, httpReq *http.Request) ([]byte, int, error) {
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ErrTimeout
		}
		return nil, 0, fmt.Errorf("ai: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap on response body
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, resp.StatusCode, ErrUnauthorized
	case http.StatusTooManyRequests:
		return nil, resp.StatusCode, ErrRateLimited
	}
	return raw, resp.StatusCode, nil
}

// firstLine trims a response body to its first line + an ellipsis —
// the admin toast only has room for one short sentence, so we don't
// dump 500 chars of JSON onto the user.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}
