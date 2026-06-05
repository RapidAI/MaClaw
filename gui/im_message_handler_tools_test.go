package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func testCodingIntentClassifier() *intent.UnifiedIntentClassifier {
	return intent.New(intent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"bug_fix","score":0.95,"reason":"test coding task"}]}`, nil
		},
		LLMTimeout: time.Second,
	})
}

func testIntentClassifier(label string) *intent.UnifiedIntentClassifier {
	return intent.New(intent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			return fmt.Sprintf(`{"top":[{"skill":%q,"score":0.95,"reason":"test %s task"}]}`, label, label), nil
		},
		LLMTimeout: time.Second,
	})
}

// ---------------------------------------------------------------------------
// Tests for IMMessageHandler dynamic tool integration (Task 6.3)
// ---------------------------------------------------------------------------

func TestIMToolWriteFile_AllowsEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	h := &IMMessageHandler{}
	got := h.toolWriteFile(map[string]interface{}{"path": "empty.txt", "content": ""})
	if !strings.Contains(got, "已清空") {
		t.Fatalf("toolWriteFile() = %q, want clear message", got)
	}
	data, err := os.ReadFile(filepath.Join(tmpDir, "empty.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("file content = %q, want empty", string(data))
	}
}

func TestIMToolWriteFile_RejectsMissingContentField(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolWriteFile(map[string]interface{}{"path": "empty.txt"})
	if got != "缺少 content 参数" {
		t.Fatalf("toolWriteFile() = %q, want missing content error", got)
	}
}

func TestIMToolWriteFile_NormalizesWorkflowDocFileName(t *testing.T) {
	tmpDir := t.TempDir()
	h := &IMMessageHandler{}
	got := h.toolWriteFile(map[string]interface{}{
		"path":     filepath.Join(tmpDir, "需求文档.md"),
		"content":  "# Requirements\n\nbody",
		"phase_id": "requirements",
	})
	if !strings.Contains(got, "01-requirements.md") {
		t.Fatalf("toolWriteFile() = %q, want normalized workflow doc path", got)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "01-requirements.md")); err != nil {
		t.Fatalf("expected normalized workflow doc file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "需求文档.md")); !os.IsNotExist(err) {
		t.Fatalf("localized workflow doc filename should not be written, stat err=%v", err)
	}
}

func TestWorkflowDocWritePathUsesDocType(t *testing.T) {
	got := workflowDocWritePath(filepath.Join("docs", "任务列表.md"), map[string]interface{}{
		"doc_type": "task_plan",
	})
	want := filepath.Join("docs", "03-task-breakdown.md")
	if got != want {
		t.Fatalf("workflowDocWritePath() = %q, want %q", got, want)
	}
}

func TestWorkflowDocWritePathDropsLocalizedDocDirectory(t *testing.T) {
	got := workflowDocWritePath(filepath.Join("docs", "需求文档", "需求文档.md"), map[string]interface{}{
		"phase_id": "requirements",
	})
	want := filepath.Join("docs", "01-requirements.md")
	if got != want {
		t.Fatalf("workflowDocWritePath() = %q, want %q", got, want)
	}
}

func TestIMToolSendFile_NormalizesWorkflowDocDisplayFileName(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "requirements.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &IMMessageHandler{}
	got := h.toolSendFile(map[string]interface{}{
		"path":      path,
		"file_name": "需求文档.pdf",
		"phase_id":  "requirements",
	})
	if !strings.HasPrefix(got, "[file_base64|01-requirements.pdf|application/pdf]") {
		t.Fatalf("toolSendFile() = %q, want normalized display filename", got)
	}
	if strings.Contains(got, "需求文档.pdf") {
		t.Fatalf("toolSendFile() should not expose localized workflow filename: %q", got)
	}
}

func TestIMToolSendFile_WorkflowForwardIMUsesStructuredDeliveryMessage(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "requirements.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &IMMessageHandler{}
	got := h.toolSendFile(map[string]interface{}{
		"path":          path,
		"file_name":     "需求文档.pdf",
		"phase_id":      "requirements",
		"forward_to_im": true,
	})
	payload := parseToolPayloadResult(got)
	if payload.File == nil {
		t.Fatalf("expected file payload, got %#v", payload)
	}
	if payload.File.name != "01-requirements.pdf" {
		t.Fatalf("file name = %q, want 01-requirements.pdf", payload.File.name)
	}
	if !payload.File.forwardIM {
		t.Fatalf("expected forwardIM=true, got %#v", payload.File)
	}
	if !strings.Contains(payload.File.message, "需求文档已生成") {
		t.Fatalf("delivery message = %q, want requirements prompt from metadata", payload.File.message)
	}
}

func TestIMToolSendFile_NormalizesWorkflowDocDisplayFileNameWithPathExtension(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "requirements.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := &IMMessageHandler{}
	got := h.toolSendFile(map[string]interface{}{
		"path":      path,
		"file_name": "需求文档",
		"phase_id":  "requirements",
	})
	if !strings.HasPrefix(got, "[file_base64|01-requirements.pdf|application/pdf]") {
		t.Fatalf("toolSendFile() = %q, want normalized display filename with path extension", got)
	}
}

func TestWorkflowDocDeliveryFileNameUsesDocTypeAndExtension(t *testing.T) {
	got := workflowDocDeliveryFileName("任务列表.pdf", map[string]interface{}{"doc_type": "task_plan"})
	if got != "03-task-breakdown.pdf" {
		t.Fatalf("workflowDocDeliveryFileName() = %q, want 03-task-breakdown.pdf", got)
	}
}

func TestWorkflowDocDeliveryFileNameUsesFallbackExtension(t *testing.T) {
	got := workflowDocDeliveryFileNameWithFallbackExt("任务列表", map[string]interface{}{"doc_type": "task_plan"}, ".pdf")
	if got != "03-task-breakdown.pdf" {
		t.Fatalf("workflowDocDeliveryFileNameWithFallbackExt() = %q, want 03-task-breakdown.pdf", got)
	}
}

func TestWorkflowDocDeliveryFileNameRejectsLocalizedExtension(t *testing.T) {
	got := workflowDocDeliveryFileNameWithFallbackExt("任务列表.文档", map[string]interface{}{"doc_type": "task_plan"}, ".pdf")
	if got != "03-task-breakdown.pdf" {
		t.Fatalf("workflowDocDeliveryFileNameWithFallbackExt() = %q, want 03-task-breakdown.pdf", got)
	}
}

func TestBuildToolDefinitionsExposeWorkflowDocMetadata(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	defs := h.buildToolDefinitions()
	for _, toolName := range []string{"write_file", "send_file"} {
		props := toolDefinitionProperties(t, defs, toolName)
		for _, prop := range []string{"phase_id", "doc_type"} {
			if _, ok := props[prop]; !ok {
				t.Fatalf("%s should expose %s metadata for workflow document filenames", toolName, prop)
			}
		}
	}
}

func TestBuildToolDefinitionsWorkflowDocMetadataDescriptions(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	defs := h.buildToolDefinitions()

	writeProps := toolDefinitionProperties(t, defs, "write_file")
	if got := toolSchemaDescription(t, writeProps, "phase_id"); got != workflowDocPhaseIDSchemaDescription() {
		t.Fatalf("write_file phase_id description = %q, want %q", got, workflowDocPhaseIDSchemaDescription())
	}
	if got := toolSchemaDescription(t, writeProps, "doc_type"); got != workflowDocTypeSchemaDescription() {
		t.Fatalf("write_file doc_type description = %q, want %q", got, workflowDocTypeSchemaDescription())
	}

	sendProps := toolDefinitionProperties(t, defs, "send_file")
	if got := toolSchemaDescription(t, sendProps, "phase_id"); got != workflowDocDeliveryPhaseIDSchemaDescription() {
		t.Fatalf("send_file phase_id description = %q, want %q", got, workflowDocDeliveryPhaseIDSchemaDescription())
	}
	if got := toolSchemaDescription(t, sendProps, "doc_type"); got != workflowDocDeliveryTypeSchemaDescription() {
		t.Fatalf("send_file doc_type description = %q, want %q", got, workflowDocDeliveryTypeSchemaDescription())
	}
}

func TestBuildToolDefinitionsCapInlinePayloads(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	defs := h.buildToolDefinitions()

	bashProps := toolDefinitionProperties(t, defs, "bash")
	if got := toolSchemaMaxLength(t, bashProps, "command"); got != float64(maxAgentLoopInlineBashCommandRunes) {
		t.Fatalf("bash command maxLength = %v, want %d", got, maxAgentLoopInlineBashCommandRunes)
	}

	writeProps := toolDefinitionProperties(t, defs, "write_file")
	if got := toolSchemaMaxLength(t, writeProps, "content"); got != float64(maxAgentLoopInlineWriteFileContentRunes) {
		t.Fatalf("write_file content maxLength = %v, want %d", got, maxAgentLoopInlineWriteFileContentRunes)
	}
}

func TestBuiltinRegistryCapsInlinePayloads(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})

	bash, ok := registry.Get("bash")
	if !ok || bash == nil {
		t.Fatal("bash registry tool missing")
	}
	if got := registeredToolSchemaMaxLength(t, registeredToolSchemaProperties(bash.InputSchema), "command"); got != float64(maxAgentLoopInlineBashCommandRunes) {
		t.Fatalf("registry bash command maxLength = %v, want %d", got, maxAgentLoopInlineBashCommandRunes)
	}

	write, ok := registry.Get("write_file")
	if !ok || write == nil {
		t.Fatal("write_file registry tool missing")
	}
	if got := registeredToolSchemaMaxLength(t, registeredToolSchemaProperties(write.InputSchema), "content"); got != float64(maxAgentLoopInlineWriteFileContentRunes) {
		t.Fatalf("registry write_file content maxLength = %v, want %d", got, maxAgentLoopInlineWriteFileContentRunes)
	}
}

func toolDefinitionProperties(t *testing.T, defs []map[string]interface{}, name string) map[string]interface{} {
	t.Helper()
	for _, def := range defs {
		if extractToolName(def) != name {
			continue
		}
		fn, _ := def["function"].(map[string]interface{})
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		if props == nil {
			t.Fatalf("%s properties missing in tool definition: %#v", name, def)
		}
		return props
	}
	t.Fatalf("tool definition %s not found", name)
	return nil
}

func toolSchemaDescription(t *testing.T, props map[string]interface{}, name string) string {
	t.Helper()
	raw, ok := props[name]
	if !ok {
		t.Fatalf("schema property %s missing", name)
	}
	switch schema := raw.(type) {
	case map[string]string:
		return schema["description"]
	case map[string]interface{}:
		desc, _ := schema["description"].(string)
		return desc
	default:
		t.Fatalf("schema property %s has unexpected type: %#v", name, raw)
		return ""
	}
}

func toolSchemaMaxLength(t *testing.T, props map[string]interface{}, name string) float64 {
	t.Helper()
	raw, ok := props[name]
	if !ok {
		t.Fatalf("schema property %s missing", name)
	}
	schema, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("schema property %s has unexpected type: %#v", name, raw)
	}
	value, ok := schema["maxLength"]
	if !ok {
		t.Fatalf("schema property %s missing maxLength", name)
	}
	switch v := value.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	default:
		t.Fatalf("schema property %s maxLength has unexpected type: %#v", name, value)
		return 0
	}
}

func registeredToolSchemaMaxLength(t *testing.T, props map[string]map[string]interface{}, name string) float64 {
	t.Helper()
	raw, ok := props[name]
	if !ok {
		t.Fatalf("schema property %s missing", name)
	}
	value, ok := raw["maxLength"]
	if !ok {
		t.Fatalf("schema property %s missing maxLength", name)
	}
	switch v := value.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	default:
		t.Fatalf("schema property %s maxLength has unexpected type: %#v", name, value)
		return 0
	}
}

func TestTrialReflectObserveIteration_BuildsReflectionNote(t *testing.T) {
	state := newTrialReflectState(true)
	toolCalls := []llm.ToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "bash",
				Arguments: `{"command":"npm test"}`,
			},
		},
	}
	toolResults := []string{"error: test failed"}
	toolOutcomes := []toolOutcome{toolOutcomeFailed}

	observation := state.observeIteration(toolCalls, toolResults, toolOutcomes)
	if observation.Outcome != trialReflectOutcomeFailed {
		t.Fatalf("expected failed outcome, got %q", observation.Outcome.String())
	}
	if !contains(observation.Text, "bash=failed") {
		t.Fatalf("expected failed observation, got %q", observation.Text)
	}
	if !contains(state.pendingNote, "[Trial reflection]") || !contains(state.pendingNote, "do not repeat the same failed attempt") {
		t.Fatalf("expected reflection note, got %q", state.pendingNote)
	}
	if len(observation.RepeatedFailures) != 1 || observation.RepeatedFailures[0] != "bash" {
		t.Fatalf("expected repeated failure guard for bash, got %#v", observation.RepeatedFailures)
	}
}

func TestTrialReflectObserveIteration_ClearsFailureAfterSuccess(t *testing.T) {
	state := newTrialReflectState(true)
	toolCalls := []llm.ToolCall{
		{
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "bash",
				Arguments: `{"command":"npm test"}`,
			},
		},
	}

	state.observeIteration(toolCalls, []string{"previous failure"}, []toolOutcome{toolOutcomeFailed})
	observation := state.observeIteration(toolCalls, []string{"completed"}, []toolOutcome{toolOutcomeSucceeded})
	if observation.Outcome != trialReflectOutcomeSucceeded {
		t.Fatalf("expected succeeded outcome, got %q", observation.Outcome.String())
	}
	if !contains(observation.Text, "bash=succeeded") {
		t.Fatalf("expected succeeded observation, got %q", observation.Text)
	}
	if len(observation.RepeatedFailures) != 0 {
		t.Fatalf("expected repeated failures to clear after success, got %#v", observation.RepeatedFailures)
	}
}

func TestTrialReflectObserveIteration_ListSkillsEmptyRegistryIsSucceeded(t *testing.T) {
	state := newTrialReflectState(true)
	toolCalls := []llm.ToolCall{{
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "list_skills",
			Arguments: `{}`,
		},
	}}
	toolResults := []string{"本地没有已注册的 Skill。\n\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n"}
	toolOutcomes := []toolOutcome{toolOutcomeSucceeded}

	observation := state.observeIteration(toolCalls, toolResults, toolOutcomes)
	if observation.Outcome != trialReflectOutcomeSucceeded {
		t.Fatalf("expected succeeded outcome, got %q", observation.Outcome.String())
	}
	if !contains(observation.Text, "list_skills=succeeded") {
		t.Fatalf("expected succeeded observation, got %q", observation.Text)
	}
	if len(observation.RepeatedFailures) != 0 {
		t.Fatalf("expected no repeated failures, got %#v", observation.RepeatedFailures)
	}
}

func TestDidSkillToolFail_IgnoresListSkillsBusinessEmptyState(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      "list_skills",
			Arguments: `{}`,
		},
	}}
	if didSkillToolFail(toolCalls, []toolOutcome{toolOutcomeSucceeded}) {
		t.Fatal("expected list_skills empty state to not count as skill failure")
	}
}

func TestHasRecentFailedToolCallUsesOutcomeMetadata(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "tool", Content: "error: text-only legacy failure"},
		{Role: "tool", Content: "all good", ToolOutcome: toolOutcomeFailed.String()},
	}
	if !hasRecentFailedToolCall(history) {
		t.Fatal("expected structured failed outcome to be detected")
	}
	history[1].ToolOutcome = toolOutcomeSucceeded.String()
	if hasRecentFailedToolCall(history) {
		t.Fatal("expected failure-looking text without failed metadata to be ignored")
	}
}

func TestExecuteToolDetailed_ArgumentParseFailureUsesMetadata(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.executeToolDetailed("write_file", `{"path":`, nil)
	if result.Outcome != toolOutcomeFailed {
		t.Fatalf("Outcome = %q, want failed", result.Outcome.String())
	}
	if result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("FailureKind = %q, want argument_parse", result.FailureKind)
	}
	if result.ToolName != "write_file" {
		t.Fatalf("ToolName = %q, want write_file", result.ToolName)
	}
	if result.ToolKind != agentToolKindWriteFile {
		t.Fatalf("ToolKind = %v, want write_file", result.ToolKind)
	}
	if !result.IsWriteFileRecoverableFailure() {
		t.Fatal("expected write_file argument parse failure to be recoverable")
	}
	result.ToolName = "bash"
	result.ToolKind = agentToolKindUnknown
	if result.IsWriteFileRecoverableFailure() {
		t.Fatal("expected non-write_file tool to not use write_file recovery")
	}
}

func TestExecuteToolDetailed_RegisteredHandlerInfersOutcome(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "ssh",
		Description: "SSH",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return "SSH connection succeeded"
		},
	}); err != nil {
		t.Fatalf("Register ssh: %v", err)
	}
	if err := registry.Register(RegisteredTool{
		Name:        "failing_tool",
		Description: "Failing tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return "error: test failed"
		},
	}); err != nil {
		t.Fatalf("Register failing_tool: %v", err)
	}
	if err := registry.Register(RegisteredTool{
		Name:        "empty_tool",
		Description: "Empty tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return ""
		},
	}); err != nil {
		t.Fatalf("Register empty_tool: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	success := h.executeToolDetailed("ssh", `{}`, nil)
	if success.Outcome != toolOutcomeSucceeded || success.FailureKind != toolFailureNone {
		t.Fatalf("success result metadata = %+v, want succeeded/no failure", success)
	}

	failure := h.executeToolDetailed("failing_tool", `{}`, nil)
	if failure.Outcome != toolOutcomeFailed || failure.FailureKind != toolFailureHandlerReported {
		t.Fatalf("failure result metadata = %+v, want failed/handler_reported", failure)
	}

	empty := h.executeToolDetailed("empty_tool", `{}`, nil)
	if empty.Outcome != toolOutcomeUncertain || empty.FailureKind != toolFailureNone {
		t.Fatalf("empty result metadata = %+v, want uncertain/no failure", empty)
	}
}

func TestPinConditionalToolAfterSuccessRequiresSucceededOutcome(t *testing.T) {
	h := &IMMessageHandler{toolRouter: NewToolRouter(NewToolDefinitionGenerator(nil, nil))}

	h.pinConditionalToolAfterSuccess("ssh", toolExecutionResult{Outcome: toolOutcomeUncertain, FailureKind: toolFailureNone})
	if h.toolRouter.IsSessionPinned("ssh") {
		t.Fatal("uncertain outcome should not session-pin ssh")
	}

	h.pinConditionalToolAfterSuccess("ssh", toolExecutionResult{Outcome: toolOutcomeSucceeded, FailureKind: toolFailureNone})
	if !h.toolRouter.IsSessionPinned("ssh") {
		t.Fatal("succeeded outcome should session-pin ssh")
	}
}

func TestDiscoverToolDoesNotSessionPinConditionalTool(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "ssh", Description: "SSH remote server access", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register ssh: %v", err)
	}
	h := &IMMessageHandler{registry: registry, toolRouter: NewToolRouter(NewToolDefinitionGenerator(nil, nil))}

	out := h.toolDiscoverTool(map[string]interface{}{"need": "ssh remote server"})
	if !strings.Contains(out, "ssh") {
		t.Fatalf("discover output should mention ssh, got %q", out)
	}
	if h.toolRouter.IsSessionPinned("ssh") {
		t.Fatal("discover_tool should not session-pin ssh before actual successful use")
	}
}

func TestDiscoverToolActivatesDeferredToolDefinition(t *testing.T) {
	guiObserveDef := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "gui_observe",
			"description": "observe desktop gui screen",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
	gen := NewToolDefinitionGenerator(nil, []map[string]interface{}{guiObserveDef})
	gen.SetDeferredTools([]string{"gui_observe"})
	h := &IMMessageHandler{toolDefGen: gen, cachedTools: []map[string]interface{}{{"name": "stale"}}, toolsCacheTime: time.Now()}

	out := h.toolDiscoverTool(map[string]interface{}{"need": "observe desktop gui"})
	if !strings.Contains(out, "gui_observe") || !strings.Contains(out, "activated") {
		t.Fatalf("discover output should activate gui_observe, got %q", out)
	}
	if h.cachedTools != nil {
		t.Fatalf("discover activation should invalidate cached tools")
	}
	if !toolDefsContain(gen.Generate(), "gui_observe") {
		t.Fatalf("activated deferred tool should be included by Generate")
	}
}

func TestDiscoverToolSkipsHiddenDispatchTools(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "browser", Description: "browser automation merged tool", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register browser: %v", err)
	}
	if err := registry.Register(RegisteredTool{Name: "browser_navigate", Description: "", Tags: []string{"browser", "navigate"}, Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register browser_navigate: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	out := h.toolDiscoverTool(map[string]interface{}{"need": "browser navigate page"})
	if strings.Contains(out, "browser_navigate") {
		t.Fatalf("discover_tool should hide internal dispatch tools, got %q", out)
	}
	if !strings.Contains(out, "browser") {
		t.Fatalf("discover_tool should still show merged browser tool, got %q", out)
	}
}

func TestDirectInternalBrowserToolCallRewritesToMergedBrowser(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	if err := registry.Register(RegisteredTool{
		Name:        "browser",
		Description: "browser automation merged tool",
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			received = args
			return "merged"
		},
	}); err != nil {
		t.Fatalf("Register browser: %v", err)
	}
	if err := registry.Register(RegisteredTool{
		Name:        "browser_navigate",
		Description: "",
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			return "legacy"
		},
	}); err != nil {
		t.Fatalf("Register browser_navigate: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	result := h.executeToolDetailed("browser_navigate", `{"url":"https://example.com","session_id":"browser-session-test"}`, nil)
	if result.Text != "merged" {
		t.Fatalf("executeToolDetailed text = %q, want merged", result.Text)
	}
	if result.ToolName != "browser" {
		t.Fatalf("ToolName = %q, want browser", result.ToolName)
	}
	if received["action"] != "navigate" || received["url"] != "https://example.com" {
		t.Fatalf("merged browser args = %#v", received)
	}
}

func TestInternalBrowserToolCallJSONRewriteAddsAction(t *testing.T) {
	name, argsJSON, rewritten := rewriteInternalBrowserToolCallJSON("browser_click", `{"session_id":"browser-session-test","ref":"@e1"}`)
	if !rewritten || name != "browser" {
		t.Fatalf("rewrite = (%q, %q, %v), want browser/.../true", name, argsJSON, rewritten)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("rewritten args JSON invalid: %v", err)
	}
	if args["action"] != "click" || args["ref"] != "@e1" {
		t.Fatalf("rewritten args = %#v", args)
	}
}

func TestMergedBrowserToolReceivesRuntimeOwner(t *testing.T) {
	if !toolAcceptsRuntimePolicyOwnerArg("browser") {
		t.Fatal("merged browser tool must accept hidden runtime owner args")
	}
	registry := NewToolRegistry()
	var received map[string]interface{}
	if err := registry.Register(RegisteredTool{
		Name:        "browser",
		Description: "browser automation merged tool",
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			received = cloneMISInterfaceMap(args)
			return "merged"
		},
	}); err != nil {
		t.Fatalf("Register browser: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	result := h.executeToolDetailedWithRuntimeState("owner-browser", true, "", "browser", `{"action":"session_start"}`, "", nil)
	if result.Text != "merged" {
		t.Fatalf("executeToolDetailedWithRuntimeState text = %q, want merged", result.Text)
	}
	if got := received[registeredToolPolicyOwnerIDField]; got != "owner-browser" {
		t.Fatalf("runtime owner = %#v, want owner-browser; args=%#v", got, received)
	}
}

func TestUnstableBrowserToolCallRejectedBeforeExecution(t *testing.T) {
	registry := NewToolRegistry()
	called := false
	if err := registry.Register(RegisteredTool{
		Name:        "browser",
		Description: "browser automation merged tool",
		Status:      RegToolAvailable,
		Handler: func(args map[string]interface{}) string {
			called = true
			return "legacy path reached"
		},
	}); err != nil {
		t.Fatalf("Register browser: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	result := h.executeToolDetailed("browser", `{"action":"eval","expression":"document.cookie"}`, nil)
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got outcome=%v failure=%v text=%q", result.Outcome, result.FailureKind, result.Text)
	}
	if called {
		t.Fatal("unstable browser action reached browser handler")
	}
}

func TestAgentLoopRejectsUnstableBrowserBeforeAudit(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	recorded := false

	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{ID: "call-1", Function: llm.ToolCallFunction{Name: "browser_eval", Arguments: `{"expression":"fetch('/api', {method: 'POST'})"}`}},
		RecordToolCall: func(string, string, string) {
			recorded = true
		},
	})
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got outcome=%v failure=%v text=%q", result.Outcome, result.FailureKind, result.Text)
	}
	if recorded {
		t.Fatal("unstable browser action should be rejected before tool-call audit/progress")
	}
}

func TestFilterInactiveDeferredToolsForRegistryPath(t *testing.T) {
	guiObserveDef := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "gui_observe",
			"description": "observe desktop gui screen",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
	bashDef := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "bash",
			"description": "run command",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
	gen := NewToolDefinitionGenerator(nil, []map[string]interface{}{guiObserveDef})
	gen.SetDeferredTools([]string{"gui_observe"})
	h := &IMMessageHandler{toolDefGen: gen}

	filtered := h.filterInactiveDeferredTools([]map[string]interface{}{bashDef, guiObserveDef})
	if toolDefsContain(filtered, "gui_observe") {
		t.Fatalf("inactive deferred tool should be filtered from registry-built tools")
	}
	if !toolDefsContain(filtered, "bash") {
		t.Fatalf("non-deferred tool should remain available")
	}

	gen.ActivateDeferredTool("gui_observe")
	filtered = h.filterInactiveDeferredTools([]map[string]interface{}{bashDef, guiObserveDef})
	if !toolDefsContain(filtered, "gui_observe") {
		t.Fatalf("activated deferred tool should remain available")
	}
}

func TestPreCheckToolArgsForAgentLoopArgumentParseFailureUsesMetadata(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "write_file",
		Description: "Write file",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"path", "content"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	result := h.preCheckToolArgsForAgentLoop("write_file", `{"path":`, 2)
	if result == nil {
		t.Fatal("expected precheck argument parse failure")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("unexpected metadata: %+v", *result)
	}
	if !strings.Contains(result.Text, "arguments JSON parse failed") {
		t.Fatalf("expected parse failure text, got: %q", result.Text)
	}
}

func TestPreCheckAgentLoopInlinePayloadLimitGuidesChunking(t *testing.T) {
	writeResult := preCheckAgentLoopInlinePayloadLimit("write_file", fmt.Sprintf(`{"path":"out.txt","content":%q}`, strings.Repeat("x", maxAgentLoopInlineWriteFileContentRunes+1)), 3)
	if writeResult == nil {
		t.Fatal("expected oversized write_file content to be rejected before execution")
	}
	if writeResult.Outcome != toolOutcomeFailed || writeResult.FailureKind != toolFailureValidation {
		t.Fatalf("unexpected write_file metadata: %+v", *writeResult)
	}
	for _, want := range []string{"too large", "mode=overwrite", "mode=append"} {
		if !strings.Contains(writeResult.Text, want) {
			t.Fatalf("write_file result %q missing %q", writeResult.Text, want)
		}
	}

	bashResult := preCheckAgentLoopInlinePayloadLimit("bash", fmt.Sprintf(`{"command":%q}`, strings.Repeat("x", maxAgentLoopInlineBashCommandRunes+1)), 4)
	if bashResult == nil {
		t.Fatal("expected oversized bash command to be rejected before execution")
	}
	for _, want := range []string{"too large", "Do not embed generated file bodies", "craft_tool"} {
		if !strings.Contains(bashResult.Text, want) {
			t.Fatalf("bash result %q missing %q", bashResult.Text, want)
		}
	}
}

func TestExecuteAgentLoopToolCallRejectsOversizedInlinePayloadBeforeHandler(t *testing.T) {
	registry := NewToolRegistry()
	executed := false
	if err := registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "Shell",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"command"},
		},
		Handler: func(args map[string]interface{}) string {
			executed = true
			return "should not execute"
		},
	}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}
	h := &IMMessageHandler{registry: registry}

	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{
			ID: "call_large_bash",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: fmt.Sprintf(`{"command":%q}`, strings.Repeat("x", maxAgentLoopInlineBashCommandRunes+1)),
			},
		},
	})

	if executed {
		t.Fatal("oversized bash payload reached handler")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailureValidation {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
	if !strings.Contains(result.Text, "too large") || !strings.Contains(result.Text, "Do not embed generated file bodies") {
		t.Fatalf("unexpected oversized payload guidance: %q", result.Text)
	}
}

func TestParseToolPayloadResult(t *testing.T) {
	image := parseToolPayloadResult("[screenshot_base64]abc123")
	if image.ImageKey != "abc123" || image.TraceResult != "Tool result prepared for the user." {
		t.Fatalf("image payload = %#v", image)
	}
	file := parseToolPayloadResult("[file_base64|report.pdf|application/pdf|im]ZGF0YQ==")
	if file.File == nil || file.File.name != "report.pdf" || !file.File.forwardIM {
		t.Fatalf("file payload = %#v", file)
	}
	if file.File.message == "" {
		t.Fatalf("expected file delivery message, got %#v", file.File)
	}
	withMessage := parseToolPayloadResult("[file_base64|01-requirements.pdf|application/pdf|im|msg64:" + strings.TrimPrefix(encodeToolPayloadMessage("需求文档已生成"), "msg64:") + "]ZGF0YQ==")
	if withMessage.File == nil || withMessage.File.message != "需求文档已生成" {
		t.Fatalf("file payload message = %#v", withMessage.File)
	}
	pdfMessage := parseToolPayloadResult("[file_base64|01-requirements.pdf|application/pdf|msg64:" + strings.TrimPrefix(encodeToolPayloadMessage("需求文档已生成"), "msg64:") + "]ZGF0YQ==")
	if pdfMessage.File == nil || pdfMessage.File.forwardIM || pdfMessage.File.message != "需求文档已生成" {
		t.Fatalf("generate_pdf-style payload message = %#v", pdfMessage.File)
	}
	voice := parseToolPayloadResult("[voice_base64|voice.ogg|audio/ogg]AAAA")
	if voice.VoiceData != "AAAA" || voice.VoiceFileName != "voice.ogg" || voice.VoiceMimeType != "audio/ogg" {
		t.Fatalf("voice payload = %#v", voice)
	}
}

func TestBuildRemoteSkillSearchPrompt(t *testing.T) {
	prompt := buildRemoteSkillSearchPrompt()
	if !contains(prompt, "Search/install a reusable Skill first") {
		t.Fatalf("expected reusable Skill guidance, got %q", prompt)
	}
	if !contains(prompt, "Only switch to craft_tool or bash") {
		t.Fatalf("expected craft_tool restriction, got %q", prompt)
	}
}

func TestBuildNoToolActionPrompt(t *testing.T) {
	prompt := buildNoToolActionPrompt(true, "hf_daily_papers_report", "")
	if !contains(prompt, `manage_skill(action="run", name="hf_daily_papers_report")`) {
		t.Fatalf("expected preferred skill action guidance, got %q", prompt)
	}
	if !contains(prompt, `get_skill_run(run_id=...)`) {
		t.Fatalf("expected get_skill_run guidance, got %q", prompt)
	}

	runningPrompt := buildNoToolActionPrompt(true, "hf_daily_papers_report", "run-456")
	if !contains(runningPrompt, `get_skill_run(run_id="run-456")`) {
		t.Fatalf("expected concrete run_id guidance, got %q", runningPrompt)
	}
	if contains(runningPrompt, `manage_skill(action="run", name="hf_daily_papers_report")`) {
		t.Fatalf("expected running prompt to avoid restarting skill, got %q", runningPrompt)
	}

	fallbackPrompt := buildNoToolActionPrompt(false, "", "")
	if !contains(fallbackPrompt, "Choose the best real tool and start executing") {
		t.Fatalf("expected generic action guidance, got %q", fallbackPrompt)
	}
}

func TestBuildNoToolStallRecoverPrompt(t *testing.T) {
	prompt := buildNoToolStallRecoverPrompt(2, true, "hf_daily_papers_report", "")
	if !contains(prompt, "No real tool was called for 2 consecutive rounds") {
		t.Fatalf("expected stall count, got %q", prompt)
	}
	if !contains(prompt, `manage_skill(action="run", name="hf_daily_papers_report")`) {
		t.Fatalf("expected preferred skill guidance, got %q", prompt)
	}
	runningPrompt := buildNoToolStallRecoverPrompt(2, true, "hf_daily_papers_report", "run-456")
	if !contains(runningPrompt, `get_skill_run(run_id="run-456")`) {
		t.Fatalf("expected concrete run_id guidance, got %q", runningPrompt)
	}
	if contains(runningPrompt, `manage_skill(action="run", name="hf_daily_papers_report")`) {
		t.Fatalf("expected running prompt to avoid restarting skill, got %q", runningPrompt)
	}

	fallbackPrompt := buildNoToolStallRecoverPrompt(3, false, "", "")
	if !contains(fallbackPrompt, "No real tool was called for 3 consecutive rounds") {
		t.Fatalf("expected fallback stall count, got %q", fallbackPrompt)
	}
	if !contains(fallbackPrompt, "Choose the best real tool now") {
		t.Fatalf("expected generic execution guidance, got %q", fallbackPrompt)
	}
}

func TestBuildSkillRecoverPrompt_PrefersConcreteRunID(t *testing.T) {
	prompt := buildSkillRecoverPrompt("hf_daily_papers_report", "run-123")
	if !contains(prompt, `get_skill_run(run_id="run-123")`) {
		t.Fatalf("expected concrete run_id guidance, got %q", prompt)
	}
}

func TestBuildDeliverableRecoverPrompt_PrefersConcreteRunID(t *testing.T) {
	prompt := buildDeliverableRecoverPrompt("hf_daily_papers_report", true, "run-789")
	if !contains(prompt, `get_skill_run(run_id="run-789")`) {
		t.Fatalf("expected concrete run_id guidance, got %q", prompt)
	}
	if contains(prompt, `run_skill(name="hf_daily_papers_report")`) {
		t.Fatalf("expected deliverable prompt to avoid restarting skill, got %q", prompt)
	}
}

func TestLooksLikePDFRelatedWork(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"生成 PDF 综述并发送给我", true},
		{"use pandoc to convert markdown to pdf", true},
		{"reportlab script for markdown to pdf", true},
		{"run npm test", false},
		{"plain shell command", false},
	}
	for _, tc := range cases {
		if got := looksLikePDFRelatedWork(tc.input); got != tc.want {
			t.Fatalf("looksLikePDFRelatedWork(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestResolveBashTimeout_PrefersPDFDefaultOnlyWhenNoExplicitTimeout(t *testing.T) {
	if got := resolveBashTimeout(map[string]interface{}{}, "python render_pdf.py --input review.md"); got != bashPDFTimeout {
		t.Fatalf("resolveBashTimeout(pdf) = %d, want %d", got, bashPDFTimeout)
	}
	if got := resolveBashTimeout(map[string]interface{}{}, "npm test"); got != bashDefaultTimeout {
		t.Fatalf("resolveBashTimeout(non-pdf) = %d, want %d", got, bashDefaultTimeout)
	}
	if got := resolveBashTimeout(map[string]interface{}{"timeout": float64(360)}, "python render_pdf.py --input review.md"); got != 360 {
		t.Fatalf("resolveBashTimeout(explicit) = %d, want 360", got)
	}
	if got := resolveBashTimeout(map[string]interface{}{"timeout": float64(45)}, "python render_pdf.py --input review.md"); got != bashMinTimeout {
		t.Fatalf("resolveBashTimeout(explicit below min) = %d, want %d", got, bashMinTimeout)
	}
	if got := resolveBashTimeout(map[string]interface{}{"timeout": float64(999)}, "python render_pdf.py --input review.md"); got != bashMaxTimeout {
		t.Fatalf("resolveBashTimeout(clamped) = %d, want %d", got, bashMaxTimeout)
	}
}

func TestResolveCraftToolTimeout_PrefersPDFDefaultOnlyWhenNoExplicitTimeout(t *testing.T) {
	if got := resolveCraftToolTimeout(map[string]interface{}{}, "生成 PDF 综述文章并发给我"); got != craftToolPDFTimeout {
		t.Fatalf("resolveCraftToolTimeout(pdf) = %d, want %d", got, craftToolPDFTimeout)
	}
	if got := resolveCraftToolTimeout(map[string]interface{}{}, "抓取网页并保存为文本"); got != craftToolDefaultTimeout {
		t.Fatalf("resolveCraftToolTimeout(non-pdf) = %d, want %d", got, craftToolDefaultTimeout)
	}
	if got := resolveCraftToolTimeout(map[string]interface{}{"timeout": float64(90)}, "生成 PDF 综述文章并发给我"); got != 90 {
		t.Fatalf("resolveCraftToolTimeout(explicit) = %d, want 90", got)
	}
	if got := resolveCraftToolTimeout(map[string]interface{}{"timeout": float64(999)}, "生成 PDF 综述文章并发给我"); got != craftToolMaxTimeout {
		t.Fatalf("resolveCraftToolTimeout(clamped) = %d, want %d", got, craftToolMaxTimeout)
	}
}

func TestToolGeneratePDFValidation_RejectsOversizedContent(t *testing.T) {
	if err := swarm.ValidatePDFContent(strings.Repeat("a", 181*1024)); err == nil || !strings.Contains(err.Error(), "PDF 内容过长") {
		t.Fatalf("expected oversized content error, got %v", err)
	}
}

func TestToolGeneratePDFValidation_RejectsOversizedParagraph(t *testing.T) {
	if err := swarm.ValidatePDFContent(strings.Repeat("段", 48*1024+1)); err == nil || !strings.Contains(err.Error(), "过长段落") {
		t.Fatalf("expected oversized paragraph error, got %v", err)
	}
}

func TestToolGeneratePDFValidation_RejectsFilePayloadMarker(t *testing.T) {
	if err := swarm.ValidatePDFContent("# 报告\n\n[file_base64|x|application/pdf]AAAA"); err == nil || !strings.Contains(err.Error(), "文件载荷") {
		t.Fatalf("expected file payload error, got %v", err)
	}
}

// TestGetTools_FallbackWithoutGenerator verifies that getTools() returns
// builtin tools but strips external coding-session tools from the agent list.
func TestGetTools_FallbackWithoutGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.getTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty builtin tools")
	}

	assertNoCodingSessionTools(t, tools)

	// Verify first exposed tool is ssh after session tools are filtered.
	name := extractToolName(tools[0])
	if name != "ssh" {
		t.Errorf("expected first tool to be ssh, got %s", name)
	}
}

func hasToolNamed(tools []map[string]interface{}, name string) bool {
	for _, tool := range tools {
		if extractToolName(tool) == name {
			return true
		}
	}
	return false
}

// TestGetTools_UsesGeneratorWhenSet verifies that getTools() delegates to
// the ToolDefinitionGenerator when configured.
func TestGetTools_UsesGeneratorWhenSet(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	tools := handler.getTools()
	if len(tools) >= len(builtins) {
		t.Fatalf("expected coding-session tools to be filtered from generator output: builtins=%d tools=%d", len(builtins), len(tools))
	}
	assertNoCodingSessionTools(t, tools)
}

func assertNoCodingSessionTools(t *testing.T, tools []map[string]interface{}) {
	t.Helper()
	for _, tool := range tools {
		name := extractToolName(tool)
		if coretool.IsCodingSessionTool(name) {
			t.Fatalf("agent tool list should not expose external coding-session tool %s", name)
		}
	}
}

// TestGetTools_CacheWithin5Seconds verifies that repeated calls within 5s
// return the cached result without regenerating.
func TestGetTools_CacheWithin5Seconds(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// First call populates cache.
	tools1 := handler.getTools()
	// Second call should return same slice from cache.
	tools2 := handler.getTools()

	if len(tools1) != len(tools2) {
		t.Fatalf("cached tools length mismatch: %d vs %d", len(tools1), len(tools2))
	}

	// Verify cache timestamp was set.
	handler.toolsMu.RLock()
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be set after getTools()")
	}
}

// TestGetTools_CacheInvalidatedBySetGenerator verifies that calling
// SetToolDefGenerator invalidates the cache.
func TestGetTools_CacheInvalidatedBySetGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// Populate cache.
	_ = handler.getTools()

	// Set a new generator — should invalidate cache.
	gen2 := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen2)

	handler.toolsMu.RLock()
	cached := handler.cachedTools
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cached != nil {
		t.Error("expected cachedTools to be nil after SetToolDefGenerator")
	}
	if !cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be zero after SetToolDefGenerator")
	}
}

func TestGetTools_CacheInvalidatedBySetRegistry(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	handler.toolBuilder = NewDynamicToolBuilder(handler.registry)
	_ = handler.getTools()

	if handler.cachedTools == nil || handler.toolsCacheTime.IsZero() {
		t.Fatal("expected getTools to populate cache before registry replacement")
	}
	handler.SetToolRegistry(NewToolRegistry())

	if handler.cachedTools != nil {
		t.Fatal("expected cachedTools to be nil after SetToolRegistry")
	}
	if !handler.toolsCacheTime.IsZero() {
		t.Fatal("expected toolsCacheTime to be zero after SetToolRegistry")
	}
}

// TestRouteTools_NoRouterFailsClosed verifies that routeTools still applies
// conservative conditional-tool filtering when no router is configured.
func TestRouteTools_NoRouterFailsClosed(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.buildToolDefinitions()
	tools = append(tools, toolDef("set_nickname", "设置昵称", nil, nil))
	routed := handler.routeTools("open a browser and inspect the remote server", tools)

	if len(routed) == 0 || len(routed) > len(tools) {
		t.Fatalf("unexpected routed tool count without router: got %d of %d", len(routed), len(tools))
	}
	for _, item := range routed {
		name := extractToolName(item)
		if name == "browser" || name == "ssh" {
			t.Fatalf("conditional tool %q should fail closed without configured router", name)
		}
	}
}

func TestRouteTools_SetNicknameRequiresExplicitUserRequest(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	tools = append(tools, toolDef("set_nickname", "设置昵称", nil, nil))

	routed := handler.routeTools("检查 api 服务器", tools)
	for _, item := range routed {
		if extractToolName(item) == "set_nickname" {
			t.Fatal("set_nickname should not be exposed for unrelated tasks")
		}
	}

	routed = handler.routeTools("以后叫你小管家", tools)
	found := false
	for _, item := range routed {
		if extractToolName(item) == "set_nickname" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("set_nickname should be exposed for explicit rename requests")
	}
}

func TestExecuteAgentLoopToolCallRejectsUnsolicitedSetNickname(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:   desktopUserID,
		UserText: "检查 api 服务器",
		ToolCall: llm.ToolCall{ID: "call_1", Function: llm.ToolCallFunction{
			Name:      "set_nickname",
			Arguments: `{"nickname":"小管家"}`,
		}},
	})
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got outcome=%s failure=%s text=%q", result.Outcome, result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "only allowed") {
		t.Fatalf("unexpected rejection text: %q", result.Text)
	}
}

func TestToolSetNicknameOwnerlessCurrentRuntimeDoesNotUseLegacyTaskText(t *testing.T) {
	handler := &IMMessageHandler{
		app:            &App{},
		lastUserText:   "call me Desk Mate",
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-owner"}},
	}

	got := handler.toolSetNickname(map[string]interface{}{"nickname": "Desk Mate"})
	if !strings.Contains(got, "set_nickname") && !strings.Contains(got, "仅在用户明确要求") {
		t.Fatalf("ownerless current runtime should not inherit legacy nickname request, got %q", got)
	}
}

// TestRouteTools_WithRouterFilters verifies that routeTools delegates to
// the ToolRouter when configured.
func TestRouteTools_WithRouterFilters(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	gen := NewToolDefinitionGenerator(nil, handler.buildToolDefinitions())
	router := NewToolRouter(gen)
	handler.SetToolRouter(router)

	// With total tools exceeding maxToolBudget, router may filter dynamic tools.
	// Core tools are always kept; remaining budget goes to TF-IDF ranked candidates.
	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("test message", tools)

	if len(routed) > len(tools) {
		t.Fatalf("routed tools (%d) should not exceed total tools (%d)", len(routed), len(tools))
	}
	if len(routed) == 0 {
		t.Fatal("expected non-empty routed tools")
	}
}

// TestBuildToolDefinitions_WebFetchHasContinuationParams verifies that the
// web_fetch tool definition advertises continuation parameters.
func TestBuildToolDefinitions_WebFetchHasContinuationParams(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	for _, tool := range tools {
		if extractToolName(tool) != "web_fetch" {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		if !contains(desc, "has_more=true") || !contains(desc, "offset=next_offset") {
			t.Fatalf("unexpected web_fetch description: %s", desc)
		}
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		for _, field := range []string{"offset", "max_chars"} {
			entry, ok := props[field]
			if !ok {
				t.Fatalf("web_fetch missing %q", field)
			}
			meta, _ := entry.(map[string]string)
			if meta["type"] != "integer" {
				t.Fatalf("web_fetch %q type = %#v", field, meta["type"])
			}
		}
		return
	}
	t.Fatal("web_fetch tool not found in buildToolDefinitions")
}

func TestRouteTools_WithRouterKeepsSSHForSSHIntent(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	for i := 0; i < 20; i++ {
		handler.registry.Register(RegisteredTool{
			Name:        fmt.Sprintf("extra_%d", i),
			Description: fmt.Sprintf("extra tool %d", i),
			Category:    ToolCategoryNonCode,
			Status:      RegToolAvailable,
		})
	}

	builder := NewDynamicToolBuilder(handler.registry)
	tools := builder.BuildAll()
	if len(tools) <= maxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", maxToolBudget, len(tools))
	}

	gen := NewToolDefinitionGenerator(nil, tools)
	router := NewToolRouter(gen)
	ic := coretool.NewIntentClassifier(embedding.NoopEmbedder{})
	defer ic.Close()
	ic.SetLLMFunc(func(prompt string) (string, error) {
		if strings.Contains(prompt, "home.rapidai.tech") {
			return coretool.IntentSSH, nil
		}
		return coretool.IntentUnknown, nil
	})
	router.SetIntentClassifier(ic)
	handler.SetToolRouter(router)

	runCase := func(message string, wantSSH bool) {
		routed := handler.routeTools(message, tools)
		foundSSH := false
		foundCallMCPTool := false
		for _, tool := range routed {
			switch extractToolName(tool) {
			case "ssh":
				foundSSH = true
			case "call_mcp_tool":
				foundCallMCPTool = true
			}
		}
		if foundSSH != wantSSH {
			names := make([]string, len(routed))
			for i, tool := range routed {
				names[i] = extractToolName(tool)
			}
			t.Fatalf("ssh presence for %q = %v, want %v; got: %v", message, foundSSH, wantSSH, names)
		}
		if wantSSH && foundCallMCPTool {
			t.Fatalf("call_mcp_tool should be hidden when ssh is routed for %q", message)
		}
	}

	runCase("登录 4090 服务器，host home.rapidai.tech 端口 33", true)
	// Reset session between independent test cases so the ssh pin from
	// the first case doesn't carry over. In production, session-pinned
	// tools persist across messages (which is the desired behavior for
	// follow-up SSH operations), but these are independent test scenarios.
	router.ResetSession()
	runCase("我要查询数据库", false)
}

func TestRouteTools_WiresHandlerUnifiedClassifierIntoRouter(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	for i := 0; i < 20; i++ {
		handler.registry.Register(RegisteredTool{
			Name:        fmt.Sprintf("extra_%d", i),
			Description: fmt.Sprintf("extra tool %d", i),
			Category:    ToolCategoryNonCode,
			Status:      RegToolAvailable,
		})
	}
	tools := NewDynamicToolBuilder(handler.registry).BuildAll()
	if len(tools) <= maxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", maxToolBudget, len(tools))
	}
	handler.unifiedClassifier = intent.New(intent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(_, _ string) (string, error) {
			return `{"top":[{"skill":"ssh","score":0.95,"reason":"server operation"}]}`, nil
		},
		LLMTimeout: time.Second,
	})

	router := NewToolRouter(NewToolDefinitionGenerator(nil, tools))
	handler.SetToolRouter(router)
	routed := handler.routeTools("将驱网服务器上的 19080 端口反代到 ve.mypapers.top", tools)
	foundSSH := false
	foundCallMCPTool := false
	for _, tool := range routed {
		switch extractToolName(tool) {
		case "ssh":
			foundSSH = true
		case "call_mcp_tool":
			foundCallMCPTool = true
		}
	}
	if !foundSSH {
		t.Fatalf("expected ssh to be routed from handler unified classifier")
	}
	if foundCallMCPTool {
		t.Fatalf("call_mcp_tool should be hidden when ssh is routed")
	}
}

func TestToolCallMCPToolRejectsBuiltinToolRefs(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	if err := handler.registry.Register(RegisteredTool{
		Name:     "ssh",
		Category: ToolCategoryBuiltin,
		Status:   RegToolAvailable,
		Handler: func(map[string]interface{}) string {
			return "builtin executed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	out := handler.toolCallMCPTool(map[string]interface{}{
		"server_id": "ssh",
		"tool_name": "ssh",
		"arguments": map[string]interface{}{"action": "list"},
	})
	if strings.Contains(out, "builtin executed") {
		t.Fatalf("call_mcp_tool must not forward to builtin tools: %q", out)
	}
	if !strings.Contains(out, "MCP 调用被拒绝") || !strings.Contains(out, "直接调用 ssh") {
		t.Fatalf("unexpected rejection text: %q", out)
	}
}

func TestToolCallMCPToolRejectsDisabledExternalCodingSessionTargets(t *testing.T) {
	handler := &IMMessageHandler{}
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		out := handler.toolCallMCPTool(map[string]interface{}{
			"server_id": "external",
			"tool_name": name,
		})
		if !strings.Contains(out, name+" is disabled") {
			t.Fatalf("call_mcp_tool target %s should be disabled, got %q", name, out)
		}
	}
}

func TestPreCheckToolArgsForAgentLoopRejectsDisabledMCPExternalCodingSessionTargets(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.preCheckMCPToolArgsForAgentLoop(map[string]interface{}{
		"server_id": "external",
		"tool_name": "create_session",
	}, 1)
	if result == nil || result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("precheck should reject disabled MCP target before resolution, got %#v", result)
	}
}

func TestPreCheckToolArgsForAgentLoopReturnsMCPValidationToLLM(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "call_mcp_tool",
		Description: "Call MCP tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"server_id": map[string]interface{}{"type": "string"},
				"tool_name": map[string]interface{}{"type": "string"},
				"arguments": map[string]interface{}{"type": "object"},
			},
			"required": []interface{}{"server_id", "tool_name"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mgr := NewLocalMCPManager(nil)
	mgr.clients["wiki"] = &LocalMCPClient{
		entry: corelib.LocalMCPServerEntry{ID: "wiki", Name: "Wiki"},
		tools: []MCPToolView{{
			Name: "confluence_get_page_children",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parent_id": map[string]interface{}{"type": "string"},
					"limit":     map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"parent_id"},
			},
		}},
		running: true,
	}
	handler := &IMMessageHandler{app: &App{localMCPManager: mgr}, registry: registry}

	result := handler.preCheckToolArgsForAgentLoop("call_mcp_tool", `{"server_id":"wiki","tool_name":"confluence_get_page_children","arguments":{"limit":25}}`, 3)
	if result == nil {
		t.Fatal("expected agent-loop MCP validation result")
	}
	if result.FailureKind != toolFailureMissingParameters || result.Outcome != toolOutcomeFailed {
		t.Fatalf("unexpected result metadata: %+v", *result)
	}
	if !strings.Contains(result.Text, "parent_id") || !strings.Contains(result.Text, "Missing required MCP argument") {
		t.Fatalf("validation result should tell LLM which MCP argument to recover, got: %q", result.Text)
	}
	if strings.Contains(result.Text, "task panel") || strings.Contains(result.Text, "form") {
		t.Fatalf("agent-loop validation must not route to manual AgentView form: %q", result.Text)
	}
}

func TestPreCheckToolArgsForAgentLoopAcceptsMCPArgumentsJSONString(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "call_mcp_tool",
		Description: "Call MCP tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"server_id": map[string]interface{}{"type": "string"},
				"tool_name": map[string]interface{}{"type": "string"},
				"arguments": map[string]interface{}{"type": "object"},
			},
			"required": []interface{}{"server_id", "tool_name"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	mgr := NewLocalMCPManager(nil)
	mgr.clients["wiki"] = &LocalMCPClient{
		entry: corelib.LocalMCPServerEntry{ID: "wiki", Name: "Wiki"},
		tools: []MCPToolView{{
			Name: "confluence_get_page_children",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parent_id": map[string]interface{}{"type": "string"},
					"limit":     map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"parent_id"},
			},
		}},
		running: true,
	}
	handler := &IMMessageHandler{app: &App{localMCPManager: mgr}, registry: registry}

	result := handler.preCheckToolArgsForAgentLoop("call_mcp_tool", `{"server_id":"wiki","tool_name":"confluence_get_page_children","arguments":"{\"limit\":25}"}`, 4)
	if result == nil {
		t.Fatal("expected inner MCP validation result")
	}
	if result.FailureKind != toolFailureMissingParameters || !strings.Contains(result.Text, "parent_id") {
		t.Fatalf("expected inner missing parent_id, got: %+v", *result)
	}
	if strings.Contains(result.Text, "arguments must be an object") {
		t.Fatalf("MCP JSON-string arguments should be normalized before outer validation: %q", result.Text)
	}
}

func TestToolCallMCPToolAcceptsCleanableArgumentsJSONString(t *testing.T) {
	mgr := NewLocalMCPManager(nil)
	mgr.clients["wiki"] = &LocalMCPClient{
		entry: corelib.LocalMCPServerEntry{ID: "wiki", Name: "Wiki"},
		tools: []MCPToolView{{
			Name: "confluence_get_page_children",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parent_id": map[string]interface{}{"type": "string"},
					"limit":     map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"parent_id"},
			},
		}},
		running: true,
	}
	handler := &IMMessageHandler{app: &App{localMCPManager: mgr}}

	out := handler.toolCallMCPTool(map[string]interface{}{
		"server_id": "wiki",
		"tool_name": "confluence_get_page_children",
		"arguments": "```json\n{\"limit\":25}\n```",
	})
	if strings.Contains(out, "JSON") && strings.Contains(out, "瑙ｆ瀽") {
		t.Fatalf("cleanable JSON-string arguments should not fail parsing: %q", out)
	}
	if !strings.Contains(out, "parent_id") {
		t.Fatalf("expected inner MCP validation after parsing, got: %q", out)
	}
}

