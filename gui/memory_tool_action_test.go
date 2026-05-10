package main

import "testing"

func TestMemoryToolThemesIsRecallOnlyAllowed(t *testing.T) {
	if got := normalizeMemoryToolAction("themes"); got != memoryToolActionThemes {
		t.Fatalf("normalize themes = %q", got)
	}
	if !memoryToolActionThemes.IsRecallOnlyAllowed() {
		t.Fatal("themes should be allowed in recall-only contexts")
	}
	if memoryToolActionSave.IsRecallOnlyAllowed() {
		t.Fatal("save should not be allowed in recall-only contexts")
	}
}
