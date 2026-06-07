package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/RapidAI/CodeClaw/tui/commands"
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

func TestTUIWorkflowPendingStartAcceptsChineseControls(t *testing.T) {
	firstLLM := &tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"review contract","goals":["find risks"],"confidence":0.9,"ready":true},"reply":"Please confirm starting contract review.","ready":true}`,
	}
	app := newWorkflowTestApp(firstLLM)

	first := app.handleWorkflowInterception("review this contract")
	if !strings.Contains(first, "Please confirm starting contract review.") {
		t.Fatalf("first response = %q, want understanding reply", first)
	}

	started := app.handleWorkflowInterception("\u5f00\u59cb")
	if started == "" {
		t.Fatal("Chinese start command should start the pending workflow")
	}
	if !app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("Chinese start command did not start workflow")
	}

	cancelApp := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"review contract","goals":["find risks"],"confidence":0.9,"ready":true},"reply":"Please confirm starting contract review.","ready":true}`,
	})
	_ = cancelApp.handleWorkflowInterception("review this contract")
	cancelled := cancelApp.handleWorkflowInterception("\u53d6\u6d88")
	if cancelled == "" {
		t.Fatal("Chinese cancel command should return a cancellation response")
	}
	if cancelApp.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("Chinese cancel command should not start workflow")
	}
	cancelApp.workflowMu.Lock()
	pending := cancelApp.pendingWorkflowStart
	cancelApp.workflowMu.Unlock()
	if pending != nil {
		t.Fatal("Chinese cancel command should clear pending workflow start")
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
	if !strings.Contains(expanded, "local file path") || !strings.Contains(expanded, path) {
		t.Fatalf("expanded input = %q, want explicit attachment path context", expanded)
	}
}