func TestPreCheckToolArgsForAgentLoopRejectsInvalidMCPArgumentsShape(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:        "call_mcp_tool",
		Description: "Call MCP tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"server_id": map[string]interface{}{"type": "string"},
				"tool_name": map[string]interface{}{"type": "string"},
				"arguments": map[string]interface{}{},
			},
			"required": []interface{}{"server_id", "tool_name"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	handler := &IMMessageHandler{registry: registry}

	result := handler.preCheckToolArgsForAgentLoop("call_mcp_tool", `{"server_id":"wiki","tool_name":"search","arguments":["bad"]}`, 1)
	if result == nil || result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("expected argument parse failure, got: %+v", result)
	}
	if !strings.Contains(result.Text, "arguments must be an object") {
		t.Fatalf("expected actionable argument shape error, got: %q", result.Text)
	}
}

func TestBuiltinToolServerRefIgnoresExternalToolName(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	if got := handler.builtinToolServerRef("external-mcp"); got != "" {
		t.Fatalf("builtinToolServerRef(external-mcp) = %q, want empty", got)
	}
	if got := handler.builtinToolServerRef("ssh"); got != "ssh" {
		t.Fatalf("builtinToolServerRef(ssh) = %q, want ssh", got)
	}
}

func TestRouteTools_WithRouterKeepsMISDataForBusinessTransactionIntent(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	for i := 0; i < 20; i++ {
		handler.registry.Register(RegisteredTool{
			Name:        fmt.Sprintf("extra_%d", i),
			Description: fmt.Sprintf("extra tool %d", i),
			Category:    ToolCategoryNonCode,
			Status:      RegToolAvailable,
		})
	}

	tools := NewDynamicToolBuilder(handler.registry).BuildAll()
	if len(tools) <= maxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", maxToolBudget, len(tools))
	}
	router := NewToolRouter(NewToolDefinitionGenerator(nil, tools))
	router.SetUnifiedClassifier(intent.New(intent.Config{
		Embedder: embedding.NewNoopEmbedder(),
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"business_data","score":0.92,"workflow_type":""},{"skill":"non_coding","score":0.35,"workflow_type":""},{"skill":"continuation","score":0.30,"workflow_type":""}]}`, nil
		},
	}))
	handler.SetToolRouter(router)

	routed := handler.routeTools("继续处理上次差旅报销录入，打开未完成事务", tools)
	foundMISData := false
	for _, tool := range routed {
		if extractToolName(tool) == "mis_data" {
			foundMISData = true
			break
		}
	}
	if !foundMISData {
		names := make([]string, len(routed))
		for i, tool := range routed {
			names[i] = extractToolName(tool)
		}
		t.Fatalf("mis_data should be routed for business transaction intent, got: %v", names)
	}
}

func TestToolWebFetchIncludesContinuationMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Long Page</title></head><body><main>ABCDEFGHIJ</main></body></html>`))
	}))
	defer server.Close()

	h := &IMMessageHandler{}
	result := h.toolWebFetch(map[string]interface{}{
		"url":       server.URL,
		"offset":    2,
		"max_chars": 4,
	})
	if !contains(result, "标题: Long Page") {
		t.Fatalf("missing title in result: %s", result)
	}
	if !contains(result, "已读取: 2-6 / 10 字符") {
		t.Fatalf("missing window range: %s", result)
	}
	if !contains(result, "truncated: true | has_more: true | next_offset: 6") {
		t.Fatalf("missing continuation metadata: %s", result)
	}
	if !contains(result, "CDEF") {
		t.Fatalf("missing windowed content: %s", result)
	}
	if !contains(result, "继续读取时请传入 offset=6") {
		t.Fatalf("missing continuation hint: %s", result)
	}
}

