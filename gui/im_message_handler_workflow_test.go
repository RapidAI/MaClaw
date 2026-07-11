package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

type mockLLMCallerGUI struct {
	Response string
	Err      error
	Calls    int
}

func (m *mockLLMCallerGUI) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	m.Calls++
	if m.Err != nil {
		return "", m.Err
	}
	return m.Response, nil
}

type mockEngineCallbacksGUI struct {
	SentTexts []string
}

type failingWorkflowStateStoreGUI struct {
	v2.NullStore
	fail bool
}

func (s *failingWorkflowStateStoreGUI) SaveWorkflowState(_ *v2.EngineState) error {
	if s != nil && s.fail {
		return errors.New("save workflow state failed")
	}
	return nil
}

func (m *mockEngineCallbacksGUI) SendTextToUser(userID, text string) error {
	m.SentTexts = append(m.SentTexts, text)
	return nil
}

func (m *mockEngineCallbacksGUI) EmitPhaseUpdate(userID string, state *v2.EngineState) error {
	return nil
}

func (m *mockEngineCallbacksGUI) EmitDocUpdate(userID, phaseID, content string) error {
	return nil
}

func (m *mockEngineCallbacksGUI) EmitGateResult(userID, phaseID string, result *v2.QualityGateResult) error {
	return nil
}

func (m *mockEngineCallbacksGUI) GetLang() string { return "zh" }

// setupWorkflowTestHandler creates BOTH a WorkflowEngine AND a StateMachine, syncing
// templates between them. This allows existing tests to use WorkflowEngine methods
// (StartWorkflow, GetActiveWorkflow, etc.) while having a V2 machine available.
//
// For new tests, prefer using V2 directly:
//
//	store := v2.NewMemoryStore()
//	reg := v2.NewTemplateRegistry()
//	v2.RegisterBuiltinTemplates(reg)
//	machine := v2.NewStateMachine(store, reg)
//
// This avoids the bridge overhead and tests the actual production path.
func setupWorkflowTestHandler(llmCaller v2.LLMCaller) (*IMMessageHandler, *mockEngineCallbacksGUI) {
	// V2 infrastructure: MemoryStore (in-memory, no file I/O) + TemplateRegistry + StateMachine.
	v2Store := v2.NewMemoryStore()
	v2Registry := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(v2Registry)
	v2Machine := v2.NewStateMachine(v2Store, v2Registry)
	v2Machine.SetAllowTempTestPaths(true)
	v2Machine.SetConfirmClassifier(func(_, text string) string {
		return v2.ClassifyConfirmIntentKeyword(text)
	})

	// Register test-only templates in V2 registry so machine.Create works.
	registerTestTemplatesInV2Registry(v2Registry)

	// WorkflowRegistry populated from templates so that existing test
	// code (h.app.workflowEngine.StartWorkflow) continues to work. The
	// RegisterBuiltinTemplates is intentionally empty (T12); we bridge V2
	// template data into TemplateSpec format here.
	v1Registry := v2.NewWorkflowRegistry()
	registerV2TemplatesIntoV1Registry(v1Registry, v2Registry)

	cb := &mockEngineCallbacksGUI{}
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, llmCaller, v1Registry)
	engine := v2.NewWorkflowEngine(v1Registry, understanding, v2.NullStore{}, cb)
	engine.SetMachine(v2Machine) // Sync engine StartWorkflow to V2 StateMachine

	// Use an isolated temp directory for config to prevent tests from polluting
	// the real ~/.maclaw/config.json (root cause of stale test paths leaking into
	// production config, e.g. TestWorkflowStartProjectPathPrefersProjectScopedOwner).
	//
	// Note: os.MkdirTemp is used instead of t.TempDir() because this helper doesn't
	// accept *testing.T (changing its signature would break 30+ callers). The temp dir
	// is not auto-cleaned, but this is acceptable for test environments — the OS
	// cleans temp dirs periodically, and each dir is <4KB (one config.json file).
	testHome, _ := os.MkdirTemp("", "maclaw-workflow-test-*")

	app := &App{
		testHomeDir:    testHome,
		workflowEngine: engine,
		workflowV2: &workflowV2State{
			machine:  v2Machine,
			store:    v2Store,
			registry: v2Registry,
			router:   v2.NewWorkflowRouter(v2Machine, v2Registry, nil),
		},
	}
	handler := &IMMessageHandler{app: app, confirmationStore: newAIConfirmationStore("")}
	return handler, cb
}

func TestResolveIMEntryContextRoutesPendingRemoteTemplateWhenWorkflowToggleDisabled(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-remote-template-disabled-toggle"
	handler.app.workflowDisabled.Store(true)
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{
		SessionID:  "ssh-test",
		WorkDir:    "/home/test",
		ProjectDir: "/home/test",
	})

	trimmed := "开发一个 C++ hello world"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if result.Response != nil {
		t.Fatalf("pending remote template should route to workflow loop, got response %#v", result.Response)
	}
	if !result.WorkflowAgentLoop {
		t.Fatalf("WorkflowAgentLoop = false, want true so remote CodingSubAgent executes despite disabled cold-start routing: %#v", result)
	}
	if result.WorkflowDocPhase {
		t.Fatalf("WorkflowDocPhase = true, want false for remote coding execution")
	}
}

