package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testBundle(t *testing.T) *Bundle {
	t.Helper()
	return New("ja", map[string]Catalogue{
		"ja": {
			"hello":   "こんにちは",
			"count":   "%d 件",
			"ja_only": "日本語のみ",
		},
		"en": {
			"hello": "Hello",
			"count": "%d items",
		},
	})
}

func TestTReturnsLocaleValue(t *testing.T) {
	b := testBundle(t)
	if got := b.T("en", "hello"); got != "Hello" {
		t.Errorf("T en hello = %q, want Hello", got)
	}
	if got := b.T("ja", "hello"); got != "こんにちは" {
		t.Errorf("T ja hello = %q, want こんにちは", got)
	}
}

func TestTFallsBackToDefaultLocale(t *testing.T) {
	b := testBundle(t)
	// ja_only missing in en — should fall back to ja.
	if got := b.T("en", "ja_only"); got != "日本語のみ" {
		t.Errorf("T en ja_only = %q, want 日本語のみ (ja fallback)", got)
	}
}

func TestTReturnsKeyLiteralForMissing(t *testing.T) {
	b := testBundle(t)
	if got := b.T("en", "nonexistent.key"); got != "nonexistent.key" {
		t.Errorf("T missing = %q, want nonexistent.key (literal)", got)
	}
}

func TestTfFormatsArguments(t *testing.T) {
	b := testBundle(t)
	if got := b.Tf("en", "count", 5); got != "5 items" {
		t.Errorf("Tf en count 5 = %q, want \"5 items\"", got)
	}
	if got := b.Tf("ja", "count", 10); got != "10 件" {
		t.Errorf("Tf ja count 10 = %q, want \"10 件\"", got)
	}
}

func TestResolveHonoursCookie(t *testing.T) {
	b := testBundle(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: LangCookieName, Value: "en"})
	if got := b.Resolve(r); got != "en" {
		t.Errorf("Resolve cookie = %q, want en", got)
	}
}

func TestResolveFallsBackToDefaultOnUnknownCookie(t *testing.T) {
	b := testBundle(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: LangCookieName, Value: "fr"})
	if got := b.Resolve(r); got != "ja" {
		t.Errorf("Resolve unknown cookie = %q, want ja (default)", got)
	}
}

func TestResolveUsesAcceptLanguage(t *testing.T) {
	b := testBundle(t)
	cases := map[string]string{
		"en-US,en;q=0.8,ja;q=0.5": "en",
		"ja,en;q=0.5":             "ja",
		"fr-FR,de;q=0.5":          "ja", // nothing supported → default
		"en-GB":                   "en", // prefix match
	}
	for header, want := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", header)
		if got := b.Resolve(r); got != want {
			t.Errorf("Resolve %q = %q, want %q", header, got, want)
		}
	}
}

func TestMiddlewareInjectsLocale(t *testing.T) {
	b := testBundle(t)
	var seen string
	h := Middleware(b)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = LocaleFrom(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: LangCookieName, Value: "en"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if seen != "en" {
		t.Errorf("middleware seen locale = %q, want en", seen)
	}
}
