package v2

import (
	"testing"
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
		{"继续完善，加一个登录功能", "modify"},  // >8 runes, not short enough for confirm
		{"把技术栈换成React", "modify"},
		{"帮我查天气", "modify"},  // >4 runes but unrelated, keyword fallback is conservative
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
		{"I think this is a modification", ""},  // "modification" != "modify" substring
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
