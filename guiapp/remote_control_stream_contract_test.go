package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRemoteStreamProto1EnvelopeUnchanged(t *testing.T) {
	t.Setenv("MACLAW_REMOTE_STREAM_PROTO", "1")
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, negotiateStreamProto(2))

	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01先想想", Sequence: 2,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "正文", Sequence: 9,
	})
	for _, p := range cap.snapshot() {
		if p["event"] == "token" {
			t.Fatalf("proto 1 content left the 150ms window early: %v", eventNames(cap.snapshot()))
		}
	}
	payloads := cap.waitN(2, 2*time.Second)
	// started + delta are immediate; the body waits out the 150ms window.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		payloads = cap.snapshot()
		found := false
		for _, p := range payloads {
			if p["event"] == "token" && p["text"] == "正文" {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var sawDelta bool
	for _, p := range payloads {
		for _, key := range []string{"proto", "cursor", "cursor_from", "lane", "coding", "pieces", "terminal"} {
			if _, ok := p[key]; ok {
				t.Fatalf("proto 1 payload has %s: %v", key, p)
			}
		}
		if p["event"] == "snapshot" || p["event"] == "card" {
			t.Fatalf("proto 1 emitted %v", p["event"])
		}
		if p["event"] == "thinking" && p["state"] == "delta" && p["text"] == "先想想" {
			sawDelta = true
		}
	}
	if !sawDelta {
		t.Fatalf("missing thinking delta: %v", payloads)
	}
}

func TestRemoteStreamProto2CursorCoalesceAndProgress(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)

	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "开始", Sequence: 100,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 200,
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "执行工具", Sequence: 300,
	})
	app.relayRemoteWorkbenchSteps(session, map[string]any{"steps_revision": 1})
	app.emitAIContinuationEvent(AIContinuationEvent{
		RequestID: "credential-card-1", SessionKey: session, Kind: "confirmation", Text: "确认执行",
		Confirmation: &IMResponseConfirmation{ID: "1", Summary: "写文件"},
		Actions:      []IMResponseAction{{Label: "确认", Command: "__confirm_execution__"}},
	})
	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{SessionKey: session, Text: "完成"})

	deadline := time.Now().Add(2 * time.Second)
	var payloads []map[string]any
	for time.Now().Before(deadline) {
		payloads = cap.snapshot()
		if hasEvent(payloads, "response") && hasEvent(payloads, "card") && hasEvent(payloads, "workbench_steps") && hasEvent(payloads, "progress") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var last uint64
	saw := map[string]bool{}
	for i, p := range payloads {
		name, _ := p["event"].(string)
		saw[name] = true
		if p["state"] == "done" && name == "thinking" {
			// Progress must not seal thinking. A later response may.
			for j := 0; j < i; j++ {
				if payloads[j]["event"] == "progress" {
					goto nextPayload
				}
			}
			t.Fatalf("thinking done before progress: %v", payloads)
		}
	nextPayload:
		cur, _ := asUint(p["cursor"])
		seq, _ := asUint(p["seq"])
		if cur == 0 {
			t.Fatalf("missing cursor: %v", p)
		}
		if cur <= last {
			t.Fatalf("cursor did not increase: %v then %v", last, p)
		}
		last = cur
		if seq > 0 && seq == cur {
			t.Fatalf("cursor reused seq: %v", p)
		}
	}
	for _, name := range []string{"round_started", "thinking", "progress", "workbench_steps", "card", "response"} {
		if !saw[name] {
			t.Fatalf("missing %s in %v", name, eventNames(payloads))
		}
	}
	var progressIdx, doneBefore int
	for i, p := range payloads {
		if p["event"] == "progress" {
			progressIdx = i
		}
	}
	for i, p := range payloads {
		if i < progressIdx && p["event"] == "thinking" && p["state"] == "done" {
			doneBefore++
		}
	}
	if doneBefore != 0 {
		t.Fatalf("progress sealed thinking: %v", eventNames(payloads))
	}

	// Body and reasoning each coalesce at 80ms. A content piece seals an open
	// thought, so the two lanes are published separately. Keep each burst under
	// the 512 queue: a full queue's resync snapshots are a different path and
	// would hide whether the lanes actually batched.
	publish := func(thinking bool, n int) {
		for i := 0; i < n; i++ {
			ev := AIAssistantStreamEvent{SessionKey: session, RequestID: "r1", Sequence: 900}
			if thinking {
				ev.Deltas = []string{"\x01再"}
			} else {
				ev.Text = "字"
				ev.Sequence = 901
			}
			app.relayRemoteStreamEvent("ai-assistant-token", ev)
		}
	}
	before := len(cap.snapshot())
	start := time.Now()
	publish(true, 40)
	time.Sleep(120 * time.Millisecond)
	thinkFrames := len(cap.snapshot()) - before
	publish(false, 40)
	time.Sleep(200 * time.Millisecond)
	added := len(cap.snapshot()) - before
	bodyFrames := added - thinkFrames
	elapsed := time.Since(start)
	if thinkFrames <= 0 || bodyFrames <= 0 {
		t.Fatalf("lanes produced think=%d body=%d events=%v", thinkFrames, bodyFrames, eventNames(cap.snapshot()[before:]))
	}
	// One frame per publish means the 80ms window did not batch.
	if thinkFrames >= 20 || bodyFrames >= 20 {
		t.Fatalf("80ms coalesce did not batch: think=%d body=%d from 40 publishes each", thinkFrames, bodyFrames)
	}
	rate := float64(added) / elapsed.Seconds()
	if rate > 30 {
		t.Fatalf("80ms coalesce produced %d frames in %s (%.1f/s)", added, elapsed, rate)
	}
}

func TestRemoteStreamPublishBeforeWireTimer(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	var mu sync.Mutex
	var got []string
	app.addViewerSubscriber(func(ev viewerEvent) {
		mu.Lock()
		got = append(got, ev.Event)
		mu.Unlock()
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "立刻", Sequence: 1,
	})
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n == 0 {
		t.Fatal("in-process subscriber waited on the wire")
	}
	if len(cap.snapshot()) != 0 {
		t.Fatal("wire timer fired before the local subscriber returned")
	}
}

func TestRemoteStreamNilHubPublishesLocally(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	session := streamSessionKey()
	reg.watch("g1", session)
	var got string
	app.addViewerSubscriber(func(ev viewerEvent) { got = ev.Text })
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "本地", Sequence: 1,
	})
	if got != "本地" {
		t.Fatalf("local text = %q", got)
	}
	if reg.wireQueue != nil {
		t.Fatal("nil hub still enqueued")
	}
}

func TestRemoteStreamNoWatchDoesNotEnqueue(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	var got int
	app.addViewerSubscriber(func(viewerEvent) { got++ })
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: streamSessionKey(), RequestID: "r1", Text: "无人看", Sequence: 1,
	})
	time.Sleep(50 * time.Millisecond)
	if got != 1 {
		t.Fatalf("subscriber count = %d", got)
	}
	if len(cap.snapshot()) != 0 || reg.wireQueue != nil {
		t.Fatal("unwatched session enqueued")
	}
}

func TestRemoteStreamReducerFlagOffSkipsViewerEvent(t *testing.T) {
	t.Setenv("MACLAW_VIEWER_REDUCER", "0")
	app := &App{}
	var names []string
	app.testEmit = func(name string, _ ...interface{}) { names = append(names, name) }
	var got int
	app.addViewerSubscriber(func(viewerEvent) { got++ })
	app.publishViewerEvent(viewerEvent{SessionKey: streamSessionKey(), Event: "token", Text: "x"})
	if got != 1 {
		t.Fatalf("subscriber = %d", got)
	}
	for _, name := range names {
		if name == "viewer-event" {
			t.Fatal("MACLAW_VIEWER_REDUCER=0 emitted viewer-event")
		}
	}
}

