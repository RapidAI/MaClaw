package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	owner := projectSessionOwnerID(created.ProjectPath)
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

	owner := projectSessionOwnerID(created.ProjectPath)
	h := &IMMessageHandler{
		app: app,
		currentLoopCtx: &LoopContext{
			Runtime: RuntimeContext{
				RequestID:     "req-office-read",
				PolicyOwnerID: owner,
			},
		},
	}
	if got := h.currentRuntimeOrLegacyPolicyOwnerID(); got != owner {
		t.Fatalf("runtime owner = %q, want %q", got, owner)
	}
	if got := h.officeResolvedPathForOwner("a.docx", owner); filepath.Clean(got) != filepath.Clean(docx) {
		t.Fatalf("resolved office path = %q, want %q", got, docx)
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

func TestToolOfficeHonorsAssistantBindingFileBoundary(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	outsideDir := filepath.Join(root, "outside")
	for _, dir := range []string{workDir, outsideDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeMinimalDOCXForOfficeTest(t, filepath.Join(workDir, "inside.docx"), "inside-office-body")
	outsideDoc := filepath.Join(outsideDir, "outside.docx")
	writeMinimalDOCXForOfficeTest(t, outsideDoc, "outside-office-body")

	owner := "lansenger:bot-support:office-boundary"
	cleanup, errText := bindAssistantForTurn(IMUserMessage{
		UserID: owner,
		AssistantBinding: &agent.AssistantBinding{
			BotProfileID:     "support",
			WorkingDirectory: workDir,
		},
	})
	if errText != "" {
		t.Fatalf("bind assistant: %s", errText)
	}
	defer cleanup()

	h := &IMMessageHandler{}
	inside := h.toolOffice(map[string]interface{}{
		"action":                         "read_document",
		"path":                           "inside.docx",
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(inside, "inside-office-body") {
		t.Fatalf("relative Office path should resolve in profile workdir, got: %s", inside)
	}

	outside := h.toolOffice(map[string]interface{}{
		"action":                         "read_document",
		"file_path":                      outsideDoc,
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(outside, "outside its authorized directories") {
		t.Fatalf("Office path outside bot profile boundary was accepted: %s", outside)
	}

	writeOutside := h.toolOffice(map[string]interface{}{
		"action":                         "write_excel",
		"file_path":                      filepath.Join(outsideDir, "outside.xlsx"),
		"data":                           map[string]interface{}{"sheets": []interface{}{}},
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(writeOutside, "outside its authorized directories") {
		t.Fatalf("write_excel outside bot profile boundary was accepted: %s", writeOutside)
	}
}

func TestToolOfficeAllowsAssistantBindingAllDirectories(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	outsideDir := filepath.Join(root, "outside")
	for _, dir := range []string{workDir, outsideDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outsideDoc := filepath.Join(outsideDir, "outside.docx")
	writeMinimalDOCXForOfficeTest(t, outsideDoc, "allow-all-office-body")

	owner := "lansenger:bot-support:office-allow-all"
	cleanup, errText := bindAssistantForTurn(IMUserMessage{
		UserID: owner,
		AssistantBinding: &agent.AssistantBinding{
			BotProfileID:        "support",
			WorkingDirectory:    workDir,
			AllowAllDirectories: true,
		},
	})
	if errText != "" {
		t.Fatalf("bind assistant: %s", errText)
	}
	defer cleanup()

	out := (&IMMessageHandler{}).toolOffice(map[string]interface{}{
		"action":                         "read_document",
		"file_path":                      outsideDoc,
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(out, "allow-all-office-body") {
		t.Fatalf("allow-all profile should retain absolute Office path behavior, got: %s", out)
	}
}

func TestToolOfficeRejectsSymlinkedOutputPathOutsideAssistantBinding(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(workDir, "linked-output")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	owner := "lansenger:bot-support:office-symlink-output"
	cleanup, errText := bindAssistantForTurn(IMUserMessage{
		UserID: owner,
		AssistantBinding: &agent.AssistantBinding{
			BotProfileID:     "support",
			WorkingDirectory: workDir,
		},
	})
	if errText != "" {
		t.Fatalf("bind assistant: %s", errText)
	}
	defer cleanup()

	out := (&IMMessageHandler{}).toolOffice(map[string]interface{}{
		"action":                         "write_excel",
		"file_path":                      filepath.Join("linked-output", "outside.xlsx"),
		"data":                           map[string]interface{}{"sheets": []interface{}{}},
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(out, "outside its authorized directories") {
		t.Fatalf("Office write through symlinked parent escaped profile boundary: %s", out)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "outside.xlsx")); !os.IsNotExist(err) {
		t.Fatalf("Office write created external file through symlink: %v", err)
	}
}

func TestToolOfficeRejectsMissingExplicitRuntimeOwner(t *testing.T) {
	if !toolAcceptsRuntimePolicyOwnerArg("office") {
		t.Fatal("office must receive the hidden runtime owner at execution")
	}
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Runtime: RuntimeContext{
		RequestID:     "req-office-missing-owner",
		PolicyOwnerID: desktopUserID,
	}}}
	out := h.toolOffice(map[string]interface{}{
		"action":                         "read_document",
		"path":                           "does-not-matter.docx",
		registeredToolPolicyOwnerIDField: "",
	})
	if !strings.Contains(out, "runtime owner is missing") {
		t.Fatalf("Office tool should fail closed instead of using desktop owner, got: %s", out)
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