func TestTruncateToolResultForWebFetchPreservesIntegritySignal(t *testing.T) {
	meta := "\n\n--- 完整性信号 ---\nhas_more: true\nnext_offset: 123\n继续读取时请传入 offset=123\n"
	s := "标题: demo\n\n" + strings.Repeat("A", webFetchMaxToolResult) + meta
	got := truncateToolResultForTool("web_fetch", s)
	if len(got) > webFetchMaxToolResult {
		t.Fatalf("len(got) = %d", len(got))
	}
	if !contains(got, "--- 完整性信号 ---") || !contains(got, "next_offset: 123") {
		t.Fatalf("missing integrity metadata after truncation: %s", got)
	}
}

// TestToolsCacheTTL_Value verifies the cache TTL constant is 5 seconds.
func TestToolsCacheTTL_Value(t *testing.T) {
	expected := 5 * time.Second
	if toolsCacheTTL != expected {
		t.Errorf("expected toolsCacheTTL = %v, got %v", expected, toolsCacheTTL)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 7.2: Smart session startup & template tools
// ---------------------------------------------------------------------------

// TestToolCreateSession_SmartToolRecommendation verifies that toolCreateSession
// auto-recommends a tool when the tool parameter is empty and contextResolver is set.
func TestToolCreateSession_SmartToolRecommendation(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{
		app:          &App{},
		lastUserText: "请修改代码并创建会话",
	}
	handler.unifiedClassifier = testCodingIntentClassifier()

	// Without contextResolver, empty tool should return error.
	result := handler.toolCreateSession(map[string]interface{}{})
	if result != "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolCreateSession_WithToolProvided verifies that toolCreateSession
// uses the provided tool parameter directly (no auto-recommendation).
func TestToolCreateSession_WithToolProvided(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{
		app:          &App{},
		lastUserText: "fix code and create a coding session",
	}
	handler.unifiedClassifier = testCodingIntentClassifier()

	// With tool provided but no manager, should fail at session creation.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Should attempt to create session (will fail because app is minimal).
	if result == "缺少 tool 参数" || result == "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("should not report missing tool when tool is provided, got: %s", result)
	}
}

func TestToolCreateSession_SSHIntentBlocked(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "ssh to 10.0.0.8 and inspect nginx logs"}
	handler.unifiedClassifier = testIntentClassifier("ssh")
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "SSH/server operation") || !contains(result, "Use the ssh tool") {
		t.Fatalf("expected ssh redirect hint, got: %s", result)
	}
}

