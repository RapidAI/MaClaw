package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestNormalizeWorkflowStateForFrontendCanonicalizesAllPhaseFields(t *testing.T) {
	state := &workflow.WorkflowState{
		CurrentPhase:         "tech_design",
		PendingReviewPhaseID: "task_breakdown",
		PhaseOutputs: map[string]string{
			"requirements":   "requirements doc",
			"tech_design":    "design doc",
			"task_breakdown": "tasks doc",
		},
		GateResults: map[string]*workflow.QualityGateResult{
			"tech_design": {
				PhaseID: "tech_design",
				Passed:  true,
			},
		},
	}

	got := normalizeWorkflowStateForFrontend(state)

	if got.CurrentPhase != "design" {
		t.Fatalf("CurrentPhase = %q, want design", got.CurrentPhase)
	}
	if got.PendingReviewPhaseID != "tasks" {
		t.Fatalf("PendingReviewPhaseID = %q, want tasks", got.PendingReviewPhaseID)
	}
	if got.PhaseOutputs["design"] != "design doc" {
		t.Fatalf("design output missing after normalization: %#v", got.PhaseOutputs)
	}
	if got.PhaseOutputs["tasks"] != "tasks doc" {
		t.Fatalf("tasks output missing after normalization: %#v", got.PhaseOutputs)
	}
	if _, ok := got.PhaseOutputs["tech_design"]; ok {
		t.Fatalf("raw tech_design key leaked to frontend: %#v", got.PhaseOutputs)
	}
	if got.GateResults["design"] == nil || got.GateResults["design"].PhaseID != "design" {
		t.Fatalf("design gate result not canonicalized: %#v", got.GateResults["design"])
	}
}

func TestNormalizeWorkflowStateForFrontendIncludesCanonicalTemplatePhases(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowCoding,
		CurrentPhase: "tech_design",
	}

	got := normalizeWorkflowStateForFrontendWithRegistry(state, registry)

	if len(got.Phases) != 5 {
		t.Fatalf("len(Phases) = %d, want 5: %#v", len(got.Phases), got.Phases)
	}
	if got.Phases[1].ID != "design" {
		t.Fatalf("second phase ID = %q, want design: %#v", got.Phases[1].ID, got.Phases)
	}
	if got.Phases[2].ID != "tasks" {
		t.Fatalf("third phase ID = %q, want tasks: %#v", got.Phases[2].ID, got.Phases)
	}
	if got.Phases[1].Name == "" || got.Phases[2].Name == "" {
		t.Fatalf("phase names should be populated: %#v", got.Phases)
	}
	if !got.Phases[0].ExpectsDocument || !got.Phases[1].ExpectsDocument || !got.Phases[2].ExpectsDocument {
		t.Fatalf("planning phases should expect preview documents: %#v", got.Phases)
	}
	if got.Phases[3].ID != "implementation" || got.Phases[3].ExpectsDocument {
		t.Fatalf("implementation should be marked as a non-document phase: %#v", got.Phases[3])
	}
	if !got.Phases[4].ExpectsDocument {
		t.Fatalf("review should expect a preview document: %#v", got.Phases[4])
	}
}

func TestNormalizeWorkflowStateForFrontendIncludesOpsMaintenancePhases(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowOpsMaintenance,
		CurrentPhase: "risk_policy",
	}

	got := normalizeWorkflowStateForFrontendWithRegistry(state, registry)

	wantIDs := []string{"ops_intake", "readonly_collection", "artifact_plan", "risk_policy", "controlled_execution"}
	if len(got.Phases) != len(wantIDs) {
		t.Fatalf("len(Phases) = %d, want %d: %#v", len(got.Phases), len(wantIDs), got.Phases)
	}
	for i, wantID := range wantIDs {
		if got.Phases[i].ID != wantID {
			t.Fatalf("phase %d ID = %q, want %q: %#v", i, got.Phases[i].ID, wantID, got.Phases)
		}
		wantDocument := i < len(wantIDs)-1
		if got.Phases[i].ExpectsDocument != wantDocument {
			t.Fatalf("phase %s ExpectsDocument = %v, want %v", wantID, got.Phases[i].ExpectsDocument, wantDocument)
		}
	}
}

