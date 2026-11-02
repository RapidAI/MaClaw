package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// consumedGrantSurfaceCallbacks renders the tool set only on the first model
// request, simulating a semantic host whose one-shot grant is consumed by the
// first successful call (production 2026-08-25: send_file succeeded, the next
// request surface dropped it, the model's retry was denied, and the assistant
// wrongly told the user the PDF could not be delivered).
type consumedGrantSurfaceCallbacks struct {
	mockCallbacks
	renders int
}

func (m *consumedGrantSurfaceCallbacks) BuildTools(string) []map[string]interface{} {
	m.renders++
	if m.renders == 1 {
		return m.tools
	}
	return nil
}

type renewingSurfaceCallbacks struct {
	consumedGrantSurfaceCallbacks
	opened bool
	opens  int
}

func (m *renewingSurfaceCallbacks) BuildTools(string) []map[string]interface{} {
	m.renders++
	if m.renders == 1 || m.opened {
		return m.tools
	}
	return nil
}

func (m *renewingSurfaceCallbacks) OpenNextRepeatWave(string) bool {
	m.opened = true
	m.opens++
	return true
}

func TestLatestToolResultUnsettledUsesTheHostStatusLine(t *testing.T) {
	if !LatestToolResultUnsettled([]ConversationEntry{{Role: "tool", ToolName: "ssh", Content: "[system unknown] trusted_ssh_outcome_unobserved"}}) {
		t.Fatal("unknown status was settled")
	}
	if !LatestToolResultUnsettled([]ConversationEntry{{Role: "tool", ToolName: "send_file", Content: "[system rejected] dynamic_effect_awaiting_receipt"}}) {
		t.Fatal("parked receipt was settled")
	}
	if LatestToolResultUnsettled([]ConversationEntry{{Role: "tool", ToolName: "read_file", Content: "stays awaiting_receipt\nreturn \"[system unknown] lost\""}}) {
		t.Fatal("a quoted marker was in flight")
	}
	if LatestToolResultUnsettled([]ConversationEntry{{Role: "tool", ToolName: "ssh", Content: "[system rejected] selection_execution_not_awaiting_receipt"}}) {
		t.Fatal("a settled rejection was in flight")
	}
	if LatestToolResultUnsettled([]ConversationEntry{{Role: "tool", ToolName: "ssh", Content: "trusted_ssh_outcome_unobserved"}}) {
		t.Fatal("a bare error name was in flight")
	}
	if LatestToolResultUnsettled([]ConversationEntry{
		{Role: "tool", ToolName: "ssh", Content: "[system unknown] trusted_ssh_outcome_unobserved"},
		{Role: "tool", ToolName: "tools_search", Content: "[system unknown] ignored"},
		{Role: "tool", ToolName: "ssh", Content: "EXIT=0"},
	}) {
		t.Fatal("a later settled result stayed in flight")
	}
}

