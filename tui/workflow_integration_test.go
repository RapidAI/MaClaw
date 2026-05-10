package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
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
		response: `{"intent":{"category":"none","summary":"short","ready":true},"reply":"","ready":true}`,
	}
	app := newWorkflowTestApp(llm)

	if got := app.handleWorkflowInterception("x"); got != "" {
		t.Fatalf("handleWorkflowInterception(x) = %q, want pass-through", got)
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
		response: `{"intent":{"category":"none","summary":"weather","ready":true},"reply":"","ready":true}`,
	}
	app := newWorkflowTestApp(llm)

	if got := app.handleWorkflowInterception("weather query"); got != "" {
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
		response: `{"intent":{"category":"contract_review","summary":"review contract","goals":["find risks"],"confidence":0.9,"ready":true},"reply":"Please confirm starting contract review.","ready":true}`,
	})

	first := app.handleWorkflowInterception("review this contract")
	if !strings.Contains(first, "Please confirm starting contract review.") {
		t.Fatalf("first response = %q, want understanding reply", first)
	}
	if app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("workflow should wait for ready confirmation after Start creates understanding session")
	}

	started := app.handleWorkflowInterception("start")
	if started == "" {
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
		Summary:  "contract review",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if state == nil {
		t.Fatal("StartWorkflow returned nil state")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "contract.txt")
	if err := os.WriteFile(path, []byte("contract body"), 0o644); err != nil {
		t.Fatalf("write temp attachment: %v", err)
	}

	expanded := app.expandWorkflowAttachmentInput("tui-user", "review \""+path+"\"")
	if !strings.Contains(expanded, "本地文件路径") || !strings.Contains(expanded, path) {
		t.Fatalf("expanded input = %q, want explicit attachment path context", expanded)
	}
}

func TestClearCommandCancelsUnderstandingSession(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"review contract","confidence":0.8,"ready":false},"reply":"Please upload the contract.","ready":false}`,
	})
	if got := app.handleWorkflowInterception("review contract"); got == "" {
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

func TestTUIOpsMaintenanceControlledPhaseFiltersTools(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	state, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	if tmpl == nil {
		t.Fatal("ops maintenance template is not registered")
	}
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}
	if state.CurrentPhase != "controlled_execution" {
		t.Fatal("controlled_execution phase not found")
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`

	tools := []map[string]interface{}{
		agent.ToolDef("bash", "run shell", nil, nil),
		agent.ToolDef("ssh", "remote shell", nil, nil),
		agent.ToolDef("read_file", "read file", nil, nil),
		agent.ToolDef("task", "spawn task", nil, nil),
		agent.ToolDef("create_session", "create session", nil, nil),
		agent.ToolDef("edit_file", "edit file", nil, nil),
	}
	got := agent.FilterToolDefinitionsByAuthorizer(&tuiCallbacks{app: app}, tools)

	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[tooldef.Name(def)] = true
	}
	for _, name := range []string{"bash", "ssh", "read_file"} {
		if !names[name] {
			t.Fatalf("expected %s to remain allowed; got names=%v", name, names)
		}
	}
	for _, name := range []string{"task", "create_session", "edit_file"} {
		if names[name] {
			t.Fatalf("expected %s to be filtered out; got names=%v", name, names)
		}
		if app.isWorkflowToolAllowedTUI(name) {
			t.Fatalf("expected execution guard to block %s", name)
		}
	}
	allowed, reason := (&tuiCallbacks{app: app}).IsToolCallAllowed("bash", `{"command":"rm -rf / --no-preserve-root"}`)
	if allowed {
		t.Fatal("expected high-risk command arguments to be rejected")
	}
	if !strings.Contains(reason, "reviewed runbook") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
	delete(state.PhaseOutputs, "risk_policy")
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("bash", `{"command":"systemctl restart nginx"}`)
	if allowed {
		t.Fatal("expected mutating command without approved manifest to be rejected")
	}
	if !strings.Contains(reason, "allowed_commands") {
		t.Fatalf("unexpected missing-manifest rejection reason: %q", reason)
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("bash", `{"command":"systemctl restart mysql"}`)
	if allowed {
		t.Fatal("expected command outside approved manifest to be rejected")
	}
	if !strings.Contains(reason, "approved risk-policy") {
		t.Fatalf("unexpected manifest rejection reason: %q", reason)
	}
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("bash", `{"command":"systemctl   restart   nginx"}`)
	if !allowed {
		t.Fatalf("expected approved command to pass, got %q", reason)
	}
	delete(state.PhaseOutputs, "risk_policy")
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("ssh", `{"action":"upload","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected ssh upload without approved manifest to be rejected")
	}
	if !strings.Contains(reason, "allowed_commands") {
		t.Fatalf("unexpected ssh upload rejection reason: %q", reason)
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
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("ssh", `{"action":"upload","session_id":"prod-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if !allowed {
		t.Fatalf("expected approved ssh upload to pass, got %q", reason)
	}
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("ssh", `{"action":"upload","session_id":"prod-session","local_path":"other.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected ssh upload outside approved manifest to be rejected")
	}
	allowed, reason = (&tuiCallbacks{app: app}).IsToolCallAllowed("ssh", `{"action":"upload","session_id":"staging-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected ssh upload to wrong target to be rejected")
	}
}