func TestNormalizeWorkflowStateForFrontendDoesNotMutateEngineState(t *testing.T) {
	state := &workflow.WorkflowState{
		CurrentPhase: "tech_design",
		PhaseOutputs: map[string]string{
			"tech_design": "design doc",
		},
		GateResults: map[string]*workflow.QualityGateResult{
			"tech_design": {PhaseID: "tech_design"},
		},
	}

	got := normalizeWorkflowStateForFrontend(state)
	got.PhaseOutputs["design"] = "changed"
	got.GateResults["design"].PhaseID = "changed"

	if state.CurrentPhase != "tech_design" {
		t.Fatalf("original CurrentPhase mutated: %q", state.CurrentPhase)
	}
	if state.PhaseOutputs["tech_design"] != "design doc" {
		t.Fatalf("original PhaseOutputs mutated: %#v", state.PhaseOutputs)
	}
	if state.GateResults["tech_design"].PhaseID != "tech_design" {
		t.Fatalf("original GateResults mutated: %#v", state.GateResults["tech_design"])
	}
}

func TestEmitDocUpdateUsesExplicitPhaseIDWithoutContentInference(t *testing.T) {
	dir := t.TempDir()
	adapter := NewGUIWorkflowAdapter(&App{}, nil)
	adapter.SetWorkingDir("u1", dir)

	requirementsContent := "# Technical Design\n\nThis title must not move the document."
	if err := adapter.EmitDocUpdate("u1", "requirements", requirementsContent); err != nil {
		t.Fatalf("EmitDocUpdate requirements failed: %v", err)
	}
	designContent := "# Tasks\n\nThis title must not move the document either."
	if err := adapter.EmitDocUpdate("u1", "design", designContent); err != nil {
		t.Fatalf("EmitDocUpdate design failed: %v", err)
	}

	workflowDir := filepath.Join(dir, ".maclaw", "workflow")
	requirementsPath := filepath.Join(workflowDir, workflowPhaseFileName("requirements"))
	designPath := filepath.Join(workflowDir, workflowPhaseFileName("design"))
	tasksPath := filepath.Join(workflowDir, workflowPhaseFileName("tasks"))

	gotRequirements, err := os.ReadFile(requirementsPath)
	if err != nil {
		t.Fatalf("requirements doc was not persisted at explicit phase path: %v", err)
	}
	if string(gotRequirements) != requirementsContent {
		t.Fatalf("requirements doc content = %q, want %q", string(gotRequirements), requirementsContent)
	}
	gotDesign, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("design doc was not persisted at explicit phase path: %v", err)
	}
	if string(gotDesign) != designContent {
		t.Fatalf("design doc content = %q, want %q", string(gotDesign), designContent)
	}
	if _, err := os.Stat(tasksPath); !os.IsNotExist(err) {
		t.Fatalf("tasks doc should not be created from content heading, stat err=%v", err)
	}
}

func TestGUIWorkflowAdapterSetWorkingDirDoesNotCreateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "project")
	adapter := NewGUIWorkflowAdapter(&App{}, nil)

	adapter.SetWorkingDir("u1", dir)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("SetWorkingDir must not create project directory, stat err=%v path=%s", err, dir)
	}
	if got := adapter.GetWorkingDir(); got != dir {
		t.Fatalf("GetWorkingDir() = %q, want %q", got, dir)
	}
}

func TestGUIWorkflowAdapterUsesWorkflowStateProjectPathForDocs(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	currentProject := t.TempDir()
	workflowProject := t.TempDir()
	if err := app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "current", Name: "Current", Path: currentProject}},
		CurrentProject: "current",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	adapter := NewGUIWorkflowAdapter(app, engine)
	engine.SetCallbacks(adapter)

	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{ProjectPath: workflowProject})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != workflowProject {
		t.Fatalf("adapter working dir = %q, want workflow project %q", got, workflowProject)
	}
	content := "# Requirements\n\nUse workflow project path."
	if err := adapter.EmitDocUpdate("u1", workflow.PhaseCodingRequirements, content); err != nil {
		t.Fatalf("EmitDocUpdate() error = %v", err)
	}

	wantPath := filepath.Join(workflowProject, ".maclaw", "workflow", state.ID, workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if got, err := os.ReadFile(wantPath); err != nil || string(got) != content {
		t.Fatalf("workflow doc not persisted under workflow project, content=%q err=%v path=%s", string(got), err, wantPath)
	}
	wrongPath := filepath.Join(currentProject, ".maclaw", "workflow", state.ID, workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Fatalf("workflow doc should not persist under current app project, stat err=%v path=%s", err, wrongPath)
	}
}

