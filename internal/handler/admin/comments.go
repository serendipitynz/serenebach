package admin

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// adminCommentPageSize is the per-page row count for the admin
// comment list. Aligned with the entry list pager so the two
// moderation surfaces feel consistent.
const adminCommentPageSize = 50

// commentSortColumns lists the sortable columns of the admin comment
// list with the direction used on first click.
var commentSortColumns = []struct {
	Key        string
	DefaultDir string
}{
	{"id", "desc"},
	{"author", "asc"},
	{"status", "asc"},
	{"posted", "desc"},
	{"entry", "desc"},
	{"body", "asc"},
}

func (h *Handler) mountComments(r chi.Router) {
	r.Get("/comments", h.commentList)
	r.Get("/comments/settings", h.commentSettingsForm)
	r.Post("/comments/settings", h.commentSettingsSubmit)
	r.Post("/comments/{id}/approve", h.commentApprove)
	r.Post("/comments/{id}/hide", h.commentHide)
	r.Post("/comments/{id}/delete", h.commentDelete)
}

// ---- settings tab ------------------------------------------------------

type commentSettingsPageData struct {
	pageBase
	Weblog       domain.Weblog
	Error        string
	FlashSuccess string
}

func (h *Handler) commentSettingsForm(w http.ResponseWriter, r *http.Request) {
	h.renderCommentSettings(w, r, "", r.URL.Query().Get("ok") != "")
}

func (h *Handler) commentSettingsSubmit(w http.ResponseWriter, r *http.Request) {
	current, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.commentSettingsSubmit: load: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderCommentSettingsWith(w, r, *current, tr(r, "flash.formParseError"), false)
		return
	}
	mode := domain.CommentMode(strings.TrimSpace(r.PostFormValue("comment_mode")))
	if !mode.Valid() {
		h.renderCommentSettingsWith(w, r, *current, tr(r, "comments.settings.error.modeInvalid"), false)
		return
	}
	current.CommentMode = mode
	spam := r.PostFormValue("spam_words")
	spam = strings.ReplaceAll(spam, "\r\n", "\n")
	spam = strings.Trim(spam, "\n")
	current.SpamWords = spam
	ipList := r.PostFormValue("ip_blacklist")
	ipList = strings.ReplaceAll(ipList, "\r\n", "\n")
	ipList = strings.Trim(ipList, "\n")
	current.IPBlacklist = ipList

	if err := h.Store.UpdateWeblog(r.Context(), *current); err != nil {
		log.Printf("admin.commentSettingsSubmit: save: %v", err)
		h.renderCommentSettingsWith(w, r, *current, tr(r, "flash.saveFailed"), false)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/comments/settings?ok=1", http.StatusFound)
}

func (h *Handler) renderCommentSettings(w http.ResponseWriter, r *http.Request, errMsg string, success bool) {
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.commentSettings: load: %v", err)
		http.Error(w, "failed to load weblog", http.StatusInternalServerError)
		return
	}
	h.renderCommentSettingsWith(w, r, *weblog, errMsg, success)
}

