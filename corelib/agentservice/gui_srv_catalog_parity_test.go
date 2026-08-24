package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSrvAdvertisesSharedCoreCapabilities(t *testing.T) {
	cb := fullyWiredSharedCatalogCallbacks(t)
	caps := cb.toolCapabilities()
	advertised := map[string]AgentToolCapability{}
	for _, cap := range caps {
		advertised[cap.Name] = cap
	}
	for _, name := range agent.SharedCoreCapabilityNames() {
		cap, ok := advertised[name]
		if !ok {
			t.Errorf("srv catalog missing shared core capability %s", name)
			continue
		}
		if !cap.Enabled {
			t.Errorf("srv capability %s should be enabled under equivalent host wiring; reason=%q", name, cap.DisabledReason)
		}
	}
	for _, name := range agent.ExtraSharedHostCapabilityNames() {
		cap, ok := advertised[name]
		if !ok {
			t.Errorf("srv catalog missing extra shared capability %s", name)
			continue
		}
		if !cap.Enabled {
			t.Errorf("srv extra capability %s should be enabled under equivalent host wiring; reason=%q", name, cap.DisabledReason)
		}
	}
}

func TestSrvSharedToolsExecuteThroughShippedExecutor(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("hello shared core"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mem, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("memory store: %v", err)
	}

	skillProvider := &installCaptureSkillProvider{}
	mcpProvider := listMCPProvider{entries: []MCPToolEntry{{
		ServerID: "srv-1", ServerName: "demo", ToolName: "ping", Description: "ping the demo server",
	}}}
	cb := &coreAgentCallbacks{
		ctx:               context.Background(),
		principal:         Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:         root,
		knowledgeStore:    store,
		memory:            mem,
		skillProvider:     skillProvider,
		mcpProvider:       mcpProvider,
		speechTranscriber: &fakeSpeechTranscriber{result: "transcribed hello"},
		speechSynthesizer: fakeCatalogSpeechSynthesizer{wav: []byte("RIFF....WAVEfmt ")},
		delegateSubtask: func(_ context.Context, _ Principal, task string) (string, error) {
			return "delegated: " + task, nil
		},
	}

	readOut := cb.ExecuteToolStructured("read_file", `{"path":"note.txt"}`)
	if readOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(readOut.Result, "hello shared core") {
		t.Fatalf("read_file = %#v", readOut)
	}
	missingRead := cb.ExecuteToolStructured("read_file", `{}`)
	if missingRead.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(missingRead.Result, "Error:") {
		t.Fatalf("read_file missing path should be an error, got %#v", missingRead)
	}
	taskOut := cb.ExecuteToolStructured("task", `{"action":"create"}`)
	if taskOut.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("task without store/title should be an error, got %#v", taskOut)
	}
	dirAsFile := cb.ExecuteToolStructured("FileRead", `{"path":"."}`)
	if dirAsFile.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("FileRead on a directory should be an error, got %#v", dirAsFile)
	}
	errNote := filepath.Join(root, "err.txt")
	if err := os.WriteFile(errNote, []byte("Error: not a tool failure\n"), 0o644); err != nil {
		t.Fatalf("write err note: %v", err)
	}
	errRead := cb.ExecuteToolStructured("read_file", `{"path":"err.txt"}`)
	if errRead.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(errRead.Result, "not a tool failure") {
		t.Fatalf("read_file must not treat file contents as tool failure, got %#v", errRead)
	}
	writeOut := cb.ExecuteToolStructured("write_file", `{"path":"out.txt","content":"written by srv"}`)
	if writeOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(writeOut.Result, "out.txt") {
		t.Fatalf("write_file = %#v", writeOut)
	}
	saved, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil || string(saved) != "written by srv" {
		t.Fatalf("write_file disk = %q err=%v", saved, err)
	}

	memOut := cb.ExecuteToolStructured("memory", `{"action":"list"}`)
	if memOut.Outcome != agent.ToolExecutionOutcomeOK || strings.TrimSpace(memOut.Result) == "" {
		t.Fatalf("memory = %#v", memOut)
	}

	saveKnowledge := cb.ExecuteToolStructured("knowledge_save_text", `{"text":"srv knowledge fact","title":"fact"}`)
	if saveKnowledge.Outcome != agent.ToolExecutionOutcomeOK || strings.HasPrefix(saveKnowledge.Result, "Error:") {
		t.Fatalf("knowledge_save_text = %#v", saveKnowledge)
	}
	searchKnowledge := cb.ExecuteToolStructured("knowledge_search", `{"query":"srv knowledge"}`)
	if searchKnowledge.Outcome != agent.ToolExecutionOutcomeOK || strings.HasPrefix(searchKnowledge.Result, "Error:") {
		t.Fatalf("knowledge_search = %#v", searchKnowledge)
	}
	exportOut := cb.ExecuteToolStructured("knowledge_export", `{"description":"srv catalog parity export","title":"srv export"}`)
	if exportOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(exportOut.Result, "knowledge-export-") {
		t.Fatalf("knowledge_export = %#v", exportOut)
	}
	matches, err := filepath.Glob(filepath.Join(root, "knowledge-export-*.knowledge.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("knowledge_export file = %v err=%v result=%#v", matches, err, exportOut)
	}
	exported, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	body := string(exported)
	if !strings.Contains(body, "maclaw.knowledge.package") || !strings.Contains(body, "srv knowledge fact") {
		t.Fatalf("knowledge_export contents = %q", body)
	}
	missingExport := cb.ExecuteToolStructured("knowledge_export", `{}`)
	if missingExport.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(missingExport.Result, "description") {
		t.Fatalf("knowledge_export without description = %#v", missingExport)
	}
	recordOut := cb.ExecuteToolStructured("record_audio", `{"title":"meeting"}`)
	if recordOut.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(recordOut.Result, "headless") {
		t.Fatalf("record_audio on srv must be an honest error, got %#v", recordOut)
	}

	skillOut := cb.ExecuteToolStructured("manage_skill", `{"action":"list"}`)
	if skillOut.Outcome != agent.ToolExecutionOutcomeOK || strings.Contains(skillOut.Result, "control-plane") {
		t.Fatalf("manage_skill list = %#v", skillOut)
	}

	mcpOut := cb.ExecuteToolStructured("list_mcp_tools", `{}`)
	if mcpOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(mcpOut.Result, "ping") {
		t.Fatalf("list_mcp_tools = %#v", mcpOut)
	}

	asrOut := cb.ExecuteToolStructured("asr", `{"path":"note.txt"}`)
	if asrOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(asrOut.Result, "transcribed hello") {
		t.Fatalf("asr = %#v", asrOut)
	}
	ttsOut := cb.ExecuteToolStructured("tts_render", `{"text":"hello"}`)
	if ttsOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(ttsOut.Result, "tts-render-") || !strings.Contains(ttsOut.Result, ".wav") {
		t.Fatalf("tts_render = %#v", ttsOut)
	}
	delegateOut := cb.ExecuteToolStructured("delegate_task", `{"request":"inspect repo"}`)
	if delegateOut.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(delegateOut.Result, "coding_workflow") {
		t.Fatalf("delegate_task catalog = %#v", delegateOut)
	}

	// web_fetch through the shipped executor: invalid URL is a real fetch
	// failure class, not a missing-route stub.
	fetchOut := cb.ExecuteToolStructured("web_fetch", `{"url":"not-a-url"}`)
	if fetchOut.Outcome != agent.ToolExecutionOutcomeOK && !strings.Contains(fetchOut.Result, "Error:") {
		t.Fatalf("web_fetch missing-route? %#v", fetchOut)
	}
	if strings.Contains(fetchOut.Result, "unknown tool") {
		t.Fatalf("web_fetch should be wired, got %#v", fetchOut)
	}
}

