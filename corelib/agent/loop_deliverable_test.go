package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type deliverableGuardCallbacks struct {
	*mockCallbacks
	missing []string
}

func (c *deliverableGuardCallbacks) UnassignedDeliverables(string) []string {
	if c == nil || len(c.missing) == 0 {
		return nil
	}
	return append([]string(nil), c.missing...)
}

func (c *deliverableGuardCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	if name == "write_file" {
		c.missing = nil
	}
	return c.mockCallbacks.ExecuteToolStructured(name, args)
}

func TestRunLoopProseDoneLeavesDeclaredDeliverableUnassigned(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 && !strings.Contains(string(body), "report.pdf") {
			t.Errorf("deliverable nudge missing from follow-up request")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "已经做完"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()
	cb := &deliverableGuardCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   8,
		sysPrompt: "sys",
	}, missing: []string{"report.pdf"}}
	result := RunLoop(cb, "把报告写成文件", nil, nil)
	if result.Error != ErrDeliverableUnassigned || !result.HardExit {
		t.Fatalf("result=%+v", result)
	}
	if result.Text != "已经做完" {
		t.Fatalf("text=%q", result.Text)
	}
	if callCount != 2 {
		t.Fatalf("calls=%d", callCount)
	}
}

func TestRunLoopAssignedDeliverableAllowsProseStop(t *testing.T) {
	const report = "The report file is written and the section check passed."
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message":       map[string]interface{}{"role": "assistant", "content": "已经做完"},
					"finish_reason": "stop",
				}},
			}
		case 2:
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_write",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "write_file",
								"arguments": `{"path":"report.pdf"}`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			}
		default:
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message":       map[string]interface{}{"role": "assistant", "content": report},
					"finish_reason": "stop",
				}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &deliverableGuardCallbacks{
		mockCallbacks: &mockCallbacks{
			config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:    8,
			sysPrompt:  "sys",
			toolResult: "wrote report.pdf",
			tools:      []map[string]interface{}{tooldef.BuildToolDef("write_file", "write a file", map[string]interface{}{"type": "object"})},
		},
		missing: []string{"report.pdf"},
	}
	result := RunLoop(cb, "把报告写成文件", nil, nil)
	if result.Error != "" || result.HardExit {
		t.Fatalf("result=%+v", result)
	}
	if result.Text != report {
		t.Fatalf("text=%q", result.Text)
	}
	if len(cb.missing) != 0 {
		t.Fatal("deliverable still unassigned after write_file")
	}
	if callCount != 3 || result.ToolCalls != 1 {
		t.Fatalf("calls=%d tools=%d", callCount, result.ToolCalls)
	}
}

func TestRunLoopEmptyDeliverableContractStopsOnProse(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "声明的交付") {
			t.Errorf("empty contract inserted a deliverable nudge")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "普通回答"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()
	cb := &deliverableGuardCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   8,
		sysPrompt: "sys",
	}}
	result := RunLoop(cb, "今天星期几", nil, nil)
	if result.Error != "" || result.HardExit {
		t.Fatalf("result=%+v", result)
	}
	if result.Text != "普通回答" {
		t.Fatalf("text=%q", result.Text)
	}
	if callCount != 1 {
		t.Fatalf("calls=%d", callCount)
	}
}
