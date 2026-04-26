package app_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeOpenAIServer stands up a tiny HTTP endpoint speaking the
// OpenAI chat-completions shape. Tests point the ai config at its
// URL so we can exercise the admin alt-generation handler without
// requiring a live LM Studio instance in CI.
func fakeOpenAIServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"type":"image_url"`)) {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"vision part missing"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}],
			"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}
		}`))
	}))
}

func TestImagesGenerateAltEndToEnd(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-images-alt")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	fake := fakeOpenAIServer(t, "A red square on a blank background.")
	defer fake.Close()

	_ = authedPOSTForm(t, a.Handler(), "/admin/settings/ai", url.Values{
		"ai_enabled":  {"on"},
		"ai_kind":     {"openai-compat"},
		"ai_base_url": {fake.URL},
		"ai_model":    {"fake-vision"},
		"ai_auto_alt": {"on"},
	}, cookies)

	// Build a real PNG so the upload handler's image.Decode succeeds
	// and the stored file exists on disk for the alt reader.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}

	w := postUpload(t, a.Handler(), cookies, "probe.png", buf.Bytes(), true)
	if w.Code != 201 {
		t.Fatalf("upload = %d, body=%s", w.Code, w.Body.String())
	}
	var upload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	id := int64(upload["id"].(float64))
	if id == 0 {
		t.Fatalf("upload returned no id")
	}
	if got, _ := upload["auto_alt_requested"].(bool); !got {
		t.Fatalf("expected auto_alt_requested=true after enabling auto-alt")
	}

	var stored string
	_ = a.DB.QueryRow(`SELECT stored_path FROM images WHERE id = ?`, id).Scan(&stored)
	if stored == "" {
		t.Fatalf("stored_path blank for id=%d", id)
	}
	if _, err := os.Stat(filepath.Join(a.Config.ImageDir, filepath.FromSlash(stored))); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}

	w = authedPOSTForm(t, a.Handler(), "/admin/images/"+strconv.FormatInt(id, 10)+"/alt", url.Values{}, cookies)
	if w.Code != 200 {
		t.Fatalf("alt endpoint = %d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("response !ok: %v", resp)
	}
	if got, _ := resp["alt"].(string); !strings.Contains(got, "red square") {
		t.Errorf("alt = %q, want fake-reply text", got)
	}

	var savedAlt string
	_ = a.DB.QueryRow(`SELECT alt_text FROM images WHERE id = ?`, id).Scan(&savedAlt)
	if !strings.Contains(savedAlt, "red square") {
		t.Errorf("saved alt_text = %q", savedAlt)
	}

	var auditCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'ai.alt_generate'`).Scan(&auditCount)
	if auditCount != 1 {
		t.Errorf("ai.alt_generate audit count = %d, want 1", auditCount)
	}
}

// TestImagesUploadFlagsAutoAltWhenEnabled confirms the upload JSON
// returns auto_alt_requested = true for users who opted into auto
// alt, and false when the user hasn't configured an AI provider.
func TestImagesUploadFlagsAutoAltWhenEnabled(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-upload-flag")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Step 1: no AI configured. auto_alt_requested should be false.
	w := postUpload(t, a.Handler(), cookies, "pre.png", buf.Bytes(), true)
	if w.Code != 201 {
		t.Fatalf("pre upload = %d", w.Code)
	}
	var pre map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &pre)
	if got, _ := pre["auto_alt_requested"].(bool); got {
		t.Errorf("auto_alt_requested = true without AI config")
	}

	// Step 2: configure AI + autoAlt, upload again.
	fake := fakeOpenAIServer(t, "x")
	defer fake.Close()
	_ = authedPOSTForm(t, a.Handler(), "/admin/settings/ai", url.Values{
		"ai_enabled":  {"on"},
		"ai_kind":     {"openai-compat"},
		"ai_base_url": {fake.URL},
		"ai_model":    {"fake-vision"},
		"ai_auto_alt": {"on"},
	}, cookies)

	w = postUpload(t, a.Handler(), cookies, "post.png", buf.Bytes(), true)
	if w.Code != 201 {
		t.Fatalf("post upload = %d", w.Code)
	}
	var post map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &post)
	if got, _ := post["auto_alt_requested"].(bool); !got {
		t.Errorf("auto_alt_requested should be true after enabling auto-alt")
	}
}