func TestGUIWorkflowAdapterMissingWorkflowProjectPersistsDocsInternally(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	workflowProject := filepath.Join(t.TempDir(), "new-project")
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	adapter := NewGUIWorkflowAdapter(app, engine)
	engine.SetCallbacks(adapter)

	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{ProjectPath: workflowProject})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	content := "# Requirements\n\nUse internal storage until coding agent creates the project."
	if err := adapter.EmitDocUpdate("u1", workflow.PhaseCodingRequirements, content); err != nil {
		t.Fatalf("EmitDocUpdate() error = %v", err)
	}

	projectDocPath := filepath.Join(workflowProject, ".maclaw", "workflow", state.ID, workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(projectDocPath); !os.IsNotExist(err) {
		t.Fatalf("missing workflow project must not be created for docs, stat err=%v path=%s", err, projectDocPath)
	}
	internalDocPath := filepath.Join(app.GetDataDir(), "workflow", sanitizeWorkflowPhaseFileStem(state.ID), workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if got, err := os.ReadFile(internalDocPath); err != nil || string(got) != content {
		t.Fatalf("workflow doc not persisted internally, content=%q err=%v path=%s", string(got), err, internalDocPath)
	}
	if got := adapter.readPersistedDoc(workflow.PhaseCodingRequirements); got != content {
		t.Fatalf("readPersistedDoc() = %q, want %q", got, content)
	}
}

func TestGUIWorkflowAdapterCompletionPublishesInternalDocsAfterProjectCreated(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	workflowProject := filepath.Join(t.TempDir(), "new-project")
	engine := workflow.NewWorkflowEngine(workflow.NewWorkflowRegistry(), nil, nil, nil)
	adapter := NewGUIWorkflowAdapter(app, engine)
	engine.SetCallbacks(adapter)

	state, err := engine.StartWorkflowWithOptions("u1", workflow.StructuredIntent{Category: workflow.WorkflowCoding}, workflow.WorkflowStartOptions{ProjectPath: workflowProject})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions() error = %v", err)
	}
	adapter.workflowStartDate = time.Date(2026, 6, 9, 10, 0, 0, 0, time.Local)
	content := "# Requirements\n\nPublish once coding agent creates the project."
	if err := adapter.EmitDocUpdate("u1", workflow.PhaseCodingRequirements, content); err != nil {
		t.Fatalf("EmitDocUpdate() error = %v", err)
	}
	internalDocPath := filepath.Join(app.GetDataDir(), "workflow", sanitizeWorkflowPhaseFileStem(state.ID), workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(workflowProject); !os.IsNotExist(err) {
		t.Fatalf("planning doc persistence must not create project root, stat err=%v path=%s", err, workflowProject)
	}
	if err := os.MkdirAll(workflowProject, 0o755); err != nil {
		t.Fatalf("MkdirAll(workflowProject) error = %v", err)
	}

	state.Status = workflow.WorkflowCompleted
	state.PhaseOutputs[workflow.PhaseCodingRequirements] = content
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(completed) error = %v", err)
	}

	wantPath := filepath.Join(workflowProject, "docs", "workflow", "coding", "2026-06-09", workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if got, err := os.ReadFile(wantPath); err != nil || string(got) != content {
		t.Fatalf("completion should publish internal phase doc to project storage, content=%q err=%v path=%s", string(got), err, wantPath)
	}
	if _, err := os.Stat(internalDocPath); !os.IsNotExist(err) {
		t.Fatalf("completion should clean internal fallback after project publish, stat err=%v path=%s", err, internalDocPath)
	}
}

func TestGUIWorkflowAdapterClearsStaleWorkingDirForNewWorkflowWithoutProjectPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	firstProject := filepath.Join(t.TempDir(), "first-project")
	currentProject := t.TempDir()
	if err := app.SaveConfig(corelib.AppConfig{
		Projects:       []corelib.ProjectConfig{{Id: "current", Name: "Current", Path: currentProject}},
		CurrentProject: "current",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	adapter := NewGUIWorkflowAdapter(app, nil)

	if err := adapter.EmitPhaseUpdate("u1", &workflow.WorkflowState{ID: "wf-first", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding, ProjectPath: firstProject}); err != nil {
		t.Fatalf("EmitPhaseUpdate(first) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != firstProject {
		t.Fatalf("first working dir = %q, want %q", got, firstProject)
	}
	if err := adapter.EmitPhaseUpdate("u1", &workflow.WorkflowState{ID: "wf-second", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding}); err != nil {
		t.Fatalf("EmitPhaseUpdate(second) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != "" {
		t.Fatalf("new workflow without ProjectPath must clear stale working dir, got %q", got)
	}

	content := "# Requirements\n\nNo stale project path."
	if err := adapter.EmitDocUpdate("u1", workflow.PhaseCodingRequirements, content); err != nil {
		t.Fatalf("EmitDocUpdate() error = %v", err)
	}
	stalePath := filepath.Join(firstProject, ".maclaw", "workflow", "wf-second", workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("doc should not persist under stale workflow project, stat err=%v path=%s", err, stalePath)
	}
	wrongPath := filepath.Join(currentProject, ".maclaw", "workflow", "wf-second", workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Fatalf("active workflow without ProjectPath should not fall back to current project, stat err=%v path=%s", err, wrongPath)
	}
}

func TestGUIWorkflowAdapterDoesNotUseUnpreparedWorkflowProjectPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	staleProject := filepath.Join(t.TempDir(), "stale-project")
	invalidProject := filepath.Join(t.TempDir(), "project-file")
	if err := os.WriteFile(invalidProject, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	adapter := NewGUIWorkflowAdapter(app, nil)

	if err := adapter.EmitPhaseUpdate("u1", &workflow.WorkflowState{ID: "wf-first", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding, ProjectPath: staleProject}); err != nil {
		t.Fatalf("EmitPhaseUpdate(first) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != staleProject {
		t.Fatalf("first working dir = %q, want %q", got, staleProject)
	}
	if err := adapter.EmitPhaseUpdate("u1", &workflow.WorkflowState{ID: "wf-second", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding, ProjectPath: invalidProject}); err != nil {
		t.Fatalf("EmitPhaseUpdate(second) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != "" {
		t.Fatalf("invalid workflow ProjectPath must clear stale working dir, got %q", got)
	}

	content := "# Requirements\n\nInvalid project path should not reuse stale dir."
	if err := adapter.EmitDocUpdate("u1", workflow.PhaseCodingRequirements, content); err != nil {
		t.Fatalf("EmitDocUpdate() error = %v", err)
	}
	stalePath := filepath.Join(staleProject, ".maclaw", "workflow", "wf-second", workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("doc should not persist under stale workflow project after invalid ProjectPath, stat err=%v path=%s", err, stalePath)
	}
	invalidPath := filepath.Join(invalidProject, ".maclaw", "workflow", "wf-second", workflowPhaseFileName(workflow.PhaseCodingRequirements))
	if _, err := os.Stat(invalidPath); !os.IsNotExist(err) {
		t.Fatalf("doc should not persist under invalid workflow ProjectPath, stat err=%v path=%s", err, invalidPath)
	}
}

func TestGUIWorkflowAdapterWorkflowStateProjectPathChangeInvalidatesProjectStorage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	firstProject := t.TempDir()
	secondProject := t.TempDir()
	adapter := NewGUIWorkflowAdapter(app, nil)
	state := &workflow.WorkflowState{ID: "wf-same", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding, ProjectPath: firstProject}

	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(first) error = %v", err)
	}
	firstStorage := adapter.resolveProjectStorageDir()
	if firstStorage == "" || !strings.HasPrefix(firstStorage, filepath.Join(firstProject, "docs", "workflow")) {
		t.Fatalf("first project storage dir = %q, want under %q", firstStorage, firstProject)
	}
	state.ProjectPath = secondProject
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(second) error = %v", err)
	}
	secondStorage := adapter.resolveProjectStorageDir()
	if secondStorage == "" || !strings.HasPrefix(secondStorage, filepath.Join(secondProject, "docs", "workflow")) {
		t.Fatalf("second project storage dir = %q, want under %q", secondStorage, secondProject)
	}
	if secondStorage == firstStorage {
		t.Fatalf("project storage dir cache was not invalidated: %q", secondStorage)
	}
}

func TestGUIWorkflowAdapterWorkflowStateEmptyProjectPathClearsWorkingDirForSameWorkflow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	project := filepath.Join(t.TempDir(), "workflow-project")
	adapter := NewGUIWorkflowAdapter(app, nil)
	state := &workflow.WorkflowState{ID: "wf-same", Status: workflow.WorkflowActive, Type: workflow.WorkflowCoding, ProjectPath: project}

	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(first) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != project {
		t.Fatalf("working dir = %q, want %q", got, project)
	}
	state.ProjectPath = ""
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(clear) error = %v", err)
	}
	if got := adapter.GetWorkingDir(); got != "" {
		t.Fatalf("same workflow empty ProjectPath should clear working dir, got %q", got)
	}
}

func TestGUIWorkflowAdapterCompletionUsesWorkflowStateProjectPathForManifest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	workflowProject := t.TempDir()
	adapter := NewGUIWorkflowAdapter(app, nil)
	adapter.activeWorkflowID = "wf-complete"
	adapter.activeWorkflowType = workflow.WorkflowCoding
	adapter.workflowStartDate = time.Date(2026, 5, 31, 9, 0, 0, 0, time.Local)

	state := &workflow.WorkflowState{
		ID:          "wf-complete",
		Status:      workflow.WorkflowCompleted,
		Type:        workflow.WorkflowCoding,
		ProjectPath: workflowProject,
		PhaseOutputs: map[string]string{
			workflow.PhaseCodingRequirements: "requirements",
		},
	}
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(completed) error = %v", err)
	}

	manifestPath := filepath.Join(workflowProject, "docs", "workflow", "coding", "2026-05-31", "workflow-manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("completion manifest should use workflow state ProjectPath, stat err=%v path=%s", err, manifestPath)
	}
}

