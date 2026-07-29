package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestTrajectoryRecorder_RecordHistoryAndDelta(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sess-1", "Custom1", "m1", "openai", "u1", "desktop", "main", "", nil)

	r.Record("system", "sys", nil, "", "")
	r.RecordHistory([]agent.ConversationEntry{
		{Role: "user", Content: "prior q"},
		{
			Role:    "assistant",
			Content: "prior a",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id": "call-prior",
					"function": map[string]interface{}{
						"name":      "read_file",
						"arguments": `{"path":"a.go"}`,
					},
				},
			},
		},
		{Role: "tool", Content: "file body", ToolCallID: "call-prior", ToolName: "read_file"},
	})
	r.Record("user", "current q", nil, "", "")

	r.RecordHistoryDelta([]agent.ConversationEntry{
		{Role: "user", Content: "current q"}, // skipped
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id": "call-1",
					"function": map[string]interface{}{
						"name":      "bash",
						"arguments": `{"command":"echo hi"}`,
					},
				},
			},
			FinishReason: "tool_calls",
		},
		{Role: "tool", Content: "tool ok", ToolCallID: "call-1", ToolName: "bash", ToolOutcome: "succeeded"},
		{Role: "assistant", Content: "done", FinishReason: "stop"},
	}, true)
	r.SetOutcome("success", "", 2, 1, 10, 5)
	r.Flush()

	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("file count = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if session.Kind != "main" {
		t.Fatalf("Kind = %q, want main", session.Kind)
	}
	if session.Status != "success" || session.Iterations != 2 || session.ToolCallCount != 1 {
		t.Fatalf("outcome meta = status=%q iters=%d tools=%d", session.Status, session.Iterations, session.ToolCallCount)
	}
	if session.InputTokens != 10 || session.OutputTokens != 5 {
		t.Fatalf("tokens = %d/%d", session.InputTokens, session.OutputTokens)
	}

	roles := make([]string, 0, len(session.Entries))
	for _, e := range session.Entries {
		roles = append(roles, e.Role)
	}
	// system + prior user/assistant/tool/tool_result + current user + delta assistant/tool/tool_result/assistant
	want := []string{"system", "user", "assistant", "tool", "tool_result", "user", "assistant", "tool", "tool_result", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	// Expanded tool call from assistant tool_calls (current turn)
	if session.Entries[7].ToolCallID != "call-1" {
		t.Fatalf("tool call id = %q", session.Entries[7].ToolCallID)
	}
	if payload, ok := session.Entries[7].Content.(map[string]interface{}); !ok || payload["name"] != "bash" {
		t.Fatalf("tool call payload = %#v", session.Entries[7].Content)
	}
	if session.Entries[8].Role != "tool_result" || session.Entries[8].ToolCallID != "call-1" {
		t.Fatalf("tool_result = %+v", session.Entries[8])
	}
	if session.Entries[8].ToolOutcome != "succeeded" {
		t.Fatalf("tool outcome = %q", session.Entries[8].ToolOutcome)
	}
	// FinishReason from HistoryDelta must survive trajectory rebuild.
	if session.Entries[6].Role != "assistant" || session.Entries[6].FinishReason != "tool_calls" {
		t.Fatalf("delta assistant finish_reason = %+v", session.Entries[6])
	}
	if session.Entries[9].Role != "assistant" || session.Entries[9].FinishReason != "stop" {
		t.Fatalf("final assistant finish_reason = %+v", session.Entries[9])
	}
}

func TestBuildTrajectoryEntriesFromConversation_FinishReason(t *testing.T) {
	entries := buildTrajectoryEntriesFromConversation([]agent.ConversationEntry{
		{Role: "assistant", Content: "hi", FinishReason: "length"},
		{Role: "assistant", Content: "x", ToolCalls: []llm.ToolCall{{ID: "c1"}}, FinishReason: "tool_calls"},
	}, false)
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	if entries[0].FinishReason != "length" {
		t.Fatalf("finish_reason=%q want length", entries[0].FinishReason)
	}
	if entries[1].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason=%q want tool_calls", entries[1].FinishReason)
	}
}

