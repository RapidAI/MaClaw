package main

import "testing"

func TestMemoryToolThemesIsRecallOnlyAllowed(t *testing.T) {
	if got := normalizeMemoryToolAction("themes"); got != memoryToolActionThemes {
		t.Fatalf("normalize themes = %q", got)
	}
	if !memoryToolActionThemes.IsRecallOnlyAllowed() {
		t.Fatal("themes should be allowed in recall-only contexts")
	}
	if got := normalizeMemoryToolAction("memory_candidates"); got != memoryToolActionCandidates {
		t.Fatalf("normalize memory_candidates = %q", got)
	}
	if !memoryToolActionCandidates.IsRecallOnlyAllowed() {
		t.Fatal("candidates should be allowed in recall-only contexts")
	}
	if memoryToolActionSave.IsRecallOnlyAllowed() {
		t.Fatal("save should not be allowed in recall-only contexts")
	}
}
