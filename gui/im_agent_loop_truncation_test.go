package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestAttemptPartialWriteFileWritesInsideWorkingDir(t *testing.T) {
	dir := t.TempDir()
	raw := "{\n  \"path\" : \"nested/out.txt\",\n  \"content\" : \"hello\\npartial"

	result := attemptPartialWriteFile(raw, dir)
	if result == nil {
		t.Fatal("expected partial write result")
	}
	wantPath := filepath.Join(dir, "nested", "out.txt")
	if result.Path != wantPath {
		t.Fatalf("path = %q, want %q", result.Path, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello\npartial" {
		t.Fatalf("written content = %q", string(data))
	}
}

func TestAttemptPartialWriteFileRejectsPathOutsideWorkingDir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "outside.txt")
	raw := `{"path":"` + filepath.ToSlash(outside) + `","content":"escape"}`

	if result := attemptPartialWriteFile(raw, dir); result != nil {
		t.Fatalf("expected outside path to be rejected, got %#v", result)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside file should not be written, stat err=%v", err)
	}
}

func TestAttemptPartialWriteFileDoesNotOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	raw := `{"path":"existing.txt","content":"partial replacement`

	if result := attemptPartialWriteFile(raw, dir); result != nil {
		t.Fatalf("expected partial overwrite of existing file to be rejected, got %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("existing file was modified: %q", string(data))
	}
}

func TestAttemptPartialWriteFileAllowsAppendToExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	raw := `{"path":"existing.txt","mode":"append","content":" plus partial`

	result := attemptPartialWriteFile(raw, dir)
	if result == nil {
		t.Fatal("expected append partial write result")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "original plus partial" {
		t.Fatalf("append content = %q", string(data))
	}
}

func TestTruncatedToolArgsForNameTrimsToolName(t *testing.T) {
	args := map[string]string{" write_file ": `{"path":"out.txt","content":"partial`}
	got, ok := truncatedToolArgsForName(args, "write_file")
	if !ok || got == "" {
		t.Fatalf("expected trimmed tool name lookup to find raw args, got ok=%v value=%q", ok, got)
	}
}

func TestExtractJSONStringFieldHandlesWhitespaceEscapesAndTruncation(t *testing.T) {
	raw := "{\n  \"path\" \t : \t \"src/file.txt\",\n  \"content\" : \"line1\\nline2\\u0021"

	if got := extractJSONStringField(raw, "path"); got != "src/file.txt" {
		t.Fatalf("path = %q", got)
	}
	if got := extractJSONStringField(raw, "content"); got != "line1\nline2!" {
		t.Fatalf("content = %q", got)
	}
}

func TestEnsureTruncationFallbackToolsAddsRealAlternates(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"write_file"}, map[string]bool{"write_file": true})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["craft_tool"] {
		t.Fatalf("craft_tool fallback missing after write_file block: %v", names)
	}
	if names["bash"] {
		t.Fatalf("bash should not be re-added from the fallback catalog: %v", names)
	}
}

func TestEnsureTruncationFallbackToolsDoesNotAddUnroutedFallback(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"write_file"}, map[string]bool{"write_file": true})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if names["craft_tool"] || names["bash"] {
		t.Fatalf("fallback tools should not be added when they were not routed: %v", names)
	}
}

func TestEnsureTruncationFallbackToolsDoesNotDuplicate(t *testing.T) {
	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(current, catalog, []string{"generate_pdf", "edit_file"}, nil)
	if len(got) != len(current) {
		t.Fatalf("got %d tools, want no duplicates (%d)", len(got), len(current))
	}
}

func TestEnsureTruncationFallbackToolsRespectsBlockedSet(t *testing.T) {
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	got := ensureTruncationFallbackTools(nil, catalog, []string{"write_file"}, map[string]bool{
		"write_file":   true,
		"craft_tool":   true,
		"generate_pdf": true,
	})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if names["craft_tool"] {
		t.Fatalf("craft_tool was re-added despite blocked set: %v", names)
	}
	if names["bash"] {
		t.Fatalf("bash should not be added as truncation fallback: %v", names)
	}
}

func TestTruncationFallbackCatalogRestoresWorkflowDocRequiredTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "truncation-fallback-workflow-tools-user"
	_, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build docs",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("list_directory", "list", nil, nil),
		toolDef("send_file", "send", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("edit_file", "edit", nil, nil),
		toolDef("task", "task", nil, nil),
	})

	got := handler.truncationFallbackToolCatalog(nil, userID, nil, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("task", "task", nil, nil),
	})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}

	if !names["read_file"] || !names["list_directory"] || !names["send_file"] {
		t.Fatalf("workflow truncation fallback should restore doc context/delivery tools, got %v", names)
	}
	if names["task"] || names["write_file"] || names["edit_file"] {
		t.Fatalf("workflow truncation fallback should still remove disallowed tools, got %v", names)
	}
}

