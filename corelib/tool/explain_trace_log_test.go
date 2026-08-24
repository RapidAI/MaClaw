package tool

import (
	"strings"
	"testing"
)

func TestFormatExplainTraceLinesOmitsEmptyTrace(t *testing.T) {
	if got := FormatExplainTraceLines(ExplainTrace{}); len(got) != 0 {
		t.Fatalf("empty trace lines=%#v", got)
	}
}

func TestFormatExplainTraceLinesStripsControlsAndCapsEvents(t *testing.T) {
	events := make([]TraceEvent, explainTraceLogEventLimit+4)
	for i := range events {
		events[i] = TraceEvent{Stage: TraceStageSemantics, Subject: "information.search.web", Event: "recognized", ReasonCode: "need_required"}
	}
	events[0].Subject = "information.search.web\nC:\\secret\\key.txt"
	got := FormatExplainTraceLines(ExplainTrace{
		PlanID: "plan-1", SnapshotDigest: "snap-1", Events: events,
	})
	if len(got) != 1+explainTraceLogEventLimit {
		t.Fatalf("lines=%d want header+%d", len(got), explainTraceLogEventLimit)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "[explain-trace] plan=plan-1") {
		t.Fatalf("missing header: %q", joined)
	}
	if strings.Contains(got[1], "\n") || strings.Contains(got[1], "\r") {
		t.Fatalf("control chars survived: %q", got[1])
	}
	if strings.Contains(joined, "user said please ssh") {
		t.Fatalf("unexpected user text: %q", joined)
	}
}

func TestFormatExplainTraceLinesTruncatesLongTokens(t *testing.T) {
	long := strings.Repeat("a", explainTraceTokenMaxRunes+20)
	got := FormatExplainTraceLines(ExplainTrace{
		PlanID: long, SnapshotDigest: "snap",
		Events: []TraceEvent{{Stage: TraceStageCatalog, Subject: long, Event: "frozen", ReasonCode: "snapshot_bound"}},
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, long) {
		t.Fatalf("long token was not truncated: %d", len(joined))
	}
	if !strings.Contains(got[0], strings.Repeat("a", explainTraceTokenMaxRunes)) {
		t.Fatalf("truncated plan id missing: %q", got[0])
	}
}
