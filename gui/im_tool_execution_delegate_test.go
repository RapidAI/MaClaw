package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func testHandlerWithDelegateGate(label intent.IntentLabel, workflowType string) *IMMessageHandler {
	uic := intent.New(intent.Config{
		LLMTimeout: time.Second,
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return fmt.Sprintf(`{"top":[{"skill":"%s","score":0.95,"workflow_type":"%s"}]}`, label, workflowType), nil
		},
	})
	return &IMMessageHandler{app: &App{unifiedClassifier: uic}}
}

func TestExecuteAgentLoopDelegateTaskRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	var called bool
	var tokenCallbackForwarded bool
	var gotProject string
	var gotTask *TaskItem
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		tokenCallbackForwarded = onToken != nil
		gotProject = projectPath
		gotTask = task
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "implemented"}
	}

	h := testHandlerWithDelegateGate(intent.LabelCoding, "coding")
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserText: "create app in d:\\workprj\\testprj, write index.html",
		ToolCall: llm.ToolCall{Function: llm.ToolCallFunction{
			Name:      "delegate_task",
			Arguments: `{"agent":"coding_workflow","request":"create app in d:\\workprj\\testprj, write index.html"}`,
		}},
		OnToken: func(string) {},
	})

	if !called {
		t.Fatal("expected delegate_task(coding_workflow) to execute CodingSubAgent")
	}
	if result.Outcome != toolOutcomeSucceeded || result.Text != "implemented" {
		t.Fatalf("result = %+v, want successful CodingSubAgent result", result)
	}
	if gotProject != `d:\workprj\testprj` {
		t.Fatalf("project = %q, want %q", gotProject, `d:\workprj\testprj`)
	}
	if gotTask == nil || gotTask.Description == "" {
		t.Fatalf("task = %#v, want populated delegated task", gotTask)
	}
	if gotTask.Index != 0 || taskDisplayNumber(gotTask) != 1 {
		t.Fatalf("delegated task numbering = index %d display T%d, want index 0 display T1", gotTask.Index, taskDisplayNumber(gotTask))
	}
	if !tokenCallbackForwarded {
		t.Fatal("expected delegate_task(coding_workflow) to forward token callback")
	}
}

func TestToolDelegateTaskCodingWorkflowRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "direct implemented"}
	}

	h := testHandlerWithDelegateGate(intent.LabelCoding, "coding")
	got := h.toolDelegateTask(map[string]interface{}{
		"agent":   "coding_workflow",
		"request": `create app in d:\workprj\testprj`,
	})

	if !called {
		t.Fatal("expected direct delegate_task(coding_workflow) to execute CodingSubAgent")
	}
	if got != "direct implemented" {
		t.Fatalf("delegate result = %q, want CodingSubAgent summary", got)
	}
}

func TestToolDelegateTaskCodingWorkflowBlockedByActiveDocOnlyWorkflow(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject direct delegate_task before CodingSubAgent starts")
		return nil
	}

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.app.unifiedClassifier = testHandlerWithDelegateGate(intent.LabelCoding, "coding").app.unifiedClassifier
	userID := "manual-delegate-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	h.lastUserID = userID

	got := h.toolDelegateTask(map[string]interface{}{
		"agent":                          "coding_workflow",
		"request":                        `create app in d:\workprj\testprj`,
		registeredToolPolicyOwnerIDField: userID,
	})
	if !strings.Contains(got, "not allowed by the current workflow phase") {
		t.Fatalf("delegate result = %q, want workflow phase rejection", got)
	}
}

func TestToolDelegateTaskCodingWorkflowBlocksBrowserPublicationContext(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("browser publication context must reject delegate_task before CodingSubAgent starts")
		return nil
	}

	h := testHandlerWithDelegateGate(intent.LabelBugFix, "")
	got := h.toolDelegateTask(map[string]interface{}{
		"agent":   "coding_workflow",
		"request": "log into Zhihu and publish a post; submit failed",
	})
	if !strings.Contains(got, "browser publication") {
		t.Fatalf("delegate result = %q, want browser publication rejection", got)
	}
}

func TestToolDelegateTaskCodingWorkflowBlocksBugFixWithoutCodeEvidence(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("bug-fix delegation without code evidence must reject before CodingSubAgent starts")
		return nil
	}

	h := testHandlerWithDelegateGate(intent.LabelBugFix, "")
	got := h.toolDelegateTask(map[string]interface{}{
		"agent":        "coding_workflow",
		"request":      "repair the failing submit path",
		"project_path": t.TempDir(),
	})
	if !strings.Contains(got, "no existing code project evidence") {
		t.Fatalf("delegate result = %q, want code evidence rejection", got)
	}
}

