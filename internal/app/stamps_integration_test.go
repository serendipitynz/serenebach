package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/csrf"
)

func postStamp(t *testing.T, h http.Handler, entryID int64, kind string, extraCookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	form := url.Values{
		"csrf_token": {token},
		"kind":       {kind},
	}
	req := httptest.NewRequest("POST", "/entry/"+itoa64(entryID)+"/stamp", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.Header.Set("User-Agent", "Mozilla/5.0 (stamps)")
	req.RemoteAddr = "192.168.0.1:1"
	req.AddCookie(csrfCookie)
	for _, c := range extraCookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestStampPostIncrementsCounter(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	h := a.Handler()

	w := postStamp(t, h, 1, "heart")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body:\n%s", w.Code, w.Body.String())
	}
	var total int64
	_ = a.DB.QueryRow(`SELECT stamps_count FROM entries WHERE id = 1`).Scan(&total)
	if total != 1 {
		t.Errorf("stamps_count = %d, want 1", total)
	}
	// Repeat same (entry, kind, fingerprint) — should NOT double-count.
	_ = postStamp(t, h, 1, "heart")
	var total2 int64
	_ = a.DB.QueryRow(`SELECT stamps_count FROM entries WHERE id = 1`).Scan(&total2)
	if total2 != 1 {
		t.Errorf("after second POST, stamps_count = %d, want 1", total2)
	}
}

func TestStampPostAcceptsDifferentKindFromSameFingerprint(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	h := a.Handler()

	_ = postStamp(t, h, 1, "heart")
	_ = postStamp(t, h, 1, "laugh")

	var total int64
	_ = a.DB.QueryRow(`SELECT stamps_count FROM entries WHERE id = 1`).Scan(&total)
	if total != 2 {
		t.Errorf("stamps_count = %d, want 2 (heart + laugh from same fingerprint)", total)
	}
}

func TestStampPostRejectsInvalidKind(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token": {token},
		"kind":       {"trash"},
	}
	req := httptest.NewRequest("POST", "/entry/1/stamp", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestEntryPermalinkExposesStampTags(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Pre-seed a couple of stamps directly so the permalink has
	// something to reflect in the per-kind counts.
	if _, err := a.DB.Exec(`
		INSERT INTO entry_stamps (entry_id, stamp_kind, fingerprint, created_at)
		VALUES (1, 'heart', 'fp-a', 1), (1, 'heart', 'fp-b', 2), (1, 'laugh', 'fp-c', 3)`); err != nil {
		t.Fatal(err)
	}
	// Prime the denormalised counter too so the hot-path tag matches.
	if _, err := a.DB.Exec(`UPDATE entries SET stamps_count = 3 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	// Inject a stamp-aware template so the tags render into the output.
	body := "<html><body>total={entry_stamps_count} heart={entry_stamps_heart} laugh={entry_stamps_laugh} url={entry_stamp_url}\n<!-- BEGIN entry -->\n{entry_title}\n<!-- END entry -->\n</body></html>"
	form := url.Values{
		"main_body": {body},
		"css":       {""},
	}
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)
	if w := authedPOSTForm(t, a.Handler(), "/admin/templates/"+itoa64(activeID)+"/edit", form, cookies); w.Code != http.StatusFound {
		t.Fatalf("template save status = %d; body:\n%s", w.Code, w.Body.String())
	}

	pub := authedGET(t, a.Handler(), "/entry/1/", cookies).Body.String()
	for _, want := range []string{
		"total=3",
		"heart=2",
		"laugh=1",
		"url=/entry/1/stamp",
	} {
		if !strings.Contains(pub, want) {
			t.Errorf("permalink missing %q; got:\n%s", want, pub)
		}
	}
}

func TestAnalyticsTopEntriesSortByLikes(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Nudge engagement: entry 2 has more likes, entry 1 has more stamps.
	if _, err := a.DB.Exec(`UPDATE entries SET likes_count = 5 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE entries SET likes_count = 99, stamps_count = 0 WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE entries SET stamps_count = 99 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Likes sort → entry 2 first.
	likes := authedGET(t, a.Handler(), "/admin/analytics?sort=likes", cookies).Body.String()
	idx1 := strings.Index(likes, `/entry/1/`)
	idx2 := strings.Index(likes, `/entry/2/`)
	if idx2 == -1 || (idx1 != -1 && idx1 < idx2) {
		t.Errorf("likes sort should list entry 2 before entry 1; indices %d vs %d", idx1, idx2)
	}

	// Stamps sort → entry 1 first.
	stamps := authedGET(t, a.Handler(), "/admin/analytics?sort=stamps", cookies).Body.String()
	idx1 = strings.Index(stamps, `/entry/1/`)
	idx2 = strings.Index(stamps, `/entry/2/`)
	if idx1 == -1 || (idx2 != -1 && idx2 < idx1) {
		t.Errorf("stamps sort should list entry 1 before entry 2; indices %d vs %d", idx1, idx2)
	}
}

var _ = csrf.CookieName // keep import in the test binary
