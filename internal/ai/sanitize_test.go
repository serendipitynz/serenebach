package ai

import "testing"

func TestSanitizeAssistantTextHarmonyFinal(t *testing.T) {
	// llm-jp-4-8b-thinking-style leak: reasoning + Harmony channel
	// marker + final answer all packed into the content field. The
	// prefix (including stray <|end|>/<|start|> tokens) is sliced
	// away as part of the Harmony-final cut, so only HarmonyFinal
	// gets flagged — HarmonyTokens covers the trailing-tokens case.
	raw := " We need to rewrite passage. Let's produce.\n\nRewrite: \"draft\"\n\nFinalize.<|end|><|start|>assistant<|channel|>final<|message|> 最終回答です。"
	out, info := sanitizeAssistantText(raw)

	if !info.HarmonyFinal {
		t.Errorf("HarmonyFinal flag not set")
	}
	if out != "最終回答です。" {
		t.Errorf("out = %q, want %q", out, "最終回答です。")
	}
	if info.RawLen <= info.CleanLen {
		t.Errorf("RawLen=%d should exceed CleanLen=%d after stripping", info.RawLen, info.CleanLen)
	}
}

func TestSanitizeAssistantTextStripsTrailingHarmonyTokens(t *testing.T) {
	// Some thinking-mode outputs include trailing Harmony control
	// tokens (<|end|>, <|return|>) after the final answer with no
	// preceding channel marker. Those are the HarmonyTokens-flag case.
	raw := "本文だけ採用してほしい。<|end|>"
	out, info := sanitizeAssistantText(raw)
	if info.HarmonyFinal {
		t.Errorf("HarmonyFinal flag should not be set when no channel marker")
	}
	if !info.HarmonyTokens {
		t.Errorf("HarmonyTokens flag not set; info=%+v", info)
	}
	if out != "本文だけ採用してほしい。" {
		t.Errorf("out = %q", out)
	}
}

func TestSanitizeAssistantTextThinkBlock(t *testing.T) {
	raw := "<think>this is the chain of thought</think>本文だけ残るべき。"
	out, info := sanitizeAssistantText(raw)
	if !info.ThinkBlock {
		t.Errorf("ThinkBlock flag not set")
	}
	if out != "本文だけ残るべき。" {
		t.Errorf("out = %q", out)
	}
}

func TestSanitizeAssistantTextThinkingTagVariant(t *testing.T) {
	raw := "<thinking>foo</thinking>本文。"
	out, info := sanitizeAssistantText(raw)
	if !info.ThinkBlock {
		t.Errorf("ThinkBlock flag not set for <thinking>")
	}
	if out != "本文。" {
		t.Errorf("out = %q", out)
	}
}

func TestSanitizeAssistantTextNoOpOnCleanInput(t *testing.T) {
	raw := "これは普通の本文です。"
	out, info := sanitizeAssistantText(raw)
	if out != raw {
		t.Errorf("out = %q, want unchanged", out)
	}
	if info.Stripped() {
		t.Errorf("Stripped() = true on clean input; flags=%+v", info)
	}
}

func TestSanitizeAssistantTextHarmonyFinalTakesLastOccurrence(t *testing.T) {
	// If the model emits the marker more than once (it shouldn't,
	// but observed in the wild), we want the LAST chunk — that's the
	// canonical "final" answer.
	raw := "<|channel|>final<|message|> first try.<|end|><|channel|>final<|message|> second try."
	out, _ := sanitizeAssistantText(raw)
	if out != "second try." {
		t.Errorf("out = %q, want %q", out, "second try.")
	}
}

func TestSanitizeAssistantTextStripsLeadingTrailingWhitespace(t *testing.T) {
	raw := "   \n本文\n\n  "
	out, _ := sanitizeAssistantText(raw)
	if out != "本文" {
		t.Errorf("out = %q", out)
	}
}
