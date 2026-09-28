package agent

import (
	"encoding/json"
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
