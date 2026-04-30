package initparser

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseScalar(t *testing.T) {
	in := strings.NewReader("conf_dir_log\tlog/\nbasic_preid\teid\nbasic_suffix\t.html\n")
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"conf_dir_log": "log/",
		"basic_preid":  "eid",
		"basic_suffix": ".html",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}

func TestParseSpaceSeparator(t *testing.T) {
	// InitParser.pm's default mode treats any whitespace run as separator,
	// not just tabs. Older SB exports occasionally have multiple spaces
	// or a mix of tab+space.
	in := strings.NewReader("conf_lang  ja\nconf_timezone \t +0900\n")
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["conf_lang"] != "ja" {
		t.Errorf("conf_lang: got %q", got["conf_lang"])
	}
	if got["conf_timezone"] != "+0900" {
		t.Errorf("conf_timezone: got %q", got["conf_timezone"])
	}
}

func TestParseEscapes(t *testing.T) {
	// \n encodes a newline inside a scalar; \t a tab; \\ a literal
	// backslash; \X any other char becomes X. Multi-line spam-word lists
	// in real SB3 configure.cgi files use this exact shape.
	in := strings.NewReader(`conf_spamword	name=poker\nname=slot\nname=diet
basic_admntag	sb\\admin
conf_dir_log	\ log/
`)
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["conf_spamword"] != "name=poker\nname=slot\nname=diet" {
		t.Errorf("conf_spamword: got %q", got["conf_spamword"])
	}
	if got["basic_admntag"] != "sb\\admin" {
		t.Errorf("basic_admntag: got %q", got["basic_admntag"])
	}
	if got["conf_dir_log"] != " log/" {
		t.Errorf("conf_dir_log: got %q", got["conf_dir_log"])
	}
}

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	in := strings.NewReader("# leading comment\n\nconf_dir_log\tlog/\n# trailing\n")
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got["conf_dir_log"] != "log/" || len(got) != 1 {
		t.Errorf("got %#v", got)
	}
}

func TestParseSkipsArrayAndHashShapes(t *testing.T) {
	// Array (`[v1\tv2\t]`) and hash (`{k\tv}`) lines round-trip in SB's
	// own format but the importer doesn't consume them — they should be
	// silently dropped, not crash the parser.
	in := strings.NewReader("setup_lang\t[ja\t]\nbasic_cookie\t[email\turl\t]\nplugin_x\t{enabled\t1}\nconf_dir_log\tlog/\n")
	got, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := got["setup_lang"]; ok {
		t.Errorf("setup_lang should have been skipped")
	}
	if _, ok := got["plugin_x"]; ok {
		t.Errorf("plugin_x should have been skipped")
	}
	if got["conf_dir_log"] != "log/" {
		t.Errorf("conf_dir_log: got %q", got["conf_dir_log"])
	}
}

func TestParseFileMissing(t *testing.T) {
	got, err := ParseFile(filepath.Join(t.TempDir(), "nope.cgi"))
	if err != nil {
		t.Fatalf("ParseFile missing: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing file should yield empty map, got %#v", got)
	}
}

func TestParseFileSB3Sandbox(t *testing.T) {
	// _sandbox/data-sb3/configure.cgi is the real SB3 sandbox — the URL
	// settings we care about for legacy redirect must round-trip.
	path := sandboxPath(t, "_sandbox/data-sb3/configure.cgi")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	wantLong := map[string]string{
		"conf_srv_base":      "http://serennz.sakura.ne.jp/sb/",
		"conf_dir_log":       "log/",
		"conf_entry_archive": "Individual",
	}
	for k, v := range wantLong {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}

func TestParseFileSB2Sandbox(t *testing.T) {
	// SB2's configure.cgi uses the same format; this confirms the parser
	// is version-agnostic.
	path := sandboxPath(t, "_sandbox/sb2/data/configure.cgi")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got["conf_dbtype"] != "Text" {
		t.Errorf("conf_dbtype: got %q want Text", got["conf_dbtype"])
	}
	if got["conf_srv_cgi"] != "http://127.0.0.1/sblog/" {
		t.Errorf("conf_srv_cgi: got %q", got["conf_srv_cgi"])
	}
}

// sandboxPath resolves a repo-relative path from the test file location.
// The importer/initparser tests live two levels deep, so we walk up to
// the repo root.
func sandboxPath(t *testing.T, rel string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../internal/importer/initparser/initparser_test.go
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(root, rel)
}
