package public

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/spam"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/turnstile"
)

// minFormLifetime is the shortest time a legitimate visitor could plausibly
// take between loading the form and submitting it. Faster submissions are
// almost certainly bots and we reject them before touching the database.
const minFormLifetime = 3 * time.Second

// commentRateWindow is the sliding window for the IP-based rate limit.
const commentRateWindow = 60 * time.Second

// commentRateLimit is the max comments one IP can post within the window.
const commentRateLimit = 3

func (h *Handler) commentSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.commentSubmit: load entry: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}
	entryID := entry.ID

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.commentSubmit: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	if weblog.CommentMode == domain.CommentClosed {
		http.Error(w, "comments are closed", http.StatusForbidden)
		return
	}
	if entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}

	// Preserve the URL key the user submitted from so the redirect lands
	// on the same canonical surface (slug when present, id otherwise).
	siteBack := root(r) + "/entry/" + entryKeyFor(entry) + "/"
	redirectBack := func(reason string) {
		target := siteBack
		if reason != "" {
			target += "?err=" + url.QueryEscape(reason) + "#comment-form"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}

	if err := r.ParseForm(); err != nil {
		redirectBack(tr(weblog, r, "comment.error.parseForm"))
		return
	}

	// Honeypot — legitimate users can't see or fill this field.
	if strings.TrimSpace(r.PostFormValue("website")) != "" {
		log.Printf("public.commentSubmit: honeypot tripped from %s", h.clientIP(r))
		redirectBack("")
		return
	}

	// IP blacklist — silent drop of any client matching a configured
	// block range. Runs before any other anti-spam layer so blocked
	// ranges never touch the DB or the Turnstile API. Empty /
	// misconfigured lists are no-ops.
	if blocklist := spam.ParseIPBlocklist(weblog.IPBlacklist); len(blocklist) > 0 {
		if ipAddr := h.clientIP(r); ipAddr != "" && blocklist.Contains(ipAddr) {
			log.Printf("public.commentSubmit: ip-blacklist hit from %s", ipAddr)
			redirectBack("")
			return
		}
	}

	// Time check — reject submissions that arrive suspiciously soon after
	// the form was rendered.
	if ts, err := strconv.ParseInt(r.PostFormValue("_ts"), 10, 64); err == nil {
		if time.Since(time.Unix(ts, 0)) < minFormLifetime {
			redirectBack(tr(weblog, r, "comment.error.tooFast"))
			return
		}
	}

	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	urlField := strings.TrimSpace(r.PostFormValue("url"))
	body := strings.TrimSpace(r.PostFormValue("description"))
	if name == "" || body == "" {
		redirectBack(tr(weblog, r, "comment.error.required"))
		return
	}
	const maxBodyLen = 5000
	if len(body) > maxBodyLen {
		redirectBack(tr(weblog, r, "comment.error.tooLong"))
		return
	}
	// URL allow-list: http / https / mailto only. Rejects
	// javascript:, data:, vbscript:, etc. so a stored URL can never
	// ride an anchor into a browser-executed script.
	if urlField != "" && !isAllowedCommentURLScheme(urlField) {
		redirectBack(tr(weblog, r, "comment.error.badScheme"))
		return
	}

	// Spam-word check: silent rejection, same UX as honeypot trip.
	if spam.MatchesAny([]string{name, email, urlField, body}, spam.ParseWords(weblog.SpamWords)) {
		log.Printf("public.commentSubmit: spam word match from %s", h.clientIP(r))
		redirectBack("")
		return
	}

	// Turnstile verification. Skipped entirely when not configured.
	if h.Turnstile != nil && h.Turnstile.Enabled() {
		token := r.PostFormValue("cf-turnstile-response")
		ok, err := h.Turnstile.Verify(ctx, token, h.clientIP(r))
		if err != nil {
			log.Printf("public.commentSubmit: turnstile error: %v", err)
			redirectBack(tr(weblog, r, "comment.error.turnstileVerify"))
			return
		}
		if !ok {
			redirectBack(tr(weblog, r, "comment.error.turnstileFail"))
			return
		}
	}

	ip := h.clientIP(r)
	if ip != "" {
		if n, err := h.Store.CountRecentCommentsFromIP(ctx, ip, commentRateWindow); err == nil && n >= commentRateLimit {
			redirectBack(tr(weblog, r, "comment.error.rateLimit"))
			return
		}
	}

	status := resolveMessageStatus(ctx, h, weblog.CommentMode, email)

	msg := domain.Message{
		WID:         h.WID,
		EntryID:     entry.ID,
		Status:      status,
		PostedAt:    time.Now(),
		AuthorName:  name,
		AuthorEmail: email,
		AuthorURL:   urlField,
		Body:        body,
		IPAddress:   ip,
		UserAgent:   r.UserAgent(),
	}
	if _, err := h.Store.CreateMessage(ctx, msg); err != nil {
		log.Printf("public.commentSubmit: create: %v", err)
		http.Error(w, "failed to save comment", http.StatusInternalServerError)
		return
	}

	// Cookie prefill — save on explicit opt-in, clear on opt-out so visitors
	// can change machines / reset. Classic SB3 behaviour.
	if r.PostFormValue("set_cookie") == "1" {
		setPrefillCookie(w, r, commenterCookieName, name)
		setPrefillCookie(w, r, commenterCookieEmail, email)
		setPrefillCookie(w, r, commenterCookieURL, urlField)
	} else {
		clearPrefillCookie(w, commenterCookieName)
		clearPrefillCookie(w, commenterCookieEmail)
		clearPrefillCookie(w, commenterCookieURL)
	}

	// Successful submit: drop back to the entry page. Using SeeOther (303)
	// converts the POST into a GET so refreshes don't resend.
	http.Redirect(w, r, root(r)+fmt.Sprintf("/entry/%d/#comments", entryID), http.StatusSeeOther)
}