func TestToolCreateSession_NonCodingIntentBlocked(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "translate this paper"}
	handler.unifiedClassifier = testIntentClassifier("non_coding")
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "not a coding task") || !contains(result, "read_file / write_file / edit_file") {
		t.Fatalf("expected non-coding guard hint, got: %s", result)
	}
}

func TestToolCreateSession_AmbiguousIntentBlocked(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "help me handle the production issue"}
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "ambiguous") || !contains(result, "Do not create a coding session yet") {
		t.Fatalf("expected ambiguous guard hint, got: %s", result)
	}
}

func TestToolCreateSessionDisabled(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}, lastUserText: "fix code"}
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "create_session is disabled") || !contains(result, "CodingSubAgent") {
		t.Fatalf("expected disabled CodingSubAgent guidance, got: %s", result)
	}
}

func TestSessionTaskGuardOwnerlessCurrentRuntimeDoesNotUseLegacyTaskText(t *testing.T) {
	handler := &IMMessageHandler{
		app:            &App{},
		lastUserText:   "fix code and create a coding session",
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-owner"}},
	}

	if got := handler.checkSessionTaskGuard(); got == "" {
		t.Fatal("ownerless current runtime inherited legacy coding text and allowed session creation")
	}
}

func TestExternalCodingSessionFollowupToolsDisabled(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	for name, call := range map[string]func() string{
		"send_input": func() string { return handler.toolSendInput(map[string]interface{}{"session_id": "s1", "text": "go"}) },
		"send_and_observe": func() string {
			return handler.toolSendAndObserve(map[string]interface{}{"session_id": "s1", "text": "go"})
		},
		"parallel_execute": func() string { return handler.toolParallelExecute(map[string]interface{}{"tasks": []interface{}{}}) },
		"launch_template":  func() string { return handler.toolLaunchTemplate(map[string]interface{}{"template_name": "t1"}) },
	} {
		result := call()
		if !contains(result, name+" is disabled") || !contains(result, "CodingSubAgent") {
			t.Fatalf("%s should be disabled with CodingSubAgent guidance, got: %s", name, result)
		}
	}
}

