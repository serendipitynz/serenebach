package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/serendipitynz/serenebach/internal/analytics"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// requiredScope returns the scope a given tool needs. Keep this table
// alongside the tool names — future capability granularity (e.g.
// per-tool scopes) can slot in without rewriting the dispatcher.
func requiredScope(toolName string) domain.MCPScope {
	switch toolName {
	case "create_entry", "update_entry", "publish_entry", "upload_image":
		return domain.MCPScopeWrite
	}
	return domain.MCPScopeRead
}

// toolDescriptor is the shape the MCP spec expects for each entry in
// the tools/list response. InputSchema is a JSON Schema object the
// client uses to validate and hint at arguments.
type toolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

const defaultListLimit = 20
const maxListLimit = 100

// toolDescriptors returns the catalogue filtered by the caller's
// scope. Read-scoped clients never see the write tools at all —
// that keeps the LLM from attempting calls it's guaranteed to fail.
func toolDescriptors(scope domain.MCPScope) []toolDescriptor {
	all := allToolDescriptors()
	if scope.CanWrite() {
		return all
	}
	out := make([]toolDescriptor, 0, len(all))
	for _, t := range all {
		if requiredScope(t.Name) == domain.MCPScopeRead {
			out = append(out, t)
		}
	}
	return out
}

func allToolDescriptors() []toolDescriptor {
	return []toolDescriptor{
		{
			Name:        "list_entries",
			Description: "List recent published entries on this weblog, newest-first. Returns metadata only (title, slug, category, tags, posted_at); call get_entry to fetch the body.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit},
					"offset": map[string]any{"type": "integer", "minimum": 0, "default": 0},
				},
			},
		},
		{
			Name:        "get_entry",
			Description: "Fetch one entry by numeric id or URL slug. Returns the full body (raw — format field tells the renderer which syntax it's in).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":   map[string]any{"type": "integer", "description": "Numeric entry id"},
					"slug": map[string]any{"type": "string", "description": "URL slug (falls back to id when empty)"},
				},
			},
		},
		{
			Name:        "search_entries",
			Description: "Full-text search across published entries' title, body, 追記, and keywords. Case-insensitive substring match. Returns matches newest-first.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "minLength": 1},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit},
				},
			},
		},
		{
			Name:        "list_categories",
			Description: "List every category on this weblog with its sort order and entry count. Hierarchical relationships surface via parent_id.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "list_tags",
			Description: "List every tag on this weblog. Tags are flat (no hierarchy) and accumulate from entry form input; each row carries the tag's assigned entry count.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_analytics",
			Description: "First-party analytics summary for the weblog: total page views, unique + returning visitors, and the top entries in the window. `days` chooses the window (default 30).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"days": map[string]any{"type": "integer", "minimum": 1, "maximum": 365, "default": 30},
					"top":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
				},
			},
		},
		{
			Name:        "list_images",
			Description: "List uploaded files newest-first. Optional `kind` filter narrows to image / audio / document / movie.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxListLimit, "default": defaultListLimit},
					"offset": map[string]any{"type": "integer", "minimum": 0, "default": 0},
					"kind":   map[string]any{"type": "string", "enum": []string{"image", "audio", "document", "movie"}},
				},
			},
		},
		{
			Name:        "create_entry",
			Description: "Create a new entry. `title` and `body` are required; `status` defaults to \"draft\". Optional: slug (auto-fills from title when omitted), more (追記), keywords, format (\"html\" | \"markdown\" | \"sbtext\", default \"html\"), category_id, tags (array of tag names — unknown names are created on the fly), posted_at (RFC3339; defaults to now). Intended for AI-drafted content: the human reviews the draft in the admin before publishing.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"title", "body"},
				"properties": map[string]any{
					"title":           map[string]any{"type": "string", "minLength": 1},
					"body":            map[string]any{"type": "string"},
					"more":            map[string]any{"type": "string"},
					"slug":            map[string]any{"type": "string"},
					"keywords":        map[string]any{"type": "string"},
					"format":          map[string]any{"type": "string", "enum": []string{"html", "markdown", "sbtext"}},
					"status":          map[string]any{"type": "string", "enum": []string{"draft", "published", "closed"}, "default": "draft"},
					"category_id":     map[string]any{"type": "integer", "minimum": 0},
					"tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"posted_at":       map[string]any{"type": "string", "description": "RFC3339 timestamp; defaults to now"},
					"pinned":          map[string]any{"type": "boolean", "description": "Float the entry to the top of home and category page 1"},
					"accept_comments": map[string]any{"type": "boolean", "description": "Accept comments on this entry. Defaults to true; ignored when the weblog's comment_mode is closed."},
				},
			},
		},
		{
			Name:        "update_entry",
			Description: "Update an existing entry by id. Every field except `id` is optional; unset fields preserve the current row's value. Passing `tags` replaces the full tag set — pass an empty array to clear. Passing `status` changes publish state; prefer publish_entry for the common publish flow so the intent is explicit in the audit log.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":              map[string]any{"type": "integer", "minimum": 1},
					"title":           map[string]any{"type": "string"},
					"body":            map[string]any{"type": "string"},
					"more":            map[string]any{"type": "string"},
					"slug":            map[string]any{"type": "string"},
					"keywords":        map[string]any{"type": "string"},
					"format":          map[string]any{"type": "string", "enum": []string{"html", "markdown", "sbtext"}},
					"status":          map[string]any{"type": "string", "enum": []string{"draft", "published", "closed"}},
					"category_id":     map[string]any{"type": "integer", "minimum": 0},
					"tags":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"posted_at":       map[string]any{"type": "string", "description": "RFC3339 timestamp"},
					"pinned":          map[string]any{"type": "boolean", "description": "Float the entry to the top of home and category page 1"},
					"accept_comments": map[string]any{"type": "boolean", "description": "Accept comments on this entry. Ignored when the weblog's comment_mode is closed."},
				},
			},
		},
		{
			Name:        "publish_entry",
			Description: "Flip an entry's status to published. Optional `posted_at` (RFC3339) lets the caller set the publication timestamp — pass a future time to schedule, or omit to keep the existing posted_at. Separate from update_entry so \"write\" authority and \"publish\" authority can diverge in a future release.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":        map[string]any{"type": "integer", "minimum": 1},
					"posted_at": map[string]any{"type": "string", "description": "RFC3339 timestamp; omit to keep current"},
				},
			},
		},
		{
			Name:        "upload_image",
			Description: "Upload an image asset for use in entries. `data` is the raw image bytes, base64-encoded. `filename` seeds the on-disk slug and is kept as the human-readable label (defaults to \"image\" when empty). `mime_type` is optional — when omitted, the server sniffs the first 512 bytes. Accepted types: image/jpeg, image/png, image/gif, image/webp. Returns the stored id, URL, thumb URL, and pixel dimensions so the caller can embed the image into a subsequent create_entry / update_entry call.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"data"},
				"properties": map[string]any{
					"data":      map[string]any{"type": "string", "description": "base64-encoded image bytes"},
					"filename":  map[string]any{"type": "string"},
					"mime_type": map[string]any{"type": "string", "enum": []string{"image/jpeg", "image/png", "image/gif", "image/webp"}},
				},
			},
		},
	}
}