func TestSpentRepeatHandoffDetectsUserContinueAsk(t *testing.T) {
	if !answerHandsWorkBackToUser("请再发一次「继续」，我读取结果。") {
		t.Fatal("explicit continue ask was ignored")
	}
	if !answerHandsWorkBackToUser("下轮我会核对 EXIT。") {
		t.Fatal("next-round handoff was ignored")
	}
	if !answerHandsWorkBackToUser("本轮 SSH 额度已用尽，无法再轮询。") {
		t.Fatal("exhausted-quota handoff was ignored")
	}
	if !answerHandsWorkBackToUser("本轮 ssh 调用额度已用完（计划 7 次），请发一条消息（比如「查看升级进度」）。") {
		t.Fatal("quota-used-up handoff was ignored")
	}
	if !answerHandsWorkBackToUser("你回一句「看进度」，我再查日志。") {
		t.Fatal("reply-again handoff was ignored")
	}
	if answerHandsWorkBackToUser("VACUUM 已完成，磁盘降到 50%。") || answerHandsWorkBackToUser("清理完成后可以继续观察磁盘。") || answerHandsWorkBackToUser("我回一句结论：升级已完成。") {
		t.Fatal("a finished report was treated as a handoff")
	}
	if !remoteCommandWaveSpent("RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. another call will be listed.") {
		t.Fatal("canonical remote-command note was ignored")
	}
	if remoteCommandWaveSpent("another call will be listed\nshell.execute.remote_host") || remoteCommandWaveSpent("Planned invocations for information.search.web in this turn (5) are complete. another call will be listed.") {
		t.Fatal("a non-note result published another remote command")
	}
	history := []ConversationEntry{
		{Role: "tool", ToolName: "ssh", Content: "pid running\n\n[system] Planned invocations for shell.execute.remote_host in this turn (8) are complete. If this task is unfinished, call this tool again on the next request; another call will be listed."},
	}
	if got := spentRepeatToolName(history); got != "ssh" {
		t.Fatalf("spent tool=%q", got)
	}
	withSearch := append(append([]ConversationEntry{}, history...), ConversationEntry{Role: "tool", ToolName: "tools_search", Content: "tools_search reached its limit"})
	if got := spentRepeatToolName(withSearch); got != "ssh" {
		t.Fatalf("discovery hid the spent command, got %q", got)
	}
	history = append(history, ConversationEntry{Role: "tool", ToolName: "read_file", Content: "ok"})
	if got := spentRepeatToolName(history); got != "" {
		t.Fatalf("later tool must hide the spent command, got %q", got)
	}
	remote := []ConversationEntry{{Role: "tool", ToolName: "ssh", Content: "RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. another call will be listed."}}
	if got := spentRemoteCommandName(remote); got != "ssh" {
		t.Fatalf("remote spent wave = %q", got)
	}
	search := []ConversationEntry{{Role: "tool", ToolName: "web_search", Content: "results\n\n[system] Planned invocations for information.search.web in this turn (4) are complete. another call will be listed."}}
	if got := spentRemoteCommandName(search); got != "" {
		t.Fatalf("search spent wave must not auto-relist, got %q", got)
	}
	if got := spentRepeatToolName(search); got != "web_search" {
		t.Fatalf("explicit handoff must still see the search wave, got %q", got)
	}
}

func TestRunLoopListsSpentRemoteCommandBeforeTheModelAsks(t *testing.T) {
	callCount := 0
	sawRelistedTool := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_ssh", "type": "function",
						"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"tail /tmp/apt_upgrade.log"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		default:
			if strings.Contains(string(body), `"name":"ssh"`) || strings.Contains(string(body), `"name": "ssh"`) {
				sawRelistedTool = true
			}
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "升级仍在后台运行。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   6,
		sysPrompt: "sys",
		toolResult: "RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. " +
			"If this task is unfinished, call this tool again on the next request in this same turn; another call will be listed.",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	result := RunLoop(cb, "更新系统上的所有包", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "升级仍在后台运行。" {
		t.Fatalf("text=%q", result.Text)
	}
	if !cb.opened || !sawRelistedTool {
		t.Fatalf("opened=%v relisted=%v calls=%d", cb.opened, sawRelistedTool, callCount)
	}
}

func TestRunLoopSpentRepeatWaveContinuesWithoutUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var resp map[string]interface{}
		sshCall := map[string]interface{}{
			"message": map[string]interface{}{
				"role": "assistant", "content": "",
				"tool_calls": []map[string]interface{}{{
					"id": "call_ssh", "type": "function",
					"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"echo ok"}`},
				}},
			},
			"finish_reason": "tool_calls",
		}
		switch callCount {
		case 1, 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{sshCall}}
		case 3:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "请再发一次继续"},
				"finish_reason": "stop",
			}}}
		case 4:
			if !strings.Contains(string(body), "已再次列入") {
				t.Errorf("auto-continue missing from request %d", callCount)
			}
			resp = map[string]interface{}{"choices": []map[string]interface{}{sshCall}}
		default:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "VACUUM 已完成。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    8,
		sysPrompt:  "sys",
		toolResult: "pid 373912 still running",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	// The second model call finds ssh absent. The opener lists it again, the
	// model still answers without calling, and the loop must not stop there.
	result := RunLoop(cb, "继续", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "VACUUM 已完成。" {
		t.Fatalf("text=%q", result.Text)
	}
	if !cb.opened || callCount < 5 {
		t.Fatalf("opened=%v calls=%d", cb.opened, callCount)
	}
}

