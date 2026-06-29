package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// authorIDForCtx returns the user id that newly-created or updated
// entries should be attributed to, sourced from the token's bound
// author (HTTP transport) or the seed admin (stdio transport, which
// has no token row). Zero would indicate a plumbing bug — fall back
// to 1 so the write still lands attributable to *someone*.
func authorIDForCtx(ctx context.Context) int64 {
	if a := authFromContext(ctx); a.AuthorID > 0 {
		return a.AuthorID
	}
	return 1
}

// createEntryArgs mirrors the JSON schema declared in toolDescriptors.
// Pointer types on the optional strings let us distinguish "omitted"
// from "explicit empty" on update_entry — unset keeps the current
// value, explicit empty clears it.
type createEntryArgs struct {
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	More           *string  `json:"more"`
	Slug           *string  `json:"slug"`
	Keywords       *string  `json:"keywords"`
	Format         *string  `json:"format"`
	Status         *string  `json:"status"`
	CategoryID     *int64   `json:"category_id"`
	Tags           []string `json:"tags"`
	PostedAt       *string  `json:"posted_at"`
	Pinned         *bool    `json:"pinned"`
	AcceptComments *bool    `json:"accept_comments"`
}

func (s *Server) toolCreateEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	var args createEntryArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if strings.TrimSpace(args.Title) == "" {
		return "", errors.New("title is required")
	}

	status, err := parseEntryStatus(args.Status, domain.EntryDraft)
	if err != nil {
		return "", err
	}
	slug, err := normaliseSlug(args.Slug)
	if err != nil {
		return "", err
	}
	format := "html"
	if args.Format != nil && *args.Format != "" {
		format = *args.Format
	}
	postedAt, err := parsePostedAt(args.PostedAt, time.Now())
	if err != nil {
		return "", err
	}

	entry := domain.Entry{
		WID:            s.WID,
		AuthorID:       authorIDForCtx(ctx),
		CategoryID:     derefInt64(args.CategoryID, 0),
		Title:          args.Title,
		Slug:           slug,
		Keywords:       derefString(args.Keywords, ""),
		Body:           args.Body,
		More:           derefString(args.More, ""),
		Format:         format,
		Status:         status,
		PostedAt:       postedAt,
		Pinned:         args.Pinned != nil && *args.Pinned,
		AcceptComments: args.AcceptComments == nil || *args.AcceptComments,
	}
	id, err := s.Store.CreateEntry(ctx, entry)
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			return "", fmt.Errorf("slug %q already in use", slug)
		}
		return "", err
	}

	if args.Tags != nil {
		if err := s.applyTags(ctx, id, args.Tags); err != nil {
			// Entry already exists — surface the tag error but don't try to
			// roll back; the human reviewer can fix tags from the admin UI.
			return "", fmt.Errorf("entry %d created, but tag sync failed: %w", id, err)
		}
	}
	s.auditWrite(ctx, "create_entry", id)
	return s.entryPayload(ctx, id)
}

type updateEntryArgs struct {
	ID             int64    `json:"id"`
	Title          *string  `json:"title"`
	Body           *string  `json:"body"`
	More           *string  `json:"more"`
	Slug           *string  `json:"slug"`
	Keywords       *string  `json:"keywords"`
	Format         *string  `json:"format"`
	Status         *string  `json:"status"`
	CategoryID     *int64   `json:"category_id"`
	Tags           []string `json:"tags"`
	PostedAt       *string  `json:"posted_at"`
	Pinned         *bool    `json:"pinned"`
	AcceptComments *bool    `json:"accept_comments"`
}

func (s *Server) toolUpdateEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	var args updateEntryArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.ID <= 0 {
		return "", errors.New("id is required")
	}
	existing, err := s.Store.EntryByID(ctx, s.WID, args.ID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return "", fmt.Errorf("entry %d not found", args.ID)
		}
		return "", err
	}

	updated, err := applyEntryUpdates(*existing, args)
	if err != nil {
		return "", err
	}

	if err := s.Store.UpdateEntry(ctx, updated); err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			return "", fmt.Errorf("slug %q already in use", updated.Slug)
		}
		if errors.Is(err, repo.ErrNotFound) {
			return "", fmt.Errorf("entry %d not found", args.ID)
		}
		return "", err
	}
	if args.Tags != nil {
		if err := s.applyTags(ctx, updated.ID, args.Tags); err != nil {
			return "", fmt.Errorf("entry %d updated, but tag sync failed: %w", updated.ID, err)
		}
	}
	s.auditWrite(ctx, "update_entry", updated.ID)
	return s.entryPayload(ctx, updated.ID)
}

type publishEntryArgs struct {
	ID       int64   `json:"id"`
	PostedAt *string `json:"posted_at"`
}

