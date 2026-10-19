package guiapp

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func applyAssistantTokenLanes(deltas []string) (content string, reasoning string, rejects int) {
	for _, delta := range deltas {
		if strings.HasPrefix(delta, rejectedRoundContentToken) {
			content = ""
			rejects++
			continue
		}
		if strings.HasPrefix(delta, "\x01") {
			reasoning += strings.TrimPrefix(delta, "\x01")
			continue
		}
		content += delta
	}
	return content, reasoning, rejects
}

func coalescedTokenTexts(items []coalescedAssistantToken) []string {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.Text
	}
	return texts
}

func TestCoalesceAssistantTokenDeltasSettledTextAndFewerEmits(t *testing.T) {
	deltas := []string{"Hel", "lo", " ", "\x01thi", "\x01nk", "!", "\x02", "kept"}
	got := coalesceAssistantTokenDeltas(deltas)
	if len(got) == 0 || len(got) >= len(deltas) {
		t.Fatalf("emits = %d, want 1..%d (%#v)", len(got), len(deltas)-1, got)
	}
	texts := coalescedTokenTexts(got)
	wantContent, wantReasoning, wantRejects := applyAssistantTokenLanes(deltas)
	gotContent, gotReasoning, gotRejects := applyAssistantTokenLanes(texts)
	if gotContent != wantContent || gotReasoning != wantReasoning || gotRejects != wantRejects {
		t.Fatalf("settled lanes content=%q reasoning=%q rejects=%d, want content=%q reasoning=%q rejects=%d (payloads=%#v)",
			gotContent, gotReasoning, gotRejects, wantContent, wantReasoning, wantRejects, got)
	}
	if got[1].Text != "\x01think" {
		t.Fatalf("reasoning payload = %q, want one leading lane marker", got[1].Text)
	}
	// A join carries the original pieces. A lone delta, including the
	// rejected-round marker, does not, so the UI can still trim a snapshot.
	if !sameStrings(got[0].Deltas, []string{"Hel", "lo", " "}) || !sameStrings(got[1].Deltas, []string{"\x01thi", "\x01nk"}) {
		t.Fatalf("joined pieces = %#v", got)
	}
	if got[2].Deltas != nil || got[3].Deltas != nil || got[4].Deltas != nil {
		t.Fatalf("single payloads must not carry deltas: %#v", got)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestStreamEmitterFlushesTokenBurstAsFewerEvents(t *testing.T) {
	var names []string
	var events []AIAssistantStreamEvent
	emitter := newAIAssistantStreamEventEmitter(nil, "req-1", "desktop-user")
	emitter.coalesceWindow = time.Hour
	emitter.emitFn = func(name string, event AIAssistantStreamEvent) {
		names = append(names, name)
		events = append(events, event)
	}

	deltas := []string{"Hel", "lo", " ", "wor", "ld"}
	for _, delta := range deltas {
		emitter.emit("ai-assistant-token", delta)
	}
	if len(events) != 0 {
		t.Fatalf("burst emitted %d events before flush", len(events))
	}
	emitter.flushTokens()
	if len(events) != 1 {
		t.Fatalf("flush emits = %d, want 1", len(events))
	}
	if events[0].Text != "Hello world" || names[0] != "ai-assistant-token" || !sameStrings(events[0].Deltas, deltas) {
		t.Fatalf("first event = %s %#v", names[0], events[0])
	}
	wire, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(wire, []byte(`"deltas":`)) {
		t.Fatalf("merged payload wire = %s", wire)
	}
	if events[0].RequestID != "req-1" || events[0].SessionKey != "desktop-user" || events[0].Sequence != 1 {
		t.Fatalf("routing lost: %#v", events[0])
	}

	emitter.emit("ai-assistant-token", "\x01a")
	emitter.emit("ai-assistant-token", "\x01b")
	emitter.emit("ai-assistant-progress", "working")
	if len(events) != 3 {
		t.Fatalf("events after progress = %d, want 3", len(events))
	}
	if events[1].Text != "\x01ab" || events[1].Sequence != 2 || !sameStrings(events[1].Deltas, []string{"\x01a", "\x01b"}) {
		t.Fatalf("reasoning event = %#v", events[1])
	}
	if names[2] != "ai-assistant-progress" || events[2].Text != "working" || events[2].Sequence != 3 {
		t.Fatalf("progress event = %s %#v", names[2], events[2])
	}
	emitter.flushTokens()
	if len(events) != 3 {
		t.Fatalf("second flush emitted again: %d events", len(events))
	}

	emitter.emit("ai-assistant-token", "Z")
	emitter.flushTokens()
	if len(events) != 4 || events[3].Text != "Z" || events[3].Deltas != nil {
		t.Fatalf("single delta = %#v", events)
	}
	single, err := json.Marshal(events[3])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(single, []byte("deltas")) {
		t.Fatalf("single delta wire = %s", single)
	}
}

// TestStreamEmitterKeepsDrainedBatchAheadOfControlEvent blocks after the batch
// is drained and before it is sent. stream-done must not finish in that window.
// That fails when flush drops the pending lock before taking the send lock:
// the control event then observes an empty queue and goes out first.
func TestStreamEmitterKeepsDrainedBatchAheadOfControlEvent(t *testing.T) {
	var mu sync.Mutex
	var order []string
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	emitter := newAIAssistantStreamEventEmitter(nil, "req-1", "desktop-user")
	emitter.coalesceWindow = time.Hour
	emitter.emitFn = func(name string, event AIAssistantStreamEvent) {
		mu.Lock()
		order = append(order, name+"\x00"+event.Text)
		mu.Unlock()
	}
	emitter.beforeDeliver = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	emitter.emit("ai-assistant-token", "Alpha ")
	emitter.emit("ai-assistant-token", "beta")

	flushDone := make(chan struct{})
	go func() {
		emitter.flushTokens()
		close(flushDone)
	}()
	<-entered

	controlDone := make(chan struct{})
	go func() {
		emitter.emit("ai-assistant-stream-done", "ok")
		close(controlDone)
	}()
	for i := 0; i < 100000; i++ {
		select {
		case <-controlDone:
			t.Fatal("stream-done finished while the drained token batch was still held")
		default:
			runtime.Gosched()
		}
	}

	close(release)
	<-flushDone
	<-controlDone

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 {
		t.Fatalf("order = %#v", order)
	}
	if !strings.HasPrefix(order[0], "ai-assistant-token\x00") || !strings.Contains(order[0], "Alpha beta") {
		t.Fatalf("token batch = %q", order[0])
	}
	if order[1] != "ai-assistant-stream-done\x00ok" {
		t.Fatalf("control event = %q, want it after the token batch", order[1])
	}
}