func TestRemoteStreamBtwDoesNotEnterContract(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	reg.watch("g1", streamSessionKey())
	var names []string
	app.testEmit = func(name string, _ ...interface{}) { names = append(names, name) }
	var got int
	app.addViewerSubscriber(func(viewerEvent) { got++ })
	app.emitAIAssistantStreamEvent("ai-btw-token", AIAssistantStreamEvent{
		SessionKey: streamSessionKey(), RequestID: "r1", Text: "旁路", Sequence: 1,
	})
	time.Sleep(50 * time.Millisecond)
	if got != 0 || len(cap.snapshot()) != 0 {
		t.Fatalf("btw entered contract subscriber=%d wire=%d", got, len(cap.snapshot()))
	}
	for _, name := range names {
		if name == "viewer-event" {
			t.Fatal("btw emitted viewer-event")
		}
	}
}

func TestRemoteStreamInfraWriteLockDoesNotBlockPublish(t *testing.T) {
	app := &App{}
	app.ensureRemoteWatchRegistry()
	var got string
	app.addViewerSubscriber(func(ev viewerEvent) { got = ev.Event })
	app.remoteInfraMu.Lock()
	start := time.Now()
	app.publishViewerEvent(viewerEvent{SessionKey: streamSessionKey(), Event: "heartbeat"})
	elapsed := time.Since(start)
	app.remoteInfraMu.Unlock()
	if elapsed > 100*time.Millisecond {
		t.Fatalf("publish blocked on remoteInfraMu for %s", elapsed)
	}
	if got != "heartbeat" {
		t.Fatalf("event = %q", got)
	}
}

func TestRemoteStreamQueueOverflowSetsResyncAndDoesNotBlockEmit(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	reg.holdWrite = make(chan struct{})
	sessionA := projectSessionOwnerID(`D:\proj\a`)
	sessionB := projectSessionOwnerID(`D:\proj\b`)
	reg.watchWithProto("ga", sessionA, 2)
	reg.watchWithProto("gb", sessionB, 2)
	var mu sync.Mutex
	var local []string
	app.addViewerSubscriber(func(ev viewerEvent) {
		mu.Lock()
		local = append(local, ev.SessionKey+"|"+ev.Event)
		mu.Unlock()
	})

	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: sessionB, RequestID: "b", DisplayText: "乙", Sequence: 1,
	})
	for i := 0; i < remoteStreamQueueCap-1; i++ {
		app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
			SessionKey: sessionA, RequestID: "a", Text: "步", Sequence: uint64(i + 2),
		})
	}
	start := time.Now()
	app.relayRemoteAssistantResponse("a", &IMAgentResponse{SessionKey: sessionA, Text: "终稿"})
	app.relayRemoteWorkbenchSteps(sessionA, map[string]any{"steps_revision": 4})
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("overflow publish blocked for %s", time.Since(start))
	}
	mu.Lock()
	joined := strings.Join(local, "\n")
	mu.Unlock()
	if !strings.Contains(joined, sessionA+"|response") || !strings.Contains(joined, sessionA+"|workbench_steps") {
		t.Fatalf("local subscriber missed overflowed events: %s", joined)
	}

	close(reg.holdWrite)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sessionHas(cap.snapshot(), sessionB, "round_started") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sessionHas(cap.snapshot(), sessionB, "round_started") {
		t.Fatal("the other session did not send while this session was gapped")
	}
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: sessionA, RequestID: "a", Text: "续", Sequence: 900,
	})
	deadline = time.Now().Add(3 * time.Second)
	var snap map[string]any
	for time.Now().Before(deadline) {
		for _, p := range cap.snapshot() {
			if p["session_key"] == sessionA && p["event"] == "snapshot" && p["resync"] == true {
				snap = p
				break
			}
		}
		if snap != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if snap == nil {
		t.Fatalf("gapped session next frame was not snapshot resync: %v", eventNames(cap.snapshot()))
	}
	live, _ := snap["live"].(*remoteLiveSnapshot)
	if live == nil || !live.Responded || !snapshotHasText(live, "终稿") {
		t.Fatalf("resync snapshot dropped the responded turn: %#v", snap["live"])
	}
	// watch_task still hides a settled turn. Only the resync carries it.
	if reg.liveSnapshot(sessionA) != nil {
		t.Fatal("watch_task live stayed after the dropped response")
	}
	state := reduceMarshaledViewerEvent(t, snap)
	if state.Body != "终稿" || state.Gap || !state.Responded || state.Last == 0 {
		t.Fatalf("reducer missed responded snapshot: body=%q last=%d gap=%v responded=%v", state.Body, state.Last, state.Gap, state.Responded)
	}
}

// A full queue of prefix tokens plus one dropped tail must reduce, in wire
// order, to prefix+tail. Folding the tail first and snapshotting at its
// cursor drops the prefix still sitting in the queue.
func TestRemoteStreamQueueOverflowKeepsPrefixBeforeDroppedTail(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	reg.holdWrite = make(chan struct{})
	session := projectSessionOwnerID(`D:\proj\overflow-prefix`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)

	coding := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T2", Title: "edit parser",
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: coding, Sequence: 1,
	})
	prefixN := remoteStreamQueueCap - 1
	for i := 0; i < prefixN; i++ {
		app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
			SessionKey: session, RequestID: "r1", Text: "前", Sequence: uint64(i + 2),
		})
	}
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "尾", Sequence: uint64(prefixN + 2),
	})
	close(reg.holdWrite)

	want := strings.Repeat("前", prefixN) + "尾"
	deadline := time.Now().Add(3 * time.Second)
	var got viewerReduceState
	for time.Now().Before(deadline) {
		got = reduceSessionWire(t, cap.snapshot(), session)
		if got.Body == want && !got.Gap && len(got.Timeline) == 1 && got.Timeline[0].TaskID == "T2" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got.Body != want || got.Gap || len(got.Timeline) != 1 || got.Timeline[0].TaskID != "T2" || got.Timeline[0].Title != "edit parser" || got.Timeline[0].RunID != "run-1" {
		t.Fatalf("wire order body=%q want=%q gap=%v timeline=%+v events=%v", got.Body, want, got.Gap, got.Timeline, eventNames(cap.snapshot()))
	}
	time.Sleep(200 * time.Millisecond)
	again := reduceSessionWire(t, cap.snapshot(), session)
	if again.Body != want || again.Gap || len(again.Timeline) != 1 {
		t.Fatalf("after coalesce body=%q gap=%v timeline=%+v frames=%v", again.Body, again.Gap, again.Timeline, eventNames(cap.snapshot()))
	}
	var snap map[string]any
	for _, p := range cap.snapshot() {
		if p["session_key"] == session && p["event"] == "snapshot" && p["resync"] == true {
			snap = p
		}
	}
	if snap == nil {
		t.Fatal("gapped session did not emit a resync snapshot")
	}
	alone := reduceMarshaledViewerEvent(t, snap)
	if alone.Body != want || alone.Gap || len(alone.Timeline) != 1 || alone.Timeline[0].TaskID != "T2" || alone.Last == 0 {
		t.Fatalf("snapshot alone body=%q gap=%v last=%d timeline=%+v", alone.Body, alone.Gap, alone.Last, alone.Timeline)
	}
}

