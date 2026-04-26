package ai

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"
)

// TestLMStudioSmoke is an opt-in end-to-end test that actually hits
// LM Studio. Set SB_LMSTUDIO_URL to the local endpoint (e.g.
// http://127.0.0.1:1234/v1) and SB_LMSTUDIO_MODEL to a loaded model
// to enable it. CI never sets these vars, so the test is skipped
// everywhere except a developer laptop.
func TestLMStudioSmoke(t *testing.T) {
	url := os.Getenv("SB_LMSTUDIO_URL")
	model := os.Getenv("SB_LMSTUDIO_MODEL")
	if url == "" || model == "" {
		t.Skip("SB_LMSTUDIO_URL + SB_LMSTUDIO_MODEL not set; skipping live LM Studio smoke")
	}

	p, err := New(Config{
		Kind:    KindOpenAICompat,
		BaseURL: url,
		Model:   model,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := p.Complete(ctx, Request{
		System:      "You are a terse assistant. Answer in English in five words or fewer.",
		Prompt:      "Say hello.",
		MaxTokens:   50,
		Temperature: 0.2,
	})
	if err != nil {
		t.Fatalf("LM Studio Complete: %v", err)
	}
	if resp.Text == "" {
		t.Fatalf("empty response text from LM Studio")
	}
	t.Logf("LM Studio text: %q (tokens in=%d out=%d)", resp.Text, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}

// TestLMStudioVisionSmoke verifies the vision path with a real
// multimodal model. Separate from the text smoke so a text-only
// model loaded in LM Studio doesn't fail the whole file — SB_LMSTUDIO_VISION_MODEL
// gates the vision model selection explicitly (e.g. google/gemma-3-12b).
func TestLMStudioVisionSmoke(t *testing.T) {
	url := os.Getenv("SB_LMSTUDIO_URL")
	model := os.Getenv("SB_LMSTUDIO_VISION_MODEL")
	if url == "" || model == "" {
		t.Skip("SB_LMSTUDIO_URL + SB_LMSTUDIO_VISION_MODEL not set; skipping vision smoke")
	}

	// Build a small coloured PNG so the model has *something* to
	// describe. 16×16 solid red — models can be expected to recognise
	// "red square".
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: 220, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	p, err := New(Config{Kind: KindOpenAICompat, BaseURL: url, Model: model}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := p.Complete(ctx, Request{
		System:    "You are a terse accessibility assistant. Describe the image in one short English sentence.",
		Prompt:    "Describe the image.",
		Image:     buf.Bytes(),
		ImageMIME: "image/png",
		MaxTokens: 60,
	})
	if err != nil {
		t.Fatalf("LM Studio vision Complete: %v", err)
	}
	if resp.Text == "" {
		t.Fatalf("empty vision response")
	}
	t.Logf("LM Studio vision text: %q (tokens in=%d out=%d)", resp.Text, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}
