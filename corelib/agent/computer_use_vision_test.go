package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type visionToolCallbacks struct {
	mockCallbacks
	images []ToolModelImage
}

func (m *visionToolCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	_ = callID
	res := m.ExecuteTool(name, args)
	return ToolExecutionResult{
		Result:      res,
		Outcome:     ToolExecutionOutcomeOK,
		ModelImages: m.images,
	}
}

func TestRunLoopAttachesComputerUseScreenshotForVisionModel(t *testing.T) {
	var sawImage atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		for _, msg := range req.Messages {
			if fmt.Sprint(msg["role"]) != "user" {
				continue
			}
			raw, _ := json.Marshal(msg["content"])
			if strings.Contains(string(raw), ComputerUseVisionImageMarker) &&
				strings.Contains(string(raw), "image_url") &&
				strings.Contains(string(raw), "abc123png") {
				sawImage.Store(true)
			}
		}
		if !sawImage.Load() {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_obs","type":"function","function":{"name":"computer_observe","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"clicked"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &visionToolCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL: server.URL, Model: "vision", Key: "k", SupportsVision: true,
			},
			maxIter:    3,
			sysPrompt:  "sys",
			tools:      []map[string]interface{}{tooldef.BuildToolDef("computer_observe", "Observe", map[string]interface{}{"type": "object"})},
			toolResult: "mode=vision_assist",
		},
		images: []ToolModelImage{{MIME: "image/png", Base64: "abc123png"}},
	}
	result := RunLoop(cb, "click the button", nil, server.Client())
	if result.Error != "" || result.Text != "clicked" {
		t.Fatalf("result=%+v", result)
	}
	if !sawImage.Load() {
		t.Fatal("expected computer_observe screenshot on the next LLM round")
	}
}

func TestRunLoopDropsComputerUseScreenshotWithoutVision(t *testing.T) {
	var requests atomic.Int32
	var sawImage atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, msg := range req.Messages {
			raw, _ := json.Marshal(msg["content"])
			if strings.Contains(string(raw), "abc123png") {
				sawImage.Store(true)
			}
		}
		if n == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_obs","type":"function","function":{"name":"computer_observe","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &visionToolCallbacks{
		mockCallbacks: mockCallbacks{
			config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "text", Key: "k", SupportsVision: false},
			maxIter:    3,
			sysPrompt:  "sys",
			tools:      []map[string]interface{}{tooldef.BuildToolDef("computer_observe", "Observe", map[string]interface{}{"type": "object"})},
			toolResult: "mode=text_primary",
		},
		images: []ToolModelImage{{MIME: "image/png", Base64: "abc123png"}},
	}
	result := RunLoop(cb, "observe", nil, server.Client())
	if result.Error != "" {
		t.Fatalf("error=%s", result.Error)
	}
	if sawImage.Load() {
		t.Fatal("text-only models must not receive computer_observe screenshots")
	}
}

func TestPruneComputerUseVisionImagesKeepsLatest(t *testing.T) {
	old := buildComputerUseVisionMessage("openai", []ToolModelImage{{Base64: "oldpng"}})
	convo := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
		old,
	}
	got := pruneComputerUseVisionImages(convo)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	msg := got[1].(map[string]interface{})
	text, _ := msg["content"].(string)
	if !strings.Contains(text, "previous screenshot omitted") {
		t.Fatalf("stale image not pruned: %#v", msg["content"])
	}
}
