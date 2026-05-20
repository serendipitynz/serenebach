package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
)

// seedAdminUserAndToken creates a minimum-viable admin user + one MCP
// token, returning the admin's ID so the test can request the AI
// settings page authenticated as them. The MCP block only renders for
// users where CanManageUsers() == true (admin tier).
func seedAdminUserAndToken(t *testing.T, h *Handler) int64 {
	t.Helper()
	uid, err := h.Store.CreateUser(context.Background(), domain.User{
		WID:  1,
		Name: "admin-mcp",
		Role: domain.RoleAdmin,
	}, "pw")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := h.Store.CreateMCPToken(context.Background(), 1, "tok-alpha", "raw-alpha",
		domain.MCPScopeRead, uid); err != nil {
		t.Fatalf("CreateMCPToken: %v", err)
	}
	return uid
}

func TestSettingsAI_MCPTokenSortLinkRenders(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	uid := seedAdminUserAndToken(t, h)

	req := httptest.NewRequest(http.MethodGet, "/admin/settings/ai?sort=name&dir=asc", nil)
	u := &domain.User{ID: uid, Name: "admin-mcp", Role: domain.RoleAdmin}
	req = req.WithContext(session.WithUser(req.Context(), u))
	rec := httptest.NewRecorder()
	h.settingsAIForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tok-alpha") {
		t.Error("seeded token should be listed")
	}
	if !strings.Contains(body, `sort-link active asc`) {
		t.Error(`name column should render with "active asc" class`)
	}
	// Active column toggles to desc on next click.
	if !strings.Contains(body, `sort=name&amp;dir=desc`) && !strings.Contains(body, `sort=name&dir=desc`) {
		t.Error("active name column should toggle to desc on next click")
	}
}