func (h *Handler) renderCommentSettingsWith(w http.ResponseWriter, r *http.Request, weblog domain.Weblog, errMsg string, success bool) {
	data := commentSettingsPageData{
		pageBase: pageBase{
			Title:      tr(r, "comments.settings.title"),
			ActiveMenu: "comments",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Weblog: weblog,
		Error:  errMsg,
	}
	if success {
		data.FlashSuccess = tr(r, "flash.saved")
	}
	renderMain(w, r, pageCommentSettings, data)
}

type commentsListPageData struct {
	pageBase
	Messages    []domain.Message
	Search      string
	StatusRaw   string // "" | "waiting" | "approved" | "hidden" — for filter-tab active state
	SortLinks   map[string]sortLink
	FilterLinks map[string]string // "all"/"waiting"/... -> href preserving q/sort/dir
	Pager       pagerView
	TotalCount  int64
}

func (h *Handler) commentList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statusRaw := q.Get("status")

	search := repo.NormalizeSearch(q.Get("q"))
	sortKey := repo.ParseMessageSortKey(q.Get("sort"))
	sortDir := repo.ParseSortDir(q.Get("dir"))

	listQ := repo.ListMessagesQuery{
		Filter:  parseMessageStatusFilter(statusRaw),
		Search:  search,
		SortBy:  sortKey,
		SortDir: sortDir,
		Limit:   adminCommentPageSize,
	}

	total, err := h.Store.CountMessagesForAdmin(r.Context(), h.wid(), listQ)
	if err != nil {
		log.Printf("admin.commentList: count: %v", err)
		http.Error(w, "failed to list comments", http.StatusInternalServerError)
		return
	}
	page, totalPages, offset := listPagination(q.Get("page"), total, adminCommentPageSize)
	listQ.Offset = offset

	messages, err := h.Store.ListMessagesForAdmin(r.Context(), h.wid(), listQ)
	if err != nil {
		log.Printf("admin.commentList: %v", err)
		http.Error(w, "failed to list comments", http.StatusInternalServerError)
		return
	}

	extras := map[string]string{"status": statusRaw}
	state := listURLState{
		BasePath: root(r) + "/admin/comments",
		Search:   search,
		SortKey:  sortKey.String(),
		SortDir:  sortDirString(sortDir),
		Page:     page,
		Extras:   extras,
	}
	sortLinks := make(map[string]sortLink, len(commentSortColumns))
	for _, col := range commentSortColumns {
		sortLinks[col.Key] = sortLink{
			Href:  state.hrefSort(col.Key, col.DefaultDir),
			Class: state.classFor(col.Key),
		}
	}
	// Filter tabs preserve q / sort / dir but flip ?status= (and reset
	// to page 1 since switching tabs invalidates the previous index).
	filterLinks := map[string]string{
		"all":      commentFilterHref(root(r), search, sortKey.String(), sortDirString(sortDir), ""),
		"waiting":  commentFilterHref(root(r), search, sortKey.String(), sortDirString(sortDir), "waiting"),
		"approved": commentFilterHref(root(r), search, sortKey.String(), sortDirString(sortDir), "approved"),
		"hidden":   commentFilterHref(root(r), search, sortKey.String(), sortDirString(sortDir), "hidden"),
	}
	prev, next := pagerNeighbours(page, totalPages)

	renderMain(w, r, pageCommentsList, commentsListPageData{
		pageBase: pageBase{
			Title:      tr(r, "comments.title"),
			ActiveMenu: "comments",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Messages:    messages,
		Search:      search,
		StatusRaw:   statusRaw,
		SortLinks:   sortLinks,
		FilterLinks: filterLinks,
		Pager: pagerView{
			Page:       page,
			TotalPages: totalPages,
			PrevHref:   state.hrefPage(prev),
			NextHref:   state.hrefPage(next),
		},
		TotalCount: total,
	})
}

// commentFilterHref builds the URL for a filter tab. The tabs sit
// outside the listURLState mechanism because they themselves change
// the Extras value; reusing listURLState would force a redundant
// per-tab listURLState construction.
func commentFilterHref(basePath, search, sortKey, sortDir, status string) string {
	state := listURLState{
		BasePath: basePath + "/admin/comments",
		Search:   search,
		SortKey:  sortKey,
		SortDir:  sortDir,
		Extras:   map[string]string{"status": status},
	}
	return state.encode(state)
}

// parseMessageStatusFilter maps the ?status= query value to a pointer
// for ListMessagesQuery.Filter. Empty / unknown values return nil so
// the repo layer renders "no status filter".
func parseMessageStatusFilter(raw string) *domain.MessageStatus {
	var s domain.MessageStatus
	switch raw {
	case "waiting":
		s = domain.MessageWaiting
	case "approved":
		s = domain.MessageApproved
	case "hidden":
		s = domain.MessageHidden
	default:
		return nil
	}
	return &s
}

func (h *Handler) commentApprove(w http.ResponseWriter, r *http.Request) {
	h.commentSetStatus(w, r, domain.MessageApproved)
}

func (h *Handler) commentHide(w http.ResponseWriter, r *http.Request) {
	h.commentSetStatus(w, r, domain.MessageHidden)
}

func (h *Handler) commentSetStatus(w http.ResponseWriter, r *http.Request, status domain.MessageStatus) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	// Snapshot the previous status before flipping so the webhook
	// dispatcher only fires on transitions into "approved" (not, say,
	// when an already-approved comment is re-saved).
	prev, prevErr := h.Store.MessageByID(r.Context(), h.wid(), id)
	if err := h.Store.UpdateMessageStatus(r.Context(), h.wid(), id, status); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.commentSetStatus: %v", err)
		http.Error(w, "failed to update comment", http.StatusInternalServerError)
		return
	}
	if prevErr == nil && prev != nil && status == domain.MessageApproved && prev.Status != domain.MessageApproved {
		// The dispatch payload must reflect the post-transition state
		// ("approved"), not the pre-update snapshot — otherwise the
		// payload would report status "waiting" or "hidden" for an
		// approve event, inconsistent with the public auto-approval
		// path. Copy first so we don't mutate the snapshot that
		// other code paths might read later.
		approved := *prev
		approved.Status = domain.MessageApproved
		h.dispatchCommentApproved(r.Context(), approved)
	}
	redirectBackToCommentList(w, r)
}

func (h *Handler) commentDelete(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	if u == nil || !u.CanDeleteComment() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteMessage(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.commentDelete: %v", err)
		http.Error(w, "failed to delete comment", http.StatusInternalServerError)
		return
	}
	redirectBackToCommentList(w, r)
}

// redirectBackToCommentList preserves the ?status= filter the admin was
// viewing when they clicked an action.
func redirectBackToCommentList(w http.ResponseWriter, r *http.Request) {
	target := root(r) + "/admin/comments"
	if s := r.Referer(); strings.Contains(s, "/admin/comments") {
		target = s
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