func TestNeedleWorkflowReviewLogging(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	app.appConfig.LocalNeedleEnabled = true
	app.appConfig.LocalNeedleLogEnabled = true
	app.initNeedleRuntime()
	state, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "contract review",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs[state.CurrentPhase] = "draft output"
	state.PendingReviewPhaseID = state.CurrentPhase

	_ = app.handleWorkflowReviewTUI("tui-user", "looks good, continue")

	logDir := needledata.DefaultLogDir(commands.ResolveDataDir())
	files, err := filepath.Glob(filepath.Join(logDir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("expected needle event log in %s: files=%v err=%v", logDir, files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read needle log: %v", err)
	}
	var event needledata.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &event); err != nil {
		t.Fatalf("parse needle event: %v", err)
	}
	if event.Type != needledata.EventWorkflowReview || event.FinalDecision.Name == "" || !event.Privacy.Redacted {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestNeedleWorkflowReviewCanBypassLLM(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	app.appConfig.LocalNeedleEnabled = true
	app.initNeedleRuntime()
	app.llmConfig = corelib.MaclawLLMConfig{}
	state, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "contract review",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs[state.CurrentPhase] = strings.Repeat("draft output ", 20)
	state.PendingReviewPhaseID = state.CurrentPhase

	_ = app.handleWorkflowReviewTUI("tui-user", "looks good, continue")

	updated := app.workflowEngine.GetActiveWorkflow("tui-user")
	if updated == nil || updated.PendingReviewPhaseID != "" {
		t.Fatalf("Needle confirm should clear pending review, got %#v", updated)
	}
}

func TestLocalNeedleModelPathUsesActiveDataDirArtifact(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	activePath := needledata.DefaultModelDir(dataDir)
	if err := os.MkdirAll(activePath, 0o755); err != nil {
		t.Fatalf("mkdir active path: %v", err)
	}
	if got := (&TUIApp{}).localNeedleModelPath(); got != "" {
		t.Fatalf("localNeedleModelPath without manifest = %q, want empty", got)
	}
	if err := os.WriteFile(filepath.Join(activePath, "manifest.json"), []byte(`{"format":"maclaw-needle"}`), 0o644); err != nil {
		t.Fatalf("write active manifest: %v", err)
	}
	app := &TUIApp{}
	if got := app.localNeedleModelPath(); got != activePath {
		t.Fatalf("localNeedleModelPath = %q, want %q", got, activePath)
	}
	app.appConfig.LocalNeedleModelPath = filepath.Join(dataDir, "custom")
	if got := app.localNeedleModelPath(); got != app.appConfig.LocalNeedleModelPath {
		t.Fatalf("localNeedleModelPath custom = %q, want %q", got, app.appConfig.LocalNeedleModelPath)
	}
}

func TestLocalNeedleModelPathUsesCollectionRoot(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	activePath := needledata.DefaultModelDir(dataDir)
	workflowPath := filepath.Join(activePath, needledata.EventWorkflowReview)
	if err := os.MkdirAll(workflowPath, 0o755); err != nil {
		t.Fatalf("mkdir workflow artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowPath, "manifest.json"), []byte(`{"format":"maclaw-needle"}`), 0o644); err != nil {
		t.Fatalf("write workflow manifest: %v", err)
	}
	collection := map[string]any{
		"format": "maclaw-needle-collection",
		"tasks": map[string]any{
			needledata.EventWorkflowReview: map[string]any{"path": needledata.EventWorkflowReview},
		},
	}
	data, err := json.Marshal(collection)
	if err != nil {
		t.Fatalf("marshal collection: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activePath, "collection.json"), data, 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	if got := (&TUIApp{}).localNeedleModelPath(); got != activePath {
		t.Fatalf("localNeedleModelPath collection = %q, want %q", got, activePath)
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

func TestClearCommandClearsPendingWorkflowStart(t *testing.T) {
	llm := &tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"review contract","confidence":0.9,"ready":true},"reply":"Please confirm starting contract review.","ready":true}`,
	}
	app := newWorkflowTestApp(llm)
	if got := app.handleWorkflowInterception("review contract"); got == "" {
		t.Fatal("expected pending workflow start reply")
	}
	if app.pendingWorkflowStart == nil {
		t.Fatal("expected pending workflow start")
	}

	model := &tuiModel{app: app}
	model.handleSlashCommand("/clear")

	if app.pendingWorkflowStart != nil {
		t.Fatal("/clear should remove pending workflow start")
	}
	// The next input should not be consumed by the stale pending start. Make a
	// fresh IUM pass reject so any non-empty response would come from stale state.
	llm.response = `{"intent":{"category":"none","summary":"plain start","ready":true},"reply":"","ready":true}`
	if got := app.handleWorkflowInterception("start"); got != "" {
		t.Fatalf("stale pending workflow start should not consume post-clear input, got %q", got)
	}
	if app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("post-clear start command must not start stale pending workflow")
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
		agent.ToolDef("write_file", "write file", nil, nil),
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
	for _, name := range []string{"task", "write_file", "edit_file"} {
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

func TestTUIDocOnlyWorkflowPhaseBlocksImplementationTools(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	workflowType := workflow.WorkflowType("tui_doc_only_policy_boundary")
	app.workflowEngine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "tui doc only policy boundary",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{{
			ID:          "analysis",
			Name:        "Analysis",
			Prompt:      "write analysis",
			Deliverable: "analysis doc",
			ToolPolicy:  workflow.ToolFilterDocOnly,
		}},
	})
	_, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflowType,
		Summary:  "analyze project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	tools := []map[string]interface{}{
		agent.ToolDef("read_file", "read file", nil, nil),
		agent.ToolDef("list_directory", "list directory", nil, nil),
		agent.ToolDef("bash", "run shell", nil, nil),
		agent.ToolDef("write_file", "write file", nil, nil),
		agent.ToolDef("edit_file", "edit file", nil, nil),
		agent.ToolDef("task", "spawn task", nil, nil),
	}
	got := agent.FilterToolDefinitionsByAuthorizer(&tuiCallbacks{app: app}, tools)

	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[tooldef.Name(def)] = true
	}
	for _, name := range []string{"read_file", "list_directory"} {
		if !names[name] {
			t.Fatalf("expected %s to remain available for doc-only context; got %#v", name, names)
		}
	}
	for _, name := range []string{"bash", "write_file", "edit_file", "task"} {
		if names[name] {
			t.Fatalf("expected %s to be filtered out in doc-only phase; got %#v", name, names)
		}
		if app.isWorkflowToolAllowedTUI(name) {
			t.Fatalf("expected execution guard to block %s in doc-only phase", name)
		}
		allowed, reason := (&tuiCallbacks{app: app}).IsToolCallAllowed(name, `{}`)
		if allowed {
			t.Fatalf("expected concrete %s call to be blocked in doc-only phase", name)
		}
		if reason == "" {
			t.Fatalf("expected rejection reason for %s in doc-only phase", name)
		}
	}
}

func TestTUIWorkflowBlockedPhaseRejectsTools(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	_, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := app.workflowEngine.SkipPhaseForm("tui-user"); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if _, _, err := app.workflowEngine.SavePhaseOutputAndMaybeAdvance("tui-user", strings.Repeat("requirements\n", 80)); err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}
	if !app.workflowEngine.IsPhaseExecutionBlocked("tui-user") {
		t.Fatal("review gate should block phase execution")
	}
	if policy := app.currentWorkflowToolFilterTUI(); policy != workflow.ToolFilterDocOnly {
		t.Fatalf("active phase filter should remain doc-only while blocked, got %s", policy)
	}

	tools := []map[string]interface{}{
		agent.ToolDef("read_file", "read file", nil, nil),
		agent.ToolDef("write_file", "write file", nil, nil),
	}
	got := agent.FilterToolDefinitionsByAuthorizer(&tuiCallbacks{app: app}, tools)
	if len(got) != 0 {
		t.Fatalf("blocked workflow phase should expose no executable tools, got %v", got)
	}
	allowed, reason := (&tuiCallbacks{app: app}).IsToolCallAllowed("write_file", `{"path":"out.md","content":"body"}`)
	if allowed {
		t.Fatal("blocked workflow phase should reject tool calls")
	}
	if !strings.Contains(reason, "paused") {
		t.Fatalf("unexpected blocked reason: %q", reason)
	}
}

func TestTUIAuxiliaryCallbacksHonorWorkflowPolicy(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	if _, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := app.workflowEngine.SkipPhaseForm("tui-user"); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	callbacks := []struct {
		name string
		cb   interface {
			IsToolAllowed(string) bool
			IsToolCallAllowed(string, string) (bool, string)
		}
	}{
		{name: "scheduler", cb: &tuiSchedulerCallbacks{app: app}},
		{name: "pipe", cb: &pipeCallbacks{app: app}},
		{name: "weixin", cb: &tuiWeixinCallbacks{app: app}},
		{name: "loop_cycle", cb: &tuiLoopCycleCallbacks{parent: &tuiLoopCommandCallbacks{app: app}}},
	}
	for _, tc := range callbacks {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cb.IsToolAllowed("write_file") {
				t.Fatal("write_file should be blocked by doc-only workflow policy")
			}
			if !tc.cb.IsToolAllowed("read_file") {
				t.Fatal("read_file should remain available under doc-only workflow policy")
			}
			allowed, reason := tc.cb.IsToolCallAllowed("write_file", `{"path":"out.md","content":"body"}`)
			if allowed || !strings.Contains(reason, "not allowed") {
				t.Fatalf("write_file call = %v,%q; want policy rejection", allowed, reason)
			}
		})
	}
}

func TestTUIWorkflowTextOnlyInputSchemaWaitsForUserDetails(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"coding","summary":"build app","goals":["build app"],"confidence":0.9,"ready":true},"reply":"Please confirm starting coding workflow.","ready":true}`,
	})

	first := app.handleWorkflowInterception("build an app")
	if !strings.Contains(first, "Please confirm starting coding workflow.") {
		t.Fatalf("first response = %q, want confirmation prompt", first)
	}
	started := app.handleWorkflowInterception("start")
	if !strings.Contains(started, "1.") || !strings.Contains(strings.ToLower(started), "required") {
		t.Fatalf("start response should ask for numbered text input, got %q", started)
	}
	app.workflowMu.Lock()
	loopReady := app.workflowAgentLoop || app.pendingPhasePrompt != ""
	app.workflowMu.Unlock()
	if loopReady {
		t.Fatal("TUI should wait for text input before starting the first phase agent loop")
	}

	got := app.handleWorkflowInterception("1. Build a Go CLI\n2. Windows and Linux\n3. Include tests")
	if got != "" {
		t.Fatalf("text input should fall through to agent loop, got %q", got)
	}
	app.workflowMu.Lock()
	defer app.workflowMu.Unlock()
	if !app.workflowAgentLoop || strings.TrimSpace(app.pendingPhasePrompt) == "" {
		t.Fatalf("text input should arm workflow agent loop, loop=%v prompt=%q", app.workflowAgentLoop, app.pendingPhasePrompt)
	}
}

func TestTUIWorkflowAutoAdvanceIntoFormReturnsGuidance(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{})
	_, err := app.workflowEngine.StartWorkflow("tui-user", workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	guidance := app.applyWorkflowAutoAdvanceTUI("tui-user", &workflow.WorkflowResponse{
		Text:     "Advanced to input collection",
		ShowForm: true,
		FormSchema: &workflow.PhaseInputSchema{
			Title:  "Collect details",
			Fields: []workflow.PhaseInputField{{Name: "scope", Label: "Scope", Type: "text", Required: true}},
		},
	})
	if !strings.Contains(guidance, "Advanced to input collection") || !strings.Contains(guidance, "1.") {
		t.Fatalf("auto-advance into form should return visible text guidance, got %q", guidance)
	}
	app.workflowMu.Lock()
	defer app.workflowMu.Unlock()
	if app.workflowAgentLoop || strings.TrimSpace(app.pendingPhasePrompt) != "" {
		t.Fatalf("form-gated auto-advance must not arm agent loop, loop=%v prompt=%q", app.workflowAgentLoop, app.pendingPhasePrompt)
	}
}

func TestTUIWorkflowInputRequiredDoesNotArmPhaseLoopBeforeInput(t *testing.T) {
	app := newWorkflowTestApp(&tuiWorkflowTestLLM{
		response: `{"intent":{"category":"contract_review","summary":"review contract","goals":["review contract"],"confidence":0.9,"ready":true},"reply":"Please confirm starting contract review.","ready":true}`,
	})

	_ = app.handleWorkflowInterception("review this contract")
	started := app.handleWorkflowInterception("start")
	if !strings.Contains(started, "Please confirm starting contract review.") {
		t.Fatalf("start response missing confirmation prefix: %q", started)
	}
	if !app.workflowEngine.HasActiveWorkflow("tui-user") {
		t.Fatal("workflow should start and wait for required input")
	}
	app.workflowMu.Lock()
	defer app.workflowMu.Unlock()
	if app.workflowAgentLoop || strings.TrimSpace(app.pendingPhasePrompt) != "" {
		t.Fatalf("input-required workflow must not arm phase loop before input, loop=%v prompt=%q", app.workflowAgentLoop, app.pendingPhasePrompt)
	}
}
