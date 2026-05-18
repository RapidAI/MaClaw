package memory

import (
	"strings"
	"testing"
)

func TestFormatRecallEntryForToolIncludesSourceDrillDown(t *testing.T) {
	sourcePath := "/tmp/memory_refs/note.md"
	out := FormatRecallEntryForTool(Entry{
		Category:   CategoryTaskArtifact,
		Content:    "migration notes",
		SourceType: "conversation_trim_ref",
		SourceURL:  sourcePath,
	})

	for _, want := range []string{"source_type=conversation_trim_ref", "source_url=" + sourcePath, "drill_down: use read_file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatRecallEntryForTool() missing %q in %q", want, out)
		}
	}
}

func TestFormatRecallEntryForPromptCompactsSourceHint(t *testing.T) {
	out := FormatRecallEntryForPrompt(Entry{
		Content:   "abcdefghijklmnopqrstuvwxyz",
		SourceURL: "/tmp/memory_refs/full.md",
	}, 8)

	if !strings.Contains(out, "abcdefgh...") {
		t.Fatalf("expected truncated content, got %q", out)
	}
	if !strings.Contains(out, "source: /tmp/memory_refs/full.md; full: read_file") {
		t.Fatalf("expected compact source hint, got %q", out)
	}
}
