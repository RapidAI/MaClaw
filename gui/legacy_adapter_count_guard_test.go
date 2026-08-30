package main

import (
	"strings"
	"testing"
)

// TestClosedLegacyReplacementPrunesRouterTailUnderCountGuard pins the
// production failure found in the 2026-08-24 full-suite audit: the router
// selects up to MaxToolBudget tools and the post-route pipeline then adds a
// few mandatory tools (channel delivery, result reader). The closed
// replacement used to reject the whole plan as plan_over_budget, emptying
// the model surface. The guard must instead prune the router's lowest-ranked
// retrieval candidates and keep every mandatory selection.
func TestClosedLegacyReplacementPrunesRouterTailUnderCountGuard(t *testing.T) {
	bootstrap := []string{"task", "async_wait", "compress_context"}
	candidates := []string{
		"git_status", "git_diff", "git_commit", "git_push", "office", "generate_pdf",
		"web_search", "web_fetch", "download_file", "knowledge_search", "knowledge_stats",
		"knowledge_health", "knowledge_entity_profile", "knowledge_import_status",
		"computer_click", "computer_type", "computer_observe", "computer_wait",
		"computer_find", "computer_focus", "computer_key", "ocr_recognize",
		"search_files", "session_search",
	}
	additions := []string{"im_message", "send_to_im", "send_file"}

	ranked := append(append([]string{}, bootstrap...), candidates...)
	definitions := make([]map[string]interface{}, 0, len(ranked)+len(additions))
	for _, name := range ranked {
		definitions = append(definitions, legacyReviewedDefinition(name))
	}
	for _, name := range additions {
		definitions = append(definitions, legacyReviewedDefinition(name))
	}
	// 3 bootstrap + 24 candidates + 3 additions = 30 > MaxToolBudget (28).
	rendered, planBacked, err := renderReviewedLegacySurface("send the report to the IM group", definitions, ranked)
	if err != nil || !planBacked {
		t.Fatalf("over-guard surface with prunable candidates must render: planBacked=%v err=%v", planBacked, err)
	}
	names := toolNameSetForWorkflowFilterTest(rendered)
	if len(rendered) != 28 {
		t.Fatalf("rendered %d tools, want exactly the count guard 28", len(rendered))
	}
	for _, name := range append(append([]string{}, bootstrap...), additions...) {
		if !names[name] {
			t.Fatalf("mandatory tool %q was pruned: %#v", name, names)
		}
	}
	// The two lowest-ranked retrieval candidates are the pruning victims.
	for _, name := range []string{"search_files", "session_search"} {
		if names[name] {
			t.Fatalf("lowest-ranked candidate %q should have been pruned: %#v", name, names)
		}
	}
	// The remaining candidates survive.
	if !names["git_status"] || !names["ocr_recognize"] {
		t.Fatalf("higher-ranked candidates must survive pruning: %#v", names)
	}
}

// TestClosedLegacyReplacementRejectsOverGuardMandatorySurface keeps the
// fail-closed property: when required selections alone exceed the count
// guard there is nothing optional to prune and the plan must be rejected,
// never silently widened.
func TestClosedLegacyReplacementRejectsOverGuardMandatorySurface(t *testing.T) {
	names := []string{
		"task", "async_wait", "compress_context", "git_status", "git_diff", "git_commit",
		"git_push", "office", "generate_pdf", "web_search", "web_fetch", "download_file",
		"knowledge_search", "knowledge_stats", "knowledge_health", "knowledge_entity_profile",
		"knowledge_import_status", "computer_click", "computer_type", "computer_observe",
		"computer_wait", "computer_find", "computer_focus", "computer_key", "ocr_recognize",
		"search_files", "session_search", "im_message", "send_to_im",
	}
	definitions := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, legacyReviewedDefinition(name))
	}
	// 29 mandatory selections (nil ranked list: nothing is a prunable retrieval
	// candidate) exceed the guard.
	rendered, planBacked, err := renderReviewedLegacySurface("test", definitions, nil)
	if err == nil || planBacked || len(rendered) != 0 {
		t.Fatalf("over-guard all-mandatory surface must be rejected: rendered=%d planBacked=%v err=%v", len(rendered), planBacked, err)
	}
	if !strings.Contains(err.Error(), "plan_over_budget") {
		t.Fatalf("error = %v, want plan_over_budget", err)
	}
}