// resolveMessageStatus turns the weblog's CommentMode + the submitter's
// email into a concrete starting status. "open" always publishes; "closed"
// is rejected earlier so never reaches here; "moderated" auto-approves when
// the email has been vetted before ("trust memory") and otherwise queues.
func resolveMessageStatus(ctx context.Context, h *Handler, mode domain.CommentMode, email string) domain.MessageStatus {
	if mode == domain.CommentOpen {
		return domain.MessageApproved
	}
	if email == "" {
		return domain.MessageWaiting
	}
	trusted, err := h.Store.HasApprovedCommentFromEmail(ctx, h.WID, email)
	if err != nil {
		log.Printf("public.commentSubmit: trust lookup: %v", err)
		return domain.MessageWaiting
	}
	if trusted {
		return domain.MessageApproved
	}
	return domain.MessageWaiting
}

// readCookieEscaped pulls one of the commenter-prefill cookies, URL-decodes
// it, and HTML-escapes the result so it can land in a `value="..."`
// attribute without breaking layout or enabling script injection.
func readCookieEscaped(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		raw = c.Value
	}
	return html.EscapeString(raw)
}

func setPrefillCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		Path:     "/",
		MaxAge:   int(commenterCookieTTL.Seconds()),
		HttpOnly: false, // UX cookie — JS may read it to pre-fill a richer editor later.
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func clearPrefillCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// turnstileWidget returns the embed HTML when the injected verifier is
// enabled. Handlers that don't plug in a Turnstile verifier just get "".
func turnstileWidget(v turnstile.Verifier) string {
	if v == nil || !v.Enabled() {
		return ""
	}
	if c, ok := v.(interface{ WidgetHTML() string }); ok {
		return c.WidgetHTML()
	}
	return ""
}

// isAllowedCommentURLScheme accepts only http / https / mailto plus
// the schemeless forms (site-relative / protocol-relative URLs).
// Guards against `javascript:` / `data:` / `vbscript:` etc. being
// stored and later clicked by a reader. The render-time helper
// `safeExternalURL` in internal/content is a belt-and-braces second
// line of defence.
func isAllowedCommentURLScheme(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/") {
		return true
	}
	colon := strings.Index(s, ":")
	if colon < 0 {
		return true // relative — no scheme to worry about
	}
	switch strings.ToLower(s[:colon]) {
	case "http", "https", "mailto":
		return true
	}
	return false
}
