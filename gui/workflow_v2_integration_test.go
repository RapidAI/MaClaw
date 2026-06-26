package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestInferExplicitWorkflowHint_Presentation(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "generate a ppt about Beijing", want: "presentation_design"},
		{text: "build a slide deck for the meeting", want: "presentation_design"},
		{text: "make a PowerPoint for the launch review", want: "presentation_design"},
		{text: "生成一份ppt，介绍北京", want: "presentation_design"},
		{text: "制作演示文稿用于项目汇报", want: "presentation_design"},
		{text: "open the ppt file and take a screenshot", want: ""},
		{text: "read this powerpoint and summarize it", want: ""},
		{text: "打开桌面上的ppt文件并截图", want: ""},
		{text: "读取这个演示文稿然后总结", want: ""},
		{text: "design a product strategy", want: ""},
	}
	for _, tc := range tests {
		if got := inferExplicitWorkflowHint(tc.text); got != tc.want {
			t.Errorf("inferExplicitWorkflowHint(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestRouteWithWorkflowV2_PresentationCreationUsesPresentationWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-presentation-explicit-hint"

	result := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   userID,
		Text:     "生成一份ppt，介绍北京。",
		Platform: "desktop",
	}, "生成一份ppt，介绍北京。")

	if result.Response == nil {
		t.Fatal("expected workflow confirmation response for explicit presentation request")
	}
	raw, ok := handler.pendingWorkflowChoice.Load(userID)
	if !ok {
		t.Fatal("expected pending workflow choice to be stored")
	}
	pending := raw.(*pendingWorkflowChoice)
	if pending.RouteResult == nil || pending.RouteResult.WorkflowType != "presentation_design" {
		t.Fatalf("workflow type = %#v, want presentation_design", pending.RouteResult)
	}
	if !strings.Contains(result.Response.Text, "PPT") {
		t.Fatalf("response text should mention presentation workflow, got %q", result.Response.Text)
	}
}

func TestRouteWithWorkflowV2_PresentationReadDoesNotStartWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	cases := []struct {
		userID string
		text   string
	}{
		{userID: "test-presentation-read-no-workflow-en", text: "open the ppt file and take a screenshot"},
		{userID: "test-presentation-read-no-workflow-zh", text: "打开桌面上的ppt文件并截图"},
	}

	for _, tc := range cases {
		result := handler.routeWithWorkflowV2(IMUserMessage{
			UserID:   tc.userID,
			Text:     tc.text,
			Platform: "desktop",
		}, tc.text)

		if result.Response != nil || result.WorkflowAgentLoop || result.WorkflowDocPhase || result.SkipNeedsConfirmGate {
			t.Fatalf("expected plain pass-through for non-creation ppt request %q, got %#v", tc.text, result)
		}
		if _, ok := handler.pendingWorkflowChoice.Load(tc.userID); ok {
			t.Fatalf("did not expect pending workflow choice for non-creation ppt request %q", tc.text)
		}
	}
}

func TestEnsureWorkflowV2PhaseWorkDirCreatesMissingProjectPath(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "presentation_design")
	state := &v2.WorkflowState{
		Type:         string(v2.WorkflowPresentationDesign),
		ProjectPath:  projectPath,
		CurrentPhase: 0,
		Phases: []v2.Phase{{
			ID:     "ppt_generation",
			Name:   "PPT Generation",
			Status: v2.PhaseRunning,
		}},
	}

	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("project path should start missing, stat err=%v", err)
	}
	if err := ensureWorkflowV2PhaseWorkDir(state); err != nil {
		t.Fatalf("ensureWorkflowV2PhaseWorkDir failed: %v", err)
	}
	info, err := os.Stat(projectPath)
	if err != nil {
		t.Fatalf("project path was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("project path is not a directory: %s", projectPath)
	}
}

func TestEnsureWorkflowV2PhaseWorkDirNormalizesProjectPath(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "presentation_design")
	state := &v2.WorkflowState{
		ProjectPath: projectPath + string(os.PathSeparator) + ".",
		Phases: []v2.Phase{{
			ID:     "ppt_generation",
			Status: v2.PhaseRunning,
		}},
	}

	if err := ensureWorkflowV2PhaseWorkDir(state); err != nil {
		t.Fatalf("ensureWorkflowV2PhaseWorkDir failed: %v", err)
	}
	if state.ProjectPath != filepath.Clean(projectPath) {
		t.Fatalf("ProjectPath = %q, want cleaned %q", state.ProjectPath, filepath.Clean(projectPath))
	}
}

func TestEnsureWorkflowV2PhaseWorkDirRejectsFilePath(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	state := &v2.WorkflowState{ProjectPath: filePath}

	err := ensureWorkflowV2PhaseWorkDir(state)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ensureWorkflowV2PhaseWorkDir error = %v, want not-a-directory error", err)
	}
}

func TestEnsureWorkflowV2PhaseWorkDirRejectsRelativePath(t *testing.T) {
	state := &v2.WorkflowState{ProjectPath: "relative-presentation-design"}

	err := ensureWorkflowV2PhaseWorkDir(state)
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("ensureWorkflowV2PhaseWorkDir error = %v, want not-absolute error", err)
	}
}

func TestRunWorkflowV2PhaseHandlesNilState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})

	result := handler.runWorkflowV2Phase("test-nil-state", nil, "")
	if result.Response == nil {
		t.Fatal("expected response for nil workflow state")
	}
	if !strings.Contains(result.Response.Error, "workflow state is nil") {
		t.Fatalf("response error = %q, want nil-state error", result.Response.Error)
	}
}

func TestRunWorkflowV2PhaseDoesNotCreateWorkDirBeforeFormSubmit(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-workflow-v2-form-no-workdir"
	projectPath := filepath.Join(t.TempDir(), "presentation_design")
	state := &v2.WorkflowState{
		ID:           "wf-test-form-no-workdir",
		UserID:       userID,
		Type:         string(v2.WorkflowPresentationDesign),
		Status:       v2.StatusActive,
		ProjectPath:  projectPath,
		CurrentPhase: 0,
		Phases: []v2.Phase{{
			ID:          "audience_goal",
			Name:        "Audience & Goal",
			InputSchema: &v2.PhaseInputSchema{Title: "Audience & Goal"},
			Status:      v2.PhaseRunning,
		}},
	}

	result := handler.runWorkflowV2Phase(userID, state, "")
	if result.Response == nil {
		t.Fatal("expected form response before agent loop")
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("form-only phase should not create project path before submission, stat err=%v", err)
	}
}

func TestRunWorkflowV2PhaseEnsuresProjectPathBeforeAgentLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-workflow-v2-create-phase-workdir"
	projectPath := filepath.Join(t.TempDir(), "presentation_design")
	state := &v2.WorkflowState{
		ID:           "wf-test-create-workdir",
		UserID:       userID,
		Type:         string(v2.WorkflowPresentationDesign),
		Status:       v2.StatusActive,
		ProjectPath:  projectPath,
		CurrentPhase: 0,
		Phases: []v2.Phase{{
			ID:           "ppt_generation",
			Name:         "PPT Generation",
			NeedsConfirm: true,
			Status:       v2.PhaseRunning,
		}},
	}

	result := handler.runWorkflowV2Phase(userID, state, "")
	if result.Response != nil {
		t.Fatalf("runWorkflowV2Phase response = %#v, want agent loop route", result.Response)
	}
	if !result.WorkflowAgentLoop {
		t.Fatalf("WorkflowAgentLoop = false, want true")
	}
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("workflow phase project path was not created: %v", err)
	}
}