func TestExecuteToolDetailedRejectsExternalCodingSessionTools(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		result := handler.executeToolDetailed(name, `{}`, nil)
		if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
			t.Fatalf("%s outcome = %s/%s, want failed/policy_rejected", name, result.Outcome, result.FailureKind)
		}
		if !contains(result.Text, name+" is disabled") {
			t.Fatalf("%s should return disabled text, got %q", name, result.Text)
		}
	}
}

// TestToolCreateTemplate verifies template creation via the tool.
func TestToolCreateTemplate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Missing required params.
	result := handler.toolCreateTemplate(map[string]interface{}{})
	if result != "缺少 name 或 tool 参数" {
		t.Errorf("expected missing params error, got: %s", result)
	}

	// Successful creation.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name":         "my-template",
		"tool":         "claude",
		"project_path": "/tmp/project",
		"yolo_mode":    true,
	})
	if result != "模板已创建: my-template（工具=claude, 项目=/tmp/project）" {
		t.Errorf("unexpected result: %s", result)
	}

	// Duplicate name.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name": "my-template",
		"tool": "codex",
	})
	if result == "" || !contains(result, "创建模板失败") {
		t.Errorf("expected duplicate error, got: %s", result)
	}
}

// TestToolCreateTemplate_NilManager verifies graceful handling when
// templateManager is nil.
func TestToolCreateTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolCreateTemplate(map[string]interface{}{
		"name": "test", "tool": "claude",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolListTemplates verifies listing templates.
func TestToolListTemplates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Empty list.
	result := handler.toolListTemplates()
	if result != "当前没有会话模板。" {
		t.Errorf("expected empty list message, got: %s", result)
	}

	// Add a template and list again.
	_ = mgr.Create(remote.SessionTemplate{Name: "dev", Tool: "claude", ProjectPath: "/tmp/dev", YoloMode: true})
	result = handler.toolListTemplates()
	if !contains(result, "dev") || !contains(result, "claude") || !contains(result, "[Yolo]") {
		t.Errorf("expected template details in list, got: %s", result)
	}
}

// TestToolListTemplates_NilManager verifies graceful handling.
func TestToolListTemplates_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListTemplates()
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NotFound verifies error when template doesn't exist.
func TestToolLaunchTemplate_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "nonexistent",
	})
	if !contains(result, "获取模板失败") {
		t.Errorf("expected not found error, got: %s", result)
	}
}

