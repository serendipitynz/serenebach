package admin

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/webhook"
)

// dispatchEntryEvent fires the given entry.* event for an entry that was
// just created, updated, or published. Wraps the lookup of weblog +
// author + categories + tags so call sites only need to hand over the
// entry itself.
func (h *Handler) dispatchEntryEvent(ctx context.Context, event string, entry domain.Entry) {
	if h.Webhooks == nil {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.dispatchEntryEvent: load weblog: %v", err)
		return
	}
	var author domain.User
	if u, err := h.Store.UserByID(ctx, entry.AuthorID); err == nil && u != nil {
		author = *u
	}
	var categoryNames []string
	if cats, err := h.Store.CategoriesByIDs(ctx, []int64{entry.CategoryID}); err == nil {
		if c, ok := cats[entry.CategoryID]; ok {
			categoryNames = []string{c.Name}
		}
	}
	var tagNames []string
	if tags, err := h.Store.TagsByEntry(ctx, entry.ID); err == nil {
		for _, t := range tags {
			tagNames = append(tagNames, t.Name)
		}
	}
	payload := webhook.EntryPayload(*weblog, entry, author, entryAbsoluteURL(*weblog, entry), categoryNames, tagNames, event)
	if err := h.Webhooks.Dispatch(ctx, h.wid(), event, payload); err != nil {
		log.Printf("admin.dispatchEntryEvent: dispatch %s: %v", event, err)
	}
}

// dispatchImageUploaded fires image.uploaded after a successful
// upload. imageURL is the absolute URL readers can resolve the file at.
func (h *Handler) dispatchImageUploaded(ctx context.Context, image domain.Image, imageURL string) {
	if h.Webhooks == nil {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.dispatchImageUploaded: load weblog: %v", err)
		return
	}
	abs := imageURL
	if base := strings.TrimRight(weblog.BaseURL, "/"); base != "" && strings.HasPrefix(imageURL, "/") {
		abs = base + imageURL
	}
	payload := webhook.ImagePayload(*weblog, image, abs)
	if err := h.Webhooks.Dispatch(ctx, h.wid(), webhook.EventImageUploaded, payload); err != nil {
		log.Printf("admin.dispatchImageUploaded: dispatch: %v", err)
	}
}

// dispatchCommentApproved fires the comment.approved event from the
// admin moderation queue. Loads the parent entry + weblog so the
// payload carries the same shape the public-side dispatcher produces.
func (h *Handler) dispatchCommentApproved(ctx context.Context, msg domain.Message) {
	if h.Webhooks == nil {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.dispatchCommentApproved: load weblog: %v", err)
		return
	}
	entry, err := h.Store.EntryByID(ctx, h.wid(), msg.EntryID)
	if err != nil {
		log.Printf("admin.dispatchCommentApproved: load entry: %v", err)
		return
	}
	payload := webhook.CommentPayload(*weblog, msg, entryAbsoluteURL(*weblog, *entry), commentsModerationURL(*weblog), "comment.approved")
	if err := h.Webhooks.Dispatch(ctx, h.wid(), "comment.approved", payload); err != nil {
		log.Printf("admin.dispatchCommentApproved: dispatch: %v", err)
	}
}

// commentsModerationURL returns the admin moderation URL prefixed with
// the weblog's BaseURL when set. Mirrors the public-side helper so the
// payload's admin_url field is consistent across event sources.
func commentsModerationURL(weblog domain.Weblog) string {
	const path = "/admin/comments?status=waiting"
	base := strings.TrimRight(weblog.BaseURL, "/")
	if base == "" {
		return path
	}
	return base + path
}

// entryAbsoluteURL returns the entry's permalink in absolute form,
// joining the weblog's BaseURL with the canonical /entry/<key>/ shape.
// Falls back to the relative path when BaseURL is unset — subscribers
// can still build a full URL from their side, but cron / chat
// integrations won't have a single click-through link in that case.
func entryAbsoluteURL(weblog domain.Weblog, entry domain.Entry) string {
	key := entry.Slug
	if key == "" {
		key = strconv.FormatInt(entry.ID, 10)
	}
	path := "/entry/" + key + "/"
	base := strings.TrimRight(weblog.BaseURL, "/")
	if base == "" {
		return path
	}
	return base + path
}
