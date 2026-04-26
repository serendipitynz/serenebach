package spam

import (
	"net"
	"strings"
)

// IPBlocklist is the parsed form of weblogs.ip_blacklist. Each entry is
// either a single-address prefix (/32 for v4, /128 for v6) or a CIDR
// range; storing everything as *net.IPNet lets the match path be a plain
// Contains loop without IP-vs-CIDR branching.
type IPBlocklist []*net.IPNet

// ParseIPBlocklist compiles a newline-separated admin textarea into a
// ready-to-match list. Lines may be:
//
//   - empty (skipped),
//   - comments starting with "#" (skipped — admins annotate with these),
//   - bare IPs: "198.51.100.5" or "2001:db8::1",
//   - CIDR: "198.51.100.0/24" or "2001:db8::/48".
//
// Malformed lines are silently skipped rather than erroring out — an
// admin with a typo shouldn't end up with every comment rejected. A
// future admin-form lint could highlight the bad lines; for v1 we
// trade precision for robustness. Returns an empty (non-nil) slice
// when the input has no valid entries so callers can treat the result
// uniformly.
func ParseIPBlocklist(raw string) IPBlocklist {
	lines := strings.Split(raw, "\n")
	out := make(IPBlocklist, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if n := parseEntry(l); n != nil {
			out = append(out, n)
		}
	}
	return out
}

// parseEntry normalises one line. When the line is a bare address we
// promote it to a /32 (v4) or /128 (v6) net so the match loop stays
// uniform.
func parseEntry(s string) *net.IPNet {
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil
		}
		return n
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
}

// Contains reports whether addr is blocked by any entry on the list.
// Accepts the string form from handler.clientIP() — ports are not
// expected (the caller strips them), but an "address:port" form is
// tolerated so we never crash on an unexpected shape.
func (l IPBlocklist) Contains(addr string) bool {
	if len(l) == 0 {
		return false
	}
	// Tolerate an accidental "host:port" — split the port off before
	// parsing. SplitHostPort fails on bare IPv6 without a port, which
	// is why we only call it when a port-like suffix looks present.
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	// Normalise IPv4-in-IPv6 form so blocklist entries written as v4
	// still match requests that arrive with v4-mapped v6 addresses.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range l {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
