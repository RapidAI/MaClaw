package guiapp

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// Remote-control live mirror.
//
// A controlling desktop opens a remote task and sends the watch_task op; from
// then on every assistant stream event for that session is relayed over the
// Hub as a remote_control.stream envelope so the controller renders the same
// round the local panel does. Reasoning-lane tokens (\x01) are the one
// deliberate exception: they are buffered into a thinking part and shipped
// once the block closes, so the controller shows a folded 思考 box instead of
// a live character stream. Everything else — content tokens, progress lines,
// round boundaries, the final response — is forwarded verbatim.

// remoteStreamPart mirrors RemoteLivePart in remoteAssistantSession.ts. The
// same shape is used on the wire and inside the watch snapshot, so a watcher
// joining mid-turn and one receiving deltas see identical state.
type remoteStreamPart struct {
	Kind string `json:"kind"` // "text" | "thinking" | "progress"
	Seq  uint64 `json:"seq"`
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"` // thinking parts only
}

// remoteLiveSnapshot is the watch_task reply payload for a turn in flight.
type remoteLiveSnapshot struct {
	RequestID   string             `json:"request_id"`
	SessionKey  string             `json:"session_key"`
	ProjectPath string             `json:"project_path"`
	DisplayText string             `json:"display_text,omitempty"`
	Parts       []remoteStreamPart `json:"parts"`
	StreamDone  bool               `json:"stream_done"`
	Responded   bool               `json:"responded"`
	StartedAt   int64              `json:"started_at,omitempty"`
}

// remoteLiveTurn tracks the relay state of one in-flight request so a late
// watcher can be handed the parts accumulated so far.
type remoteLiveTurn struct {
	snapshot     remoteLiveSnapshot
	thinkingOpen bool           // the tail part is an unfinished thinking block
	pending      []string       // content-lane deltas inside the coalesce window
	pendingSeq   uint64         // sequence of the first pending delta
	flushTimer   *time.Timer    // coalesce timer for pending
}

const (
	// remoteWatchTTL bounds a watch that is never refreshed. watch_task,
	// get_task, and the other task ops all refresh it, so a session stays
	// watched while its conversation is open on the controller.
	remoteWatchTTL = 15 * time.Minute
	// remoteStreamTokenWindow batches content deltas for the wire. Tighter
	// than reading cadence matters little; the 20ms desktop window exists to
	// spare the renderer, not the network.
	remoteStreamTokenWindow = 150 * time.Millisecond
	// Per-part caps keep one envelope under the Hub's 256KB stream limit.
	remoteStreamPartTextCap     = 192 << 10
	remoteStreamPartThinkingCap = 128 << 10
	remoteStreamProgressKeep    = 40
	remoteStreamProgressTextCap = 4096
	// remoteStreamEnvelopeSkip is the encoded-size ceiling after which an
	// event is dropped (the Hub would discard it anyway).
	remoteStreamEnvelopeSkip = 240 << 10
)

type remoteWatchRegistry struct {
	app *App
	// sink, when set (tests), replaces the Hub connection as the event target.
	sink func(map[string]any)

	mu      sync.Mutex
	watches map[string]map[string]time.Time // grantID → sessionKey → expiry
	turns   map[string]*remoteLiveTurn      // sessionKey → live turn

	// The WebSocket write happens off the emit path: a stalled Hub connection
	// must never back up ai-assistant-token for the local panel, so events are
	// handed to one writer goroutine through a bounded queue. Overflow drops —
	// the transcript poll is the designed fallback.
	queue     chan map[string]any
	queueOnce sync.Once
}

func (a *App) remoteWatchRegistryOrNil() *remoteWatchRegistry {
	if a == nil {
		return nil
	}
	a.remoteInfraMu.RLock()
	defer a.remoteInfraMu.RUnlock()
	return a.remoteWatches
}

func (a *App) ensureRemoteWatchRegistry() *remoteWatchRegistry {
	if reg := a.remoteWatchRegistryOrNil(); reg != nil {
		return reg
	}
	a.remoteInfraMu.Lock()
	defer a.remoteInfraMu.Unlock()
	if a.remoteWatches == nil {
		a.remoteWatches = &remoteWatchRegistry{
			app:     a,
			watches: map[string]map[string]time.Time{},
			turns:   map[string]*remoteLiveTurn{},
		}
	}
	return a.remoteWatches
}

// --- watch management -------------------------------------------------------

func (r *remoteWatchRegistry) watch(grantID, sessionKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.watchLocked(grantID, sessionKey)
}