func TestClassifyIMAgentResponseOutcome(t *testing.T) {
	status, errMsg := classifyIMAgentResponseOutcome(nil)
	if status != "success" || errMsg != "" {
		t.Fatalf("nil = %q/%q", status, errMsg)
	}
	status, errMsg = classifyIMAgentResponseOutcome(&IMAgentResponse{Text: "Task cancelled: x"})
	if status != "cancelled" || errMsg != "cancelled" {
		t.Fatalf("task cancelled = %q/%q", status, errMsg)
	}
	status, errMsg = classifyIMAgentResponseOutcome(&IMAgentResponse{HardExit: true, Error: "empty"})
	if status != "hard_exit" || errMsg != "empty" {
		t.Fatalf("hard_exit = %q/%q", status, errMsg)
	}
	status, errMsg = classifyIMAgentResponseOutcome(&IMAgentResponse{
		Text: "q", ResponseSource: imResponseSourceAskUser.String(),
	})
	if status != "paused" || errMsg != "" {
		t.Fatalf("ask_user = %q/%q", status, errMsg)
	}
	// Bare "cancel" substring must not be treated as cancelled.
	status, errMsg = classifyIMAgentResponseOutcome(&IMAgentResponse{Error: "cannot cancel pending write"})
	if status != "error" || errMsg != "cannot cancel pending write" {
		t.Fatalf("bare cancel substring = %q/%q, want error", status, errMsg)
	}
	if isTrajectoryCancelledSignal("cancelled during LLM retry", "") != true {
		t.Fatal("expected cancelled during LLM retry")
	}
}

func TestRecordEarlyStopToolResult_IdempotentPrimary(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{ID: "call-ask", Function: llm.ToolCallFunction{Name: "ask_user", Arguments: `{}`}},
			{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}},
		},
	})
	r.RecordEarlyStopToolResult("call-ask", "ask_user", "first")
	r.RecordEarlyStopToolResult("call-ask", "ask_user", "second") // must not duplicate primary
	r.Flush()
	entries, _ := os.ReadDir(filepath.Join(dir, "trajectories"))
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	var askCount, bashCount int
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call-ask":
			askCount++
			if e.Content != "first" {
				t.Fatalf("primary content overwritten/duplicated: %+v", e)
			}
		case "call-bash":
			bashCount++
		}
	}
	if askCount != 1 || bashCount != 1 {
		t.Fatalf("ask=%d bash=%d entries=%+v", askCount, bashCount, session.Entries)
	}
}