// A queue that still holds cursor 5 must not resync at a dropped cursor 6.
// Cap 4 is the same cut as a full 512 queue with one older event left behind.
func TestRemoteStreamQueueOverflowPlanBatch(t *testing.T) {
	ev := func(cursor uint64, kind, text string) viewerEvent {
		return viewerEvent{SessionKey: "s", RequestID: "r", Event: kind, Text: text, Cursor: cursor, CursorFrom: cursor}
	}
	fold, snapshot := planOverflowBatchCap(
		[]viewerEvent{ev(5, "token", "五")},
		[]viewerEvent{ev(6, "token", "六")},
		4,
	)
	if snapshot {
		t.Fatal("snapshot moved past cursor 5 still queued")
	}
	if len(fold) != 2 || fold[0].Cursor != 5 || fold[0].Text != "五" || fold[1].Cursor != 6 || fold[1].Text != "六" {
		t.Fatalf("fold = %+v", fold)
	}
	var prefix []viewerEvent
	for cursor := uint64(1); cursor <= 4; cursor++ {
		prefix = append(prefix, ev(cursor, "token", "前"))
	}
	fold, snapshot = planOverflowBatchCap(prefix, []viewerEvent{ev(5, "response", "终稿")}, 4)
	if !snapshot {
		t.Fatal("quiescent full queue should snapshot")
	}
	if len(fold) != 5 || fold[4].Event != "response" || fold[4].Text != "终稿" {
		t.Fatalf("quiescent fold = %+v", fold)
	}
	for i := 0; i < 4; i++ {
		if fold[i].Cursor != uint64(i+1) || fold[i].Text != "前" {
			t.Fatalf("prefix[%d] = %+v", i, fold[i])
		}
	}
}

// The client has already applied cursors 1..4. Cursor 5 is still queued and
// cursor 6 was dropped. The shipped drain must fold 5 before 6, and the wire
// reduction must still contain 5's text.
func TestRemoteStreamQueueOverflowDrainKeepsLowerCursor(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	session := projectSessionOwnerID(`D:\proj\overflow-race`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)
	reg.pending = []viewerEvent{{
		SessionKey: session, RequestID: "r1", Event: "token", Lane: "content",
		Text: "五", Pieces: []string{"五"}, Cursor: 5, CursorFrom: 5,
	}}
	reg.dropped = map[string][]viewerEvent{
		session: {{
			SessionKey: session, RequestID: "r1", Event: "token", Lane: "content",
			Text: "六", Pieces: []string{"六"}, Cursor: 6, CursorFrom: 6,
		}},
	}
	reg.mu.Lock()
	frames, _ := reg.drainLocked()
	flushed := reg.flushSessionLocked(session)
	reg.mu.Unlock()
	for _, frame := range append(frames, flushed...) {
		wire.sink(frame)
	}
	state := emptyViewerReduceState()
	for cursor := uint64(1); cursor <= 4; cursor++ {
		state = reduceViewerEvent(state, map[string]any{
			"event": "token", "session_key": session,
			"cursor": cursor, "cursor_from": cursor, "text": "前",
		})
	}
	for _, frame := range wire.snapshot() {
		if frame["session_key"] != session {
			continue
		}
		state = reduceMarshaledOnto(t, state, frame)
	}
	if !strings.Contains(state.Body, "五") || state.Gap {
		t.Fatalf("queued cursor dropped body=%q gap=%v events=%v", state.Body, state.Gap, eventNames(wire.snapshot()))
	}
}

// A second publisher must not admit a higher cursor while the first publish
// is still inside its subscriber. Otherwise the writer can snapshot past the
// cursor that has been allocated but not queued.
func TestRemoteStreamPublishKeepsCursorOrderAcrossGoroutines(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	// Keep the writer off the queue until the assertion has read pending.
	reg.holdWrite = make(chan struct{})
	defer close(reg.holdWrite)
	session := projectSessionOwnerID(`D:\proj\order-gate`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)

	started := make(chan struct{})
	holdSub := make(chan struct{})
	app.addViewerSubscriber(func(ev viewerEvent) {
		if ev.Text != "先" {
			return
		}
		close(started)
		<-holdSub
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		app.publishViewerEvent(viewerEvent{SessionKey: session, Event: "token", Lane: "content", Text: "先"})
	}()
	<-started
	secondDone := make(chan struct{})
	go func() {
		app.publishViewerEvent(viewerEvent{SessionKey: session, Event: "token", Lane: "content", Text: "后"})
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("higher cursor was published while the lower one still held the session gate")
	case <-time.After(80 * time.Millisecond):
	}
	reg.mu.Lock()
	for _, ev := range reg.pending {
		if ev.Text == "后" {
			reg.mu.Unlock()
			t.Fatal("higher cursor admitted before the lower publish finished")
		}
	}
	reg.mu.Unlock()
	close(holdSub)
	<-secondDone
	wg.Wait()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.pending) < 2 || reg.pending[0].Text != "先" || reg.pending[1].Text != "后" || reg.pending[0].Cursor >= reg.pending[1].Cursor {
		t.Fatalf("pending = %+v", reg.pending)
	}
}

// A frame folded into outbound still has to leave before a higher cursor
// that this same drain already holds. Appending it delivers the higher
// cursor first. The reducer gaps, then the lower frame clears that gap,
// and the skipped token is gone. Another session's cursor is not a sort key.
func TestRemoteStreamOutboundKeepsCursorOrder(t *testing.T) {
	session := projectSessionOwnerID(`D:\proj\outbound-order`)
	other := projectSessionOwnerID(`D:\proj\outbound-other`)
	merged := mergeOutboundFrames(
		[]map[string]any{
			{"session_key": session, "cursor": uint64(6), "text": "乙"},
			{"session_key": other, "cursor": uint64(1), "text": "他"},
			{"session_key": session, "cursor": uint64(8), "text": "终"},
		},
		[]map[string]any{
			{"session_key": session, "cursor": uint64(5), "text": "甲"},
			{"session_key": session, "cursor": uint64(9), "text": "余"},
		},
	)
	want := []string{"甲", "乙", "他", "终", "余"}
	if len(merged) != len(want) {
		t.Fatalf("merged = %#v", merged)
	}
	for i, text := range want {
		if viewerText(merged[i]["text"]) != text {
			t.Fatalf("merged order = %#v", merged)
		}
	}

	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	hold := make(chan struct{})
	reg.holdWrite = hold
	closed := false
	defer func() {
		if !closed {
			close(hold)
		}
	}()
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	viewerThinkOpen.Delete(other)
	viewerThinkText.Delete(other)
	reg.watchWithProto("g1", session, 2)
	reg.watchWithProto("g1", other, 2)
	// One drain must own both: the lower cursor is already folded, and the
	// higher one is queued while the writer is still outside r.mu.
	reg.mu.Lock()
	reg.outbound = []map[string]any{{
		"grant_id": "g1", "session_key": session, "request_id": "r1",
		"event": "progress", "text": "甲", "proto": 2,
		"cursor": uint64(5), "cursor_from": uint64(5),
	}}
	reg.pending = []viewerEvent{{
		SessionKey: session, RequestID: "r1", Event: "progress",
		Text: "乙", Cursor: 6, CursorFrom: 6,
	}, {
		SessionKey: other, RequestID: "r2", Event: "progress",
		Text: "他", Cursor: 1, CursorFrom: 1,
	}}
	reg.mu.Unlock()
	reg.wakeWriter()
	close(hold)
	closed = true

	got := wire.waitN(3, 2*time.Second)
	idx := map[string]int{}
	for i, frame := range got {
		if text := viewerText(frame["text"]); text != "" {
			idx[text] = i
		}
	}
	_, hasLow := idx["甲"]
	_, hasHigh := idx["乙"]
	_, hasOther := idx["他"]
	if !hasLow || !hasHigh || !hasOther || idx["甲"] >= idx["乙"] || idx["乙"] >= idx["他"] {
		t.Fatalf("wire order = %#v", got)
	}
	state := emptyViewerReduceState()
	for cursor := uint64(1); cursor <= 4; cursor++ {
		state = reduceViewerEvent(state, map[string]any{
			"event": "token", "session_key": session,
			"cursor": cursor, "cursor_from": cursor, "text": "前",
		})
	}
	for _, frame := range got {
		if viewerText(frame["session_key"]) != session {
			continue
		}
		state = reduceMarshaledOnto(t, state, frame)
	}
	if state.Gap || state.Last != 6 {
		t.Fatalf("reduce gap=%v last=%d order=%#v", state.Gap, state.Last, got)
	}
}