func (s *Server) toolPublishEntry(ctx context.Context, raw json.RawMessage) (string, error) {
	var args publishEntryArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if args.ID <= 0 {
		return "", errors.New("id is required")
	}
	existing, err := s.Store.EntryByID(ctx, s.WID, args.ID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return "", fmt.Errorf("entry %d not found", args.ID)
		}
		return "", err
	}
	updated := *existing
	updated.Status = domain.EntryPublished
	if args.PostedAt != nil {
		t, err := parsePostedAt(args.PostedAt, existing.PostedAt)
		if err != nil {
			return "", err
		}
		updated.PostedAt = t
	}
	if err := s.Store.UpdateEntry(ctx, updated); err != nil {
		return "", err
	}
	s.auditWrite(ctx, "publish_entry", updated.ID)
	return s.entryPayload(ctx, updated.ID)
}

type uploadImageArgs struct {
	Data     string `json:"data"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
}

// toolUploadImage accepts base64-encoded bytes and persists them through
// the same images.Store + repo.CreateImage path the admin upload form
// uses. Decoding loosely: stdlib base64 variants all land through
// RawStdEncoding / StdEncoding — try StdEncoding first and fall back so
// callers that strip padding still work.
func (s *Server) toolUploadImage(ctx context.Context, raw json.RawMessage) (string, error) {
	if s.ImageStore == nil {
		return "", errors.New("upload_image: server has no image store configured")
	}
	var args uploadImageArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	if strings.TrimSpace(args.Data) == "" {
		return "", errors.New("data is required (base64-encoded image bytes)")
	}

	decoded, err := decodeBase64Flexible(args.Data)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(decoded) == 0 {
		return "", errors.New("decoded image is empty")
	}

	mime, err := resolveUploadMIME(args.MimeType, decoded)
	if err != nil {
		return "", err
	}

	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		filename = "image." + images.ExtensionFor(mime)
	}

	stored, err := s.ImageStore.SaveUpload(bytes.NewReader(decoded), filename, mime, time.Now())
	if err != nil {
		return "", fmt.Errorf("save upload: %w", err)
	}

	uploader := authorIDForCtx(ctx)
	img := domain.Image{
		WID:        s.WID,
		UploadedBy: uploader,
		Kind:       stored.Kind,
		Filename:   stored.Filename,
		StoredPath: stored.StoredPath,
		ThumbPath:  stored.ThumbPath,
		MimeType:   mime,
		SizeBytes:  stored.SizeBytes,
	}
	if stored.Width > 0 {
		img.Width = sql.NullInt64{Int64: int64(stored.Width), Valid: true}
	}
	if stored.Height > 0 {
		img.Height = sql.NullInt64{Int64: int64(stored.Height), Valid: true}
	}
	id, err := s.Store.CreateImage(ctx, img)
	if err != nil {
		// Best-effort: clean up the orphaned file so repeated failures
		// don't grow the disk.
		s.ImageStore.DeleteFiles(stored.StoredPath, stored.ThumbPath)
		return "", fmt.Errorf("create image row: %w", err)
	}

	s.auditWrite(ctx, "upload_image", id)

	resp := map[string]any{
		"id":          id,
		"filename":    stored.Filename,
		"stored_path": stored.StoredPath,
		"url":         "/img/" + stored.StoredPath,
		"mime_type":   mime,
		"size_bytes":  stored.SizeBytes,
		"width":       stored.Width,
		"height":      stored.Height,
	}
	if stored.ThumbPath != "" {
		resp["thumb_path"] = stored.ThumbPath
		resp["thumb_url"] = "/img/" + stored.ThumbPath
	}
	return jsonResponse(resp)
}

// resolveUploadMIME determines the MIME for an upload_image call: the
// caller-supplied value when present, otherwise sniffed from the decoded
// bytes. It rejects anything outside the image whitelist so the tool
// handler stays linear.
func resolveUploadMIME(declared string, decoded []byte) (string, error) {
	mime := strings.TrimSpace(declared)
	if mime == "" {
		// http.DetectContentType returns values like "image/jpeg; charset=..."
		// — split off the parameters before the whitelist check.
		sniffed := http.DetectContentType(decoded)
		if i := strings.IndexByte(sniffed, ';'); i >= 0 {
			sniffed = sniffed[:i]
		}
		mime = strings.TrimSpace(sniffed)
	}
	if !images.AllowedMIMEs[mime] {
		return "", fmt.Errorf("unsupported mime %q (accepted: image/jpeg, image/png, image/gif, image/webp)", mime)
	}
	if images.KindFor(mime) != domain.KindImage {
		return "", fmt.Errorf("upload_image accepts only image MIMEs (jpeg, png, gif, webp); got %q", mime)
	}
	return mime, nil
}

// decodeBase64Flexible tolerates padded + raw base64 variants since MCP
// clients vary in what they emit — pad/no-pad, std/url. Whitespace is
// stripped first so multi-line payloads copy-pasted from a terminal
// round-trip cleanly.
func decodeBase64Flexible(raw string) ([]byte, error) {
	stripped := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, raw)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if out, err := enc.DecodeString(stripped); err == nil {
			return out, nil
		}
	}
	return nil, errors.New("not a valid base64 string")
}

// --- helpers ------------------------------------------------------------

func (s *Server) applyTags(ctx context.Context, entryID int64, names []string) error {
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			cleaned = append(cleaned, n)
		}
	}
	if len(cleaned) == 0 {
		return s.Store.SetEntryTags(ctx, entryID, nil)
	}
	tags, err := s.Store.EnsureTagsByName(ctx, s.WID, cleaned)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(tags))
	for _, t := range tags {
		ids = append(ids, t.ID)
	}
	return s.Store.SetEntryTags(ctx, entryID, ids)
}

// entryPayload reloads the row after write so the caller sees the
// DB's canonical view (timestamps, normalised fields) rather than
// echoing back the input struct.
func (s *Server) entryPayload(ctx context.Context, id int64) (string, error) {
	entry, err := s.Store.EntryByID(ctx, s.WID, id)
	if err != nil {
		return "", err
	}
	tags, _ := s.Store.TagsByEntry(ctx, id)
	tagNames := make([]string, 0, len(tags))
	for _, t := range tags {
		tagNames = append(tagNames, t.Name)
	}
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
		"tags":        tagNames,
	})
}

// applyEntryUpdates merges the partial update represented by args into a
// copy of entry. Any field present (non-nil pointer) on args wins; fields
// that need parsing (slug / status / posted_at) bail out with a non-nil
// error so the caller can surface the validation message before the
// repo write.
func applyEntryUpdates(entry domain.Entry, args updateEntryArgs) (domain.Entry, error) {
	applyEntrySimpleFields(&entry, args)
	if err := applyEntryParsedFields(&entry, args); err != nil {
		return entry, err
	}
	return entry, nil
}

// applyEntrySimpleFields copies every field where the contract is just
// "non-nil pointer → use this value". Format is the one exception that
// also rejects an empty string, to avoid wiping the format hint when a
// caller passes "" by accident.
func applyEntrySimpleFields(entry *domain.Entry, args updateEntryArgs) {
	if args.Title != nil {
		entry.Title = *args.Title
	}
	if args.Body != nil {
		entry.Body = *args.Body
	}
	if args.More != nil {
		entry.More = *args.More
	}
	if args.Keywords != nil {
		entry.Keywords = *args.Keywords
	}
	if args.Format != nil && *args.Format != "" {
		entry.Format = *args.Format
	}
	if args.CategoryID != nil {
		entry.CategoryID = *args.CategoryID
	}
	if args.Pinned != nil {
		entry.Pinned = *args.Pinned
	}
	if args.AcceptComments != nil {
		entry.AcceptComments = *args.AcceptComments
	}
}

// applyEntryParsedFields handles the three fields that need parsing /
// validation before being assigned: slug, status, posted_at. Any
// failure is surfaced verbatim so the caller can return it as a
// validation error to the MCP client.
func applyEntryParsedFields(entry *domain.Entry, args updateEntryArgs) error {
	if args.Slug != nil {
		slug, err := normaliseSlug(args.Slug)
		if err != nil {
			return err
		}
		entry.Slug = slug
	}
	if args.Status != nil {
		status, err := parseEntryStatus(args.Status, entry.Status)
		if err != nil {
			return err
		}
		entry.Status = status
	}
	if args.PostedAt != nil {
		t, err := parsePostedAt(args.PostedAt, entry.PostedAt)
		if err != nil {
			return err
		}
		entry.PostedAt = t
	}
	return nil
}

func parseEntryStatus(raw *string, fallback domain.EntryStatus) (domain.EntryStatus, error) {
	if raw == nil || *raw == "" {
		return fallback, nil
	}
	switch *raw {
	case "draft":
		return domain.EntryDraft, nil
	case "published":
		return domain.EntryPublished, nil
	case "closed":
		return domain.EntryClosed, nil
	}
	return 0, fmt.Errorf("invalid status %q (expected draft / published / closed)", *raw)
}

func normaliseSlug(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return "", nil
	}
	if !domain.IsValidSlug(s) {
		return "", fmt.Errorf("invalid slug %q (lowercase alphanum + single hyphens only)", s)
	}
	return s, nil
}

func parsePostedAt(raw *string, fallback time.Time) (time.Time, error) {
	if raw == nil || *raw == "" {
		return fallback, nil
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid posted_at %q (expect RFC3339): %w", *raw, err)
	}
	return t, nil
}

func derefString(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func derefInt64(p *int64, fallback int64) int64 {
	if p == nil {
		return fallback
	}
	return *p
}

// auditWrite emits a machine-grepable log line and, when an audit store
// is configured, persists one row to mcp_audit_log so the admin UI can
// render "who did what when" without scraping the process log. Insert
// failures are logged only — audit is observational, never a gate on
// the mutation itself.
func (s *Server) auditWrite(ctx context.Context, tool string, id int64) {
	auth := authFromContext(ctx)
	log.Printf("mcp.write: tool=%s id=%d token=%d author=%d", tool, id, auth.TokenID, auth.AuthorID)

	if s.Audit == nil {
		return
	}
	if _, err := s.Audit.Insert(ctx, mcpaudit.Entry{
		WID:      s.WID,
		TokenID:  auth.TokenID,
		AuthorID: auth.AuthorID,
		Tool:     tool,
		TargetID: id,
	}); err != nil {
		log.Printf("mcp.write: audit insert: %v", err)
	}
}