func TestResolveIMEntryContextRoutesActiveWorkflowWhenWorkflowToggleDisabled(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{Response: "other"})
	engine := handler.app.workflowEngine
	userID := "test-active-workflow-disabled-toggle"
	workflowType := v2.WorkflowType("gui_active_workflow_disabled_toggle")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "active workflow disabled toggle",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "build a project"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("workflow should be awaiting review before disabled-toggle confirmation")
	}
	handler.app.workflowDisabled.Store(true)

	trimmed := "ok"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("active workflow should continue despite disabled cold-start routing, got %#v", result)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.CurrentPhase != "execute" || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to execute, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestResolveIMEntryContextRoutesEngineOnlyActiveWorkflowWhenWorkflowToggleDisabled(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{Response: "other"})
	engine := handler.app.workflowEngine
	userID := "test-engine-only-workflow-disabled-toggle"
	workflowType := v2.WorkflowType("gui_engine_only_workflow_disabled_toggle")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "engine only workflow disabled toggle",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "build a project"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}
	if wf := handler.getWorkflowV2(); wf == nil || wf.machine == nil {
		t.Fatal("test requires workflow v2 machine")
	} else if err := wf.machine.Cancel(userID); err != nil {
		t.Fatalf("Cancel machine workflow failed: %v", err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("engine workflow should remain awaiting review after machine state is absent")
	}
	handler.app.workflowDisabled.Store(true)

	trimmed := "ok"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("engine-only active workflow should continue despite disabled cold-start routing, got %#v", result)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.CurrentPhase != "execute" || engine.IsAwaitingReview(userID) {
		t.Fatalf("engine workflow should advance to execute, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

// syncPhaseToV2Machine advances the V2 StateMachine state to match the engine
// state's PhaseIndex. Called after test code manually sets state.PhaseIndex.
func syncPhaseToV2Machine(handler *IMMessageHandler, userID string, phaseIndex int) {
	if wf := handler.getWorkflowV2(); wf != nil && wf.machine != nil {
		wf.machine.SetActivePhaseForTest(userID, phaseIndex)
	}
}

// newPopulatedWorkflowRegistry creates a WorkflowRegistry populated from
// templates. Standalone tests that create their own WorkflowEngine should use
// this instead of the empty v2.NewWorkflowRegistry().
func newPopulatedWorkflowRegistry() *v2.WorkflowRegistry {
	v2Reg := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(v2Reg)
	v1Reg := v2.NewWorkflowRegistry()
	registerV2TemplatesIntoV1Registry(v1Reg, v2Reg)
	return v1Reg
}

// registerV2TemplatesIntoV1Registry iterates known V2 template types and
// registers equivalent TemplateSpec entries so that WorkflowEngine.s
// StartWorkflow/Match calls succeed in tests.
//
// Deprecated: This bridge exists solely for backward compat with 50+ test files
// that create WorkflowEngine instances via setupWorkflowTestHandler. New tests
// should use V2 directly:
//
//	reg := v2.NewTemplateRegistry()
//	v2.RegisterBuiltinTemplates(reg)
//	store := v2.NewMemoryStore()
//	machine := v2.NewStateMachine(store, reg)
//	// Then call machine.Create(), machine.Advance(), etc.
//
// Migration guide for existing tests:
//  1. Replace setupWorkflowTestHandler(&mockLLMCallerGUI{}) with direct V2 setup.
//  2. Replace h.app.workflowEngine.StartWorkflow(uid, intent) with machine.Create(uid, type, summary).
//  3. Replace h.app.workflowEngine.GetActiveWorkflow(uid) with machine.GetActive(uid).
//  4. Replace h.app.workflowEngine.CancelWorkflow(uid) with machine.Cancel(uid).
func registerV2TemplatesIntoV1Registry(v1Reg *v2.WorkflowRegistry, v2Reg *v2.TemplateRegistry) {
	// Dynamically obtain all registered types from the single source of truth.
	allTypes := v2Reg.AllTypes()
	for _, typ := range allTypes {
		v2Tmpl := v2Reg.Get(typ)
		if v2Tmpl == nil {
			continue
		}
		v1Phases := make([]v2.PhaseSpec, 0, len(v2Tmpl.Phases))
		for _, p := range v2Tmpl.Phases {
			v1Phases = append(v1Phases, v2.PhaseSpec{
				ID:           p.ID,
				Name:         p.Name,
				NeedsConfirm: p.NeedsConfirm,
				ToolPolicy:   mapToolPolicyToFilterPolicy(p.ToolPolicy),
			})
		}
		v1Reg.MustRegister(&v2.TemplateSpec{
			Type:          v2.WorkflowType(v2Tmpl.Type),
			Name:          v2Tmpl.Name,
			Description:   v2Tmpl.Description,
			Keywords:      v2Tmpl.Keywords,
			Phases:        v1Phases,
			RequiresInput: inferRequiresInputFromV2(v2Tmpl),
		})
	}

	// Register test-only workflow types that exist in tests but not in production.
	// These provide minimal phase data for backward compat.
	registerV1OnlyTestTemplates(v1Reg)
}

// registerTestTemplatesInV2Registry registers test-only workflow templates in the
// V2 TemplateRegistry so that machine.Create can find them during tests.
func registerTestTemplatesInV2Registry(reg *v2.TemplateRegistry) {
	reg.Register(&v2.WorkflowTemplate{
		Type: "ops_maintenance",
		Name: "运维操作",
		Phases: []v2.PhaseTemplate{
			{ID: "ops_intake", Name: "运维需求确认", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
			{ID: "readonly_collection", Name: "信息采集", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
			{ID: "artifact_plan", Name: "维护工件计划", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
			{ID: "risk_policy", Name: "风险策略", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
			{ID: "controlled_execution", Name: "受控执行", NeedsConfirm: false, ToolPolicy: v2.ToolPolicyOpsControlled},
		},
	})
	reg.Register(&v2.WorkflowTemplate{
		Type: "changjiang_scholar",
		Name: "长江学者申报",
		Phases: []v2.PhaseTemplate{
			{ID: "eligibility", Name: "资格审查", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
			{ID: "materials", Name: "材料准备", NeedsConfirm: true, ToolPolicy: v2.ToolPolicyDocOnly},
		},
	})
}

// registerV1OnlyTestTemplates registers workflow types that are used in test
// code but not (yet) present in V2. These are minimal stubs.
func registerV1OnlyTestTemplates(v1Reg *v2.WorkflowRegistry) {
	// ops_maintenance — used extensively in ops-guard and tool-execution tests.
	v1Reg.MustRegister(&v2.TemplateSpec{
		Type: v2.WorkflowOpsMaintenance,
		Name: "运维操作",
		Phases: []v2.PhaseSpec{
			{ID: "ops_intake", Name: "运维需求确认", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "readonly_collection", Name: "信息采集", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "artifact_plan", Name: "维护工件计划", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "risk_policy", Name: "风险策略", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "controlled_execution", Name: "受控执行", NeedsConfirm: false, ToolPolicy: v2.ToolFilterOpsControlled},
		},
	})

	// changjiang_scholar — used in workflow interception tests.
	v1Reg.MustRegister(&v2.TemplateSpec{
		Type: v2.WorkflowChangjiangScholar,
		Name: "长江学者申报",
		Phases: []v2.PhaseSpec{
			{ID: "applicant_info", Name: "申请人信息", Description: "长江学者申请人信息收集", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "research_profile", Name: "科研概况", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "application_doc", Name: "申报材料", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
		},
	})

	// changjiang_scholar_review — used in persistence test tables.
	v1Reg.MustRegister(&v2.TemplateSpec{
		Type: v2.WorkflowChangjiangScholarReview,
		Name: "长江学者评审",
		Phases: []v2.PhaseSpec{
			{ID: "review_criteria", Name: "评审标准", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
		},
	})
}

// mapToolPolicyToFilterPolicy converts ToolPolicy to ToolFilterPolicy alias.
//
// Note: Since ToolFilterPolicy is now a type alias for ToolPolicy
// (type ToolFilterPolicy = ToolPolicy), this function is semantically a no-op
// for matching cases. It exists only because the default branch maps unhandled
// policies to ToolFilterDocOnly (conservative fallback). If all V2 ToolPolicy
// values are handled, this function could be replaced with a direct assignment.
//
// Deprecated: New test code should use v2.ToolPolicy constants directly.
func mapToolPolicyToFilterPolicy(p v2.ToolPolicy) v2.ToolFilterPolicy {
	switch p {
	case v2.ToolPolicyDocOnly:
		return v2.ToolFilterDocOnly
	case v2.ToolPolicyFull:
		return v2.ToolFilterFull
	default:
		return v2.ToolFilterDocOnly
	}
}

// inferRequiresInputFromV2 checks if a V2 template's first phase has an
// InputSchema, which semantically means the workflow requires user input
// before it can start. Input-driven templates (contract_review, due_diligence,
// etc.) use this pattern.
func inferRequiresInputFromV2(tmpl *v2.WorkflowTemplate) *v2.InputRequirement {
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	schema := tmpl.Phases[0].InputSchema
	if schema == nil || schema.Title == "" {
		return nil
	}
	return &v2.InputRequirement{
		Description: schema.Title,
		AcceptText:  true,
	}
}

func TestActiveWorkflowIgnoresUICWorkflowTypeReplacement(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"workflow_task","score":0.95,"reason":"looks like another workflow","workflow_type":"grant_proposal"}]}`, nil
		},
	})
	engine := handler.app.workflowEngine
	userID := "test-active-workflow-ignores-uic-workflow-type"
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowChangjiangScholar,
		Summary:  "draft application",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler.handleWorkflowInterception(userID, "我是青年学者，研究方向是人工智能。", "weixin")

	if got := engine.GetActiveWorkflow(userID); got == nil || got.Type != v2.WorkflowChangjiangScholar {
		t.Fatalf("active workflow should not be replaced by UIC workflow_type, got %#v", got)
	}
}

func TestActiveWorkflowIgnoresUICNonWorkflowBypass(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.unifiedClassifier = testIntentClassifier(string(intent.LabelSSH))
	engine := handler.app.workflowEngine
	userID := "test-active-workflow-ignores-uic-nonworkflow-bypass"
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowChangjiangScholar,
		Summary:  "draft application",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "请把这段研究经历补充进当前申请材料。", "weixin")
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("active workflow input should be owned by workflow state machine, got %#v", resp)
	}
	if got := engine.GetActiveWorkflow(userID); got == nil || got.Type != v2.WorkflowChangjiangScholar {
		t.Fatalf("active workflow should remain active, got %#v", got)
	}
	if !strings.Contains(resp.Text, "长江学者申请人信息") {
		t.Fatalf("active workflow should return its own form prompt, got %q", resp.Text)
	}
}

func TestApprovePendingWorkflowConfirmationRecordsProjectPathWithoutCreatingDirectory(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-workflow-start-records-project-dir"
	projectPath := filepath.Join(t.TempDir(), "new-project", "nested")

	result := handler.approvePendingWorkflowConfirmation(userID, &pendingConfirmation{
		UserID:          userID,
		OriginalText:    "build a desktop game",
		Summary:         "build a desktop game",
		WorkflowType:    string(v2.WorkflowCoding),
		WorkflowSummary: "build a desktop game",
		WorkflowGoals:   []string{"build a desktop game"},
		LastProjectPath: projectPath,
	}, "desktop")

	if result.Handled && result.Response != nil && result.Response.Error != "" {
		t.Fatalf("approvePendingWorkflowConfirmation returned error: %s", result.Response.Error)
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("workflow start must not create project directory, stat err=%v path=%s", err, projectPath)
	}
	state := handler.app.workflowEngine.GetActiveWorkflow(userID)
	if state == nil {
		t.Fatal("workflow should be active after confirmation")
	}
	if state.ProjectPath != projectPath {
		t.Fatalf("workflow ProjectPath = %q, want %q", state.ProjectPath, projectPath)
	}
}

func TestApprovePendingWorkflowConfirmationInfersExplicitCodingProjectPath(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-workflow-start-infers-explicit-project-dir"
	projectPath := filepath.Join(t.TempDir(), "explicit-new-project")
	if filepath.VolumeName(projectPath) == "" {
		projectPath = `D:\workprj\maclaw-explicit-new-project`
	}

	result := handler.approvePendingWorkflowConfirmation(userID, &pendingConfirmation{
		UserID:          userID,
		OriginalText:    "build a desktop game in " + projectPath + " and create src files",
		Summary:         "build a desktop game",
		WorkflowType:    string(v2.WorkflowCoding),
		WorkflowSummary: "build a desktop game",
		WorkflowGoals:   []string{"build a desktop game in " + projectPath},
	}, "desktop")

	if result.Handled && result.Response != nil && result.Response.Error != "" {
		t.Fatalf("approvePendingWorkflowConfirmation returned error: %s", result.Response.Error)
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("workflow start must not create inferred project directory, stat err=%v path=%s", err, projectPath)
	}
	state := handler.app.workflowEngine.GetActiveWorkflow(userID)
	if state == nil {
		t.Fatal("workflow should be active after confirmation")
	}
	if state.ProjectPath != filepath.Clean(projectPath) {
		t.Fatalf("workflow ProjectPath = %q, want %q", state.ProjectPath, filepath.Clean(projectPath))
	}
}

func TestSetWorkflowWorkingDirRecordsProjectPathWithoutCreatingDirectory(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	adapter := NewGUIWorkflowAdapter(handler.app, engine)
	engine.SetCallbacks(adapter)
	userID := desktopUserID
	projectPath := filepath.Join(t.TempDir(), "selected-project")

	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler.app.SetWorkflowWorkingDir("  " + projectPath + "  ")

	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("SetWorkflowWorkingDir must not create project directory, stat err=%v path=%s", err, projectPath)
	}
	if got := adapter.GetWorkingDir(); got != projectPath {
		t.Fatalf("adapter working dir = %q, want %q", got, projectPath)
	}
	state := engine.GetActiveWorkflow(userID)
	if state == nil || state.ProjectPath != projectPath {
		t.Fatalf("workflow ProjectPath = %#v, want %q", state, projectPath)
	}
}

func TestSetWorkflowWorkingDirUsesProjectScopedWorkflowOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	adapter := NewGUIWorkflowAdapter(handler.app, engine)
	engine.SetCallbacks(adapter)
	projectRoot := filepath.Join(t.TempDir(), "project-root")
	projectUserID := projectSessionOwnerID(projectRoot)
	workingDir := filepath.Join(t.TempDir(), "selected-project")

	if err := handler.app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "p1", Name: "Project", Path: projectRoot}},
		CurrentProject: "p1",
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	if _, err := engine.StartWorkflowWithOptions(projectUserID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
	}, v2.WorkflowStartOptions{ProjectPath: projectRoot}); err != nil {
		t.Fatalf("StartWorkflowWithOptions project failed: %v", err)
	}
	if _, err := engine.StartWorkflow(desktopUserID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "unrelated desktop workflow",
	}); err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}

	handler.app.SetWorkflowWorkingDir("  " + workingDir + "  ")

	projectState := engine.GetActiveWorkflow(projectUserID)
	if projectState == nil || projectState.ProjectPath != workingDir {
		t.Fatalf("project workflow ProjectPath = %#v, want %q", projectState, workingDir)
	}
	desktopState := engine.GetActiveWorkflow(desktopUserID)
	if desktopState == nil || desktopState.ProjectPath == workingDir {
		t.Fatalf("desktop workflow must not receive project working dir, got %#v", desktopState)
	}
}

func TestSetWorkflowWorkingDirUpdatesWorkflowV2ProjectPath(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowEngine = nil
	handler.app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())

	workingDir := filepath.Join(t.TempDir(), "selected-project")
	if _, err := handler.app.workflowV2.machine.Create(desktopUserID, "coding", ".", "build a desktop game"); err != nil {
		t.Fatalf("Create v2 workflow failed: %v", err)
	}

	handler.app.SetWorkflowWorkingDir("  " + workingDir + "  ")

	if _, err := os.Stat(workingDir); !os.IsNotExist(err) {
		t.Fatalf("SetWorkflowWorkingDir must not create project directory, stat err=%v path=%s", err, workingDir)
	}
	state := handler.app.workflowV2.machine.GetActive(desktopUserID)
	if state == nil || state.ProjectPath != workingDir {
		t.Fatalf("workflowV2 ProjectPath = %#v, want %q", state, workingDir)
	}
	if got := handler.app.GetWorkflowWorkingDir(); got != workingDir {
		t.Fatalf("GetWorkflowWorkingDir() = %q, want %q", got, workingDir)
	}
}

func TestSetWorkflowWorkingDirRejectsFilePathForWorkflowV2(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowEngine = nil
	handler.app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())

	filePath := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if _, err := handler.app.workflowV2.machine.Create(desktopUserID, "coding", ".", "build a desktop game"); err != nil {
		t.Fatalf("Create v2 workflow failed: %v", err)
	}

	handler.app.SetWorkflowWorkingDir(filePath)

	state := handler.app.workflowV2.machine.GetActive(desktopUserID)
	if state == nil || state.ProjectPath != "." {
		t.Fatalf("workflowV2 ProjectPath = %#v, want unchanged '.'", state)
	}
}

func TestGetWorkflowWorkingDirHandlesWorkflowV2WithoutMachine(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowEngine = nil
	handler.app.workflowV2 = &workflowV2State{}

	if got := handler.app.GetWorkflowWorkingDir(); got != "" {
		t.Fatalf("GetWorkflowWorkingDir() = %q, want empty string", got)
	}
}

func TestWorkflowStartProjectPathPrefersProjectScopedOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	ownerProject := filepath.Join(t.TempDir(), "owner-project")
	currentProject := filepath.Join(t.TempDir(), "current-project")
	ownerID := projectSessionOwnerID(ownerProject)

	if err := handler.app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "current", Name: "Current", Path: currentProject}},
		CurrentProject: "current",
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	if got := handler.workflowStartProjectPathForOwner(ownerID); got != normalizeProjectSessionPath(ownerProject) {
		t.Fatalf("workflowStartProjectPathForOwner(%q) = %q, want %q", ownerID, got, normalizeProjectSessionPath(ownerProject))
	}
	if got := handler.workflowStartProjectPathForOwner(desktopUserID); got != currentProject {
		t.Fatalf("desktop workflowStartProjectPathForOwner = %q, want current project %q", got, currentProject)
	}
}

func TestStartAIAssistantBackgroundTaskUsesProjectWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	app := handler.app
	projectPath := filepath.Join(t.TempDir(), "policy-project")
	ownerID := projectSessionOwnerID(projectPath)
	if err := app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "p1", Name: "Project", Path: projectPath}},
		CurrentProject: "p1",
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	if _, err := app.workflowEngine.StartWorkflowWithOptions(ownerID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build app",
	}, v2.WorkflowStartOptions{ProjectPath: projectPath}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := app.workflowEngine.SkipPhaseForm(ownerID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	_, err := app.StartAIAssistantBackgroundTask(AIAssistantBackgroundTaskRequest{
		Text:        "run background implementation",
		ProjectPath: projectPath,
	})
	if err == nil || !strings.Contains(err.Error(), "delegate_task") {
		t.Fatalf("project workflow doc_only phase should reject background delegate_task, err=%v", err)
	}
}

func TestStartDesktopBackgroundTaskUsesProjectWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	projectPath := filepath.Join(t.TempDir(), "policy-project")
	ownerID := projectSessionOwnerID(projectPath)
	if _, err := handler.app.workflowEngine.StartWorkflowWithOptions(ownerID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build app",
	}, v2.WorkflowStartOptions{ProjectPath: projectPath}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(ownerID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	_, err := handler.StartDesktopBackgroundTask("run background implementation", projectPath)
	if err == nil || !strings.Contains(err.Error(), "delegate_task") {
		t.Fatalf("handler background task should reject by project workflow policy, err=%v", err)
	}
}

func TestWorkflowInterceptionLowConfidenceUnknownStillReachesUnderstandingConfirmation(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"Create community anniversary PPT","goals":["Build a polished deck"],"constraints":[],"confidence":0.82,"ready":true},"reply":"Ready to confirm presentation workflow.","ready":true}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = intent.New(intent.Config{})
	userID := "test-low-conf-uic-understanding-confirm"

	resp := handler.handleWorkflowInterception(userID, "create a five year community anniversary ppt", "desktop")
	if llm.Calls == 0 {
		t.Fatal("low-confidence unknown UIC result must still call IntentUnderstandingManager")
	}
	if resp == nil {
		t.Fatal("ready workflow from IntentUnderstandingManager should return confirmation response")
	}
	if handler.app.workflowEngine.HasActiveWorkflow(userID) {
		t.Fatal("workflow should wait for confirmation instead of starting immediately")
	}
}

func TestWorkflowInterceptionSurfacesUnderstandingErrorForWorkflowCandidate(t *testing.T) {
	llm := &mockLLMCallerGUI{Err: errors.New("connection refused")}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"workflow_task","score":0.82,"reason":"workflow-capable document task"}]}`, nil
		},
	})
	userID := "test-workflow-candidate-understanding-error"

	resp := handler.handleWorkflowInterception(userID, "write a multi-stage application document", "desktop")
	if resp == nil || resp.Error == "" {
		t.Fatal("workflow candidate should surface understanding LLM failure instead of falling through to ordinary agent")
	}
	if !strings.Contains(resp.Error, "工作流理解服务暂时不可用") {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if llm.Calls == 0 {
		t.Fatal("IntentUnderstandingManager should still be consulted before surfacing the failure")
	}
}

func TestWorkflowInterceptionSurfacesUnderstandingErrorLocalized(t *testing.T) {
	llm := &mockLLMCallerGUI{Err: errors.New("connection refused")}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.app.CurrentLanguage = "en"
	handler.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"workflow_task","score":0.82,"reason":"workflow-capable document task"}]}`, nil
		},
	})
	userID := "test-workflow-candidate-understanding-error-en"

	resp := handler.handleWorkflowInterception(userID, "write a multi-stage application document", "desktop")
	if resp == nil || !strings.Contains(resp.Error, "Workflow understanding service is temporarily unavailable") {
		t.Fatalf("expected English localized error, got %#v", resp)
	}
}

func TestShouldSurfaceWorkflowUnderstandingStartErrorRequiresWorkflowCandidate(t *testing.T) {
	uic := intent.New(intent.Config{})
	if shouldSurfaceWorkflowUnderstandingStartError(uic, &intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.90,
	}) {
		t.Fatal("unknown intent should not surface workflow-understanding transport errors")
	}
	if !shouldSurfaceWorkflowUnderstandingStartError(uic, &intent.ClassificationResult{
		Primary:    intent.LabelWorkflowTask,
		Confidence: 0.82,
	}) {
		t.Fatal("confident workflow candidate should surface workflow-understanding transport errors")
	}
}

func TestBugCondition_CategoryNoneReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-none"

	if _, err := understanding.Start(userID, "summarize a paper"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"none","summary":"content task","confidence":0.9,"ready":true},"reply":"ok","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp != nil {
		t.Fatalf("category none should fall through without starting a workflow, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("category none should not create an active workflow")
	}
}

func TestBugCondition_CategoryEmptyReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-empty"

	if _, err := understanding.Start(userID, "organize meeting notes"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"","summary":"simple task","confidence":0.5,"ready":true},"reply":"ok","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp != nil {
		t.Fatalf("empty category should fall through without starting a workflow, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("empty category should not create an active workflow")
	}
}

func TestActiveUnderstanding_ErrorPreservesSession(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"make slides","confidence":0.8,"ready":false},"reply":"please add style","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-understanding-error"

	if _, err := understanding.Start(userID, "make a memorial PPT"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Err = fmt.Errorf("temporary LLM failure")
	resp := handler.handleActiveUnderstanding(engine, userID, "energetic public theme")
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("expected user-visible retry guidance, got %#v", resp)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("active understanding session should survive transient HandleInput error")
	}
}

func TestActiveUnderstandingKeepsWorkflowCandidateWithoutConcreteType(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"make slides","confidence":0.8,"ready":false},"reply":"please add audience","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"workflow_task","score":0.86,"reason":"workflow candidate but no concrete type"}]}`, nil
		},
	})
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-active-understanding-keeps-workflow-candidate"

	if _, err := understanding.Start(userID, "make a memorial PPT"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	llm.Response = `{"intent":{"category":"presentation_design","summary":"make slides for students","confidence":0.82,"ready":false},"reply":"please add page count","ready":false}`

	resp := handler.handleWorkflowInterception(userID, "the audience is university students and the tone should be formal", "desktop")
	if resp == nil || !strings.Contains(resp.Text, "please add page count") {
		t.Fatalf("workflow candidate should stay in active understanding, got %#v", resp)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("workflow-candidate UIC result without concrete workflow_type must not cancel active understanding")
	}
}