// A keepalive during the 80ms window owns a higher cursor than the buffered
// body. Sending it first makes the reducer drop it and then gap on every
// later frame. The buffered tail has to leave first. The elapsed window
// also has to ride the writer: the timer must not deliver on its own.
func TestRemoteStreamCoalesceFlushesBeforeHigherCursor(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	session := projectSessionOwnerID(`D:\proj\coalesce-order`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "甲", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: imHeartbeatMsg, Sequence: 2,
	})
	got := wire.waitN(2, 2*time.Second)
	tokenAt, beatAt := -1, -1
	for i, frame := range got {
		switch viewerText(frame["event"]) {
		case "token":
			if viewerText(frame["text"]) == "甲" && tokenAt < 0 {
				tokenAt = i
			}
		case "heartbeat":
			if beatAt < 0 {
				beatAt = i
			}
		}
	}
	if tokenAt < 0 || beatAt < 0 || tokenAt > beatAt {
		t.Fatalf("wire order = %#v", got)
	}
	state := emptyViewerReduceState()
	for _, frame := range got {
		if viewerText(frame["session_key"]) != session {
			continue
		}
		state = reduceMarshaledOnto(t, state, frame)
	}
	if state.Gap || state.Body != "甲" || state.Last == 0 {
		t.Fatalf("reduce body=%q gap=%v last=%d order=%#v", state.Body, state.Gap, state.Last, got)
	}

	thinkSession := projectSessionOwnerID(`D:\proj\coalesce-think`)
	viewerThinkOpen.Delete(thinkSession)
	viewerThinkText.Delete(thinkSession)
	reg.watchWithProto("g1", thinkSession, 2)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: thinkSession, RequestID: "r1", Text: "丙", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: thinkSession, RequestID: "r1", Text: "\x01丁", Sequence: 2,
	})
	deadline := time.Now().Add(2 * time.Second)
	var think []map[string]any
	for time.Now().Before(deadline) {
		think = nil
		for _, frame := range wire.snapshot() {
			if viewerText(frame["session_key"]) == thinkSession {
				think = append(think, frame)
			}
		}
		sawToken, sawStart := false, false
		for _, frame := range think {
			if viewerText(frame["event"]) == "token" && viewerText(frame["text"]) == "丙" {
				sawToken = true
			}
			if viewerText(frame["event"]) == "thinking" && viewerText(frame["state"]) == "started" {
				sawStart = true
			}
		}
		if sawToken && sawStart {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	textAt, startAt := -1, -1
	for i, frame := range think {
		if viewerText(frame["event"]) == "token" && viewerText(frame["text"]) == "丙" && textAt < 0 {
			textAt = i
		}
		if viewerText(frame["event"]) == "thinking" && viewerText(frame["state"]) == "started" && startAt < 0 {
			startAt = i
		}
	}
	if textAt < 0 || startAt < 0 || textAt > startAt {
		t.Fatalf("thinking order = %#v", think)
	}
	thinkState := emptyViewerReduceState()
	for _, frame := range think {
		thinkState = reduceMarshaledOnto(t, thinkState, frame)
	}
	if thinkState.Gap || thinkState.Body != "丙" {
		t.Fatalf("thinking reduce body=%q gap=%v last=%d frames=%#v", thinkState.Body, thinkState.Gap, thinkState.Last, think)
	}
}

func TestRemoteStreamCoalesceDueStaysOnWriter(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	hold := make(chan struct{})
	reg.holdWrite = hold
	closed := false
	defer func() {
		if !closed {
			close(hold)
		}
	}()
	session := projectSessionOwnerID(`D:\proj\coalesce-due`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)
	reg.mu.Lock()
	turn := reg.turnForLocked(session, "r1")
	turn.pending = []string{"甲"}
	turn.pendingFrom = 1
	turn.pendingTo = 1
	turn.pendingSeq = 1
	turn.cursor = 1
	turn.snapshot.Cursor = 1
	reg.pending = []viewerEvent{{
		SessionKey: session, RequestID: "r1", Event: "heartbeat",
		Cursor: 2, CursorFrom: 2,
	}}
	reg.mu.Unlock()
	reg.coalesceDue(session)
	if len(wire.snapshot()) != 0 {
		t.Fatalf("timer delivered while the writer was held: %#v", wire.snapshot())
	}
	close(hold)
	closed = true
	got := wire.waitN(2, 2*time.Second)
	tokenAt, beatAt := -1, -1
	for i, frame := range got {
		switch viewerText(frame["event"]) {
		case "token":
			if tokenAt < 0 {
				tokenAt = i
			}
		case "heartbeat":
			if beatAt < 0 {
				beatAt = i
			}
		}
	}
	if tokenAt < 0 || beatAt < 0 || tokenAt > beatAt {
		t.Fatalf("wire order = %#v", got)
	}
	state := emptyViewerReduceState()
	for _, frame := range got {
		state = reduceMarshaledOnto(t, state, frame)
	}
	if state.Gap || state.Body != "甲" || state.Last != 2 {
		t.Fatalf("reduce body=%q gap=%v last=%d order=%#v", state.Body, state.Gap, state.Last, got)
	}
}

// Unwatch while the writer still holds an older cursor. The next tokens must
// not fold first: watch_task would seed past the queued event and the
// reducer would drop it. The body stays in admission order.
func TestRemoteStreamUnwatchKeepsQueuedOrder(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	wire := &remoteStreamCapture{}
	reg.sink = wire.sink
	hold := make(chan struct{})
	reg.holdWrite = hold
	closed := false
	defer func() {
		if !closed {
			close(hold)
		}
	}()
	session := projectSessionOwnerID(`D:\proj\aside-order`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "甲", Sequence: 1,
	})
	reg.unwatch("g1", session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "乙", Sequence: 2,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "丙", Sequence: 3,
	})

	reg.mu.Lock()
	queued := append([]viewerEvent(nil), reg.pending...)
	parked := append([]viewerEvent(nil), reg.aside...)
	body := ""
	if turn := reg.turns[session]; turn != nil {
		for _, part := range turn.snapshot.Parts {
			if part.Kind == "text" {
				body += part.Text
			}
		}
		body += strings.Join(turn.pending, "")
	}
	reg.mu.Unlock()
	if len(queued) != 1 || queued[0].Text != "甲" {
		t.Fatalf("queued = %+v", queued)
	}
	if len(parked) != 1 || parked[0].Text != "乙丙" || parked[0].CursorFrom != queued[0].Cursor+1 || parked[0].Cursor <= queued[0].Cursor {
		t.Fatalf("aside = %+v queued %d", parked, queued[0].Cursor)
	}
	if strings.Contains(body, "乙") || strings.Contains(body, "丙") {
		t.Fatalf("folded ahead of the queue: %q", body)
	}
	if len(wire.snapshot()) != 0 {
		t.Fatalf("unwatched tail was written early: %v", wire.snapshot())
	}

	close(hold)
	closed = true
	live := waitLiveSnapshot(t, reg, session, func(snap *remoteLiveSnapshot) bool {
		return snapshotHasText(snap, "甲乙丙")
	})
	if live == nil || !snapshotHasText(live, "甲乙丙") {
		t.Fatalf("live = %#v", live)
	}
	text := ""
	for _, part := range live.Parts {
		if part.Kind == "text" {
			text += part.Text
		}
	}
	if strings.Index(text, "甲") > strings.Index(text, "乙") || strings.Index(text, "乙") > strings.Index(text, "丙") {
		t.Fatalf("body order = %q", text)
	}
	reg.watchWithProto("g1", session, 2)
	state := reduceMarshaledOnto(t, emptyViewerReduceState(), map[string]any{
		"event": "snapshot", "resync": true, "cursor": live.Cursor, "cursor_from": live.Cursor, "live": live,
	})
	if state.Gap || state.Body != "甲乙丙" || state.Last != live.Cursor {
		t.Fatalf("reduce body=%q gap=%v last=%d cursor=%d", state.Body, state.Gap, state.Last, live.Cursor)
	}
}

