// Package sbtemplate is a Go port of the Serene Bach template engine
// (originally sb::Template in Perl).
//
// The engine recognises exactly three directives, kept verbatim from the
// original to allow existing SB templates to render as-is:
//
//	<!-- BEGIN name -->   // start a nestable, repeatable block
//	<!-- END -->          // close the innermost block (name not matched)
//	{tagname}             // scalar substitution (plus internally generated {-blockname})
//
// No conditionals, no loops beyond repeat-count semantics, no Perl eval.
// Text-format filters run before rendering and emit HTML strings; templates
// themselves contain no filter directives.
package sbtemplate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// block holds one parsed block's body (with {-childname} placeholders) and the
// deduplicated list of tag tokens that appear in body.
type block struct {
	text string
	tags []string
}

// Template is a parsed, immutable template ready to be instantiated for
// rendering. Create a render Context with New.
type Template struct {
	blocks map[string]*block
}

// ParseCallback is invoked during parsing at BEGIN (isEnd=false) and END
// (isEnd=true) for each block, mirroring sb::Template's _default_callback hook.
// Implementations may append hidden tags/text to the block currently being
// built.
type ParseCallback func(name string, isEnd bool, blocks map[string]*block)

var (
	beginRe = regexp.MustCompile(`<!--\s*BEGIN\s+(\w+)\s*-->`)
	tagRe   = regexp.MustCompile(`\{\w+\}`)
	// childTagRe recognises the {-name} placeholders that Parse inserts to
	// mark child-block render sites; name may contain dots (divided blocks).
	childTagRe = regexp.MustCompile(`\{-([a-zA-Z0-9_.]+)\}`)
)

const rootBlock = "-main"

// Parse compiles src into a Template. If cb is nil, DefaultCallback is used,
// which injects a couple of SB-specific tags around the `entry` and
// `comment_area` blocks. Pass NoCallback to disable injection entirely.
func Parse(src string, cb ParseCallback) (*Template, error) {
	if cb == nil {
		cb = DefaultCallback
	}

	blocks := map[string]*block{rootBlock: {}}
	stack := []string{rootBlock}
	cur := rootBlock

	// Perl's split("\n", $str) drops trailing empty fields; Go's strings.Split
	// keeps them. Mirror the Perl behavior so a template ending in a newline
	// doesn't produce a spurious trailing "\n" in the output.
	lines := strings.Split(src, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		if m := beginRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			for _, exists := blocks[name]; exists; _, exists = blocks[name] {
				name += "."
			}
			placeholder := "{-" + name + "}"
			blocks[cur].text += placeholder
			blocks[cur].tags = append(blocks[cur].tags, placeholder)

			blocks[name] = &block{}
			cb(name, false, blocks)
			stack = append(stack, name)
			cur = name
			continue
		}
		if strings.Contains(line, "<!-- END ") {
			// An END at the root level has no matching BEGIN — return a
			// syntax error instead of underflowing the stack and panicking.
			if len(stack) <= 1 {
				return nil, fmt.Errorf("sbtemplate: unexpected END at line %d", i+1)
			}
			cb(cur, true, blocks)
			blocks[cur].tags = dedup(blocks[cur].tags)
			stack = stack[:len(stack)-1]
			cur = stack[len(stack)-1]
			continue
		}
		if matches := tagRe.FindAllString(line, -1); matches != nil {
			blocks[cur].tags = append(blocks[cur].tags, matches...)
		}
		blocks[cur].text += line + "\n"
	}

	return &Template{blocks: blocks}, nil
}

// NoCallback is a no-op ParseCallback, used when no tag injection is desired.
func NoCallback(name string, isEnd bool, blocks map[string]*block) {}

// DefaultCallback reproduces sb::Template::_default_callback, which adds
// {sb_entry_marking} at the start of the `entry` block and {sb_comment_js}
// at the end of `comment_area`. Many SB templates depend on these injections.
func DefaultCallback(name string, isEnd bool, blocks map[string]*block) {
	var tag string
	switch {
	case name == "entry" && !isEnd:
		tag = "{sb_entry_marking}"
	case name == "comment_area" && isEnd:
		tag = "{sb_comment_js}"
	}
	if tag == "" {
		return
	}
	blocks[name].text += tag + "\n"
	blocks[name].tags = append(blocks[name].tags, tag)
}

