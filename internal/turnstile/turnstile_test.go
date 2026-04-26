package turnstile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDisabledClientAcceptsEverything(t *testing.T) {
	c := New("", "")
	if c.Enabled() {
		t.Fatal("client with empty keys must be disabled")
	}
	ok, err := c.Verify(context.Background(), "irrelevant", "1.2.3.4")
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if !ok {
		t.Errorf("disabled Verify should return true")
	}
	if c.WidgetHTML() != "" {
		t.Errorf("disabled WidgetHTML should be empty; got %q", c.WidgetHTML())
	}
}

func TestVerifyPassesOnSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.PostFormValue("secret") != "SEC" {
			t.Errorf("missing/wrong secret")
		}
		if r.PostFormValue("response") != "TOKEN" {
			t.Errorf("missing/wrong token")
		}
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	c := New("KEY", "SEC")
	c.verifyURL = srv.URL

	ok, err := c.Verify(context.Background(), "TOKEN", "1.2.3.4")
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if !ok {
		t.Errorf("expected success=true")
	}
}

func TestVerifyRejectsOnFailureResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer srv.Close()

	c := New("KEY", "SEC")
	c.verifyURL = srv.URL

	ok, err := c.Verify(context.Background(), "BAD", "")
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if ok {
		t.Errorf("expected success=false")
	}
}

func TestVerifyRejectsEmptyToken(t *testing.T) {
	c := New("KEY", "SEC")
	ok, err := c.Verify(context.Background(), "   ", "")
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if ok {
		t.Errorf("empty token must not pass")
	}
}

func TestWidgetHTMLEmbedsSiteKey(t *testing.T) {
	c := New("SITE123", "SEC")
	html := c.WidgetHTML()
	if html == "" {
		t.Fatal("enabled client should return HTML")
	}
	if !contains(html, `data-sitekey="SITE123"`) {
		t.Errorf("sitekey missing from widget: %q", html)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
