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

func TestOpenCodeFreeAdjacentUserTurnsStillMerge(t *testing.T) {
	caller := map[string]any{
		"model": "hy4-flash-free",
		"messages": []map[string]any{
			{"role": "user", "content": "第一段任务"},
			{"role": "user", "content": "第二段任务"},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var users int
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		if strings.Contains(msg, "第一段任务") && strings.Contains(msg, "第二段任务") {
			users++
			if strings.Contains(msg, "<system-reminder>") {
				t.Fatalf("plain user turn gained a note: %s", msg)
			}
		}
	}
	if users != 1 {
		t.Fatalf("adjacent user turns = %d\n%s", users, body)
	}
}

func TestOpenCodeFreeDeveloperTurnIsNotUserTurn(t *testing.T) {
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "developer", "content": "只报告标点，不要改文件"},
			{"role": "user", "content": "扫描论文"},
			{"role": "developer", "content": "第二段指令"},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawTask, sawFirst, sawSecond bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		switch {
		case strings.Contains(msg, "只报告标点"):
			sawFirst = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "扫描论文") || strings.Contains(msg, "第二段指令") {
				t.Fatalf("first developer turn = %s", msg)
			}
		case strings.Contains(msg, "第二段指令"):
			sawSecond = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "扫描论文") || strings.Contains(msg, "只报告标点") {
				t.Fatalf("second developer turn = %s", msg)
			}
		case strings.HasPrefix(msg, "扫描论文"):
			sawTask = true
			if strings.Count(msg, "<system-reminder>") != 1 || strings.Contains(msg, "只报告标点") || strings.Contains(msg, "第二段指令") {
				t.Fatalf("task turn = %s", msg)
			}
		}
	}
	if !sawTask || !sawFirst || !sawSecond {
		t.Fatalf("body = %s", body)
	}
}

func TestOpenCodeFreeDeveloperOnlyBodyKeepsInstruction(t *testing.T) {
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "developer", "content": "只报告标点，不要改文件"},
			{"role": "system", "content": "你是助手"},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawInstruction, sawSystem, sawNote bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		if strings.Contains(msg, "你是助手") {
			sawSystem = true
		}
		if strings.Contains(msg, "<system-reminder>") && !strings.Contains(msg, "只报告标点") {
			sawNote = true
		}
		if strings.Contains(msg, "只报告标点") {
			sawInstruction = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "你是助手") {
				t.Fatalf("developer-only turn = %s", msg)
			}
		}
	}
	if sawSystem || !sawInstruction || !sawNote {
		t.Fatalf("body = %s", body)
	}
}

func TestOpenCodeFreeEmptyRoleStillReachesWire(t *testing.T) {
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "", "content": "keep blank role"},
			{"role": "user", "content": "扫描论文"},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawBlank, sawTask bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		switch {
		case strings.Contains(msg, "keep blank role"):
			sawBlank = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "扫描论文") {
				t.Fatalf("blank role = %s", msg)
			}
		case strings.HasPrefix(msg, "扫描论文"):
			sawTask = true
			if strings.Count(msg, "<system-reminder>") != 1 || strings.Contains(msg, "keep blank role") {
				t.Fatalf("task turn = %s", msg)
			}
		}
	}
	if !sawBlank || !sawTask {
		t.Fatalf("body = %s", body)
	}
}


func TestOpenCodeFreeHumanTurnReceivesHostNote(t *testing.T) {
	caller := map[string]any{
		"model": "hy4-flash-free",
		"tools": []map[string]any{
			{"type": "function", "function": map[string]string{"name": "chat_task_request_input"}},
		},
		"messages": []map[string]any{
			{"role": "human", "content": "扫描论文"},
			{"role": "critic", "content": "不要改文件"},
		},
	}
	body := openCodeFreeMergeBody(t, caller)
	var sawTask, sawCritic bool
	for _, msg := range openCodeFreeMergeMessages(t, body) {
		switch {
		case strings.HasPrefix(msg, "扫描论文"):
			sawTask = true
			if strings.Count(msg, "<system-reminder>") != 1 || strings.Contains(msg, "不要改文件") {
				t.Fatalf("human turn = %s", msg)
			}
		case strings.Contains(msg, "不要改文件"):
			sawCritic = true
			if strings.Contains(msg, "<system-reminder>") || strings.Contains(msg, "扫描论文") {
				t.Fatalf("other role = %s", msg)
			}
		}
	}
	if !sawTask || !sawCritic {
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
