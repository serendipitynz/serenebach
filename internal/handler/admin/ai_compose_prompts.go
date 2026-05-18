package admin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// composePromptsJSONC is the canonical catalogue for the
// /admin/ai/compose endpoint — system prompt + per-action sampling
// knobs. Edit ai_compose_prompts.jsonc to tweak wording or limits;
// the embed ships the file with the binary so no extra I/O happens
// at runtime. Action keys mirror composeActions; the
// summarize/summarise alias is collapsed at lookup time.
//
//go:embed ai_compose_prompts.jsonc
var composePromptsJSONC []byte

// composePromptTemplate is a parsed catalogue entry. System holds
// the system message with `{format}` / `{lang}` placeholders still
// embedded; resolution happens in composePromptSystem. MaxTokens
// and Temperature are pointers so an explicit zero in the JSONC is
// distinguishable from "missing" and survives unchanged through
// composeMaxTokens / composeTemperature.
type composePromptTemplate struct {
	System      string   `json:"system"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

// composePrompts is decoded once at package init from the JSONC
// catalogue. A malformed file is treated as a build bug and panics
// so the regression is caught loudly in tests.
var composePrompts = mustLoadComposePrompts()

func mustLoadComposePrompts() map[string]composePromptTemplate {
	out := map[string]composePromptTemplate{}
	clean := stripJSONCComments(composePromptsJSONC)
	if err := json.Unmarshal(clean, &out); err != nil {
		panic(fmt.Errorf("ai_compose_prompts.jsonc: %w", err))
	}
	return out
}

// composePromptLookup resolves the catalogue entry for action,
// collapsing the summarize/summarise alias so the JSONC only needs
// one canonical key.
func composePromptLookup(action string) (composePromptTemplate, bool) {
	key := action
	if key == "summarize" {
		key = "summarise"
	}
	tpl, ok := composePrompts[key]
	return tpl, ok
}

// composePromptSystem returns the resolved system prompt for one
// compose action. Unknown actions return an empty string + false so
// callers can fall through to an "unknown_action" error without an
// extra map lookup on their side.
//
// Placeholders supported in the JSONC file:
//
//	{format} → "markdown" | "html"   (raw value, lowercased upstream)
//	{lang}   → "Japanese" | "English" (display name from langName)
func composePromptSystem(action, format, lang string) (string, bool) {
	tpl, ok := composePromptLookup(action)
	if !ok {
		return "", false
	}
	r := strings.NewReplacer(
		"{format}", format,
		"{lang}", lang,
	)
	return r.Replace(tpl.System), true
}

// composeMaxTokens caps output per action. Title / tag suggestions
// nominally need only a single line, but reasoning models still
// burn a comparable chain-of-thought budget on the way there — see
// the headroom note at the top of ai_compose_prompts.jsonc. The
// fallback (catalogue entry missing or max_tokens omitted) is a
// generous 2048 so a stripped-down JSONC still produces working
// output rather than truncated chain-of-thought.
func composeMaxTokens(action string) int {
	if tpl, ok := composePromptLookup(action); ok && tpl.MaxTokens != nil {
		return *tpl.MaxTokens
	}
	return 2048
}

// composeTemperature trades off determinism. Per-action values live
// in ai_compose_prompts.jsonc; the fallback (0.2) matches the
// previous in-code default for actions that prefer faithful
// reproduction over variety.
func composeTemperature(action string) float64 {
	if tpl, ok := composePromptLookup(action); ok && tpl.Temperature != nil {
		return *tpl.Temperature
	}
	return 0.2
}

// stripJSONCComments removes `//` line comments and `/* … */` block
// comments from JSONC input while preserving comment-like byte
// sequences that appear inside string literals (e.g. a URL containing
// "//" inside a prompt). Trailing commas are NOT handled — strictly
// JSON-with-comments, matching the VS Code JSONC dialect.
func stripJSONCComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	i, n := 0, len(in)
	for i < n {
		c := in[i]
		// Preserve string literals verbatim, including any
		// embedded `/`. JSON strings cannot span lines without
		// escaping, so a stray `"` inside `\"…\"` will not
		// confuse the state machine as long as we honour `\`.
		if c == '"' {
			out = append(out, c)
			i++
			for i < n {
				ch := in[i]
				out = append(out, ch)
				i++
				if ch == '\\' && i < n {
					out = append(out, in[i])
					i++
					continue
				}
				if ch == '"' {
					break
				}
			}
			continue
		}
		if c == '/' && i+1 < n {
			switch in[i+1] {
			case '/':
				for i < n && in[i] != '\n' {
					i++
				}
				continue
			case '*':
				i += 2
				for i+1 < n && !(in[i] == '*' && in[i+1] == '/') {
					i++
				}
				if i+1 < n {
					i += 2
				} else {
					i = n
				}
				continue
			}
		}
		out = append(out, c)
		i++
	}
	return out
}