func TestUnpairedCloseReasonFromIMResponse(t *testing.T) {
	if got := unpairedCloseReasonFromIMResponse(nil); got != "aborted" {
		t.Fatalf("nil = %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{Text: "Task cancelled: x"}); got != "cancelled" {
		t.Fatalf("task cancelled text = %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{Error: "cancelled during tool"}); got != "cancelled" {
		t.Fatalf("cancel error = %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{Error: "Agent loop panicked: boom"}); !strings.Contains(got, "panic") {
		t.Fatalf("panic = %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{HardExit: true}); got != "hard_exit" {
		t.Fatalf("hard_exit = %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{Text: "done"}); got != "" {
		t.Fatalf("success should not close unpaired, got %q", got)
	}
	if got := unpairedCloseReasonFromIMResponse(&IMAgentResponse{
		Text: "pick", ResponseSource: imResponseSourceAskUser.String(),
	}); got != "" {
		t.Fatalf("paused ask_user should not force close reason, got %q", got)
	}
}

func TestCloseUnpairedToolCalls_UpgradesEmptyToolName(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	// Assistant tool_calls without names in nested payload are rare; simulate
	// open id first via assistant, then expanded tool row supplies the name.
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant", Iteration: 1,
		ToolCalls: []interface{}{
			map[string]interface{}{"id": "c1", "function": map[string]interface{}{"name": "", "arguments": "{}"}},
		},
	})
	r.RecordEntry(TrajectoryEntry{
		Role: "tool", ToolCallID: "c1", Iteration: 1,
		Content: map[string]interface{}{"name": "bash", "arguments": "{}"},
	})
	r.CloseUnpairedToolCalls("cancelled")
	r.mu.Lock()
	defer r.mu.Unlock()
	var found bool
	for _, e := range r.session.Entries {
		if e.Role == "tool_result" && e.ToolCallID == "c1" {
			found = true
			if e.ToolName != "bash" {
				t.Fatalf("expected name upgraded to bash, got %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("missing synthetic result: %+v", r.session.Entries)
	}
}

func TestCloseUnpairedToolCalls_PrefersEntryIteration(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.SetCurrentIteration(4) // would wrongly stamp 5 if preferred over entry iter
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant", Iteration: 2, FinishReason: "tool_calls",
		ToolCalls: []llm.ToolCall{{
			ID: "old", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`},
		}},
	})
	r.CloseUnpairedToolCalls("cancelled")
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.session.Entries) != 2 {
		t.Fatalf("entries=%d", len(r.session.Entries))
	}
	last := r.session.Entries[1]
	if last.Role != "tool_result" || last.ToolCallID != "old" {
		t.Fatalf("last=%+v", last)
	}
	if last.Iteration != 2 {
		t.Fatalf("iteration=%d want 2 (from assistant entry, not currentIteration=5)", last.Iteration)
	}
}

func TestSetOutcomeFromLoopResult_DoesNotWipeTokensWithZeroUsage(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.SetOutcome("success", "", 2, 1, 50, 20)
	// A later seal with empty usage must not zero prior totals.
	r.SetOutcomeFromLoopResult(agent.LoopResult{Text: "done", Iterations: 3, ToolCalls: 2})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session.InputTokens != 50 || r.session.OutputTokens != 20 {
		t.Fatalf("tokens wiped to %d/%d", r.session.InputTokens, r.session.OutputTokens)
	}
	if r.session.Iterations != 3 || r.session.ToolCallCount != 2 {
		t.Fatalf("counters=%d/%d", r.session.Iterations, r.session.ToolCallCount)
	}
}

func TestNormalizeTrajectoryToolOutcome(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"ok":        "succeeded",
		"OK":        "succeeded",
		"succeeded": "succeeded",
		"error":     "failed",
		"timeout":   "failed",
		"failed":    "failed",
		"cancelled": "cancelled",
		"paused":    "paused",
		"custom":    "custom",
	}
	for in, want := range cases {
		if got := normalizeTrajectoryToolOutcome(in); got != want {
			t.Fatalf("normalizeTrajectoryToolOutcome(%q)=%q want %q", in, got, want)
		}
	}
	// Shared HistoryDelta uses corelib "ok" — must become "succeeded" for training parity.
	built := buildTrajectoryEntriesFromConversation([]agent.ConversationEntry{
		{Role: "tool", Content: "out", ToolCallID: "c1", ToolName: "bash", ToolOutcome: "ok"},
	}, false)
	if len(built) != 1 || built[0].ToolOutcome != "succeeded" {
		t.Fatalf("history delta outcome = %+v", built)
	}
}

func TestSetOutcomeFromLoopResult_RecordAudioPaused(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sess-rec", "Custom1", "m1", "openai", "u1", "desktop", "shared", "", nil)
	// Flush requires ≥1 entry; record a placeholder turn first.
	r.Record("user", "开始录音", nil, "", "")
	r.SetOutcomeFromLoopResult(agent.LoopResult{
		Text:        "录音中",
		RecordAudio: &agent.RecordAudioRequest{Title: "周会"},
		Iterations:  1,
		ToolCalls:   1,
	})
	r.Flush()
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "paused" {
		t.Fatalf("status = %q, want paused for interactive record_audio", session.Status)
	}
	if session.Iterations != 1 || session.ToolCallCount != 1 {
		t.Fatalf("iters=%d tools=%d", session.Iterations, session.ToolCallCount)
	}
}

func TestAgentLoopRecorder_RecordSystemMessagesInterfaceMap(t *testing.T) {
	dir := t.TempDir()
	rec := NewTrajectoryRecorderForBaseDir(dir)
	rec.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	loop := newAgentLoopRecorder(rec)
	items := []interface{}{
		map[string]interface{}{"role": "system", "content": "via interface map"},
		map[string]string{"role": "system", "content": "via string map"},
		map[string]interface{}{"role": "user", "content": "skip me"},
	}
	loop.RecordSystemMessages(0, items)
	rec.Flush()

	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries err=%v n=%d", err, len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if len(session.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 system messages", len(session.Entries))
	}
	if session.Entries[0].Content != "via interface map" || session.Entries[1].Content != "via string map" {
		t.Fatalf("contents = %#v %#v", session.Entries[0].Content, session.Entries[1].Content)
	}
}

func TestTrajectoryRecorder_RewriteToolCallID(t *testing.T) {
	r2 := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r2.StartSession("s2", "p", "m", "openai", "u", "desktop", nil)
	r2.RecordEntry(TrajectoryEntry{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID: "a", Type: "function",
			Function: llm.ToolCallFunction{Name: "browser", Arguments: "{}"},
		}},
	})
	r2.RecordEntry(TrajectoryEntry{Role: "tool", ToolCallID: "a", ToolName: "browser"})
	r2.RecordEntry(TrajectoryEntry{Role: "tool_result", ToolCallID: "a", Content: "ok"})
	r2.RewriteToolCallID("a", "b")
	r2.RewriteToolCallID("missing", "x") // no-op

	r2.mu.Lock()
	defer r2.mu.Unlock()
	if r2.session.Entries[1].ToolCallID != "b" || r2.session.Entries[2].ToolCallID != "b" {
		t.Fatalf("rewrite tool/result failed: %+v", r2.session.Entries)
	}
	calls, ok := r2.session.Entries[0].ToolCalls.([]llm.ToolCall)
	if !ok || len(calls) != 1 || calls[0].ID != "b" {
		t.Fatalf("assistant tool_calls id not rewritten: %#v", r2.session.Entries[0].ToolCalls)
	}
}

func TestTrajectoryRecorder_TruncateToolCallArguments(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunction{Name: "write_file", Arguments: strings.Repeat("x", 500)},
		}},
	})
	r.RecordEntry(TrajectoryEntry{
		Role: "tool", ToolCallID: "c1", ToolName: "write_file",
		Content: map[string]interface{}{"name": "write_file", "arguments": strings.Repeat("x", 500)},
	})
	r.TruncateToolCallArguments("c1", "truncated-args")
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := r.session.Entries[0].ToolCalls.([]llm.ToolCall)
	if calls[0].Function.Arguments != "truncated-args" {
		t.Fatalf("assistant args = %q", calls[0].Function.Arguments)
	}
	payload := r.session.Entries[1].Content.(map[string]interface{})
	if payload["arguments"] != "truncated-args" {
		t.Fatalf("tool entry args = %#v", payload["arguments"])
	}
}

func TestTrajectoryUserContentText_Multimodal(t *testing.T) {
	if got := trajectoryUserContentText("plain"); got != "plain" {
		t.Fatalf("string = %q", got)
	}
	blocks := []interface{}{
		map[string]interface{}{"type": "text", "text": "hello multimodal"},
		map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "x"}},
	}
	if got := trajectoryUserContentText(blocks); got != "hello multimodal" {
		t.Fatalf("multimodal = %q", got)
	}
	session := &TrajectorySession{Entries: []TrajectoryEntry{
		{Role: "user", Content: blocks},
	}}
	if got := extractUserDescription(session); got != "hello multimodal" {
		t.Fatalf("extractUserDescription = %q", got)
	}
}

func TestBuildToolResultMap_PrefersToolResultRole(t *testing.T) {
	m := buildToolResultMap([]TrajectoryEntry{
		{Role: "tool", ToolCallID: "c1", Content: map[string]interface{}{"name": "bash", "arguments": "{}"}},
		{Role: "tool_result", ToolCallID: "c1", Content: "ok"},
		{Role: "tool", ToolCallID: "c2", Content: "legacy string result"},
	})
	if m["c1"] != "ok" {
		t.Fatalf("c1 = %q, want ok", m["c1"])
	}
	if m["c2"] != "legacy string result" {
		t.Fatalf("c2 = %q", m["c2"])
	}
}

func TestRecordHistoryDelta_StampsIterationByAssistantRounds(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.Record("system", "sys", nil, "", "")
	r.Record("user", "q", nil, "", "")
	r.RecordHistoryDelta([]agent.ConversationEntry{
		{Role: "user", Content: "q"}, // skipped
		{
			Role: "assistant", Content: "",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id": "c1",
					"function": map[string]interface{}{
						"name": "bash", "arguments": `{"command":"echo 1"}`,
					},
				},
			},
		},
		{Role: "tool", Content: "1", ToolCallID: "c1", ToolName: "bash"},
		{Role: "assistant", Content: "done"},
	}, true)
	r.mu.Lock()
	defer r.mu.Unlock()
	// system, user, assistant(iter1), tool(iter1), tool_result(iter1), assistant(iter2)
	if len(r.session.Entries) < 6 {
		t.Fatalf("entries=%d", len(r.session.Entries))
	}
	if r.session.Entries[2].Iteration != 1 || r.session.Entries[3].Iteration != 1 || r.session.Entries[4].Iteration != 1 {
		t.Fatalf("round1 iters: %+v %+v %+v", r.session.Entries[2], r.session.Entries[3], r.session.Entries[4])
	}
	if r.session.Entries[5].Role != "assistant" || r.session.Entries[5].Iteration != 2 {
		t.Fatalf("round2 assistant = %+v", r.session.Entries[5])
	}
}

func TestRecordLoopResult_ClosesUnpairedOnCancel(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.Record("system", "sys", nil, "", "")
	r.Record("user", "q", nil, "", "")
	r.RecordLoopResult(agent.LoopResult{
		Error: "cancelled",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "user", Content: "q"},
			{
				Role: "assistant", Content: "",
				ToolCalls: []llm.ToolCall{{
					ID: "c1", Type: "function",
					Function: llm.ToolCallFunction{Name: "bash", Arguments: "{}"},
				}},
			},
			// no tool result — cancel mid-flight
		},
		Iterations: 1,
		ToolCalls:  0,
	})
	r.Flush()
	entries, _ := os.ReadDir(filepath.Join(dir, "trajectories"))
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	_ = json.Unmarshal(data, &session)
	if session.Status != "cancelled" {
		t.Fatalf("status=%q", session.Status)
	}
	found := false
	for _, e := range session.Entries {
		if e.Role == "tool_result" && e.ToolCallID == "c1" && e.Content == "cancelled" {
			found = true
			if e.Iteration != 1 {
				t.Fatalf("synthetic iteration=%d", e.Iteration)
			}
		}
	}
	if !found {
		t.Fatalf("expected synthetic cancelled tool_result, entries=%+v", session.Entries)
	}
}

func TestTrajectoryRecorder_SetCurrentIterationAndCloseUnpaired(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.SetCurrentIteration(2) // 0-based 2 → 1-based 3
	r.RecordEntry(TrajectoryEntry{
		Role:    "assistant",
		Content: "calling",
		ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo"}`},
		}, {
			ID: "c2", Type: "function",
			Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`},
		}},
		FinishReason: "tool_calls",
	})
	r.RecordEntry(TrajectoryEntry{
		Role: "tool", ToolCallID: "c1", ToolName: "bash",
		Content: map[string]interface{}{"name": "bash", "arguments": `{"command":"echo"}`},
	})
	// Only c1 gets a result; c2 is orphaned by cancel.
	r.RecordEntry(TrajectoryEntry{Role: "tool_result", ToolCallID: "c1", Content: "ok", ToolName: "bash"})
	r.CloseUnpairedToolCalls("cancelled")
	r.Flush()

	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Entries[0].Iteration != 3 {
		t.Fatalf("assistant iteration=%d want 3", session.Entries[0].Iteration)
	}
	if session.Entries[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason=%q", session.Entries[0].FinishReason)
	}
	// assistant + tool(c1) + result(c1) + synthetic result(c2)
	if len(session.Entries) != 4 {
		t.Fatalf("entries=%d want 4: %+v", len(session.Entries), session.Entries)
	}
	last := session.Entries[3]
	if last.Role != "tool_result" || last.ToolCallID != "c2" || last.Content != "cancelled" {
		t.Fatalf("synthetic close = %+v", last)
	}
	if last.ToolOutcome != "cancelled" {
		t.Fatalf("outcome=%q", last.ToolOutcome)
	}
}

func TestSetOutcomeFromIMResponse_PrefersTelemetryTotals(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.Record("user", "q", nil, "", "")
	tel := &agentLoopTelemetry{
		LastLLMInputTokens:   10,
		LastLLMOutputTokens:  5,
		TotalLLMInputTokens:  100,
		TotalLLMOutputTokens: 40,
	}
	r.SetOutcomeFromIMResponse(&IMAgentResponse{Text: "done", InputTokens: 10, OutputTokens: 5}, tel, 4, 2)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session.InputTokens != 100 || r.session.OutputTokens != 40 {
		t.Fatalf("tokens=%d/%d want full-loop totals 100/40", r.session.InputTokens, r.session.OutputTokens)
	}
}

func TestSetOutcomeFromIMResponse_PreservesCountersAndPaused(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sess-im", "Custom1", "m1", "openai", "u1", "desktop", "main", "", nil)
	r.Record("user", "q", nil, "", "")
	r.SetOutcomeFromIMResponse(&IMAgentResponse{
		ResponseSource: imResponseSourceAskUser.String(),
		InputTokens:    11,
		OutputTokens:   7,
	}, nil, 3, 2)
	if !r.HasOutcome() {
		t.Fatal("expected HasOutcome after SetOutcomeFromIMResponse")
	}
	r.Flush()
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "paused" {
		t.Fatalf("status=%q want paused", session.Status)
	}
	if session.Iterations != 3 || session.ToolCallCount != 2 {
		t.Fatalf("iters=%d tools=%d", session.Iterations, session.ToolCallCount)
	}
	if session.InputTokens != 11 || session.OutputTokens != 7 {
		t.Fatalf("tokens=%d/%d", session.InputTokens, session.OutputTokens)
	}
}

func TestAppendAndSealSubAgentTrajectory_RecordsPostLoopVerify(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sub-verify", "p", "m", "openai", "u", "coding", "coding_subagent", "", nil)
	r.Record("system", "sys", nil, "", "")
	appendSubAgentLoopResult(r, agent.LoopResult{
		Text: "done main",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "user", Content: "implement feature"},
			{Role: "assistant", Content: "done main"},
		},
		Iterations: 1,
		ToolCalls:  0,
	}, false)
	recordSubAgentPostLoopVerify(r, 1, "go test ./...", `{"command":"go test ./...","timeout":60}`, "ok", "succeeded")
	appendSubAgentLoopResult(r, agent.LoopResult{
		Text: "fixed",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "user", Content: "fix failures"},
			{Role: "assistant", Content: "fixed"},
		},
		Iterations: 2,
		ToolCalls:  1,
	}, false)
	sealSubAgentTrajectory(r, agent.LoopResult{Text: "fixed", Iterations: 3, ToolCalls: 1})
	flushSubAgentTrajectory(r)

	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, e := range session.Entries {
		roles = append(roles, e.Role)
	}
	// system + main user/assistant + verify system/tool/tool_result + fix user/assistant
	want := []string{"system", "user", "assistant", "system", "tool", "tool_result", "user", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles=%v want %v", roles, want)
	}
	if session.Status != "success" || session.Iterations != 3 {
		t.Fatalf("status=%q iters=%d", session.Status, session.Iterations)
	}
}

func TestFlushSubAgentTrajectory_StampsAbortWhenNoOutcome(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sub-abort", "p", "m", "openai", "u", "coding", "coding_subagent", "parent", nil)
	r.Record("system", "sys", nil, "", "")
	// Mid-batch orphan that must be closed on abort flush.
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID: "orphan", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`},
		}},
	})
	if r.HasOutcome() {
		t.Fatal("expected no outcome before finish")
	}
	flushSubAgentTrajectory(r)
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "error" || !strings.Contains(session.Error, "aborted") {
		t.Fatalf("status=%q err=%q", session.Status, session.Error)
	}
	var closed bool
	for _, e := range session.Entries {
		if e.Role == "tool_result" && e.ToolCallID == "orphan" {
			closed = true
			if e.ToolOutcome != "failed" && e.Content != "subagent aborted" {
				// reason has no "cancel"/"pause" → outcome failed; content is the reason string.
				if e.Content != "subagent aborted" {
					t.Fatalf("orphan close = %+v", e)
				}
			}
		}
	}
	if !closed {
		t.Fatalf("expected orphan tool_result on abort flush: %+v", session.Entries)
	}
}

func TestSetOutcomeFromIMResponse_DetectsTaskCancelledText(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.Record("user", "q", nil, "", "")
	// Sparse cancel response (legacy cancelledExitResponse shape before Error was set).
	r.SetOutcomeFromIMResponse(&IMAgentResponse{Text: "Task cancelled: do stuff"}, nil, 2, 1)
	r.mu.Lock()
	status, errMsg := r.session.Status, r.session.Error
	r.mu.Unlock()
	if status != "cancelled" || errMsg != "cancelled" {
		t.Fatalf("status=%q err=%q", status, errMsg)
	}
}

func TestSetOutcomeFromIMResponse_DoesNotWipePriorTokensWithZero(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.Record("user", "q", nil, "", "")
	r.SetOutcomeFromLoopResult(agent.LoopResult{
		Error:      "cancelled",
		Iterations: 4,
		ToolCalls:  3,
		Usage:      agent.TurnUsage{InputTokens: 100, OutputTokens: 50},
	})
	// Re-apply sparse IM response — token zeros must not wipe prior LoopResult usage.
	r.SetOutcomeFromIMResponse(&IMAgentResponse{Text: "Task cancelled."}, nil, -1, -1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session.InputTokens != 100 || r.session.OutputTokens != 50 {
		t.Fatalf("tokens wiped: %d/%d", r.session.InputTokens, r.session.OutputTokens)
	}
	if r.session.Iterations != 4 || r.session.ToolCallCount != 3 {
		t.Fatalf("counters wiped: iters=%d tools=%d", r.session.Iterations, r.session.ToolCallCount)
	}
	if r.session.Status != "cancelled" {
		t.Fatalf("status=%q", r.session.Status)
	}
}

func TestRecordEarlyStopToolResult_EmptyContentStillClosesSiblings(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	r.RecordEntry(TrajectoryEntry{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{
			{ID: "call-ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user", Arguments: `{}`}},
			{ID: "call-bash", Type: "function", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{}`}},
		},
	})
	// Empty content must not skip sibling close.
	r.RecordEarlyStopToolResult("call-ask", "ask_user", "   ")
	r.Flush()
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	var ask, bash bool
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call-ask":
			ask = true
			if e.ToolOutcome != "paused" || e.Content == "" {
				t.Fatalf("ask result = %+v", e)
			}
		case "call-bash":
			bash = true
			if e.ToolOutcome != "paused" {
				t.Fatalf("bash close = %+v", e)
			}
		}
	}
	if !ask || !bash {
		t.Fatalf("ask=%v bash=%v entries=%+v", ask, bash, session.Entries)
	}
}

func TestCloseUnpairedToolCalls_LegacyToolRoleResultIsNotOpen(t *testing.T) {
	r := NewTrajectoryRecorderForBaseDir(t.TempDir())
	r.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	// Legacy shape: role=tool with string content is a result, not a call.
	r.RecordEntry(TrajectoryEntry{Role: "tool", ToolCallID: "c1", ToolName: "bash", Content: "ok"})
	r.CloseUnpairedToolCalls("cancelled")
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.session.Entries {
		if e.Role == "tool_result" {
			t.Fatalf("should not synthesize close for legacy string tool result: %+v", r.session.Entries)
		}
	}
}

func TestRecordEarlyStopToolResult_PairsAskUser(t *testing.T) {
	dir := t.TempDir()
	r := NewTrajectoryRecorderForBaseDir(dir)
	r.StartSessionWithMeta("sess-early", "p", "m", "openai", "u", "desktop", "shared", "", nil)
	r.RecordLoopResult(agent.LoopResult{
		Text:            "Please choose",
		AskUser:         &agent.AskUserRequest{Question: "OK?"},
		PauseToolCallID: "call-ask",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "user", Content: "hi"},
			{
				Role: "assistant",
				ToolCalls: []interface{}{
					map[string]interface{}{
						"id": "call-ask",
						"function": map[string]interface{}{
							"name":      "ask_user",
							"arguments": `{"question":"OK?"}`,
						},
					},
				},
			},
		},
		Iterations: 1,
		ToolCalls:  1,
	})
	r.RecordEarlyStopToolResult("call-ask", "ask_user", "Please choose")
	r.Flush()
	entries, err := os.ReadDir(filepath.Join(dir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "paused" {
		t.Fatalf("status=%q", session.Status)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, e := range session.Entries {
		roles = append(roles, e.Role)
	}
	// leading user skipped by RecordLoopResult → assistant + expanded tool + early-stop result
	if strings.Join(roles, ",") != "assistant,tool,tool_result" {
		t.Fatalf("roles=%v", roles)
	}
	last := session.Entries[len(session.Entries)-1]
	if last.Role != "tool_result" || last.ToolCallID != "call-ask" || last.ToolName != "ask_user" {
		t.Fatalf("last=%+v", last)
	}
}
