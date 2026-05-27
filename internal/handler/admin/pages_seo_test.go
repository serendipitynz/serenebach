package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// postPageForm builds a form-encoded POST request parsePageForm can read.
func postPageForm(values url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/admin/pages/new", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestParsePageFormSEOFields(t *testing.T) {
	t.Run("captures summary, canonical, noindex", func(t *testing.T) {
		r := postPageForm(url.Values{
			"title":         {"About"},
			"slug":          {"about"},
			"status":        {"1"},
			"summary":       {"  about summary  "},
			"canonical_url": {"https://example.com/about"},
			"noindex":       {"1"},
		})
		got, errMsg := parsePageForm(r, domain.Page{})
		if errMsg != "" {
			t.Fatalf("unexpected error: %q", errMsg)
		}
		if got.Summary != "about summary" {
			t.Errorf("Summary = %q, want %q", got.Summary, "about summary")
		}
		if got.CanonicalURL != "https://example.com/about" {
			t.Errorf("CanonicalURL = %q", got.CanonicalURL)
		}
		if !got.NoIndex {
			t.Error("NoIndex = false, want true")
		}
	})

	t.Run("noindex defaults false when checkbox absent", func(t *testing.T) {
		r := postPageForm(url.Values{"title": {"About"}, "slug": {"about"}, "status": {"1"}})
		got, errMsg := parsePageForm(r, domain.Page{})
		if errMsg != "" {
			t.Fatalf("unexpected error: %q", errMsg)
		}
		if got.NoIndex {
			t.Error("NoIndex = true, want false")
		}
	})

	t.Run("rejects non-absolute canonical URL", func(t *testing.T) {
		r := postPageForm(url.Values{
			"title":         {"About"},
			"slug":          {"about"},
			"status":        {"1"},
			"canonical_url": {"/relative/path"},
		})
		_, errMsg := parsePageForm(r, domain.Page{})
		want := tr(r, "entries.form.error.canonicalInvalid")
		if errMsg != want {
			t.Errorf("error = %q, want %q", errMsg, want)
		}
	})
}