// watchLocked refreshes the expiry; get_task and friends count as refreshes
// so a watch lives as long as the conversation stays open.
func (r *remoteWatchRegistry) watchLocked(grantID, sessionKey string) {
	grantID = strings.TrimSpace(grantID)
	sessionKey = strings.TrimSpace(sessionKey)
	if grantID == "" || sessionKey == "" {
		return
	}
	set := r.watches[grantID]
	if set == nil {
		set = map[string]time.Time{}
		r.watches[grantID] = set
	}
	set[sessionKey] = time.Now().Add(remoteWatchTTL)
}

func (r *remoteWatchRegistry) unwatch(grantID, sessionKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if set := r.watches[grantID]; set != nil {
		delete(set, sessionKey)
		if len(set) == 0 {
			delete(r.watches, grantID)
		}
	}
	// A turn nobody watches is only kept while other grants still see it.
	if !r.sessionWatchedLocked(sessionKey) {
		r.dropTurnLocked(sessionKey)
	}
}

// grantsForLocked lists the non-expired watchers of one session and removes
// the dead entries it passes.
func (r *remoteWatchRegistry) grantsForLocked(sessionKey string) []string {
	now := time.Now()
	var out []string
	for grantID, set := range r.watches {
		expires, ok := set[sessionKey]
		if !ok {
			continue
		}
		if now.After(expires) {
			delete(set, sessionKey)
			if len(set) == 0 {
				delete(r.watches, grantID)
			}
			continue
		}
		out = append(out, grantID)
	}
	return out
}

func (r *remoteWatchRegistry) sessionWatchedLocked(sessionKey string) bool {
	return len(r.grantsForLocked(sessionKey)) > 0
}

func (r *remoteWatchRegistry) dropTurnLocked(sessionKey string) {
	if turn := r.turns[sessionKey]; turn != nil {
		if turn.flushTimer != nil {
			turn.flushTimer.Stop()
		}
		delete(r.turns, sessionKey)
	}
}

// liveSnapshot answers watch_task: the turn in flight on this session, so a
// watcher opening mid-turn renders what it missed instead of waiting for the
// next event.
func (r *remoteWatchRegistry) liveSnapshot(sessionKey string) *remoteLiveSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	turn := r.turns[sessionKey]
	if turn == nil || turn.snapshot.Responded {
		return nil
	}
	snap := turn.snapshot
	snap.Parts = append([]remoteStreamPart(nil), snap.Parts...)
	return &snap
}

// --- relay ------------------------------------------------------------------

// relayRemoteStreamEvent is the emitAIAssistantStreamEvent hook. Events for
// sessions nobody watches exit on the registry-nil / no-grants paths without
// touching the Hub.
func (a *App) relayRemoteStreamEvent(name string, event AIAssistantStreamEvent) {
	reg := a.remoteWatchRegistryOrNil()
	if reg == nil {
		return
	}
	reg.relay(name, event)
}