func TestActiveUnderstandingKeepsOfficeClarificationWithoutConcreteType(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"make slides","confidence":0.8,"ready":false},"reply":"please add style","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = intent.New(intent.Config{
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"office","score":0.90,"reason":"office clarification without concrete workflow type"}]}`, nil
		},
	})
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-active-understanding-keeps-office-clarification"

	if _, err := understanding.Start(userID, "make a memorial PPT"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	llm.Response = `{"intent":{"category":"presentation_design","summary":"make slides in PowerPoint","confidence":0.82,"ready":false},"reply":"please add audience","ready":false}`

	resp := handler.handleWorkflowInterception(userID, "use PowerPoint format and keep the deck around 12 pages", "desktop")
	if resp == nil || !strings.Contains(resp.Text, "please add audience") {
		t.Fatalf("office clarification should stay in active understanding, got %#v", resp)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("office clarification must not cancel active understanding")
	}
}

func TestActiveUnderstandingEscapesForDirectExecutionIntent(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"make slides","confidence":0.8,"ready":false},"reply":"please add style","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = testIntentClassifier(string(intent.LabelSSH))
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-active-understanding-escapes-direct-execution"

	if _, err := understanding.Start(userID, "make a memorial PPT"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "ssh into the server and check current deployment logs", "desktop")
	if resp != nil {
		t.Fatalf("direct execution intent should fall through to agent loop, got %#v", resp)
	}
	if understanding.HasActiveSession(userID) {
		t.Fatal("direct execution intent should cancel active understanding session")
	}
}

func TestPreservation_ValidWorkflowCategory_AsksBeforeStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, cb := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-preservation-coding"

	if _, err := understanding.Start(userID, "help me build a project"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"coding","summary":"user confirmed start","confidence":0.9,"ready":true},"reply":"ok, start working","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp == nil || resp.Confirmation == nil {
		t.Fatalf("coding workflow should ask before startup, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("workflow should not start before user confirmation")
	}
	if got := handler.confirmationStore.get(userID); got == nil || got.WorkflowType != string(v2.WorkflowCoding) {
		t.Fatalf("expected pending workflow confirmation, got %#v", got)
	}
	if len(cb.SentTexts) != 0 {
		t.Fatal("workflow startup overview should wait for confirmation")
	}
}

func TestWorkflowConfirmation_ApproveStartsWorkflow(t *testing.T) {
	handler, cb := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-confirm-start"
	item := buildPendingWorkflowConfirmation(userID, "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)

	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled || result.Response == nil || (!strings.Contains(result.Response.Text, "right-side panel") && !strings.Contains(result.Response.Text, "右侧面板")) {
		t.Fatalf("confirmed workflow should show the structured phase form, got %#v", result)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("form-first workflow should not start the agent loop before form submission, got %#v", result)
	}
	if !engine.HasActiveWorkflow(userID) {
		t.Fatal("confirmed workflow should create an active workflow")
	}
	if len(cb.SentTexts) == 0 {
		t.Fatal("confirmed workflow startup should send an overview message")
	}
}

func TestWorkflowConfirmation_DirectExecutionSkipsWorkflowOnce(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-direct"
	item := buildPendingWorkflowConfirmation(userID, "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("cancel", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.SkipWorkflowOnce || !result.SkipExecutionConfirm {
		t.Fatalf("expected direct execution to skip workflow once, got %#v", result)
	}
	if trimmed != "build a project" || msg.Text != "build a project" {
		t.Fatalf("expected original text restored for agent loop, got trimmed=%q msg=%q", trimmed, msg.Text)
	}
	if handler.app.workflowEngine.HasActiveWorkflow(userID) {
		t.Fatal("direct execution should not start workflow")
	}
	// SkipWorkflowOnce=true means the V2 preflight layer will set
	// SkipWorkflowRouting=true, preventing workflow-v2 re-routing.
	if !result.SkipWorkflowOnce {
		t.Fatal("direct execution must set SkipWorkflowOnce to prevent re-routing")
	}
}

func TestWorkflowConfirmation_DirectExecutionUsesSummaryWhenGoalsMissing(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-direct-summary"
	item := buildPendingWorkflowConfirmation(userID, "start", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "Build the login flow and tests",
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("cancel", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.SkipWorkflowOnce || !result.SkipExecutionConfirm {
		t.Fatalf("expected direct execution to skip workflow once, got %#v", result)
	}
	if trimmed != "Build the login flow and tests" {
		t.Fatalf("expected direct execution to preserve semantic summary, got %q", trimmed)
	}
}

func TestWorkflowConfirmation_FreeformRevisionReentersRouting(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-revise"
	item := buildPendingWorkflowConfirmation(userID, "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: "Actually make it a presentation workflow"}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if result.Handled || result.SkipWorkflowOnce || result.ConfirmedResume || !result.ReprocessAsFreshTask || !result.SkipExecutionConfirm {
		t.Fatalf("free-form revision should re-enter routing, got %#v", result)
	}
	if !strings.Contains(trimmed, "build a project") || !strings.Contains(trimmed, "Actually make it a presentation workflow") {
		t.Fatalf("expected revised routing text to include original task and clarification, got %q", trimmed)
	}
	if got := handler.confirmationStore.get(userID); got != nil {
		t.Fatalf("expected old pending workflow confirmation to be cleared, got %#v", got)
	}
}

func TestWorkflowConfirmation_ExplicitGoalTakesPrecedenceOverSummary(t *testing.T) {
	item := buildPendingWorkflowConfirmation("u1", "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "Internal routing metadata",
		Goals:      []string{"build a project"},
		Confidence: 0.92,
	}, "", "zh")
	if strings.Contains(item.Summary, "Internal routing metadata") {
		t.Fatalf("workflow confirmation should ignore summary when explicit goal exists: %q", item.Summary)
	}
	if item.OriginalText != "build a project" {
		t.Fatalf("expected original task text from goal, got %q", item.OriginalText)
	}
}

func TestWorkflowConfirmation_NormalizesEmptyGoals(t *testing.T) {
	item := buildPendingWorkflowConfirmation("u1", "fallback text", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "summary text",
		Goals:      []string{"", "  real task  "},
		Confidence: 0.92,
	}, "", "zh")
	if item.OriginalText != "real task" {
		t.Fatalf("expected first non-empty goal as original text, got %q", item.OriginalText)
	}
	if len(item.WorkflowGoals) != 1 || item.WorkflowGoals[0] != "real task" {
		t.Fatalf("expected normalized workflow goals, got %#v", item.WorkflowGoals)
	}
}

func TestFilterToolsForOpsControlledAllowsOnlyOperationalTools(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("ssh", "ssh", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("create_session", "create session", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
	}

	filtered := v2.FilterToolDefinitions(v2.ToolFilterOpsControlled, tools)
	names := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		names[extractToolName(tool)] = true
	}

	for _, allowed := range []string{"ssh", "bash", "read_file"} {
		if !names[allowed] {
			t.Fatalf("expected ops-controlled filter to keep %s, got %#v", allowed, names)
		}
	}
	for _, blocked := range []string{"task", "create_session", "edit_file"} {
		if names[blocked] {
			t.Fatalf("expected ops-controlled filter to block %s, got %#v", blocked, names)
		}
	}
}

func TestApplyWorkflowToolFilterUsesActiveOpsPhasePolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-filter-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	tools := []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("task", "task", nil, nil),
	}
	filtered := handler.applyWorkflowToolFilter(userID, tools)
	names := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		names[extractToolName(tool)] = true
	}
	if !names["bash"] || names["task"] {
		t.Fatalf("expected active ops phase to keep bash and block task, got %#v", names)
	}
}

func TestWorkflowToolExecutionGuardBlocksDisallowedTool(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "task",
				Arguments: `{"action":"run"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Fatalf("expected rejection text, got %q", result.Text)
	}
}

func TestWorkflowPlanningToolExecutionGuardAllowsWriteFileOnly(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "planning-write-guard-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowCoding)
	for i, phase := range tmpl.Phases {
		if phase.ID == v2.PhaseCodingTaskBreakdown {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	if !handler.isWorkflowToolAllowedForOwner(userID, "write_file") {
		t.Fatal("planning phase should allow write_file for reviewable planning artifacts")
	}
	if allowed, reason := handler.isWorkflowToolCallAllowedForOwner(userID, "write_file", `{"path":"PLAN.md","content":"plan"}`); !allowed {
		t.Fatalf("planning phase should allow write_file execution, reason=%q", reason)
	}
	if handler.isWorkflowToolAllowedForOwner(userID, "edit_file") {
		t.Fatal("planning phase should still block edit_file implementation")
	}
	if allowed, _ := handler.isWorkflowToolCallAllowedForOwner(userID, "edit_file", `{"path":"main.go","old":"x","new":"y"}`); allowed {
		t.Fatal("planning phase should still reject edit_file execution")
	}
}

func TestConsumePendingTemplateCodingSubAgentExecutionClearsState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "template-coding-consume-user"
	projectPath := t.TempDir()
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateCodingProjectPath.Store(userID, projectPath)

	original := runTaskWithSubAgent
	t.Cleanup(func() { runTaskWithSubAgent = original })
	var capturedTask string
	var capturedProject string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		if task != nil {
			capturedTask = task.Description
		}
		capturedProject = projectPath
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "done"}
	}

	resp, handled := handler.consumePendingTemplateSubAgentExecution(
		IMUserMessage{UserID: userID, Text: "continue", Platform: "desktop"},
		"build from form",
		NewLoopContext("template-coding-test", 1, nil),
		"req-template-coding-test",
		nil,
		nil,
	)

	if !handled || resp == nil {
		t.Fatalf("pending coding template should be consumed, handled=%v resp=%#v", handled, resp)
	}
	if !strings.Contains(capturedTask, "build from form") || capturedProject != projectPath {
		t.Fatalf("coding template runner received task=%q project=%q", capturedTask, capturedProject)
	}
	if _, pending := handler.pendingV2SubAgentExecution.Load(userID); pending {
		t.Fatal("pendingV2SubAgentExecution should be cleared after coding template consumption")
	}
	if _, pending := handler.pendingTemplateCodingProjectPath.Load(userID); pending {
		t.Fatal("pendingTemplateCodingProjectPath should be cleared after coding template consumption")
	}
}

