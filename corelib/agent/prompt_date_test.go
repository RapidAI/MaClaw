package agent

import (
	"strings"
	"testing"
	"time"
)

func TestPromptCurrentDateLine(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	got := promptCurrentDateLine(time.Date(2026, 10, 23, 2, 30, 0, 0, loc))
	for _, want := range []string{"2026-10-23", "Friday", "+08:00", "most recent occurrence"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(BuildPromptBundle(SystemPromptDeps{}, "hi", true).SessionContext, "Current date: ") {
		t.Fatal("session context has no date")
	}
}
