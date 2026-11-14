package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type remoteStreamCapture struct {
	payloads []map[string]any
}

func (c *remoteStreamCapture) sink(payload map[string]any) {
	c.payloads = append(c.payloads, payload)
}

func (c *remoteStreamCapture) events() []string {
	out := make([]string, 0, len(c.payloads))
	for _, p := range c.payloads {
		name, _ := p["event"].(string)
		out = append(out, name)
	}
	return out
}

func streamSessionKey() string {
	return projectSessionOwnerID("D:\\proj\\demo")
}

func TestRemoteStreamRelayMirrorsLanesAndSettles(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "〔远控〕你好", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01先想想", Sequence: 2,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "答案", Sequence: 3,
	})
	app.relayRemoteStreamEvent("ai-assistant-stream-done", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Sequence: 4,
	})

	got := cap.events()
	want := []string{"round_started", "thinking", "thinking", "thinking", "token", "stream_done"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	// Reasoning streams while the block is open: the first token opens the
	// box, the collected text rides a delta, and the close (triggered here by
	// the first content delta) carries the same text as ground truth.
	thinkDelta := cap.payloads[2]
	if thinkDelta["state"] != "delta" || thinkDelta["text"] != "先想想" {
		t.Fatalf("thinking delta payload = %v", thinkDelta)
	}
	thinkDone := cap.payloads[3]
	if thinkDone["state"] != "done" || thinkDone["text"] != "先想想" {
		t.Fatalf("thinking done payload = %v", thinkDone)
	}
	if cap.payloads[4]["text"] != "答案" {
		t.Fatalf("token payload = %v", cap.payloads[4])
	}
	for _, p := range cap.payloads {
		if p["grant_id"] != "g1" {
			t.Fatalf("payload missing grant_id: %v", p)
		}
		if p["project_path"] == "" {
			t.Fatalf("payload missing project_path: %v", p)
		}
	}

	snap := reg.liveSnapshot(session)
	if snap == nil || snap.RequestID != "r1" || !snap.StreamDone {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Parts) != 2 || snap.Parts[0].Kind != "thinking" || !snap.Parts[0].Done || snap.Parts[1].Kind != "text" || snap.Parts[1].Text != "答案" {
		t.Fatalf("snapshot parts = %+v", snap.Parts)
	}
}

func TestRemoteStreamHeartbeatKeepsThinkingOpen(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01先想想", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: imHeartbeatMsg, Sequence: 2,
	})

	got := cap.events()
	want := []string{"thinking", "thinking", "heartbeat"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	// The keepalive is liveness only: no reasoning text, no state change.
	if _, ok := cap.payloads[2]["text"]; ok {
		t.Fatalf("heartbeat carried text: %v", cap.payloads[2])
	}
	snap := reg.liveSnapshot(session)
	if snap == nil || len(snap.Parts) != 1 || snap.Parts[0].Kind != "thinking" || snap.Parts[0].Done {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRemoteWorkbenchStepsRelayMirrorsSnapshot(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteWorkbenchSteps(session, map[string]interface{}{
		"project_path":   "D:\\proj\\demo",
		"steps_revision": 3,
		"step_statuses": []interface{}{
			map[string]interface{}{"index": 1, "title": "plan", "status": "running"},
		},
	})

	got := cap.events()
	if len(got) != 1 || got[0] != "workbench_steps" {
		t.Fatalf("events = %v", got)
	}
	wb, ok := cap.payloads[0]["workbench"].(map[string]interface{})
	if !ok || wb["steps_revision"] != 3 {
		t.Fatalf("workbench payload = %v", cap.payloads[0])
	}
}

func TestRemoteStreamReasoningStaysInThinkingLaneAfterReset(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 1,
	})
	// A rejected draft appends a fresh text part while reasoning continues —
	// the next \x01 delta must land in the open thinking part, not the text tail.
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x02重写", Sequence: 2,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01再想", Sequence: 3,
	})

	snap := reg.liveSnapshot(session)
	if snap == nil || len(snap.Parts) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Parts[0].Kind != "thinking" || snap.Parts[0].Done || snap.Parts[0].Text != "想想再想" {
		t.Fatalf("thinking part = %+v", snap.Parts[0])
	}
	if snap.Parts[1].Kind != "text" || snap.Parts[1].Text != "重写" {
		t.Fatalf("text part = %+v", snap.Parts[1])
	}
}