func TestRunLoopQuotaHandoffContinuesWithoutASpentNote(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_ssh", "type": "function",
						"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"check bg_1792484853_1"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		case 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "本轮操作额度已用完，因此结果还没取到，下一轮我再整理 MAE。"},
				"finish_reason": "stop",
			}}}
		case 3:
			if !strings.Contains(string(body), "已再次列入") || !strings.Contains(string(body), "不要说额度已用完") {
				t.Errorf("quota handoff was accepted as the final answer: %s", body)
			}
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "MAE 已整理。"},
				"finish_reason": "stop",
			}}}
		default:
			t.Errorf("unexpected model call %d", callCount)
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "MAE 已整理。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    6,
		sysPrompt:  "sys",
		toolResult: "bg_1792484853_1 running",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	result := RunLoop(cb, "整理评测结果", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "MAE 已整理。" {
		t.Fatalf("text=%q calls=%d opened=%v", result.Text, callCount, cb.opened)
	}
	if !cb.opened {
		t.Fatal("quota handoff did not list the next call")
	}
}

// listedClosedWaveCallbacks keeps the tool on every request and refuses to
// publish another semantic sibling. A handback still has to continue.
type listedClosedWaveCallbacks struct {
	mockCallbacks
}

func (m *listedClosedWaveCallbacks) OpenNextRepeatWave(string) bool { return false }

func TestRunLoopListedToolContinuesWhenTheWaveStaysClosed(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_ssh", "type": "function",
						"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"check bg_1792484853_1"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		case 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "本轮操作额度已用完，因此结果还没取到，下一轮我再整理 MAE。"},
				"finish_reason": "stop",
			}}}
		case 3:
			if !strings.Contains(string(body), "已再次列入") {
				t.Errorf("a listed tool was treated as finished: %s", body)
			}
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "MAE 已整理。"},
				"finish_reason": "stop",
			}}}
		default:
			t.Errorf("unexpected model call %d", callCount)
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "MAE 已整理。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &listedClosedWaveCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    6,
		sysPrompt:  "sys",
		toolResult: "bg_1792484853_1 running",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}
	result := RunLoop(cb, "整理评测结果", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "MAE 已整理。" {
		t.Fatalf("text=%q calls=%d", result.Text, callCount)
	}
	if callCount < 3 {
		t.Fatalf("calls=%d", callCount)
	}
}

func TestRunLoopDoesNotRelistNonRemoteSpentWave(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_search", "type": "function",
						"function": map[string]interface{}{"name": "web_search", "arguments": `{"query":"go"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		} else {
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "搜索完成。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   4,
		sysPrompt: "sys",
		toolResult: "results\n\n[system] Planned invocations for information.search.web in this turn (4) are complete. " +
			"If this task is unfinished, call this tool again on the next request in this same turn; another call will be listed.",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "web_search",
				"description": "Search",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	result := RunLoop(cb, "搜索 Go", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "搜索完成。" {
		t.Fatalf("text=%q", result.Text)
	}
	if cb.opened {
		t.Fatal("a spent search wave was republished like a remote command")
	}
}

func TestRunLoopRemoteCommandWaveListsNextCall(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		sshCall := map[string]interface{}{
			"message": map[string]interface{}{
				"role": "assistant", "content": "",
				"tool_calls": []map[string]interface{}{{
					"id": "call_ssh", "type": "function",
					"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"tail /tmp/apt_upgrade.log"}`},
				}},
			},
			"finish_reason": "tool_calls",
		}
		switch callCount {
		case 1:
			resp := map[string]interface{}{"choices": []map[string]interface{}{sshCall}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case 2:
			if !strings.Contains(string(body), `"name":"ssh"`) && !strings.Contains(string(body), `"name": "ssh"`) {
				t.Errorf("spent remote wave did not list ssh on the next request")
			}
			resp := map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "本轮 ssh 调用额度已用完（计划 7 次）。请发一条消息（比如「查看升级进度」）。"},
				"finish_reason": "stop",
			}}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case 3:
			if !strings.Contains(string(body), "已再次列入") {
				t.Errorf("auto-continue missing from request %d", callCount)
			}
			resp := map[string]interface{}{"choices": []map[string]interface{}{sshCall}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			resp := map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "升级已完成，EXIT:0。"},
				"finish_reason": "stop",
			}}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    8,
		sysPrompt:  "sys",
		toolResult: "RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. If this task is unfinished, call this tool again on the next request in this same turn; another call will be listed.",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	result := RunLoop(cb, "更新系统上的所有包", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "升级已完成，EXIT:0。" {
		t.Fatalf("text=%q", result.Text)
	}
	if !cb.opened || callCount < 4 {
		t.Fatalf("opened=%v calls=%d", cb.opened, callCount)
	}
}

