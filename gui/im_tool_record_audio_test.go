package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestRecordAudioOpenedSessionResult(t *testing.T) {
	if got := recordAudioOpenedSessionResult(" 周会 "); got != "Opened recording session: 周会" {
		t.Fatalf("got %q", got)
	}
	if got := recordAudioOpenedSessionResult(""); !strings.Contains(got, "录音") {
		t.Fatalf("default title missing: %q", got)
	}
}

func TestRecordAudioUserFacingRejectText(t *testing.T) {
	if got := recordAudioUserFacingRejectText(recordAudioDesktopOnlyRejection(), "🎙️ 录音中"); !strings.Contains(got, "桌面") {
		t.Fatalf("desktop-only user text = %q", got)
	}
	concurrent := recordAudioConcurrentRejection("进行中")
	if got := recordAudioUserFacingRejectText(concurrent, ""); got != concurrent {
		t.Fatalf("concurrent = %q", got)
	}
	if got := recordAudioUserFacingRejectText("record_audio is blocked by gate", ""); got != "无法打开录音会话。" {
		t.Fatalf("english tool text = %q", got)
	}
}

func TestRewriteRecordAudioMarkerForSharedLoop_DesktopOK(t *testing.T) {
	h := &IMMessageHandler{}
	raw := agent.ToolRecordAudio(map[string]interface{}{"title": "周会"})
	if got := h.rewriteRecordAudioMarkerForSharedLoop("desktop-user", "desktop", raw); got != "" {
		t.Fatalf("desktop should keep marker, got rejection %q", got)
	}
}

func TestRewriteRecordAudioMarkerForSharedLoop_RejectsIM(t *testing.T) {
	h := &IMMessageHandler{}
	raw := agent.ToolRecordAudio(map[string]interface{}{"title": "周会"})
	got := h.rewriteRecordAudioMarkerForSharedLoop("im-user", "weixin", raw)
	if !strings.Contains(got, "desktop-only") {
		t.Fatalf("want desktop-only rejection, got %q", got)
	}
}

func TestFinalizeSharedLoopRecordAudioOpensUI(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	req := &agent.RecordAudioRequest{Title: "项目例会", Purpose: "讨论排期"}
	// Multi-tool batch: record_audio is first; PauseToolCallID must win over
	// name-fallback or last-id heuristics.
	history := []agent.ConversationEntry{
		{Role: "user", Content: "开始会议录音"},
		{
			Role:         "assistant",
			Content:      "",
			FinishReason: "tool_calls",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id":   "call_rec_shared",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "record_audio",
						"arguments": `{"title":"项目例会"}`,
					},
				},
				map[string]interface{}{
					"id":   "call_bash_last",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "bash",
						"arguments": `{"command":"echo x"}`,
					},
				},
			},
		},
	}
	loopResult := agent.LoopResult{
		Text:            agent.FormatRecordAudioForDisplay(req),
		RecordAudio:     req,
		PauseToolCallID: "call_rec_shared",
		HistoryDelta:    history,
		ToolCalls:       1,
		Iterations:      1,
		Usage:           agent.TurnUsage{InputTokens: 11, OutputTokens: 5},
	}
	trajDir := t.TempDir()
	recorder := NewTrajectoryRecorderForBaseDir(trajDir)
	recorder.StartSessionWithMeta("rec-shared-open", "Custom1", "m", "openai", "desktop-user", "desktop", "shared", "", nil)
	recorder.Record("system", "sys", nil, "", "")
	recorder.Record("user", "开始会议录音", nil, "", "")

	resp := h.finalizeSharedLoopRecordAudio(
		"desktop-user", "desktop", "开始会议录音", history, loopResult, "req-1", nil, nil, nil, recorder,
	)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource != imResponseSourceRecordAudio.String() {
		t.Fatalf("ResponseSource = %q, want record_audio", resp.ResponseSource)
	}
	if !strings.Contains(resp.Text, "项目例会") {
		t.Fatalf("display text = %q", resp.Text)
	}
	var foundTitle bool
	for _, f := range resp.Fields {
		if f.Label == "recording_title" && f.Value == "项目例会" {
			foundTitle = true
		}
	}
	if !foundTitle {
		t.Fatalf("missing recording_title field: %#v", resp.Fields)
	}
	rawPending, ok := h.pendingRecordAudio.Load("desktop-user")
	if !ok {
		t.Fatal("pending record audio not stored")
	}
	pending := rawPending.(*pendingRecordAudioState)
	if pending.Title != "项目例会" {
		t.Fatalf("pending title = %q", pending.Title)
	}
	if time.Since(pending.Timestamp) > time.Minute {
		t.Fatal("timestamp not set")
	}
	// Tool result must pair with PauseToolCallID, not the last batch id.
	var paired bool
	for _, e := range pending.History {
		if e.Role == "tool" && e.ToolCallID == "call_rec_shared" {
			paired = true
			break
		}
	}
	if !paired {
		t.Fatalf("pending history missing tool result for call_rec_shared: %#v", pending.History)
	}

	// Trajectory: pause pairing + sibling tool closed as paused.
	recorder.Flush()
	entries, err := os.ReadDir(filepath.Join(trajDir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, "trajectories", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "paused" {
		t.Fatalf("trajectory status=%q want paused", session.Status)
	}
	if session.InputTokens != 11 || session.OutputTokens != 5 {
		t.Fatalf("tokens=%d/%d", session.InputTokens, session.OutputTokens)
	}
	var recResult, bashClosed bool
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call_rec_shared":
			recResult = true
			if e.ToolName != "record_audio" || e.ToolOutcome != "paused" {
				t.Fatalf("record_audio result = %+v", e)
			}
		case "call_bash_last":
			bashClosed = true
			if e.ToolOutcome != "paused" {
				t.Fatalf("sibling close = %+v", e)
			}
		}
	}
	if !recResult {
		t.Fatalf("missing record_audio tool_result: %+v", session.Entries)
	}
	if !bashClosed {
		t.Fatalf("missing paused close for sibling bash: %+v", session.Entries)
	}
	// Assistant finish_reason survives HistoryDelta replay.
	for _, e := range session.Entries {
		if e.Role == "assistant" && e.FinishReason != "tool_calls" {
			t.Fatalf("assistant finish_reason=%q want tool_calls", e.FinishReason)
		}
	}
}

