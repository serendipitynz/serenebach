package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// claudeProvider talks to Anthropic's Messages API
// (https://docs.anthropic.com/en/api/messages). The wire shape is
// close enough to OpenAI that a lot of code looks similar, but the
// response envelope is different (`content` is a typed-block array
// rather than a string) and image inputs use `source.type=base64`
// instead of data URLs.
type claudeProvider struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *claudeProvider) Kind() Kind           { return KindClaude }
func (p *claudeProvider) SupportsVision() bool { return true }

type claudeContentBlock struct {
	Type   string             `json:"type"`
	Text   string             `json:"text,omitempty"`
	Source *claudeImageSource `json:"source,omitempty"`
}

type claudeImageSource struct {
	Type      string `json:"type"`       // always "base64"
	MediaType string `json:"media_type"` // e.g. "image/png"
	Data      string `json:"data"`
}

type claudeMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature,omitempty"`
	System      string          `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *claudeProvider) Complete(ctx context.Context, req Request) (Response, error) {
	payload, err := buildClaudePayload(p.model, req)
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
	return parseClaudeResponse(raw, status)
}

// buildClaudePayload assembles the JSON body for a single sync
// Messages call.
func buildClaudePayload(model string, req Request) ([]byte, error) {
	blocks, err := buildClaudeContentBlocks(req)
	if err != nil {
		return nil, err
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		// Anthropic rejects requests without max_tokens; pick a
		// conservative default that still leaves room for a short
		// rewrite. Admin UI can override per-feature.
		maxTokens = 1024
	}
	body := claudeRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages:    []claudeMessage{{Role: "user", Content: blocks}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal request: %w", err)
	}
	return payload, nil
}

func buildClaudeContentBlocks(req Request) ([]claudeContentBlock, error) {
	blocks := make([]claudeContentBlock, 0, 2)
	if len(req.Image) > 0 {
		if strings.TrimSpace(req.ImageMIME) == "" {
			return nil, errors.New("ai: ImageMIME required when Image is set")
		}
		blocks = append(blocks, claudeContentBlock{
			Type: "image",
			Source: &claudeImageSource{
				Type:      "base64",
				MediaType: req.ImageMIME,
				Data:      base64.StdEncoding.EncodeToString(req.Image),
			},
		})
	}
	blocks = append(blocks, claudeContentBlock{Type: "text", Text: req.Prompt})
	return blocks, nil
}

func (p *claudeProvider) newHTTPRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	url := strings.TrimRight(p.baseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("ai: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", p.apiKey)
	return httpReq, nil
}

func parseClaudeResponse(raw []byte, status int) (Response, error) {
	var parsed claudeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("ai: decode response (status %d): %w", status, err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Response{}, fmt.Errorf("ai: %s", parsed.Error.Message)
	}
	if status >= 400 {
		return Response{}, fmt.Errorf("ai: provider returned %d: %s", status, firstLine(string(raw)))
	}
	return finalizeClaudeResponse(parsed)
}

func finalizeClaudeResponse(parsed claudeResponse) (Response, error) {
	// Messages API returns a list of content blocks; concatenate the
	// text ones. Non-text blocks (e.g. tool_use) are unexpected here
	// since we never enable tools, so silently dropping them is safe.
	var sb strings.Builder
	for _, b := range parsed.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	usage := Usage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	clean, info := sanitizeAssistantText(sb.String())
	if clean == "" {
		return Response{
			Usage:        usage,
			FinishReason: parsed.StopReason,
			Sanitized:    info,
		}, ErrEmptyResponse
	}
	return Response{
		Text:         clean,
		Usage:        usage,
		FinishReason: parsed.StopReason,
		Sanitized:    info,
	}, nil
}
