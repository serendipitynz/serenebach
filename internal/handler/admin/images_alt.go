package admin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/ai"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// altTextTimeout is the budget for one synchronous alt-text
// generation. 45 s covers most local vision models at reasonable
// image sizes without wedging the admin UI on a slow response.
const altTextTimeout = 45 * time.Second

// altTextSystemPrompt asks the vision model for a short,
// accessibility-style description. "One sentence" + "no markdown"
// guardrails keep the output usable as a raw HTML alt attribute
// without further post-processing.
const altTextSystemPrompt = `You generate alt text for screen-reader users. Produce one short descriptive sentence in the language of the blog (Japanese if the user speaks Japanese, otherwise English). Do not start with "An image of" or "A picture of". Do not use Markdown. Under 160 characters.`

// imagesGenerateAlt runs vision inference against an already-uploaded
// image and stores the result as images.alt_text. JS calls this right
// after a successful upload when the uploader's AIAutoAlt flag is
// set; the endpoint is also usable manually if a user wants to
// re-generate alt for an older upload.
func (h *Handler) imagesGenerateAlt(w http.ResponseWriter, r *http.Request) {
	actor := session.UserFrom(r.Context())
	if actor == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	id, ok := parsePositiveID(r, "id")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad id"})
		return
	}

	user, provider, ok := h.loadAltGenerationProvider(w, r, actor.ID)
	if !ok {
		return
	}

	img, ok := h.loadAltGenerationImage(w, r, id)
	if !ok {
		return
	}

	// Read the stored bytes off disk. The thumbnail would be smaller
	// (faster) but the stored file is what actually ships in the entry,
	// so describing that is what screen-reader users will hear.
	fullPath := filepath.Join(h.ImageDir, filepath.FromSlash(img.StoredPath))
	bytesRaw, err := os.ReadFile(fullPath)
	if err != nil {
		log.Printf("admin.imagesGenerateAlt: read file %q: %v", fullPath, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "read_file"})
		return
	}

	altPrompt := "Please write alt text for this image."
	ctx, cancel := context.WithTimeout(r.Context(), resolveAITimeout(*user, altTextTimeout))
	defer cancel()
	startedAt := time.Now()
	resp, err := provider.Complete(ctx, ai.Request{
		System:      altTextSystemPrompt,
		Prompt:      altPrompt,
		Image:       bytesRaw,
		ImageMIME:   img.MimeType,
		MaxTokens:   180,
		Temperature: 0.3,
	})
	latencyMS := time.Since(startedAt).Milliseconds()
	// Image bytes are excluded — they're sent base64 in vision calls
	// but the comparison we care about is text-token vs text-bytes.
	requestBytes := len(altTextSystemPrompt) + len(altPrompt)
	if err != nil {
		msg := altGenerationErrorCode(err)
		h.auditAI(r.Context(), *actor, "ai.alt_generate", id, aiCallRecord{
			Status:       "err",
			ErrCode:      msg,
			Model:        user.AIModel,
			FinishReason: resp.FinishReason,
			Usage:        resp.Usage,
			Sanitized:    resp.Sanitized,
			LatencyMS:    latencyMS,
			RequestBytes: requestBytes,
		})
		// tool-error shape — non-transport errors use 200 + ok:false.
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
		return
	}

	alt := resp.Text
	if err := h.Store.UpdateImageAltText(r.Context(), h.wid(), id, alt); err != nil {
		log.Printf("admin.imagesGenerateAlt: save alt: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "save_alt"})
		return
	}

	h.auditAI(r.Context(), *actor, "ai.alt_generate", id, aiCallRecord{
		Status:       "ok",
		Model:        user.AIModel,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
		Sanitized:    resp.Sanitized,
		LatencyMS:    latencyMS,
		RequestBytes: requestBytes,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":  true,
		"alt": alt,
		"usage": map[string]int{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
	})
}

// loadAltGenerationProvider resolves the signed-in user's saved AI
// provider and validates that it supports vision. ok=false means the
// JSON error response has already been written and the caller must
// stop.
func (h *Handler) loadAltGenerationProvider(w http.ResponseWriter, r *http.Request, userID int64) (*domain.User, ai.Provider, bool) {
	user, err := h.Store.UserByID(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "load user"})
		return nil, nil, false
	}
	provider, err := providerForUser(*user)
	if err != nil {
		if errors.Is(err, ai.ErrUnconfigured) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "ai_unconfigured"})
			return nil, nil, false
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return nil, nil, false
	}
	if !provider.SupportsVision() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "vision_unsupported"})
		return nil, nil, false
	}
	return user, provider, true
}