func TestRemoteStreamRelayIgnoresUnwatchedSessions(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	reg.watch("g1", streamSessionKey())

	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: "desktop-user:elsewhere", RequestID: "rX", Text: "nope",
	})
	if len(cap.payloads) != 0 {
		t.Fatalf("unwatched session leaked %d events", len(cap.payloads))
	}
}

func TestRemoteStreamCoalesceFlushAndUnwatch(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "hello", Sequence: 1,
	})
	// Content deltas ride the coalesce window: nothing on the wire yet.
	if len(cap.payloads) != 0 {
		t.Fatalf("content delta escaped the coalesce window")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(cap.payloads) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(cap.payloads) != 1 || cap.payloads[0]["event"] != "token" || cap.payloads[0]["text"] != "hello" {
		t.Fatalf("flushed payloads = %v", cap.payloads)
	}

	reg.unwatch("g1", session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "after", Sequence: 2,
	})
	time.Sleep(200 * time.Millisecond)
	if len(cap.payloads) != 1 {
		t.Fatalf("unwatched session still relays: %v", cap.payloads)
	}
}

func TestRemoteStreamResponseStripsMediaAndSettles(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{
		SessionKey:      session,
		Text:            "完成",
		VoiceData:       strings.Repeat("A", 300<<10),
		ThumbnailBase64: strings.Repeat("B", 300<<10),
		FileData:        strings.Repeat("C", 300<<10),
		VoiceParts:      []IMVoicePart{{Data: strings.Repeat("D", 100<<10), FileName: "a.ogg"}},
		LocalFilePaths:  []string{"out/report.md"},
		ResponseSource:  "test",
	})

	got := cap.events()
	if len(got) != 1 || got[0] != "response" {
		t.Fatalf("events = %v", got)
	}
	raw, _ := cap.payloads[0]["response"].(json.RawMessage)
	var resp IMAgentResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("response payload unmarshal: %v", err)
	}
	if resp.Text != "完成" || resp.VoiceData != "" || resp.FileData != "" || resp.ThumbnailBase64 != "" || len(resp.VoiceParts) != 0 {
		t.Fatalf("media leaked into mirror: %+v", resp)
	}
	if len(resp.LocalFilePaths) != 1 || resp.LocalFilePaths[0] != "out/report.md" {
		t.Fatalf("file paths lost: %+v", resp.LocalFilePaths)
	}
	snap := reg.liveSnapshot(session)
	// A responded turn is settled — the snapshot is gone for the next watcher.
	if snap != nil {
		t.Fatalf("settled turn still snapshots: %+v", snap)
	}
}

func TestRemoteStreamStaleResponseKeepsLiveTurn(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watch("g1", session)

	// Round r2 is live and has accumulated state when a late response for the
	// previous round r1 lands. The response event still relays — the
	// controller folds by request_id — but it must not wipe r2's snapshot.
	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r2", DisplayText: "第二轮", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r2", Text: "进展中", Sequence: 2,
	})
	// A non-token event commits the pending text run into the snapshot parts.
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r2", Text: "执行工具", Sequence: 3,
	})
	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{
		SessionKey: session,
		Text:       "上一轮完成",
	})

	got := cap.events()
	if got[len(got)-1] != "response" {
		t.Fatalf("last event = %v, want response", got)
	}
	last := cap.payloads[len(cap.payloads)-1]
	if last["request_id"] != "r1" {
		t.Fatalf("response request_id = %v, want r1", last["request_id"])
	}
	snap := reg.liveSnapshot(session)
	if snap == nil || snap.RequestID != "r2" || snap.Responded {
		t.Fatalf("live turn lost to stale response: %+v", snap)
	}
	found := false
	for _, part := range snap.Parts {
		if part.Kind == "text" && part.Text == "进展中" {
			found = true
		}
	}
	if !found {
		t.Fatalf("live parts wiped: %+v", snap.Parts)
	}
}

func TestRemoteControlWatchOps(t *testing.T) {
	app := &App{}
	// watch_task without a grant id is the version-skew path: the Hub that
	// cannot name a grant cannot route its stream either.
	if _, err := app.remoteControlWatchTask("", json.RawMessage(`{"project_path":"x"}`)); err == nil {
		t.Fatalf("watch without grant id must fail")
	}
	if _, err := app.remoteControlUnwatchTask("g1", json.RawMessage(`{"project_path":"x"}`)); err != nil {
		t.Fatalf("unwatch must be idempotent: %v", err)
	}
}
