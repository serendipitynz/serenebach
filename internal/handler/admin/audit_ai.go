package admin

import "github.com/serendipitynz/serenebach/internal/mcpaudit"

// newAIAuditEntry builds an mcpaudit.Entry scoped to an AI tool
// invocation. TokenID is always 0 because these calls originate from
// the admin UI (session-authenticated), not an MCP bearer token.
// AuthorID carries the logged-in user so the ops panel can show
// per-user usage alongside MCP-driven writes.
func newAIAuditEntry(wid, authorID int64, tool string, targetID int64, extra string) mcpaudit.Entry {
	return mcpaudit.Entry{
		WID:      wid,
		TokenID:  0,
		AuthorID: authorID,
		Tool:     tool,
		TargetID: targetID,
		Extra:    extra,
	}
}
