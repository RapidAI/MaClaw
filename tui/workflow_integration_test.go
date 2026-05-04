package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type tuiWorkflowTestLLM struct {
	response string
	calls    int
}

func (m *tuiWorkflowTestLLM) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	m.calls++
	return m.response, nil
}

func newWorkflowTestApp(llm *tuiWorkflowTestLLM) *TUIApp {
	registry := workflow.NewWorkflowRegistry()
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, llm, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, &TUIWorkflowCallbacks{})
	return &TUIApp{
		llmConfig: corelib.MaclawLLMConfig{
			URL:   "http://127.0.0.1/test",
			Model: "test-model",
		},
		appConfig:      corelib.AppConfig{},
		history:        agent.NewConversationMemory(),
		workflowEngine: engine,
	}
}

func TestTUIWorkflowDoesNotStartFromKeywordFallback(t *testing.T) {
	llm := &tuiWorkflowTestLLM{
		response: `{"intent":{"category":"none","summary":"普通查询","ready":true},"reply":"","ready":true}`,
	}
	app := newWorkflowTestApp(llm)

	if got := app.handleWorkflowInterception("查"); got != "" {
		t.Fatalf("handleWorkflowInterception(查) = %q, want pass-through", got)
	}
	if llm.calls != 0 {
		t.Fatalf("single-character input should bypass workflow understanding, calls=%d", llm.calls)
	}
	if app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("single-character input must not start a workflow")
	}
}

func TestTUIWorkflowRejectedIntentFallsThrough(t *testing.T) {
	llm := &tuiWorkflowTestLLM{
		response: `{"intent":{"category":"none","summary":"普通查询","ready":true},"reply":"","ready":true}`,
	}
	app := newWorkflowTestApp(llm)

	if got := app.handleWorkflowInterception("查询北京天气"); got != "" {
		t.Fatalf("handleWorkflowInterception(weather) = %q, want pass-through", got)
	}
	if llm.calls != 1 {
		t.Fatalf("expected one understanding call for non-trivial text, calls=%d", llm.calls)
	}
	if app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("rejected intent must not start a workflow")
	}
}

func TestTUIWorkflowStartsAfterIntentUnderstandingReady(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"审查合同","goals":["识别风险"],"confidence":0.9,"ready":true},"reply":"可以开始合同审查。","ready":true}`,
	})

	first := app.handleWorkflowInterception("帮我审查一份合同")
	if !strings.Contains(first, "可以开始合同审查") {
		t.Fatalf("first response = %q, want understanding reply", first)
	}
	if app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("workflow should wait for ready confirmation after Start creates understanding session")
	}

	started := app.handleWorkflowInterception("开工")
	if !strings.Contains(started, "工作流已启动") || !strings.Contains(started, "合同审查") {
		t.Fatalf("ready response = %q, want workflow start overview", started)
	}
	if !app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("ready understanding should start workflow")
	}
}

func TestTUIWorkflowAttachmentPathExpansion(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	state, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "合同审查",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if state == nil {
		t.Fatal("StartWorkflow returned nil state")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "合同 样本.txt")
	if err := os.WriteFile(path, []byte("合同正文"), 0o644); err != nil {
		t.Fatalf("write temp attachment: %v", err)
	}

	expanded := app.expandWorkflowAttachmentInput("tui-user", "请看 \""+path+"\"")
	if !strings.Contains(expanded, "本地文件路径作为附件") || !strings.Contains(expanded, path) {
		t.Fatalf("expanded input = %q, want explicit attachment path context", expanded)
	}
}

func TestClearCommandCancelsUnderstandingSession(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"审查合同","confidence":0.8,"ready":false},"reply":"请补充合同类型。","ready":false}`,
	})
	if got := app.handleWorkflowInterception("帮我审查合同"); got == "" {
		t.Fatal("expected understanding reply")
	}
	if !app.workflowEngine.GetUnderstanding().HasActiveSession("tui-user") {
		t.Fatal("expected active understanding session")
	}

	model := &tuiModel{app: app}
	model.handleSlashCommand("/clear")

	if app.workflowEngine.GetUnderstanding().HasActiveSession("tui-user") {
		t.Fatal("/clear should cancel active understanding session")
	}
}
