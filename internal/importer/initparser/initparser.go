// Package initparser reads SB2/SB3 flat-file config (sb::InitParser
// format), used by configure.cgi, init.cgi, and a few other text files
// the legacy Perl runtime persisted via sb::InitParser.
//
// Format: one line per entry, `key<sep>value`, where <sep> is either a
// tab or a run of whitespace. Lines starting with `#` are comments;
// blank lines are skipped. The value carries shell-style backslash
// escapes — `\t` → tab, `\n` → newline, `\X` → X for any other X — so
// multi-line values (e.g. SB3's conf_spamword) survive a round trip.
//
// SB's array (`[v1\tv2\t]`) and hash (`{key\tval}`) value shapes are
// recognised and skipped silently. The importer only needs the scalar
// surface (every conf_* / basic_* URL setting is a scalar); supporting
// the other shapes here would just be ballast.
package initparser

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// Parse reads r and returns a map of every scalar key to its decoded
// string value. Duplicate keys keep the last occurrence — InitParser's
// own loader has the same behaviour.
func Parse(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	// Some SB3 configure.cgi files carry long conf_spamword values; the
	// 64KB default is enough in practice, but bump the buffer to 1MB to
	// be safe across pathological cases.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// InitParser strips CR/LF from the read line via tr; bufio.Scanner
		// already drops the trailing LF, but a stray CR (Windows EOL on
		// Unix-edited files, or vice versa) needs explicit removal.
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw := splitKeyValue(line)
		if key == "" {
			continue
		}
		key = decode(key)
		// Array / hash shapes — skip. The importer doesn't consume them
		// and silently skipping is the safe default for unknown lines.
		if isArrayOrHash(raw) {
			continue
		}
		out[key] = decode(raw)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseFile is a convenience wrapper around Parse. A missing file is
// not an error — the caller gets an empty map. SB3 instances that
// ran with config kept entirely in defaults have no configure.cgi at
// all, and that is a valid state.
func ParseFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// splitKeyValue mirrors InitParser.pm's split(/$sep/, $line, 2) where
// $sep defaults to \s+ (any whitespace run). The first whitespace run
// separates key from value; everything after the run — including any
// further whitespace or tabs — is the verbatim raw value.
func splitKeyValue(line string) (key, val string) {
	for i, r := range line {
		if r == ' ' || r == '\t' {
			j := i
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			return line[:i], line[j:]
		}
	}
	// No separator: the whole line is a bare key with empty value. SB
	// treats this as a present-but-empty entry, which round-trips cleanly.
	return line, ""
}

// isArrayOrHash returns true when the raw value is wrapped in `[...]`
// or `{...}` — InitParser's array and hash shapes. We only detect the
// outer wrapper; the importer doesn't consume either kind.
func isArrayOrHash(raw string) bool {
	if len(raw) < 2 {
		return false
	}
	first, last := raw[0], raw[len(raw)-1]
	return (first == '[' && last == ']') || (first == '{' && last == '}')
}

// decode reverses InitParser._encode: backslash escapes \t/\n become
// tab/newline, and any other \X becomes literal X (so \\ → \, \# → #,
// \<space> → <space>). Lone trailing backslash is preserved as-is.
func decode(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		next := s[i+1]
		switch next {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		default:
			b.WriteByte(next)
		}
		i++
	}
	return b.String()
}