func TestDescribeCapabilitiesAdvertisesSharedHostTools(t *testing.T) {
	executor := &CoreAgentExecutor{
		ScheduleHandler:  func(map[string]interface{}) string { return "ok" },
		IMMessageHandler: func(map[string]interface{}) string { return "ok" },
		IMFileHandler:    func(map[string]interface{}) string { return "ok" },
		DelegateSubtask:  func(context.Context, Principal, string) (string, error) { return "ok", nil },
	}
	executor.SetSkillToolProvider(&installCaptureSkillProvider{})
	executor.SetMCPToolProvider(listMCPProvider{})
	caps, err := executor.DescribeCapabilities(context.Background(), ExecuteRequest{
		Principal: Principal{TenantID: "t", UserID: "u"},
		Instance:  Instance{Workspace: t.TempDir()},
		DataDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range caps.Tools {
		if tool.Enabled {
			seen[tool.Name] = true
		}
	}
	for _, name := range []string{"manage_skill", "delegate_task", "list_mcp_tools", "goal", "office"} {
		if !seen[name] {
			raw, _ := json.Marshal(caps.Tools)
			t.Fatalf("DescribeCapabilities missing enabled %s: %s", name, raw)
		}
	}
}

func fullyWiredSharedCatalogCallbacks(t *testing.T) *coreAgentCallbacks {
	t.Helper()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mem, err := memory.NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("memory store: %v", err)
	}
	return &coreAgentCallbacks{
		ctx:                        context.Background(),
		principal:                  Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:                  t.TempDir(),
		knowledgeStore:             store,
		memory:                     mem,
		goals:                      (&CoreAgentExecutor{}).goalStoreForDataDir(t.TempDir()),
		skillProvider:              &installCaptureSkillProvider{},
		mcpProvider:                importCapableMCPProvider{},
		scheduleHandler:            func(map[string]interface{}) string { return "ok" },
		imMessageHandler:           func(map[string]interface{}) string { return "ok" },
		imFileHandler:              func(map[string]interface{}) string { return "ok" },
		speechTranscriber:          &fakeSpeechTranscriber{result: "ok"},
		speechSynthesizer:          fakeCatalogSpeechSynthesizer{wav: []byte("wav")},
		delegateSubtask:            func(context.Context, Principal, string) (string, error) { return "ok", nil },
		allowLocalBash:             true,
		localBashTrustedSingleUser: true,
		localBashTenantID:          "tenant_a",
		localBashUserID:            "user_a",
		allowDirectSSH:             true,
		appCfg:                     corelib.AppConfig{SSHHosts: []corelib.SSHHostEntry{{Label: "prod", Host: "example.com", User: "root"}}},
	}
}

type listMCPProvider struct {
	entries []MCPToolEntry
}

type importCapableMCPProvider struct {
	listMCPProvider
}

func (importCapableMCPProvider) ImportMCPServers(context.Context, Principal, []MCPServerCreateInput) ([]string, error) {
	return []string{"imported"}, nil
}

func (p listMCPProvider) ListAvailableTools(context.Context, Principal) []MCPToolEntry {
	return p.entries
}

func (p listMCPProvider) CallTool(context.Context, Principal, string, string, map[string]interface{}) (string, error) {
	return "called", nil
}

type fakeCatalogSpeechSynthesizer struct{ wav []byte }

func (f fakeCatalogSpeechSynthesizer) Ready() bool { return true }

func (f fakeCatalogSpeechSynthesizer) RenderSpeech(context.Context, string) ([]byte, error) {
	return f.wav, nil
}

func TestSrvAdvertisesDesktopOnlyToolsAsHonestlyUnavailable(t *testing.T) {
	cb := fullyWiredSharedCatalogCallbacks(t)
	advertised := map[string]AgentToolCapability{}
	for _, cap := range cb.toolCapabilities() {
		advertised[cap.Name] = cap
	}
	for _, name := range []string{"screenshot", "open", "record_audio"} {
		cap, ok := advertised[name]
		if !ok {
			t.Errorf("desktop-only capability %s must stay visible in the srv catalog", name)
			continue
		}
		if cap.Enabled {
			t.Errorf("desktop-only capability %s must be disabled without a display adapter", name)
		}
		if strings.TrimSpace(cap.DisabledReason) == "" {
			t.Errorf("desktop-only capability %s needs an honest DisabledReason", name)
		}
	}
}

func TestSharedCatalogNamesComeFromRegisterCoreTools(t *testing.T) {
	// Guard against the test accidentally copying a frozen name list.
	names := agent.SharedCoreCapabilityNames()
	if len(names) < 10 {
		t.Fatalf("shared core catalog too small: %v", names)
	}
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	for _, name := range []string{"read_file", "write_file", "memory", "web_fetch", "manage_skill", "asr", "tts"} {
		if !seen[name] {
			t.Fatalf("RegisterCoreTools-derived catalog missing %s", name)
		}
	}
	if seen["screenshot"] || seen["open"] {
		t.Fatal("desktop-only names leaked into shared core catalog")
	}
}

func TestSrvManageSkillRunExecutesInstalledSkillThroughSkillToolBridge(t *testing.T) {
	svc := newStatusTestService(t)
	t.Cleanup(func() { _ = svc.Close() })
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	root, err := svc.ensureUserSkillsRoot(principal)
	if err != nil {
		t.Fatalf("ensureUserSkillsRoot: %v", err)
	}
	dir := filepath.Join(root, "echo-skill")
	if err := writeEntryToSkillDir(dir, corelib.NLSkillEntry{
		Name:        "echo-skill",
		Description: "echo marker for shipped run path",
		SkillDir:    dir,
		Status:      "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo srv-skill-ran"},
		}},
	}); err != nil {
		t.Fatalf("writeEntryToSkillDir: %v", err)
	}
	got, err := svc.GetSkill(context.Background(), principal, "echo-skill")
	if err != nil || got == nil || got.Name != "echo-skill" {
		t.Fatalf("GetSkill after persist: %#v err=%v", got, err)
	}
	bridge := NewSkillToolBridge(svc)
	cb := &coreAgentCallbacks{
		ctx:           context.Background(),
		principal:     principal,
		skillProvider: bridge,
		workspace:     t.TempDir(),
	}
	out := cb.ExecuteToolStructured("manage_skill", `{"action":"run","name":"echo-skill"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "srv-skill-ran") {
		t.Fatalf("manage_skill run through SkillToolBridge = %#v", out)
	}
}

func TestSrvDelegateTaskCodingWorkflowUsesCodingRuntime(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "implemented"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(llm.Close)
	executor := &CoreAgentExecutor{HTTPClient: llm.Client()}
	ledger := codingruntime.NewMemoryStore()
	executor.SetCodingRuntimeStore(ledger)
	svc, principal, inst := setupCaptureAgentService(t, executor)
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		MaclawLLMUrl: llm.URL, MaclawLLMKey: "test-key", MaclawLLMModel: "test-model",
	}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	cb := &coreAgentCallbacks{
		ctx:       context.Background(),
		principal: principal,
		instance:  inst,
		session:   Session{ID: "sess-delegate"},
		workspace: inst.Workspace,
		appCfg:    corelib.AppConfig{MaclawLLMUrl: llm.URL, MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"},
		executor:  executor,
	}
	out := cb.ExecuteToolStructured("delegate_task", `{"agent":"coding_workflow","request":"implement hello in the workspace"}`)
	if strings.Contains(out.Result, "delegated:") || strings.Contains(out.Result, "host adapter is not initialized") {
		t.Fatalf("coding_workflow must use the coding runtime, got %#v", out)
	}
	taskID := ""
	for _, part := range strings.Fields(out.Result) {
		if strings.HasPrefix(part, "coding_runtime_task_id=") {
			taskID = strings.TrimPrefix(part, "coding_runtime_task_id=")
			break
		}
	}
	if taskID == "" {
		t.Fatalf("coding_workflow did not return a coding runtime task id: %#v", out)
	}
	task, err := ledger.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask(%s): %v result=%#v", taskID, err, out)
	}
	if task.WorkflowID != "delegate_coding" || task.PhaseID != "implementation" {
		t.Fatalf("runtime task was not created by coding_workflow: %#v", task)
	}
	second := cb.ExecuteToolStructured("delegate_task", `{"agent":"coding_workflow","request":"implement a second change"}`)
	secondID := ""
	for _, part := range strings.Fields(second.Result) {
		if strings.HasPrefix(part, "coding_runtime_task_id=") {
			secondID = strings.TrimPrefix(part, "coding_runtime_task_id=")
			break
		}
	}
	if secondID == "" || secondID == taskID {
		t.Fatalf("second coding_workflow in the same session must be a new runtime task, first=%s second=%#v", taskID, second)
	}
	if _, err := ledger.GetTask(secondID); err != nil {
		t.Fatalf("GetTask(%s): %v result=%#v", secondID, err, second)
	}
}

func TestCodingWorkflowRejectsProjectPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	executor := &CoreAgentExecutor{}
	executor.SetCodingRuntimeStore(codingruntime.NewMemoryStore())
	cb := &coreAgentCallbacks{
		workspace: workspace,
		instance:  Instance{Workspace: workspace},
		executor:  executor,
	}
	out := cb.executeCodingWorkflowDelegate("implement hello", outside)
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "escapes") {
		t.Fatalf("project_path outside workspace = %#v", out)
	}
}

func TestCodingWorkflowRejectsNestedDelegate(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	attempt := &codingruntime.Attempt{AttemptID: "attempt-1"}
	cb := &coreAgentCallbacks{
		workspace:      t.TempDir(),
		runtimeStore:   store,
		runtimeAttempt: attempt,
	}
	out := cb.executeCodingWorkflowDelegate("implement nested", "")
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "nested coding_workflow") {
		t.Fatalf("nested coding_workflow = %#v", out)
	}
}

func TestSrvScreenshotUsesHostCapturer(t *testing.T) {
	root := t.TempDir()
	cb := &coreAgentCallbacks{
		ctx:             context.Background(),
		workspace:       root,
		desktopCapturer: &fakeHostDesktopCapturer{png: []byte("PNGDATA")},
	}
	out := cb.ExecuteToolStructured("screenshot", `{}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "screenshot-") {
		t.Fatalf("screenshot = %#v", out)
	}
	matches, err := filepath.Glob(filepath.Join(root, "screenshot-*.png"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("screenshot file = %v err=%v result=%#v", matches, err, out)
	}
	saved, err := os.ReadFile(matches[0])
	if err != nil || string(saved) != "PNGDATA" {
		t.Fatalf("screenshot bytes = %q err=%v", saved, err)
	}
}