func TestRunLoopRemoteWavePublishesPastTheHandoffCap(t *testing.T) {
	const polls = 10
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount <= polls {
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_ssh", "type": "function",
						"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"tail /tmp/apt_upgrade.log"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		} else {
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "升级已完成，EXIT:0。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    polls + 2,
		sysPrompt:  "sys",
		toolResult: "RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. another call will be listed.",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ssh",
				"description": "Remote command",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}}}
	result := RunLoop(cb, "更新系统上的所有包", nil, nil)
	if result.Error != "" || result.Text != "升级已完成，EXIT:0。" {
		t.Fatalf("text=%q err=%s", result.Text, result.Error)
	}
	if cb.opens != polls {
		t.Fatalf("opens=%d, want %d", cb.opens, polls)
	}
}

type scriptedWaveCallbacks struct {
	renewingSurfaceCallbacks
	script []ToolExecutionResult
	at     int
}

func (m *scriptedWaveCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	if m.at >= len(m.script) {
		return ToolExecutionResult{Result: "done", Outcome: ToolExecutionOutcomeOK}
	}
	result := m.script[m.at]
	m.at++
	return result
}

func TestRunLoopUnsettledRemoteCommandDoesNotNudge(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		var resp map[string]interface{}
		switch callCount {
		case 1, 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]interface{}{{
						"id": "call_ssh", "type": "function",
						"function": map[string]interface{}{"name": "ssh", "arguments": `{"command":"tail /tmp/apt_upgrade.log"}`},
					}},
				},
				"finish_reason": "tool_calls",
			}}}
		default:
			if strings.Contains(string(body), "已再次列入") {
				t.Errorf("unsettled command was nudged back onto ssh")
			}
			resp = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "请发一条消息，我再查一次。"},
				"finish_reason": "stop",
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	note := "RUNNING\n\n[system] Planned invocations for shell.execute.remote_host in this turn (7) are complete. another call will be listed."
	cb := &scriptedWaveCallbacks{
		renewingSurfaceCallbacks: renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
			config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:   6,
			sysPrompt: "sys",
			tools: []map[string]interface{}{{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "ssh",
					"description": "Remote command",
					"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
				},
			}},
		}}},
		script: []ToolExecutionResult{
			{Result: note, Outcome: ToolExecutionOutcomeOK},
			{Result: "[system unknown] trusted_ssh_outcome_unobserved", Outcome: ToolExecutionOutcomeError},
		},
	}
	result := RunLoop(cb, "更新系统上的所有包", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.Text != "请发一条消息，我再查一次。" {
		t.Fatalf("text=%q", result.Text)
	}
	if cb.opens != 1 {
		t.Fatalf("opens=%d calls=%d", cb.opens, callCount)
	}
}

func TestRunLoopConsumedGrantDenialPreservesEarlierSuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		toolCall := map[string]interface{}{
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "send_file",
							"arguments": `{}`,
						},
					},
				},
			},
			"finish_reason": "tool_calls",
		}
		switch callCount {
		case 1, 2:
			resp = map[string]interface{}{"choices": []map[string]interface{}{toolCall}}
		default:
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "报告已生成并投递。"},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "Artifact prepared for delivery to the current channel",
		tools: []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "send_file",
					"parameters": map[string]interface{}{"type": "object"},
				},
			},
		},
	}}

	result := RunLoop(cb, "生成并发送报告", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The grant executed exactly once; the second call must be denied before
	// reaching the executor.
	if got := len(cb.toolCalls); got != 1 {
		t.Fatalf("tool executed %d times, want exactly one (the live grant)", got)
	}
	// The denial for the consumed grant must tell the model the earlier success
	// still stands, not a bare "not available" that reads as delivery failure.
	var denial string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if entry.Role == "tool" && strings.Contains(text, "not") {
			denial = text
		}
	}
	if denial == "" {
		t.Fatalf("no denial recorded in history delta: %+v", result.HistoryDelta)
	}
	if !strings.Contains(denial, "already ran successfully") || strings.Contains(denial, "was not available") {
		t.Fatalf("consumed-grant denial must preserve the earlier success, got: %q", denial)
	}
	if !strings.Contains(result.Text, "报告已生成并投递") {
		t.Fatalf("unexpected final text: %q", result.Text)
	}
}

