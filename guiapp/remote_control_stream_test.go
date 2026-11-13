package guiapp

import (
	"encoding/json"
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
	want := []string{"round_started", "thinking", "thinking", "token", "stream_done"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	// Reasoning ships only once, when the block closes, carrying its text —
	// here the closing trigger is the first content delta after it.
	thinkDone := cap.payloads[2]
	if thinkDone["state"] != "done" || thinkDone["text"] != "先想想" {
		t.Fatalf("thinking done payload = %v", thinkDone)
	}
	if cap.payloads[3]["text"] != "答案" {
		t.Fatalf("token payload = %v", cap.payloads[3])
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

