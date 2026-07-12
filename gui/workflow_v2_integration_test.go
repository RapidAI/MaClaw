package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestPrepareWorkflowTemplatePanelLaunchStoresTemplateChoice(t *testing.T) {
	handler := &IMMessageHandler{}
	now := time.Unix(0, 123456789)

	launch, err := prepareWorkflowTemplatePanelLaunch(handler, "remote_coding_subagent", "/srv/app", "", now)
	if err != nil {
		t.Fatalf("prepareWorkflowTemplatePanelLaunch failed: %v", err)
	}
	if launch.UserID != desktopAIAssistantUserIDForProjectPath("") {
		t.Fatalf("user ID = %q, want desktop AI assistant user", launch.UserID)
	}
	if launch.RequestID != "desktop-ai-123456789-remote_coding_subagent" {
		t.Fatalf("request ID = %q", launch.RequestID)
	}
	if launch.ChoiceID != "template-123456789" {
		t.Fatalf("choice ID = %q, want template prefix", launch.ChoiceID)
	}
	choice, choiceID, ok := parseWorkflowChoiceCommand(launch.ChoiceCommand)
	if !ok || choice != workflowChoiceRemoteCoding || choiceID != launch.ChoiceID {
		t.Fatalf("choice command = %q parsed as (%q,%q,%v)", launch.ChoiceCommand, choice, choiceID, ok)
	}

	raw, ok := handler.pendingWorkflowChoice.Load(launch.UserID)
	if !ok {
		t.Fatal("expected pending workflow choice to be stored")
	}
	pending, ok := raw.(*pendingWorkflowChoice)
	if !ok || pending == nil {
		t.Fatalf("pending workflow choice type = %T", raw)
	}
	if pending.RouteResult == nil || pending.RouteResult.Target != v2.RouteToWorkflow || pending.RouteResult.WorkflowType != "remote_coding_subagent" {
		t.Fatalf("pending route = %#v, want remote_coding_subagent workflow route", pending.RouteResult)
	}
	if pending.RouteResult.ProjectPath != "/srv/app" {
		t.Fatalf("project path = %q", pending.RouteResult.ProjectPath)
	}
	if pending.ChoiceID != launch.ChoiceID {
		t.Fatalf("pending choice ID = %q, want %q", pending.ChoiceID, launch.ChoiceID)
	}
	if !strings.Contains(pending.Msg.Text, "remote_coding_subagent") {
		t.Fatalf("synthetic text should mention workflow type, got %q", pending.Msg.Text)
	}
}

func TestWorkflowTemplatePanelLaunchCodingSubAgentStartsSinglePhaseTemplate(t *testing.T) {
	wf := buildWorkflowV2State(v2.NewMemoryStore())
	handler := &IMMessageHandler{app: &App{workflowV2: wf}}
	now := time.Unix(0, 223456789)
	projectPath := t.TempDir()

	launch, err := prepareWorkflowTemplatePanelLaunch(handler, "coding_subagent", projectPath, "", now)
	if err != nil {
		t.Fatalf("prepareWorkflowTemplatePanelLaunch failed: %v", err)
	}
	choice, choiceID, ok := parseWorkflowChoiceCommand(launch.ChoiceCommand)
	if !ok || choice != workflowChoiceCodingSubAgent || choiceID != launch.ChoiceID {
		t.Fatalf("choice command = %q parsed as (%q,%q,%v), want coding_subagent", launch.ChoiceCommand, choice, choiceID, ok)
	}

	result := handler.handleCodingComplexityCommand(IMUserMessage{
		UserID: launch.UserID,
		Text:   launch.ChoiceCommand,
	}, launch.ChoiceCommand)
	if result == nil {
		t.Fatal("expected workflow choice to be handled")
	}

	active := wf.machine.GetActive(launch.UserID)
	if active == nil {
		t.Fatal("expected active workflow")
	}
	if active.Type != "coding_subagent" {
		t.Fatalf("active workflow type = %q, want coding_subagent", active.Type)
	}
	if len(active.Phases) != 1 {
		t.Fatalf("coding_subagent phase count = %d, want 1; phases=%#v", len(active.Phases), active.Phases)
	}
	if active.Phases[0].ID != "coding_subagent_execution" || active.Phases[0].ExecMode != v2.ExecModeSubAgent {
		t.Fatalf("phase = %#v, want coding_subagent_execution subagent phase", active.Phases[0])
	}
}

