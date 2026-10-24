package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsPublicAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":        false,
		"10.1.2.3":         false,
		"172.16.0.1":       false,
		"192.168.1.10":     false,
		"169.254.169.254":  false, // cloud metadata
		"100.100.100.200":  false, // metadata in the shared address space
		"0.1.2.3":          false, // reaches the local host on Linux
		"255.255.255.255":  false,
		"::1":              false,
		"fe80::1":          false,
		"fd00::1":          false,
		"::ffff:127.0.0.1": false, // IPv4-mapped loopback
		"64:ff9b::a00:1":   false, // NAT64 for 10.0.0.1
		"8.8.8.8":          true,
		"1.1.1.1":          true,
		"2606:4700::1111":  true,
	}
	for s, want := range cases {
		if got := isPublicAddr(netip.MustParseAddr(s)); got != want {
			t.Errorf("isPublicAddr(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestPublicOnlyClientRefusesLoopbackByNameAndAddress(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	// "localhost" checks the address after name resolution, not the URL text.
	for _, target := range []string{srv.URL, "http://localhost:" + u.Port()} {
		resp, err := PublicOnlyClient().Get(target)
		if err == nil {
			_ = resp.Body.Close()
		}
		if !errors.Is(err, ErrDestinationBlocked) {
			t.Errorf("GET %s err = %v, want ErrDestinationBlocked", target, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// The check sits in the dialler, so a redirect to a refused address is
// stopped at its own dial. Both test servers are on loopback, so the
// predicate allows only the first dial (to the redirecting server) and
// refuses every later one; with keep-alives off the redirect must dial.
func TestDialCheckRunsOnRedirectHops(t *testing.T) {
	var innerHits atomic.Int32
	inner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { innerHits.Add(1) }))
	defer inner.Close()
	outer := httptest.NewServer(http.RedirectHandler(inner.URL, http.StatusFound))
	defer outer.Close()

	var dials atomic.Int32
	client := dialCheckedClient(func(netip.Addr) bool { return dials.Add(1) == 1 })
	client.Transport.(*http.Transport).DisableKeepAlives = true

	resp, err := client.Get(outer.URL)
	if err == nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, ErrDestinationBlocked) {
		t.Fatalf("GET via redirect err = %v, want ErrDestinationBlocked", err)
	}
	if n := innerHits.Load(); n != 0 {
		t.Errorf("redirect target received %d requests, want 0", n)
	}
}

func TestProviderSurfacesBlockedDestination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	p, err := New(Config{Kind: KindOpenAICompat, BaseURL: srv.URL, Model: "m"}, PublicOnlyClient())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(context.Background(), Request{Prompt: "hi", MaxTokens: 1})
	if !errors.Is(err, ErrDestinationBlocked) || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Complete err = %v, want ErrDestinationBlocked naming the address", err)
	}
}
