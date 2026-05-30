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
	}}, nil, nil, nil, "desktop-user")

	if !called {
		t.Fatal("expected bonus-round delegate_task(coding_workflow) to execute CodingSubAgent")
	}
	if result.Outcome != toolOutcomeSucceeded || result.Text != "bonus implemented" {
		t.Fatalf("result = %+v, want successful CodingSubAgent result", result)
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
