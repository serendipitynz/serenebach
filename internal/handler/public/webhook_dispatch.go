package public

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/webhook"
)


// dispatchCommentEvent fires the given comment.* event for a freshly
// stored or moderated comment. weblog is passed in because the caller
// already loaded it; entry is needed to build a click-through URL.
func (h *Handler) dispatchCommentEvent(ctx context.Context, event string, weblog domain.Weblog, entry domain.Entry, msg domain.Message) {
	if h.Webhooks == nil {
		return
	}
	entryURL := absoluteEntryURL(weblog, entry)
	adminURL := absoluteAdminCommentsURL(weblog)
	payload := webhook.CommentPayload(weblog, msg, entryURL, adminURL, event)
	if err := h.Webhooks.Dispatch(ctx, h.WID, event, payload); err != nil {
		log.Printf("public.dispatchCommentEvent: dispatch %s: %v", event, err)
	}
}

// absoluteEntryURL returns the entry permalink prefixed with the
// weblog's BaseURL when available. Mirrors entryAbsoluteURL on the
// admin side so subscribers see the same URL shape regardless of which
// path fired the event.
func absoluteEntryURL(weblog domain.Weblog, entry domain.Entry) string {
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

// absoluteAdminCommentsURL returns the moderation-queue URL for the
// weblog, joined onto BaseURL when set. The path is the listing
// filtered to waiting comments — what an operator wants to look at
// when alerted to a freshly received comment.
func absoluteAdminCommentsURL(weblog domain.Weblog) string {
	const path = "/admin/comments?status=waiting"
	base := strings.TrimRight(weblog.BaseURL, "/")
	if base == "" {
		return path
	}
	return base + path
}
