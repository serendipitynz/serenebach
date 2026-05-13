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

	entry, weblog, ok := h.loadCommentTarget(w, r, chi.URLParam(r, "key"))
	if !ok {
		return
	}
	// Preserve the URL key the user submitted from so the redirect lands
	// on the same canonical surface (slug when present, id otherwise).
	siteBack := root(r) + "/entry/" + entryKeyFor(entry) + "/"
	redirectBack := makeCommentRedirect(w, r, siteBack)

	fields, ok := h.screenCommentSubmission(w, r, weblog, redirectBack)
	if !ok {
		return
	}

	ip := h.clientIP(r)
	if ip != "" {
		if n, err := h.Store.CountRecentCommentsFromIP(ctx, ip, commentRateWindow); err == nil && n >= commentRateLimit {
			redirectBack(tr(weblog, r, "comment.error.rateLimit"))
			return
		}
	}

	status := resolveMessageStatus(ctx, h, weblog.CommentMode, fields.email)
	msg := domain.Message{
		WID:         h.WID,
		EntryID:     entry.ID,
		Status:      status,
		PostedAt:    time.Now(),
		AuthorName:  fields.name,
		AuthorEmail: fields.email,
		AuthorURL:   fields.url,
		Body:        fields.body,
		IPAddress:   ip,
		UserAgent:   r.UserAgent(),
	}
	if _, err := h.Store.CreateMessage(ctx, msg); err != nil {
		log.Printf("public.commentSubmit: create: %v", err)
		http.Error(w, "failed to save comment", http.StatusInternalServerError)
		return
	}

	applyCommenterCookies(w, r, fields)

	// Successful submit: drop back to the entry page. Using SeeOther (303)
	// converts the POST into a GET so refreshes don't resend.
	http.Redirect(w, r, root(r)+fmt.Sprintf("/entry/%d/#comments", entry.ID), http.StatusSeeOther)
}

// commentFields is the validated subset of the POST form that downstream
// persistence and cookie writes need. screenCommentSubmission populates
// it after every anti-spam layer has cleared.
type commentFields struct {
	name      string
	email     string
	url       string
	body      string
	setCookie bool
}

// loadCommentTarget resolves the entry-key URL parameter to a published
// entry on a comment-accepting weblog. Every failure mode is wired to
// the appropriate HTTP response so the caller only has to bail on the
// `ok=false` return.
//
// The order of checks matters: per-entry AcceptComments runs *after*
// the publish-status gate so an unpublished entry stays 404 — surfacing
// 403 there would leak the existence of a draft / closed entry to
// unauthenticated readers.
func (h *Handler) loadCommentTarget(w http.ResponseWriter, r *http.Request, key string) (*domain.Entry, *domain.Weblog, bool) {
	ctx := r.Context()
	entry, _, err := h.resolveEntryKey(ctx, key)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return nil, nil, false
		}
		log.Printf("public.commentSubmit: load entry: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return nil, nil, false
	}
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.commentSubmit: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return nil, nil, false
	}
	if weblog.CommentMode == domain.CommentClosed {
		http.Error(w, "comments are closed", http.StatusForbidden)
		return nil, nil, false
	}
	if entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return nil, nil, false
	}
	if !entry.AcceptComments {
		http.Error(w, "comments are closed", http.StatusForbidden)
		return nil, nil, false
	}
	return entry, weblog, true
}

