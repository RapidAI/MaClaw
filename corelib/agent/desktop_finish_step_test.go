package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type finishGateCallbacks struct {
	mockCallbacks
	needs bool
	armed bool
	calls []string
}

func (g *finishGateCallbacks) DesktopNeedsFinish() bool { return g.needs }
func (g *finishGateCallbacks) ArmDesktopFinishStep()    { g.armed = true }
func (g *finishGateCallbacks) DesktopFinishArmed() (bool, string) {
	if !g.armed {
		return false, ""
	}
	return true, "desktop"
}
func (g *finishGateCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	g.calls = append(g.calls, name+" "+args)
	if strings.Contains(args, `"finish"`) {
		g.needs = false
		g.armed = false
	}
	return ToolExecutionResult{Result: "finish recorded", Outcome: ToolExecutionOutcomeOK}
}

func TestForcedFinishStepUsesOnlyThatTool(t *testing.T) {
	var choices []string
	var enums []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ToolChoice interface{}              `json:"tool_choice"`
			Tools      []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(payload.ToolChoice)
		choices = append(choices, string(raw))
		enum := ""
		if len(payload.Tools) == 1 {
			fn, _ := payload.Tools[0]["function"].(map[string]interface{})
			params, _ := fn["parameters"].(map[string]interface{})
			props, _ := params["properties"].(map[string]interface{})
			action, _ := props["action"].(map[string]interface{})
			encoded, _ := json.Marshal(action["enum"])
			enum = string(encoded)
		}
		enums = append(enums, enum)
		switch len(choices) {
		case 1:
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"做好了"},"finish_reason":"stop"}]}`)
		case 2:
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-finish","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"finish\",\"outcome\":\"done\",\"text\":\"做好了\"}"}}]},"finish_reason":"tool_calls"}]}`)
		default:
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"做好了"},"finish_reason":"stop"}]}`)
		}
	}))
	defer server.Close()
	desktop := tooldef.BuildToolDef("desktop", "desktop", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []interface{}{"bash", "finish"}},
		},
	})
	cb := &finishGateCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:    4,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{desktop, tooldef.BuildToolDef("bash", "bash", map[string]interface{}{"type": "object"})},
		toolResult: "finish recorded",
	}, needs: true}
	result := RunLoop(cb, "做完告诉我", nil, server.Client())
	if result.Error != "" || result.Text != "做好了" {
		t.Fatalf("result=%+v", result)
	}
	if len(choices) < 2 || choices[1] == "null" || !strings.Contains(choices[1], "desktop") {
		t.Fatalf("tool choices %#v", choices)
	}
	if enums[1] != `["finish"]` {
		t.Fatalf("finish enum %q choices %#v", enums[1], choices)
	}
	if len(cb.calls) != 1 || !strings.Contains(cb.calls[0], "finish") {
		t.Fatalf("calls %#v", cb.calls)
	}
}
