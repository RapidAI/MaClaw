package memory

import (
	"strings"
	"testing"
	"time"
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

func TestFormatRecallEntryForToolIncludesDerivedMetadata(t *testing.T) {
	since := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	out := FormatRecallEntryForTool(Entry{
		Category:    CategoryProjectKnowledge,
		Content:     "schema summary",
		DerivedKind: "schema:recurring",
		EvidenceIDs: []string{"e1", "e2", "e3", "e4", "e5", "e6"},
		Boundary: &MemoryBoundary{
			ProjectPath: "D:/workprj/alpha",
			OwnerID:     "owner-a",
			SourceScope: "conversation",
			Since:       &since,
		},
	})

	for _, want := range []string{
		"derived: kind=schema:recurring",
		"evidence_ids=e1,e2,e3,e4,e5(+1)",
		"project_path=D:/workprj/alpha",
		"owner_id=owner-a",
		"source_scope=conversation",
		"since=2026-05-02T12:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatRecallEntryForTool() missing %q in %q", want, out)
		}
	}
}