func TestTruncationFallbackCatalogRespectsCodingImplementationMainLoopPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "truncation-fallback-implementation-main-loop-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseIndex = 3
	state.CurrentPhase = workflow.PhaseCodingImplementation
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
		toolDef("delegate_task", "delegate", nil, nil),
		toolDef("list_directory", "list", nil, nil),
		toolDef("read_file", "read", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("write_file", "write", nil, nil),
	})

	got := handler.truncationFallbackToolCatalog(&LoopContext{WorkflowAgentLoop: true}, userID, nil, []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
		toolDef("read_file", "read", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("write_file", "write", nil, nil),
	})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}

	for _, name := range []string{"read_file", "list_directory", "delegate_task"} {
		if !names[name] {
			t.Fatalf("implementation truncation fallback should keep %s, got %v", name, names)
		}
	}
	for _, name := range []string{"bash", "write_file", "task", "craft_tool"} {
		if names[name] {
			t.Fatalf("implementation truncation fallback must not expose %s, got %v", name, names)
		}
	}
}

func TestTruncationFallbackCatalogUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-truncation-doc-only-owner"
	weixinID := "weixin-truncation-user"
	remoteOwnerID := "remote:mobile-truncation-owner"
	_, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build docs",
	})
	if err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm desktop failed: %v", err)
	}
	handler.lastUserID = desktopID
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	ctx := &LoopContext{SkipNeedsConfirmGate: true, Runtime: RuntimeContext{RequestID: "req-weixin-truncation", PolicyOwnerID: remoteOwnerID}}

	got := handler.truncationFallbackToolCatalog(ctx, weixinID, nil, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	names := map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["write_file"] || !names["bash"] {
		t.Fatalf("weixin truncation fallback must not inherit desktop workflow policy, got %v", names)
	}

	_, err = handler.app.workflowEngine.StartWorkflow(remoteOwnerID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "remote docs",
	})
	if err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(remoteOwnerID); err != nil {
		t.Fatalf("SkipPhaseForm remote failed: %v", err)
	}

	got = handler.truncationFallbackToolCatalog(ctx, weixinID, nil, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	names = map[string]bool{}
	for _, td := range got {
		names[tool.ExtractToolName(td)] = true
	}
	if names["write_file"] || !names["bash"] || !names["read_file"] {
		t.Fatalf("runtime owner workflow must drive truncation fallback catalog, got %v", names)
	}
}

func TestBashTruncationKeepsBashAvailableAndAddsRecoveryHint(t *testing.T) {
	if !classifyAgentToolKind("bash").PreserveAfterTruncation() {
		t.Fatal("bash should remain available; oversized payloads are handled by schema and pre-execution limits")
	}

	current := []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}
	conversation := []interface{}{}
	recorded := false
	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		7,
		llm.Choice{TruncatedToolNames: []string{"bash"}},
		&agentLoopPhase{TruncationRetries: maxTruncationRetries},
		conversation,
		append([]map[string]interface{}{toolDef("bash", "shell", nil, nil)}, current...),
		catalog,
		func(int, []interface{}) { recorded = true },
	)
	names := map[string]bool{}
	for _, td := range result.Tools {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["bash"] {
		t.Fatalf("bash should remain available after truncation recovery: %v", names)
	}
	if !names["read_file"] {
		t.Fatalf("existing safe tools should remain available: %v", names)
	}
	if !recorded || len(result.Conversation) != 1 {
		t.Fatalf("expected recovery hint to be recorded, recorded=%v conversation=%#v", recorded, result.Conversation)
	}
	hint, _ := result.Conversation[0].(map[string]string)
	if !containsText(hint["content"], "bash only for short commands") {
		t.Fatalf("expected bash recovery hint, got %#v", hint)
	}
}