// makeCommentRedirect builds the redirect-back closure used by every
// anti-spam layer in the comment pipeline. An empty reason becomes a
// "silent" redirect (no error fragment), matching the honeypot / IP
// blacklist / spam-word UX that deliberately avoids surfacing why the
// submission was rejected.
func makeCommentRedirect(w http.ResponseWriter, r *http.Request, siteBack string) func(reason string) {
	return func(reason string) {
		target := siteBack
		if reason != "" {
			target += "?err=" + url.QueryEscape(reason) + "#comment-form"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// screenCommentSubmission runs the anti-spam pipeline against the
// posted form: parse, honeypot, IP blacklist, form-lifetime, required
// fields + length, URL scheme allow-list, spam words, Turnstile. When
// any layer rejects, it calls redirectBack with the appropriate reason
// (or "" for the silent-drop variants) and returns ok=false; the caller
// must stop without writing further to w.
func (h *Handler) screenCommentSubmission(w http.ResponseWriter, r *http.Request, weblog *domain.Weblog, redirectBack func(string)) (commentFields, bool) {
	_ = w // redirect helpers own w
	if err := r.ParseForm(); err != nil {
		redirectBack(tr(weblog, r, "comment.error.parseForm"))
		return commentFields{}, false
	}
	// Honeypot — legitimate users can't see or fill this field.
	if strings.TrimSpace(r.PostFormValue("website")) != "" {
		log.Printf("public.commentSubmit: honeypot tripped from %s", h.clientIP(r))
		redirectBack("")
		return commentFields{}, false
	}
	// IP blacklist — silent drop of any client matching a configured
	// block range. Runs before any other anti-spam layer so blocked
	// ranges never touch the DB or the Turnstile API.
	if h.screenCommentBlocklist(weblog, r) {
		redirectBack("")
		return commentFields{}, false
	}
	// Time check — reject submissions that arrive suspiciously soon after
	// the form was rendered.
	if ts, err := strconv.ParseInt(r.PostFormValue("_ts"), 10, 64); err == nil {
		if time.Since(time.Unix(ts, 0)) < minFormLifetime {
			redirectBack(tr(weblog, r, "comment.error.tooFast"))
			return commentFields{}, false
		}
	}

	fields := commentFields{
		name:      strings.TrimSpace(r.PostFormValue("name")),
		email:     strings.TrimSpace(r.PostFormValue("email")),
		url:       strings.TrimSpace(r.PostFormValue("url")),
		body:      strings.TrimSpace(r.PostFormValue("description")),
		setCookie: r.PostFormValue("set_cookie") == "1",
	}
	if reason, ok := validateCommentFields(weblog, r, fields); !ok {
		redirectBack(reason)
		return commentFields{}, false
	}
	// Spam-word check: silent rejection, same UX as honeypot trip.
	if spam.MatchesAny([]string{fields.name, fields.email, fields.url, fields.body}, spam.ParseWords(weblog.SpamWords)) {
		log.Printf("public.commentSubmit: spam word match from %s", h.clientIP(r))
		redirectBack("")
		return commentFields{}, false
	}
	// Turnstile verification. Skipped entirely when not configured.
	if reason, ok := h.screenCommentTurnstile(weblog, r); !ok {
		redirectBack(reason)
		return commentFields{}, false
	}
	return fields, true
}

// screenCommentBlocklist returns true when the request's client IP
// matches a configured blacklist range so the caller can silently drop
// the submission. Empty / misconfigured lists are no-ops.
func (h *Handler) screenCommentBlocklist(weblog *domain.Weblog, r *http.Request) bool {
	blocklist := spam.ParseIPBlocklist(weblog.IPBlacklist)
	if len(blocklist) == 0 {
		return false
	}
	ipAddr := h.clientIP(r)
	if ipAddr == "" || !blocklist.Contains(ipAddr) {
		return false
	}
	log.Printf("public.commentSubmit: ip-blacklist hit from %s", ipAddr)
	return true
}

// validateCommentFields enforces the required / length / URL-scheme
// checks on the parsed form values. Returns the localised error reason
// the caller should pass to redirectBack when ok=false.
func validateCommentFields(weblog *domain.Weblog, r *http.Request, fields commentFields) (reason string, ok bool) {
	if fields.name == "" || fields.body == "" {
		return tr(weblog, r, "comment.error.required"), false
	}
	const maxBodyLen = 5000
	if len(fields.body) > maxBodyLen {
		return tr(weblog, r, "comment.error.tooLong"), false
	}
	// URL allow-list: http / https / mailto only. Rejects
	// javascript:, data:, vbscript:, etc. so a stored URL can never
	// ride an anchor into a browser-executed script.
	if fields.url != "" && !isAllowedCommentURLScheme(fields.url) {
		return tr(weblog, r, "comment.error.badScheme"), false
	}
	return "", true
}

// screenCommentTurnstile runs the Cloudflare Turnstile challenge when
// configured and returns the localised reason + ok=false on either a
// server-side verify error or a failed challenge. When Turnstile is
// not configured it short-circuits to ok=true.
func (h *Handler) screenCommentTurnstile(weblog *domain.Weblog, r *http.Request) (reason string, ok bool) {
	if h.Turnstile == nil || !h.Turnstile.Enabled() {
		return "", true
	}
	token := r.PostFormValue("cf-turnstile-response")
	passed, err := h.Turnstile.Verify(r.Context(), token, h.clientIP(r))
	if err != nil {
		log.Printf("public.commentSubmit: turnstile error: %v", err)
		return tr(weblog, r, "comment.error.turnstileVerify"), false
	}
	if !passed {
		return tr(weblog, r, "comment.error.turnstileFail"), false
	}
	return "", true
}

// applyCommenterCookies persists the visitor's name / email / url so
// the form pre-fills on the next visit, mirroring SB3's behaviour.
// Visitors opt in via the "set_cookie" form checkbox; opting out
// clears any previously stored values so visitors can change machines
// or reset cleanly.
func applyCommenterCookies(w http.ResponseWriter, r *http.Request, fields commentFields) {
	if fields.setCookie {
		setPrefillCookie(w, r, commenterCookieName, fields.name)
		setPrefillCookie(w, r, commenterCookieEmail, fields.email)
		setPrefillCookie(w, r, commenterCookieURL, fields.url)
		return
	}
	clearPrefillCookie(w, commenterCookieName)
	clearPrefillCookie(w, commenterCookieEmail)
	clearPrefillCookie(w, commenterCookieURL)
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
