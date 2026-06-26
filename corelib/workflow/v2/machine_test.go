package v2

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func setupTestMachine() *StateMachine {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	m := NewStateMachine(store, templates)
	// Use keyword-based classifier for tests (simulates LLM always available)
	m.SetConfirmClassifier(func(phaseContext, userText string) string {
		return ClassifyConfirmIntentKeyword(userText)
	})
	return m
}

func TestCreate(t *testing.T) {
	m := setupTestMachine()
	state, err := m.Create("user1", "coding", "d:\\game2", "开发贪吃蛇")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if state.UserID != "user1" {
		t.Fatalf("UserID = %q", state.UserID)
	}
	if state.ProjectPath != "d:\\game2" {
		t.Fatalf("ProjectPath = %q", state.ProjectPath)
	}
	if state.Type != "coding" {
		t.Fatalf("Type = %q", state.Type)
	}
	if state.Status != StatusActive {
		t.Fatalf("Status = %q", state.Status)
	}
	phase := state.ActivePhase()
	if phase == nil || phase.ID != "requirements" {
		t.Fatalf("ActivePhase = %v", phase)
	}
	if phase.Status != PhaseRunning {
		t.Fatalf("phase.Status = %q", phase.Status)
	}
}

func TestCreate_RejectsTempPath(t *testing.T) {
	m := setupTestMachine()
	_, err := m.Create("user1", "coding", "C:\\Users\\ma139\\AppData\\Local\\Temp\\TestSomething123\\current-project", "test")
	if err == nil {
		t.Fatal("expected error for temp path")
	}
}

