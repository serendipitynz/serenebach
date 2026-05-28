// Package lint flags SB3 tags and blocks that the Go port doesn't
// populate the way an imported template expects. It's not a template
// validator — a template that references only unknown names is
// considered fine (a theme may introduce author-specific tags).
// The lint draws a bright line around the small set of tags and
// blocks the Go port either (a) does not implement, or (b)
// implements with different semantics than SB3, so importers and
// admin template-editors can surface those specifically.
package lint

import (
	"regexp"
	"sort"
	"strings"

	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// Severity ranks findings by how the caller should react.
type Severity string

const (
	// SevUnsupported: the tag / block is never populated. An
	// imported template referencing it will render empty — usually
	// silently broken behaviour (dead form action, missing sidebar).
	SevUnsupported Severity = "unsupported"
	// SevDiffers: the tag / block is wired but its semantics don't
	// match SB3's. Templates keep working but readers may see
	// unexpected content.
	SevDiffers Severity = "differs"
)

// Kind distinguishes tag-level findings from block-level findings
// when presenting the lint output.
type Kind string

const (
	KindTag   Kind = "tag"
	KindBlock Kind = "block"
)

// Finding is one lint hit. Note carries the human-readable
// explanation — stable enough to use as an i18n lookup key later
// without being one today, so the importer can emit it verbatim.
// Lines is populated only by AnalyzeSource; it stays nil for callers
// that ran Analyze against a pre-parsed Template.
type Finding struct {
	Kind     Kind
	Name     string
	Severity Severity
	Note     string
	Lines    []int
}

// unsupportedTags holds exact tag names the Go port never populates.
// Prefixes that should match any tag starting with the string live in
// unsupportedTagPrefixes below.
var unsupportedTags = map[string]string{
	"trackback_url":         "trackback feature is out of scope",
	"trackback_count":       "trackback feature is out of scope",
	"recent_trackback_list": "trackback feature is out of scope",
	"comment_iconform":      "comment icons aren't supported in the Go port",
	"related_category":      "secondary categories aren't modelled in the Go port",
	"related_category_disp": "secondary categories aren't modelled in the Go port",
	"calendar":              "calendar sidebar widget isn't implemented yet",
	"calendar2":             "calendar sidebar widget isn't implemented yet",
	"calendar_horizontal":   "calendar sidebar widget isn't implemented yet",
	"calendar_vertical":     "calendar sidebar widget isn't implemented yet",
}

var unsupportedTagPrefixes = []struct {
	Prefix string
	Note   string
}{
	{"trackback_", "trackback feature is out of scope"},
	{"amazon_", "Amazon affiliate integration is out of scope"},
	{"asin_", "Amazon affiliate integration is out of scope"},
}

// differsTags are wired but their values don't match SB3's output.
var differsTags = map[string]string{
	"site_mobile":   "always empty — the Go port has no mobile-specific route",
	"comment_icon":  "always empty — reader avatars aren't modelled yet",
	"profile_email": "always empty — author email is an admin credential, not public",
	"sb_comment_js": "always empty — the Go port has no reader-facing comment script",
}

var unsupportedBlocks = map[string]string{
	"trackback_area":        "trackback feature is out of scope",
	"recent_trackback":      "trackback feature is out of scope",
	"trackback":             "trackback feature is out of scope",
	"amazon_area":           "Amazon affiliate integration is out of scope",
	"amazon":                "Amazon affiliate integration is out of scope",
	"comment_iconform":      "comment icons aren't supported in the Go port",
	"calendar":              "calendar sidebar widget isn't implemented yet",
	"mobile_top":            "mobile mode was dropped in the Go port",
	"mobile_entry":          "mobile mode was dropped in the Go port",
	"mobile_comment_area":   "mobile mode was dropped in the Go port",
	"mobile_comment_form":   "mobile mode was dropped in the Go port",
	"mobile_trackback_area": "mobile mode was dropped in the Go port",
}

var differsBlocks = map[string]string{
	"selected_entry":      "always 0 — recommended-posts flag isn't modelled yet",
	"selected_entry_list": "always 0 — recommended-posts flag isn't modelled yet",
}

// nativeTags / nativeBlocks document the names introduced by the Go
// rewrite that have no SB3 counterpart. They are NOT flagged by the
// linter (the engine treats unknown names as silently fine), but
// listing them here keeps the inventory traceable: if a future SB3
// compat audit ever pulls "every tag this engine emits", these are
// the ones that should be flagged as engine extensions rather than
// implementations of an SB3 contract.
var nativeTags = map[string]string{
	"search_query": "Serene Bach native — search-result page only",
	"search_total": "Serene Bach native — search-result page only",
	"search_url":   "Serene Bach native — /search action URL",
}

var nativeBlocks = map[string]string{
	"search_form":    "Serene Bach native — embed search form (gated by static_search_form_enabled on static builds)",
	"search_results": "Serene Bach native — search-result page only",
	"search_empty":   "Serene Bach native — search-result page only",
}

// Analyze walks the parsed template and returns every tag / block
// the Go port handles unusually. Unknown names (custom tags, rare
// SB3 names we haven't inventoried) are silently ignored so themes
// don't get flooded with false positives.
func Analyze(tmpl *sbtemplate.Template) []Finding {
	if tmpl == nil {
		return nil
	}
	var out []Finding
	for _, tag := range tmpl.UsedTags() {
		if _, native := nativeTags[tag]; native {
			continue
		}
		if note, ok := unsupportedTags[tag]; ok {
			out = append(out, Finding{Kind: KindTag, Name: tag, Severity: SevUnsupported, Note: note})
			continue
		}
		if note, ok := differsTags[tag]; ok {
			out = append(out, Finding{Kind: KindTag, Name: tag, Severity: SevDiffers, Note: note})
			continue
		}
		for _, p := range unsupportedTagPrefixes {
			if strings.HasPrefix(tag, p.Prefix) {
				out = append(out, Finding{Kind: KindTag, Name: tag, Severity: SevUnsupported, Note: p.Note})
				break
			}
		}
	}
	for _, block := range tmpl.UsedBlocks() {
		if _, native := nativeBlocks[block]; native {
			continue
		}
		if note, ok := unsupportedBlocks[block]; ok {
			out = append(out, Finding{Kind: KindBlock, Name: block, Severity: SevUnsupported, Note: note})
			continue
		}
		if note, ok := differsBlocks[block]; ok {
			out = append(out, Finding{Kind: KindBlock, Name: block, Severity: SevDiffers, Note: note})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// AnalyzeSource runs Analyze against `src` (parsing it first) and
// then enriches each Finding with the 1-indexed source line numbers
// where the tag / block name appears. Used by the admin template
// editor so reviewers can jump to the offending line. Parse failures
// return nil — the admin save path runs its own syntax check already,
// so lint gracefully degrades to "no findings" when the body doesn't
// parse.
func AnalyzeSource(src string) []Finding {
	tmpl, err := sbtemplate.Parse(src, sbtemplate.NoCallback)
	if err != nil || tmpl == nil {
		return nil
	}
	findings := Analyze(tmpl)
	if len(findings) == 0 {
		return nil
	}
	tagLines := map[string][]int{}
	blockLines := map[string][]int{}
	for i, line := range strings.Split(src, "\n") {
		lineNo := i + 1
		if m := lintBeginRe.FindStringSubmatch(line); m != nil {
			blockLines[m[1]] = append(blockLines[m[1]], lineNo)
			continue
		}
		for _, match := range lintTagRe.FindAllString(line, -1) {
			name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
			if name == "" {
				continue
			}
			tagLines[name] = append(tagLines[name], lineNo)
		}
	}
	for i := range findings {
		switch findings[i].Kind {
		case KindTag:
			findings[i].Lines = tagLines[findings[i].Name]
		case KindBlock:
			findings[i].Lines = blockLines[findings[i].Name]
		}
	}
	return findings
}

var (
	// Mirrors sbtemplate.Parse's regexes so the lint scanner reports
	// the same anchor positions the parser would key off.
	lintBeginRe = regexp.MustCompile(`<!--\s*BEGIN\s+(\w+)\s*-->`)
	lintTagRe   = regexp.MustCompile(`\{\w+\}`)
)