func TestWriteFileTruncationKeepsWriteFileAvailableAndAddsRecoveryHint(t *testing.T) {
	if !classifyAgentToolKind("write_file").PreserveAfterTruncation() {
		t.Fatal("write_file should remain available; completed large content is handled by the backend")
	}

	phase := &agentLoopPhase{}
	tools := []map[string]interface{}{
		toolDef("write_file", "write", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}
	recorded := false
	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		7,
		llm.Choice{TruncatedToolNames: []string{"write_file"}},
		phase,
		nil,
		tools,
		tools,
		func(int, []interface{}) { recorded = true },
	)

	if !result.ContinueLoop {
		t.Fatal("expected loop to continue after write_file truncation hint")
	}
	if phase.TruncationBlockedTools["write_file"] {
		t.Fatalf("write_file should not be marked blocked after truncation: %#v", phase.TruncationBlockedTools)
	}
	names := map[string]bool{}
	for _, td := range result.Tools {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["write_file"] || !names["read_file"] {
		t.Fatalf("write_file/read_file should remain available: %v", names)
	}
	if !recorded || len(result.Conversation) != 1 {
		t.Fatalf("expected write_file recovery hint, recorded=%v conversation=%#v", recorded, result.Conversation)
	}
	hint, _ := result.Conversation[0].(map[string]string)
	if !containsText(hint["content"], "complete JSON object") || !containsText(hint["content"], "overwrite/append") {
		t.Fatalf("write_file hint should guide complete/chunked JSON, got %#v", hint)
	}
}

func TestHandleTruncatedToolCallsDoesNotBlockRepeatedBashTruncation(t *testing.T) {
	phase := &agentLoopPhase{}
	tools := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("read_file", "read", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		7,
		llm.Choice{TruncatedToolNames: []string{"bash"}},
		phase,
		nil,
		tools,
		catalog,
		func(int, []interface{}) {},
	)

	if !result.ContinueLoop {
		t.Fatal("expected loop to continue after applying truncation block")
	}
	if phase.TruncationBlockedTools["bash"] {
		t.Fatalf("bash should not be marked blocked after repeated truncation: %#v", phase.TruncationBlockedTools)
	}
	if phase.TruncationRetries != 0 {
		t.Fatalf("essential truncation should not consume generic truncation retries, got %d", phase.TruncationRetries)
	}
	if phase.EssentialTruncationHints != 1 {
		t.Fatalf("essential truncation hint count = %d, want 1", phase.EssentialTruncationHints)
	}
	names := map[string]bool{}
	for _, td := range result.Tools {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["bash"] {
		t.Fatalf("bash should remain available after repeated truncation: %v", names)
	}
	if !names["read_file"] {
		t.Fatalf("safe tools should remain available: %v", names)
	}
}

func TestHandleTruncatedToolCallsDoesNotBlockDelegateTaskHandoff(t *testing.T) {
	if !classifyAgentToolKind("delegate_task").PreserveAfterTruncation() {
		t.Fatal("delegate_task should remain available; coding implementation depends on this handoff tool")
	}

	phase := &agentLoopPhase{}
	tools := []map[string]interface{}{
		toolDef("delegate_task", "delegate", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}
	recorded := false
	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		7,
		llm.Choice{TruncatedToolNames: []string{"delegate_task"}},
		phase,
		nil,
		tools,
		tools,
		func(int, []interface{}) { recorded = true },
	)

	if !result.ContinueLoop {
		t.Fatal("expected loop to continue after delegate_task truncation hint")
	}
	if phase.TruncationBlockedTools["delegate_task"] {
		t.Fatalf("delegate_task should not be marked blocked after truncation: %#v", phase.TruncationBlockedTools)
	}
	names := map[string]bool{}
	for _, td := range result.Tools {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["delegate_task"] || !names["read_file"] {
		t.Fatalf("delegate_task/read_file should remain available: %v", names)
	}
	if !recorded || len(result.Conversation) != 1 {
		t.Fatalf("expected delegate_task recovery hint, recorded=%v conversation=%#v", recorded, result.Conversation)
	}
	hint, _ := result.Conversation[0].(map[string]string)
	if !containsText(hint["content"], "keep request concise") || containsText(hint["content"], "write_file chunks") {
		t.Fatalf("delegate_task hint should guide concise handoff, got %#v", hint)
	}
}

func TestRepeatedEssentialTruncationFallsThroughAfterOneHint(t *testing.T) {
	phase := &agentLoopPhase{TruncationRetries: maxTruncationRetries, EssentialTruncationHints: maxEssentialTruncationHints}
	tools := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}

	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		8,
		llm.Choice{TruncatedToolNames: []string{"bash"}},
		phase,
		nil,
		tools,
		nil,
		func(int, []interface{}) { t.Fatal("second essential truncation should not inject another hint") },
	)

	if result.ContinueLoop {
		t.Fatal("expected repeated essential truncation to fall through to no-tool recovery instead of looping")
	}
	if phase.TruncationBlockedTools["bash"] {
		t.Fatalf("bash should remain unblocked: %#v", phase.TruncationBlockedTools)
	}
	if len(result.Tools) != len(tools) {
		t.Fatalf("tools changed unexpectedly: %#v", result.Tools)
	}
}

func TestMixedEssentialTruncationKeepsBashAndWriteFileAvailable(t *testing.T) {
	phase := &agentLoopPhase{TruncationRetries: maxTruncationRetries}
	tools := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}
	catalog := []map[string]interface{}{
		toolDef("bash", "shell", nil, nil),
		toolDef("write_file", "write", nil, nil),
		toolDef("read_file", "read", nil, nil),
		toolDef("craft_tool", "craft", nil, nil),
	}

	result := (&IMMessageHandler{}).handleAgentLoopTruncatedToolCalls(
		9,
		llm.Choice{TruncatedToolNames: []string{"bash", "write_file"}},
		phase,
		nil,
		tools,
		catalog,
		func(int, []interface{}) {},
	)

	if !result.ContinueLoop {
		t.Fatal("expected loop to continue after essential truncation hint")
	}
	if phase.TruncationBlockedTools["bash"] {
		t.Fatalf("bash should remain unblocked: %#v", phase.TruncationBlockedTools)
	}
	if phase.TruncationBlockedTools["write_file"] {
		t.Fatalf("write_file should remain unblocked: %#v", phase.TruncationBlockedTools)
	}
	names := map[string]bool{}
	for _, td := range result.Tools {
		names[tool.ExtractToolName(td)] = true
	}
	if !names["bash"] || !names["write_file"] || !names["read_file"] {
		t.Fatalf("expected bash/write_file/read_file to remain available: %v", names)
	}
}