// TestToolLaunchTemplate_MissingParam verifies error when template_name is missing.
func TestToolLaunchTemplate_MissingParam(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{})
	if result != "缺少 template_name 参数" {
		t.Errorf("expected missing param error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NilManager verifies graceful handling.
func TestToolLaunchTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "test",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestExecuteTool_TemplateToolsRouting verifies that executeTool routes
// template tool names to the correct handlers.
func TestExecuteTool_TemplateToolsRouting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}
	// Initialize registry so executeTool can dispatch via registry.
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	// create_template via executeTool
	result := handler.executeTool("create_template", `{"name":"t1","tool":"claude"}`, nil)
	if !contains(result, "模板已创建") {
		t.Errorf("create_template via executeTool failed: %s", result)
	}

	// list_templates via executeTool
	result = handler.executeTool("list_templates", "", nil)
	if !contains(result, "t1") {
		t.Errorf("list_templates via executeTool failed: %s", result)
	}

	// launch_template via executeTool (will fail at session creation, but routing works)
	result = handler.executeTool("launch_template", `{"template_name":"t1"}`, nil)
	// Should get past template lookup (routing works) — will fail at session creation
	if contains(result, "未知工具") || contains(result, "模板管理器未初始化") {
		t.Errorf("launch_template routing failed: %s", result)
	}
}

func TestExecuteTool_GeneratePDF_IsRegistered(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	handler := &IMMessageHandler{app: app}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	result := handler.executeTool("generate_pdf", `{"content":"# 标题\n\n正文","title":"通用标题"}`, nil)
	if contains(result, "未知工具") {
		t.Fatalf("generate_pdf should be registered as a builtin tool, got: %s", result)
	}
	// The tool is now registered; it may fail due to missing fonts in CI,
	// but it must NOT return "未知工具".
}

// Regression coverage for the uploaded-image flow through the real tool dispatcher:
// even if the model tries to call screenshot, executeTool must return the
// guard message before any screenshot/session logic runs.
func TestExecuteTool_ScreenshotBlockedForUserSuppliedImagePath(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
		lastUserText: strings.Join([]string{
			"图上有什么？",
			"",
			"[用户选择的本地文件路径]",
			`C:\Users\ma139\Pictures\Screenshots\屏幕截图 2026-03-14 073217.png`,
			"这是用户已经提供的本地图片文件。不要调用 screenshot 或重新截图；请直接使用这些路径，并优先用 read_file 或 open 查看图片内容后回答。",
		}, "\n"),
	}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	result := handler.executeTool("screenshot", "", nil)
	if !contains(result, "不要调用 screenshot") {
		t.Fatalf("expected screenshot guard via executeTool, got: %s", result)
	}
	if contains(result, "缺少 session_id") {
		t.Fatalf("expected guard to trigger before screenshot execution flow, got: %s", result)
	}
}

func TestIsPureScreenshotAction_AllowsCooldownRetries(t *testing.T) {
	if !isPureScreenshotAction(0) {
		t.Fatal("expected repeated screenshot-only tool calls to remain a pure screenshot action")
	}
	if isPureScreenshotAction(1) {
		t.Fatal("expected any non-screenshot tool call to make the action intermediate")
	}
}

func TestShouldContinueTextOutput_DoesNotContinueWeatherTilde(t *testing.T) {
	text := strings.Repeat("北京今天阴天，晚间不下雨，明天有小雨，出门带伞。", 20) + "早晚凉建议带件外套~"
	if got, reason := shouldContinueTextOutput("stop", text); got {
		t.Fatalf("shouldContinueTextOutput(stop, weather) = true (%s), want false", reason)
	}
}

func TestShouldContinueTextOutput_ExplicitLengthStillContinues(t *testing.T) {
	if got, reason := shouldContinueTextOutput("length", "短文本也要续写"); !got || reason != "finish_reason=length" {
		t.Fatalf("shouldContinueTextOutput(length) = (%v,%q), want true finish_reason=length", got, reason)
	}
}

func TestShouldContinueTextOutput_DoesNotContinueLongCompleteEmojiEnding(t *testing.T) {
	text := strings.Repeat("Complete paragraph with enough content to exceed the heuristic threshold. ", 30) + "All checks are done \U0001F60A"
	if got, reason := shouldContinueTextOutput("stop", text); got {
		t.Fatalf("shouldContinueTextOutput(stop, emoji ending) = true (%s), want false", reason)
	}
}

func TestShouldContinueTextOutput_DoesNotApplyStructuralHeuristicToNonStopReasons(t *testing.T) {
	text := strings.Repeat("Long generated report segment near the model output limit. ", 30) + "and the next item begins,"
	for _, finishReason := range []string{"content_filter", "tool_calls"} {
		if got, reason := shouldContinueTextOutput(finishReason, text); got {
			t.Fatalf("shouldContinueTextOutput(%q, structural text) = true (%s), want false", finishReason, reason)
		}
	}
}

func TestShouldContinueTextOutput_LongStructuralFragmentContinues(t *testing.T) {
	text := strings.Repeat("这是一个很长的报告段落，用来模拟模型在输出限制附近被截断。", 90) + "下一节："
	if got, reason := shouldContinueTextOutput("stop", text); !got || reason != "structural_heuristic" {
		t.Fatalf("shouldContinueTextOutput(long fragment) = (%v,%q), want true structural_heuristic", got, reason)
	}
}

func TestShouldContinueTextOutput_LongCommaFragmentContinues(t *testing.T) {
	text := strings.Repeat("Long generated report segment near the model output limit. ", 30) + "and the next item begins,"
	if got, reason := shouldContinueTextOutput("stop", text); !got || reason != "structural_heuristic" {
		t.Fatalf("shouldContinueTextOutput(long comma fragment) = (%v,%q), want true structural_heuristic", got, reason)
	}
}

func TestShouldContinueTextOutput_UnclosedCodeFenceContinues(t *testing.T) {
	text := strings.Repeat("Long generated report segment near the model output limit. ", 30) + "```go\nfunc unfinished() {\n"
	if got, reason := shouldContinueTextOutput("stop", text); !got || reason != "structural_heuristic" {
		t.Fatalf("shouldContinueTextOutput(unclosed code fence) = (%v,%q), want true structural_heuristic", got, reason)
	}
}

