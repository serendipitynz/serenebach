package spam

import "testing"

func TestParseWordsDropsBlanksAndComments(t *testing.T) {
	in := "  viagra\n\n# these are spam triggers\nCasino\n  \n  cialis  \n"
	got := ParseWords(in)
	want := []string{"viagra", "casino", "cialis"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestMatchesAnyCaseInsensitive(t *testing.T) {
	words := ParseWords("viagra\ncasino")
	if !MatchesAny([]string{"Check out VIAGRA now"}, words) {
		t.Errorf("expected match on uppercase occurrence")
	}
	if !MatchesAny([]string{"clean", "go to casino tonight"}, words) {
		t.Errorf("expected match in second field")
	}
	if MatchesAny([]string{"clean text here"}, words) {
		t.Errorf("unexpected match")
	}
}

func TestMatchesAnyEmptyList(t *testing.T) {
	if MatchesAny([]string{"anything"}, nil) {
		t.Errorf("empty list must never match")
	}
}