// A name that never succeeded keeps the generic unrendered denial.
func TestRunLoopUnrenderedDenialWithoutPriorSuccessStaysGeneric(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "ghost_tool",
										"arguments": `{}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "done"},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter: 5, sysPrompt: "test",
	}
	result := RunLoop(cb, "call a ghost tool", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	var denial string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if entry.Role == "tool" {
			denial = text
		}
	}
	if !strings.Contains(denial, "was not available in this request's rendered tool surface") {
		t.Fatalf("never-executed tool must keep the generic denial, got: %q", denial)
	}
	if strings.Contains(denial, "already ran successfully") {
		t.Fatalf("never-executed tool must not claim an earlier success: %q", denial)
	}
}

// landedCommitCallbacks is a one-shot surface whose success is a commit, not a
// poll. A repeat of the same payload must not publish another wave.
type landedCommitCallbacks struct {
	renewingSurfaceCallbacks
	poll map[string]bool
}

func (m *landedCommitCallbacks) IdenticalSuccessIsPoll(name string) bool {
	return m != nil && m.poll[strings.TrimSpace(name)]
}

func TestRunLoopLandedCommitDoesNotOpenAnotherWave(t *testing.T) {
	payloads := []string{
		`{"path":"deck.pptx","theme":"clean"}`,
		`{"theme":"clean","path":"deck.pptx"}`,
		`{"path":"other.pptx","theme":"clean"}`,
		`{"path":"other.pptx","theme":"clean"}`,
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount <= len(payloads) {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "office",
										"arguments": payloads[callCount-1],
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "写好了"}, "finish_reason": "stop"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &landedCommitCallbacks{renewingSurfaceCallbacks: renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     8,
		sysPrompt:   "test",
		toolResult:  "published",
		toolOutcome: ToolExecutionOutcomeOK,
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name":       "office",
				"parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.HardExit || result.Text != "写好了" {
		t.Fatalf("result = %+v, want the model to finish after the revision", result)
	}
	if len(cb.toolCalls) != 2 || len(cb.toolArgs) != 2 {
		t.Fatalf("executed %d calls args=%v, want the original and the revision only", len(cb.toolCalls), cb.toolArgs)
	}
	if !strings.Contains(cb.toolArgs[0], "deck.pptx") || strings.Contains(cb.toolArgs[0], "other.pptx") {
		t.Fatalf("first execution = %s", cb.toolArgs[0])
	}
	if !strings.Contains(cb.toolArgs[1], "other.pptx") {
		t.Fatalf("revision was not the second execution: %s", cb.toolArgs[1])
	}
	if cb.opens != 1 {
		t.Fatalf("waves opened = %d, want one wave for the changed payload", cb.opens)
	}
	var toolTexts []string
	for _, entry := range result.HistoryDelta {
		if entry.Role != "tool" {
			continue
		}
		text, _ := entry.Content.(string)
		toolTexts = append(toolTexts, text)
	}
	if len(toolTexts) < 3 {
		t.Fatalf("tool results = %#v", toolTexts)
	}
	if !strings.Contains(toolTexts[1], "本次没有再次执行") || strings.Contains(toolTexts[1], "listed on the next request") {
		t.Fatalf("duplicate payload must skip without inviting another call, got %q", toolTexts[1])
	}
	if !strings.Contains(toolTexts[2], "listed on the next request") {
		t.Fatalf("changed payload must still open a wave, got %q", toolTexts[2])
	}
}

func TestRunLoopPollStillOpensTheNextWave(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount <= 3 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "ssh",
										"arguments": `{"command":"status"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "还在跑"}, "finish_reason": "stop"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &landedCommitCallbacks{
		poll: map[string]bool{"ssh": true},
		renewingSurfaceCallbacks: renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
			config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:     8,
			sysPrompt:   "test",
			toolResult:  "running",
			toolOutcome: ToolExecutionOutcomeOK,
			tools: []map[string]interface{}{
				{"type": "function", "function": map[string]interface{}{
					"name":       "ssh",
					"parameters": map[string]interface{}{"type": "object"},
				}},
			},
		}}},
	}
	result := RunLoop(cb, "看一下任务", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(cb.toolCalls) != 2 {
		t.Fatalf("ssh executed %d times, want the first check and the check after the wave", len(cb.toolCalls))
	}
	if cb.opens != 1 {
		t.Fatalf("waves opened = %d, want the status poll to keep renewing", cb.opens)
	}
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "本次没有再次执行") {
			t.Fatalf("a status poll was treated as an already-landed commit: %q", text)
		}
	}
}

