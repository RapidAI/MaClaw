package main

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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

func TestToolOfficeUsesTrustedRoutedContextBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routed-context.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("\u4e2d", agent.DocumentReadMaxRunesForContext(400_000)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{ContextLength: 8_000}
	}}}
	args := map[string]interface{}{
		"action":                         "read_document",
		"file_path":                      path,
		registeredToolContextTokensField: 400_000,
	}
	out := h.toolOffice(args)
	if got, want := parseOfficeReadCharsForTest(t, out), agent.DocumentReadMaxRunesForContext(400_000); got != want {
		t.Fatalf("routed context page chars = %d, want %d", got, want)
	}
	if _, ok := args[registeredToolContextTokensField]; ok {
		t.Fatal("trusted runtime context field leaked into office handler arguments")
	}
}

func TestOfficeExecutionContextBudgetIsHostControlled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatcher-context.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("\u4e2d", agent.DocumentReadMaxRunesForContext(400_000)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{ContextLength: 8_000}
	}}}
	h.registry = NewToolRegistry()
	if err := h.registry.Register(RegisteredTool{
		Name:    "office",
		Handler: h.toolOffice,
	}); err != nil {
		t.Fatal(err)
	}
	result := h.executeToolDetailedWithRuntimeContextAndContextTokens(
		context.Background(), "", false, "", 400_000, "office",
		fmt.Sprintf(`{"action":"read_document","file_path":%q,"_runtime_context_tokens":999999}`, path), "", nil,
	)
	if got, want := parseOfficeReadCharsForTest(t, result.Text), agent.DocumentReadMaxRunesForContext(400_000); got != want {
		t.Fatalf("dispatcher context page chars = %d, want host-selected %d; result=%q", got, want, result.Text)
	}
}

func TestLegacyToolProjectionUsesRoutedContextBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-projection-context.json")
	contextTokens := 400_000
	pageRunes := agent.DocumentReadMaxRunesForContext(contextTokens)
	if err := os.WriteFile(path, []byte(strings.Repeat("\u4e2d", pageRunes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{ContextLength: 8_000}
	}}}
	h.registry = NewToolRegistry()
	if err := h.registry.Register(RegisteredTool{Name: "office", Handler: h.toolOffice}); err != nil {
		t.Fatal(err)
	}
	result := h.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		UserID: "projection-context",
		Config: corelib.MaclawLLMConfig{ContextLength: 500_000},
		ToolCalls: []llm.ToolCall{{
			ID: "call-read",
			Function: llm.ToolCallFunction{
				Name:      "office",
				Arguments: fmt.Sprintf(`{"action":"read_document","file_path":%q}`, path),
			},
		}},
	})
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(result.ToolResults))
	}
	if got, want := len(result.ToolResults[0]), agent.DocumentReadToolResultLimit(contextTokens); got > want {
		t.Fatalf("legacy projection bytes = %d, want <= %d", got, want)
	}
	if !strings.Contains(result.ToolResults[0], "# chars: "+fmt.Sprint(pageRunes)) {
		t.Fatalf("legacy projection did not retain routed document page: %q", result.ToolResults[0][:min(len(result.ToolResults[0]), 500)])
	}
}

func parseOfficeReadCharsForTest(t *testing.T, output string) int {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "# chars: ") {
			continue
		}
		var chars int
		if _, err := fmt.Sscanf(line, "# chars: %d", &chars); err != nil {
			t.Fatalf("parse chars header %q: %v", line, err)
		}
		return chars
	}
	t.Fatalf("missing chars header in %q", output)
	return 0
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

func TestToolOfficeWritePPTXDispatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.pptx")
	h := &IMMessageHandler{}
	got := h.toolOffice(map[string]interface{}{
		"action": "write_pptx",
		"path":   path,
		"data": map[string]interface{}{
			"title":  "生日会",
			"slides": []interface{}{map[string]interface{}{"title": "封面", "bullets": []interface{}{"第一条"}}},
		},
	})
	if !strings.Contains(got, "已成功写入 PPTX") {
		t.Fatalf("write_pptx = %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("deck missing: %v", err)
	}
	// The alias reaches the same writer.
	got = h.toolOffice(map[string]interface{}{
		"action": "generate_pptx",
		"path":   filepath.Join(dir, "alias.pptx"),
		"data":   map[string]interface{}{"slides": []interface{}{map[string]interface{}{"title": "t"}}},
	})
	if !strings.Contains(got, "已成功写入 PPTX") {
		t.Fatalf("generate_pptx = %q", got)
	}
}
