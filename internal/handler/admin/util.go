package admin

import (
	"net/http"
)

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
