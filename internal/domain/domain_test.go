package domain

import "testing"

func TestIsValidCanonicalURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/post", true},
		{"http://example.com", true},
		{"https://example.com/?a=1&b=2", true},
		{"", false},                  // empty is "no canonical", not a valid URL
		{"/relative/path", false},    // must be absolute
		{"example.com/post", false},  // missing scheme
		{"ftp://example.com", false}, // non-web scheme
		{"javascript:alert(1)", false},
		{"https://", false}, // scheme but no host
	}
	for _, c := range cases {
		if got := IsValidCanonicalURL(c.in); got != c.want {
			t.Errorf("IsValidCanonicalURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