func TestRemoteStreamTrimKeepsRune(t *testing.T) {
	body := strings.Repeat("你", 10)
	if got := trimRemoteStreamTail(body, 10); got != "…"+strings.Repeat("你", 3) {
		t.Fatalf("tail = %q", got)
	}
	if got := trimRemoteStreamTail("你", 1); got != "…你" {
		t.Fatalf("last rune = %q", got)
	}
	if got := trimRemoteStreamText("你你", 4); got != "你…" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestRemoteStreamSettledUnwatchedKeepsCursor(t *testing.T) {
	app := &App{}
	session := projectSessionOwnerID(`D:\proj\settled-cursor`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "答完", Sequence: 1,
	})
	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{SessionKey: session, Text: "答完"})
	reg := app.remoteWatchRegistryTry()
	if reg == nil {
		t.Fatal("missing registry")
	}
	if live := reg.liveSnapshot(session); live != nil {
		t.Fatalf("settled unwatched turn stayed resident: %#v", live)
	}
	cursor := reg.foldedCursor(session)
	if cursor == 0 {
		t.Fatal("dropping a settled turn forgot the cursor")
	}
	reg.watchWithProto("g1", session, 2)
	if again := reg.foldedCursor(session); again != cursor {
		t.Fatalf("watch cursor = %d, folded %d", again, cursor)
	}
	if live := reg.liveSnapshot(session); live != nil {
		t.Fatalf("watch revived a settled turn: %#v", live)
	}
}

func TestRemoteStreamFoldBeforeFirstWatch(t *testing.T) {
	app := &App{}
	session := projectSessionOwnerID(`D:\proj\before-registry`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "先写", Sequence: 1,
	})
	reg := app.remoteWatchRegistryTry()
	if reg == nil || reg.wireQueue != nil {
		t.Fatal("first publish did not fold off the wire queue")
	}
	live := reg.liveSnapshot(session)
	if live == nil || !snapshotHasText(live, "先写") || live.Cursor == 0 {
		t.Fatalf("live before watch = %#v", live)
	}
	reg.watchWithProto("g1", session, 2)
	var next viewerEvent
	app.addViewerSubscriber(func(ev viewerEvent) {
		if ev.Text == "后写" {
			next = ev
		}
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "后写", Sequence: 2,
	})
	if next.Cursor != live.Cursor+1 {
		t.Fatalf("next cursor = %d live %d", next.Cursor, live.Cursor)
	}
	state := reduceMarshaledOnto(t, emptyViewerReduceState(), map[string]any{
		"event": "snapshot", "resync": true, "cursor": live.Cursor, "cursor_from": live.Cursor, "live": live,
	})
	state = reduceMarshaledOnto(t, state, map[string]any{
		"event": "token", "cursor": next.Cursor, "cursor_from": next.Cursor, "text": "后写",
	})
	if state.Gap || state.Body != "先写后写" {
		t.Fatalf("body=%q gap=%v last=%d", state.Body, state.Gap, state.Last)
	}
}

func TestRemoteStreamLateWatchKeepsUnwatchedTurn(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := projectSessionOwnerID(`D:\proj\late-watch`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "已写", Sequence: 1,
	})
	if reg.wireQueue != nil || len(cap.snapshot()) != 0 {
		t.Fatal("unwatched text entered the wire queue")
	}
	reg.mu.Lock()
	if turn := reg.turns[session]; turn == nil || turn.flushTimer != nil {
		reg.mu.Unlock()
		t.Fatal("unwatched publish armed the coalesce timer")
	}
	_ = reg.flushSessionLocked(session)
	reg.mu.Unlock()
	live := reg.liveSnapshot(session)
	if live == nil || !snapshotHasText(live, "已写") || live.Cursor == 0 {
		t.Fatalf("late live = %#v", live)
	}
	var next viewerEvent
	app.addViewerSubscriber(func(ev viewerEvent) {
		if ev.Text == "续" {
			next = ev
		}
	})
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "续", Sequence: 2,
	})
	if next.Cursor != live.Cursor+1 {
		t.Fatalf("next cursor = %d live %d", next.Cursor, live.Cursor)
	}
	state := reduceMarshaledOnto(t, emptyViewerReduceState(), map[string]any{
		"event": "snapshot", "resync": true, "cursor": live.Cursor, "cursor_from": live.Cursor, "live": live,
	})
	state = reduceMarshaledOnto(t, state, map[string]any{
		"event": "token", "cursor": next.Cursor, "cursor_from": next.Cursor, "text": "续",
	})
	if state.Gap || state.Body != "已写续" {
		t.Fatalf("late reduce body=%q gap=%v last=%d", state.Body, state.Gap, state.Last)
	}
	// The watched token is admitted on the writer. Wait until the turn
	// actually holds it, then leave; the next unwatched token must extend
	// that same turn instead of starting the cursor over.
	if got := waitLiveSnapshot(t, reg, session, func(snap *remoteLiveSnapshot) bool {
		return snapshotHasText(snap, "已写续")
	}); got == nil {
		t.Fatal("watched token never folded into the live turn")
	}

	reg.unwatch("g1", session)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "再写", Sequence: 3,
	})
	reg.mu.Lock()
	_ = reg.flushSessionLocked(session)
	reg.mu.Unlock()
	again := reg.liveSnapshot(session)
	if again == nil || !snapshotHasText(again, "已写续再写") {
		t.Fatalf("turn dropped on unwatch: %#v", again)
	}
}

func TestRemoteStreamDroppedTokensCoalesce(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	session := projectSessionOwnerID(`D:\proj\drop-coalesce`)
	reg.mu.Lock()
	reg.noteGapLocked(viewerEvent{SessionKey: session, RequestID: "r", Event: "token", Lane: "content", Text: "甲", Cursor: 10, CursorFrom: 10})
	reg.noteGapLocked(viewerEvent{SessionKey: session, RequestID: "r", Event: "token", Lane: "content", Text: "乙", Cursor: 11, CursorFrom: 11})
	reg.noteGapLocked(viewerEvent{SessionKey: session, RequestID: "r", Event: "progress", Text: "步进", Cursor: 12, CursorFrom: 12})
	reg.noteGapLocked(viewerEvent{SessionKey: session, RequestID: "r", Event: "token", Lane: "content", Text: "丙", Cursor: 13, CursorFrom: 13})
	reg.noteGapLocked(viewerEvent{SessionKey: session, RequestID: "r", Event: "token", Lane: "content", Text: "\x02", Cursor: 14, CursorFrom: 14})
	got := append([]viewerEvent(nil), reg.dropped[session]...)
	reg.mu.Unlock()
	if len(got) != 4 || got[0].Text != "甲乙" || got[0].CursorFrom != 10 || got[0].Cursor != 11 || got[1].Event != "progress" || got[2].Text != "丙" || got[3].Text != "\x02" {
		t.Fatalf("dropped = %+v", got)
	}
}