func TestGUIWorkflowAdapterCompletionInvalidProjectPathDoesNotUseStaleWorkingDir(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	staleProject := filepath.Join(t.TempDir(), "stale-project")
	invalidProject := filepath.Join(t.TempDir(), "project-file")
	if err := os.WriteFile(invalidProject, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	adapter := NewGUIWorkflowAdapter(app, nil)
	adapter.workingDir = staleProject
	adapter.activeWorkflowID = "wf-complete"
	adapter.activeWorkflowType = workflow.WorkflowCoding
	adapter.workflowStartDate = time.Date(2026, 5, 31, 9, 0, 0, 0, time.Local)

	state := &workflow.WorkflowState{
		ID:          "wf-complete",
		Status:      workflow.WorkflowCompleted,
		Type:        workflow.WorkflowCoding,
		ProjectPath: invalidProject,
		PhaseOutputs: map[string]string{
			workflow.PhaseCodingRequirements: "requirements",
		},
	}
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(completed) error = %v", err)
	}

	staleManifestPath := filepath.Join(staleProject, "docs", "workflow", "coding", "2026-05-31", "workflow-manifest.json")
	if _, err := os.Stat(staleManifestPath); !os.IsNotExist(err) {
		t.Fatalf("completion with invalid ProjectPath should not write manifest under stale dir, stat err=%v path=%s", err, staleManifestPath)
	}
}

