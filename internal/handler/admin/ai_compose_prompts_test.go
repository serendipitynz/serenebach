package admin

import "testing"

// TestComposeActionsHaveCatalogueEntries fails if a verb wired into
// composeActions has no corresponding entry in
// ai_compose_prompts.jsonc. Without this guard, adding a new action
// to the dispatch map (or misspelling an existing one) would let
// the HTTP handler reach the provider with an empty system prompt,
// because resolveComposePrompt currently only flags fully-unknown
// actions. We cannot run this check at package init time without
// creating an initialization cycle, so the invariant is enforced by
// CI instead.
func TestComposeActionsHaveCatalogueEntries(t *testing.T) {
	for action := range composeActions {
		key := action
		if key == "summarize" {
			key = "summarise"
		}
		if _, ok := composePrompts[key]; !ok {
			t.Errorf("action %q is wired into composeActions but has no entry in ai_compose_prompts.jsonc", action)
		}
	}
}
