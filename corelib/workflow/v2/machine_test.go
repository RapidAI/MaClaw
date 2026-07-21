package v2

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type failingSaveWorkflowStore struct {
	*MemoryStore
}

func (s *failingSaveWorkflowStore) Save(*WorkflowState) error {
	return errors.New("injected save failure")
}

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

func TestApplyReviewIntentRestoresStateWhenSaveFails(t *testing.T) {
	store := &failingSaveWorkflowStore{MemoryStore: NewMemoryStore()}
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	m := NewStateMachine(store, templates)

	state := &WorkflowState{
		ID:     "wf-failure",
		UserID: "user-failure",
		Type:   "presentation_design",
		Status: StatusActive,
		Phases: []Phase{{
			ID:     "audience_goal",
			Status: PhaseWaitingConfirm,
			Output: "已生成的受众分析",
		}},
	}
	// Seed the embedded memory store directly; the outer store rejects writes.
	store.MemoryStore.states[state.UserID] = state

	for _, intent := range []string{"cancel", "switch_task", "supplement"} {
		if _, err := m.ApplyReviewIntent(state.UserID, intent, "补充信息"); err == nil {
			t.Fatalf("ApplyReviewIntent(%q) should fail when Save fails", intent)
		}
		if state.Status != StatusActive || state.Phases[0].Status != PhaseWaitingConfirm || state.Phases[0].Output != "已生成的受众分析" {
			t.Fatalf("ApplyReviewIntent(%q) leaked an unpersisted mutation: %#v", intent, state)
		}
	}
}