func TestGUIWorkflowAdapterCompletionInvalidProjectPathClearsCachedProjectStorage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	staleProject := t.TempDir()
	invalidProject := filepath.Join(t.TempDir(), "project-file")
	if err := os.WriteFile(invalidProject, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	adapter := NewGUIWorkflowAdapter(app, nil)
	adapter.workingDir = staleProject
	adapter.activeWorkflowID = "wf-complete"
	adapter.activeWorkflowType = workflow.WorkflowCoding
	adapter.workflowStartDate = time.Date(2026, 5, 31, 9, 0, 0, 0, time.Local)
	staleStorage := adapter.resolveProjectStorageDir()
	if staleStorage == "" {
		t.Fatal("expected initial cached project storage dir")
	}
	adapter.workingDir = ""

	state := &workflow.WorkflowState{
		ID:          "wf-complete",
		Status:      workflow.WorkflowCompleted,
		Type:        workflow.WorkflowCoding,
		ProjectPath: invalidProject,
		PhaseOutputs: map[string]string{
			workflow.PhaseCodingRequirements: "requirements",
		},
	}
	if err := adapter.EmitPhaseUpdate("u1", state); err != nil {
		t.Fatalf("EmitPhaseUpdate(completed) error = %v", err)
	}

	staleManifestPath := filepath.Join(staleStorage, "workflow-manifest.json")
	if _, err := os.Stat(staleManifestPath); !os.IsNotExist(err) {
		t.Fatalf("completion with invalid ProjectPath should clear cached project storage, stat err=%v path=%s", err, staleManifestPath)
	}
}

