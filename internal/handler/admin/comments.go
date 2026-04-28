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

const adminCommentListLimit = 200

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
	Messages []domain.Message
}

func (h *Handler) commentList(w http.ResponseWriter, r *http.Request) {
	filter := parseMessageStatusFilter(r.URL.Query().Get("status"))
	messages, err := h.Store.ListMessagesForAdmin(r.Context(), h.wid(), filter, adminCommentListLimit)
	if err != nil {
		log.Printf("admin.commentList: %v", err)
		http.Error(w, "failed to list comments", http.StatusInternalServerError)
		return
	}
	renderMain(w, r, pageCommentsList, commentsListPageData{
		pageBase: pageBase{
			Title:      tr(r, "comments.title"),
			ActiveMenu: "comments",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Messages: messages,
	})
}

// parseMessageStatusFilter maps the ?status= query value to a MessageStatus
// for the repo layer, or returns a sentinel value that disables the filter.
func parseMessageStatusFilter(raw string) domain.MessageStatus {
	switch raw {
	case "waiting":
		return domain.MessageWaiting
	case "approved":
		return domain.MessageApproved
	case "hidden":
		return domain.MessageHidden
	}
	return domain.MessageStatus(-99)
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
	if err := h.Store.UpdateMessageStatus(r.Context(), h.wid(), id, status); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.commentSetStatus: %v", err)
		http.Error(w, "failed to update comment", http.StatusInternalServerError)
		return
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
	target := "/admin/comments"
	if s := r.Referer(); strings.Contains(s, "/admin/comments") {
		target = s
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