// relayRemoteAssistantResponse is the emitAIAssistantResponse hook; the
// settled response mirrors to watchers as the terminal "response" event.
func (a *App) relayRemoteAssistantResponse(requestID string, resp *IMAgentResponse) {
	reg := a.remoteWatchRegistryOrNil()
	if reg == nil || resp == nil {
		return
	}
	sessionKey := strings.TrimSpace(resp.SessionKey)
	if sessionKey == "" {
		return
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	reg.relayResponse(sessionKey, requestID, raw)
}

func newRemoteLiveTurn(sessionKey, requestID string) *remoteLiveTurn {
	return &remoteLiveTurn{snapshot: remoteLiveSnapshot{
		RequestID:   requestID,
		SessionKey:  sessionKey,
		ProjectPath: projectPathFromSessionOwnerID(sessionKey),
		Parts:       []remoteStreamPart{},
		StartedAt:   time.Now().Unix(),
	}}
}

// sender returns the event target: the injected sink synchronously, or an
// enqueue onto the writer goroutine for the Hub path. A nil sender means
// "drop and let the transcript poll cover it".
func (r *remoteWatchRegistry) sender() func(map[string]any) {
	if r.sink != nil {
		return r.sink
	}
	if r.app.hubClient() == nil {
		return nil
	}
	return r.dispatch
}

// dispatch queues one event for the writer goroutine. Callers hold r.mu, so
// the queue order is the relay's event order.
func (r *remoteWatchRegistry) dispatch(payload map[string]any) {
	r.queueOnce.Do(func() {
		r.queue = make(chan map[string]any, 512)
		go r.writeLoop()
	})
	select {
	case r.queue <- payload:
	default:
	}
}

func (r *remoteWatchRegistry) writeLoop() {
	for payload := range r.queue {
		if hub := r.app.hubClient(); hub != nil {
			hub.sendRemoteControlStream(payload)
		}
	}
}

func (r *remoteWatchRegistry) relay(name string, event AIAssistantStreamEvent) {
	sessionKey := strings.TrimSpace(event.SessionKey)
	if sessionKey == "" {
		return
	}
	send := r.sender()
	if send == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grants := r.grantsForLocked(sessionKey)
	if len(grants) == 0 {
		return
	}
	turn := r.turnForLocked(sessionKey, event.RequestID)
	seq := event.Sequence
	switch name {
	case "ai-assistant-foreground-round-started":
		// A new request on this session replaces whatever was live: rounds are
		// sequential per session and the controller mirrors the latest one.
		if turn.snapshot.RequestID != event.RequestID || turn.snapshot.Responded {
			turn = newRemoteLiveTurn(sessionKey, event.RequestID)
			r.turns[sessionKey] = turn
		}
		turn.snapshot.DisplayText = event.DisplayText
		if turn.snapshot.DisplayText == "" {
			turn.snapshot.DisplayText = event.Text
		}
		r.sendLocked(send, grants, sessionKey, event.RequestID, seq, "round_started", map[string]any{"display_text": turn.snapshot.DisplayText})
	case "ai-assistant-token":
		r.relayTokenLocked(send, grants, turn, event)
	case "ai-assistant-progress":
		r.flushPendingLocked(send, grants, turn, seq)
		r.closeThinkingLocked(send, grants, turn, seq)
		text := trimRemoteStreamText(event.Text, remoteStreamProgressTextCap)
		turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "progress", Seq: seq, Text: text})
		r.trimProgressLocked(turn)
		r.sendLocked(send, grants, sessionKey, event.RequestID, seq, "progress", map[string]any{"text": text})
	case "ai-assistant-new-round":
		r.flushPendingLocked(send, grants, turn, seq)
		r.closeThinkingLocked(send, grants, turn, seq)
		r.sendLocked(send, grants, sessionKey, event.RequestID, seq, "new_round", nil)
	case "ai-assistant-stream-done":
		r.flushPendingLocked(send, grants, turn, seq)
		r.closeThinkingLocked(send, grants, turn, seq)
		turn.snapshot.StreamDone = true
		r.sendLocked(send, grants, sessionKey, event.RequestID, seq, "stream_done", nil)
	}
}

// relayTokenLocked splits one coalesced token batch into lanes. Reasoning
// deltas accumulate into the open thinking part and ship only when it closes;
// content deltas join the pending run flushed on the 150ms window or before
// the next non-token event, keeping wire order identical to local order.
func (r *remoteWatchRegistry) relayTokenLocked(send func(map[string]any), grants []string, turn *remoteLiveTurn, event AIAssistantStreamEvent) {
	deltas := event.Deltas
	if len(deltas) == 0 && event.Text != "" {
		deltas = []string{event.Text}
	}
	for _, delta := range deltas {
		switch {
		case strings.HasPrefix(delta, rejectedRoundContentToken):
			// Draft reset: flush what was real, drop trailing text parts, and
			// forward the marker verbatim — the controller drops its trailing
			// text run and keeps whatever follows the marker as fresh content.
			r.flushPendingLocked(send, grants, turn, event.Sequence)
			r.dropTrailingTextPartsLocked(turn)
			part := remoteStreamPart{Kind: "text", Seq: event.Sequence, Text: strings.TrimPrefix(delta, rejectedRoundContentToken)}
			if part.Text != "" {
				turn.snapshot.Parts = append(turn.snapshot.Parts, part)
			}
			r.sendLocked(send, grants, turn.snapshot.SessionKey, turn.snapshot.RequestID, event.Sequence, "token", map[string]any{"text": delta})
		case strings.HasPrefix(delta, "\x01"):
			r.flushPendingLocked(send, grants, turn, event.Sequence)
			if !turn.thinkingOpen {
				turn.thinkingOpen = true
				turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "thinking", Seq: event.Sequence})
				r.sendLocked(send, grants, turn.snapshot.SessionKey, turn.snapshot.RequestID, event.Sequence, "thinking", map[string]any{"state": "started"})
			}
			part := &turn.snapshot.Parts[len(turn.snapshot.Parts)-1]
			part.Text = trimRemoteStreamTail(part.Text+strings.TrimPrefix(delta, "\x01"), remoteStreamPartThinkingCap)
		default:
			// Content tokens mean the model finished thinking — close the box
			// the same moment the local panel folds it.
			if turn.thinkingOpen {
				r.closeThinkingLocked(send, grants, turn, event.Sequence)
			}
			r.queueContentDelta(turn, event.Sequence, delta)
		}
	}
}

