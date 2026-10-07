package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

// A read must never force the right-hand code preview open or steal the view:
// while the pane is already visible the read fills a background tab; when it
// is closed it stays closed. Only create/modify events carry the open flags,
// so both must be zero on a successful read_file preview event.
func TestEmitReadFilePreviewDoesNotForceOpenThePane(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.h"), []byte("// sysinfo report\n#ifndef SYSINFO_REPORT_H\n#define SYSINFO_REPORT_H\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	var captured []CodeFileEvent
	app.codePreviewEventObserver = func(evt CodeFileEvent) { captured = append(captured, evt) }
	app.codeEventEmitter = NewCodeEventEmitter(app)
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			projectPath:     dir,
			fullEnvironment: true,
			handler:         &IMMessageHandler{app: app},
		},
		task: &TaskItem{Index: 1, Title: "build the sysinfo tool"},
	}

	cb.emitReadFilePreview("report.h")

	if len(captured) != 1 {
		t.Fatalf("captured %d events, want exactly one read event", len(captured))
	}
	evt := captured[0]
	if evt.OpType != "read" || evt.FilePath != "report.h" {
		t.Fatalf("unexpected event: op=%q file=%q", evt.OpType, evt.FilePath)
	}
	if evt.ForceOpen || evt.AutoOpenPreview {
		t.Fatalf("read event must not open the pane: force_open=%v auto_open_preview=%v", evt.ForceOpen, evt.AutoOpenPreview)
	}
	if evt.ProjectPath != dir {
		t.Fatalf("ProjectPath = %q, want the local tab path %q", evt.ProjectPath, dir)
	}
	if evt.Content == "" {
		t.Fatal("read event should carry the file content from disk")
	}
}

// Source preview stays scoped to implementation work. An inquiry task only
// reads while answering, so it must emit no read preview events at all.
func TestEmitReadFilePreviewSkipsInquiryTasks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# project notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	var captured []CodeFileEvent
	app.codePreviewEventObserver = func(evt CodeFileEvent) { captured = append(captured, evt) }
	app.codeEventEmitter = NewCodeEventEmitter(app)
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			projectPath:     dir,
			fullEnvironment: true,
			handler:         &IMMessageHandler{app: app},
		},
		task: &TaskItem{Index: 1, Title: "explain the project", RequestKind: codingRequestInquiry},
	}

	cb.emitReadFilePreview("notes.md")

	if len(captured) != 0 {
		t.Fatalf("inquiry task must not emit read preview events, got %d", len(captured))
	}
}
