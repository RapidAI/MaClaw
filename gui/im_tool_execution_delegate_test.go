package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestExecuteAgentLoopDelegateTaskRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	var called bool
	var gotProject string
	var gotTask *TaskItem
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		gotProject = projectPath
		gotTask = task
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "implemented"}
	}

	h := &IMMessageHandler{}
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{Function: llm.ToolCallFunction{
			Name:      "delegate_task",
			Arguments: `{"agent":"coding_workflow","request":"create app in d:\\workprj\\testprj, write index.html"}`,
		}},
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
}

func TestToolDelegateTaskCodingWorkflowRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	called := false
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "direct implemented"}
	}

	h := &IMMessageHandler{}
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

	h := &IMMessageHandler{}
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

	h := &IMMessageHandler{}
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
