package app_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
)

// TestMCPAuditLogsPersistAcrossWriteTools verifies that every write-tool
// invocation lands in the audit store and the admin panel at
// /admin/settings/ai then surfaces those rows. Failure modes checked:
//   - audit rows don't include read calls (list_entries)
//   - audit table rows render in the settings/ops response body
//   - the panel's localized heading appears even on an empty-log instance
//     (regression guard — an earlier draft only rendered the table wrapper)
func TestMCPAuditLogsPersistAcrossWriteTools(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, tokenID := issueMCPTokenWithScope(t, a, cookies, "audit-probe", "write")

	// Read call — must NOT produce an audit row.
	_ = toolCallResult(t, callTool(t, a.Handler(), token, "list_entries", map[string]any{"limit": 2}))

	// Two writes under the same token: create + publish. Both should land.
	created := parseEntryJSON(t, toolCallResult(t, callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "Audited entry",
		"body":  "body",
	})))
	_ = toolCallResult(t, callTool(t, a.Handler(), token, "publish_entry", map[string]any{
		"id": created.ID,
	}))

	// Row count: exactly 2 (create + publish), both bound to the issued
	// token and the seed admin author (1).
	var (
		total    int
		creates  int
		publishs int
		byToken  int
	)
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 2 {
		t.Fatalf("audit row count = %d, want 2 (list_entries leaked into audit?)", total)
	}
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'create_entry'`).Scan(&creates)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'publish_entry'`).Scan(&publishs)
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log WHERE token_id = ?`, tokenID).Scan(&byToken)
	if creates != 1 || publishs != 1 {
		t.Errorf("per-tool counts = %d / %d, want 1 / 1", creates, publishs)
	}
	if byToken != 2 {
		t.Errorf("rows bound to token %d = %d, want 2", tokenID, byToken)
	}

	// Admin panel renders the newly-created rows — assert against the
	// i18n-rendered heading + tool column content so a cosmetic class
	// rename doesn't break the test, but a missing panel does.
	w := authedGET(t, a.Handler(), "/admin/settings/ai", cookies)
	if w.Code != 200 {
		t.Fatalf("settings/ops GET = %d; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"MCP 監査ログ",
		"create_entry",
		"publish_entry",
		"audit-probe",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/settings/ai body missing %q", want)
		}
	}
}

// TestMCPAuditLogExternalDBIsolatesAuditRows covers the SB_MCP_AUDIT_DB
// escape hatch. Rows must land in the external file, the main DB's
// mcp_audit_log must stay empty, and the admin panel must still
// surface the entries because it reads through the wired store rather
// than querying the main DB directly.
func TestMCPAuditLogExternalDBIsolatesAuditRows(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	externalAudit := filepath.Join(tmp, "audit.db")

	cfg := &config.Config{
		Mode:           config.ModeServer,
		Addr:           ":0",
		DBPath:         filepath.Join(tmp, "main.db"),
		RebuildOutDir:  filepath.Join(tmp, "public"),
		ImageDir:       filepath.Join(tmp, "img"),
		TemplateDir:    filepath.Join(tmp, "templates"),
		UploadMaxBytes: 10 << 20,
		MCPAuditDBPath: externalAudit,
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Seed(context.Background(), app.DefaultSeed()); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "external-audit", "write")
	_ = toolCallResult(t, callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "external audit",
		"body":  "body",
	}))

	// Main DB must be untouched.
	var mainCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log`).Scan(&mainCount)
	if mainCount != 0 {
		t.Fatalf("main-DB mcp_audit_log got %d rows; want 0 when SB_MCP_AUDIT_DB points elsewhere", mainCount)
	}

	// External DB carries the audit row.
	extDB, err := sql.Open("sqlite", "file:"+externalAudit)
	if err != nil {
		t.Fatalf("open external audit db: %v", err)
	}
	defer extDB.Close()
	var extCount int
	if err := extDB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log`).Scan(&extCount); err != nil {
		t.Fatalf("query external: %v", err)
	}
	if extCount != 1 {
		t.Fatalf("external audit db count = %d, want 1", extCount)
	}

	// Admin panel renders the row through the wired store on the
	// AI settings tab.
	w := authedGET(t, a.Handler(), "/admin/settings/ai", cookies)
	if w.Code != 200 {
		t.Fatalf("settings/ai GET = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "create_entry") {
		t.Errorf("panel body should surface create_entry audit row; got:\n%s", w.Body.String())
	}
	// Env-var snapshot with the external path lives on the 基本設定 tab
	// (the admin dashboards-style reference table). AI tab doesn't
	// re-render it.
	wBasic := authedGET(t, a.Handler(), "/admin/settings/basic", cookies)
	if !strings.Contains(wBasic.Body.String(), "external:"+externalAudit) {
		t.Errorf("basic settings page should show external audit path %q", externalAudit)
	}
}

func TestMCPAuditLogNotEmittedForReadOnlyCalls(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "read-probe")

	_ = toolCallResult(t, callTool(t, a.Handler(), token, "list_entries", nil))
	_ = toolCallResult(t, callTool(t, a.Handler(), token, "list_tags", nil))

	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("audit rows after read-only calls = %d, want 0", count)
	}
}
