package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// parsePositiveID extracts an int64 URL param, returning ok=false when
// missing, non-numeric, or non-positive. The caller is responsible for
// writing the response when ok=false — HTML form routes typically
// respond with http.NotFound, JSON/API routes with
// writeJSON(StatusBadRequest, ...). The helper deliberately does not
// own that choice so the existing status-code split survives.
//
//nolint:unparam // name is always "id" today but kept parametric for future non-id params.
func parsePositiveID(r *http.Request, name string) (int64, bool) {
	raw := chi.URLParam(r, name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// postFormValue reads a POST-only form field with leading/trailing
// whitespace trimmed. Mirrors the recurring
// `strings.TrimSpace(r.PostFormValue(key))` pattern.
//
// PostFormValue (not FormValue) is intentional: the admin surface
// rejects URL-query-string values to avoid accidental leakage from
// GET parameters into write-path handlers.
func postFormValue(r *http.Request, key string) string {
	return strings.TrimSpace(r.PostFormValue(key))
}

// writeJSON serialises payload as JSON with the given status code.
// Centralised here (rather than images.go) because every JSON endpoint
// in the admin surface — image alt, AI compose, MCP token mutations,
// webhook tests — funnels through this helper. Encode failures are
// ignored: the response is already committed by the time Encode runs,
// so the caller has no recovery option.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = encodeJSON(w, payload)
}