// queueContentDelta appends to the pending run and arms the flush timer.
// Callers hold r.mu.
func (r *remoteWatchRegistry) queueContentDelta(turn *remoteLiveTurn, seq uint64, delta string) {
	if len(turn.pending) == 0 {
		turn.pendingSeq = seq
	}
	turn.pending = append(turn.pending, delta)
	if turn.flushTimer == nil {
		turn.flushTimer = time.AfterFunc(remoteStreamTokenWindow, func() {
			r.flushPending(turn)
		})
	}
}

// flushPending is the timer entry; it re-locks the registry and sends the
// drained batch. Hub delivery order is registry.mu order, so a batch taken
// here can never pass a control event sent while it was pending.
func (r *remoteWatchRegistry) flushPending(turn *remoteLiveTurn) {
	send := r.sender()
	if send == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grants := r.grantsForLocked(turn.snapshot.SessionKey)
	if len(grants) == 0 {
		turn.pending = nil
		return
	}
	r.flushPendingLocked(send, grants, turn, 0)
}

// flushPendingLocked drains the content run into one token event.
func (r *remoteWatchRegistry) flushPendingLocked(send func(map[string]any), grants []string, turn *remoteLiveTurn, seq uint64) {
	if len(turn.pending) == 0 {
		return
	}
	text := trimRemoteStreamTail(strings.Join(turn.pending, ""), remoteStreamPartTextCap)
	if useSeq := turn.pendingSeq; useSeq > 0 {
		seq = useSeq
	}
	turn.pending = nil
	if turn.flushTimer != nil {
		turn.flushTimer.Stop()
		turn.flushTimer = nil
	}
	if text == "" {
		return
	}
	if n := len(turn.snapshot.Parts); n > 0 && turn.snapshot.Parts[n-1].Kind == "text" {
		part := &turn.snapshot.Parts[n-1]
		part.Text = trimRemoteStreamTail(part.Text+text, remoteStreamPartTextCap)
	} else {
		turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "text", Seq: seq, Text: text})
	}
	r.sendLocked(send, grants, turn.snapshot.SessionKey, turn.snapshot.RequestID, seq, "token", map[string]any{"text": text})
}

// closeThinkingLocked seals the open thinking block, shipping its full text
// in a single "done" event — the controller's folded 思考 box content.
func (r *remoteWatchRegistry) closeThinkingLocked(send func(map[string]any), grants []string, turn *remoteLiveTurn, seq uint64) {
	if !turn.thinkingOpen {
		return
	}
	// The open block is the newest unfinished thinking part; text flushed
	// after it may sit at the tail, so scan back rather than peek.
	for i := len(turn.snapshot.Parts) - 1; i >= 0; i-- {
		part := &turn.snapshot.Parts[i]
		if part.Kind != "thinking" || part.Done {
			continue
		}
		part.Done = true
		r.sendLocked(send, grants, turn.snapshot.SessionKey, turn.snapshot.RequestID, seq, "thinking", map[string]any{
			"state": "done",
			"text":  part.Text,
		})
		break
	}
	turn.thinkingOpen = false
}

// dropTrailingTextPartsLocked removes the tail text run a \x02 reset voids.
func (r *remoteWatchRegistry) dropTrailingTextPartsLocked(turn *remoteLiveTurn) {
	for len(turn.snapshot.Parts) > 0 && turn.snapshot.Parts[len(turn.snapshot.Parts)-1].Kind == "text" {
		turn.snapshot.Parts = turn.snapshot.Parts[:len(turn.snapshot.Parts)-1]
	}
}

func (r *remoteWatchRegistry) trimProgressLocked(turn *remoteLiveTurn) {
	count := 0
	for _, part := range turn.snapshot.Parts {
		if part.Kind == "progress" {
			count++
		}
	}
	overflow := count - remoteStreamProgressKeep
	if overflow <= 0 {
		return
	}
	out := turn.snapshot.Parts[:0]
	for _, part := range turn.snapshot.Parts {
		if part.Kind == "progress" && overflow > 0 {
			overflow--
			continue
		}
		out = append(out, part)
	}
	turn.snapshot.Parts = out
}

