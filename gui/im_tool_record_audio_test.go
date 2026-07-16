package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

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
			func(string, interface{}) {},
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
		func(string, interface{}) {},
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
		nil, history, nil, func(string, interface{}) {},
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
