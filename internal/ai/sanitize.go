package ai

import (
	"regexp"
	"strings"
)

// SanitizeInfo records what sanitizeAssistantText stripped from a
// provider's raw output. The audit log surfaces these flags + the
// before/after lengths so an operator debugging a "the model spat out
// garbage" report can tell, without re-running the call, whether the
// model leaked Harmony control tokens, a <think> block, etc.
type SanitizeInfo struct {
	HarmonyFinal  bool // a <|channel|>final<|message|> marker was used to slice the answer
	ThinkBlock    bool // one or more <think>/<thinking> blocks were stripped
	HarmonyTokens bool // stray <|...|> control tokens were stripped
	RawLen        int  // len of the input before sanitisation
	CleanLen      int  // len of the output after sanitisation
}

// Stripped reports whether the sanitiser changed anything material.
// Used by the audit log to decide whether to mention the sanitiser at
// all in the per-call extras.
func (s SanitizeInfo) Stripped() bool {
	return s.HarmonyFinal || s.ThinkBlock || s.HarmonyTokens
}

var (
	// Harmony "final channel" marker — emitted by gpt-oss / llm-jp
	// thinking variants when LM Studio fails to separate reasoning
	// from the final answer. We slice on the LAST occurrence so a
	// model that reasons → answers in the same content slot ends up
	// with just the answer.
	harmonyFinalRE = regexp.MustCompile(`(?s)<\|channel\|>\s*final\s*<\|message\|>`)

	// <think>/<thinking> blocks (DeepSeek-R1, Qwen-thinking, etc.).
	// Non-greedy across newlines.
	thinkBlockRE = regexp.MustCompile(`(?is)<think(?:ing)?>.*?</think(?:ing)?>`)

	// Stray Harmony control tokens like <|end|>, <|start|>assistant,
	// <|return|>, <|message|>, etc. left behind once the channel
	// marker has been consumed.
	harmonyTokenRE = regexp.MustCompile(`<\|[^|>]*\|>`)
)

// sanitizeAssistantText cleans provider-returned text of common
// reasoning-model artefacts. Idempotent: a second pass over already-
// clean text is a no-op and returns SanitizeInfo with all flags false.
func sanitizeAssistantText(s string) (string, SanitizeInfo) {
	info := SanitizeInfo{RawLen: len(s)}
	out := s

	if loc := harmonyFinalRE.FindAllStringIndex(out, -1); len(loc) > 0 {
		last := loc[len(loc)-1]
		out = out[last[1]:]
		info.HarmonyFinal = true
	}
	if thinkBlockRE.MatchString(out) {
		out = thinkBlockRE.ReplaceAllString(out, "")
		info.ThinkBlock = true
	}
	if harmonyTokenRE.MatchString(out) {
		out = harmonyTokenRE.ReplaceAllString(out, "")
		info.HarmonyTokens = true
	}

	out = strings.TrimSpace(out)
	info.CleanLen = len(out)
	return out, info
}