func TestPrepareWorkflowTemplatePanelLaunchRejectsNilHandler(t *testing.T) {
	if _, err := prepareWorkflowTemplatePanelLaunch(nil, "coding_subagent", "", "", time.Unix(0, 1)); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestPrepareWorkflowTemplatePanelLaunchRejectsBlankWorkflowType(t *testing.T) {
	if _, err := prepareWorkflowTemplatePanelLaunch(&IMMessageHandler{}, "  ", "", "", time.Unix(0, 1)); err == nil {
		t.Fatal("expected blank workflow type error")
	}
}

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

func TestRouteWithWorkflowV2_PresentationCreationDoesNotStartWorkflowImplicitly(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-presentation-explicit-hint"

	result := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   userID,
		Text:     "生成一份ppt，介绍北京。",
		Platform: "desktop",
	}, "生成一份ppt，介绍北京。")

	if result.Response != nil || result.WorkflowAgentLoop || result.WorkflowDocPhase || result.SkipNeedsConfirmGate {
		t.Fatalf("expected normal agent pass-through, got %#v", result)
	}
	if _, ok := handler.pendingWorkflowChoice.Load(userID); ok {
		t.Fatal("did not expect an implicit workflow choice")
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

func TestWorkflowV2DocUpdatePersistsPhaseDocument(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	projectPath := t.TempDir()
	state := &v2.WorkflowState{
		ID:          "wf-v2-persist-doc",
		UserID:      "test-v2-persist-doc",
		Type:        string(v2.WorkflowPresentationDesign),
		Status:      v2.StatusActive,
		ProjectPath: projectPath,
		Phases: []v2.Phase{{
			ID:     "outline",
			Name:   "Outline",
			Status: v2.PhaseWaitingConfirm,
		}},
	}

	handler.emitWorkflowV2Progress(state.UserID, state)
	handler.emitDocUpdateV2(state.UserID, "outline", "# Outline\n\nAuthoritative phase output")

	path := filepath.Join(projectPath, ".maclaw", "workflow", state.ID, workflowPhaseFileNameForTemplate(handler.app.workflowEngine.GetRegistry().Match(v2.WorkflowPresentationDesign), "outline"))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("V2 phase document was not persisted: %v", err)
	}
	if !strings.Contains(string(content), "Authoritative phase output") {
		t.Fatalf("persisted document = %q", content)
	}
}

func TestWorkflowV2GUIAdapterIsScopedPerUser(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	first := handler.workflowV2GUIAdapter("workflow-user-a")
	second := handler.workflowV2GUIAdapter("workflow-user-b")
	if first == nil || second == nil {
		t.Fatal("expected V2 GUI adapters")
	}
	if first == second {
		t.Fatal("V2 workflows for different users must not share adapter instance state")
	}
	if first != handler.workflowV2GUIAdapter("workflow-user-a") {
		t.Fatal("same user should keep the same V2 adapter")
	}
}

func TestWorkflowV2GUIAdapterIsReleasedOnTerminalState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-user-terminal"
	if handler.workflowV2GUIAdapter(userID) == nil {
		t.Fatal("expected V2 GUI adapter")
	}
	handler.emitWorkflowV2Progress(userID, &v2.WorkflowState{UserID: userID, Status: v2.StatusCompleted})
	if _, ok := handler.workflowV2Adapters.Load(userID); ok {
		t.Fatal("terminal workflow should release its V2 GUI adapter")
	}
}