func TestWorkflowPhaseFileNameUsesStableLocalizedKeys(t *testing.T) {
	tests := []struct {
		phaseID string
		want    string
	}{
		{phaseID: "requirements", want: "01-requirements.md"},
		{phaseID: "tech_design", want: "02-technical-design.md"},
		{phaseID: "task_breakdown", want: "03-task-breakdown.md"},
		{phaseID: "ops_intake", want: "01-ops-intake.md"},
		{phaseID: "Needs 文档", want: "needs.md"},
	}

	for _, tt := range tests {
		got := workflowPhaseFileName(tt.phaseID)
		if got != tt.want {
			t.Fatalf("workflowPhaseFileName(%q) = %q, want %q", tt.phaseID, got, tt.want)
		}
		if strings.ContainsAny(got, "需求技术任务文档设计拆分计划") {
			t.Fatalf("workflow phase file name should not contain localized display text: %q", got)
		}
	}
}

func TestWorkflowPhaseKindFromMetadataUsesEnum(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   workflowPhaseKind
	}{
		{name: "phase wins", values: []string{"tech_design", "requirements"}, want: workflowPhaseKind(workflowPhaseDesign)},
		{name: "doc type fallback", values: []string{"", "task_plan"}, want: workflowPhaseKind(workflowPhaseTasks)},
		{name: "localized display text rejected", values: []string{"需求文档"}, want: workflowPhaseUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workflowPhaseKindFromMetadata(tt.values...); got != tt.want {
				t.Fatalf("workflowPhaseKindFromMetadata(%q) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestWorkflowPhaseFileNameWithExt(t *testing.T) {
	tests := []struct {
		name    string
		phaseID string
		ext     string
		want    string
	}{
		{name: "pdf extension", phaseID: "requirements", ext: ".pdf", want: "01-requirements.pdf"},
		{name: "uppercase extension normalized", phaseID: "tech_design", ext: ".PDF", want: "02-technical-design.pdf"},
		{name: "extension without dot", phaseID: "task_breakdown", ext: "docx", want: "03-task-breakdown.docx"},
		{name: "no extension keeps markdown", phaseID: "tasks", ext: "", want: "03-task-breakdown.md"},
		{name: "localized extension ignored", phaseID: "requirements", ext: ".文档", want: "01-requirements.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workflowPhaseFileNameWithExt(tt.phaseID, tt.ext); got != tt.want {
				t.Fatalf("workflowPhaseFileNameWithExt(%q, %q) = %q, want %q", tt.phaseID, tt.ext, got, tt.want)
			}
		})
	}
}

func TestWorkflowPhaseFileNameUnknownPhaseFallbackIsASCII(t *testing.T) {
	got := workflowPhaseFileName("未命名 阶段")
	if got != "workflow-phase.md" {
		t.Fatalf("workflowPhaseFileName unknown localized phase = %q, want workflow-phase.md", got)
	}
	if strings.ContainsAny(got, "未命名阶段") {
		t.Fatalf("unknown phase fallback should not contain localized text: %q", got)
	}
}
