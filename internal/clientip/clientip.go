// Package clientip resolves the originating client IP from an HTTP request
// while only trusting forwarded headers when the immediate peer is on a
// configured trusted-proxy list.
//
// Without this, anyone able to reach the binary directly can spoof the
// X-Forwarded-For / X-Real-IP headers and bypass the IP blacklist, evade
// the comment rate-limit, or impersonate another reader's like/stamp
// fingerprint. Operators behind a known proxy (Cloudflare, nginx,
// Caddy) opt in via SB_TRUSTED_PROXIES; everyone else gets RemoteAddr.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver decides whether forwarded headers may be trusted. The zero
// value trusts nothing — handlers always see the immediate peer's IP.
type Resolver struct {
	trusted []netip.Prefix
}

// Parse reads a comma-separated CIDR / single-address list and returns a
// Resolver. Empty input yields the zero Resolver. A bare address ("10.0.0.1")
// is accepted as a /32 (IPv4) or /128 (IPv6) for convenience.
func Parse(spec string) (Resolver, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Resolver{}, nil
	}
	var prefixes []netip.Prefix
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		p, err := parseEntry(entry)
		if err != nil {
			return Resolver{}, fmt.Errorf("clientip: %q: %w", entry, err)
		}
		prefixes = append(prefixes, p)
	}
	return Resolver{trusted: prefixes}, nil
}

func parseEntry(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		return netip.ParsePrefix(entry)
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits), nil
}

// From returns the client IP for the request. When the immediate peer
// (r.RemoteAddr) is on the trusted list, the leftmost X-Forwarded-For
// entry wins, then X-Real-IP; otherwise RemoteAddr is the answer
// regardless of forwarded headers. Result is always portless and may
// be empty if the request carried no usable address.
func (r Resolver) From(req *http.Request) string {
	peer := remoteHost(req.RemoteAddr)
	if !r.trusts(peer) {
		return peer
	}
	if v := req.Header.Get("X-Forwarded-For"); v != "" {
		if idx := strings.IndexByte(v, ','); idx > 0 {
			v = v[:idx]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(req.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	return peer
}

func (r Resolver) trusts(host string) bool {
	if len(r.trusted) == 0 || host == "" {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, p := range r.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteHost strips the port from an http.Request.RemoteAddr value
// ("1.2.3.4:5678" -> "1.2.3.4", "[::1]:80" -> "::1"). RemoteAddr is
// not guaranteed to carry a port (CGI mode, tests), so a missing port
// is not an error — return the input as-is in that case.
func remoteHost(addr string) string {
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
