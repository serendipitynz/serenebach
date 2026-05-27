package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// postForm builds a form-encoded POST request parseEntryForm can read.
func postForm(values url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/admin/entries/new", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestParseEntryFormSEOFields(t *testing.T) {
	h, _ := newAdminTestHandler(t)

	t.Run("captures summary, canonical, noindex", func(t *testing.T) {
		r := postForm(url.Values{
			"title":         {"Post"},
			"status":        {"1"},
			"summary":       {"  trimmed summary  "},
			"canonical_url": {"https://example.com/original"},
			"noindex":       {"1"},
		})
		got, errMsg := h.parseEntryForm(r, domain.Entry{})
		if errMsg != "" {
			t.Fatalf("unexpected error: %q", errMsg)
		}
		if got.Summary != "trimmed summary" {
			t.Errorf("Summary = %q, want %q", got.Summary, "trimmed summary")
		}
		if got.CanonicalURL != "https://example.com/original" {
			t.Errorf("CanonicalURL = %q", got.CanonicalURL)
		}
		if !got.NoIndex {
			t.Error("NoIndex = false, want true")
		}
	})

	t.Run("noindex defaults false when checkbox absent", func(t *testing.T) {
		r := postForm(url.Values{"title": {"Post"}, "status": {"1"}})
		got, errMsg := h.parseEntryForm(r, domain.Entry{})
		if errMsg != "" {
			t.Fatalf("unexpected error: %q", errMsg)
		}
		if got.NoIndex {
			t.Error("NoIndex = true, want false")
		}
	})

	t.Run("rejects non-absolute canonical URL", func(t *testing.T) {
		r := postForm(url.Values{
			"title":         {"Post"},
			"status":        {"1"},
			"canonical_url": {"/relative/path"},
		})
		_, errMsg := h.parseEntryForm(r, domain.Entry{})
		want := tr(r, "entries.form.error.canonicalInvalid")
		if errMsg != want {
			t.Errorf("error = %q, want %q", errMsg, want)
		}
	})

	t.Run("empty canonical URL is allowed", func(t *testing.T) {
		r := postForm(url.Values{"title": {"Post"}, "status": {"1"}, "canonical_url": {""}})
		got, errMsg := h.parseEntryForm(r, domain.Entry{})
		if errMsg != "" {
			t.Fatalf("unexpected error: %q", errMsg)
		}
		if got.CanonicalURL != "" {
			t.Errorf("CanonicalURL = %q, want empty", got.CanonicalURL)
		}
	})
}
