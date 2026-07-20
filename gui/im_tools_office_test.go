package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeResolvedPathUsesTaskWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	app.ensureMemoryStore()
	created := app.CreateTask("OfficePath", "")
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	ws := filepath.Join(created.ProjectPath, "workspace")
	target := filepath.Join(ws, "note.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &IMMessageHandler{app: app}
	owner := desktopUserID + ":" + created.ProjectPath
	got := h.officeResolvedPathForOwner("note.txt", owner)
	if filepath.Clean(got) != filepath.Clean(target) {
		t.Fatalf("office relative resolve = %q, want %q", got, target)
	}
	abs := filepath.Join(t.TempDir(), "outside.txt")
	gotAbs := h.officeResolvedPathForOwner(abs, owner)
	if filepath.Clean(gotAbs) != filepath.Clean(abs) {
		t.Fatalf("office absolute resolve = %q, want %q", gotAbs, abs)
	}
}

func TestToolOfficeReadDocumentFromTaskWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	app.ensureMemoryStore()
	created := app.CreateTask("ReadInTask", "")
	ws := filepath.Join(created.ProjectPath, "workspace")
	docx := filepath.Join(ws, "a.docx")
	writeMinimalDOCXForOfficeTest(t, docx, "task-workspace-body")

	owner := desktopUserID + ":" + created.ProjectPath
	h := &IMMessageHandler{
		app: app,
		currentLoopCtx: &LoopContext{
			Runtime: RuntimeContext{
				RequestID:     "req-office-read",
				PolicyOwnerID: owner,
			},
		},
	}

	out := h.toolOffice(map[string]interface{}{
		"action": "read_document",
		"path":   "a.docx",
	})
	if strings.Contains(out, "读取失败") || strings.Contains(out, "文件不存在") {
		t.Fatalf("expected successful read, got: %s", out)
	}
	if !strings.Contains(out, "task-workspace-body") {
		t.Fatalf("expected extracted body, got: %s", out)
	}
}

func writeMinimalDOCXForOfficeTest(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document.xml: %v", err)
	}
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + escaped + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}