func reduceSessionWire(t *testing.T, frames []map[string]any, session string) viewerReduceState {
	t.Helper()
	state := emptyViewerReduceState()
	for _, frame := range frames {
		if frame["session_key"] != session {
			continue
		}
		state = reduceMarshaledOnto(t, state, frame)
	}
	return state
}

func TestRemoteStreamLivePartsRoundTrip(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "问", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 2,
	})
	coding := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T2", Title: "edit parser",
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: coding, Sequence: 3,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "helloworld", Sequence: 4,
	})
	live := waitLiveSnapshot(t, reg, session, func(snap *remoteLiveSnapshot) bool {
		return snapshotHasText(snap, "helloworld") && snapshotHasText(snap, "想想") && snapshotHasKind(snap, "progress")
	})
	if live == nil || live.Cursor == 0 || live.Responded {
		t.Fatalf("watch live = %#v", live)
	}
	var thought string
	var thoughtDone bool
	var row *codingAgentEventPayload
	for _, part := range live.Parts {
		if part.Kind == "thinking" && strings.Contains(part.Text, "想想") {
			thought = part.Text
			thoughtDone = part.Done
		}
		if part.Kind == "progress" && part.Coding != nil {
			row = part.Coding
		}
	}
	if thought != "想想" || !thoughtDone || row == nil || row.TaskID != "T2" {
		t.Fatalf("live parts = %+v thought=%q done=%v", live.Parts, thought, thoughtDone)
	}
	state := reduceMarshaledViewerEvent(t, map[string]any{
		"event": "snapshot", "resync": true, "cursor": live.Cursor, "live": live,
	})
	if state.Body != "helloworld" || state.Gap || state.Responded || state.Last != live.Cursor || state.DisplayText != "问" || state.RequestID != "r1" {
		t.Fatalf("reduced watch live: body=%q last=%d cursor=%d gap=%v responded=%v display=%q request=%q", state.Body, state.Last, live.Cursor, state.Gap, state.Responded, state.DisplayText, state.RequestID)
	}
	if len(state.Thinking) != 1 || state.Thinking[0].Text != "想想" || !state.Thinking[0].Done {
		t.Fatalf("thinking = %+v", state.Thinking)
	}
	if len(state.Timeline) != 1 || state.Timeline[0].Phase != "running" || state.Timeline[0].TaskID != "T2" || state.Timeline[0].Title != "edit parser" || state.Timeline[0].RunID != "run-1" {
		t.Fatalf("timeline = %+v", state.Timeline)
	}
}

func TestRemoteStreamResyncSnapshotCarriesRespondedTurn(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "问", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 2,
	})
	coding := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T2", Title: "edit parser",
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: coding, Sequence: 3,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "草稿", Sequence: 4,
	})
	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{SessionKey: session, Text: "终稿"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !hasEvent(cap.snapshot(), "response") {
		time.Sleep(5 * time.Millisecond)
	}
	if !hasEvent(cap.snapshot(), "response") {
		t.Fatalf("events = %v", eventNames(cap.snapshot()))
	}
	if reg.liveSnapshot(session) != nil {
		t.Fatal("watch_task returned a settled turn")
	}
	reg.mu.Lock()
	frame := reg.snapshotFrameLocked(session)
	reg.mu.Unlock()
	if frame.Live == nil || !frame.Live.Responded || !snapshotHasText(frame.Live, "终稿") {
		t.Fatalf("snapshot live = %#v", frame.Live)
	}
	state := reduceMarshaledViewerEvent(t, frame.envelope("g1", 2))
	if state.Body != "终稿" || state.Gap || !state.Responded || state.Last == 0 || state.Last != frame.Cursor {
		t.Fatalf("reduced snapshot: body=%q last=%d cursor=%d gap=%v responded=%v", state.Body, state.Last, frame.Cursor, state.Gap, state.Responded)
	}
	if len(state.Thinking) != 1 || state.Thinking[0].Text != "想想" || !state.Thinking[0].Done {
		t.Fatalf("thinking = %+v", state.Thinking)
	}
	if len(state.Timeline) != 1 || state.Timeline[0].TaskID != "T2" || state.Timeline[0].Phase != "running" || state.Timeline[0].Title != "edit parser" || state.Timeline[0].RunID != "run-1" {
		t.Fatalf("timeline = %+v", state.Timeline)
	}
}

