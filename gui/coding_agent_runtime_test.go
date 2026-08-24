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
		Version:  1,
		Agent:    "coding",
		Event:    "tool_finished",
		Phase:    "running",
		Detail:   "bash",
		Outcome:  "blocked",
		Severity: "diagnostic",
	})

	if len(progress) != 1 {
		t.Fatalf("expected one progress event, got %#v", progress)
	}
	if !strings.Contains(progress[0], `"event":"tool_finished"`) ||
		!strings.Contains(progress[0], `"outcome":"blocked"`) ||
		!strings.Contains(progress[0], `"severity":"diagnostic"`) ||
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
	if len(event.FileChanges) != 3 {
		t.Fatalf("expected file change rows for the table, got %#v", event.FileChanges)
	}
}

func TestCodingDiffSnapshotEventIncludesLineStats(t *testing.T) {
	snapshot := newCodingDiffSnapshot([]string{"src/auth/login.go", "src/new.go"}, []string{"src/new.go"}, "")
	stat := &SubAgentDiffStat{
		FilesChanged: 2,
		Insertions:   48,
		Deletions:    6,
		FileStats: []SubAgentFileDiffStat{
			{Path: "src/auth/login.go", Insertions: 12, Deletions: 6},
			{Path: "src/new.go", Insertions: 36, Deletions: 0},
		},
	}
	event := newCodingAgentDiffSummaryEventWithStat(&TaskItem{Index: 9, Title: "Stat task"}, "", snapshot, stat)
	if event.Added != 48 || event.Removed != 6 || !strings.Contains(event.Detail, "+48 -6") {
		t.Fatalf("expected aggregate line stats on the event, got %#v", event)
	}
	if len(event.FileChanges) != 2 {
		t.Fatalf("expected two file rows, got %#v", event.FileChanges)
	}
	if event.FileChanges[0].Path != "src/auth/login.go" || event.FileChanges[0].Added != 12 || event.FileChanges[0].Removed != 6 {
		t.Fatalf("login.go row = %#v", event.FileChanges[0])
	}
	if event.FileChanges[1].Path != "src/new.go" || event.FileChanges[1].Added != 36 || event.FileChanges[1].Removed != 0 {
		t.Fatalf("new.go row = %#v", event.FileChanges[1])
	}
}

func TestCodingAgentDisplayPathRelativizesAbsAndRemote(t *testing.T) {
	if got := codingAgentDisplayPath(`D:\workprj\demo\hello_world.cpp`, `D:\workprj\demo`); got != "hello_world.cpp" {
		t.Fatalf("local root-relative = %q", got)
	}
	if got := codingAgentDisplayPath(`D:\workprj\demo\src\main.go`, `D:\workprj\demo`); got != "src/main.go" {
		t.Fatalf("local nested = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/app/src/main.go", "/opt/app"); got != "src/main.go" {
		t.Fatalf("remote unix = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/app/src/pkg/foo.go", "/opt/app"); got != "src/pkg/foo.go" {
		t.Fatalf("deep relative path must stay intact for preview: %q", got)
	}
	if got := codingAgentDisplayPath(`D:\Work\Demo\src\main.go`, `d:\work\demo`); got != "src/main.go" {
		t.Fatalf("case-insensitive root = %q", got)
	}
	if got := codingAgentDisplayPath("D:/workprj/demo/hello_world.cpp", ""); got != "D:/workprj/demo/hello_world.cpp" {
		t.Fatalf("unrooted abs path must stay resolvable: %q", got)
	}
	if got := codingAgentDisplayPath(`D:\workprj\demo\.\src\..\src\main.go`, `D:\workprj\demo\`); got != "src/main.go" {
		t.Fatalf("dot segments and trailing root slash = %q", got)
	}
	if got := codingAgentDisplayPath("/opt/App/foo.go", "/opt/app"); got != "/opt/App/foo.go" {
		t.Fatalf("remote linux paths must stay case-sensitive: %q", got)
	}
	if got := codingAgentDisplayPath("/opt2/foo.go", "/opt"); got != "/opt2/foo.go" {
		t.Fatalf("prefix must require a path boundary: %q", got)
	}
	if got := codingAgentDisplayPath("D:/hello.cpp", "D:/"); got != "hello.cpp" {
		t.Fatalf("drive-root project: %q", got)
	}
	if got := codingAgentDisplayPath("/foo.go", "/"); got != "foo.go" {
		t.Fatalf("unix-root project: %q", got)
	}
	files := codingToolEventFiles("write_file", `{"path":"D:\\workprj\\demo\\hello_world.cpp"}`, `D:\workprj\demo`)
	if len(files) != 1 || files[0] != "hello_world.cpp" {
		t.Fatalf("tool event files = %#v", files)
	}
}

func TestSuppressCodingWorkbenchStatusProgress(t *testing.T) {
	var got []string
	passthrough := suppressCodingWorkbenchStatusProgress(func(s string) { got = append(got, s) }, false)
	passthrough("[Status] Task received")
	passthrough("Coding Agent: running T1")
	hidden := suppressCodingWorkbenchStatusProgress(func(s string) { got = append(got, s) }, true)
	hidden("[Status] Task received")
	hidden("Coding Agent Event: {\"event\":\"tool_started\"}")
	if len(got) != 3 {
		t.Fatalf("unexpected progress: %#v", got)
	}
	if got[0] != "[Status] Task received" || !strings.HasPrefix(got[1], "Coding Agent:") || !strings.HasPrefix(got[2], "Coding Agent Event:") {
		t.Fatalf("coding workbench must drop only [Status]: %#v", got)
	}
	if suppressCodingWorkbenchStatusProgress(nil, true) != nil {
		t.Fatal("nil callback should stay nil")
	}
}

func TestAttachCodingToolFileChangesFromWriteAndEditArgs(t *testing.T) {
	write := CodingAgentEvent{Files: []string{"hello.cpp"}}
	attachCodingToolFileChanges(&write, "write_file", `{"path":"hello.cpp","content":"a\nb\nc\n"}`, nil)
	if write.Added != 3 || write.Removed != 0 || len(write.FileChanges) != 1 || write.FileChanges[0].Path != "hello.cpp" {
		t.Fatalf("write estimate = %#v", write)
	}
	edit := CodingAgentEvent{Files: []string{"a.go"}}
	attachCodingToolFileChanges(&edit, "ssh_edit_file", `{"path":"a.go","old_str":"x\ny","new_str":"x\ny\nz"}`, nil)
	if edit.Added != 1 || edit.Removed != 0 {
		t.Fatalf("edit estimate = %#v", edit)
	}
	read := CodingAgentEvent{Files: []string{"a.go"}}
	attachCodingToolFileChanges(&read, "read_file", `{"path":"a.go"}`, nil)
	if len(read.FileChanges) != 0 {
		t.Fatalf("read should not get line stats: %#v", read)
	}
}