// callTool looks up the named tool and runs it. Returned string is the
// tool's user-visible output; callers wrap it in the MCP content
// envelope. An error here becomes an MCP "tool error" on the wire.
func (s *Server) callTool(ctx context.Context, name string, rawArgs json.RawMessage) (string, error) {
	// Scope gate runs before any work — a read-only token trying to
	// create an entry stops here rather than lighting up the repo.
	if requiredScope(name) == domain.MCPScopeWrite {
		auth := authFromContext(ctx)
		if !auth.Scope.CanWrite() {
			return "", fmt.Errorf("tool %q requires write scope (token has %q)", name, auth.Scope)
		}
	}
	switch name {
	case "list_entries":
		return s.toolListEntries(ctx, rawArgs)
	case "get_entry":
		return s.toolGetEntry(ctx, rawArgs)
	case "search_entries":
		return s.toolSearchEntries(ctx, rawArgs)
	case "list_categories":
		return s.toolListCategories(ctx)
	case "list_tags":
		return s.toolListTags(ctx)
	case "get_analytics":
		return s.toolGetAnalytics(ctx, rawArgs)
	case "list_images":
		return s.toolListImages(ctx, rawArgs)
	case "create_entry":
		return s.toolCreateEntry(ctx, rawArgs)
	case "update_entry":
		return s.toolUpdateEntry(ctx, rawArgs)
	case "publish_entry":
		return s.toolPublishEntry(ctx, rawArgs)
	case "upload_image":
		return s.toolUploadImage(ctx, rawArgs)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// --- tool implementations ----------------------------------------------

// entrySummary is the shape returned by list_entries / search_entries —
// metadata only, body omitted so an LLM can browse the catalogue
// cheaply before pulling full content via get_entry.
type entrySummary struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug,omitempty"`
	Keywords  string    `json:"keywords,omitempty"`
	Status    string    `json:"status"`
	Format    string    `json:"format"`
	PostedAt  time.Time `json:"posted_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func summariseEntry(e domain.Entry) entrySummary {
	return entrySummary{
		ID: e.ID, Title: e.Title, Slug: e.Slug, Keywords: e.Keywords,
		Status: entryStatusLabel(e.Status), Format: e.Format,
		PostedAt: e.PostedAt, UpdatedAt: e.UpdatedAt,
	}
}

func (s *Server) toolListEntries(ctx context.Context, raw json.RawMessage) (string, error) {
	args := struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}{Limit: defaultListLimit, Offset: 0}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.Limit <= 0 {
		args.Limit = defaultListLimit
	}
	if args.Limit > maxListLimit {
		args.Limit = maxListLimit
	}
	if args.Offset < 0 {
		args.Offset = 0
	}

	entries, err := s.Store.RecentPublishedEntriesPage(ctx, s.WID, args.Limit, args.Offset)
	if err != nil {
		return "", err
	}
	total, err := s.Store.CountPublishedEntries(ctx, s.WID)
	if err != nil {
		return "", err
	}
	out := make([]entrySummary, 0, len(entries))
	for _, e := range entries {
		out = append(out, summariseEntry(e))
	}
	return jsonResponse(map[string]any{
		"entries": out,
		"total":   total,
		"limit":   args.Limit,
		"offset":  args.Offset,
	})
}

// toolGetEntry fetches by id when present; falls back to slug otherwise.
// Numeric-looking slugs are tried as ids first so `get_entry {"slug":"42"}`
// also works (some MCP clients stringify everything).
func (s *Server) toolGetEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	args := struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.ID == 0 && args.Slug == "" {
		return "", errors.New("either id or slug is required")
	}
	var entry *domain.Entry
	var err error
	n, perr := strconv.ParseInt(args.Slug, 10, 64)
	switch {
	case args.ID > 0:
		entry, err = s.Store.EntryByID(ctx, s.WID, args.ID)
	case perr == nil && n > 0:
		// args.Slug is numeric — try the id form first so authors can
		// paste either form. Fall back to slug lookup if no entry with
		// that id exists (the slug may itself look numeric).
		entry, err = s.Store.EntryByID(ctx, s.WID, n)
		if errors.Is(err, repo.ErrNotFound) {
			entry, err = s.Store.EntryBySlug(ctx, s.WID, args.Slug)
		}
	default:
		entry, err = s.Store.EntryBySlug(ctx, s.WID, args.Slug)
	}
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return "", errors.New("entry not found")
		}
		return "", err
	}
	// The full entry response includes the body + more (追記) so the
	// caller gets everything it needs in one round-trip.
	return jsonResponse(map[string]any{
		"id":          entry.ID,
		"title":       entry.Title,
		"slug":        entry.Slug,
		"keywords":    entry.Keywords,
		"status":      entryStatusLabel(entry.Status),
		"format":      entry.Format,
		"body":        entry.Body,
		"more":        entry.More,
		"posted_at":   entry.PostedAt,
		"updated_at":  entry.UpdatedAt,
		"category_id": entry.CategoryID,
		"author_id":   entry.AuthorID,
	})
}

func (s *Server) toolSearchEntries(ctx context.Context, raw json.RawMessage) (string, error) {
	args := struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{Limit: defaultListLimit}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.Query == "" {
		return "", errors.New("query is required")
	}
	if args.Limit <= 0 {
		args.Limit = defaultListLimit
	}
	if args.Limit > maxListLimit {
		args.Limit = maxListLimit
	}
	entries, err := s.Store.SearchPublishedEntries(ctx, s.WID, args.Query, args.Limit)
	if err != nil {
		return "", err
	}
	out := make([]entrySummary, 0, len(entries))
	for _, e := range entries {
		out = append(out, summariseEntry(e))
	}
	return jsonResponse(map[string]any{
		"query":   args.Query,
		"entries": out,
		"matched": len(out),
	})
}

func (s *Server) toolListCategories(ctx context.Context) (string, error) {
	cats, err := s.Store.AllCategories(ctx, s.WID)
	if err != nil {
		return "", err
	}
	type row struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Slug       string `json:"slug,omitempty"`
		ParentID   int64  `json:"parent_id"`
		SortOrder  int    `json:"sort_order"`
		EntryCount int64  `json:"entry_count"`
	}
	out := make([]row, 0, len(cats))
	for _, c := range cats {
		n, _ := s.Store.CountPublishedEntriesByCategory(ctx, s.WID, c.ID)
		out = append(out, row{c.ID, c.Name, c.Slug, c.ParentID, c.SortOrder, n})
	}
	return jsonResponse(map[string]any{"categories": out})
}

func (s *Server) toolListTags(ctx context.Context) (string, error) {
	tags, err := s.Store.AllTags(ctx, s.WID)
	if err != nil {
		return "", err
	}
	type row struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		EntryCount int64  `json:"entry_count"`
	}
	out := make([]row, 0, len(tags))
	for _, t := range tags {
		n, _ := s.Store.CountPublishedEntriesByTag(ctx, s.WID, t.ID)
		out = append(out, row{t.ID, t.Name, t.Slug, n})
	}
	return jsonResponse(map[string]any{"tags": out})
}

func (s *Server) toolGetAnalytics(ctx context.Context, raw json.RawMessage) (string, error) {
	args := struct {
		Days int `json:"days"`
		Top  int `json:"top"`
	}{Days: 30, Top: 10}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.Days <= 0 {
		args.Days = 30
	}
	if args.Days > 365 {
		args.Days = 365
	}
	if args.Top <= 0 {
		args.Top = 10
	}
	if args.Top > 50 {
		args.Top = 50
	}
	if s.Analytics == nil {
		return jsonResponse(map[string]any{
			"enabled": false,
			"note":    "analytics disabled; start with SB_ANALYTICS_DISABLED unset to collect data",
		})
	}
	since := time.Now().Add(-time.Duration(args.Days) * 24 * time.Hour)
	summary, err := s.Analytics.Summarise(ctx, since)
	if err != nil {
		return "", err
	}
	out := map[string]any{
		"enabled":         true,
		"window_days":     args.Days,
		"page_views":      summary.PageViews,
		"unique_visitors": summary.UniqueVisitors,
		"return_visitors": summary.ReturnVisitors,
	}
	// TopEntries returns (entry_id, views, likes, stamps). Title
	// resolution is left to the agent — it can call get_entry with the
	// id — so the tool stays one SQL round-trip.
	if s.Store != nil {
		top, err := s.Analytics.TopEntries(ctx, s.Store.DB(), since, args.Top, analytics.SortByViews)
		if err == nil {
			rows := make([]map[string]any, 0, len(top))
			for _, t := range top {
				rows = append(rows, map[string]any{
					"id":     t.EntryID,
					"views":  t.Views,
					"likes":  t.Likes,
					"stamps": t.Stamps,
				})
			}
			out["top_entries"] = rows
		}
	}
	return jsonResponse(out)
}

func (s *Server) toolListImages(ctx context.Context, raw json.RawMessage) (string, error) {
	args := struct {
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
		Kind   string `json:"kind"`
	}{Limit: defaultListLimit, Offset: 0}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.Limit <= 0 {
		args.Limit = defaultListLimit
	}
	if args.Limit > maxListLimit {
		args.Limit = maxListLimit
	}
	if args.Offset < 0 {
		args.Offset = 0
	}
	images, err := s.Store.ListImagesForAdmin(ctx, s.WID, args.Kind, args.Limit, args.Offset)
	if err != nil {
		return "", err
	}
	type row struct {
		ID        int64  `json:"id"`
		Filename  string `json:"filename"`
		Stored    string `json:"stored_path"`
		URL       string `json:"url"`
		Thumb     string `json:"thumb_url,omitempty"`
		Kind      string `json:"kind"`
		MimeType  string `json:"mime_type"`
		SizeBytes int64  `json:"size_bytes"`
		Width     int64  `json:"width,omitempty"`
		Height    int64  `json:"height,omitempty"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]row, 0, len(images))
	for _, img := range images {
		r := row{
			ID: img.ID, Filename: img.Filename, Stored: img.StoredPath,
			URL:       "/img/" + img.StoredPath,
			Kind:      img.Kind,
			MimeType:  img.MimeType,
			SizeBytes: img.SizeBytes,
			CreatedAt: img.CreatedAt.Unix(),
		}
		if img.ThumbPath != "" {
			r.Thumb = "/img/" + img.ThumbPath
		}
		if img.Width.Valid {
			r.Width = img.Width.Int64
		}
		if img.Height.Valid {
			r.Height = img.Height.Int64
		}
		out = append(out, r)
	}
	return jsonResponse(map[string]any{
		"images": out,
		"limit":  args.Limit,
		"offset": args.Offset,
	})
}

// entryStatusLabel maps the Go constant to the public label MCP tools
// expose. Kept local so the content package's admin-only labels
// (公開 / 下書き / 非公開) don't leak into a JSON API.
func entryStatusLabel(s domain.EntryStatus) string {
	switch s {
	case domain.EntryPublished:
		return "published"
	case domain.EntryDraft:
		return "draft"
	case domain.EntryClosed:
		return "closed"
	}
	return "unknown"
}

func jsonResponse(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