// residentCommitCallbacks keeps the tool listed and treats it as a commit.
// Outcomes are scripted so a later identical call can be distinguished from
// a real failure of a different payload.
type residentCommitCallbacks struct {
	mockCallbacks
	executions int
}

func (m *residentCommitCallbacks) BuildTools(string) []map[string]interface{} { return m.tools }

func (m *residentCommitCallbacks) IdenticalSuccessIsPoll(name string) bool {
	return strings.TrimSpace(name) == "bash"
}

func (m *residentCommitCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	m.executions++
	if name == "bash" {
		return ToolExecutionResult{Result: "ok", Outcome: ToolExecutionOutcomeOK}
	}
	if strings.Contains(args, "a.pptx") {
		return ToolExecutionResult{Result: "published", Outcome: ToolExecutionOutcomeOK}
	}
	return ToolExecutionResult{Result: "Error: write failed", Outcome: ToolExecutionOutcomeError}
}

func TestRunLoopCommitSkipDoesNotClearFailureStreak(t *testing.T) {
	const failuresBeforeSkip = 7
	type call struct{ name, args string }
	calls := []call{{name: "office", args: `{"path":"a.pptx"}`}}
	for i := 0; i < failuresBeforeSkip; i++ {
		calls = append(calls,
			call{name: "office", args: `{"path":"b.pptx"}`},
			call{name: "bash", args: `{"command":"true"}`},
		)
	}
	calls = append(calls,
		call{name: "office", args: `{"path":"a.pptx"}`},
		call{name: "office", args: `{"path":"b.pptx"}`},
	)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount <= len(calls) {
			step := calls[callCount-1]
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      step.name,
										"arguments": step.args,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "还在改另一份"}, "finish_reason": "stop"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   24,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
			{"type": "function", "function": map[string]interface{}{
				"name": "bash", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}
	result := RunLoop(cb, "改两份演示", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// a.pptx once, b.pptx seven times, bash seven times, the repeat of a.pptx
	// skipped, then b.pptx once more.
	if cb.executions != failuresBeforeSkip*2+2 {
		t.Fatalf("executions = %d, want %d", cb.executions, failuresBeforeSkip*2+2)
	}
	var sawGuidance bool
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "已连续失败") {
			sawGuidance = true
		}
	}
	if !sawGuidance {
		t.Fatalf("skipping the already-written file cleared the failure streak: %#v", result.HistoryDelta)
	}
}

type allowCountCallbacks struct {
	residentCommitCallbacks
	callAllows int
}

func (m *allowCountCallbacks) IsToolCallAllowed(string, string) (bool, string) {
	m.callAllows++
	return true, ""
}

func TestRunLoopSameBatchDuplicateIsNotAuthorized(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{"id": "call_1", "type": "function", "function": map[string]interface{}{
									"name": "office", "arguments": `{"path":"a.pptx","theme":"clean"}`,
								}},
								{"id": "call_2", "type": "function", "function": map[string]interface{}{
									"name": "office", "arguments": `{"theme":"clean","path":"a.pptx"}`,
								}},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "写好了"}, "finish_reason": "stop"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &allowCountCallbacks{residentCommitCallbacks: residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   6,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if cb.executions != 1 {
		t.Fatalf("executions = %d, want the first call only", cb.executions)
	}
	if cb.callAllows != 1 {
		t.Fatalf("authorization checks = %d, want the duplicate to skip before the host can open another wave", cb.callAllows)
	}
}

func TestRunLoopLandedCommitHandoffDoesNotDemandTheSameCall(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var content string
		var toolCalls []map[string]interface{}
		switch callCount {
		case 1:
			toolCalls = []map[string]interface{}{
				{"id": "call_1", "type": "function", "function": map[string]interface{}{
					"name": "office", "arguments": `{"path":"a.pptx"}`,
				}},
			}
		case 2:
			content = "额度已用完，请发一条消息。"
		default:
			content = "写好了"
		}
		message := map[string]interface{}{"role": "assistant", "content": content}
		if len(toolCalls) > 0 {
			message["tool_calls"] = toolCalls
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": message, "finish_reason": "stop"},
			},
		}
		if len(toolCalls) > 0 {
			resp["choices"].([]map[string]interface{})[0]["finish_reason"] = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   6,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if result.Error != "" || result.Text != "写好了" {
		t.Fatalf("result = %+v", result)
	}
	var nudge string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "不要再调用") {
			nudge = text
		}
	}
	if nudge == "" || strings.Contains(nudge, "请直接调用") || !strings.Contains(nudge, "已在本轮工具列表中") {
		t.Fatalf("handoff after a landed commit must not demand the same call, got %q", nudge)
	}
}

