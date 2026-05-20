package repo

import "testing"

func TestSortDirString(t *testing.T) {
	if SortDesc.String() != "DESC" {
		t.Errorf("SortDesc: got %q, want DESC", SortDesc.String())
	}
	if SortAsc.String() != "ASC" {
		t.Errorf("SortAsc: got %q, want ASC", SortAsc.String())
	}
}

func TestParseSortDir(t *testing.T) {
	cases := []struct {
		in   string
		want SortDir
	}{
		{"asc", SortAsc},
		{"ASC", SortAsc},
		{"Asc", SortAsc},
		{"desc", SortDesc},
		{"DESC", SortDesc},
		{"", SortDesc},
		{"garbage", SortDesc},
		{"ascending", SortDesc},
	}
	for _, tc := range cases {
		if got := ParseSortDir(tc.in); got != tc.want {
			t.Errorf("ParseSortDir(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"plain", "plain"},
		{"100%", `100\%`},
		{"a_b", `a\_b`},
		{`a\b`, `a\\b`},
		{`mix _ % \ end`, `mix \_ \% \\ end`},
	}
	for _, tc := range cases {
		if got := escapeLike(tc.in); got != tc.want {
			t.Errorf("escapeLike(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeSearch(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"hello", "hello"},
		{"  hello  ", "hello"},
		{"hello   world", "hello world"},
		{"  a  b  c  ", "a b c"},
	}
	for _, tc := range cases {
		if got := NormalizeSearch(tc.in); got != tc.want {
			t.Errorf("NormalizeSearch(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}