func TestFinalizeSharedLoopRecordAudio_RejectTrajectoryStampsError(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	req := &agent.RecordAudioRequest{Title: "周会"}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "录音"},
		{
			Role: "assistant",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id":       "call_rec_rej",
					"function": map[string]interface{}{"name": "record_audio", "arguments": `{"title":"周会"}`},
				},
			},
			FinishReason: "tool_calls",
		},
	}
	loopResult := agent.LoopResult{
		Text:            agent.FormatRecordAudioForDisplay(req),
		RecordAudio:     req,
		PauseToolCallID: "call_rec_rej",
		HistoryDelta:    history,
		Iterations:      1,
		ToolCalls:       1,
	}
	trajDir := t.TempDir()
	recorder := NewTrajectoryRecorderForBaseDir(trajDir)
	recorder.StartSessionWithMeta("rec-reject", "p", "m", "openai", "im-user", "weixin", "shared", "", nil)
	recorder.Record("user", "录音", nil, "", "")

	// Weixin rejects interactive recording UI.
	resp := h.finalizeSharedLoopRecordAudio(
		"im-user", "weixin", "录音", history, loopResult, "req-rej", nil, nil, nil, recorder,
	)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource == imResponseSourceRecordAudio.String() {
		t.Fatalf("IM must not open recording UI, source=%q", resp.ResponseSource)
	}
	recorder.Flush()
	entries, err := os.ReadDir(filepath.Join(trajDir, "trajectories"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(trajDir, "trajectories", entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "error" {
		t.Fatalf("status=%q want error (host rejected interactive pause)", session.Status)
	}
	var found bool
	for _, e := range session.Entries {
		if e.Role == "tool_result" && e.ToolCallID == "call_rec_rej" {
			found = true
			if e.ToolOutcome != "failed" {
				t.Fatalf("reject tool_result = %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("missing reject tool_result: %+v", session.Entries)
	}
}

func TestRewriteRecordAudioMarkerForSharedLoop_Concurrent(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "录"},
		{Role: "tool", Content: "Opened recording session: 进行中", ToolCallID: "tc-0"},
	}
	mem.Save("u1", history)
	h.pendingRecordAudio.Store("u1", &pendingRecordAudioState{
		Title:     "进行中",
		History:   history,
		Timestamp: time.Now(),
	})
	raw := agent.ToolRecordAudio(map[string]interface{}{"title": "第二次"})
	got := h.rewriteRecordAudioMarkerForSharedLoop("u1", "desktop", raw)
	if !strings.Contains(got, "已在进行中") {
		t.Fatalf("want concurrent rejection, got %q", got)
	}
}

func TestToolCallIDFromHistoryDeltaByName(t *testing.T) {
	delta := []agent.ConversationEntry{
		{Role: "user", Content: "录"},
		{
			Role: "assistant",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id": "call_rec",
					"function": map[string]interface{}{"name": "record_audio"},
				},
				map[string]interface{}{
					"id": "call_bash_last",
					"function": map[string]interface{}{"name": "bash"},
				},
			},
		},
	}
	if got := toolCallIDFromHistoryDeltaByName(delta, "record_audio"); got != "call_rec" {
		t.Fatalf("got %q, want call_rec (not last batch id)", got)
	}
	if got := toolCallIDFromHistoryDeltaByName(delta, "missing"); got != "" {
		t.Fatalf("missing name got %q", got)
	}
}

func TestFinalizeSharedLoopRecordAudio_NameFallbackWhenPauseIDEmpty(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	req := &agent.RecordAudioRequest{Title: "纪要"}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "录音"},
		{
			Role: "assistant",
			ToolCalls: []interface{}{
				map[string]interface{}{
					"id":       "call_rec_fb",
					"function": map[string]interface{}{"name": "record_audio"},
				},
				map[string]interface{}{
					"id":       "call_other",
					"function": map[string]interface{}{"name": "bash"},
				},
			},
		},
	}
	loopResult := agent.LoopResult{
		Text:         agent.FormatRecordAudioForDisplay(req),
		RecordAudio:  req,
		// PauseToolCallID intentionally empty — name-match fallback must work.
		HistoryDelta: history,
	}
	resp := h.finalizeSharedLoopRecordAudio(
		"desktop-user", "desktop", "录音", history, loopResult, "req-fb", nil, nil, nil, nil,
	)
	if resp == nil || resp.ResponseSource != imResponseSourceRecordAudio.String() {
		t.Fatalf("resp = %#v", resp)
	}
	rawPending, ok := h.pendingRecordAudio.Load("desktop-user")
	if !ok {
		t.Fatal("pending missing")
	}
	pending := rawPending.(*pendingRecordAudioState)
	var paired bool
	for _, e := range pending.History {
		if e.Role == "tool" && e.ToolCallID == "call_rec_fb" {
			paired = true
		}
	}
	if !paired {
		t.Fatalf("expected tool pair call_rec_fb, history=%#v", pending.History)
	}
}