func TestValidToolBranchResetsTruncationRecoveryCounters(t *testing.T) {
	phase := &agentLoopPhase{
		TruncationRetries:        maxTruncationRetries,
		EssentialTruncationHints: maxEssentialTruncationHints,
		TruncationBlockedTools:   map[string]bool{"write_file": true},
	}
	choice := llm.Choice{Message: llm.Message{ToolCalls: []llm.ToolCall{{ID: "call_read", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`}}}}}

	(&IMMessageHandler{}).startAgentLoopToolBranch(agentLoopToolBranchStartOptions{Choice: choice, Phase: phase})

	if phase.TruncationRetries != 0 || phase.EssentialTruncationHints != 0 {
		t.Fatalf("truncation counters not reset: retries=%d essential_hints=%d", phase.TruncationRetries, phase.EssentialTruncationHints)
	}
	if !phase.TruncationBlockedTools["write_file"] {
		t.Fatalf("blocked tools should persist until loop end: %#v", phase.TruncationBlockedTools)
	}
}

func TestPartialTruncationDoesNotResetTruncationRecoveryCounters(t *testing.T) {
	phase := &agentLoopPhase{TruncationRetries: 2, EssentialTruncationHints: 1}
	choice := llm.Choice{
		Message:            llm.Message{ToolCalls: []llm.ToolCall{{ID: "call_read", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`}}}},
		TruncatedToolNames: []string{"bash"},
	}

	(&IMMessageHandler{}).startAgentLoopToolBranch(agentLoopToolBranchStartOptions{Choice: choice, Phase: phase})

	if phase.TruncationRetries != 2 || phase.EssentialTruncationHints != 1 {
		t.Fatalf("partial truncation should keep counters: retries=%d essential_hints=%d", phase.TruncationRetries, phase.EssentialTruncationHints)
	}
}

func TestTruncationRetryHintStatesHardInlineLimits(t *testing.T) {
	got := buildTruncationRetryHint("write_file, bash", []map[string]interface{}{
		toolDef("write_file", "write", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	for _, want := range []string{"write_file has no backend content limit", "complete", "bash.command <= 4000", "mode=append", "do not embed generated file bodies"} {
		if !containsText(got, want) {
			t.Fatalf("hint %q missing %q", got, want)
		}
	}
}

func TestBuildTruncationRetryHintOnlyMentionsAvailableAlternates(t *testing.T) {
	got := buildTruncationRetryHint("write_file", []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
		toolDef("write_file", "write", nil, nil),
	})
	if containsText(got, "craft_tool") {
		t.Fatalf("hint mentioned unavailable alternates: %q", got)
	}

	got = buildTruncationRetryHint("write_file", []map[string]interface{}{
		toolDef("craft_tool", "craft", nil, nil),
	})
	if !containsText(got, "craft_tool") {
		t.Fatalf("hint did not match available alternates: %q", got)
	}
	if containsText(got, "mode=append") {
		t.Fatalf("hint should not suggest unavailable write_file chunking: %q", got)
	}
}

func TestBuildTruncationBlockInstructionsOnlyMentionsAvailableAlternates(t *testing.T) {
	got := buildTruncationBlockAlternativeInstructions([]string{"write_file"}, []map[string]interface{}{
		toolDef("read_file", "read", nil, nil),
	})
	if containsText(got, "craft_tool") || containsText(got, "bash") {
		t.Fatalf("block instruction mentioned unavailable alternates: %q", got)
	}

	got = buildTruncationBlockAlternativeInstructions([]string{"write_file"}, []map[string]interface{}{
		toolDef("craft_tool", "craft", nil, nil),
		toolDef("bash", "shell", nil, nil),
	})
	if !containsText(got, "craft_tool") || !containsText(got, "bash") {
		t.Fatalf("block instruction missing available alternates: %q", got)
	}
}

func containsText(s, sub string) bool {
	return strings.Contains(s, sub)
}
