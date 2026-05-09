package main

import (
	"strings"
	"testing"
)

func TestCodingTurnContextAppliesStableIdentity(t *testing.T) {
	task := &TaskItem{Index: 3, Title: "Refactor runner"}
	ctx := newCodingTurnContext("run/42", task, "/project")

	event := ctx.TaskEvent("running", task, "")
	if event.RunID != "run/42" {
		t.Fatalf("expected run id to be preserved, got %q", event.RunID)
	}
	if event.TurnID != "coding-turn-run-42-T3" {
		t.Fatalf("expected sanitized stable turn id, got %q", event.TurnID)
	}
	if event.TaskID != "T3" || event.Title != "Refactor runner" {
		t.Fatalf("expected task metadata, got %#v", event)
	}
}

func TestCodingTurnContextWrapProgressBackfillsStructuredEvents(t *testing.T) {
	ctx := newCodingTurnContext("7", &TaskItem{Index: 2, Title: "Wire tools"}, "/project")
	var progress []string
	wrapped := ctx.WrapProgress(func(text string) {
		progress = append(progress, text)
	})

	emitCodingAgentEvent(wrapped, CodingAgentEvent{
		Version: 1,
		Agent:   "coding",
		Event:   "tool_started",
		Phase:   "running",
		Detail:  "read_file",
	})
	wrapped("plain progress")

	if len(progress) != 2 {
		t.Fatalf("expected structured and plain progress, got %#v", progress)
	}
	if !strings.Contains(progress[0], `"run_id":"7"`) ||
		!strings.Contains(progress[0], `"turn_id":"coding-turn-7-T2"`) ||
		!strings.Contains(progress[0], `"task_id":"T2"`) ||
		!strings.Contains(progress[0], `"title":"Wire tools"`) {
		t.Fatalf("expected wrapped event to include turn identity, got %q", progress[0])
	}
	if progress[1] != "plain progress" {
		t.Fatalf("expected plain progress to pass through, got %#v", progress)
	}
}

func TestCodingTurnContextWrapProgressPreservesToolOutcome(t *testing.T) {
	ctx := newCodingTurnContext("8", &TaskItem{Index: 1, Title: "Run tool"}, "/project")
	var progress []string
	wrapped := ctx.WrapProgress(func(text string) {
		progress = append(progress, text)
	})

	emitCodingAgentEvent(wrapped, CodingAgentEvent{
		Version: 1,
		Agent:   "coding",
		Event:   "tool_finished",
		Phase:   "running",
		Detail:  "bash",
		Outcome: "blocked",
	})

	if len(progress) != 1 {
		t.Fatalf("expected one progress event, got %#v", progress)
	}
	if !strings.Contains(progress[0], `"event":"tool_finished"`) ||
		!strings.Contains(progress[0], `"outcome":"blocked"`) ||
		!strings.Contains(progress[0], `"turn_id":"coding-turn-8-T1"`) {
		t.Fatalf("expected wrapped tool outcome event, got %q", progress[0])
	}
}

func TestFormatCodingAgentEventPreservesZeroCountForCountedEvents(t *testing.T) {
	formatted := formatCodingAgentEvent(CodingAgentEvent{
		Event:   "quality_summary",
		Phase:   "result",
		Outcome: "passed",
		Count:   0,
	})
	if !strings.Contains(formatted, `"count":0`) {
		t.Fatalf("expected zero count to be emitted for counted event, got %q", formatted)
	}

	taskStatus := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status",
		Phase: "running",
		Count: 0,
	})
	if strings.Contains(taskStatus, `"count":0`) {
		t.Fatalf("task status should not emit noisy zero count, got %q", taskStatus)
	}
}

func TestFormatCodingAgentEventPreservesZeroDurationForFinishedTools(t *testing.T) {
	formatted := formatCodingAgentEvent(CodingAgentEvent{
		Event:      "tool_finished",
		Phase:      "running",
		Detail:     "read_file",
		DurationMS: 0,
	})
	if !strings.Contains(formatted, `"duration_ms":0`) {
		t.Fatalf("expected zero duration to be emitted for finished tool, got %q", formatted)
	}

	started := formatCodingAgentEvent(CodingAgentEvent{
		Event:      "tool_started",
		Phase:      "running",
		Detail:     "read_file",
		DurationMS: 0,
	})
	if strings.Contains(started, `"duration_ms":0`) {
		t.Fatalf("tool started should not emit noisy zero duration, got %q", started)
	}
}

func TestCodingDiffSnapshotEvent(t *testing.T) {
	snapshot := newCodingDiffSnapshot(
		[]string{"z.go", "a.go"},
		[]string{"new.go", "a.go"},
		"diff --git a/a.go b/a.go\n+changed",
	)
	event := newCodingAgentDiffSummaryEvent(&TaskItem{Index: 8, Title: "Diff task"}, "", snapshot)

	if event.Event != "diff_summary" || event.Phase != "result" {
		t.Fatalf("unexpected diff event identity: %#v", event)
	}
	if event.Count != 3 {
		t.Fatalf("expected 3 changed files, got %d", event.Count)
	}
	if got := strings.Join(event.Files, ","); got != "a.go,new.go,z.go" {
		t.Fatalf("expected sorted unique files, got %q", got)
	}
	if !strings.Contains(event.Detail, "3 files") || !strings.Contains(event.Detail, "2 created") {
		t.Fatalf("expected diff detail to summarize counts, got %q", event.Detail)
	}
}