func TestSrvOpenUsesHostURLLauncher(t *testing.T) {
	opener := &fakeHostURLOpener{}
	cb := &coreAgentCallbacks{
		ctx:         context.Background(),
		principal:   Principal{TenantID: "t", UserID: "u"},
		urlLauncher: opener,
	}
	out := cb.ExecuteToolStructured("open", `{"target":"https://example.com/docs"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || opener.rawURL == "" {
		t.Fatalf("open = %#v url=%q", out, opener.rawURL)
	}
}

func TestSrvImportMCPServersPersistsThroughBridge(t *testing.T) {
	svc := newStatusTestService(t)
	t.Cleanup(func() { _ = svc.Close() })
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	cb := &coreAgentCallbacks{
		ctx:         context.Background(),
		principal:   principal,
		mcpProvider: NewMCPToolBridge(svc),
	}
	cfgJSON, err := json.Marshal(map[string]interface{}{
		"json_config": `{"mcpServers":{"docs":{"url":"https://mcp.example.com/mcp"}}}`,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := cb.ExecuteToolStructured("import_mcp_servers", string(cfgJSON))
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "docs") {
		t.Fatalf("import_mcp_servers = %#v", out)
	}
	servers, err := svc.ListMCPServers(context.Background(), principal)
	if err != nil || len(servers) != 1 || servers[0].Name != "docs" || servers[0].Kind != "remote" {
		t.Fatalf("persisted MCP = %#v err=%v", servers, err)
	}
}

func TestSrvImportMCPServersRejectsDuplicateName(t *testing.T) {
	svc := newStatusTestService(t)
	t.Cleanup(func() { _ = svc.Close() })
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	cb := &coreAgentCallbacks{
		ctx:         context.Background(),
		principal:   principal,
		mcpProvider: NewMCPToolBridge(svc),
	}
	cfg := `{"json_config":"{\"mcpServers\":{\"docs\":{\"url\":\"https://mcp.example.com/mcp\"}}}"}`
	first := cb.ExecuteToolStructured("import_mcp_servers", cfg)
	if first.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("first import = %#v", first)
	}
	second := cb.ExecuteToolStructured("import_mcp_servers", cfg)
	if second.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(second.Result, "already exists") {
		t.Fatalf("duplicate import = %#v", second)
	}
	servers, err := svc.ListMCPServers(context.Background(), principal)
	if err != nil || len(servers) != 1 {
		t.Fatalf("after duplicate = %#v err=%v", servers, err)
	}
}

func TestManageSkillRunArgsKeepsNestedAndTopLevelParams(t *testing.T) {
	nested := manageSkillRunArgs(map[string]interface{}{
		"action": "run", "name": "demo", "args": map[string]interface{}{"project": "alpha"},
	})
	args, _ := nested["args"].(map[string]interface{})
	if args["project"] != "alpha" {
		t.Fatalf("nested args = %#v", nested)
	}
	top := manageSkillRunArgs(map[string]interface{}{
		"action": "run", "name": "demo", "project": "beta", "input": "hello",
	})
	if top["input"] != "hello" {
		t.Fatalf("input not preserved: %#v", top)
	}
	topArgs, _ := top["args"].(map[string]interface{})
	if topArgs["project"] != "beta" {
		t.Fatalf("top-level params = %#v", top)
	}
	if _, ok := topArgs["action"]; ok {
		t.Fatalf("control keys leaked into skill args: %#v", top)
	}
	encoded := manageSkillRunArgs(map[string]interface{}{
		"action": "run", "name": "demo", "args": `{"project":"gamma"}`,
	})
	encodedArgs, _ := encoded["args"].(map[string]interface{})
	if encodedArgs["project"] != "gamma" {
		t.Fatalf("JSON-string args = %#v", encoded)
	}
}

func TestIMMessageAndScheduleChineseErrorsAreFailures(t *testing.T) {
	cb := &coreAgentCallbacks{
		ctx: context.Background(),
		imMessageHandler: func(map[string]interface{}) string {
			return "缺少 text 参数（要发送的消息正文）"
		},
		scheduleHandler: func(map[string]interface{}) string {
			return "未知 manage_schedule action: nope（支持: create/list/delete/update/list_targets）"
		},
		imFileHandler: func(map[string]interface{}) string {
			return "发送失败: connection refused"
		},
		workspace: t.TempDir(),
	}
	msg := cb.ExecuteToolStructured("im_message", `{"action":"send"}`)
	if msg.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(msg.Result, "缺少") {
		t.Fatalf("im_message missing text = %#v", msg)
	}
	sched := cb.ExecuteToolStructured("manage_schedule", `{"action":"nope"}`)
	if sched.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(sched.Result, "未知 manage_schedule") {
		t.Fatalf("manage_schedule unknown action = %#v", sched)
	}
	note := filepath.Join(cb.workspace, "note.txt")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	send := cb.ExecuteToolStructured("send_file", `{"path":"note.txt"}`)
	if send.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(send.Result, "发送失败") {
		t.Fatalf("send_file handler failure = %#v", send)
	}
	createFail := toolTextResult("创建定时任务失败: db locked")
	if createFail.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("schedule create failure = %#v", createFail)
	}
	needID := toolTextResult("请提供 id 或 name 参数")
	if needID.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("schedule missing id = %#v", needID)
	}
	hour := toolTextResult("hour 必须在 0-23 之间")
	if hour.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("schedule hour range = %#v", hour)
	}
	emptyList := toolTextResult("当前没有定时任务")
	if emptyList.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("empty schedule list should be OK = %#v", emptyList)
	}
}

func TestMemoryMissingQueryIsError(t *testing.T) {
	cb := fullyWiredSharedCatalogCallbacks(t)
	out := cb.ExecuteToolStructured("memory", `{"action":"recall"}`)
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "missing query") {
		t.Fatalf("memory recall without query = %#v", out)
	}
	unknown := cb.ExecuteToolStructured("memory", `{"action":"explode"}`)
	if unknown.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(unknown.Result, "unknown memory action") {
		t.Fatalf("unknown memory action = %#v", unknown)
	}
}

func TestGoalUnknownActionIsError(t *testing.T) {
	cb := fullyWiredSharedCatalogCallbacks(t)
	out := cb.ExecuteToolStructured("goal", `{"action":"pause"}`)
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "未知 goal action") {
		t.Fatalf("unknown goal action = %#v", out)
	}
}

func TestCommandOutputToolResultDoesNotTreatStdoutAsFailure(t *testing.T) {
	if got := commandOutputToolResult("Error: printed by the script"); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("successful stdout starting with Error: = %#v", got)
	}
	if got := commandOutputToolResult("缺少 command 参数"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("missing command = %#v", got)
	}
	if got := commandOutputToolResult("hello\n[错误] 退出码: exit status 1"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("nonzero exit = %#v", got)
	}
	if got := commandOutputToolResult("[错误] 命令启动失败: exec"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("start failure = %#v", got)
	}
	if got := commandOutputToolResult("hello\n[错误] 命令超时（30 秒）"); got.Outcome != agent.ToolExecutionOutcomeTimeout {
		t.Fatalf("timeout = %#v", got)
	}
}

func TestSSHToolResultDoesNotTreatRemoteStdoutAsFailure(t *testing.T) {
	if got := sshToolResult("Error: remote process printed this"); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("remote stdout Error: = %#v", got)
	}
	if got := sshToolResult("错误: exec 需要 session_id 和 command 参数"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("tool-layer 错误: = %#v", got)
	}
	if got := sshToolResult("未知 SSH 操作: ping"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("unknown action = %#v", got)
	}
	if got := sshToolResult("SSH 会话已断开，自动重连失败: eof\n\n建议使用 ssh(action=close, session_id=s1) 关闭此会话，然后重新 connect"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("multiline disconnect = %#v", got)
	}
	if got := sshToolResult("连接已断开并自动重连\n发送命令失败: broken pipe"); got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("reconnect then send failure = %#v", got)
	}
	if got := sshToolResult("ls output\nError: not a tool failure"); got.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("multiline remote stdout = %#v", got)
	}
}

func TestIntArgParsesStringNumbers(t *testing.T) {
	if got := intArg(map[string]interface{}{"display": "2"}, "display", 0); got != 2 {
		t.Fatalf("string intArg = %d", got)
	}
	if got := intArg(map[string]interface{}{"display": "2.0"}, "display", 0); got != 2 {
		t.Fatalf("float-string intArg = %d", got)
	}
}

func TestBoolArgParsesJSONNumbersAndStrings(t *testing.T) {
	if got := boolArg(map[string]interface{}{"replace_all": true}, "replace_all", false); !got {
		t.Fatalf("bool true = %v", got)
	}
	if got := boolArg(map[string]interface{}{"replace_all": 1.0}, "replace_all", false); !got {
		t.Fatalf("float true = %v", got)
	}
	if got := boolArg(map[string]interface{}{"replace_all": 0.0}, "replace_all", true); got {
		t.Fatalf("float false = %v", got)
	}
	if got := boolArg(map[string]interface{}{"replace_all": "true"}, "replace_all", false); !got {
		t.Fatalf("string true = %v", got)
	}
	if got := boolArg(map[string]interface{}{"replace_all": "0"}, "replace_all", true); got {
		t.Fatalf("string false = %v", got)
	}
}

func TestSrvImportMCPServersRollsBackFailedBatch(t *testing.T) {
	svc := newStatusTestService(t)
	t.Cleanup(func() { _ = svc.Close() })
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	bridge := NewMCPToolBridge(svc)
	_, err := bridge.ImportMCPServers(context.Background(), principal, []MCPServerCreateInput{
		{Kind: "remote", Name: "ok-docs", EndpointURL: "https://mcp.example.com/mcp", AuthType: "none"},
		{Kind: "bogus", Name: "bad-docs"},
	})
	if err == nil {
		t.Fatal("expected second server to fail the batch")
	}
	servers, listErr := svc.ListMCPServers(context.Background(), principal)
	if listErr != nil {
		t.Fatalf("ListMCPServers: %v", listErr)
	}
	if len(servers) != 0 {
		t.Fatalf("failed batch must not leave a partial install, got %#v", servers)
	}
}

func TestSrvImportMCPServersWithoutBridgeIsHonestError(t *testing.T) {
	cb := &coreAgentCallbacks{ctx: context.Background()}
	out := cb.ExecuteToolStructured("import_mcp_servers", `{"json_config":"{\"mcpServers\":{\"docs\":{\"url\":\"https://mcp.example.com\"}}}"}`)
	if out.Outcome != agent.ToolExecutionOutcomeError || strings.Contains(out.Result, "json_config length=") {
		t.Fatalf("import without bridge = %#v", out)
	}
	if !strings.Contains(out.Result, "cannot persist") {
		t.Fatalf("import without bridge should be honest persist failure, got %#v", out)
	}
}

func TestParseMCPServerCreateInputsAcceptsAliases(t *testing.T) {
	inputs, err := parseMCPServerCreateInputs(`{"mcp_servers":{"fs":{"command":"npx","args":["-y","pkg"]}}}`, "")
	if err != nil || len(inputs) != 1 || inputs[0].Kind != "local" || inputs[0].Name != "fs" || inputs[0].Command != "npx" {
		t.Fatalf("parse local = %#v err=%v", inputs, err)
	}
	if len(inputs[0].Args) != 2 || inputs[0].Args[0] != "-y" || inputs[0].Args[1] != "pkg" {
		t.Fatalf("parse local args = %#v", inputs[0].Args)
	}
	stringArgs, err := parseMCPServerCreateInputs(`{"mcpServers":{"fs":{"command":"npx","args":"[\"-y\",\"pkg\"]"}}}`, "")
	if err != nil || len(stringArgs) != 1 || len(stringArgs[0].Args) != 2 || stringArgs[0].Args[1] != "pkg" {
		t.Fatalf("parse JSON-string args = %#v err=%v", stringArgs, err)
	}
	envInputs, err := parseMCPServerCreateInputs(`{"mcpServers":{"fs":{"command":"npx","env":{"PORT":8080,"DEBUG":true},"auto_start":"true"}}}`, "")
	if err != nil || len(envInputs) != 1 || envInputs[0].Env["PORT"] != "8080" || envInputs[0].Env["DEBUG"] != "true" || !envInputs[0].AutoStart {
		t.Fatalf("parse numeric env / string auto_start = %#v err=%v", envInputs, err)
	}
	kvInputs, err := parseMCPServerCreateInputs(`{"mcpServers":{"fs":{"command":"npx","env":["TOKEN=secret"]}}}`, "")
	if err != nil || len(kvInputs) != 1 || kvInputs[0].Env["TOKEN"] != "secret" {
		t.Fatalf("parse KEY=VAL env = %#v err=%v", kvInputs, err)
	}
	sseInputs, err := parseMCPServerCreateInputs(`{"mcpServers":{"wiki":{"type":"sse","serverUrl":"https://mcp.example.com/sse","args":["-p",8080]}}}`, "")
	if err != nil || len(sseInputs) != 1 || sseInputs[0].Kind != "remote" || sseInputs[0].EndpointURL != "https://mcp.example.com/sse" {
		t.Fatalf("parse SSE serverUrl = %#v err=%v", sseInputs, err)
	}
	if len(sseInputs[0].Args) != 2 || sseInputs[0].Args[1] != "8080" {
		t.Fatalf("parse numeric args = %#v", sseInputs[0].Args)
	}
	authInputs, err := parseMCPServerCreateInputs(`{"mcpServers":{"wiki":{"url":"https://mcp.example.com","apiKey":"secret-key","headers":["X-Debug: 1"]}}}`, "")
	if err != nil || len(authInputs) != 1 || authInputs[0].AuthType != "api_key" || authInputs[0].AuthSecret != "secret-key" || authInputs[0].Headers["X-Debug"] != "1" {
		t.Fatalf("parse apiKey / header list = %#v err=%v", authInputs, err)
	}
	argvInputs, err := parseMCPServerCreateInputs(`{"mcpServers":[{"name":"fs","command":["npx","-y","pkg"]}]}`, "")
	if err != nil || len(argvInputs) != 1 || argvInputs[0].Name != "fs" || argvInputs[0].Command != "npx" || len(argvInputs[0].Args) != 2 || argvInputs[0].Args[0] != "-y" || argvInputs[0].Args[1] != "pkg" {
		t.Fatalf("parse command argv / server list = %#v err=%v", argvInputs, err)
	}
	vsCode, err := parseMCPServerCreateInputs(`{"servers":{"fs":{"cmd":["npx","-y","pkg"],"autoStart":true}}}`, "")
	if err != nil || len(vsCode) != 1 || vsCode[0].Name != "fs" || vsCode[0].Command != "npx" || !vsCode[0].AutoStart || len(vsCode[0].Args) != 2 {
		t.Fatalf("parse VS Code servers/cmd/autoStart = %#v err=%v", vsCode, err)
	}
	envAlias, err := parseMCPServerCreateInputs(`{"mcpServers":{"fs":{"command":"npx","arguments":["-y"],"environment":{"TOKEN":"s"}}}}`, "")
	if err != nil || len(envAlias) != 1 || envAlias[0].Env["TOKEN"] != "s" || len(envAlias[0].Args) != 1 || envAlias[0].Args[0] != "-y" {
		t.Fatalf("parse environment/arguments aliases = %#v err=%v", envAlias, err)
	}
	inputs, err = parseMCPServerCreateInputs(`{"name":"wiki","url":"https://mcp.example.com"}`, "remote")
	if err != nil || len(inputs) != 1 || inputs[0].Kind != "remote" || inputs[0].EndpointURL != "https://mcp.example.com" {
		t.Fatalf("parse remote = %#v err=%v", inputs, err)
	}
}

func TestSrvImportMCPServersAcceptsObjectConfig(t *testing.T) {
	svc := newStatusTestService(t)
	t.Cleanup(func() { _ = svc.Close() })
	tenant, user := createStatusTestUser(t, svc)
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	cb := &coreAgentCallbacks{
		ctx:         context.Background(),
		principal:   principal,
		mcpProvider: NewMCPToolBridge(svc),
	}
	out := cb.executeImportMCPServers(map[string]interface{}{
		"json_config": map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"wiki": map[string]interface{}{
					"url": "https://mcp.example.com/mcp",
					"headers": map[string]interface{}{
						"Authorization": "Bearer secret-token",
					},
				},
			},
		},
	})
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "wiki") {
		t.Fatalf("object json_config = %#v", out)
	}
	servers, err := svc.ListMCPServers(context.Background(), principal)
	if err != nil || len(servers) != 1 || servers[0].AuthType != "bearer" || !servers[0].HasAuthSecret {
		t.Fatalf("imported auth = %#v err=%v", servers, err)
	}
}

func TestSrvEditLinesPreservesCRLF(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("a\r\nb\r\nc\r\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := &coreAgentCallbacks{ctx: context.Background(), workspace: root}
	out := cb.ExecuteToolStructured("edit_lines", `{"path":"note.txt","operation":"replace","start_line":2,"end_line":2,"content":"B"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("edit_lines = %#v", out)
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != "a\r\nB\r\nc\r\n" {
		t.Fatalf("CRLF preserved? got %q err=%v", saved, err)
	}
}

func TestDownloadFileNameStripsQuery(t *testing.T) {
	if got := downloadFileNameFromURL("https://example.com/files/report.pdf?token=abc#top"); got != "report.pdf" {
		t.Fatalf("download name = %q", got)
	}
	if got := sanitizePDFFileStem("Q2:report.pdf"); got != "Q2_report" {
		t.Fatalf("pdf stem = %q", got)
	}
}

func TestParseMCPServerCreateInputsAcceptsFencedJSON(t *testing.T) {
	raw := "```json\n{\"mcpServers\":{\"fs\":{\"command\":\"npx\"}}}\n```"
	inputs, err := parseMCPServerCreateInputs(raw, "")
	if err != nil || len(inputs) != 1 || inputs[0].Name != "fs" || inputs[0].Command != "npx" {
		t.Fatalf("fenced MCP JSON = %#v err=%v", inputs, err)
	}
	inputs, err = parseMCPServerCreateInputs(`"mcpServers":{"wiki":{"url":"https://mcp.example.com"}}`, "remote")
	if err != nil || len(inputs) != 1 || inputs[0].Name != "wiki" {
		t.Fatalf("fragment MCP JSON = %#v err=%v", inputs, err)
	}
	inputs, err = parseMCPServerCreateInputs("```json\n{\"mcpServers\":{\"fs\":{\"command\":\"npx\"}}}\n", "")
	if err != nil || len(inputs) != 1 || inputs[0].Name != "fs" {
		t.Fatalf("unclosed fenced MCP JSON = %#v err=%v", inputs, err)
	}
}

func TestSrvFileReadMissingPathIsError(t *testing.T) {
	cb := &coreAgentCallbacks{ctx: context.Background(), workspace: t.TempDir()}
	out := cb.ExecuteToolStructured("FileRead", `{}`)
	if out.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("missing FileRead path should be an error, got %#v", out)
	}
}

func TestSrvRipgrepMissingPatternIsError(t *testing.T) {
	cb := &coreAgentCallbacks{ctx: context.Background(), workspace: t.TempDir()}
	out := cb.ExecuteToolStructured("ripgrep", `{}`)
	if out.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("missing ripgrep pattern should be an error, got %#v", out)
	}
}

