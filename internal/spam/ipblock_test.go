package spam

import "testing"

func TestParseIPBlocklistSkipsBlanksAndComments(t *testing.T) {
	in := "\n# banned in incident 2026-04-22\n198.51.100.5\n\n  198.51.100.0/24  \n# IPv6\n2001:db8::/48\n"
	got := ParseIPBlocklist(in)
	if len(got) != 3 {
		t.Fatalf("parsed %d entries, want 3: %#v", len(got), got)
	}
}

func TestParseIPBlocklistDropsGarbage(t *testing.T) {
	in := "not-an-ip\n999.999.999.999\n198.51.100.0/33\n198.51.100.5\n"
	got := ParseIPBlocklist(in)
	if len(got) != 1 {
		t.Fatalf("parsed %d, want 1 valid entry; got %#v", len(got), got)
	}
}

func TestIPBlocklistContainsExactIP(t *testing.T) {
	bl := ParseIPBlocklist("198.51.100.5\n")
	if !bl.Contains("198.51.100.5") {
		t.Errorf("exact v4 should match")
	}
	if bl.Contains("198.51.100.6") {
		t.Errorf("non-matching v4 should not match")
	}
}

func TestIPBlocklistContainsCIDR(t *testing.T) {
	bl := ParseIPBlocklist("198.51.100.0/24\n")
	if !bl.Contains("198.51.100.1") {
		t.Errorf("in-range v4 should match /24")
	}
	if !bl.Contains("198.51.100.254") {
		t.Errorf("in-range v4 should match /24")
	}
	if bl.Contains("198.51.101.1") {
		t.Errorf("out-of-range v4 should not match")
	}
}

func TestIPBlocklistContainsIPv6CIDR(t *testing.T) {
	bl := ParseIPBlocklist("2001:db8::/48\n")
	if !bl.Contains("2001:db8::1") {
		t.Errorf("in-range v6 should match")
	}
	if bl.Contains("2001:db9::1") {
		t.Errorf("out-of-range v6 should not match")
	}
}

func TestIPBlocklistV4MappedV6Match(t *testing.T) {
	// When the client reaches us as a v4-mapped v6 address but the
	// admin wrote the blocklist in v4 form, they should still match.
	bl := ParseIPBlocklist("198.51.100.5\n")
	if !bl.Contains("::ffff:198.51.100.5") {
		t.Errorf("v4-mapped v6 should match the v4 blocklist entry")
	}
}

func TestIPBlocklistToleratesHostPort(t *testing.T) {
	// clientIP() strips ports, but a caller that forgets shouldn't
	// crash the match path.
	bl := ParseIPBlocklist("198.51.100.5\n")
	if !bl.Contains("198.51.100.5:54321") {
		t.Errorf("host:port form should still match")
	}
}

func TestIPBlocklistEmptyListNeverMatches(t *testing.T) {
	bl := ParseIPBlocklist("")
	if bl.Contains("198.51.100.5") {
		t.Errorf("empty list should never match")
	}
}

func TestIPBlocklistCIDREndAddresses(t *testing.T) {
	// The existing /24 test checks .1 and .254. The actual prefix ends —
	// the network (.0) and broadcast (.255) addresses — are also inside
	// the range, so a blocklist entry must match a client arriving as
	// either. Locks the "whole prefix is blocked" boundary.
	bl := ParseIPBlocklist("198.51.100.0/24\n")
	for _, addr := range []string{"198.51.100.0", "198.51.100.255"} {
		if !bl.Contains(addr) {
			t.Errorf("%s should be inside 198.51.100.0/24", addr)
		}
	}
	// One past each end must NOT match.
	for _, addr := range []string{"198.51.99.255", "198.51.101.0"} {
		if bl.Contains(addr) {
			t.Errorf("%s is outside 198.51.100.0/24 and must not match", addr)
		}
	}
}

func TestIPBlocklistInvalidAddrNeverMatches(t *testing.T) {
	// A garbage address against a NON-empty list must not crash and must
	// not match. The empty-list case is covered separately; this guards
	// the parse-failure path on the hot match loop.
	bl := ParseIPBlocklist("198.51.100.5\n198.51.100.0/24\n")
	for _, addr := range []string{"not-an-ip", "", "999.999.999.999", "::ffff:zz"} {
		if bl.Contains(addr) {
			t.Errorf("unparseable addr %q must never match a blocklist entry", addr)
		}
	}
}
