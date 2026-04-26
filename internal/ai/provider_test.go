package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc lets a test stub the HTTP client without standing up
// a real server. We just inspect the request and return a canned
// response.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func stubResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestOpenAICompatHappyPath(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		capturedBody, _ = io.ReadAll(req.Body)
		return stubResponse(200, `{
			"choices":[{"message":{"role":"assistant","content":"Hello there"}}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
		}`), nil
	})}

	p, err := New(Config{
		Kind:    KindOpenAICompat,
		BaseURL: "http://127.0.0.1:1234/v1",
		Model:   "test-model",
		APIKey:  "secret",
	}, client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.Complete(context.Background(), Request{
		System:    "system",
		Prompt:    "prompt",
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "Hello there" {
		t.Errorf("Text = %q, want %q", resp.Text, "Hello there")
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("TotalTokens = %d, want 8", resp.Usage.TotalTokens)
	}
	if captured.URL.String() != "http://127.0.0.1:1234/v1/chat/completions" {
		t.Errorf("URL = %q", captured.URL)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization header = %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if body["model"] != "test-model" {
		t.Errorf("model field = %v", body["model"])
	}
	if body["stream"] != false {
		t.Errorf("stream = %v, want false", body["stream"])
	}
}

func TestOpenAICompatVisionSendsImageURL(t *testing.T) {
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedBody, _ = io.ReadAll(req.Body)
		return stubResponse(200, `{"choices":[{"message":{"content":"a cat"}}]}`), nil
	})}

	p, _ := New(Config{Kind: KindOpenAICompat, BaseURL: "http://x/v1", Model: "m", APIKey: "k"}, client)
	_, err := p.Complete(context.Background(), Request{
		Prompt:    "describe",
		Image:     []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic, just needs to be non-empty
		ImageMIME: "image/png",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Messages[0].content should be an array with both a text part and
	// an image_url part; both OpenAI and LM Studio accept this shape.
	if !bytes.Contains(capturedBody, []byte(`"type":"image_url"`)) {
		t.Errorf("vision call missing image_url part; body=%s", capturedBody)
	}
	if !bytes.Contains(capturedBody, []byte(`"data:image/png;base64,`)) {
		t.Errorf("vision call missing data: URL; body=%s", capturedBody)
	}
}

func TestOpenAICompatErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"401 maps to ErrUnauthorized", 401, `{"error":{"message":"bad key"}}`, ErrUnauthorized},
		{"429 maps to ErrRateLimited", 429, `{"error":{"message":"slow down"}}`, ErrRateLimited},
		{"empty content → ErrEmptyResponse", 200, `{"choices":[{"message":{"content":""}}]}`, ErrEmptyResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return stubResponse(tc.status, tc.body), nil
			})}
			p, _ := New(Config{Kind: KindOpenAICompat, BaseURL: "http://x/v1", Model: "m"}, client)
			_, err := p.Complete(context.Background(), Request{Prompt: "x"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestOpenAICompatVisionUnsupportedHint(t *testing.T) {
	// LM Studio returns 400 with "this model does not support images"
	// when the loaded model is text-only; we want to convert that into
	// ErrVisionUnsupported so the toast can point users at a different
	// model rather than a raw JSON error.
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stubResponse(400, `{"error":{"message":"This model does not support image inputs","type":"invalid_request_error"}}`), nil
	})}
	p, _ := New(Config{Kind: KindOpenAICompat, BaseURL: "http://x/v1", Model: "m"}, client)
	_, err := p.Complete(context.Background(), Request{
		Prompt:    "x",
		Image:     []byte{1, 2, 3},
		ImageMIME: "image/png",
	})
	if !errors.Is(err, ErrVisionUnsupported) {
		t.Fatalf("err = %v, want ErrVisionUnsupported", err)
	}
}

func TestClaudeHappyPath(t *testing.T) {
	var capturedURL string
	var capturedHeaders http.Header
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.String()
		capturedHeaders = req.Header
		return stubResponse(200, `{
			"content":[{"type":"text","text":"hello from claude"}],
			"usage":{"input_tokens":7,"output_tokens":3}
		}`), nil
	})}
	p, err := New(Config{
		Kind:    KindClaude,
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-opus-4-5",
		APIKey:  "sk-ant-xxx",
	}, client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.Complete(context.Background(), Request{Prompt: "hi", MaxTokens: 10})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello from claude" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("TotalTokens = %d, want 10", resp.Usage.TotalTokens)
	}
	if capturedURL != "https://api.anthropic.com/v1/messages" {
		t.Errorf("URL = %q", capturedURL)
	}
	if got := capturedHeaders.Get("x-api-key"); got != "sk-ant-xxx" {
		t.Errorf("x-api-key = %q", got)
	}
	if got := capturedHeaders.Get("anthropic-version"); got == "" {
		t.Errorf("anthropic-version header missing")
	}
}

func TestNewRejectsInvalidKind(t *testing.T) {
	_, err := New(Config{Kind: Kind("nope"), BaseURL: "http://x"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestContextCancelMapsToTimeout(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	p, _ := New(Config{Kind: KindOpenAICompat, BaseURL: "http://x/v1", Model: "m"}, client)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := p.Complete(ctx, Request{Prompt: "x"})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}
