package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const executableCodingBreakdown = "### T1: Create CMake project scaffold\n- **Description**: Create build files and source directory.\n- **Files**: `CMakeLists.txt`, `src/main.cpp`\n- **Dependencies**: None\n- **Priority**: P0\n- **Effort**: Small\n\n### T2: Implement game loop\n- **Description**: Add gameplay loop.\n- **Files**: `src/main.cpp`\n- **Dependencies**: T1\n- **Priority**: P0\n- **Effort**: Medium"

func moveWorkflowStateToPhase(t *testing.T, engine *workflow.WorkflowEngine, state *workflow.EngineState, phaseID string) {
	t.Helper()
	tmpl := engine.GetRegistry().Match(state.Type)
	if tmpl == nil {
		t.Fatalf("workflow template not found for %s", state.Type)
	}
	for i, phase := range tmpl.Phases {
		if phase.ID == phaseID {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			// Sync to V2 StateMachine if wired.
			if m := engine.GetMachine(); m != nil {
				m.SetActivePhaseForTest(state.UserID, i)
			}
			return
		}
	}
	t.Fatalf("workflow phase %s not found", phaseID)
}

func TestActivateWorkflowTaskOrchestratorUsesWorkflowProjectPathWithoutCreatingDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	projectDir := t.TempDir()
	tempWorkflowDir := filepath.Join(t.TempDir(), "workflow-project")
	if err := app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "proj-current", Name: "Current", Path: projectDir}},
		CurrentProject: "proj-current",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{ProjectPath: tempWorkflowDir})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: app, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		ActivateOrchestrator: true,
		TaskBreakdownText:    executableCodingBreakdown,
	}

	activated, errResp := h.activateWorkflowTaskOrchestrator(engine, "u1", resp)
	if errResp != nil {
		t.Fatalf("activateWorkflowTaskOrchestrator returned error: %#v", errResp)
	}
	if !activated {
		t.Fatal("expected orchestrator activation")
	}
	orch := h.getTaskOrchestratorReadOnly("u1")
	if orch == nil || !orch.IsActive() {
		t.Fatalf("orchestrator should be active, got %#v", orch)
	}
	if orch.ProjectPath != tempWorkflowDir {
		t.Fatalf("ProjectPath = %q, want workflow project %q", orch.ProjectPath, tempWorkflowDir)
	}
	if _, err := os.Stat(tempWorkflowDir); !os.IsNotExist(err) {
		t.Fatalf("orchestrator activation must not create workflow project directory, stat err=%v path=%s", err, tempWorkflowDir)
	}
	if len(orch.Tasks) != 2 || len(orch.Tasks[1].DependsOn) != 1 || orch.Tasks[1].DependsOn[0] != 0 {
		t.Fatalf("expected parsed tasks with bootstrap dependency, got %#v", orch.Tasks)
	}
}

func TestHandleWorkflowEngineResponseActivatesOrchestratorOnFirstAgentLoop(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}

	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: app, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    executableCodingBreakdown,
		PhasePrompt:          "implementation prompt",
	}

	if got := h.handleWorkflowEngineResponse(engine, "u1", resp, "desktop"); got != nil {
		t.Fatalf("RunAgentLoop response should be nil, got %#v", got)
	}
	orch := h.getTaskOrchestratorReadOnly("u1")
	if orch == nil || !orch.IsActive() {
		t.Fatalf("orchestrator should activate before agent loop marker, got %#v", orch)
	}
	// workflowStartProjectPathForOwner resolves via EffectiveWorkingDirForOwner —
	// the single source of truth (not the Projects list). With no owner-scoped
	// project path this is the effective workspace dir.
	if want := normalizeProjectSessionPath(corelib.EffectiveWorkspaceDir()); orch.ProjectPath != want {
		t.Fatalf("ProjectPath = %q, want %q", orch.ProjectPath, want)
	}
	if marker, ok := h.workflowAgentLoopMarker.Load("u1"); !ok || marker != true {
		t.Fatalf("workflow agent loop marker not set: %#v ok=%v", marker, ok)
	}
}

func TestHandleWorkflowEngineResponseBlocksWhenProjectPathInvalid(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), CurrentLanguage: "en"}
	projectPath := filepath.Join(t.TempDir(), "project-file")
	if err := os.WriteFile(projectPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{ProjectPath: projectPath})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: app, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    executableCodingBreakdown,
		PhasePrompt:          "implementation prompt",
	}

	got := h.handleWorkflowEngineResponse(engine, "u1", resp, "desktop")

	if got == nil || !strings.Contains(got.Error, "Invalid workflow project path") {
		t.Fatalf("expected invalid workflow project path error, got %#v", got)
	}
	if orch := h.getTaskOrchestratorReadOnly("u1"); orch != nil && orch.IsActive() {
		t.Fatalf("orchestrator must not activate when workflow project path is invalid, got %#v", orch)
	}
	if _, ok := h.workflowAgentLoopMarker.Load("u1"); ok {
		t.Fatal("workflow agent loop must not start after invalid workflow project path")
	}
}