func TestWorkflowV2PayloadOnlyReleasesTerminalAdapter(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-user-terminal-payload"
	if handler.workflowV2GUIAdapter(userID) == nil {
		t.Fatal("expected V2 GUI adapter")
	}
	handler.emitWorkflowV2ProgressPayloadOnly(userID, &v2.WorkflowState{UserID: userID, Status: v2.StatusCancelled})
	if _, ok := handler.workflowV2Adapters.Load(userID); ok {
		t.Fatal("terminal payload refresh should release its V2 GUI adapter")
	}
}

func TestRunWorkflowV2PhaseBlocksMissingFullDependency(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	state := &v2.WorkflowState{
		ID:           "wf-missing-dependency",
		UserID:       "test-missing-dependency",
		Type:         string(v2.WorkflowPresentationDesign),
		Status:       v2.StatusActive,
		ProjectPath:  t.TempDir(),
		CurrentPhase: 1,
		Phases: []v2.Phase{
			{ID: "outline", Status: v2.PhaseCompleted},
			{ID: "slide_scripting", DependsOnFull: []string{"outline"}, Status: v2.PhaseRunning},
		},
	}

	result := handler.runWorkflowV2Phase(state.UserID, state, "")
	if result.WorkflowAgentLoop || result.Response == nil || result.Response.Error == "" {
		t.Fatalf("missing dependency must block the agent loop: %#v", result)
	}
	if !strings.Contains(result.Response.Error, "outline") {
		t.Fatalf("blocker should identify missing dependency: %q", result.Response.Error)
	}
}

func TestWorkflowV2ArtifactPhaseUserRequestForbidsRediscovery(t *testing.T) {
	request := workflowV2PhaseUserRequest(&v2.Phase{
		Name:          "PPT 生成",
		Kind:          v2.PhaseKindArtifactGeneration,
		MutationScope: v2.MutationScopeArtifact,
	})
	for _, want := range []string{"不要询问主题、受众、页数或要点", "不要搜索项目目录、PDF、记忆或历史对话", "pptx-generator", ".pptx"} {
		if !strings.Contains(request, want) {
			t.Fatalf("artifact request missing %q: %s", want, request)
		}
	}
}

func TestWorkflowV2ArtifactPhaseModificationRemainsArtifactRequest(t *testing.T) {
	phase := &v2.Phase{Name: "PPT 生成", Kind: v2.PhaseKindArtifactGeneration, MutationScope: v2.MutationScopeArtifact}
	request := workflowV2PhaseUserRequest(phase) + "\n\n用户修改意见：改为深蓝色主题。直接重新生成并发送最终文件。"
	if strings.Contains(request, "完整文档") || !strings.Contains(request, "最终文件") {
		t.Fatalf("artifact modification request regressed to document generation: %s", request)
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
	// Use a single-phase template type so DependsOnFull backfill cannot block
	// the agent-loop route; the assertion here is workdir creation only.
	state := &v2.WorkflowState{
		ID:           "wf-test-create-workdir",
		UserID:       userID,
		Type:         string(v2.WorkflowPresentationDesign),
		Status:       v2.StatusActive,
		ProjectPath:  projectPath,
		CurrentPhase: 0,
		Phases: []v2.Phase{
			{
				ID:     "outline",
				Name:   "Outline",
				Status: v2.PhaseCompleted,
				Output: strings.Repeat("outline content ", 20),
			},
			{
				ID:     "slide_scripting",
				Name:   "Slide Scripting",
				Status: v2.PhaseCompleted,
				Output: strings.Repeat("script content ", 20),
			},
			{
				ID:            "ppt_generation",
				Name:          "PPT Generation",
				NeedsConfirm:  true,
				Status:        v2.PhaseRunning,
				DependsOnFull: []string{"outline", "slide_scripting"},
			},
		},
	}
	// Active phase index must point at ppt_generation.
	state.CurrentPhase = 2

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
