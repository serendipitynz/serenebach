package ai

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrDestinationBlocked is returned, wrapped with the refused address, when
// a PublicOnlyClient is asked to connect anywhere but the public internet.
var ErrDestinationBlocked = errors.New("ai: this account may only connect to public addresses")

// PublicOnlyClient returns the HTTP client for AI requests made on behalf
// of a user who is not trusted with the server's network position: it
// refuses loopback, private, link-local and other non-public addresses.
// The check runs on the address actually dialled, after DNS resolution and
// on every redirect hop, so a hostname that resolves inward or a redirect
// pointing inward is refused just like a literal 127.0.0.1.
func PublicOnlyClient() *http.Client {
	return dialCheckedClient(isPublicAddr)
}

func dialCheckedClient(allowed func(netip.Addr) bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrDestinationBlocked, address)
			}
			if !allowed(ap.Addr()) {
				return fmt.Errorf("%w: %s", ErrDestinationBlocked, ap.Addr())
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: DefaultTimeout,
		Transport: &http.Transport{
			// No proxy: through one, the dial reaches the proxy rather than
			// the destination, and the check would vet the wrong address.
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// nonPublicPrefixes covers the special-purpose ranges netip's predicates
// leave out. 0.0.0.0/8 reaches the local host on Linux; 100.64.0.0/10 is
// where some clouds put their metadata service; the NAT64 prefixes would
// carry a request to an embedded private IPv4 address.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("fec0::/10"),
}

func isPublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