// HasBlock reports whether the parsed template contains a block of this name.
func (t *Template) HasBlock(name string) bool {
	_, ok := t.blocks[name]
	return ok
}

// UsedTags returns the set of `{tag}` names referenced across all
// blocks, sorted and deduplicated. Internal child-block placeholders
// (`{-blockname}`) are filtered out — the caller wants author-visible
// tags only, which is what the template lint compares against. Bare
// names, no braces.
func (t *Template) UsedTags() []string {
	set := map[string]struct{}{}
	for _, b := range t.blocks {
		for _, raw := range b.tags {
			if strings.HasPrefix(raw, "{-") {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}")
			if name == "" {
				continue
			}
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// UsedBlocks returns every author-defined `<!-- BEGIN name -->` block
// name, sorted. The synthetic root block and the `.`-suffixed
// divided-block aliases are excluded so callers see the same names
// the template author wrote.
func (t *Template) UsedBlocks() []string {
	set := map[string]struct{}{}
	for name := range t.blocks {
		if name == rootBlock {
			continue
		}
		base := strings.TrimRight(name, ".")
		if base == "" {
			continue
		}
		set[base] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Context is a single render invocation's state: the block repetition counts,
// per-iteration tag values, and the current iteration cursor used by Tag.
// Create one per output; contexts are not safe for concurrent use.
type Context struct {
	tmpl      *Template
	blockOnce map[string]string // deleted blocks: name -> "" override (simulates deleteBlock)
	counts    map[string]int
	tags      map[string][]string
	num       int
}

// New returns a fresh render context for this template.
func (t *Template) New() *Context {
	return &Context{
		tmpl:      t,
		blockOnce: map[string]string{},
		counts:    map[string]int{},
		tags:      map[string][]string{},
	}
}

// Tag records a value for {name} at the current iteration index (set via Num).
func (c *Context) Tag(name, value string) {
	key := "{" + name + "}"
	vals := c.tags[key]
	for len(vals) <= c.num {
		vals = append(vals, "")
	}
	vals[c.num] = value
	c.tags[key] = vals
}

// Num sets the iteration cursor used by subsequent Tag calls. Call Num(0),
// set tags, Num(1), set tags, ..., then Block("name", N) to bind the loop.
func (c *Context) Num(i int) { c.num = i }

// Block records the repetition count for a named block. 0 hides the block.
func (c *Context) Block(name string, count int) { c.counts[name] = count }

// DeleteBlock mirrors sb::Template::deleteBlock — it blanks the block's body
// so any render of it emits the empty string.
func (c *Context) DeleteBlock(name string) { c.blockOnce[name] = "" }

// Clear resets per-render state (tags and block counts). Mirrors
// sb::Template::clear.
func (c *Context) Clear() {
	c.tags = map[string][]string{}
	for k := range c.counts {
		c.counts[k] = 0
	}
}

// Render produces the final output, starting from the synthetic `-main` block
// whose count is fixed at 1.
func (c *Context) Render() string {
	c.counts[rootBlock] = 1
	return c.renderBlock(rootBlock)
}

func (c *Context) renderBlock(name string) string {
	b, ok := c.tmpl.blocks[name]
	if !ok {
		return ""
	}
	// DeleteBlock override: rendered text is always empty.
	if override, killed := c.blockOnce[name]; killed {
		return override
	}
	if b.text == "" {
		return ""
	}

	num := c.counts[name]
	// Divided blocks (name ending with dots) share the base name's count.
	if strings.HasSuffix(name, ".") {
		base := strings.TrimRight(name, ".")
		num = c.counts[base]
	}
	if num == 0 {
		return ""
	}

	var out strings.Builder
	for i := 0; i < num; i++ {
		text := b.text
		for _, tag := range b.tags {
			var repl string
			if m := childTagRe.FindStringSubmatch(tag); m != nil {
				repl = c.renderBlock(m[1])
			} else if vals := c.tags[tag]; i < len(vals) && vals[i] != "" {
				repl = vals[i]
			} else if len(vals) > 0 {
				repl = vals[0]
			}
			text = strings.ReplaceAll(text, tag, repl)
		}
		out.WriteString(text)
	}
	return out.String()
}

// dedup preserves first-occurrence order while removing duplicate strings,
// matching the behavior of sb::Utils::remove_duplicates used by _parse.
func dedup(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
