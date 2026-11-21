package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeFreeParallelToolObservationsStaySeparate(t *testing.T) {
	page := "===== PAGE 1 =====\n论文正文"
	denial := OpenCodeFreeHostChannelRetry([]string{"chat_task_request_input"})
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "扫描论文"},
			{"role": "assistant", "content": "", "tool_calls": []map[string]any{
				{"function": map[string]string{"name": "read_file", "arguments": `{"path":"a"}`}},
				{"function": map[string]string{"name": "execute", "arguments": `{"code":"1"}`}},
			}},
			{"role": "tool", "content": page},
			{"role": "tool", "content": denial},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawPage, sawDenial bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		if strings.Contains(msg, page) && strings.Contains(msg, denial) {
			t.Fatalf("parallel observations merged: %s", msg)
		}
		if msg == page {
			sawPage = true
		}
		if msg == denial {
			sawDenial = true
		}
	}
	if !sawPage || !sawDenial {
		t.Fatalf("body = %s", body)
	}
}

func TestOpenCodeFreeLegacyFunctionResultIsNotUserTurn(t *testing.T) {
	page := "function result page"
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "扫描论文"},
			{"role": "assistant", "content": "", "tool_calls": []map[string]any{
				{"function": map[string]string{"name": "read_file", "arguments": `{"path":"a"}`}},
			}},
			{"role": "function", "name": "read_file", "content": page},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawTask, sawPage bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		if strings.HasPrefix(msg, "扫描论文") {
			sawTask = true
			if strings.Count(msg, "<system-reminder>") != 1 || strings.Contains(msg, page) {
				t.Fatalf("task turn = %s", msg)
			}
		}
		if strings.Contains(msg, page) {
			sawPage = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "扫描论文") {
				t.Fatalf("legacy observation = %s", msg)
			}
		}
	}
	if !sawTask || !sawPage {
		t.Fatalf("body = %s", body)
	}
}

func openCodeFreeMergeBody(t *testing.T, caller map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(caller)
	if err != nil {
		t.Fatal(err)
	}
	body, err := openCodeFreeClientBody(raw)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func openCodeFreeMergeMessages(t *testing.T, body []byte) []string {
	t.Helper()
	var parsed struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(parsed.Messages))
	for i, msg := range parsed.Messages {
		out[i] = msg.Content
	}
	return out
}
