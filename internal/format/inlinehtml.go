package format

import (
	"html"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type attrKind int

const (
	attrText attrKind = iota
	// attrHTTPURL keeps the value only when it is an absolute http(s) URL,
	// so a `cite` cannot carry `javascript:` or a relative path that would
	// resolve differently on the static mirror and the dynamic origin.
	attrHTTPURL
)

// allowedInlineTags maps each tag a Markdown body may carry as inline HTML
// to the attributes kept on it; every other attribute is dropped.
//
// HTML formatting elements (b, i, s, small, u, …) are deliberately absent:
// left unclosed, the HTML parser reconstructs them into every following
// paragraph — past the entry and into the template around it. The tags here
// are popped when their enclosing block closes, so a typo stays inside the
// paragraph it was made in.
var allowedInlineTags = map[string]map[string]attrKind{
	"q":    {"cite": attrHTTPURL},
	"cite": {},
	"abbr": {"title": attrText},
	"dfn":  {"title": attrText},
	"kbd":  {},
	"sub":  {},
	"sup":  {},
	"mark": {},
	"ins":  {"cite": attrHTTPURL, "datetime": attrText},
	"del":  {"cite": attrHTTPURL, "datetime": attrText},
}

const omittedRawHTML = "<!-- raw HTML omitted -->"

type inlineHTMLAllowlist struct{}

func (inlineHTMLAllowlist) Extend(m goldmark.Markdown) {
	// Priorities sort ascending and the lowest registers last, so anything
	// below the stock HTML renderer's 1000 takes over KindRawHTML from it.
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(inlineHTMLAllowlist{}, 100),
	))
}

func (inlineHTMLAllowlist) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderAllowedRawHTML)
}

func renderAllowedRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*ast.RawHTML)
	var raw strings.Builder
	for i := 0; i < n.Segments.Len(); i++ {
		seg := n.Segments.At(i)
		raw.Write(seg.Value(source))
	}
	if tag, ok := sanitizeInlineTag(raw.String()); ok {
		_, _ = w.WriteString(tag)
	} else {
		_, _ = w.WriteString(omittedRawHTML)
	}
	return ast.WalkSkipChildren, nil
}

// sanitizeInlineTag rebuilds one raw inline HTML tag from its parsed parts
// rather than echoing the source, so nothing outside the allowlist — an
// unexpected attribute, an unquoted value, a stray character goldmark
// tolerated — reaches the output. ok is false when the tag is to be omitted.
func sanitizeInlineTag(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "<") || !strings.HasSuffix(raw, ">") {
		return "", false
	}
	body := raw[1 : len(raw)-1]
	if rest, closing := strings.CutPrefix(body, "/"); closing {
		return sanitizeClosingTag(rest)
	}

	name, rest := scanTagName(body)
	allowedAttrs, ok := allowedInlineTags[name]
	if !ok {
		return "", false
	}

	var out strings.Builder
	out.WriteString("<" + name)
	seen := map[string]bool{}
	for {
		rest = strings.TrimLeft(rest, htmlSpace)
		if rest == "" {
			break
		}
		// None of the allowed tags is void, and HTML ignores the
		// self-closing flag on non-void elements, so `<q/>` would open a
		// quote the author believed closed. Checked here rather than on
		// the raw suffix because an unquoted value may end in `/`.
		if rest == "/" {
			return "", false
		}
		var key, value string
		key, value, rest, ok = scanAttr(rest)
		if !ok {
			return "", false
		}
		// Browsers keep the first of duplicated attributes; marking the key
		// seen before validating it means a rejected first value is not
		// replaced by a later one the browser would never have used.
		if seen[key] {
			continue
		}
		seen[key] = true
		kind, ok := allowedAttrs[key]
		if !ok {
			continue
		}
		if kind == attrHTTPURL {
			value = strings.TrimSpace(value)
			if !isHTTPURL(value) {
				continue
			}
		}
		out.WriteString(" " + key + `="` + html.EscapeString(value) + `"`)
	}
	out.WriteString(">")
	return out.String(), true
}

func sanitizeClosingTag(body string) (string, bool) {
	name, rest := scanTagName(body)
	if _, ok := allowedInlineTags[name]; !ok || strings.TrimLeft(rest, htmlSpace) != "" {
		return "", false
	}
	return "</" + name + ">", true
}

// scanAttr reads one attribute off the front of s. The key comes back
// lowercased and the value with character references decoded, so callers
// validate what the browser would see rather than its source spelling.
func scanAttr(s string) (key, value, rest string, ok bool) {
	name, rest := scanAttrName(s)
	if name == "" {
		return "", "", "", false
	}
	if afterEq, hasValue := strings.CutPrefix(strings.TrimLeft(rest, htmlSpace), "="); hasValue {
		var raw string
		raw, rest, ok = scanAttrValue(strings.TrimLeft(afterEq, htmlSpace))
		if !ok {
			return "", "", "", false
		}
		value = html.UnescapeString(raw)
	}
	return strings.ToLower(name), value, rest, true
}

// htmlSpace is CommonMark's whitespace inside a tag, which includes the line
// endings of a tag that spans lines.
const htmlSpace = " \t\n\r\f\v"

func scanTagName(s string) (name, rest string) {
	i := 0
	for i < len(s) && (isASCIILetter(s[i]) || (i > 0 && (isASCIIDigit(s[i]) || s[i] == '-'))) {
		i++
	}
	return strings.ToLower(s[:i]), s[i:]
}

func scanAttrName(s string) (name, rest string) {
	i := 0
	for i < len(s) {
		c := s[i]
		if isASCIILetter(c) || c == '_' || c == ':' || (i > 0 && (isASCIIDigit(c) || c == '.' || c == '-')) {
			i++
			continue
		}
		break
	}
	return s[:i], s[i:]
}

func scanAttrValue(s string) (value, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if q := s[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", "", false
		}
		return s[1 : 1+end], s[2+end:], true
	}
	end := strings.IndexAny(s, htmlSpace+"\"'=<>`")
	if end < 0 {
		end = len(s)
	}
	if end == 0 {
		return "", "", false
	}
	return s[:end], s[end:], true
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