func TestConsumePendingTemplateRemoteCodingExecutionClearsState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "template-remote-coding-consume-user"
	remoteCtx := remoteCodingTemplateContext{SessionID: "ssh-test", WorkDir: "/srv/app", ProjectDir: "/srv/app"}
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateRemoteCoding.Store(userID, remoteCtx)

	original := remoteCodingTemplateRunner
	t.Cleanup(func() { remoteCodingTemplateRunner = original })
	var capturedTask string
	var capturedRemote remoteCodingTemplateContext
	remoteCodingTemplateRunner = func(h *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, ctx remoteCodingTemplateContext, loopCtx *LoopContext, userText string, onProgress func(string), onToken func(string)) *RemoteCodingSubAgentResult {
		capturedTask = userText
		capturedRemote = ctx
		return &RemoteCodingSubAgentResult{Status: "success", Summary: "remote done", ToolCalls: 1}
	}

	resp, handled := handler.consumePendingTemplateSubAgentExecution(
		IMUserMessage{UserID: userID, Text: "continue", Platform: "desktop"},
		"deploy from form",
		NewLoopContext("template-remote-coding-test", 1, nil),
		"req-template-remote-coding-test",
		nil,
		nil,
	)

	if !handled || resp == nil {
		t.Fatalf("pending remote template should be consumed, handled=%v resp=%#v", handled, resp)
	}
	if capturedTask != "deploy from form" || capturedRemote != remoteCtx {
		t.Fatalf("remote template runner received task=%q remote=%#v", capturedTask, capturedRemote)
	}
	if _, pending := handler.pendingV2SubAgentExecution.Load(userID); pending {
		t.Fatal("pendingV2SubAgentExecution should be cleared after remote template consumption")
	}
	if _, pending := handler.pendingTemplateRemoteCoding.Load(userID); pending {
		t.Fatal("pendingTemplateRemoteCoding should be cleared after remote template consumption")
	}
}

func TestConsumePendingTemplateRemoteCodingExecutionShowsFailureReason(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "template-remote-coding-failure-reason-user"
	remoteCtx := remoteCodingTemplateContext{SessionID: "ssh-test", WorkDir: "/srv/app", ProjectDir: "/srv/app"}
	handler.pendingV2SubAgentExecution.Store(userID, true)
	handler.pendingTemplateRemoteCoding.Store(userID, remoteCtx)

	original := remoteCodingTemplateRunner
	t.Cleanup(func() { remoteCodingTemplateRunner = original })
	remoteCodingTemplateRunner = func(*IMMessageHandler, corelib.MaclawLLMConfig, *http.Client, remoteCodingTemplateContext, *LoopContext, string, func(string), func(string)) *RemoteCodingSubAgentResult {
		return &RemoteCodingSubAgentResult{
			Status:  "failed",
			Summary: "已完成远程检查，但任务收尾失败。",
			Error:   "max iterations reached",
		}
	}

	resp, handled := handler.consumePendingTemplateSubAgentExecution(
		IMUserMessage{UserID: userID, Text: "continue", Platform: "desktop"},
		"verify remote project",
		NewLoopContext("template-remote-coding-failure-reason-test", 1, nil),
		"req-template-remote-coding-failure-reason-test",
		nil,
		nil,
	)
	if !handled || resp == nil || !strings.Contains(resp.Text, "远程编程未完成") || !strings.Contains(resp.Text, "失败原因：max iterations reached") {
		t.Fatalf("failed remote template response should retain the failure reason, handled=%v resp=%#v", handled, resp)
	}
}

func TestRemoteCodingTemplateFinalSummaryIsNotStreamedTwice(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	original := remoteCodingTemplateRunner
	t.Cleanup(func() { remoteCodingTemplateRunner = original })
	remoteCodingTemplateRunner = func(*IMMessageHandler, corelib.MaclawLLMConfig, *http.Client, remoteCodingTemplateContext, *LoopContext, string, func(string), func(string)) *RemoteCodingSubAgentResult {
		return &RemoteCodingSubAgentResult{Status: "success", Summary: "remote verification passed"}
	}

	var streamed []string
	resp := handler.runRemoteCodingTemplateSubAgent(
		"remote-summary-no-duplicate-user",
		"verify remote project",
		remoteCodingTemplateContext{SessionID: "ssh-test", WorkDir: "/srv/app", ProjectDir: "/srv/app"},
		NewLoopContext("remote-summary-no-duplicate-test", 1, nil),
		nil,
		func(text string) { streamed = append(streamed, text) },
	)
	if resp == nil || !strings.Contains(resp.Text, "remote verification passed") {
		t.Fatalf("final response should include the remote summary, got %#v", resp)
	}
	if len(streamed) != 0 {
		t.Fatalf("final response summary must not be emitted through onToken again, got %#v", streamed)
	}
}

func TestWorkflowToolExecutionGuardBlocksDocOnlyCodingDelegate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "doc-only-delegate-guard-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a project",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	original := runTaskWithSubAgent
	t.Cleanup(func() { runTaskWithSubAgent = original })
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject delegate_task before CodingSubAgent starts")
		return nil
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		Context: &LoopContext{
			SkipNeedsConfirmGate: true,
			WorkflowAgentLoop:    true,
		},
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "delegate_task",
				Arguments: `{"agent":"coding_workflow","request":"implement the project"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Fatalf("expected workflow policy rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksHighRiskCommandArguments(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-command-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"rm -rf / --no-preserve-root"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "reviewed runbook") {
		t.Fatalf("expected high-risk rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksCommandOutsideApprovedManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-manifest-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"systemctl restart mysql"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "approved risk-policy") {
		t.Fatalf("expected approved manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksMutatingCommandWithoutManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-no-manifest-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"systemctl restart nginx"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "allowed_commands") {
		t.Fatalf("expected missing manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksSSHUploadWithoutManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-ssh-upload-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "allowed_commands") {
		t.Fatalf("expected missing manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardAllowsApprovedSSHUpload(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-ssh-upload-approved-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: ssh
    action: upload
    target: prod-session
    command: "apply.sh -> /tmp/apply.sh"
`
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","session_id":"prod-session","local_path":"other.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected unapproved descriptor rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "approved risk-policy") {
		t.Fatalf("expected approved manifest rejection text, got %q", result.Text)
	}

	result = handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_2",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","session_id":"staging-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})
	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected wrong-target rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
}

