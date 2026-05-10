package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestEnsureTruncationFallbackToolsAddsRealAlternates(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"write_file"}, map[string]bool{"write_file": true})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["craft_tool"] {
		t.Fatalf("craft_tool fallback missing after write_file block: %v", names)
	}
	if names["bash"] {
		t.Fatalf("bash should not be re-added from the fallback catalog: %v", names)
	}
}

func TestEnsureTruncationFallbackToolsDoesNotAddUnroutedFallback(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"write_file"}, map[string]bool{"write_file": true})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if names["craft_tool"] || names["bash"] {
		t.Fatalf("fallback tools should not be added when they were not routed: %v", names)
	}
}

func TestEnsureTruncationFallbackToolsDoesNotDuplicate(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"generate_pdf", "edit_file"}, nil)
	if len(got) != len(current) {
		t.Fatalf("got %d tools, want no duplicates (%d)", len(got), len(current))
	}
}

func TestEnsureTruncationFallbackToolsRespectsBlockedSet(t *testing.T) {
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(nil, catalog, []string{"write_file"}, map[string]bool{
		"write_file":   true,
		"craft_tool":   true,
		"generate_pdf": true,
	})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if names["craft_tool"] {
		t.Fatalf("craft_tool was re-added despite blocked set: %v", names)
	}
	if names["bash"] {
		t.Fatalf("bash should not be added as truncation fallback: %v", names)
	}
}

func TestBuildTruncationRetryHintOnlyMentionsAvailableAlternates(t *testing.T) {
	got := buildTruncationRetryHint("write_file", []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	})
	if containsText(got, "craft_tool") || containsText(got, "bash") {
		t.Fatalf("hint mentioned unavailable alternates: %q", got)
	}

	got = buildTruncationRetryHint("write_file", []map[string]interface{}{
		toolDef("craft_tool", "craft", nil, nil),
	})
	if !containsText(got, "craft_tool") || containsText(got, "bash") {
		t.Fatalf("hint did not match available alternates: %q", got)
	}
}

func TestBuildTruncationBlockInstructionsOnlyMentionsAvailableAlternates(t *testing.T) {
	got := buildTruncationBlockAlternativeInstructions([]string{"write_file"}, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
	})
	if containsText(got, "craft_tool") || containsText(got, "bash") {
		t.Fatalf("block instruction mentioned unavailable alternates: %q", got)
	}

	got = buildTruncationBlockAlternativeInstructions([]string{"write_file"}, []map[string]interface{}{
		toolDef("craft_tool", "craft", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	if !containsText(got, "craft_tool") || !containsText(got, "bash") {
		t.Fatalf("block instruction missing available alternates: %q", got)
	}
}

func containsText(s, sub string) bool {
	return strings.Contains(s, sub)
}