func TestBackfillExecutionOrchestratorActivationRequiresOrchestratorPhase(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	workflowType := workflow.WorkflowType("full_non_orchestrator_backfill")
	if err := engine.GetRegistry().Register(&workflow.TemplateSpec{
		Type: workflowType,
		Name: "full non orchestrator backfill",
		Phases: []workflow.PhaseSpec{{
			ID:                  "generate_artifact",
			Name:                "Generate artifact",
			Prompt:              "generate artifact",
			Deliverable:         "artifact",
			ToolPolicy:          workflow.ToolFilterFull,
			Kind:                workflow.PhaseKindArtifactGeneration,
			MutationScope:       workflow.MutationScopeArtifact,
			DisableOrchestrator: true,
		}},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflowType, Summary: "generate"}); err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	h := &IMMessageHandler{taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{RunAgentLoop: true}

	h.backfillExecutionOrchestratorActivation(engine, "u1", resp)

	if resp.ActivateOrchestrator {
		t.Fatal("backfill must not activate orchestrator for full phases that opted out")
	}
}

func TestActivateWorkflowTaskOrchestratorRequiresExecutionPhase(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	if _, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}); err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		ActivateOrchestrator: true,
		TaskBreakdownText:    executableCodingBreakdown,
	}

	activated, errResp := h.activateWorkflowTaskOrchestrator(engine, "u1", resp)
	if errResp != nil {
		t.Fatalf("unexpected error response: %#v", errResp)
	}
	if activated {
		t.Fatal("orchestrator must not activate before workflow enters execution phase")
	}
}

func TestHandleWorkflowEngineResponseDoesNotRepairOutsideExecutionPhase(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	if _, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}); err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    "1. Project files created\n2. Gameplay implemented",
		PhasePrompt:          "requirements prompt",
	}

	if got := h.handleWorkflowEngineResponse(engine, "u1", resp, "desktop"); got != nil {
		t.Fatalf("RunAgentLoop response should be nil, got %#v", got)
	}
	ws := engine.GetActiveWorkflow("u1")
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingRequirements || ws.PendingReviewRevisionRequested {
		t.Fatalf("bogus orchestrator response outside execution must not rewind workflow, got %#v", ws)
	}
	if _, ok := h.workflowAgentLoopMarker.Load("u1"); !ok {
		t.Fatal("normal phase generation marker should still be set")
	}
}

func TestActivateWorkflowTaskOrchestratorRejectsNumberedCompletionReport(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding})
	if err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    "1. Project files created\n2. Gameplay implemented\n3. Build verified",
		PhasePrompt:          "implementation prompt",
	}

	activated, errResp := h.activateWorkflowTaskOrchestrator(engine, "u1", resp)
	if errResp != nil {
		t.Fatalf("unexpected error response: %#v", errResp)
	}
	if activated {
		t.Fatal("numbered completion report must not activate orchestrator")
	}
	if orch := h.getTaskOrchestratorReadOnly("u1"); orch != nil && orch.IsActive() {
		t.Fatalf("orchestrator should remain inactive, got %#v", orch)
	}
}

func TestActivateWorkflowTaskOrchestratorRejectsTNumberedCompletionReport(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding})
	if err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    "### T1: Project files created\n- Created CMakeLists.txt.\n\n### T2: Gameplay implemented\n- Added snake logic.\n\n### T3: Build verified\n- Build succeeded.",
		PhasePrompt:          "implementation prompt",
	}

	activated, errResp := h.activateWorkflowTaskOrchestrator(engine, "u1", resp)
	if errResp != nil {
		t.Fatalf("unexpected error response: %#v", errResp)
	}
	if activated {
		t.Fatal("T-numbered completion report without required task fields must not activate orchestrator")
	}
	if isExecutableCodingTaskBreakdown(resp.TaskBreakdownText, ParseTaskListFromText(resp.TaskBreakdownText)) {
		t.Fatal("completion report should not be executable coding task breakdown")
	}
}

func TestExecutableCodingTaskBreakdownRequiresAllTaskFields(t *testing.T) {
	if !isExecutableCodingTaskBreakdown(executableCodingBreakdown, ParseTaskListFromText(executableCodingBreakdown)) {
		t.Fatal("expected canonical coding breakdown to be executable")
	}
	missingEffort := strings.Replace(executableCodingBreakdown, "- **Effort**: Small\n", "", 1)
	if isExecutableCodingTaskBreakdown(missingEffort, ParseTaskListFromText(missingEffort)) {
		t.Fatal("coding breakdown missing effort must not be executable")
	}
}

func TestExecutableCodingTaskBreakdownRejectsToolBlockedLanguage(t *testing.T) {
	blockedText := executableCodingBreakdown + "\n\n## \u6267\u884c\u53d7\u963b\u8bf4\u660e\n\n\u5f53\u524d\u5de5\u5177\u5217\u8868\u6ca1\u6709 write_file/edit_file\uff0cbash unavailable\uff0c\u9700\u8981\u624b\u52a8\u521b\u5efa\u76ee\u5f55\u3002"
	if isExecutableCodingTaskBreakdown(blockedText, ParseTaskListFromText(blockedText)) {
		t.Fatal("coding task breakdown with tool-blocked execution language must not be executable")
	}
	if !containsCodingTaskBreakdownBlockedExecutionLanguage(blockedText) {
		t.Fatal("expected tool-blocked language detector to trigger")
	}
}