func TestRunLoopLandedCommitHandoffDoesNotRepublish(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var content string
		var toolCalls []map[string]interface{}
		switch callCount {
		case 1:
			toolCalls = []map[string]interface{}{
				{"id": "call_1", "type": "function", "function": map[string]interface{}{
					"name": "office", "arguments": `{"path":"a.pptx"}`,
				}},
			}
		case 2:
			content = "额度已用完，请发一条消息。"
		default:
			content = "写好了"
		}
		message := map[string]interface{}{"role": "assistant", "content": content}
		if len(toolCalls) > 0 {
			message["tool_calls"] = toolCalls
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": message, "finish_reason": "stop"},
			},
		}
		if len(toolCalls) > 0 {
			resp["choices"].([]map[string]interface{})[0]["finish_reason"] = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &landedCommitCallbacks{renewingSurfaceCallbacks: renewingSurfaceCallbacks{consumedGrantSurfaceCallbacks: consumedGrantSurfaceCallbacks{mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "test",
		toolResult:  "published",
		toolOutcome: ToolExecutionOutcomeOK,
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if result.Error != "" || result.Text != "写好了" {
		t.Fatalf("result = %+v", result)
	}
	if cb.opens != 0 {
		t.Fatalf("waves opened = %d, a landed commit must not be republished when the model only says the quota is used up", cb.opens)
	}
	var nudge string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "没有再次列入") {
			nudge = text
		}
	}
	if nudge == "" || strings.Contains(nudge, "请直接调用") {
		t.Fatalf("unlisted landed commit handoff = %q", nudge)
	}
}

// failOpenPollCallbacks classifies only the first observation as a commit.
// A later lookup fails open and reports a poll, which must not rewrite.
type failOpenPollCallbacks struct {
	residentCommitCallbacks
	lookups int
}

func (m *failOpenPollCallbacks) IdenticalSuccessIsPoll(string) bool {
	m.lookups++
	return m.lookups > 1
}

func TestRunLoopLandedCommitSurvivesFailOpenPoll(t *testing.T) {
	callCount := 0
	args := []string{`{"path":"a.pptx","theme":"clean"}`, `{"theme":"clean","path":"a.pptx"}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount <= len(args) {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "office",
										"arguments": args[callCount-1],
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"role": "assistant", "content": "写好了"}, "finish_reason": "stop"},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &failOpenPollCallbacks{residentCommitCallbacks: residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   6,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if result.Error != "" || result.Text != "写好了" {
		t.Fatalf("result = %+v", result)
	}
	if cb.executions != 1 {
		t.Fatalf("executions = %d, a later poll classification must not rewrite the landed payload", cb.executions)
	}
}

func TestRunLoopFailOpenSkipStillStopsTheRepeat(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   fmt.Sprintf("call_%d", callCount),
								"type": "function",
								"function": map[string]interface{}{
									"name":      "office",
									"arguments": `{"path":"a.pptx"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &failOpenPollCallbacks{residentCommitCallbacks: residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   8,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if cb.executions != 1 {
		t.Fatalf("executions = %d, want one write", cb.executions)
	}
	if !result.HardExit || !strings.Contains(result.Text, "停止重复") {
		t.Fatalf("result = %+v, the repeat must stop even when later lookups say poll", result)
	}
}

func TestRunLoopSkippedRepeatDoesNotAskTheModelToContinue(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role": "assistant", "content": "",
						"tool_calls": []map[string]interface{}{{
							"id": fmt.Sprintf("call_%d", callCount), "type": "function",
							"function": map[string]interface{}{
								"name": "office", "arguments": `{"path":"a.pptx"}`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &residentCommitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   8,
		sysPrompt: "test",
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "office", "parameters": map[string]interface{}{"type": "object"},
			}},
		},
	}}
	result := RunLoop(cb, "做一份演示", nil, nil)
	if cb.executions != 1 {
		t.Fatalf("executions = %d, want one write", cb.executions)
	}
	if !result.HardExit {
		t.Fatalf("result = %+v, want the repeat to stop", result)
	}
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "若任务还要继续") {
			t.Fatalf("a skipped repeat must not be told to continue: %q", text)
		}
	}
}
