package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestHopJITActFreeToolProviderUsesOnlyDeclaredTool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step)
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
	provider, definitions, err := handler.hopJITActFreeToolProvider(step)
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
