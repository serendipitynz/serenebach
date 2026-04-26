package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestZeroResolverIgnoresForwardedHeaders(t *testing.T) {
	// Default deployment: no proxy list configured. Even when the client
	// sends X-Forwarded-For, the handler must see RemoteAddr.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")

	if got := (Resolver{}).From(r); got != "203.0.113.5" {
		t.Errorf("From = %q, want 203.0.113.5 (forwarded header must be ignored)", got)
	}
}

func TestTrustedPeerHonoursXForwardedFor(t *testing.T) {
	resolver, err := Parse("10.0.0.0/8")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:443"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.5")

	if got := resolver.From(r); got != "198.51.100.9" {
		t.Errorf("From = %q, want leftmost XFF entry 198.51.100.9", got)
	}
}

func TestTrustedPeerFallsBackToXRealIP(t *testing.T) {
	resolver, _ := Parse("127.0.0.1")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Real-IP", "198.51.100.42")

	if got := resolver.From(r); got != "198.51.100.42" {
		t.Errorf("From = %q, want 198.51.100.42 from X-Real-IP", got)
	}
}

func TestUntrustedPeerDoesNotPromoteForgedHeader(t *testing.T) {
	// The proxy list trusts the loopback only, so a direct attacker on
	// the public internet cannot use XFF to forge a "trusted" address.
	resolver, _ := Parse("127.0.0.1/32")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1") // attacker-controlled
	r.Header.Set("X-Real-IP", "10.0.0.1")

	if got := resolver.From(r); got != "203.0.113.5" {
		t.Errorf("From = %q, want 203.0.113.5 (untrusted peer must ignore forwarded headers)", got)
	}
}

func TestParseAcceptsBareAddressesAndCIDRMix(t *testing.T) {
	// Mix of bare v4, v4 CIDR, and bare v6 — bare addresses become
	// /32 or /128 so operators don't have to remember the suffix.
	resolver, err := Parse(" 127.0.0.1, 10.0.0.0/8 , ::1 ")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cases := []struct {
		remote string
		trust  bool
	}{
		{"127.0.0.1:1", true},
		{"10.5.5.5:1", true},
		{"[::1]:1", true},
		{"203.0.113.5:1", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-Forwarded-For", "198.51.100.9")
		got := resolver.From(req)
		// When trusted, the forwarded header takes over; when not, the
		// peer's own host wins. Use that to assert trust state.
		if tc.trust && got != "198.51.100.9" {
			t.Errorf("RemoteAddr=%s: expected to trust peer, got %q", tc.remote, got)
		}
		if !tc.trust && got == "198.51.100.9" {
			t.Errorf("RemoteAddr=%s: expected NOT to trust peer", tc.remote)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse("not-an-ip"); err == nil {
		t.Errorf("Parse: want error for garbage input")
	}
	if _, err := Parse("10.0.0.0/99"); err == nil {
		t.Errorf("Parse: want error for invalid CIDR")
	}
}

func TestParseEmptyIsZeroResolver(t *testing.T) {
	r, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(r.trusted) != 0 {
		t.Errorf("empty input must yield zero Resolver, got %d entries", len(r.trusted))
	}
}
