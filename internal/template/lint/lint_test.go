package lint

import (
	"testing"

	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

func parseOrFatal(t *testing.T, body string) *sbtemplate.Template {
	t.Helper()
	tmpl, err := sbtemplate.Parse(body, sbtemplate.NoCallback)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return tmpl
}

func findFinding(fs []Finding, kind Kind, name string) (Finding, bool) {
	for _, f := range fs {
		if f.Kind == kind && f.Name == name {
			return f, true
		}
	}
	return Finding{}, false
}

func TestAnalyzeFlagsTrackbackAsUnsupported(t *testing.T) {
	tmpl := parseOrFatal(t, "<!-- BEGIN trackback_area -->tb<!-- END trackback_area -->\n{trackback_count}\n")
	findings := Analyze(tmpl)
	if f, ok := findFinding(findings, KindBlock, "trackback_area"); !ok || f.Severity != SevUnsupported {
		t.Errorf("expected trackback_area block unsupported, got %+v", findings)
	}
	if f, ok := findFinding(findings, KindTag, "trackback_count"); !ok || f.Severity != SevUnsupported {
		t.Errorf("expected trackback_count tag unsupported, got %+v", findings)
	}
}

func TestAnalyzeFlagsAmazonPrefix(t *testing.T) {
	tmpl := parseOrFatal(t, "{amazon_link} {asin_title}\n")
	findings := Analyze(tmpl)
	if _, ok := findFinding(findings, KindTag, "amazon_link"); !ok {
		t.Errorf("expected amazon_link unsupported: %+v", findings)
	}
	if _, ok := findFinding(findings, KindTag, "asin_title"); !ok {
		t.Errorf("expected asin_title unsupported: %+v", findings)
	}
}

func TestAnalyzeFlagsSiteMobileAsDiffers(t *testing.T) {
	tmpl := parseOrFatal(t, "{site_mobile}\n")
	findings := Analyze(tmpl)
	f, ok := findFinding(findings, KindTag, "site_mobile")
	if !ok {
		t.Fatalf("expected site_mobile finding, got %+v", findings)
	}
	if f.Severity != SevDiffers {
		t.Errorf("expected differs, got %s", f.Severity)
	}
}

func TestAnalyzeIgnoresUnknownTags(t *testing.T) {
	tmpl := parseOrFatal(t, "{some_custom_theme_tag}\n<!-- BEGIN my_widget -->{x}<!-- END my_widget -->\n")
	if got := Analyze(tmpl); len(got) != 0 {
		t.Errorf("unknown names should not trigger findings, got %+v", got)
	}
}

func TestAnalyzeFlagsSelectedEntryBlockAsDiffers(t *testing.T) {
	tmpl := parseOrFatal(t, "<!-- BEGIN selected_entry -->\nx\n<!-- END selected_entry -->\n")
	findings := Analyze(tmpl)
	f, ok := findFinding(findings, KindBlock, "selected_entry")
	if !ok {
		t.Fatalf("expected selected_entry finding, got %+v", findings)
	}
	if f.Severity != SevDiffers {
		t.Errorf("expected differs, got %s", f.Severity)
	}
}

func TestAnalyzeSourceCapturesLineNumbers(t *testing.T) {
	src := "header\n" +
		"{site_mobile}\n" +
		"<!-- BEGIN trackback_area -->\n" +
		"some body\n" +
		"<!-- END trackback_area -->\n" +
		"{site_mobile}\n" +
		"{amazon_link}\n"
	findings := AnalyzeSource(src)
	mobile, ok := findFinding(findings, KindTag, "site_mobile")
	if !ok {
		t.Fatalf("missing site_mobile finding: %+v", findings)
	}
	if len(mobile.Lines) != 2 || mobile.Lines[0] != 2 || mobile.Lines[1] != 6 {
		t.Errorf("site_mobile lines = %v, want [2 6]", mobile.Lines)
	}
	tb, ok := findFinding(findings, KindBlock, "trackback_area")
	if !ok {
		t.Fatalf("missing trackback_area: %+v", findings)
	}
	if len(tb.Lines) != 1 || tb.Lines[0] != 3 {
		t.Errorf("trackback_area lines = %v, want [3]", tb.Lines)
	}
	az, ok := findFinding(findings, KindTag, "amazon_link")
	if !ok {
		t.Fatalf("missing amazon_link: %+v", findings)
	}
	if len(az.Lines) != 1 || az.Lines[0] != 7 {
		t.Errorf("amazon_link lines = %v, want [7]", az.Lines)
	}
}

func TestAnalyzeSourceCleanTemplateReturnsNil(t *testing.T) {
	if got := AnalyzeSource("<!-- BEGIN entry -->\n{entry_title}\n<!-- END entry -->\n"); got != nil {
		t.Errorf("expected nil for clean template, got %+v", got)
	}
}