// TestRemoteStreamWireSnapshotFixture pins the bytes snapshotFrameLocked and
// watch_task liveSnapshot actually marshal. The desktop, phone, and golden
// reducers load testdata/viewer_wire_snapshots.json, so a hand-written
// live.text object cannot stand in for the wire.
func TestRemoteStreamWireSnapshotFixture(t *testing.T) {
	watch := marshalWatchSnapshotEvent(t)
	responded := marshalRespondedSnapshotEvent(t)
	assertWireSnapshotReduction(t, watch, "helloworld", false)
	assertWireSnapshotReduction(t, responded, "终稿", true)

	fresh := map[string]any{
		"source":          "json.Marshal of watch_task liveSnapshot as a resync snapshot, and of snapshotFrameLocked.envelope for a responded turn. Regenerate with MACLAW_UPDATE_WIRE_FIXTURE=1.",
		"watch_event":     mustDecodeJSON(t, watch),
		"responded_event": mustDecodeJSON(t, responded),
	}
	path := "testdata/viewer_wire_snapshots.json"
	if os.Getenv("MACLAW_UPDATE_WIRE_FIXTURE") == "1" {
		raw, err := json.MarshalIndent(fresh, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	savedRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("wire snapshot fixture: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(savedRaw, &saved); err != nil {
		t.Fatal(err)
	}
	assertSameWireSnapshot(t, fresh["watch_event"], saved["watch_event"])
	assertSameWireSnapshot(t, fresh["responded_event"], saved["responded_event"])
	assertWireSnapshotReduction(t, mustRemarshal(t, saved["watch_event"]), "helloworld", false)
	assertWireSnapshotReduction(t, mustRemarshal(t, saved["responded_event"]), "终稿", true)
}

func marshalWatchSnapshotEvent(t *testing.T) []byte {
	t.Helper()
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	reg.sink = (&remoteStreamCapture{}).sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "问", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 2,
	})
	coding := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T2", Title: "edit parser",
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: coding, Sequence: 3,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "helloworld", Sequence: 4,
	})
	live := waitLiveSnapshot(t, reg, session, func(snap *remoteLiveSnapshot) bool {
		return snapshotHasText(snap, "helloworld") && snapshotHasText(snap, "想想") && snapshotHasKind(snap, "progress")
	})
	if live == nil || live.Cursor == 0 || live.Responded || live.RequestID != "r1" {
		t.Fatalf("watch live = %#v", live)
	}
	raw, err := json.Marshal(map[string]any{
		"event": "snapshot", "resync": true, "cursor": live.Cursor, "cursor_from": live.Cursor, "live": live,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func marshalRespondedSnapshotEvent(t *testing.T) []byte {
	t.Helper()
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := projectSessionOwnerID(`D:\proj\final`)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-foreground-round-started", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", DisplayText: "问", Sequence: 1,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "\x01想想", Sequence: 2,
	})
	coding := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T2", Title: "edit parser",
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: coding, Sequence: 3,
	})
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "草稿", Sequence: 4,
	})
	app.relayRemoteAssistantResponse("r1", &IMAgentResponse{SessionKey: session, Text: "终稿"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !hasEvent(cap.snapshot(), "response") {
		time.Sleep(5 * time.Millisecond)
	}
	if !hasEvent(cap.snapshot(), "response") {
		t.Fatalf("events = %v", eventNames(cap.snapshot()))
	}
	if reg.liveSnapshot(session) != nil {
		t.Fatal("watch_task returned a settled turn")
	}
	reg.mu.Lock()
	frame := reg.snapshotFrameLocked(session)
	reg.mu.Unlock()
	if frame.Live == nil || !frame.Live.Responded || !snapshotHasText(frame.Live, "终稿") || snapshotHasText(frame.Live, "草稿") {
		t.Fatalf("snapshot live = %#v", frame.Live)
	}
	envelope := frame.envelope("g1", 2)
	if envelope == nil {
		t.Fatal("proto 2 envelope dropped the snapshot")
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertWireSnapshotReduction(t *testing.T, raw []byte, wantBody string, wantResponded bool) {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event["event"] != "snapshot" || event["resync"] != true {
		t.Fatalf("event = %#v", event["event"])
	}
	live := viewerMap(event["live"])
	if live == nil {
		t.Fatal("snapshot has no live")
	}
	if _, ok := live["text"]; ok {
		t.Fatalf("wire live carries text, not parts: %#v", live["text"])
	}
	parts, ok := live["parts"].([]any)
	if !ok || len(parts) == 0 {
		t.Fatalf("parts = %#v", live["parts"])
	}
	cursor := viewerNum(event["cursor"])
	if cursor == 0 || viewerNum(live["cursor"]) != cursor {
		t.Fatalf("cursor event=%v live=%v", event["cursor"], live["cursor"])
	}
	var sawBody bool
	for _, item := range parts {
		part := viewerMap(item)
		if viewerText(part["kind"]) == "text" && viewerText(part["text"]) == wantBody {
			sawBody = true
		}
	}
	if !sawBody {
		t.Fatalf("parts missing %q: %#v", wantBody, parts)
	}
	state := reduceViewerEvent(emptyViewerReduceState(), event)
	if state.Body != wantBody || state.Last != cursor || state.Gap || state.Responded != wantResponded || state.RequestID != "r1" || state.DisplayText != "问" {
		t.Fatalf("reduced body=%q last=%d cursor=%d gap=%v responded=%v request=%q display=%q", state.Body, state.Last, cursor, state.Gap, state.Responded, state.RequestID, state.DisplayText)
	}
	if len(state.Thinking) != 1 || state.Thinking[0].Text != "想想" || !state.Thinking[0].Done {
		t.Fatalf("thinking = %+v", state.Thinking)
	}
	if len(state.Timeline) != 1 || state.Timeline[0].Phase != "running" || state.Timeline[0].TaskID != "T2" || state.Timeline[0].Title != "edit parser" || state.Timeline[0].RunID != "run-1" {
		t.Fatalf("timeline = %+v", state.Timeline)
	}
}

func assertSameWireSnapshot(t *testing.T, freshAny, savedAny any) {
	t.Helper()
	fresh := viewerMap(freshAny)
	saved := viewerMap(savedAny)
	if fresh == nil || saved == nil {
		t.Fatalf("snapshot missing fresh=%T saved=%T", freshAny, savedAny)
	}
	if fresh["event"] != saved["event"] || fresh["resync"] != saved["resync"] {
		t.Fatalf("event fresh=%v saved=%v", fresh["event"], saved["event"])
	}
	freshLive := viewerMap(fresh["live"])
	savedLive := viewerMap(saved["live"])
	for _, key := range []string{"request_id", "display_text", "responded", "stream_done", "project_path", "session_key"} {
		if freshLive[key] != savedLive[key] {
			t.Fatalf("live %s fresh=%#v saved=%#v", key, freshLive[key], savedLive[key])
		}
	}
	if viewerText(freshLive["project_path"]) == "" {
		t.Fatal("live project_path is empty")
	}
	freshParts, _ := freshLive["parts"].([]any)
	savedParts, _ := savedLive["parts"].([]any)
	if len(freshParts) == 0 || len(freshParts) != len(savedParts) {
		t.Fatalf("parts fresh=%d saved=%d", len(freshParts), len(savedParts))
	}
	for i := range freshParts {
		a := viewerMap(freshParts[i])
		b := viewerMap(savedParts[i])
		if viewerText(a["kind"]) != viewerText(b["kind"]) || viewerText(a["text"]) != viewerText(b["text"]) || (a["done"] == true) != (b["done"] == true) {
			t.Fatalf("part %d fresh=%#v saved=%#v", i, a, b)
		}
		ac := viewerMap(a["coding"])
		bc := viewerMap(b["coding"])
		if ac == nil && bc == nil {
			continue
		}
		if ac == nil || bc == nil {
			t.Fatalf("coding %d fresh=%#v saved=%#v", i, a["coding"], b["coding"])
		}
		for _, key := range []string{"phase", "task_id", "title", "run_id", "event"} {
			if ac[key] != bc[key] {
				t.Fatalf("coding %s fresh=%#v saved=%#v", key, ac[key], bc[key])
			}
		}
	}
}

func mustDecodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustRemarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reduceMarshaledViewerEvent(t *testing.T, frame map[string]any) viewerReduceState {
	t.Helper()
	return reduceMarshaledOnto(t, emptyViewerReduceState(), frame)
}

func reduceMarshaledOnto(t *testing.T, state viewerReduceState, frame map[string]any) viewerReduceState {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	return reduceViewerEvent(state, event)
}

func snapshotHasKind(snap *remoteLiveSnapshot, kind string) bool {
	if snap == nil {
		return false
	}
	for _, part := range snap.Parts {
		if part.Kind == kind {
			return true
		}
	}
	return false
}

func TestRemoteStreamCodingKeepsRunID(t *testing.T) {
	app := &App{}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	title := strings.Repeat("解析", 1500)
	text := formatCodingAgentEvent(CodingAgentEvent{
		Event: "task_status", Phase: "running", TaskID: "T9", Title: title,
		RunID: "run-汉字", Ts: "2026-11-17T00:00:00Z",
	})
	app.relayRemoteStreamEvent("ai-assistant-progress", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: text, Sequence: 4,
	})
	payloads := cap.waitN(1, 2*time.Second)
	if len(payloads) != 1 {
		t.Fatalf("payloads = %v", payloads)
	}
	p := payloads[0]
	shown, _ := p["text"].(string)
	if !utf8.ValidString(shown) || strings.Contains(shown, "\uFFFD") {
		t.Fatalf("coding text is not valid utf-8: %q", shown)
	}
	coding, _ := p["coding"].(*codingAgentEventPayload)
	if coding == nil || coding.RunID != "run-汉字" {
		t.Fatalf("coding = %#v", coding)
	}
	if strings.HasPrefix(strings.TrimSpace(shown), "{") || strings.Contains(shown, `"title":`) {
		t.Fatalf("coding text was cut mid-json: %q", shown)
	}
	if !strings.HasPrefix(shown, "Coding Agent:") {
		t.Fatalf("coding text = %q", shown)
	}
}

func TestRemoteStreamResponseKeepsCardsOverEnvelope(t *testing.T) {
	app := &App{}
	app.ctx = context.Background()
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := streamSessionKey()
	reg.watchWithProto("g1", session, 2)
	var rawEvent string
	app.testEmit = func(name string, data ...interface{}) {
		if name == "ai-assistant-response" && len(data) > 0 {
			rawEvent, _ = data[0].(string)
		}
	}
	var localReasoning string
	app.addViewerSubscriber(func(ev viewerEvent) {
		if ev.Event == "response" && ev.Terminal != nil {
			localReasoning = ev.Terminal.Reasoning
		}
	})
	resp := &IMAgentResponse{
		SessionKey:     session,
		Text:           strings.Repeat("答", 80000),
		Reasoning:      strings.Repeat("思", 80000),
		VoiceData:      strings.Repeat("A", 2000),
		FileData:       strings.Repeat("B", 2000),
		ImageKey:       "img-1",
		LocalFilePaths: []string{"out/report.md"},
		Confirmation:   &IMResponseConfirmation{ID: "c1", Summary: "确认写入"},
	}
	if !app.emitAIAssistantResponse("r1", resp) {
		// A background context has no Wails events value, so the raw emit still runs testEmit
		// and the function returns false only when ctx is nil. Background is non-nil.
	}
	payloads := cap.waitN(1, 2*time.Second)
	if len(payloads) == 0 || payloads[len(payloads)-1]["event"] != "response" {
		t.Fatalf("payloads = %v", eventNames(payloads))
	}
	frame := payloads[len(payloads)-1]
	encoded, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > remoteStreamEnvelopeSkip {
		t.Fatalf("response frame = %d bytes", len(encoded))
	}
	term, _ := frame["terminal"].(*viewerTerminal)
	if term == nil || term.Reasoning != "" || term.Confirmation == nil || term.Confirmation.ID != "c1" {
		t.Fatalf("terminal = %#v", term)
	}
	raw, _ := frame["response"].(json.RawMessage)
	var body IMAgentResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.VoiceData != "" || body.FileData != "" || body.ImageKey != "img-1" || len(body.LocalFilePaths) != 1 {
		t.Fatalf("wire body = %+v", body)
	}
	if body.Confirmation == nil {
		t.Fatal("wire response dropped confirmation")
	}
	if !strings.Contains(rawEvent, `"voice_data"`) || !strings.Contains(rawEvent, strings.Repeat("A", 100)) {
		t.Fatal("raw ai-assistant-response lost the media bytes")
	}
	if utf8.RuneCountInString(localReasoning) < 80000 {
		t.Fatalf("local final reasoning shrunk to %d", utf8.RuneCountInString(localReasoning))
	}
}

func TestRemoteStreamSnapshotUnder96KiB(t *testing.T) {
	cjk := strings.Repeat("测", 20000)
	coding := truncateCodingWire(codingAgentEventPayload{
		Title: strings.Repeat("题", 80), Phase: strings.Repeat("阶", 40), TaskID: strings.Repeat("号", 40),
		TurnID: strings.Repeat("轮", 40), Outcome: strings.Repeat("果", 40), Severity: strings.Repeat("级", 40),
		RunID: "run-1", Ts: "2026-11-17T00:00:00Z",
		Detail: strings.Repeat("细", 160), Summary: strings.Repeat("总", 160),
		Command: strings.Repeat("令", 160), Target: strings.Repeat("的", 160),
		Files: []string{strings.Repeat("径", 80), strings.Repeat("径", 80), strings.Repeat("径", 80), strings.Repeat("径", 80)},
	})
	parts := []remoteStreamPart{
		{Kind: "text", Text: cjk},
		{Kind: "thinking", Text: cjk},
	}
	for i := 0; i < 12; i++ {
		c := coding
		parts = append(parts, remoteStreamPart{Kind: "progress", Text: cjk, Coding: &c})
	}
	for i := 0; i < 8; i++ {
		parts = append(parts, remoteStreamPart{Kind: "progress", Text: strings.Repeat("进", 1000)})
	}
	snap := &remoteLiveSnapshot{
		RequestID: "r", SessionKey: streamSessionKey(), ProjectPath: `D:\proj\demo`,
		Parts: parts, Cursor: 40,
	}
	wb, _ := json.Marshal(map[string]any{
		"execution_plan":          strings.Repeat("计划", 4000),
		"requirement_restatement": strings.Repeat("需求", 4000),
	})
	out, shrunkWB := shrinkSnapshotBundle(snap, wb)
	n := snapshotBundleBytes(out, shrunkWB)
	if n >= remoteSnapshotMaxBytes || n >= remoteStreamEnvelopeSkip {
		t.Fatalf("snapshot marshal = %d", n)
	}
	if snap.Parts[0].Text != cjk {
		t.Fatal("shrink mutated the live turn")
	}
}

func TestRemoteStreamWatchTaskLiveIncludesUnflushed(t *testing.T) {
	app := newProjectSearchTestApp(t)
	task := app.createTaskRecordWithWorkingDir("远控流", "", []string{taskManagementTag, taskUserCreatedTag}, "", true)
	if task.ProjectPath == "" {
		t.Fatal("task record was not created")
	}
	reg := app.ensureRemoteWatchRegistry()
	cap := &remoteStreamCapture{}
	reg.sink = cap.sink
	session := projectSessionOwnerID(task.ProjectPath)
	reg.watchWithProto("g1", session, 2)
	app.relayRemoteStreamEvent("ai-assistant-token", AIAssistantStreamEvent{
		SessionKey: session, RequestID: "r1", Text: "还没刷", Sequence: 7,
	})
	body, _ := json.Marshal(map[string]string{"project_path": task.ProjectPath, "stream_proto": "2"})
	// stream_proto is an int. Re-marshal properly.
	body, _ = json.Marshal(map[string]any{"project_path": task.ProjectPath, "stream_proto": 2})
	deadline := time.Now().Add(70 * time.Millisecond)
	for time.Now().Before(deadline) {
		replyAny, err := app.remoteControlWatchTask("g1", body)
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := replyAny.(map[string]any)
		if reply == nil {
			t.Fatalf("watch reply = %#v", replyAny)
		}
		live, _ := reply["live"].(*remoteLiveSnapshot)
		if live != nil && snapshotHasText(live, "还没刷") && len(cap.snapshot()) == 0 {
			if live.Cursor == 0 {
				t.Fatal("live snapshot missing cursor")
			}
			if reply["stream_proto"] != 2 {
				t.Fatalf("stream_proto = %v", reply["stream_proto"])
			}
			return
		}
	}
	t.Fatal("watch_task live did not include the unflushed body before the wire flush")
}

func hasEvent(payloads []map[string]any, name string) bool {
	for _, p := range payloads {
		if p["event"] == name {
			return true
		}
	}
	return false
}

func eventNames(payloads []map[string]any) []string {
	out := make([]string, 0, len(payloads))
	for _, p := range payloads {
		name, _ := p["event"].(string)
		out = append(out, name)
	}
	return out
}

func sessionHas(payloads []map[string]any, session, name string) bool {
	for _, p := range payloads {
		if p["session_key"] == session && p["event"] == name {
			return true
		}
	}
	return false
}

func snapshotHasText(snap *remoteLiveSnapshot, text string) bool {
	if snap == nil {
		return false
	}
	for _, part := range snap.Parts {
		if strings.Contains(part.Text, text) {
			return true
		}
	}
	return false
}

func asUint(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int:
		return uint64(n), true
	case float64:
		return uint64(n), true
	case json.Number:
		i, err := n.Int64()
		return uint64(i), err == nil
	default:
		return 0, false
	}
}