func TestWorkflowToolExecutionGuardRejectsSkipNeedsConfirmGateWhenWorkflowActive(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-skip-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	syncPhaseToV2Machine(handler, userID, state.PhaseIndex)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:           userID,
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "task",
				Arguments: `{"action":"run"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("active workflow should ignore skip gate and enforce workflow policy, got kind=%q text=%q", result.FailureKind, result.Text)
	}
}

func TestWorkflowToolExecutionGuardAllowsSkipNeedsConfirmGateWithoutActiveWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-skip-no-workflow-user"

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:           userID,
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "task",
				Arguments: `{"action":"run"}`,
			},
		},
	})

	if result.FailureKind == toolFailurePolicyRejected {
		t.Fatalf("skip gate without active workflow should bypass workflow policy rejection, got %q", result.Text)
	}
}

func TestWorkflowAttachmentBypass_AllowsRequiredWorkflowInput(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-input-attachment"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	bypass := workflowAttachmentBypass(engine, userID, []MessageAttachment{{
		Type:     "image",
		FileName: "contract.png",
		MimeType: "image/png",
		Size:     64,
	}}, "")
	if bypass {
		t.Fatal("attachment must be routed into a workflow that is waiting for required input")
	}
}

func TestRouteWorkflowIMMessageSubmitsWaitingInputAfterInterception(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-route-input-attachment"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	result := handler.routeWorkflowIMMessage(IMUserMessage{
		UserID: userID,
		Text:   "",
		Attachments: []MessageAttachment{{
			Type:     "file",
			FileName: "contract.pdf",
			MimeType: "application/pdf",
			Size:     4096,
		}},
	}, "", false, false)
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("attachment input should start workflow agent loop without immediate response: %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil || len(ws.InputPayload.Attachments) != 1 {
		t.Fatalf("workflow attachment input was not persisted: %#v", ws)
	}
	if ws.InputPayload.Attachments[0].FileName != "contract.pdf" {
		t.Fatalf("unexpected attachment payload: %#v", ws.InputPayload.Attachments)
	}
}
func TestSubmitWorkflowInputIfWaitingPersistsPayloadAndStartsLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "contract body", []MessageAttachment{{
		Type:     "file",
		FileName: "contract.docx",
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Size:     2048,
	}}, "")
	if !handled || resp != nil {
		t.Fatalf("expected workflow input to be consumed and agent loop to continue, handled=%v resp=%#v", handled, resp)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil {
		t.Fatalf("workflow input payload was not persisted: %#v", ws)
	}
	if ws.InputPayload.Text != "contract body" || len(ws.InputPayload.Attachments) != 1 {
		t.Fatalf("unexpected workflow input payload: %#v", ws.InputPayload)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("workflow agent loop marker was not set after input submission")
	}
	prompt, ok := handler.stashedPhasePrompt.Load(userID)
	if !ok || !strings.Contains(prompt.(string), "contract.docx") {
		t.Fatalf("stashed phase prompt should contain submitted input evidence, got %#v", prompt)
	}
}

func TestSubmitWorkflowInputIfWaitingStopsAtFirstPhaseFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input-form-gate"
	workflowType := v2.WorkflowType("gui_input_form_gate_test")
	engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:          workflowType,
		Name:          "gui input form gate test",
		Description:   "test template",
		RequiresInput: &v2.InputRequirement{Description: "source document", AcceptText: true},
		Phases: []v2.PhaseSpec{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &v2.PhaseInputSchemaSpec{Title: "Context", Fields: []v2.PhaseInputFieldSpec{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  v2.ToolFilterDocOnly,
		}},
	})
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "review source"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "source text", nil, "")
	if !handled || resp == nil || (!strings.Contains(resp.Text, "right-side panel") && !strings.Contains(resp.Text, "右侧面板")) {
		t.Fatalf("input should return form guidance instead of starting loop, handled=%v resp=%#v", handled, resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("form-gated input must not set workflow agent loop marker")
	}
	if _, ok := handler.stashedPhasePrompt.Load(userID); ok {
		t.Fatal("form-gated input must not stash a phase prompt before form submission")
	}
}

func TestWorkflowFormSubmitContinuesSameWorkflowUser(t *testing.T) {
	t.Skip("WorkflowEngine disabled — this test exercises engine-only form submit path")
	userID := "desktop-user:C:/Users/ma139"
	registry := v2.NewWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := v2.NewWorkflowEngine(registry, understanding, v2.NullStore{}, &mockEngineCallbacksGUI{})
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{
		WorkflowEngine:    engine,
		UnifiedClassifier: intent.New(intent.Config{}),
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://localhost:8080/v1", Model: "test-model", Key: "test-key"}
		},
	})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(v2.PhaseCodingRequirements, map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: engine.GetActiveWorkflow(userID).ID,
		"project_name":              "snake",
		"tech_stack":                "cpp",
		"description":               "graphical game",
	}, "req-workflow-form")
	if resp == nil || resp.Error != "" {
		t.Fatalf("form submit failed: %#v", resp)
	}
	if !resp.Deferred || resp.RequestID != "req-workflow-form" {
		t.Fatalf("form submit continuation should preserve request id and defer response, got %#v", resp)
	}
	for i := 0; i < 50; i++ {
		if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("direct workflow continuation should consume marker for the same user")
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || !ws.PhaseFormSubmitted {
		t.Fatalf("form should be submitted on original workflow user, got %#v", ws)
	}
	if ws := engine.GetActiveWorkflow(desktopUserID); ws != nil {
		t.Fatalf("form submit must not fork workflow onto generic desktop user: %#v", ws)
	}
}

func TestBuildWorkflowPhaseFormAgentViewPreservesCodingDirectoryField(t *testing.T) {
	tmpl := v2.NewWorkflowRegistry().Match(v2.WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding workflow template not found")
	}
	var schema *v2.PhaseInputSchemaSpec
	for _, phase := range tmpl.Phases {
		if phase.ID == v2.PhaseCodingRequirements {
			schema = phase.InputSchema
			break
		}
	}
	if schema == nil {
		t.Fatal("coding requirements phase missing input schema")
	}

	view := buildWorkflowPhaseFormAgentView("desktop-user:C:/Users/ma139", "wf-1", "proj-scope-1", v2.PhaseCodingRequirements, schema)
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok {
		t.Fatalf("workflow AG UI form fields have unexpected type: %#v", view["fields"])
	}
	byName := map[string]map[string]interface{}{}
	for _, field := range fields {
		name := fmt.Sprint(field["name"])
		if name != "" {
			byName[name] = field
		}
	}
	if byName["project_path"]["type"] != "directory" {
		t.Fatalf("coding project_path must reach AG UI as directory field, got %#v", byName["project_path"])
	}
	if byName[workflowFormWorkflowIDField]["value"] != "wf-1" {
		t.Fatalf("workflow hidden id field not preserved: %#v", byName[workflowFormWorkflowIDField])
	}
	if byName[workflowFormEventScopeField]["value"] != "proj-scope-1" {
		t.Fatalf("workflow hidden event scope field not preserved: %#v", byName[workflowFormEventScopeField])
	}
}

func TestWorkflowFormSubmitCachesEventScopeForWorkflowUser(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := newPopulatedWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := v2.NewWorkflowEngine(registry, understanding, v2.NullStore{}, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(v2.PhaseCodingRequirements, map[string]interface{}{
		workflowFormPhaseField:      v2.PhaseCodingRequirements,
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		workflowFormEventScopeField: "proj-scope-2",
		"project_name":              "snake",
	}, "req-workflow-form")
	if resp == nil {
		t.Fatal("expected workflow form submit response")
	}
	if scope, ok := app.sessionEventScopeIDs.Load(userID); !ok || scope != "proj-scope-2" {
		t.Fatalf("workflow form submit should cache event scope for workflow user, got ok=%v scope=%#v", ok, scope)
	}
}

func TestWorkflowFormSubmitProjectPathUpdatesWorkflowWithoutCreatingDirectory(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := newPopulatedWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := v2.NewWorkflowEngine(registry, understanding, v2.NullStore{}, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{
		WorkflowEngine:    engine,
		UnifiedClassifier: intent.New(intent.Config{}),
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://localhost:8080/v1", Model: "test-model", Key: "test-key"}
		},
	})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app
	projectPath := filepath.Join(t.TempDir(), "missing-form-project")

	resp := app.handleWorkflowFormAgentViewSubmit(v2.PhaseCodingRequirements, map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		"project_name":              "snake",
		"tech_stack":                "cpp",
		"description":               "graphical game",
		"project_path":              "  " + projectPath + "  ",
	}, "req-workflow-form-project-path")

	if resp == nil || resp.Error != "" {
		t.Fatalf("form submit failed: %#v", resp)
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("workflow form project_path must not create project directory, stat err=%v path=%s", err, projectPath)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.ProjectPath != filepath.Clean(projectPath) {
		t.Fatalf("workflow ProjectPath = %#v, want %q", ws, filepath.Clean(projectPath))
	}
	if got := fmt.Sprint(ws.PhaseFormData["project_path"]); got != filepath.Clean(projectPath) {
		t.Fatalf("PhaseFormData project_path = %q, want %q", got, filepath.Clean(projectPath))
	}
}

func TestWorkflowFormSubmitInvalidProjectPathRejectsBeforeFormMutation(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := newPopulatedWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := v2.NewWorkflowEngine(registry, understanding, v2.NullStore{}, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	invalidProject := filepath.Join(t.TempDir(), "project-file")
	if err := os.WriteFile(invalidProject, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(v2.PhaseCodingRequirements, map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		"project_name":              "snake",
		"tech_stack":                "cpp",
		"description":               "graphical game",
		"project_path":              invalidProject,
	}, "req-workflow-form-invalid-project-path")

	if resp == nil || resp.Error == "" {
		t.Fatalf("invalid project_path should fail form submit, got %#v", resp)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.PhaseFormSubmitted || len(ws.PhaseFormData) != 0 || ws.ProjectPath != "." {
		t.Fatalf("invalid project_path must not mutate workflow form/project state, got %#v", ws)
	}
}

func TestWorkflowFormSubmitRejectsHiddenPhaseMismatch(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := newPopulatedWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := v2.NewWorkflowEngine(registry, understanding, v2.NullStore{}, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(v2.PhaseCodingRequirements, map[string]interface{}{
		workflowFormPhaseField:      "stale_phase",
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		"project_name":              "snake",
	}, "req-workflow-form")
	if resp == nil || resp.Error == "" {
		t.Fatalf("phase mismatch should fail, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSubmitted {
		t.Fatalf("phase-mismatched submit must not mutate workflow form state, got %#v", ws)
	}
}

func TestWorkflowFormLifecyclePayloadWithFallbackPreservesSubmittedIdentity(t *testing.T) {
	payload := workflowFormLifecyclePayloadWithFallback("wf-new", v2.PhaseCodingRequirements, "desktop-user:C:/new", map[string]interface{}{
		workflowFormWorkflowIDField: "wf-old",
		workflowFormPhaseField:      v2.PhaseCodingRequirements,
		workflowFormUserIDField:     "desktop-user:C:/old",
	})
	if payload["workflow_id"] != "wf-old" || payload["workflow_user_id"] != "desktop-user:C:/old" {
		t.Fatalf("submitted workflow identity must win over server fallback, got %#v", payload)
	}
}

func TestWorkflowFormDismissDoesNotClearWhenSkipPersistenceFails(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := newPopulatedWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v2.NullStore{}, &mockLLMCallerGUI{}, registry)
	store := &failingWorkflowStateStoreGUI{}
	engine := v2.NewWorkflowEngine(registry, understanding, store, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	store.fail = true

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app
	beforeSeq := app.agentViewSeq()

	resp, err := app.DismissAgentView(AgentViewDismissPayload{ViewID: "workflow:form:" + v2.PhaseCodingRequirements, Data: map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		workflowFormPhaseField:      v2.PhaseCodingRequirements,
	}})
	if err == nil {
		t.Fatalf("dismiss should surface skip persistence failure, resp=%#v", resp)
	}
	if got := app.agentViewSeq(); got != beforeSeq {
		t.Fatalf("failed dismiss must not emit a clear lifecycle event, seq=%d want %d", got, beforeSeq)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSkipped || ws.PhaseFormSubmitted || len(ws.PhaseFormData) != 0 {
		t.Fatalf("failed dismiss must not mutate workflow form gate, got %#v", ws)
	}
}

func TestSubmitWorkflowInputIfWaitingIMUsesTextFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input-form-gate-im"
	workflowType := v2.WorkflowType("gui_input_form_gate_im_test")
	engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:          workflowType,
		Name:          "gui input form gate im test",
		Description:   "test template",
		RequiresInput: &v2.InputRequirement{Description: "source document", AcceptText: true},
		Phases: []v2.PhaseSpec{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &v2.PhaseInputSchemaSpec{Title: "Context", Fields: []v2.PhaseInputFieldSpec{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  v2.ToolFilterDocOnly,
		}},
	})
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "review source"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "source text", nil, "weixin")
	if !handled || resp == nil {
		t.Fatalf("IM input should return text form guidance, handled=%v resp=%#v", handled, resp)
	}
	if strings.Contains(resp.Text, "right-side panel") {
		t.Fatalf("IM input form guidance must not mention desktop side panel: %q", resp.Text)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("IM form-gated input must not set workflow agent loop marker")
	}
}

func TestResolveWorkflowFormUserIDUsesHiddenUserAfterLoopClearsLastUserID(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.lastUserID = ""

	got := resolveWorkflowFormUserID(handler, engine, state.CurrentPhase, map[string]interface{}{
		workflowFormUserIDField: userID,
	})
	if got != userID {
		t.Fatalf("expected hidden userID %q, got %q", userID, got)
	}
}

func TestResolveWorkflowFormUserIDFallsBackToSingleActivePhase(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.lastUserID = ""

	got := resolveWorkflowFormUserID(handler, engine, state.CurrentPhase, nil)
	if got != userID {
		t.Fatalf("expected phase fallback userID %q, got %q", userID, got)
	}
}

func TestResolveWorkflowFormUserIDDoesNotInheritCurrentRuntimeWhenAmbiguous(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userA := "desktop-user:C:/project-a"
	userB := "desktop-user:C:/project-b"
	stateA, err := engine.StartWorkflow(userA, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app a"})
	if err != nil {
		t.Fatalf("StartWorkflow A failed: %v", err)
	}
	if _, err := engine.StartWorkflow(userB, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app b"}); err != nil {
		t.Fatalf("StartWorkflow B failed: %v", err)
	}
	handler.currentLoopCtx = &LoopContext{Runtime: RuntimeContext{RequestID: "req-a", PolicyOwnerID: userA}}
	handler.lastUserID = userA

	if got := resolveWorkflowFormUserID(handler, engine, stateA.CurrentPhase, nil); got != "" {
		t.Fatalf("ambiguous form without hidden user inherited runtime user %q", got)
	}
}

func TestWorkflowFormMatchesActiveWorkflowRejectsStaleDismiss(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	if workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormWorkflowIDField: "stale-workflow-id",
	}) {
		t.Fatal("stale workflow form dismiss should not match active workflow")
	}
	if workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormPhaseField:      "stale_phase",
		workflowFormWorkflowIDField: state.ID,
	}) {
		t.Fatal("stale workflow phase dismiss should not match active workflow")
	}
	if !workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormPhaseField:      state.CurrentPhase,
		workflowFormWorkflowIDField: state.ID,
	}) {
		t.Fatal("current workflow form dismiss should match active workflow")
	}
}

func TestWorkflowFormLifecyclePayloadFallsBackToServerContext(t *testing.T) {
	userID := "desktop-user:C:/work"
	payload := workflowFormLifecyclePayloadFor("wf-current", v2.PhaseCodingRequirements, userID, map[string]interface{}{})
	if payload["workflow_id"] != "wf-current" {
		t.Fatalf("workflow_id = %#v", payload["workflow_id"])
	}
	if payload["workflow_phase"] != v2.PhaseCodingRequirements {
		t.Fatalf("workflow_phase = %#v", payload["workflow_phase"])
	}
	if payload["workflow_user_id"] != userID {
		t.Fatalf("workflow_user_id = %#v", payload["workflow_user_id"])
	}
}

func TestWorkflowFormLifecyclePayloadPreservesSubmittedContext(t *testing.T) {
	userID := "desktop-user:C:/work"
	payload := workflowFormLifecyclePayloadFor("", "", "", map[string]interface{}{
		workflowFormWorkflowIDField: "wf-submitted",
		workflowFormPhaseField:      v2.PhaseCodingRequirements,
		workflowFormUserIDField:     userID,
	})
	if payload["workflow_id"] != "wf-submitted" {
		t.Fatalf("workflow_id = %#v", payload["workflow_id"])
	}
	if payload["workflow_phase"] != v2.PhaseCodingRequirements {
		t.Fatalf("workflow_phase = %#v", payload["workflow_phase"])
	}
	if payload["workflow_user_id"] != userID {
		t.Fatalf("workflow_user_id = %#v", payload["workflow_user_id"])
	}
}

func TestBuildFormSubmissionSummaryLocalizesChinese(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	setAgentViewLang("zh-Hans")
	got := buildFormSubmissionSummary(map[string]interface{}{"goal": "build app"})
	if !strings.Contains(got, "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u6570\u636e\uff1a") || !strings.Contains(got, "goal: build app") {
		t.Fatalf("expected Chinese workflow form summary, got %q", got)
	}

	setAgentViewLang("en-US")
	got = buildFormSubmissionSummary(map[string]interface{}{"goal": "build app"})
	if !strings.HasPrefix(got, "The user submitted workflow form data: ") {
		t.Fatalf("expected English workflow form summary, got %q", got)
	}
}

func TestWorkflowFormSchemaLocalizesBusinessPlanForChineseUI(t *testing.T) {
	schema := &v2.PhaseInputSchemaSpec{
		Title:     "Business plan brief",
		TitleI18N: map[string]string{"zh": "商业计划简报"},
		Fields: []v2.PhaseInputFieldSpec{
			{Name: "project_name", Label: "Project or company name", LabelI18N: map[string]string{"zh": "项目或公司名称"}, Type: "text", Required: true, Placeholder: "Example: AI customer support SaaS platform"},
			{Name: "target_audience", Label: "Target reader", Type: "select", Required: true, Options: []v2.PhaseInputOptionSpec{{Label: "Investors (angel/VC/PE)", Value: "investor", LabelI18N: map[string]string{"zh": "投资人（天使/VC/PE）"}}}},
			{Name: "core_description", Label: "Project summary", Type: "textarea", Required: true, Placeholder: "In 2-3 sentences, explain what it does, what problem it solves, and who it serves.", PlaceholderI18N: map[string]string{"zh": "用 2-3 句话说明它做什么、解决什么问题、服务谁。"}},
		},
	}
	localized := localizeWorkflowPhaseInputSchema(schema, "zh-Hans")
	if localized.Title != "商业计划简报" {
		t.Fatalf("expected localized title, got %q", localized.Title)
	}
	fields := map[string]v2.PhaseInputFieldSpec{}
	for _, field := range localized.Fields {
		fields[field.Name] = field
	}
	if got := fields["project_name"].Label; got != "项目或公司名称" {
		t.Fatalf("project_name label = %q", got)
	}
	if got := fields["core_description"].Placeholder; got != "用 2-3 句话说明它做什么、解决什么问题、服务谁。" {
		t.Fatalf("core_description placeholder = %q", got)
	}
	if got := fields["target_audience"].Options[0].Label; got != "投资人（天使/VC/PE）" {
		t.Fatalf("target_audience option = %q", got)
	}
	if schema.Title != "Business plan brief" {
		t.Fatalf("localization mutated original schema title: %q", schema.Title)
	}
}

func TestWorkflowFormSchemaLocalizesWithExplicitMetadataBeforeFallback(t *testing.T) {
	schema := &v2.PhaseInputSchemaSpec{
		Title:     "Unknown English title",
		TitleI18N: map[string]string{"zh": "显式标题"},
		Fields: []v2.PhaseInputFieldSpec{{
			Name:            "custom",
			Label:           "Unknown English label",
			LabelI18N:       map[string]string{"zh": "显式字段"},
			Type:            "text",
			Description:     "Unknown English description",
			DescriptionI18N: map[string]string{"zh": "显式说明"},
			Placeholder:     "Unknown English placeholder",
			PlaceholderI18N: map[string]string{"zh": "显式占位"},
			Options:         []v2.PhaseInputOptionSpec{{Label: "Unknown English option", Value: "x", LabelI18N: map[string]string{"zh": "显式选项"}}},
		}},
	}

	localized := localizeWorkflowPhaseInputSchema(schema, "zh-CN")
	if localized.Title != "显式标题" || localized.Fields[0].Label != "显式字段" || localized.Fields[0].Description != "显式说明" || localized.Fields[0].Placeholder != "显式占位" || localized.Fields[0].Options[0].Label != "显式选项" {
		t.Fatalf("explicit i18n metadata not applied: %#v", localized)
	}
	if schema.Fields[0].Label != "Unknown English label" || schema.Fields[0].Options[0].Label != "Unknown English option" {
		t.Fatalf("localization mutated original schema: %#v", schema.Fields[0])
	}
}

func TestWorkflowFormSchemaAppliesExplicitEnglishMetadata(t *testing.T) {
	schema := &v2.PhaseInputSchemaSpec{
		Title:     "Default title",
		TitleI18N: map[string]string{"en": "English title", "en-US": "US title"},
		Fields: []v2.PhaseInputFieldSpec{{
			Name:            "custom",
			Label:           "Default label",
			LabelI18N:       map[string]string{"en": "English label"},
			Type:            "select",
			Description:     "Default description",
			DescriptionI18N: map[string]string{"en": "English description"},
			Placeholder:     "Default placeholder",
			PlaceholderI18N: map[string]string{"en": "English placeholder"},
			Options:         []v2.PhaseInputOptionSpec{{Label: "Default option", Value: "x", LabelI18N: map[string]string{"en": "English option"}}},
		}},
	}

	localized := localizeWorkflowPhaseInputSchema(schema, "en-US")
	if localized.Title != "US title" || localized.Fields[0].Label != "English label" || localized.Fields[0].Description != "English description" || localized.Fields[0].Placeholder != "English placeholder" || localized.Fields[0].Options[0].Label != "English option" {
		t.Fatalf("explicit English i18n metadata not applied: %#v", localized)
	}
}

func TestWorkflowFormSchemaEmptyLangDefaultsChineseFallback(t *testing.T) {
	schema := &v2.PhaseInputSchemaSpec{Title: "Business plan brief"}
	localized := localizeWorkflowPhaseInputSchema(schema, "")
	if localized.Title != "商业计划简报" {
		t.Fatalf("empty lang should use Chinese fallback, got %q", localized.Title)
	}
}

func TestWorkflowInterceptionKeepsPendingWorkflowAgentLoopAgainstUICBypass(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-submit-loop"
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.unifiedClassifier = testIntentClassifier(string(intent.LabelNonCoding))
	handler.workflowAgentLoopMarker.Store(userID, true)
	handler.stashedPhasePrompt.Store(userID, "phase prompt")

	resp := handler.handleWorkflowInterception(userID, "The user submitted workflow form data: goal: build app", "desktop")
	if resp != nil {
		t.Fatalf("pending workflow agent loop should not return immediate response: %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("pending workflow agent loop marker was cleared by UIC bypass")
	}
	if _, ok := handler.stashedPhasePrompt.Load(userID); !ok {
		t.Fatal("stashed phase prompt was cleared by UIC bypass")
	}
}

func TestWorkflowFormGateShortNonWorkflowBypassesFormLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-short-nonworkflow"
	handler.workflowAgentLoopMarker.Store(userID, true)
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "服务器信息", "desktop")
	if resp != nil {
		t.Fatalf("short non-workflow input should bypass workflow form, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("form-gate chat bypass should clear stale workflow loop marker")
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("bypass should leave workflow form gate untouched, got %#v", ws)
	}
}

func TestWorkflowFormGateDirectRunSkipsFormAndStartsLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-direct-run"
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "直接写", "desktop")
	if resp != nil {
		t.Fatalf("direct-run form skip should start workflow loop without immediate response, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("direct-run form skip should mark workflow agent loop")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.PhaseFormSkipped {
		t.Fatalf("direct-run form skip should persist skipped form gate, got %#v", ws)
	}
}

func TestWorkflowFormGateCancelCancelsWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-cancel"
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "取消", "desktop")
	if resp == nil || resp.Text == "" || resp.RunID != "" {
		t.Fatalf("cancel should return workflow cancellation text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws != nil {
		t.Fatalf("cancel should clear active workflow, got %#v", ws)
	}
}

func TestDismissAgentViewCancelWorkflowCancelsForegroundLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	app := handler.app
	app.remoteSessions = NewRemoteSessionManager(nil)
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	userID := desktopUserID
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	loopCtx := NewLoopContext("chat", 3, nil)
	handler.setSessionLoopCtx(userID, loopCtx)
	handler.globalLoopMu.Lock()
	handler.currentLoopCtx = loopCtx
	handler.lastUserID = userID
	handler.lastUserText = "workflow task"
	handler.globalLoopMu.Unlock()
	go func() {
		<-loopCtx.CancelC
		loopCtx.Done()
	}()

	resp, err := handler.app.DismissAgentView(AgentViewDismissPayload{
		ViewID: "workflow:form:" + v2.PhaseCodingRequirements,
		Data: map[string]interface{}{
			"__cancel_workflow": true,
		},
	})
	if err != nil {
		t.Fatalf("DismissAgentView failed: %v", err)
	}
	if resp == nil || resp.Text == "" {
		t.Fatalf("cancel workflow should return confirmation text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws != nil {
		t.Fatalf("cancel workflow should clear active workflow, got %#v", ws)
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("cancel workflow should also cancel the active foreground loop")
	}
	if !handler.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel workflow should mark a fresh-task boundary for the next message")
	}
}

func TestDismissAgentViewCancelWorkflowWithoutHandlerClearsWorkflowState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := desktopUserID
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if state := handler.app.workflowV2.machine.GetActive(userID); state == nil {
		t.Fatal("expected workflowV2 state to be active before dismissal")
	}

	resp, err := handler.app.DismissAgentView(AgentViewDismissPayload{
		ViewID: "workflow:form:" + v2.PhaseCodingRequirements,
		Data: map[string]interface{}{
			"__cancel_workflow": true,
		},
	})
	if err != nil {
		t.Fatalf("DismissAgentView failed: %v", err)
	}
	if resp == nil || resp.Text == "" {
		t.Fatalf("cancel workflow should return confirmation text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws != nil {
		t.Fatalf("cancel workflow should clear active workflow without handler, got %#v", ws)
	}
	if state := handler.app.workflowV2.machine.GetActive(userID); state != nil {
		t.Fatalf("cancel workflow should clear workflowV2 state without handler, got %#v", state)
	}
}

func TestDismissAgentViewCancelWorkflowPrefersPayloadUserID(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/right-project"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if state := handler.app.workflowV2.machine.GetActive(userID); state == nil {
		t.Fatal("expected workflowV2 state to be active before dismissal")
	}

	resp, err := handler.app.DismissAgentView(AgentViewDismissPayload{
		ViewID: "workflow:form:" + v2.PhaseCodingRequirements,
		Data: map[string]interface{}{
			"__cancel_workflow":         true,
			workflowFormUserIDField:     userID,
			workflowFormWorkflowIDField: state.ID,
		},
	})
	if err != nil {
		t.Fatalf("DismissAgentView failed: %v", err)
	}
	if resp == nil || resp.Text == "" {
		t.Fatalf("cancel workflow should return confirmation text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws != nil {
		t.Fatalf("cancel workflow should target payload user id, got %#v", ws)
	}
	if state := handler.app.workflowV2.machine.GetActive(userID); state != nil {
		t.Fatalf("cancel workflow should clear workflowV2 state for payload user id, got %#v", state)
	}
}

func TestDismissAgentViewCancelWorkflowSeedsPayloadEventScope(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/right-project"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	_, err = handler.app.DismissAgentView(AgentViewDismissPayload{
		ViewID: "workflow:form:" + v2.PhaseCodingRequirements,
		Data: map[string]interface{}{
			"__cancel_workflow":         true,
			workflowFormUserIDField:     userID,
			workflowFormWorkflowIDField: state.ID,
			workflowFormEventScopeField: "proj-scope-from-dismiss",
		},
	})
	if err != nil {
		t.Fatalf("DismissAgentView failed: %v", err)
	}
	if got := handler.app.getEventScopeID(userID); got != "proj-scope-from-dismiss" {
		t.Fatalf("event scope after dismiss = %q, want payload scope", got)
	}
}

func TestDismissAgentViewCancelWorkflowRejectsStalePayload(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/right-project"
	state, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, err := handler.app.DismissAgentView(AgentViewDismissPayload{
		ViewID: "workflow:form:" + v2.PhaseCodingRequirements,
		Data: map[string]interface{}{
			"__cancel_workflow":         true,
			workflowFormUserIDField:     userID,
			workflowFormWorkflowIDField: "stale-workflow-id",
			workflowFormPhaseField:      state.CurrentPhase,
		},
	})
	if err != nil {
		t.Fatalf("DismissAgentView failed: %v", err)
	}
	if resp == nil || resp.Text == "" {
		t.Fatalf("stale cancel should return explanatory text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.ID != state.ID {
		t.Fatalf("stale cancel must not cancel active workflow, got %#v", ws)
	}
	if v2State := handler.app.workflowV2.machine.GetActive(userID); v2State == nil || v2State.UserID != userID {
		t.Fatalf("stale cancel must preserve workflowV2 state, got %#v", v2State)
	}
}

func TestCaptureWorkflowDocAfterAgentLoopAutoAdvancesNonConfirmPhase(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-auto-advance-capture"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	engineState := engine.GetActiveWorkflow(userID)
	if engineState == nil || engineState.CurrentPhase != v2.PhaseCodingImplementation {
		t.Fatalf("expected implementation phase before capture, got %#v", engineState)
	}

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: userID}, nil, &IMAgentResponse{Text: reviewStateValidContentGUI()}, true)

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != v2.PhaseCodingReview {
		t.Fatalf("non-confirm phase should auto-advance to review, got %#v", ws)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("next phase agent loop marker was not set")
	}
	if prompt, ok := handler.stashedPhasePrompt.Load(userID); !ok || strings.TrimSpace(prompt.(string)) == "" {
		t.Fatalf("next phase prompt was not stashed, got %#v", prompt)
	}
}

func TestCaptureWorkflowDocAfterAgentLoopUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	desktopID := "desktop-post-loop-capture-owner"
	remoteOwnerID := "remote:mobile-post-loop-capture-owner"
	if _, err := engine.StartWorkflow(desktopID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "desktop build"}); err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if _, err := engine.StartWorkflow(remoteOwnerID, v2.StructuredIntent{Category: v2.WorkflowCoding, Summary: "remote build"}); err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := engine.AdvancePhase(remoteOwnerID); err != nil {
			t.Fatalf("AdvancePhase remote %d failed: %v", i, err)
		}
	}
	ctx := &LoopContext{ID: "post-loop-capture", RunID: "trace-post-loop-capture", Runtime: RuntimeContext{RequestID: "req-post-loop-capture", PolicyOwnerID: remoteOwnerID}}

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: desktopID}, ctx, &IMAgentResponse{Text: reviewStateValidContentGUI()}, true)

	remoteWS := engine.GetActiveWorkflow(remoteOwnerID)
	if remoteWS == nil || remoteWS.CurrentPhase != v2.PhaseCodingReview {
		t.Fatalf("remote owner workflow should capture and advance, got %#v", remoteWS)
	}
	desktopWS := engine.GetActiveWorkflow(desktopID)
	if desktopWS == nil || desktopWS.PhaseOutputs[v2.PhaseCodingRequirements] != "" {
		t.Fatalf("desktop workflow must not receive runtime-owned capture, got %#v", desktopWS)
	}
	if _, ok := handler.workflowReviewExperienceContext.Load(remoteOwnerID); !ok {
		t.Fatal("review experience context should be stored under runtime owner")
	}
	if _, ok := handler.workflowReviewExperienceContext.Load(desktopID); ok {
		t.Fatal("review experience context must not be stored under message user")
	}
}

func TestSchedulePostLoopSideEffectsUsesRuntimeOwnerForV2SyncCapture(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())

	desktopID := "desktop-v2-post-loop-sync"
	remoteOwnerID := "remote:v2-post-loop-sync"
	projectPath, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if _, err := handler.app.workflowV2.machine.Create(remoteOwnerID, "coding", projectPath, "build app"); err != nil {
		t.Fatalf("Create v2 workflow failed: %v", err)
	}
	ctx := &LoopContext{
		ID:                "v2-post-loop-sync",
		WorkflowAgentLoop: true,
		Runtime:           RuntimeContext{RequestID: "req-v2-post-loop-sync", PolicyOwnerID: remoteOwnerID},
	}
	resp := &IMAgentResponse{Text: "writing\n<details><summary>思考</summary>hidden</details>\n<tool_call[]>\n" +
		`{"name":"write_file","arguments":{"file_path":"d:\\project\\docs\\requirements.md","content":"requirements output"}}`}

	handler.schedulePostLoopSideEffects(IMUserMessage{UserID: desktopID}, ctx, resp, true)

	state := handler.app.workflowV2.machine.GetActive(remoteOwnerID)
	if state == nil || state.ActivePhase() == nil || state.ActivePhase().Output != "requirements output" {
		t.Fatalf("runtime owner v2 workflow should be captured synchronously, got %#v", state)
	}
	if desktopState := handler.app.workflowV2.machine.GetActive(desktopID); desktopState != nil {
		t.Fatalf("message user workflow must not be created or captured, got %#v", desktopState)
	}
}

func TestSchedulePostLoopSideEffectsPreservesRemoteSubAgentTriggerHint(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())

	userID := "paper-reproduction-remote-trigger"
	projectPath, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	state, err := handler.app.workflowV2.machine.Create(userID, "paper_reproduction", projectPath, "reproduce paper")
	if err != nil {
		t.Fatalf("Create v2 workflow failed: %v", err)
	}
	for i := range state.Phases {
		state.Phases[i].Status = v2.PhasePending
	}
	state.CurrentPhase = 3 // baseline_reproduction, immediately before iterative_improvement(remote_subagent)
	state.Phases[0].Status = v2.PhaseCompleted
	state.Phases[1].Status = v2.PhaseCompleted
	state.Phases[2].Status = v2.PhaseCompleted
	state.Phases[3].Status = v2.PhaseRunning

	ctx := &LoopContext{
		ID:                "paper-reproduction-remote-trigger",
		WorkflowAgentLoop: true,
		Runtime:           RuntimeContext{RequestID: "req-paper-reproduction-remote-trigger", PolicyOwnerID: userID},
	}
	resp := &IMAgentResponse{Text: "baseline reproduced successfully"}

	handler.schedulePostLoopSideEffects(IMUserMessage{UserID: userID}, ctx, resp, true)

	if !strings.Contains(resp.Text, "回复「开始迭代」启动自动迭代改进循环") {
		t.Fatalf("visible response should include remote subagent trigger hint, got %q", resp.Text)
	}
	updated := handler.app.workflowV2.machine.GetActive(userID)
	if updated == nil || updated.ActivePhase() == nil || updated.ActivePhase().ID != "iterative_improvement" {
		t.Fatalf("workflow should advance to remote subagent phase, got %#v", updated)
	}
}

func TestSchedulePostLoopSideEffectsSkipsWorkflowDocCaptureForTemplateSubAgentReport(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.workflowV2 = buildWorkflowV2State(v2.NewMemoryStore())

	userID := "template-subagent-report-skip-capture"
	projectPath, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if _, err := handler.app.workflowV2.machine.Create(userID, "coding", projectPath, "build hello world"); err != nil {
		t.Fatalf("Create v2 workflow failed: %v", err)
	}
	ctx := &LoopContext{
		ID:                     "template-subagent-report",
		WorkflowAgentLoop:      true,
		SkipWorkflowDocCapture: true,
		Runtime:                RuntimeContext{RequestID: "req-template-subagent-report", PolicyOwnerID: userID},
	}
	resp := &IMAgentResponse{Text: "远程编程已完成：创建 /home/test_demo/hello.cpp，编译和运行成功。"}

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: userID, Text: "开发 hello world c++ 版本"}, ctx, resp, true)
	if state := handler.app.workflowV2.machine.GetActive(userID); state == nil || state.ActivePhase() == nil || state.ActivePhase().Output != "" {
		t.Fatalf("direct workflow doc capture must respect template SubAgent skip flag, got %#v", state)
	}

	handler.schedulePostLoopSideEffects(IMUserMessage{UserID: userID, Text: "开发 hello world c++ 版本"}, ctx, resp, true)

	state := handler.app.workflowV2.machine.GetActive(userID)
	if state == nil || state.ActivePhase() == nil {
		t.Fatalf("workflow should remain active, got %#v", state)
	}
	if state.ActivePhase().Output != "" {
		t.Fatalf("template SubAgent terminal report must not be captured as workflow phase output, got %q", state.ActivePhase().Output)
	}
}

func TestBuildIMEntrySystemPromptUsesRuntimeOwnerStashedPhasePrompt(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-stashed-phase-prompt"
	remoteOwnerID := "remote:stashed-phase-prompt"
	ctx := &LoopContext{
		ID:      "stashed-phase-prompt",
		Runtime: RuntimeContext{RequestID: "req-stashed-phase-prompt", PolicyOwnerID: remoteOwnerID},
	}
	handler.stashedPhasePrompt.Store(remoteOwnerID, "PHASE_PROMPT_SENTINEL")
	handler.stashedPhasePrompt.Store(desktopID, "WRONG_PROMPT")

	prompt := handler.buildIMEntrySystemPrompt(IMUserMessage{UserID: desktopID, Text: "continue"}, nil, ctx, true, "", "", "", "")

	if !strings.Contains(prompt, "PHASE_PROMPT_SENTINEL") {
		t.Fatalf("system prompt missing runtime-owner stashed phase prompt")
	}
	if strings.Contains(prompt, "WRONG_PROMPT") {
		t.Fatalf("system prompt used message-user stashed phase prompt")
	}
	if _, ok := handler.stashedPhasePrompt.Load(remoteOwnerID); ok {
		t.Fatal("runtime-owner stashed phase prompt should be consumed")
	}
	if _, ok := handler.stashedPhasePrompt.Load(desktopID); !ok {
		t.Fatal("message-user stashed phase prompt should remain untouched")
	}
}

func TestWorkflowReviewConfirmInvalidCodingTaskBreakdownRegenerates(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-invalid-task-breakdown-confirm"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	invalidBreakdown := "# Temple game implementation complete\n\n" +
		"## Project structure\n\nThe project contains CMakeLists.txt, src/main.cpp, src/Game.cpp, and assets.\n\n" +
		"## Features\n\n- Movement is implemented.\n- Collision is implemented.\n- Audio is implemented.\n\n" +
		"## Build\n\nRun cmake and build the executable. This is intentionally a completion report, not a T-numbered task list."
	if phaseID, err := engine.SavePhaseOutput(userID, invalidBreakdown); err != nil || phaseID != v2.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput phase=%q err=%v", phaseID, err)
	}

	resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "继续", "desktop")
	if resp != nil {
		t.Fatalf("invalid task breakdown confirm should trigger regeneration loop, got immediate response %#v", resp)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != v2.PhaseCodingTaskBreakdown || ws.PendingReviewPhaseID != v2.PhaseCodingTaskBreakdown || !ws.PendingReviewRevisionRequested {
		t.Fatalf("invalid task breakdown should stay pending regeneration, got %#v", ws)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("invalid task breakdown confirm should schedule regeneration agent loop")
	}
	if handler.taskOrchestratorRegistry != nil {
		if orch := handler.taskOrchestratorRegistry.Get(userID); orch != nil && orch.IsActive() {
			t.Fatal("invalid task breakdown must not activate task orchestrator")
		}
	}
}

func TestWorkflowReviewFastConfirmBypassesPendingUserReply(t *testing.T) {
	t.Skip("WorkflowEngine disabled — this test exercises engine-only review/confirm path")
	llm := &mockLLMCallerGUI{Response: "other"}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	userID := "test-review-pending-reply-priority"
	workflowType := v2.WorkflowType("gui_review_pending_reply_priority")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "review pending reply priority",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: workflowType,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("workflow should be awaiting review before user confirmation")
	}
	handler.pendingUserReply.Store(userID, &pendingUserReplyState{Question: "Please create the project directory first?", Timestamp: time.Now()})

	trimmed := "开工"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if result.HasPendingUserReply || result.PendingUserReplyContext != "" {
		t.Fatalf("pending prose reply must not capture workflow review confirmation: %#v", result)
	}
	if !result.WorkflowAgentLoop {
		t.Fatalf("confirmed review should schedule next workflow phase loop: %#v", result)
	}
	if llm.Calls != 0 {
		t.Fatalf("short confirmation should use deterministic fast path, LLM calls=%d", llm.Calls)
	}
	if _, ok := handler.pendingUserReply.Load(userID); ok {
		t.Fatal("stale pending user reply should be cleared when workflow review owns the turn")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != "execute" || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to design after fast confirm, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestWorkflowReviewOkBypassesShortChitChatAndAdvances(t *testing.T) {
	llm := &mockLLMCallerGUI{Response: "other"}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	userID := "test-review-ok-not-short-chitchat"
	workflowType := v2.WorkflowType("gui_review_ok_not_short_chitchat")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "review ok not short chitchat",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: workflowType,
		Summary:  "build a desktop game",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("workflow should be awaiting review before ok confirmation")
	}

	if resp, handled := handler.handleImmediateIMCommand(IMUserMessage{UserID: userID, Text: "ok", Platform: "desktop"}, "ok", nil, nil); handled {
		t.Fatalf("workflow review ok must not be consumed by short chitchat, resp=%#v", resp)
	}

	result := handler.routeWithWorkflowV2(IMUserMessage{UserID: userID, Text: "ok", Platform: "desktop"}, "ok")
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("ok should confirm review and schedule next phase loop, got %#v", result)
	}
	if llm.Calls != 0 {
		t.Fatalf("ok review confirmation should use deterministic fast path, LLM calls=%d", llm.Calls)
	}
	ws := handler.getWorkflowV2().machine.GetActive(userID)
	if ws == nil || ws.ActivePhase() == nil || ws.ActivePhase().ID != "execute" {
		t.Fatalf("workflow should advance after ok confirmation, got %#v", ws)
	}
}

func TestWorkflowReviewExecutionRequestDoesNotStartAgentLoop(t *testing.T) {
	t.Skip("WorkflowEngine disabled — this test exercises engine-only review execution path")
	llm := &mockLLMCallerGUI{Response: "confirm"}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	userID := "test-review-execution-request-blocked"
	workflowType := v2.WorkflowType("gui_review_execution_request_blocked")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "review execution request blocked",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "build a desktop game"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}

	trimmed := "你直接创建一个"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if !result.Handled || result.Response == nil || strings.TrimSpace(result.Response.Text) == "" {
		t.Fatalf("execution request during review should return workflow barrier response, got %#v", result)
	}
	if !strings.Contains(result.Response.Text, "不能创建目录") || !strings.Contains(result.Response.Text, "确认") {
		t.Fatalf("execution request should explain workflow review boundary, got %q", result.Response.Text)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("execution request during review must not start agent loop: %#v", result)
	}
	if llm.Calls != 0 {
		t.Fatalf("execution request should be classified deterministically, LLM calls=%d", llm.Calls)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("execution request during review must not stash a workflow agent loop marker")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != "plan" || !engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should remain awaiting plan review, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestWorkflowReviewExecutionBlockedResponseUsesConfiguredLanguage(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.app.CurrentLanguage = "en"
	engine := handler.app.workflowEngine
	userID := "test-review-execution-blocked-lang"
	workflowType := v2.WorkflowType("gui_review_execution_blocked_lang")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "review execution blocked language",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}

	resp := handler.workflowReviewExecutionBlockedResponse(engine, userID)
	if resp == nil || !strings.Contains(resp.Text, "Current workflow phase") || !strings.Contains(resp.Text, "has not been confirmed yet") {
		t.Fatalf("expected English localized barrier response, got %#v", resp)
	}
	if strings.Contains(resp.Text, "涓嶈兘") || strings.Contains(resp.Text, "确认") {
		t.Fatalf("barrier response leaked Chinese text: %q", resp.Text)
	}
}

func TestDetectWorkflowReviewIntentFast(t *testing.T) {
	for _, text := range []string{"\u786e\u8ba4", "\u786e\u5b9a", "\u786e\u5b9a\u7ee7\u7eed", "\u7ee7\u7eed", "\u5f00\u5de5", "\u597d\u7684", "\u5408\u7406\uff0c\u7ee7\u7eed", "\u5f00\u59cb\u7f16\u7801", "\u5f00\u59cb\u7f16\u7801\u5427", "\u786e\u8ba4\u5f00\u59cb\u5b9e\u73b0", "\u786e\u5b9a\u5f00\u59cb\u5b9e\u73b0"} {
		got, ok := detectWorkflowReviewIntentFast(text)
		if !ok || got != v2.ReviewIntentConfirm {
			t.Fatalf("detectWorkflowReviewIntentFast(%q)=(%q,%v), want confirm,true", text, got, ok)
		}
	}
	for _, text := range []string{"开工", "继续", "继续推进", "OK", "go ahead", "start"} {
		got, ok := detectWorkflowReviewIntentFast(text)
		if !ok || got != v2.ReviewIntentConfirm {
			t.Fatalf("detectWorkflowReviewIntentFast(%q)=(%q,%v), want confirm,true", text, got, ok)
		}
	}
	// __wf_review__ structured button commands
	if got, ok := detectWorkflowReviewIntentFast("__wf_review__ confirm"); !ok || got != v2.ReviewIntentConfirm {
		t.Fatalf("__wf_review__ confirm: got (%q,%v), want confirm,true", got, ok)
	}
	if got, ok := detectWorkflowReviewIntentFast("__wf_review__ abort"); !ok || got != v2.ReviewIntentCancel {
		t.Fatalf("__wf_review__ abort: got (%q,%v), want cancel,true", got, ok)
	}
	if got, ok := detectWorkflowReviewIntentFast("__wf_review__ supplement_focus"); ok {
		t.Fatalf("__wf_review__ supplement_focus should return false (not matched), got (%q,%v)", got, ok)
	}
	if got, ok := detectWorkflowReviewIntentFast("你直接创建一个"); ok || got != v2.ReviewIntentOther {
		t.Fatalf("specific file operation should not be fast-confirmed, got (%q,%v)", got, ok)
	}
	if !detectWorkflowReviewBlockedExecutionIntent("你直接创建一个") || !detectWorkflowReviewBlockedExecutionIntent("创建目录 d:\\workprj\\snake5") {
		t.Fatal("execution-like review replies should be blocked before agent tools")
	}
	if detectWorkflowReviewBlockedExecutionIntent("继续推进") || detectWorkflowReviewBlockedExecutionIntent("修改一下设计文档") {
		t.Fatal("confirmations and document edits must not be treated as execution requests")
	}
}

func TestCodingWorkflowTaskBreakdownConfirmStartsImplementationWithCodingSubAgentGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-coding-task-breakdown-confirm-starts-implementation"
	_, err := engine.StartWorkflowWithOptions(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
		Goals:    []string{"build a desktop game"},
	}, v2.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("requirements")); err != nil || phaseID != v2.PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput requirements phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("requirements confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("tech_design")); err != nil || phaseID != v2.PhaseCodingTechDesign {
		t.Fatalf("SavePhaseOutput tech_design phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("tech design confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, executableCodingBreakdown); err != nil || phaseID != v2.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput task_breakdown phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("task breakdown should await review before implementation")
	}

	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("delegate_task", "delegate task", nil, nil),
	})

	trimmed := "\u5f00\u59cb\u7f16\u7801"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("task breakdown confirmation should start implementation agent loop, got %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != v2.PhaseCodingImplementation || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to implementation after task breakdown confirm, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
	stashedPromptValue, ok := handler.stashedPhasePrompt.Load(userID)
	if !ok {
		t.Fatal("implementation phase prompt should be stashed for the workflow agent loop")
	}
	stashedPrompt, _ := stashedPromptValue.(string)
	for _, want := range []string{"Coding Implementation Handoff Contract", "CodingSubAgent", "delegate_task(agent=\"coding_workflow\""} {
		if !strings.Contains(stashedPrompt, want) {
			t.Fatalf("stashed implementation prompt missing %q:\n%s", want, stashedPrompt)
		}
	}

	toolSet := handler.prepareAgentLoopTools(userID, trimmed, &LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(toolSet.Tools)
	for _, name := range []string{"read_file", "list_directory", "delegate_task"} {
		if !names[name] {
			t.Fatalf("implementation workflow loop must expose CodingSubAgent handoff/read tool %s, got %#v", name, names)
		}
	}
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if names[name] {
			t.Fatalf("implementation workflow loop must not expose local coding tool %s to main agent, got %#v", name, names)
		}
	}
	if toolSet.WorkflowDecision != workflowToolFilterDecision(v2.ToolFilterFull) {
		t.Fatalf("workflow decision = %q, want %q", toolSet.WorkflowDecision, v2.ToolFilterFull)
	}
	if !handler.isWorkflowToolAllowedForOwner(userID, "delegate_task") {
		t.Fatal("implementation workflow execution gate must allow delegate_task(coding_workflow)")
	}
	if allowed, reason := handler.isWorkflowToolCallAllowedForOwner(userID, "delegate_task", `{"agent":"coding_workflow","request":"implement T1"}`); !allowed {
		t.Fatalf("implementation workflow should allow CodingSubAgent delegation: %s", reason)
	}
	for _, tc := range []struct {
		name string
		args string
	}{
		{name: "delegate_task", args: `{"agent":"help","request":"help"}`},
		{name: "office", args: `{"action":"write_excel","file_path":"data.xlsx","data":{"sheets":[]}}`},
		{name: "web_fetch", args: `{"url":"https://example.com/app.go","save_path":"app.go"}`},
	} {
		if allowed, _ := handler.isWorkflowToolCallAllowedForOwner(userID, tc.name, tc.args); allowed {
			t.Fatalf("implementation workflow main loop must reject %s %s", tc.name, tc.args)
		}
	}
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if handler.isWorkflowToolAllowedForOwner(userID, name) {
			t.Fatalf("implementation workflow execution gate must block main-agent local coding tool %s", name)
		}
	}
	outPath := filepath.Join(t.TempDir(), "impl", "created.txt")
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	writeResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		Context: &LoopContext{WorkflowAgentLoop: true, Runtime: RuntimeContext{
			RequestID:     "req-implementation-write",
			PolicyOwnerID: userID,
		}},
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: fmt.Sprintf(`{"path":%q,"content":"ok"}`, outPath),
		}},
	})
	if writeResult.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("implementation workflow write_file must be policy-rejected for the main agent, got %+v", writeResult)
	}
	if _, err := os.ReadFile(outPath); err == nil {
		t.Fatalf("main-agent write_file unexpectedly created %s", outPath)
	}
}

func TestCodingWorkflowTaskBreakdownContinuePushStartsImplementation(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-coding-task-breakdown-continue-push-starts-implementation"
	_, err := engine.StartWorkflowWithOptions(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
		Goals:    []string{"build a desktop game"},
	}, v2.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("requirements")); err != nil || phaseID != v2.PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput requirements phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("requirements confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("tech_design")); err != nil || phaseID != v2.PhaseCodingTechDesign {
		t.Fatalf("SavePhaseOutput tech_design phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("tech design confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, executableCodingBreakdown); err != nil || phaseID != v2.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput task_breakdown phase=%q err=%v", phaseID, err)
	}

	trimmed := "\u7ee7\u7eed\u63a8\u8fdb"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("continue-push should confirm task breakdown and start implementation loop, got %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != v2.PhaseCodingImplementation || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to implementation after continue-push, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestCodingWorkflowTaskBreakdownImplementationRequestStartsImplementation(t *testing.T) {
	for _, text := range []string{
		"\u5b9e\u73b0\u8d2a\u5403\u86c7\u6e38\u620f\u7684\u6838\u5fc3\u529f\u80fd",
		"\u521b\u5efa\u76ee\u5f55 d:\\newgame \u5e76\u5199 CMakeLists.txt",
	} {
		t.Run(text, func(t *testing.T) {
			llm := &mockLLMCallerGUI{Response: "supplement"}
			handler, _ := setupWorkflowTestHandler(llm)
			engine := prepareCodingTaskBreakdownReviewForGUI(t, handler, "test-coding-task-breakdown-advance-"+sanitizeTestNameForWorkflow(text))

			trimmed := text
			result := handler.resolveIMEntryContext(imEntryContextOptions{
				Message: &IMUserMessage{UserID: "test-coding-task-breakdown-advance-" + sanitizeTestNameForWorkflow(text), Text: trimmed, Platform: "desktop"},
				Trimmed: &trimmed,
			})
			if result.Response != nil || !result.WorkflowAgentLoop {
				t.Fatalf("implementation-like task breakdown review reply should start implementation loop, got %#v", result)
			}
			if llm.Calls != 0 {
				t.Fatalf("phase-aware implementation advance should bypass LLM classifier, calls=%d", llm.Calls)
			}
			ws := engine.GetActiveWorkflow("test-coding-task-breakdown-advance-" + sanitizeTestNameForWorkflow(text))
			if ws == nil || ws.CurrentPhase != v2.PhaseCodingImplementation || engine.IsAwaitingReview(ws.UserID) {
				t.Fatalf("workflow should advance to implementation, got %#v awaiting=%v", ws, engine.IsAwaitingReview(ws.UserID))
			}
			prompt, ok := handler.stashedPhasePrompt.Load(ws.UserID)
			if !ok || !strings.Contains(fmt.Sprint(prompt), "Coding Implementation Handoff Contract") || !strings.Contains(fmt.Sprint(prompt), "delegate_task(agent=\"coding_workflow\"") {
				t.Fatalf("implementation prompt should contain CodingSubAgent handoff contract, got %#v", prompt)
			}
		})
	}
}

func TestCodingWorkflowTaskBreakdownDocumentFeedbackDoesNotFastAdvance(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-coding-task-breakdown-cmake-feedback"
	engine := prepareCodingTaskBreakdownReviewForGUI(t, handler, userID)

	if intent, ok := detectCodingTaskBreakdownReviewAdvanceIntent(engine, userID, "CMakeLists.txt \u7684\u4efb\u52a1\u62c6\u5f97\u592a\u7c97"); ok || intent != v2.ReviewIntentOther {
		t.Fatalf("document/task feedback should not fast-advance to implementation, got (%q,%v)", intent, ok)
	}
}

func prepareCodingTaskBreakdownReviewForGUI(t *testing.T, handler *IMMessageHandler, userID string) *v2.WorkflowEngine {
	t.Helper()
	engine := handler.app.workflowEngine
	_, err := engine.StartWorkflowWithOptions(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
		Goals:    []string{"build a desktop game"},
	}, v2.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("requirements")); err != nil || phaseID != v2.PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput requirements phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("requirements confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("tech_design")); err != nil || phaseID != v2.PhaseCodingTechDesign {
		t.Fatalf("SavePhaseOutput tech_design phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("tech design confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, executableCodingBreakdown); err != nil || phaseID != v2.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput task_breakdown phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("task breakdown should await review before implementation")
	}
	return engine
}

func sanitizeTestNameForWorkflow(text string) string {
	replacer := strings.NewReplacer("\\", "-", ":", "-", " ", "-", "/", "-", "\t", "-", "\n", "-")
	name := replacer.Replace(strings.ToLower(text))
	if len([]rune(name)) > 40 {
		name = string([]rune(name)[:40])
	}
	return name
}

func TestCaptureWorkflowDocRejectsInvalidCodingTaskBreakdown(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-invalid-task-breakdown-capture"
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: v2.WorkflowCoding,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	invalidBreakdown := "# Implementation complete\n\n" +
		"## Files\n\nCMakeLists.txt and src/main.cpp were created.\n\n" +
		"## Features\n\n- Gameplay loop done.\n- Renderer done.\n- Build instructions included.\n\n" +
		"This is long enough to look substantial but it is not a T-numbered executable task list."

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: userID}, nil, &IMAgentResponse{Text: invalidBreakdown}, true)

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		t.Fatal("workflow should remain active")
	}
	if ws.PhaseOutputs[v2.PhaseCodingTaskBreakdown] != "" {
		t.Fatalf("invalid task breakdown should not be saved, got output=%q", ws.PhaseOutputs[v2.PhaseCodingTaskBreakdown])
	}
	if ws.CurrentPhase != v2.PhaseCodingTaskBreakdown || ws.PendingReviewPhaseID != v2.PhaseCodingTaskBreakdown || !ws.PendingReviewRevisionRequested {
		t.Fatalf("invalid task breakdown should reopen repair loop on same phase, got %#v", ws)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("invalid task breakdown capture should schedule regeneration loop")
	}
	if prompt, ok := handler.stashedPhasePrompt.Load(userID); !ok || strings.TrimSpace(prompt.(string)) == "" {
		t.Fatalf("invalid task breakdown capture should stash regeneration prompt, got %#v ok=%v", prompt, ok)
	}
}

func reviewStateValidContentGUI() string {
	return "# Phase Output\n\n- Functional item A\n- Functional item B\n- Functional item C\n\nThis document is long enough to pass the minimum quality gate and exercise GUI capture auto-advance behavior."
}

func TestWorkflowReviewAdvanceIMUsesTextFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-review-advance-im-form"
	workflowType := v2.WorkflowType("gui_review_form_gate_im_test")
	engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "gui review form gate im test",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "reviewed", Name: "Reviewed", Prompt: "make reviewed output", Deliverable: "reviewed output", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{
				ID:          "collect_more",
				Name:        "Collect More",
				Prompt:      "collect more",
				Deliverable: "more context",
				InputSchema: &v2.PhaseInputSchemaSpec{Title: "More", Fields: []v2.PhaseInputFieldSpec{{Name: "scope", Label: "Scope", Type: "text", Required: true}}},
				ToolPolicy:  v2.ToolFilterDocOnly,
			},
		},
	})
	_, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "test"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, reviewStateValidContentGUI()); err != nil || phaseID != "reviewed" {
		t.Fatalf("saved phase=%q err=%v", phaseID, err)
	}

	resp := handler.applyWorkflowReviewIntent(engine, userID, v2.ReviewIntentConfirm, "确认", "weixin")
	if resp == nil || strings.Contains(resp.Text, "right-side panel") || !strings.Contains(resp.Text, "1.") {
		t.Fatalf("IM review advance should return numbered text form guidance, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("review advance into form phase must not start agent loop before form details")
	}
}

func TestWorkflowConfirmation_IMChannelUsesTextFormGuidance(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-confirm-start-im"
	item := buildPendingWorkflowConfirmation(userID, "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh", corelib.EffectiveWorkspaceDir())
	handler.confirmationStore.set(item)

	// IM confirmation of a coding workflow (no InputSchema on requirements
	// phase in the current V2 coding template): handlePostStartWorkflow routes
	// to handleActiveWorkflow which sets markers and returns nil. The
	// confirmation result therefore has Response=nil and markers set.
	msg := &IMUserMessage{UserID: userID, Platform: "weixin", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled {
		t.Fatalf("confirmed IM workflow should be handled, got %#v", result)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("WorkflowAgentLoop should be false (markers set directly), got %#v", result)
	}
	if !engine.HasActiveWorkflow(userID) {
		t.Fatal("confirmed workflow should create an active workflow")
	}
}

func TestWorkflowConfirmation_IMChannelGuidanceThenTextStartsPhaseLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-start-im-next"
	item := buildPendingWorkflowConfirmation(userID, "build a project", v2.StructuredIntent{
		Category:   v2.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh", corelib.EffectiveWorkspaceDir())
	handler.confirmationStore.set(item)

	// IM confirmation of a coding workflow (no form on requirements phase):
	// should trigger agent loop directly by setting markers.
	msg := &IMUserMessage{UserID: userID, Platform: "weixin", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled {
		t.Fatalf("confirmation should be handled, got %#v", result)
	}

	// The coding template has no InputSchema on requirements → handlePostStartWorkflow
	// routes to handleActiveWorkflow which sets markers and returns nil.
	// Markers should be set after confirmation.
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("IM workflow confirmation should set workflow agent loop marker")
	}
	prompt, ok := handler.stashedPhasePrompt.Load(userID)
	if !ok || strings.TrimSpace(prompt.(string)) == "" {
		t.Fatalf("IM workflow confirmation should stash phase prompt, got %#v", prompt)
	}
}
