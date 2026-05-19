package main

import (
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
	if got := resolveBashTimeout(map[string]interface{}{"timeout": float64(45)}, "python render_pdf.py --input review.md"); got != 45 {
		t.Fatalf("resolveBashTimeout(explicit) = %d, want 45", got)
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
// the hardcoded buildToolDefinitions() output when no generator is set.
func TestGetTools_FallbackWithoutGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.getTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty builtin tools")
	}

	// Verify first tool is list_sessions.
	name := extractToolName(tools[0])
	if name != "list_sessions" {
		t.Errorf("expected first tool to be list_sessions, got %s", name)
	}
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
	// With nil registry, generator returns only builtins.
	if len(tools) != len(builtins) {
		t.Fatalf("expected %d tools from generator (nil registry), got %d", len(builtins), len(tools))
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

// TestRouteTools_NoRouterFailsClosed verifies that routeTools still applies
// conservative conditional-tool filtering when no router is configured.
func TestRouteTools_NoRouterFailsClosed(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.buildToolDefinitions()
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
		for _, tool := range routed {
			if extractToolName(tool) == "ssh" {
				foundSSH = true
				break
			}
		}
		if foundSSH != wantSSH {
			names := make([]string, len(routed))
			for i, tool := range routed {
				names[i] = extractToolName(tool)
			}
			t.Fatalf("ssh presence for %q = %v, want %v; got: %v", message, foundSSH, wantSSH, names)
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
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "ssh to 10.0.0.8 and inspect nginx logs"}
	handler.unifiedClassifier = testIntentClassifier("ssh")
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "SSH/server operation") || !contains(result, "Use the ssh tool") {
		t.Fatalf("expected ssh redirect hint, got: %s", result)
	}
}

func TestToolCreateSession_NonCodingIntentBlocked(t *testing.T) {
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "translate this paper"}
	handler.unifiedClassifier = testIntentClassifier("non_coding")
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "not a coding task") || !contains(result, "read_file / write_file / edit_file") {
		t.Fatalf("expected non-coding guard hint, got: %s", result)
	}
}

func TestToolCreateSession_AmbiguousIntentBlocked(t *testing.T) {
	handler := &IMMessageHandler{app: &App{sessionStarter: &CodingSessionStarter{}}, lastUserText: "help me handle the production issue"}
	result := handler.toolCreateSession(map[string]interface{}{"tool": "claude"})
	if !contains(result, "ambiguous") || !contains(result, "Do not create a coding session yet") {
		t.Fatalf("expected ambiguous guard hint, got: %s", result)
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

// TestBuildToolDefinitions_CreateSessionHasProviderParam verifies that the
// create_session tool definition includes the provider parameter.
func TestBuildToolDefinitions_CreateSessionHasProviderParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var createSessionDef map[string]interface{}
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			createSessionDef = tool
			break
		}
	}
	if createSessionDef == nil {
		t.Fatal("create_session tool not found in buildToolDefinitions")
	}

	// Extract the function.parameters.properties to check for "provider".
	fn, _ := createSessionDef["function"].(map[string]interface{})
	if fn == nil {
		t.Fatal("create_session missing function field")
	}
	params, _ := fn["parameters"].(map[string]interface{})
	if params == nil {
		t.Fatal("create_session missing parameters field")
	}
	props, _ := params["properties"].(map[string]interface{})
	if props == nil {
		t.Fatal("create_session missing properties field")
	}
	if _, ok := props["provider"]; !ok {
		t.Error("create_session tool definition missing 'provider' parameter")
	}

	// Verify provider is NOT in required list (it's optional).
	required, _ := params["required"].([]string)
	for _, r := range required {
		if r == "provider" {
			t.Error("provider should not be in required list")
		}
	}
}

// TestToolCreateSession_NoProviderBehaviorUnchanged verifies that not passing
// provider keeps the original behavior (tool param required, no provider passed).
func TestToolCreateSession_NoProviderBehaviorUnchanged(t *testing.T) {
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

// TestBuildToolDefinitions_CreateSessionHasResumeSessionIDParam verifies that the
// create_session tool definition includes the resume_session_id parameter.
func TestBuildToolDefinitions_CreateSessionHasResumeSessionIDParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var createSessionDef map[string]interface{}
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			createSessionDef = tool
			break
		}
	}
	if createSessionDef == nil {
		t.Fatal("create_session tool not found in buildToolDefinitions")
	}

	fn, _ := createSessionDef["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["resume_session_id"]; !ok {
		t.Error("create_session tool definition missing 'resume_session_id' parameter")
	}
	required, _ := params["required"].([]string)
	for _, r := range required {
		if r == "resume_session_id" {
			t.Error("resume_session_id should not be in required list")
		}
	}
}

// TestToolCreateSession_ProviderDescriptionInToolDef verifies the create_session
// description mentions provider selection and resume support.
func TestToolCreateSession_ProviderDescriptionInToolDef(t *testing.T) {
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

// TestBuildToolDefinitions_CreateSessionHasProjectIDParam verifies that the
// create_session tool definition includes the project_id parameter.
func TestBuildToolDefinitions_CreateSessionHasProjectIDParam(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	var createSessionDef map[string]interface{}
	for _, tool := range tools {
		name := extractToolName(tool)
		if name == "create_session" {
			createSessionDef = tool
			break
		}
	}
	if createSessionDef == nil {
		t.Fatal("create_session tool not found in buildToolDefinitions")
	}

	fn, _ := createSessionDef["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["project_id"]; !ok {
		t.Error("create_session tool definition missing 'project_id' parameter")
	}
}

// TestToolCreateSession_ProjectIDResolvesSuccessfully verifies that when
// project_id matches a configured project, its path is used.
func TestToolCreateSession_ProjectIDResolvesSuccessfully(t *testing.T) {
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
