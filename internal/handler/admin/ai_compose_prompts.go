package admin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// composePromptsJSON is the canonical system-prompt catalogue for the
// /admin/ai/compose endpoint. Edit ai_compose_prompts.json to tweak
// the wording; the embed ships the file with the binary so no extra
// I/O happens at runtime. Action keys mirror composeActions; the
// summarize/summarise alias is collapsed at lookup time so the JSON
// only needs one entry under "summarise".
//
//go:embed ai_compose_prompts.json
var composePromptsJSON []byte

// composePromptTemplate is a parsed prompt entry. system holds the
// system message with `{format}` / `{lang}` placeholders still
// embedded; resolution happens in composePromptSystem.
type composePromptTemplate struct {
	System string `json:"system"`
}

// composePrompts is the lazily-decoded catalogue. Parsing happens
// once at package init; a malformed JSON file is treated as a build
// bug and panics so the regression is caught loudly in tests.
var composePrompts = mustLoadComposePrompts()

func mustLoadComposePrompts() map[string]composePromptTemplate {
	out := map[string]composePromptTemplate{}
	if err := json.Unmarshal(composePromptsJSON, &out); err != nil {
		panic(fmt.Errorf("ai_compose_prompts.json: %w", err))
	}
	return out
}

// composePromptSystem returns the resolved system prompt for one
// compose action. Unknown actions return an empty string + false so
// callers can fall through to an "unknown_action" error without an
// extra map lookup on their side.
//
// Placeholders supported in the JSON file:
//
//	{format} → "markdown" | "html"   (raw value, lowercased upstream)
//	{lang}   → "Japanese" | "English" (display name from langName)
func composePromptSystem(action, format, lang string) (string, bool) {
	key := action
	if key == "summarize" {
		key = "summarise"
	}
	tpl, ok := composePrompts[key]
	if !ok {
		return "", false
	}
	r := strings.NewReplacer(
		"{format}", format,
		"{lang}", lang,
	)
	return r.Replace(tpl.System), true
}