func TestShouldAutoClearIncompleteTaskContext_RequiresExplicitResume(t *testing.T) {
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "搜索 huggingface daily papers，生成每日论文综述，生成pdf发我"},
		{Role: "assistant", Content: "(已达到最大推理轮次，请继续发送消息以完成任务)"},
	}

	if !shouldAutoClearIncompleteTaskContext("现在帮我把桌面上的 AI 编程评测报告放入知识库", entries) {
		t.Fatal("expected fresh task request to clear incomplete task context")
	}
	if shouldAutoClearIncompleteTaskContext("继续", entries) {
		t.Fatal("expected explicit resume message to keep incomplete task context")
	}
	if shouldAutoClearIncompleteTaskContext("继续做完上次的 pdf", entries) {
		t.Fatal("expected explicit resume request to keep incomplete task context")
	}
}

func TestLooksLikeFreshTaskRequest(t *testing.T) {
	if !looksLikeFreshTaskRequest("现在帮我把桌面上的 AI 编程评测报告放入知识库") {
		t.Fatal("expected fresh task request to be detected")
	}
	if looksLikeFreshTaskRequest("继续做完上次的 pdf") {
		t.Fatal("expected explicit resume request to not be treated as fresh task")
	}
	if looksLikeFreshTaskRequest("好的") {
		t.Fatal("expected short acknowledgement to not be treated as fresh task")
	}
}

// TestSetContextResolver verifies the setter works.
func TestSetContextResolver(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	resolver := &SessionContextResolver{app: &App{}}
	handler.SetContextResolver(resolver)
	if handler.contextResolver != resolver {
		t.Error("expected contextResolver to be set")
	}
}

// TestSetSessionPrecheck verifies the setter works.
func TestSetSessionPrecheck(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	precheck := &SessionPrecheck{app: &App{}}
	handler.SetSessionPrecheck(precheck)
	if handler.sessionPrecheck != precheck {
		t.Error("expected sessionPrecheck to be set")
	}
}

// TestSetStartupFeedback verifies the setter works.
func TestSetStartupFeedback(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Can't easily create a real SessionStartupFeedback without a manager,
	// but we can verify the field is set.
	feedback := &SessionStartupFeedback{}
	handler.SetStartupFeedback(feedback)
	if handler.startupFeedback != feedback {
		t.Error("expected startupFeedback to be set")
	}
}

// TestBuildToolDefinitions_IncludesTemplateTools verifies that the tool
// definitions include the template tools added in task 7.1.
func TestBuildToolDefinitions_IncludesTemplateTools(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	templateTools := map[string]bool{
		"create_template": false,
		"list_templates":  false,
		"launch_template": false,
	}

	for _, tool := range tools {
		name := extractToolName(tool)
		if _, ok := templateTools[name]; ok {
			templateTools[name] = true
		}
	}

	for name, found := range templateTools {
		if !found {
			t.Errorf("expected template tool %q in buildToolDefinitions", name)
		}
	}
}

func TestBuildToolDefinitions_IncludesMISData(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	for _, tool := range tools {
		if extractToolName(tool) == "mis_data" {
			return
		}
	}
	t.Fatal("mis_data tool not found in buildToolDefinitions")
}

// contains is a test helper that checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests for Task 5: create_session provider parameter
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_CreateSessionHasProviderParam verifies that legacy
// external coding-session tools no longer appear in the hardcoded tool catalog.
func TestBuildToolDefinitions_CreateSessionHasProviderParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		assertToolDefinitionAbsent(t, tools, name)
	}
}

func assertToolDefinitionAbsent(t *testing.T, tools []map[string]interface{}, name string) {
	t.Helper()
	for _, tool := range tools {
		if extractToolName(tool) == name {
			t.Fatalf("%s should not be exposed in buildToolDefinitions", name)
		}
	}
}

// TestToolCreateSession_NoProviderBehaviorUnchanged verifies that not passing
// provider keeps the original behavior (tool param required, no provider passed).
func TestToolCreateSession_NoProviderBehaviorUnchanged(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "请修改代码并创建会话"}
	handler.unifiedClassifier = testCodingIntentClassifier()

	// Without tool param, should return missing tool error.
	result := handler.toolCreateSession(map[string]interface{}{})
	if result != "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("expected missing tool error, got: %s", result)
	}

	// With tool but no provider, should attempt session creation (will fail
	// because app is minimal, but should NOT mention provider issues).
	result = handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	if result == "缺少 tool 参数" || result == "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("should not report missing tool when tool is provided, got: %s", result)
	}
	// Error should be about session creation, not about provider resolution.
	if contains(result, "至少一个有效的服务商") || contains(result, "未配置 API Key") || contains(result, "不存在") {
		t.Errorf("should not fail at provider resolution when provider is omitted, got: %s", result)
	}
}

// TestToolCreateSession_WithProviderPassedThrough verifies that the provider
// parameter is extracted and resolved via ProviderResolver. When the specified
// provider doesn't exist, the resolver returns an error before reaching session creation.
func TestToolCreateSession_WithProviderPassedThrough(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "请修改代码并创建会话"}
	handler.unifiedClassifier = testCodingIntentClassifier()

	result := handler.toolCreateSession(map[string]interface{}{
		"tool":     "claude",
		"provider": "NonExistentProvider",
	})
	// ProviderResolver should catch the invalid provider before session creation.
	if !contains(result, "不存在") {
		t.Errorf("expected provider not found error, got: %s", result)
	}
}

// TestBuildToolDefinitions_CreateSessionHasResumeSessionIDParam pins removal of
// create_session from the legacy tool catalog.
func TestBuildToolDefinitions_CreateSessionHasResumeSessionIDParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	assertToolDefinitionAbsent(t, tools, "create_session")
}

// TestToolCreateSession_ProviderDescriptionInToolDef verifies the create_session
// description mentions provider selection and resume support.
func TestToolCreateSession_ProviderDescriptionInToolDef(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			fn, _ := tool["function"].(map[string]interface{})
			desc, _ := fn["description"].(string)
			if !contains(desc, "provider") {
				t.Errorf("create_session description should mention provider, got: %s", desc)
			}
			if !contains(desc, "resume_session_id") && !contains(desc, "恢复") {
				t.Errorf("create_session description should mention resume support, got: %s", desc)
			}
			if !contains(desc, "ssh") {
				t.Errorf("create_session description should redirect ssh/server tasks, got: %s", desc)
			}
			return
		}
	}
	t.Fatal("create_session tool not found")
}

func TestBuildToolDefinitions_CreateSessionDescriptionMentionsSSH(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	for _, tool := range tools {
		if extractToolName(tool) != "create_session" {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		if !contains(desc, "SSH") && !contains(desc, "ssh") {
			t.Fatalf("expected create_session description to mention ssh redirect, got: %s", desc)
		}
		if !contains(desc, "resume_session_id") && !contains(desc, "恢复") {
			t.Fatalf("expected create_session description to mention resume support, got: %s", desc)
		}
		return
	}
	t.Fatal("create_session tool not found")
}

// ---------------------------------------------------------------------------
// Tests for Task 6: list_providers Agent tool
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_IncludesListProviders verifies that the tool
// definitions include the list_providers tool.
func TestBuildToolDefinitions_IncludesListProviders(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var found bool
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "list_providers" {
			found = true
			fn, _ := tool["function"].(map[string]interface{})
			params, _ := fn["parameters"].(map[string]interface{})
			props, _ := params["properties"].(map[string]interface{})
			if _, ok := props["tool"]; !ok {
				t.Error("list_providers missing 'tool' parameter")
			}
			required, _ := params["required"].([]string)
			hasToolRequired := false
			for _, r := range required {
				if r == "tool" {
					hasToolRequired = true
				}
			}
			if !hasToolRequired {
				t.Error("list_providers should have 'tool' in required list")
			}
			break
		}
	}
	if !found {
		t.Fatal("list_providers tool not found in buildToolDefinitions")
	}
}

func TestBuildToolDefinitions_IncludesSSH(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	for _, tool := range tools {
		if extractToolName(tool) != "ssh" {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		if !contains(desc, "服务器") || !contains(desc, "exec_background") {
			t.Fatalf("unexpected ssh description: %s", desc)
		}
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		if _, ok := props["action"]; !ok {
			t.Fatal("ssh tool missing action parameter")
		}
		if _, ok := props["timeout"]; !ok {
			t.Fatal("ssh tool missing wait_task timeout parameter")
		}
		if !strings.Contains(fmt.Sprint(props["action"]), "wait_task") {
			t.Fatalf("ssh action description missing wait_task: %#v", props["action"])
		}
		return
	}
	t.Fatal("ssh tool not found in buildToolDefinitions")
}

// TestExecuteTool_ListProvidersRouting verifies that executeTool routes
// list_providers to the correct handler.
func TestExecuteTool_ListProvidersRouting(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Initialize registry so executeTool can dispatch via registry.
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	result := handler.executeTool("list_providers", `{"tool":"claude"}`, nil)
	// With a minimal App (no config file), it should attempt to load config
	// and either return a config error or tool-related result, not "未知工具".
	if contains(result, "未知工具") {
		t.Errorf("list_providers should be routed, got: %s", result)
	}
}

// TestToolListProviders_MissingToolParam verifies that missing tool param
// returns an appropriate error.
func TestToolListProviders_MissingToolParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListProviders(map[string]interface{}{})
	if result != "缺少 tool 参数" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolListProviders_EmptyToolParam verifies that empty tool param
// returns an appropriate error.
func TestToolListProviders_EmptyToolParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListProviders(map[string]interface{}{"tool": ""})
	if result != "缺少 tool 参数" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolListProviders_UnsupportedTool verifies that an unsupported tool
// returns an appropriate error.
func TestToolListProviders_UnsupportedTool(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "nonexistent_tool"})
	if !contains(result, "不支持的工具") {
		t.Errorf("expected unsupported tool error, got: %s", result)
	}
}

// TestToolListProviders_NoValidProviders verifies that when all providers
// have no API key (and none is "Original"), the tool returns a helpful message.
// Note: LoadConfig always ensures "Original" is present, so we write the
// config JSON directly to bypass the ensureOriginal logic.
func TestToolListProviders_NoValidProviders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	// Write config JSON directly to bypass LoadConfig's ensureOriginal.
	configJSON := `{
		"claude": {
			"current_model": "EmptyProvider",
			"models": [
				{"model_name": "EmptyProvider", "model_id": "ep-1", "api_key": ""},
				{"model_name": "AlsoEmpty", "model_id": "ae-1", "api_key": "   "}
			]
		}
	}`
	configPath := filepath.Join(tempHome, ".maclaw", "config.json")
	if err := os.MkdirAll(filepath.Join(tempHome, ".maclaw"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})
	// LoadConfig will add "Original" back, so Original will be valid.
	// The test verifies the handler works correctly with the loaded config.
	// Since Original is always added, we verify it appears in the output.
	if contains(result, "没有可用的服务商") {
		// If somehow no valid providers (shouldn't happen with ensureOriginal),
		// that's also acceptable behavior.
		return
	}
	// Original should be present (added by LoadConfig).
	if !contains(result, "Original") {
		t.Errorf("expected Original in result (added by LoadConfig), got: %s", result)
	}
	// EmptyProvider should NOT be present (no API key, not Original).
	if contains(result, "EmptyProvider") {
		t.Errorf("should not contain EmptyProvider (invalid provider), got: %s", result)
	}
}

// TestToolListProviders_WithValidProviders verifies that valid providers
// are listed with correct formatting.
func TestToolListProviders_WithValidProviders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "deepseek-chat", ApiKey: "sk-test-key"},
			{ModelName: "EmptyKey", ModelId: "empty-id", ApiKey: ""},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})

	// Should contain header.
	if !contains(result, "工具 claude 的可用服务商") {
		t.Errorf("expected header in result, got: %s", result)
	}
	// Should contain Original (valid because name is "Original").
	if !contains(result, "Original") {
		t.Errorf("expected Original in result, got: %s", result)
	}
	// Should contain DeepSeek (valid because has API key).
	if !contains(result, "DeepSeek") {
		t.Errorf("expected DeepSeek in result, got: %s", result)
	}
	// Should NOT contain EmptyKey (invalid: not Original and no API key).
	if contains(result, "EmptyKey") {
		t.Errorf("should not contain EmptyKey (invalid provider), got: %s", result)
	}
	// Original should be marked as default.
	if !contains(result, "[当前默认]") {
		t.Errorf("expected [当前默认] marker for Original, got: %s", result)
	}
}