func TestHandleAgentLoopRecordAudioRejectedOnIM(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	raw := agent.ToolRecordAudio(map[string]interface{}{"title": "周会"})
	for _, platform := range []string{"weixin", "feishu", "qqbot", "telegram", "scheduler"} {
		out := h.handleAgentLoopRecordAudioToolResult(
			"im-user-"+platform,
			platform,
			"帮我录音",
			raw,
			false,
			"tc-im",
			nil,
			nil,
			nil,
			func(string, interface{}, string, string) {},
		)
		if out.Response != nil {
			t.Fatalf("%s must not open interactive recording UI", platform)
		}
		if !strings.Contains(out.Result, "desktop-only") || !strings.Contains(out.Result, "too short") {
			t.Fatalf("%s: expected desktop-only rejection, got %q", platform, out.Result)
		}
		if _, ok := h.pendingRecordAudio.Load("im-user-" + platform); ok {
			t.Fatalf("%s must not store pending recording session", platform)
		}
	}
}

func TestHandleAgentLoopRecordAudioToolResultPausesForDesktop(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	raw := agent.ToolRecordAudio(map[string]interface{}{
		"title":   "项目例会",
		"purpose": "讨论排期",
	})
	out := h.handleAgentLoopRecordAudioToolResult(
		"desktop-user",
		"desktop",
		"",
		raw,
		false,
		"tc-1",
		nil,
		nil,
		nil,
		func(string, interface{}, string, string) {},
	)
	if out.Response == nil {
		t.Fatal("expected response to pause agent loop")
	}
	if out.Response.ResponseSource != imResponseSourceRecordAudio.String() {
		t.Fatalf("ResponseSource = %q", out.Response.ResponseSource)
	}
	if !strings.Contains(out.Response.Text, "项目例会") {
		t.Fatalf("display text missing title: %q", out.Response.Text)
	}
	// Success path must replace the internal marker with a friendly tool result.
	if agent.IsRecordAudioResult(out.Result) {
		t.Fatalf("Result still marker after open: %q", out.Result)
	}
	if !strings.Contains(out.Result, "Opened recording session") || !strings.Contains(out.Result, "项目例会") {
		t.Fatalf("Result = %q", out.Result)
	}
	rawPending, ok := h.pendingRecordAudio.Load("desktop-user")
	if !ok {
		t.Fatal("pending record audio not stored")
	}
	pending := rawPending.(*pendingRecordAudioState)
	if pending.Title != "项目例会" {
		t.Fatalf("pending title = %q", pending.Title)
	}
	if time.Since(pending.Timestamp) > time.Minute {
		t.Fatal("timestamp not set")
	}
}