func TestToolDelegateTaskCodingWorkflowDoesNotInheritSingleActiveWorkflowPolicy(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "direct implemented"}
	}

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.app.unifiedClassifier = testHandlerWithDelegateGate(intent.LabelCoding, "coding").app.unifiedClassifier
	userID := "project-workflow-doc-only-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	got := h.toolDelegateTask(map[string]interface{}{
		"agent":   "coding_workflow",
		"request": `create app in d:\workprj\testprj`,
	})
	if !called {
		t.Fatalf("delegate_task without explicit owner must not inherit single active workflow policy, got %q", got)
	}
	if got != "direct implemented" {
		t.Fatalf("delegate result = %q, want CodingSubAgent summary", got)
	}
}

func TestBonusRoundDelegateTaskRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "bonus implemented"}
	}

	h := testHandlerWithDelegateGate(intent.LabelCoding, "coding")
	result := h.executeBonusRoundTool(llm.ToolCall{Function: llm.ToolCallFunction{
		Name:      "delegate_task",
		Arguments: `{"agent":"coding_workflow","request":"create app in d:\\workprj\\testprj"}`,
	}}, nil, nil, nil, "desktop-user", nil)

	if !called {
		t.Fatal("expected bonus-round delegate_task(coding_workflow) to execute CodingSubAgent")
	}
	if result.Outcome != toolOutcomeSucceeded || result.Text != "bonus implemented" {
		t.Fatalf("result = %+v, want successful CodingSubAgent result", result)
	}
}

func TestBonusRoundDelegateTaskBlockedByDocOnlyWorkflowPolicy(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject bonus-round delegate_task before CodingSubAgent starts")
		return nil
	}

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "bonus-doc-only-delegate-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	result := h.executeBonusRoundTool(llm.ToolCall{Function: llm.ToolCallFunction{
		Name:      "delegate_task",
		Arguments: `{"agent":"coding_workflow","request":"create app in d:\\workprj\\testprj"}`,
	}}, nil, nil, nil, userID, nil)

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected workflow policy rejection, got %+v", result)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Fatalf("expected not allowed text, got %q", result.Text)
	}
}

func TestBonusRoundWorkflowPolicyRejectsBeforeProgressAndTrajectory(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "bonus-doc-only-progress-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	progressEmitted := false
	recordedToolName := ""
	choice := llm.Choice{Message: llm.Message{ToolCalls: []llm.ToolCall{{ID: "call_write", Function: llm.ToolCallFunction{
		Name:      "write_file",
		Arguments: `{"path":"out.txt","content":"x"}`,
	}}}}}
	_, history, _ := h.applyBonusRoundChoice(nil, nil, choice, agentLoopBonusRoundOptions{
		UserID:            userID,
		Debug:             true,
		MilestoneTracker:  progress.NewAgentProgressTracker(nil, "build app", "coding", nil),
		InFlightLifecycle: &imInFlightLifecycle{},
		SendProgress:      func(string) { progressEmitted = true },
		RecordToolCall:    func(_ string, name string, _ string) { recordedToolName = name },
		RecordToolResult:  func(string, interface{}) {},
	})
	if progressEmitted {
		t.Fatal("bonus-round workflow policy rejection must happen before user-facing progress")
	}
	if recordedToolName != "" {
		t.Fatalf("bonus-round workflow policy rejection must happen before trajectory recording, got %q", recordedToolName)
	}
	if len(history) < 2 || history[len(history)-1].ToolOutcome != toolOutcomeFailed.String() {
		t.Fatalf("expected failed tool result in history, got %#v", history)
	}
}

func TestExtractCodingDelegateProjectPathWindows(t *testing.T) {
	got := extractCodingDelegateProjectPath(`create the app in d:\workprj\testprj, write index.html`)
	if got != `d:\workprj\testprj` {
		t.Fatalf("path = %q, want %q", got, `d:\workprj\testprj`)
	}
}