// TestToolListProviders_ModelIdTruncation verifies that long model_id values
// are truncated to 20 characters.
func TestToolListProviders_ModelIdTruncation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "LongID",
		Models: []corelib.ModelConfig{
			{ModelName: "LongID", ModelId: "this-is-a-very-long-model-id-that-exceeds-twenty-chars", ApiKey: "key123"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolListProviders(map[string]interface{}{"tool": "claude"})

	// The full model ID should NOT appear.
	if contains(result, "this-is-a-very-long-model-id-that-exceeds-twenty-chars") {
		t.Errorf("long model_id should be truncated, got: %s", result)
	}
	// The truncated version should appear.
	if !contains(result, "this-is-a-very-long-") {
		t.Errorf("expected truncated model_id prefix, got: %s", result)
	}
	if !contains(result, "...") {
		t.Errorf("expected '...' after truncated model_id, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 2: ProviderResolver integration in toolCreateSession
// ---------------------------------------------------------------------------

// TestToolCreateSession_NoProviderUsesDefault verifies that when no provider
// is specified, the ProviderResolver uses the default provider from corelib.ToolConfig.
func TestToolCreateSession_NoProviderUsesDefault(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// Set up claude with a valid default provider (Original is always valid).
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	// Will fail at StartRemoteSessionForProject (remote not enabled), but
	// should NOT fail at provider resolution.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Should NOT contain provider resolution errors.
	if contains(result, "无法创建会话") && contains(result, "服务商") {
		t.Errorf("should not fail at provider resolution, got: %s", result)
	}
	// Should fail at session creation (remote disabled), not at provider resolution.
	if contains(result, "加载配置失败") || contains(result, "获取工具配置失败") {
		t.Errorf("should not fail at config loading, got: %s", result)
	}
}

// TestToolCreateSession_DefaultUnavailableFallbackHint verifies that when the
// default provider is unavailable, the resolver falls back to the next available
// provider. Since the test environment has remote mode disabled, the session
// creation will fail, but the provider resolution should succeed with fallback.
func TestToolCreateSession_DefaultUnavailableFallbackHint(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// Set default to a provider with no API key (not "Original").
	// LoadConfig ensures "Original" is always present and valid.
	// So fallback chain: BadDefault (invalid) → Original (valid, fallback target).
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "BadDefault",
		Models: []corelib.ModelConfig{
			{ModelName: "BadDefault", ModelId: "bad-id", ApiKey: ""},
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Provider resolution should succeed (fallback), so the error
	// should be about session creation, NOT about provider resolution.
	if contains(result, "无法创建会话") {
		t.Errorf("provider resolution should succeed via fallback, got: %s", result)
	}

	// Verify the ProviderResolver directly to confirm fallback behavior.
	cfg2, _ := app.LoadConfig()
	toolCfg, _ := remoteToolConfig(cfg2, "claude")
	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, "")
	if resolveErr != nil {
		t.Fatalf("resolver should succeed with fallback, got error: %v", resolveErr)
	}
	if !resolveResult.Fallback {
		t.Error("expected Fallback=true when default provider is unavailable")
	}
	if resolveResult.OriginalName != "BadDefault" {
		t.Errorf("expected OriginalName=BadDefault, got %s", resolveResult.OriginalName)
	}
	// Fallback should go to Original (first valid after BadDefault).
	if resolveResult.Provider.ModelName != "Original" {
		t.Errorf("expected fallback to Original, got %s", resolveResult.Provider.ModelName)
	}
}

// TestToolCreateSession_UserSpecifiedProviderUsed verifies that when the user
// specifies a valid provider, it is used directly without fallback.
func TestToolCreateSession_UserSpecifiedProviderUsed(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":     "claude",
		"provider": "DeepSeek",
	})
	// Should NOT contain fallback hint.
	if contains(result, "服务商已降级") {
		t.Errorf("should not have fallback hint when provider is explicitly specified, got: %s", result)
	}
	// Should NOT fail at provider resolution.
	if contains(result, "不存在") || contains(result, "未配置 API Key") {
		t.Errorf("should not fail at provider resolution for valid provider, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 3: create_session project_id parameter
// ---------------------------------------------------------------------------

// TestBuildToolDefinitions_CreateSessionHasProjectIDParam pins removal of
// create_session from the legacy tool catalog.
func TestBuildToolDefinitions_CreateSessionHasProjectIDParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()
	assertToolDefinitionAbsent(t, tools, "create_session")
}

// TestToolCreateSession_ProjectIDResolvesSuccessfully verifies that when
// project_id matches a configured project, its path is used.
func TestToolCreateSession_ProjectIDResolvesSuccessfully(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/tmp/other-project"},
	}
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":       "claude",
		"project_id": "proj-1",
	})
	// Should resolve project_id to /tmp/my-project.
	// Session creation will fail (remote mode disabled), but project resolution
	// should succeed — the error should NOT be about project_id not found.
	if contains(result, "未找到") {
		t.Errorf("project_id should resolve successfully, got: %s", result)
	}
}

// TestToolCreateSession_ProjectIDNotFound verifies that when project_id
// doesn't match any configured project, an error with available projects is returned.
func TestToolCreateSession_ProjectIDNotFound(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/tmp/other-project"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":       "claude",
		"project_id": "nonexistent-id",
	})
	// Should return error with available project list.
	if !contains(result, "未找到") {
		t.Errorf("expected not found error, got: %s", result)
	}
	if !contains(result, "proj-1") || !contains(result, "proj-2") {
		t.Errorf("expected available project IDs in error, got: %s", result)
	}
}

// TestToolCreateSession_ProjectIDPriorityOverProjectPath verifies that
// project_id takes priority over project_path when both are provided.
func TestToolCreateSession_ProjectIDPriorityOverProjectPath(t *testing.T) {
	t.Skip("legacy external create_session is disabled; covered by TestToolCreateSessionDisabled")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/tmp/my-project"},
	}
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", ModelId: "orig-id", ApiKey: "", IsBuiltin: true},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app, lastUserText: "fix code and create a coding session"}
	handler.unifiedClassifier = testCodingIntentClassifier()
	// Provide both project_id and project_path — project_id should win.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool":         "claude",
		"project_id":   "proj-1",
		"project_path": "/tmp/should-be-ignored",
	})
	// project_id should take priority — should NOT report project_id not found.
	if contains(result, "未找到") {
		t.Errorf("project_id should resolve successfully, got: %s", result)
	}
	// The final output should reference the project_id path (/tmp/my-project),
	// not the project_path (/tmp/should-be-ignored).
	// Session creation will fail (remote mode disabled), but the error should
	// NOT contain the ignored project_path.
	if contains(result, "/tmp/should-be-ignored") {
		t.Errorf("project_path should be overridden by project_id, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Tests for project_manage tool
// ---------------------------------------------------------------------------

// TestToolProjectManage_List verifies that project_manage with action=list
// returns project data when projects exist.
func TestToolProjectManage_List(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{
		{Id: "proj-1", Name: "MyProject", Path: "/path/to/project"},
		{Id: "proj-2", Name: "OtherProject", Path: "/path/to/other"},
	}
	cfg.CurrentProject = "proj-1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolProjectManage(map[string]interface{}{"action": "list"})

	// Should contain both projects.
	if !contains(result, "proj-1") || !contains(result, "MyProject") || !contains(result, "/path/to/project") {
		t.Errorf("expected proj-1 details in result, got: %s", result)
	}
	if !contains(result, "proj-2") || !contains(result, "OtherProject") || !contains(result, "/path/to/other") {
		t.Errorf("expected proj-2 details in result, got: %s", result)
	}
}

// TestToolProjectManage_ListEmpty verifies that when no projects are configured,
// project_manage with action=list returns a hint message.
func TestToolProjectManage_ListEmpty(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = nil
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	result := handler.toolProjectManage(map[string]interface{}{"action": "list"})

	if result != "当前没有已配置的项目。请在桌面端添加项目。" {
		t.Errorf("expected no projects hint, got: %s", result)
	}
}

// TestBuildToolDefinitions_IncludesProjectManage verifies that the tool
// definitions include the project_manage tool.
func TestBuildToolDefinitions_IncludesProjectManage(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var found bool
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "project_manage" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("project_manage tool not found in buildToolDefinitions")
	}
}

func TestBuildToolDefinitions_IncludesEditFile(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var found bool
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "edit_file" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("edit_file tool not found in buildToolDefinitions")
	}
}

// TestExecuteTool_ProjectManageRouting verifies that executeTool routes
// project_manage to the correct handler.
func TestExecuteTool_ProjectManageRouting(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome, sessionStarter: &CodingSessionStarter{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Projects = nil
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	handler := &IMMessageHandler{app: app}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)

	result := handler.executeTool("project_manage", `{"action":"list"}`, nil)
	// Should NOT return "未知工具".
	if contains(result, "未知工具") {
		t.Errorf("project_manage should be routed, got: %s", result)
	}
	// With no projects, should return hint.
	if !contains(result, "当前没有已配置的项目") {
		t.Errorf("expected no projects hint, got: %s", result)
	}
}

func TestRouteTools_SSHIntentKeepsBashButHidesMCPGateway(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	for i := 0; i < 20; i++ {
		handler.registry.Register(RegisteredTool{
			Name:        fmt.Sprintf("extra_ssh_bash_%d", i),
			Description: fmt.Sprintf("extra tool %d", i),
			Category:    ToolCategoryNonCode,
			Status:      RegToolAvailable,
		})
	}
	tools := NewDynamicToolBuilder(handler.registry).BuildAll()
	handler.unifiedClassifier = testIntentClassifier("ssh")
	router := NewToolRouter(NewToolDefinitionGenerator(nil, tools))
	handler.SetToolRouter(router)

	routed := handler.routeTools("configure nginx on production server", tools)
	foundBash := false
	for _, tool := range routed {
		name := extractToolName(tool)
		if name == "call_mcp_tool" {
			t.Fatalf("call_mcp_tool should be hidden when ssh intent is routed; got tools=%v", routed)
		}
		if name == "bash" {
			foundBash = true
		}
	}
	if !foundBash {
		t.Fatalf("bash should remain available for local helper work; got tools=%v", routed)
	}
}

func TestToolBashRejectsRawSSHCommand(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolBash(map[string]interface{}{"command": "ssh root@example.com uptime"}, nil)
	if !strings.Contains(result, "Raw ssh/scp/sftp") || !strings.Contains(result, "builtin ssh tool") {
		t.Fatalf("expected raw ssh command rejection, got: %s", result)
	}
}

func TestToolBashRejectsBroadBrowserKillCommand(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolBash(map[string]interface{}{"command": "taskkill /f /im chrome.exe"}, nil)
	if !strings.Contains(result, "Broad Chrome/Edge process kill") || !strings.Contains(result, "persistent browser process and login/cookies are preserved") {
		t.Fatalf("expected browser kill rejection, got: %s", result)
	}
}

func TestToolBashRejectsShellBrowserAutomationCommand(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolBash(map[string]interface{}{"command": `python -c "from playwright.sync_api import sync_playwright; sync_playwright().start().chromium.connect_over_cdp('http://127.0.0.1:3888')"`}, nil)
	if !strings.Contains(result, "Shell Playwright/Puppeteer/Selenium/CDP/screenshot browser automation is disabled") || !strings.Contains(result, "stable browser tool/session mechanism") {
		t.Fatalf("expected shell browser automation rejection, got: %s", result)
	}
}

func TestToolBashRejectsAuthenticatedBrowserSideEffectHTTPCommand(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolBash(map[string]interface{}{"command": `curl -X POST https://www.zhihu.com/api/v4/pins -H "x-csrftoken: token" --data-raw "{}"`}, nil)
	if !strings.Contains(result, "Direct authenticated browser-side HTTP side effects") || !strings.Contains(result, "Use the browser tool") {
		t.Fatalf("expected browser side-effect HTTP rejection, got: %s", result)
	}
}