func TestExecutableCodingTaskBreakdownValidatesDependencyReferences(t *testing.T) {
	withT0Dependency := strings.Replace(executableCodingBreakdown, "- **Dependencies**: T1", "- **Dependencies**: T0", 1)
	if isExecutableCodingTaskBreakdown(withT0Dependency, ParseTaskListFromText(withT0Dependency)) {
		t.Fatal("coding breakdown dependency must not reference T0")
	}
	outOfRangeDependency := strings.Replace(executableCodingBreakdown, "- **Dependencies**: T1", "- **Dependencies**: T3", 1)
	if isExecutableCodingTaskBreakdown(outOfRangeDependency, ParseTaskListFromText(outOfRangeDependency)) {
		t.Fatal("coding breakdown dependency must reference an existing task")
	}
	futureDependency := strings.Replace(executableCodingBreakdown, "- **Dependencies**: None", "- **Dependencies**: T2", 1)
	if isExecutableCodingTaskBreakdown(futureDependency, ParseTaskListFromText(futureDependency)) {
		t.Fatal("coding breakdown dependency must reference a previous task")
	}
}

func TestExecutableCodingTaskBreakdownRequiresMarkdownT1Sequence(t *testing.T) {
	withoutHeading := strings.Replace(executableCodingBreakdown, "### T1:", "T1:", 1)
	if isExecutableCodingTaskBreakdown(withoutHeading, ParseTaskListFromText(withoutHeading)) {
		t.Fatal("coding breakdown must use markdown T-numbered headings")
	}
	wrongHeadingLevel := strings.Replace(executableCodingBreakdown, "### T1:", "## T1:", 1)
	if isExecutableCodingTaskBreakdown(wrongHeadingLevel, ParseTaskListFromText(wrongHeadingLevel)) {
		t.Fatal("coding breakdown must use level-3 markdown T-numbered headings")
	}
	withT0 := strings.Replace(executableCodingBreakdown, "### T1:", "### T0:", 1)
	if isExecutableCodingTaskBreakdown(withT0, ParseTaskListFromText(withT0)) {
		t.Fatal("coding breakdown must start at T1, not T0")
	}
	withSkippedNumber := strings.Replace(executableCodingBreakdown, "### T2:", "### T3:", 1)
	if isExecutableCodingTaskBreakdown(withSkippedNumber, ParseTaskListFromText(withSkippedNumber)) {
		t.Fatal("coding breakdown must use contiguous T numbering")
	}
}

func TestExecutableCodingTaskBreakdownRequiresFieldLabelsNotBodyMentions(t *testing.T) {
	bodyMentionsOnly := "### T1: Update profile flow\n- **Description**: Update profile.go, depends on nothing, priority high, effort small.\n\n### T2: Verify profile flow\n- **Description**: Test profile.go, depends on T1, priority high, effort small."
	if isExecutableCodingTaskBreakdown(bodyMentionsOnly, ParseTaskListFromText(bodyMentionsOnly)) {
		t.Fatal("coding breakdown must use explicit field labels, not body mentions")
	}
}

func TestHandleWorkflowEngineResponseBlocksInvalidCodingTaskBreakdownFallback(t *testing.T) {
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	state, err := engine.StartWorkflow("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding})
	if err != nil {
		t.Fatalf("StartWorkflow() error = %v", err)
	}
	moveWorkflowStateToPhase(t, engine, state, workflow.PhaseCodingImplementation)
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	resp := &workflow.WorkflowResponse{
		RunAgentLoop:         true,
		ActivateOrchestrator: true,
		TaskBreakdownText:    "1. Project files created\n2. Gameplay implemented\n3. Build verified",
		PhasePrompt:          "implementation prompt",
	}

	if got := h.handleWorkflowEngineResponse(engine, "u1", resp, "desktop"); got != nil {
		t.Fatalf("invalid coding task breakdown should schedule repair loop without immediate response, got %#v", got)
	}
	if marker, ok := h.workflowAgentLoopMarker.Load("u1"); !ok || marker != true {
		t.Fatalf("invalid coding task breakdown should start regeneration loop, marker=%#v ok=%v", marker, ok)
	}
	ws := engine.GetActiveWorkflow("u1")
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingTaskBreakdown || ws.PendingReviewPhaseID != workflow.PhaseCodingTaskBreakdown || !ws.PendingReviewRevisionRequested {
		t.Fatalf("workflow should be rewound to task breakdown regeneration, got %#v", ws)
	}
	if orch := h.getTaskOrchestratorReadOnly("u1"); orch != nil && orch.IsActive() {
		t.Fatalf("orchestrator should remain inactive, got %#v", orch)
	}
}
