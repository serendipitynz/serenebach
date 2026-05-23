package admin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountMCPTokens registers the MCP token POST endpoints. The token
// management UI itself lives on the 運用設定 tab of the settings page
// (/admin/settings/ai), so no standalone GET here — the /settings/mcp
// GET path just redirects to the ops tab for backward compatibility
// with sidebar bookmarks.
func (h *Handler) mountMCPTokens(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireAdmin)
		gr.Get("/settings/mcp", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, root(r)+"/admin/settings/ai", http.StatusMovedPermanently)
		})
		gr.Post("/settings/mcp/new", h.mcpTokensCreate)
		gr.Post("/settings/mcp/{id}/revoke", h.mcpTokensRevoke)
	})
}

// tokenPrefix is the leading marker on every generated raw token. Makes
// accidental leaks to public repos / paste dumps grep-able.
const tokenPrefix = "sb_mcp_"

type mcpTokenRow struct {
	domain.MCPToken
	CreatedAtFmt  string
	LastUsedAtFmt string
	RevokedAtFmt  string
	// AuthorLabel is the display-name (or login name) of the user the
	// token is bound to, resolved against the current user list so the
	// token list column can show a human-readable name next to scope.
	AuthorLabel string
}

func (h *Handler) mcpTokensCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderSettingsAI(w, r, "", 0, tr(r, "flash.formParseError"))
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		h.renderSettingsAI(w, r, "", 0, tr(r, "settings.ops.mcp.error.nameRequired"))
		return
	}
	if len(name) > 100 {
		h.renderSettingsAI(w, r, "", 0, tr(r, "settings.ops.mcp.error.nameTooLong"))
		return
	}
	// Two-checkbox UI: read is always implicit on the backend (every
	// token can read), write is the opt-in. `scope_write=1` promotes
	// the token to write; absence = plain read. The legacy `scope=`
	// form field is still honoured so older minted-via-API callers
	// (and the existing test helpers) keep working.
	var scope domain.MCPScope
	if r.PostFormValue("scope_write") != "" {
		scope = domain.MCPScopeWrite
	} else if legacy := strings.TrimSpace(r.PostFormValue("scope")); legacy != "" {
		scope = domain.MCPScope(legacy)
	} else {
		scope = domain.MCPScopeRead
	}
	if !scope.Valid() {
		h.renderSettingsAI(w, r, "", 0, tr(r, "settings.ops.mcp.error.scope"))
		return
	}
	// author_id: required. Reject zero / unknown / unparseable up front so
	// no token row lands unbound. Validate against the current user list
	// rather than UserByID so a typo'd id surfaces the same error.
	authorID, _ := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("author_id")), 10, 64)
	if authorID <= 0 {
		h.renderSettingsAI(w, r, "", 0, tr(r, "settings.ops.mcp.error.author"))
		return
	}
	users, err := h.Store.ListUsers(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.mcpTokensCreate: list users: %v", err)
		http.Error(w, "failed to resolve author", http.StatusInternalServerError)
		return
	}
	var authorOK bool
	for _, u := range users {
		if u.ID == authorID {
			authorOK = true
			break
		}
	}
	if !authorOK {
		h.renderSettingsAI(w, r, "", 0, tr(r, "settings.ops.mcp.error.author"))
		return
	}
	raw, err := generateRawToken()
	if err != nil {
		log.Printf("admin.mcpTokensCreate: rand: %v", err)
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}
	id, err := h.Store.CreateMCPToken(r.Context(), h.wid(), name, raw, scope, authorID)
	if err != nil {
		log.Printf("admin.mcpTokensCreate: save: %v", err)
		http.Error(w, "failed to save token", http.StatusInternalServerError)
		return
	}
	// Render the raw token ONCE, then the next page load has to use
	// the hashed form. Mirrors GitHub PAT / Anthropic console UX.
	h.renderSettingsAI(w, r, raw, id, "")
}

func (h *Handler) mcpTokensRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.RevokeMCPToken(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.mcpTokensRevoke: %v", err)
		http.Error(w, "failed to revoke", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/settings/ai", http.StatusSeeOther)
}

// generateRawToken returns a 32-byte random token encoded as hex and
// prefixed with sb_mcp_ for grep-ability. 64 hex chars = 256 bits of
// entropy, far beyond brute-force range for the life of a weblog.
func generateRawToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return tokenPrefix + hex.EncodeToString(b[:]), nil
}

func fmtUnix(ts int64) string {
	if ts <= 0 {
		return "—"
	}
	return time.Unix(ts, 0).Local().Format("2006-01-02 15:04")
}

func fmtUnixOrDash(ts int64) string { return fmtUnix(ts) }