func (r *remoteWatchRegistry) turnForLocked(sessionKey, requestID string) *remoteLiveTurn {
	turn := r.turns[sessionKey]
	if turn == nil || (requestID != "" && turn.snapshot.RequestID != "" && turn.snapshot.RequestID != requestID) {
		if turn != nil && turn.flushTimer != nil {
			turn.flushTimer.Stop()
		}
		turn = newRemoteLiveTurn(sessionKey, requestID)
		r.turns[sessionKey] = turn
	}
	if requestID != "" && turn.snapshot.RequestID == "" {
		turn.snapshot.RequestID = requestID
	}
	return turn
}

func (r *remoteWatchRegistry) relayResponse(sessionKey, requestID string, raw json.RawMessage) {
	send := r.sender()
	if send == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	grants := r.grantsForLocked(sessionKey)
	if len(grants) == 0 {
		return
	}
	turn := r.turnForLocked(sessionKey, requestID)
	r.flushPendingLocked(send, grants, turn, 0)
	r.closeThinkingLocked(send, grants, turn, 0)
	turn.snapshot.StreamDone = true
	turn.snapshot.Responded = true
	r.sendLocked(send, grants, sessionKey, requestID, 0, "response", map[string]any{
		"response": raw,
	})
}

// sendLocked writes one envelope per watching grant. The grant id is the
// Hub's routing key: it resolves the controller machine and re-checks the
// grant is active, so events can only reach a machine the Hub authorized.
func (r *remoteWatchRegistry) sendLocked(send func(map[string]any), grants []string, sessionKey, requestID string, seq uint64, eventName string, extra map[string]any) {
	if len(grants) == 0 {
		return
	}
	for _, grantID := range grants {
		payload := map[string]any{
			"grant_id":    grantID,
			"session_key": sessionKey,
			"request_id":  requestID,
			"event":       eventName,
		}
		if path := projectPathFromSessionOwnerID(sessionKey); path != "" {
			payload["project_path"] = path
		}
		if seq > 0 {
			payload["seq"] = seq
		}
		for k, v := range extra {
			payload[k] = v
		}
		if raw, err := json.Marshal(payload); err == nil && len(raw) > remoteStreamEnvelopeSkip {
			log.Printf("[remote-control] stream event %s dropped: %d bytes over limit", eventName, len(raw))
			continue
		}
		send(payload)
	}
}

func trimRemoteStreamText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// trimRemoteStreamTail keeps the newest bytes of an accumulating part: the
// tail is what a thinking trail or long answer is still producing.
func trimRemoteStreamTail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return "…" + text[len(text)-limit:]
}

// --- ops --------------------------------------------------------------------

// remoteControlWatchTask registers a live mirror for one task and returns the
// in-flight turn so the controller paints mid-round state immediately.
func (a *App) remoteControlWatchTask(grantID string, body json.RawMessage) (any, error) {
	if strings.TrimSpace(grantID) == "" {
		return nil, errors.New("不支持这个远控操作")
	}
	var req struct {
		ProjectPath string `json:"project_path"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, errors.New("远控请求无效")
	}
	path, err := a.remoteControlKnownTask(req.ProjectPath)
	if err != nil {
		return nil, err
	}
	sessionKey := projectSessionOwnerID(path)
	reg := a.ensureRemoteWatchRegistry()
	reg.watch(grantID, sessionKey)
	return map[string]any{
		"watching": true,
		"live":     reg.liveSnapshot(sessionKey),
	}, nil
}

func (a *App) remoteControlUnwatchTask(grantID string, body json.RawMessage) (any, error) {
	var req struct {
		ProjectPath string `json:"project_path"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, errors.New("远控请求无效")
	}
	sessionKey := projectSessionOwnerID(normalizeProjectSessionPath(req.ProjectPath))
	if reg := a.remoteWatchRegistryOrNil(); reg != nil {
		reg.unwatch(grantID, sessionKey)
	}
	return map[string]any{"ok": true}, nil
}

// remoteControlTouchWatch keeps an open watch alive while the controller
// polls the task; it is why a watch can outlive remoteWatchTTL quietly.
func (a *App) remoteControlTouchWatch(grantID, projectPath string) {
	if strings.TrimSpace(grantID) == "" {
		return
	}
	if reg := a.remoteWatchRegistryOrNil(); reg != nil {
		reg.mu.Lock()
		reg.watchLocked(grantID, projectSessionOwnerID(normalizeProjectSessionPath(projectPath)))
		reg.mu.Unlock()
	}
}