// loadAltGenerationImage resolves the image row and maps the not-found
// case to a 404 JSON response. Any other DB error falls back to a 500
// JSON so the caller can stop on ok=false.
func (h *Handler) loadAltGenerationImage(w http.ResponseWriter, r *http.Request, id int64) (*domain.Image, bool) {
	img, err := h.Store.ImageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not_found"})
			return nil, false
		}
		log.Printf("admin.imagesGenerateAlt: load: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "load_image"})
		return nil, false
	}
	return img, true
}

// altGenerationErrorCode maps a provider Complete() failure to the
// short code the JS toast keys off. Unknown errors fall through to
// err.Error() so the operator still has the raw text to grep.
func altGenerationErrorCode(err error) string {
	switch {
	case errors.Is(err, ai.ErrVisionUnsupported):
		return "vision_unsupported"
	case errors.Is(err, ai.ErrTimeout):
		return "timeout"
	case errors.Is(err, ai.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, ai.ErrUnauthorized):
		return "unauthorized"
	}
	return err.Error()
}

// aiCallRecord captures everything we want surfaced in the audit log
// for a single AI invocation, success or failure. The Extra() method
// renders this into a single space-separated string that fits the
// existing mcp_audit_log.extra TEXT column without schema changes.
type aiCallRecord struct {
	Status       string // "ok" | "err"
	ErrCode      string // populated when Status=="err" — the same code the JS toast keys off
	Model        string // user.AIModel snapshot at call time
	FinishReason string // provider-native ("stop" | "length" | "end_turn" | …)
	Usage        ai.Usage
	Sanitized    ai.SanitizeInfo
	LatencyMS    int64
	// RequestBytes is the byte length of system+prompt as we sent it
	// (UTF-8). Compared against Usage.PromptTokens this tells an
	// operator whether the provider is inflating the prompt with a
	// hidden chat template / preset addendum, which is the failure
	// mode behind silent reasoning-budget exhaustion on local models.
	RequestBytes int
}

// Extra renders the record as a single key=value-separated line.
// Order is fixed so a future log parser can grep for `err=` / `model=`
// without surprises. Values containing spaces are not expected
// (codes / model ids never contain whitespace) so we don't bother
// quoting.
func (r aiCallRecord) Extra() string {
	parts := make([]string, 0, 8)
	if r.Status != "" {
		parts = append(parts, "status="+r.Status)
	}
	if r.ErrCode != "" {
		parts = append(parts, "err="+r.ErrCode)
	}
	if r.Model != "" {
		parts = append(parts, "model="+r.Model)
	}
	if r.FinishReason != "" {
		parts = append(parts, "finish="+r.FinishReason)
	}
	if r.RequestBytes > 0 {
		parts = append(parts, fmt.Sprintf("bytes=%d", r.RequestBytes))
	}
	if r.Usage.TotalTokens > 0 || r.Usage.PromptTokens > 0 || r.Usage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens=%d/%d/%d",
			r.Usage.PromptTokens, r.Usage.CompletionTokens, r.Usage.TotalTokens))
	}
	if r.Sanitized.RawLen > 0 {
		parts = append(parts, fmt.Sprintf("len=%d->%d", r.Sanitized.RawLen, r.Sanitized.CleanLen))
	}
	if r.Sanitized.Stripped() {
		flags := make([]string, 0, 3)
		if r.Sanitized.HarmonyFinal {
			flags = append(flags, "harmony")
		}
		if r.Sanitized.ThinkBlock {
			flags = append(flags, "think")
		}
		if r.Sanitized.HarmonyTokens {
			flags = append(flags, "tok")
		}
		parts = append(parts, "stripped="+strings.Join(flags, ","))
	}
	if r.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("ms=%d", r.LatencyMS))
	}
	return strings.Join(parts, " ")
}

// auditAI records one AI tool call in mcp_audit_log. Reusing the same
// table as MCP writes keeps the admin's "who did what when" surface
// in one place; the tool column distinguishes (create_entry vs
// ai.alt_generate etc) so the panel can later add filters. Failures
// only log — audit is observational, never load-bearing.
func (h *Handler) auditAI(ctx context.Context, user domain.User, tool string, targetID int64, rec aiCallRecord) {
	if h.Audit == nil {
		return
	}
	_, err := h.Audit.Insert(ctx, newAIAuditEntry(h.wid(), user.ID, tool, targetID, rec.Extra()))
	if err != nil {
		log.Printf("admin.auditAI: %v", err)
	}
}