func TestExtractCodingDelegateProjectPathStopsAtWhitespaceAndChinesePunctuation(t *testing.T) {
	cases := map[string]string{
		"create in d:\\workprj\\spaceapp then write files": "d:\\workprj\\spaceapp",
		"target d:\\workprj\\cnapp\uFF0Cwrite page":        "d:\\workprj\\cnapp",
		"target d:\\workprj\\bracket\uFF09continue":        "d:\\workprj\\bracket",
	}
	for input, want := range cases {
		if got := extractCodingDelegateProjectPath(input); got != want {
			t.Fatalf("path = %q, want %q for input %q", got, want, input)
		}
	}
}

func TestExtractCodingDelegateProjectPathMissing(t *testing.T) {
	if got := extractCodingDelegateProjectPath("create a small web app here"); got != "" {
		t.Fatalf("path = %q, want empty", got)
	}
}

func TestListSubAgentsAdvertisesRealCodingWorkflowOnly(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.listSubAgents()
	if !strings.Contains(got, "coding_workflow") || !strings.Contains(got, "internal CodingSubAgent") {
		t.Fatalf("listSubAgents = %q, want real CodingSubAgent path listed", got)
	}
	if strings.Contains(got, "sub-agent active") || strings.Contains(got, "__SUBAGENT_CONTEXT__") {
		t.Fatalf("listSubAgents advertised fake context activation: %q", got)
	}
}

func TestToolDelegateTaskHelpContextIsNotActivation(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolDelegateTask(map[string]interface{}{
		"agent":   "help",
		"request": "how do I configure MaClaw?",
	})
	if !IsSubAgentContext(got) {
		t.Fatalf("help result = %q, want sub-agent context", got)
	}
	if strings.Contains(got, "sub-agent active") {
		t.Fatalf("help result used activation wording: %q", got)
	}
}

func TestToolDelegateTaskCodingWorkflowUsesProjectPathArgument(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	var gotProject string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		gotProject = projectPath
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "project path implemented"}
	}

	h := testHandlerWithDelegateGate(intent.LabelCoding, "coding")
	got := h.toolDelegateTask(map[string]interface{}{
		"agent":        "coding_workflow",
		"request":      "create app here",
		"project_path": `d:\workprj\explicit`,
	})

	if got != "project path implemented" {
		t.Fatalf("delegate result = %q, want CodingSubAgent summary", got)
	}
	if gotProject != `d:\workprj\explicit` {
		t.Fatalf("project = %q, want explicit project_path", gotProject)
	}
}

func TestExecuteAgentLoopDelegateTaskRejectsNonCodingIntent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "should not run"}
	}

	h := testHandlerWithDelegateGate(intent.LabelNonCoding, "")
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserText: "Cloudflare OAuth login is unavailable; any other operational options?",
		ToolCall: llm.ToolCall{Function: llm.ToolCallFunction{
			Name:      "delegate_task",
			Arguments: `{"agent":"coding_workflow","request":"create a Cloudflare login alternatives guide"}`,
		}},
	})

	if called {
		t.Fatal("non-coding request must not run CodingSubAgent")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("result = %+v, want policy rejection", result)
	}
	if !strings.Contains(result.Text, "semantically confirmed coding work") || !strings.Contains(result.Text, "non_coding") {
		t.Fatalf("rejection text = %q, want semantic non-coding reason", result.Text)
	}
}

func TestExecuteAgentLoopDelegateTaskRejectsWhenClassifierUnavailable(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "should not run"}
	}

	h := &IMMessageHandler{}
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserText: "create app in d:\\workprj\\testprj",
		ToolCall: llm.ToolCall{Function: llm.ToolCallFunction{
			Name:      "delegate_task",
			Arguments: `{"agent":"coding_workflow","request":"create app in d:\\workprj\\testprj"}`,
		}},
	})

	if called {
		t.Fatal("CodingSubAgent must fail closed when semantic classifier is unavailable")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("result = %+v, want policy rejection", result)
	}
	if !strings.Contains(result.Text, "classifier is unavailable") {
		t.Fatalf("rejection text = %q, want classifier unavailable reason", result.Text)
	}
}

func TestExecuteAgentLoopDelegateTaskRejectsDegradedCodingIntent(t *testing.T) {
	result := GateIntentResult{Intent: GateIntentBugFix, Confidence: 0.95, Layer: 2, Degraded: true, Reason: "embedding-only fallback"}

	if isSemanticCodingWorkflowDelegateResult(result) {
		t.Fatal("degraded coding intent must not be enough to run CodingSubAgent")
	}
}
