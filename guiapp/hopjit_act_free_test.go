package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestHopPythonBodyUsesGovernedGUIProvider(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	caller := &guiHopJITCaller{
		handler:  handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
		invoke: func(context.Context, []interface{}, bool) (string, error) {
			t.Fatal("hop_python body unexpectedly called the model")
			return "", nil
		},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 确定性工具调用
Goal: body 使用受治理工具
Outputs:
- content % text
## Steps
1. [act] 读取
  - Tools: read_file
  + → content % text
  > `+"```hop_python"+`
  > content = read_file(path: "input.md")
  > `+"```"+`
`, nil, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["content"] != "file-content" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHopPythonBodyRejectsUndeclaredGUITool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	caller := &guiHopJITCaller{
		handler:  handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 越权工具调用
Goal: 未声明工具必须失败
Outputs:
- content % text
## Steps
1. [act] 读取
  - Tools: read_file
  + → content % text
  > `+"```hop_python"+`
  > content = write_file(path: "input.md")
  > `+"```"+`
`, nil, caller)
	if err != nil || result.Status != hopjit.NextFailed || result.Failure == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHopJITActFreeMissingAppReturnsError(t *testing.T) {
	caller := &guiHopJITCaller{handler: newHopJITActFreeTestHandler(t)}
	_, err := caller.executeActFree(context.Background(), hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}})
	if err == nil {
		t.Fatal("missing app did not return an error")
	}
}

func TestHopJITActFreeToolProviderUsesOnlyDeclaredTool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 {
		t.Fatalf("definitions=%d", len(definitions))
	}
	value, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err != nil || value != "file-content" {
		t.Fatalf("value=%v err=%v", value, err)
	}
	if _, err := provider.Execute(context.Background(), "write_file", map[string]any{}); err == nil {
		t.Fatal("undeclared tool was executed")
	}
}

func TestHopJITActFreeRunLoopExecutesToolAndReturnsJSON(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-read","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"input.md\"}"}}]},"finish_reason":"tool_calls"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"content\":\"file-content\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	callbacks := &hopJITActFreeCallbacks{
		ctx: context.Background(), cfg: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, tools: definitions, provider: provider,
	}
	result := agent.RunLoop(callbacks, "读取文件并返回 JSON", nil, server.Client())
	if result.Error != "" || result.Text != `{"content":"file-content"}` {
		t.Fatalf("result=%+v", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestHopJITActFreeRejectsMalformedToolArguments(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	callbacks := &hopJITActFreeCallbacks{ctx: context.Background(), tools: definitions, provider: provider}
	if got := callbacks.ExecuteTool("read_file", "{"); got == "file-content" {
		t.Fatal("malformed tool arguments were replaced with an empty object")
	}
}

func TestHopJITActFreeProviderExecutesDeclaredTool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	for range 2 {
		provider, _, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHopJITActFreeAppUsesDurableExecutionRecords(t *testing.T) {
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	handler := newHopJITActFreeTestHandler(t)
	handler.hopJITResults = newFileHopJITResultStore(t.TempDir())
	handler.app = &App{
		semanticExecutionCoordinator: coordinator,
		semanticPlanExecutionStore:   coordinator.Executions,
		semanticInvocationStore:      coordinator.Grants,
		semanticInvocationKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	var calls atomic.Int32
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		calls.Add(1)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	step := hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}}
	identity := hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"}
	provider, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
		t.Fatal(err)
	}
	record, err := coordinator.Executions.Execution(provider.Scope, provider.Plan.Selections[0].ID)
	if err != nil || record.State != tool.PlanExecutionSucceeded {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	restarted, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err == nil {
		t.Fatal("completed execution was invoked again")
	}
	step.Recovered = true
	recovered, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	value, err := recovered.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err != nil || value != "file-content" {
		t.Fatalf("recovered value=%v err=%v", value, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestHopPythonLoopToolsUseDistinctPersistentIterations(t *testing.T) {
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	handler := newHopJITActFreeTestHandler(t)
	handler.hopJITResults = newFileHopJITResultStore(t.TempDir())
	handler.app = &App{
		semanticExecutionCoordinator: coordinator,
		semanticPlanExecutionStore:   coordinator.Executions,
		semanticInvocationStore:      coordinator.Grants,
		semanticInvocationKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	var calls atomic.Int32
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		calls.Add(1)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	caller := &guiHopJITCaller{
		app: handler.app, handler: handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 循环工具作用域
Goal: 每轮独立执行
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect content into reports]
  + → reports % [text]
  1.1. [act] 读取
    - Tools: read_file
    + → content % text
    > `+"```hop_python"+`
    > content = read_file(path: "input.md")
    > `+"```"+`
`, map[string]any{"items": []string{"A", "B"}}, caller)
	reports, ok := result.Outputs["reports"].([]any)
	if err != nil || result.Status != hopjit.NextCompleted || calls.Load() != 2 || !ok || len(reports) != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestHopJITActFreeDoesNotProjectApprovalWaitAsSuccess(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		return databaseMutationApprovalWaitText
	}
	handler.registry.mu.Unlock()
	provider, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}}, hopJITExecutionIdentity{
		PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err == nil || value != nil {
		t.Fatalf("approval wait became a tool value: value=%v err=%v", value, err)
	}
}

func TestHopJITActFreeBindsToolIdentityPerCaller(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	owners := []string{}
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(args map[string]interface{}) string {
		owner, _ := args[registeredToolPolicyOwnerIDField].(string)
		owners = append(owners, owner)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	for _, identity := range []hopJITExecutionIdentity{
		{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
		{PrincipalID: "user-b", PolicyOwnerID: "owner-b", SessionID: "session-b", Platform: "desktop"},
	} {
		provider, _, err := handler.hopJITActFreeToolProvider(step, identity)
		if err != nil {
			t.Fatal(err)
		}
		if provider.Scope.PrincipalID != identity.PrincipalID || provider.Scope.SessionID != identity.SessionID {
			t.Fatalf("scope=%+v identity=%+v", provider.Scope, identity)
		}
		if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
			t.Fatal(err)
		}
		if len(owners) == 0 || owners[len(owners)-1] != identity.PolicyOwnerID {
			t.Fatalf("tool owners=%v identity=%+v", owners, identity)
		}
	}
}

func newHopJITActFreeTestHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	registry := NewToolRegistry()
	schema := map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
		"required": []string{"path"}, "additionalProperties": false,
	}
	if err := registry.Register(RegisteredTool{
		Name: "read_file", Status: RegToolAvailable, InputSchema: schema,
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(args map[string]interface{}) string { return "file-content" },
	}); err != nil {
		t.Fatal(err)
	}
	return &IMMessageHandler{registry: registry}
}