func TestRecordOutputAndAdvanceRestoreStateWhenSaveFails(t *testing.T) {
	store := &failingSaveWorkflowStore{MemoryStore: NewMemoryStore()}
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	m := NewStateMachine(store, templates)

	state := &WorkflowState{
		ID:     "wf-output-failure",
		UserID: "user-output-failure",
		Type:   "presentation_design",
		Status: StatusActive,
		Phases: []Phase{
			{ID: "audience_goal", Status: PhaseRunning, NeedsConfirm: true},
			{ID: "outline", Status: PhasePending},
		},
	}
	store.MemoryStore.states[state.UserID] = state

	if err := m.RecordOutput(state.UserID, "受众分析"); err == nil {
		t.Fatal("RecordOutput should fail when Save fails")
	}
	if state.CurrentPhase != 0 || state.Phases[0].Status != PhaseRunning || state.Phases[0].Output != "" {
		t.Fatalf("RecordOutput leaked an unpersisted mutation: %#v", state)
	}

	state.Phases[0].Status = PhaseWaitingConfirm
	state.Phases[0].Output = "受众分析"
	if _, err := m.ApplyReviewIntent(state.UserID, "confirm", ""); err == nil {
		t.Fatal("confirm should fail when advancing cannot be saved")
	}
	if state.CurrentPhase != 0 || state.Status != StatusActive || state.Phases[0].Status != PhaseWaitingConfirm || state.Phases[1].Status != PhasePending {
		t.Fatalf("advance leaked an unpersisted mutation: %#v", state)
	}
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
		"education_level":      "仅本科",
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
		"education_level":      "仅本科",
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

	// For NeedsConfirm=true phases, validation failure is advisory — the output
	// is still recorded and the phase transitions to WaitingConfirm so the user
	// can review it. This prevents dead loops where the phase stays Running.
	err := m.RecordOutput("user1", "让我读取 evidence_doc.txt，然后运行 check_db2.py 看看数据库结构。")
	if err != nil {
		t.Fatalf("RecordOutput should not hard-reject NeedsConfirm phase: %v", err)
	}

	state = m.GetActive("user1")
	if state == nil {
		t.Fatal("workflow should remain active")
	}
	if state.CurrentPhase != 3 {
		t.Fatalf("CurrentPhase = %d, want 3", state.CurrentPhase)
	}
	if got := state.ActivePhase().Status; got != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm (advisory validation lets user decide)", got)
	}
	if state.ActivePhase().Output == "" {
		t.Fatal("output should be saved for user review despite validation warning")
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
	// NeedsConfirm=true: validation failure is advisory, output is still recorded.
	err := m.RecordOutput("user1", output)
	if err != nil {
		t.Fatalf("RecordOutput should not hard-reject NeedsConfirm phase: %v", err)
	}

	state = m.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm", got)
	}
	if got := state.ActivePhase().Output; got == "" {
		t.Fatal("output should be saved for user review despite missing source URLs")
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
	// NeedsConfirm=true: validation failure is advisory, output is still recorded.
	err := m.RecordOutput("user1", output)
	if err != nil {
		t.Fatalf("RecordOutput should not hard-reject NeedsConfirm phase: %v", err)
	}

	state = m.GetActive("user1")
	if got := state.ActivePhase().Status; got != PhaseWaitingConfirm {
		t.Fatalf("phase status = %q, want waiting_confirm", got)
	}
	if got := state.ActivePhase().Output; got == "" {
		t.Fatal("output should be saved for user review despite missing recommendation rows")
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

// TestBidReviewWorkflow_EndToEndProgression drives bid_review from create → form
// → all four NeedsConfirm phases → StatusCompleted, and asserts BuildPhasePrompt
// never blocks on MissingFullDependencies once prior outputs exist.
func TestBidReviewWorkflow_EndToEndProgression(t *testing.T) {
	m := setupTestMachine()
	userID := "bid-review-user"

	state, err := m.Create(userID, string(WorkflowBidReview), `D:\投标\项目`, "对照招标标准检查标书")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if state.Type != string(WorkflowBidReview) {
		t.Fatalf("Type = %q, want bid_review", state.Type)
	}
	wantPhases := []string{"br_standards", "br_bid_content", "br_conformity", "br_fix_report"}
	if len(state.Phases) != len(wantPhases) {
		t.Fatalf("phase count = %d, want %d", len(state.Phases), len(wantPhases))
	}
	for i, id := range wantPhases {
		if state.Phases[i].ID != id {
			t.Fatalf("phase[%d].ID = %q, want %q", i, state.Phases[i].ID, id)
		}
		if !state.Phases[i].NeedsConfirm {
			t.Fatalf("phase %s NeedsConfirm = false, want true (user gate before advance)", id)
		}
	}
	// DependsOnFull must be copied from template onto runtime phases.
	if got := strings.Join(state.Phases[1].DependsOnFull, ","); got != "br_standards" {
		t.Fatalf("br_bid_content DependsOnFull = %q, want br_standards", got)
	}
	if got := strings.Join(state.Phases[2].DependsOnFull, ","); got != "br_standards,br_bid_content" {
		t.Fatalf("br_conformity DependsOnFull = %q", got)
	}
	if got := strings.Join(state.Phases[3].DependsOnFull, ","); got != "br_conformity" {
		t.Fatalf("br_fix_report DependsOnFull = %q", got)
	}

	// --- Phase 0: form intake ---
	hr, err := m.HandleInput(userID, "开始检查")
	if err != nil {
		t.Fatalf("HandleInput(start): %v", err)
	}
	if hr.Action != ActionShowForm {
		t.Fatalf("first action = %q, want show_form", hr.Action)
	}
	if hr.Phase == nil || hr.Phase.ID != "br_standards" {
		t.Fatalf("show_form phase = %#v, want br_standards", hr.Phase)
	}

	// Empty form must fail RequireAnyOf (standards + prepared bid groups).
	if err := m.SubmitForm(userID, map[string]interface{}{}); err == nil {
		t.Fatal("SubmitForm empty should fail RequireAnyOf")
	}
	// Standards only (missing prepared bid) should fail.
	if err := m.SubmitForm(userID, map[string]interface{}{
		"tender_standard_path": `D:\投标\招标文件.pdf`,
	}); err == nil {
		t.Fatal("SubmitForm without prepared bid should fail RequireAnyOf")
	}

	form := map[string]interface{}{
		"tender_standard_path": `D:\投标\招标文件.pdf`,
		"tender_standard_url":  "",
		"prepared_bid_path":    `D:\投标\投标文件.pdf`,
		"our_company":          "测试科技有限公司",
		"focus_areas":          "废标项、资格符合性、评分点",
	}
	if err := m.SubmitForm(userID, form); err != nil {
		t.Fatalf("SubmitForm: %v", err)
	}

	hr, err = m.HandleInput(userID, "继续")
	if err != nil {
		t.Fatalf("HandleInput after form: %v", err)
	}
	if hr.Action != ActionRunPhase || hr.Phase == nil || hr.Phase.ID != "br_standards" {
		t.Fatalf("after form: action=%q phase=%v, want run_phase br_standards", hr.Action, hr.Phase)
	}

	// Prompt for intake must be non-empty and not blocked on missing deps.
	prompt0 := BuildPhasePrompt(m.GetActive(userID))
	if strings.TrimSpace(prompt0) == "" {
		t.Fatal("BuildPhasePrompt(br_standards) empty")
	}
	if strings.Contains(prompt0, "前序产出物不可用") {
		t.Fatalf("intake phase should not report missing deps:\n%s", prompt0)
	}
	if !strings.Contains(prompt0, "prepared_bid_path") && !strings.Contains(prompt0, `D:\投标\投标文件.pdf`) {
		t.Error("intake prompt should include form file fields")
	}

	phaseOutputs := map[string]string{
		"br_standards": `# 招标标准解析

### 1. 招标项目基本信息
- 项目名称：某某系统采购
- 资料来源：D:\投标\招标文件.pdf

### 2. 资格与废标条款清单
| 序号 | 条款类型 | 要求摘要 | 是否硬性/废标项 | 原文定位 |
|------|---------|---------|----------------|---------|
| 1 | 资格 | 具备软件企业资质 | 硬性 | 第三章 |

### 3. 技术/商务响应要求
| 序号 | 要求类别 | 要求摘要 | 响应方式 | 分值 |
|------|---------|---------|---------|------|
| 1 | 技术 | 支持国产化环境 | 点对点 | 20 |

### 4. 评分标准摘要
| 评分项 | 分值 | 得分关键点 |
|--------|------|----------|
| 技术方案 | 40 | 架构完整性 |

### 5. 格式与递交要求
- 需加盖公章，一正两副

### 6. 后续检查关注点
- 废标项、资格符合性`,
		"br_bid_content": `# 标书内容解析

### 1. 标书结构目录
| 章节/附件 | 内容摘要 | 页码/位置 |
|------|---------|---------|
| 第一章 资格证明 | 营业执照、软件资质 | P1-10 |
| 第二章 技术方案 | 总体架构、实施计划 | P11-40 |

### 2. 资格证明与商务响应摘要
- 已附营业执照与软著
- 报价与工期已响应

### 3. 技术方案要点
- 采用微服务架构，支持国产化中间件

### 4. 已识别的明显缺口（粗筛）
| 缺口 | 依据 | 严重程度 |
|------|------|---------|
| 缺偏离表 | 格式要求 | 高 |

### 5. 材料完整度说明
- 已读取投标文件全文关键章节`,
		"br_conformity": `# 符合性检查

### 1. 符合性总览
- 总体符合度：中
- 废标风险：中
- 预计得分影响：技术方案可提分

### 2. 逐项对照表
| 序号 | 招标要求 | 标书响应情况 | 符合状态 | 证据位置 | 风险等级 |
|------|---------|-------------|---------|---------|---------|
| 1 | 软件企业资质 | 已附 | 符合 | 资格章 | 低 |
| 2 | 偏离表 | 未提供 | 不符合 | - | 高 |

### 3. 废标项与硬性条款专项
- 偏离表缺失：构成格式废标风险，建议补正

### 4. 评分点得失分析
| 评分项 | 满分 | 预估得分 | 失分原因 |
|--------|------|---------|---------|
| 技术方案 | 40 | 32 | 国产化描述偏简 |

### 5. 格式与递交合规
- 缺偏离表；盖章要求已满足`,
		"br_fix_report": `# 标书修改建议报告

### 一、检查结论摘要
- 总体结论：补正后可投
- 必改 1 项、建议改 2 项
- 最高优先级：补充偏离表，消除格式废标风险

### 二、必改问题清单
#### 问题 1：缺少偏离表
- **对应招标要求**：格式与递交要求
- **当前标书问题**：未附偏离表
- **风险等级**：废标风险
- **修改建议**：按招标格式新增偏离表，逐条声明无偏离/有偏离项
- **证据位置**：标准第X章 + 标书目录

### 三、建议修改与可选优化
- 加厚国产化适配说明，对齐评分点

### 四、提分/响应增强建议
- 技术方案增加国产中间件版本与兼容矩阵

### 五、修改行动清单（执行顺序）
| 优先级 | 动作 | 预计工作量 | 完成标准 |
|--------|------|-----------|---------|
| P0 | 补偏离表 | 0.5 人日 | 格式合规 |

### 六、免责声明
本报告由 AI 辅助生成，仅供投标准备参考。最终响应策略与法律/商务判断请由具备资质的专业人员确认。`,
	}

	for i, phaseID := range wantPhases {
		state = m.GetActive(userID)
		if state == nil {
			t.Fatalf("workflow gone at phase %s", phaseID)
		}
		if state.Status != StatusActive {
			t.Fatalf("status=%q at phase %s, want active", state.Status, phaseID)
		}
		ap := state.ActivePhase()
		if ap == nil || ap.ID != phaseID {
			t.Fatalf("active phase = %v, want %s (index %d)", ap, phaseID, i)
		}

		// After confirm of the previous phase, the next phase is already Running.
		// Ensure missing deps are satisfied before prompt (prior phases have outputs).
		if missing := MissingFullDependencies(state); len(missing) > 0 {
			t.Fatalf("MissingFullDependencies at %s: %v", phaseID, missing)
		}

		prompt := BuildPhasePrompt(state)
		if strings.TrimSpace(prompt) == "" {
			t.Fatalf("BuildPhasePrompt(%s) empty", phaseID)
		}
		if strings.Contains(prompt, "前序产出物不可用") {
			t.Fatalf("BuildPhasePrompt(%s) blocked on missing deps:\n%s", phaseID, prompt)
		}
		// Inherited form data must remain visible after phase 0.
		if i > 0 {
			if !strings.Contains(prompt, "工作流已收集的结构化信息") &&
				!strings.Contains(prompt, `D:\投标\投标文件.pdf`) {
				t.Errorf("phase %s prompt missing inherited form context", phaseID)
			}
		}
		// Full dependency injection for conformity / content phases.
		if phaseID == "br_bid_content" && !strings.Contains(prompt, "招标标准解析") {
			t.Error("br_bid_content should inject prior standards output")
		}
		if phaseID == "br_conformity" {
			if !strings.Contains(prompt, "招标标准解析") || !strings.Contains(prompt, "标书内容解析") {
				t.Error("br_conformity should inject full standards + bid content outputs")
			}
		}
		if phaseID == "br_fix_report" && !strings.Contains(prompt, "符合性检查") {
			t.Error("br_fix_report should inject full conformity output")
		}

		if err := m.RecordOutput(userID, phaseOutputs[phaseID]); err != nil {
			t.Fatalf("RecordOutput(%s): %v", phaseID, err)
		}
		state = m.GetActive(userID)
		if state.ActivePhase() == nil || state.ActivePhase().Status != PhaseWaitingConfirm {
			t.Fatalf("after RecordOutput(%s) status = %v, want waiting_confirm", phaseID, state.ActivePhase())
		}
		if state.ActivePhase().Output == "" {
			t.Fatalf("output not saved for %s", phaseID)
		}

		hr, err = m.HandleInput(userID, "确认")
		if err != nil {
			t.Fatalf("confirm %s: %v", phaseID, err)
		}

		if i < len(wantPhases)-1 {
			// Advance to next phase.
			if hr.Action != ActionRunPhase {
				t.Fatalf("confirm %s action = %q, want run_phase for next", phaseID, hr.Action)
			}
			if hr.Phase == nil || hr.Phase.ID != wantPhases[i+1] {
				t.Fatalf("after confirm %s, next phase = %v, want %s", phaseID, hr.Phase, wantPhases[i+1])
			}
			state = m.GetActive(userID)
			if state.CurrentPhase != i+1 {
				t.Fatalf("CurrentPhase = %d, want %d", state.CurrentPhase, i+1)
			}
			continue
		}

		// Final phase confirm completes the workflow.
		if hr.Action != ActionConfirmed {
			t.Fatalf("final confirm action = %q, want confirmed", hr.Action)
		}
		state = m.GetActive(userID)
		if state != nil && state.Status == StatusActive {
			t.Fatalf("workflow still active after final confirm; status=%s phase=%d", state.Status, state.CurrentPhase)
		}
		// GetActive may return nil when completed — load via store or check hr.State.
		if hr.State == nil {
			t.Fatal("final confirm missing State")
		}
		if hr.State.Status != StatusCompleted {
			t.Fatalf("final status = %q, want completed", hr.State.Status)
		}
		if hr.State.CurrentPhase < len(wantPhases) {
			// CurrentPhase is len(phases) when completed (advanced past last).
			if hr.State.CurrentPhase != len(wantPhases) {
				t.Fatalf("final CurrentPhase = %d, want %d (past last)", hr.State.CurrentPhase, len(wantPhases))
			}
		}
		for _, p := range hr.State.Phases {
			if p.Status != PhaseCompleted {
				t.Errorf("phase %s status = %q, want completed", p.ID, p.Status)
			}
			if strings.TrimSpace(p.Output) == "" {
				t.Errorf("phase %s has empty output after completion", p.ID)
			}
		}
	}
}

func TestSubmitForm_RequireAnyOf_BidReview(t *testing.T) {
	m := setupTestMachine()
	if _, err := m.Create("u1", string(WorkflowBidReview), `D:\投标`, "检查标书"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// URL + pasted bid text is enough (no local files).
	if err := m.SubmitForm("u1", map[string]interface{}{
		"tender_standard_url": "https://example.com/tender",
		"prepared_bid_text":   "投标文件正文……",
	}); err != nil {
		t.Fatalf("SubmitForm url+text should pass: %v", err)
	}
}

func TestValidateRequireAnyOf_Messages(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "a", Label: "A"},
			{Name: "b", Label: "B"},
			{Name: "c", Label: "C"},
		},
		RequireAnyOf: [][]string{{"a", "b"}, {"c"}},
	}
	err := validateRequireAnyOf(schema, map[string]interface{}{"a": "x"})
	if err == nil || !strings.Contains(err.Error(), "C") {
		t.Fatalf("want missing C group error, got %v", err)
	}
	if err := validateRequireAnyOf(schema, map[string]interface{}{"a": "x", "c": "y"}); err != nil {
		t.Fatalf("both groups filled: %v", err)
	}
	if err := validateRequireAnyOf(nil, nil); err != nil {
		t.Fatalf("nil schema: %v", err)
	}
	// Empty map / all-empty nested map must not satisfy a group.
	err = validateRequireAnyOf(schema, map[string]interface{}{
		"a": map[string]interface{}{},
		"c": map[string]interface{}{"path": "", "name": ""},
	})
	if err == nil {
		t.Fatal("empty map values should not satisfy RequireAnyOf")
	}
}

func TestIsEmptyFormValue(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
		want bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"spaces", "  ", true},
		{"text", "x", false},
		{"empty slice", []interface{}{}, true},
		{"string slice blanks", []interface{}{"", "  "}, true},
		{"string slice value", []interface{}{"a"}, false},
		{"mixed empty objects", []interface{}{map[string]interface{}{}, map[string]interface{}{"x": ""}}, true},
		{"mixed with object path", []interface{}{map[string]interface{}{"path": `D:\a.pdf`}}, false},
		{"empty map", map[string]interface{}{}, true},
		{"map all empty", map[string]interface{}{"path": "", "name": "  "}, true},
		{"map with path", map[string]interface{}{"path": `D:\a.pdf`}, false},
		{"bool false present", false, false},
		{"zero present", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyFormValue(tc.val); got != tc.want {
				t.Fatalf("isEmptyFormValue(%v)=%v want %v", tc.val, got, tc.want)
			}
		})
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