func TestSrvEditLinesAcceptsUpdateAlias(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := &coreAgentCallbacks{ctx: context.Background(), workspace: root}
	out := cb.ExecuteToolStructured("edit_lines", `{"path":"note.txt","operation":"update","start_line":"2","end_line":"2","content":"B"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("edit_lines update alias = %#v", out)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(saved), "B") {
		t.Fatalf("updated file = %q err=%v", saved, err)
	}
}

func TestSrvScreenshotRejectsNonPrimaryDisplay(t *testing.T) {
	cb := &coreAgentCallbacks{
		ctx:             context.Background(),
		workspace:       t.TempDir(),
		desktopCapturer: &fakeHostDesktopCapturer{png: []byte("PNG")},
	}
	out := cb.ExecuteToolStructured("screenshot", `{"display":2}`)
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "primary display") {
		t.Fatalf("screenshot display=2 = %#v", out)
	}
}

func TestSrvFileReadAcceptsFilePathAlias(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello alias"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cb := &coreAgentCallbacks{ctx: context.Background(), workspace: root}
	out := cb.ExecuteToolStructured("FileRead", `{"file_path":"note.txt"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "hello alias") {
		t.Fatalf("FileRead file_path alias = %#v", out)
	}
}

func TestSrvOpenFileURLUsesDocumentLauncher(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "note.pdf")
	if err := os.WriteFile(doc, []byte("%PDF"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	launcher := &fakeHostDocumentLauncher{}
	cb := &coreAgentCallbacks{
		ctx:              context.Background(),
		principal:        Principal{TenantID: "t", UserID: "u"},
		workspace:        root,
		documentLauncher: launcher,
	}
	fileURL := "file:///" + strings.ReplaceAll(filepath.ToSlash(doc), " ", "%20")
	out := cb.ExecuteToolStructured("open", `{"target":"`+fileURL+`"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("open file URL = %#v", out)
	}
	if launcher.absPath == "" {
		t.Fatalf("document launcher was not called: %#v", out)
	}
}