func TestHandleAgentLoopRecordAudioRejectsConcurrentSession(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "开始录音"},
		{Role: "tool", Content: "Opened recording session: 进行中", ToolCallID: "tc-0"},
	}
	h.pendingRecordAudio.Store("desktop-user", &pendingRecordAudioState{
		Title:     "进行中",
		History:   history,
		Timestamp: time.Now(),
	})
	raw := agent.ToolRecordAudio(map[string]interface{}{"title": "第二次"})
	out := h.handleAgentLoopRecordAudioToolResult(
		"desktop-user", "desktop", "", raw, false, "tc-2",
		nil, history, nil, func(string, interface{}, string, string) {},
	)
	if out.Response != nil {
		t.Fatal("expected no UI pause response for concurrent session")
	}
	if !strings.Contains(out.Result, "已在进行中") {
		t.Fatalf("result = %q, want concurrent rejection", out.Result)
	}
	// Original pending must remain.
	rawPending, ok := h.pendingRecordAudio.Load("desktop-user")
	if !ok {
		t.Fatal("pending should still exist")
	}
	if rawPending.(*pendingRecordAudioState).Title != "进行中" {
		t.Fatalf("pending title overwritten: %q", rawPending.(*pendingRecordAudioState).Title)
	}
}

func TestExtractRecordingFieldFromReport(t *testing.T) {
	report := "[Recording completed]\nstatus: stopped\npath: C:\\Users\\me\\a.wav\nduration_sec: 12.5\n"
	if got := extractRecordingFieldFromReport(report, "path"); got != `C:\Users\me\a.wav` {
		t.Fatalf("path = %q", got)
	}
	if got := extractRecordingFieldFromReport(report, "STATUS"); got != "stopped" {
		t.Fatalf("status = %q", got)
	}
	if got := extractRecordingFieldFromReport(report, "duration_sec"); got != "12.5" {
		t.Fatalf("duration_sec = %q", got)
	}
	if got := extractRecordingFieldFromReport(report, "missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
}

func TestConsumePendingRecordAudioAnswer(t *testing.T) {
	h := &IMMessageHandler{}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "帮我录一下讨论"},
		{Role: "assistant", Content: "正在打开录音"},
		{Role: "tool", Content: "Opened recording session: 讨论", ToolCallID: "tc-1"},
	}
	h.pendingRecordAudio.Store("u1", &pendingRecordAudioState{
		Title:     "讨论",
		History:   history,
		Timestamp: time.Now(),
	})
	// Casual chat while recording must NOT consume pending.
	if ctx, ok := h.consumePendingRecordAudioAnswer("u1", "顺便问下天气", history); ok || ctx != "" {
		t.Fatalf("casual message should not consume pending, ok=%v ctx=%q", ok, ctx)
	}
	if _, still := h.pendingRecordAudio.Load("u1"); !still {
		t.Fatal("pending should remain after casual message")
	}

	report := "[Recording completed]\nstatus: stopped\npath: C:\\tmp\\a.wav\nduration_sec: 12.0\n"
	ctx, ok := h.consumePendingRecordAudioAnswer("u1", report, history)
	if !ok {
		t.Fatal("expected consume success")
	}
	if !strings.Contains(ctx, "record_audio") || !strings.Contains(ctx, "C:\\tmp\\a.wav") {
		t.Fatalf("context = %q", ctx)
	}
	if _, still := h.pendingRecordAudio.Load("u1"); still {
		t.Fatal("pending should be cleared")
	}
}

func TestIsRecordAudioCompletionReport(t *testing.T) {
	if !isRecordAudioCompletionReport("[Recording completed]\nstatus: stopped\n") {
		t.Fatal("marker should match")
	}
	if !isRecordAudioCompletionReport("status: cancelled\nerror: closed") {
		t.Fatal("status+error should match")
	}
	if !isRecordAudioCompletionReport("status: stopped\npath: C:\\a.wav\n") {
		t.Fatal("status+path should match")
	}
	if isRecordAudioCompletionReport("status: stopped\n") {
		t.Fatal("status alone must not match")
	}
	if isRecordAudioCompletionReport("停止") {
		t.Fatal("bare stop must not match (IM meeting recording removed)")
	}
	if isRecordAudioCompletionReport("今天天气怎么样") {
		t.Fatal("casual chat must not match")
	}
}

func TestHasActivePendingRecordAudio(t *testing.T) {
	h := &IMMessageHandler{}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "录一下"},
		{Role: "tool", Content: "Opened recording session: 会", ToolCallID: "tc-1"},
	}
	if h.hasActivePendingRecordAudio("u1", history) {
		t.Fatal("expected no pending")
	}
	h.pendingRecordAudio.Store("u1", &pendingRecordAudioState{
		Title:     "会",
		History:   history,
		Timestamp: time.Now(),
	})
	if !h.hasActivePendingRecordAudio("u1", history) {
		t.Fatal("expected active pending")
	}
}