func TestCreate_RejectsEmptyPath(t *testing.T) {
	m := setupTestMachine()
	_, err := m.Create("user1", "coding", "", "test")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSubmitForm_ValidatesGaokaoProvinceSelectOptions(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("user1", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	validForm := map[string]interface{}{
		"province":             "山东",
		"exam_year":            "2026",
		"subject_type":         "物化生",
		"gender":               "女",
		"rank":                 float64(32000),
		"accept_joint_program": "可作为备选",
	}
	if err := m.SubmitForm("user1", validForm); err != nil {
		t.Fatalf("SubmitForm valid province failed: %v", err)
	}
	state := m.GetActive("user1")
	if got := state.ActivePhase().FormData["province"]; got != "山东" {
		t.Fatalf("province FormData = %v, want 山东", got)
	}

	if _, err := m.Create("user2", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	invalidForm := map[string]interface{}{
		"province":             "山东省",
		"exam_year":            "2026",
		"subject_type":         "物化生",
		"gender":               "女",
		"rank":                 float64(32000),
		"accept_joint_program": "可作为备选",
	}
	err := m.SubmitForm("user2", invalidForm)
	if err == nil {
		t.Fatal("expected invalid province to be rejected")
	}
	if !strings.Contains(err.Error(), "地区/省份") || !strings.Contains(err.Error(), "不在可选范围内") {
		t.Fatalf("error = %v", err)
	}
	state = m.GetActive("user2")
	if state.ActivePhase().FormData != nil {
		t.Fatalf("invalid form should not be saved: %#v", state.ActivePhase().FormData)
	}
}

func TestRecordOutput_TransitionsToWaitingConfirm(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")

	err := m.RecordOutput("user1", "# 需求文档\n\n功能需求...")
	if err != nil {
		t.Fatalf("RecordOutput failed: %v", err)
	}

	state := m.GetActive("user1")
	if state.ActivePhase().Status != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm", state.ActivePhase().Status)
	}
	if state.ActivePhase().Output == "" {
		t.Fatal("output not saved")
	}
}

func TestRecordOutput_RejectsIncompleteGaokaoFinalPlan(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("user1", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := m.GetActive("user1")
	state.CurrentPhase = 3
	for i := range state.Phases {
		state.Phases[i].Status = PhaseCompleted
	}
	state.Phases[3].Status = PhaseRunning
	if err := m.store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	err := m.RecordOutput("user1", "让我读取 evidence_doc.txt，然后运行 check_db2.py 看看数据库结构。")
	if err == nil {
		t.Fatal("expected incomplete final plan to be rejected")
	}
	if !strings.Contains(err.Error(), "gaokao final plan output is incomplete") {
		t.Fatalf("error = %v", err)
	}

	state = m.GetActive("user1")
	if state == nil {
		t.Fatal("workflow should remain active")
	}
	if state.CurrentPhase != 3 {
		t.Fatalf("CurrentPhase = %d, want 3", state.CurrentPhase)
	}
	if got := state.ActivePhase().Status; got != PhaseRunning {
		t.Fatalf("phase status = %q, want running", got)
	}
	if state.ActivePhase().Output != "" {
		t.Fatalf("output should not be saved: %q", state.ActivePhase().Output)
	}
}

func TestRecordOutput_AcceptsCompleteGaokaoFinalPlan(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("user1", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := m.GetActive("user1")
	state.CurrentPhase = 3
	for i := range state.Phases {
		state.Phases[i].Status = PhaseCompleted
	}
	state.Phases[3].Status = PhaseRunning
	if err := m.store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	output := completeGaokaoFinalPlanOutputForTest()

	if err := m.RecordOutput("user1", output); err != nil {
		t.Fatalf("RecordOutput failed: %v", err)
	}
	state = m.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm", got)
	}
}

func TestRecordOutput_RejectsGaokaoFinalPlanWithoutVerifiedSourceURLs(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("user1", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := m.GetActive("user1")
	state.CurrentPhase = 3
	for i := range state.Phases {
		state.Phases[i].Status = PhaseCompleted
	}
	state.Phases[3].Status = PhaseRunning
	if err := m.store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	output := strings.Replace(completeGaokaoFinalPlanOutputForTest(), "https://example.edu/admission", "来源URL", 1)
	err := m.RecordOutput("user1", output)
	if err == nil {
		t.Fatal("expected final plan without source URLs to be rejected")
	}
	if !strings.Contains(err.Error(), "missing verified source URLs") {
		t.Fatalf("error = %v", err)
	}

	state = m.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseRunning {
		t.Fatalf("phase status = %q, want running", got)
	}
	if got := state.ActivePhase().Output; got != "" {
		t.Fatalf("output should not be saved: %q", got)
	}
}

func TestRecordOutput_RejectsGaokaoFinalPlanWithoutRecommendationRows(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("user1", string(WorkflowGaokaoApplication), "d:\\project", "高考志愿"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := m.GetActive("user1")
	state.CurrentPhase = 3
	for i := range state.Phases {
		state.Phases[i].Status = PhaseCompleted
	}
	state.Phases[3].Status = PhaseRunning
	if err := m.store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	output := `# 填报参考资料与建议

## 总排清单
学校、专业、办学地点、类型、往年最低位次、推荐理由、数据来源均需核验，参考 https://example.edu/admission。

## 冲
学校、专业、办学地点、类型、最低位次、推荐理由、依据来源。

## 稳
学校、专业、办学地点、类型、最低位次、推荐理由、依据来源。

## 保
学校、专业、办学地点、类型、最低位次、推荐理由、依据来源。`
	err := m.RecordOutput("user1", output)
	if err == nil {
		t.Fatal("expected final plan without recommendation rows to be rejected")
	}
	if !strings.Contains(err.Error(), "missing recommendation rows") {
		t.Fatalf("error = %v", err)
	}

	state = m.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseRunning {
		t.Fatalf("phase status = %q, want running", got)
	}
	if got := state.ActivePhase().Output; got != "" {
		t.Fatalf("output should not be saved: %q", got)
	}
}

func TestRecordOutput_SanitizesGLMContentToolCallProtocol(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")

	raw := "我来生成文档。\n<details><summary>思考过程</summary>hidden</details>\n<tool_call[]>\n" +
		`{"name":"write_file","arguments":{"file_path":"d:\\project\\docs\\requirements.md","content":"# 需求文档\n\n## 功能需求\n- 登录"}}`
	if err := m.RecordOutput("user1", raw); err != nil {
		t.Fatalf("RecordOutput failed: %v", err)
	}

	state := m.GetActive("user1")
	got := state.ActivePhase().Output
	if got != "# 需求文档\n\n## 功能需求\n- 登录" {
		t.Fatalf("output = %q", got)
	}
	if strings.Contains(got, "<tool_call") || strings.Contains(got, "<details") || strings.Contains(got, "hidden") || strings.Contains(got, "write_file") {
		t.Fatalf("protocol leaked into output: %q", got)
	}
}

func TestRecordOutput_StripsGLMProtocolWhenToolContentNotUsable(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")

	raw := "可见内容\n<details><summary>思考过程</summary>hidden</details>\n<tool_call[]>\n" +
		`{"name":"unknown","arguments":{"content":"ignored"}}`
	if err := m.RecordOutput("user1", raw); err != nil {
		t.Fatalf("RecordOutput failed: %v", err)
	}

	got := m.GetActive("user1").ActivePhase().Output
	if got != "可见内容" {
		t.Fatalf("output = %q, want visible content only", got)
	}
}

func TestSanitizePhaseOutput_ExtractsDocContentForPhaseAliases(t *testing.T) {
	raw := "writing\n<tool_call[]>\n" +
		`{"name":"write_file","arguments":{"file_path":"d:\\project\\docs\\task-breakdown.md","content":"# Task Breakdown\n\n- T1"}}`

	got := SanitizePhaseOutput("task_breakdown", raw)
	if got != "# Task Breakdown\n\n- T1" {
		t.Fatalf("SanitizePhaseOutput() = %q", got)
	}
}

func TestSanitizePhaseOutputFromToolCalls_ExtractsWriteFileContent(t *testing.T) {
	got := SanitizePhaseOutputFromToolCalls("Task_Breakdown", []llm.ToolCall{{
		Function: llm.ToolCallFunction{
			Name:      "Write",
			Arguments: `{"file_path":"d:\\project\\docs\\tasks.md","content":"# Tasks\n\n- T1"}`,
		},
	}})
	if got != "# Tasks\n\n- T1" {
		t.Fatalf("SanitizePhaseOutputFromToolCalls() = %q", got)
	}
}

func TestSanitizePhaseOutputFromToolCalls_ExtractsBroadDocPhasesButNotExecutionPhases(t *testing.T) {
	calls := []llm.ToolCall{{
		Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"file_path":"d:\\project\\docs\\trend.md","content":"# Trend\n\n- item"}`,
		},
	}}

	if got := SanitizePhaseOutputFromToolCalls("trend_analysis", calls); got != "# Trend\n\n- item" {
		t.Fatalf("trend_analysis output = %q", got)
	}
	if got := SanitizePhaseOutputFromToolCalls("implementation", calls); got != "" {
		t.Fatalf("implementation output = %q, want empty", got)
	}
}

func TestHandleInput_ConfirmAdvances(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# Requirements doc")

	result, err := m.HandleInput("user1", "确认")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if result.Action != ActionRunPhase {
		t.Fatalf("action = %q, want run_phase", result.Action)
	}
	if result.Phase == nil || result.Phase.ID != "design" {
		t.Fatalf("next phase = %v", result.Phase)
	}
}

func TestHandleInput_ModifyResetsPhase(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# Requirements doc")

	result, err := m.HandleInput("user1", "加一个登录功能")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if result.Action != ActionModify {
		t.Fatalf("action = %q, want modify", result.Action)
	}
	if result.ModifyHint != "加一个登录功能" {
		t.Fatalf("ModifyHint = %q", result.ModifyHint)
	}
	state := m.GetActive("user1")
	if state.ActivePhase().Status != PhaseRunning {
		t.Fatalf("phase status = %q, want running", state.ActivePhase().Status)
	}
}

func TestHandleInput_CancelTerminates(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# doc")

	result, err := m.HandleInput("user1", "取消")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if result.Action != ActionCancelled {
		t.Fatalf("action = %q", result.Action)
	}
	if m.GetActive("user1") != nil {
		t.Fatal("workflow should be cancelled")
	}
}

func TestHandleInput_UnrelatedMessagePassesThrough(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# doc")

	result, err := m.HandleInput("user1", "嗯")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if result.Action != ActionPassThrough {
		t.Fatalf("action = %q, want pass_through", result.Action)
	}
}

func TestFullCodingWorkflowLifecycle(t *testing.T) {
	m := setupTestMachine()
	m.Create("user1", "coding", "d:\\snake", "开发贪吃蛇 C++")

	// Phase 1: requirements
	m.RecordOutput("user1", "# 需求文档\n贪吃蛇功能...")
	result, _ := m.HandleInput("user1", "确认")
	if result.Phase.ID != "design" {
		t.Fatalf("expected design, got %s", result.Phase.ID)
	}

	// Phase 2: design
	m.RecordOutput("user1", "# 技术设计\nC++ Windows API...")
	result, _ = m.HandleInput("user1", "OK")
	if result.Phase.ID != "tasks" {
		t.Fatalf("expected tasks, got %s", result.Phase.ID)
	}

	// Phase 3: tasks
	m.RecordOutput("user1", "### T1: 基础框架\n- 描述...\n")
	result, _ = m.HandleInput("user1", "没问题")
	if result.Phase.ID != "implementation" {
		t.Fatalf("expected implementation, got %s", result.Phase.ID)
	}

	// Phase 4: implementation (NeedsConfirm=false)
	// RecordOutput with NeedsConfirm=false auto-advances to verification.
	m.RecordOutput("user1", "all tasks done")
	state := m.GetActive("user1")
	if state == nil {
		t.Fatal("workflow should still be active (verification phase pending)")
	}
	if state.ActivePhase().ID != "verification" {
		t.Fatalf("expected verification phase, got %s", state.ActivePhase().ID)
	}

	// Phase 5: verification (NeedsConfirm=false)
	// RecordOutput auto-advances past last phase → workflow completed.
	m.RecordOutput("user1", "all tests passed")
	state = m.GetActive("user1")
	if state != nil {
		t.Fatal("workflow should be completed after verification")
	}
}

func TestNoActiveWorkflow_PassesThrough(t *testing.T) {
	m := setupTestMachine()
	result, err := m.HandleInput("user1", "hello")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if result.Action != ActionPassThrough {
		t.Fatalf("action = %q", result.Action)
	}
}

func TestClassifyConfirmIntentKeyword_ShortConfirm(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"确认", "confirm"},
		{"OK", "confirm"},
		{"好的", "confirm"},
		{"继续", "confirm"},
		{"好", "confirm"},
		{"取消", "cancel"},
		{"不做了", "cancel"},
		{"嗯", "unrelated"},
		{".", "unrelated"},
		{"加一个登录功能", "modify"},
		{"继续完善，加一个登录功能", "modify"}, // >8 runes, not short enough for confirm
		{"把技术栈换成React", "modify"},
		{"帮我查天气", "modify"}, // >4 runes but unrelated, keyword fallback is conservative
	}
	for _, tc := range tests {
		got := ClassifyConfirmIntentKeyword(tc.input)
		if got != tc.want {
			t.Errorf("ClassifyConfirmIntentKeyword(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseConfirmClassifierResponse(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"intent": "confirm"}`, "confirm"},
		{`{"intent": "modify"}`, "modify"},
		{`{"intent": "cancel"}`, "cancel"},
		{`{"intent": "unrelated"}`, "unrelated"},
		{"```json\n{\"intent\": \"modify\"}\n```", "modify"},
		{"confirm", "confirm"},
		{"I think this is a modification", ""}, // "modification" != "modify" substring
		{"random garbage", ""},
	}
	for _, tc := range tests {
		got := ParseConfirmClassifierResponse(tc.input)
		if got != tc.want {
			t.Errorf("ParseConfirmClassifierResponse(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestHandleInput_ClassifierUnavailable_PassesThrough(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	m := NewStateMachine(store, templates)
	// Classifier always fails (returns empty)
	m.SetConfirmClassifier(func(phaseContext, userText string) string {
		return "" // simulates LLM 503
	})

	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# Requirements doc")

	// User says "确认" but classifier fails
	result, err := m.HandleInput("user1", "确认")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	// Should pass through (not advance) - workflow stays in waiting_confirm
	if result.Action != ActionPassThrough {
		t.Fatalf("action = %q, want pass_through when classifier unavailable", result.Action)
	}
	// Workflow should still be active at requirements phase
	state := m.GetActive("user1")
	if state == nil {
		t.Fatal("workflow should still be active")
	}
	if state.ActivePhase().Status != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm", state.ActivePhase().Status)
	}
}

func TestHandleInput_NoClassifierSet_PassesThrough(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	m := NewStateMachine(store, templates)
	// No classifier set at all

	m.Create("user1", "coding", "d:\\project", "build app")
	m.RecordOutput("user1", "# Requirements doc")

	result, err := m.HandleInput("user1", "确认")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	// Without any classifier, intent is empty -> pass through
	if result.Action != ActionPassThrough {
		t.Fatalf("action = %q, want pass_through without classifier", result.Action)
	}
}
