package guiapp

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Remote-control live mirror.
//
// A controlling desktop opens a remote task and sends the watch_task op; from
// then on every assistant stream event for that session is relayed over the
// Hub as a remote_control.stream envelope so the controller renders the same
// round the local panel does. The in-process subscriber sees each viewer
// event before the wire copy. Proto 2 coalesces body and reasoning at 80ms.
// Proto 1 keeps the 150ms body window and omits the new fields. Progress does
// not seal an open thinking block.

// remoteStreamPart mirrors RemoteLivePart in remoteAssistantSession.ts. The
// same shape is used on the wire and inside the watch snapshot, so a watcher
// joining mid-turn and one receiving deltas see identical state.
type remoteStreamPart struct {
	Kind   string                   `json:"kind"` // "text" | "thinking" | "progress"
	Seq    uint64                   `json:"seq"`
	Text   string                   `json:"text"`
	Done   bool                     `json:"done,omitempty"` // thinking parts only
	Coding *codingAgentEventPayload `json:"coding,omitempty"`
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
	Cursor      uint64             `json:"cursor,omitempty"`
}

// remoteLiveTurn tracks the relay state of one in-flight request so a late
// watcher can be handed the parts accumulated so far.
type remoteLiveTurn struct {
	snapshot            remoteLiveSnapshot
	thinkingOpen        bool     // the tail part is an unfinished thinking block
	pending             []string // content-lane deltas not yet sealed into parts
	pendingSeq          uint64   // sequence of the first pending delta
	pendingFrom         uint64   // cursor of the first pending content delta
	pendingTo           uint64   // cursor of the last pending content delta
	pendingBytes        int
	pendingClipped      bool // front pieces were dropped to stay under the part cap
	pendingThink        []string
	pendingThinkSeq     uint64
	pendingThinkFrom    uint64
	pendingThinkTo      uint64
	pendingThinkBytes   int
	pendingThinkClipped bool
	// thinkStaged means pendingThink is the only copy of those deltas.
	// A watched proto-2 window also fills the open part, and then this is false.
	thinkStaged bool
	cursor      uint64
	// flushTimer is armed only while someone is watching. An unwatched turn
	// keeps its tail in pending until the next watch_task, with no timer.
	flushTimer *time.Timer
	// flushDue means the coalesce window elapsed. The writer emits that
	// tail. The timer must not deliver it: a higher cursor may already be
	// leaving on the writer, and the reducer would drop that frame and
	// then gap on every later one.
	flushDue bool
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

	mu        sync.Mutex
	watchMu   sync.Mutex
	watches   map[string]map[string]time.Time // grantID → sessionKey → expiry
	protos    map[string]map[string]int       // grantID → sessionKey → negotiated proto
	turns     map[string]*remoteLiveTurn      // sessionKey → live turn
	highWater map[string]uint64               // last folded cursor, kept after a settled turn is dropped

	// pending is the real wire queue. wireQueue is only a capacity-1 wakeup.
	// publishViewerEvent holds the per-session gate from cursor allocation
	// until relayCopy returns, so a higher cursor cannot be admitted while
	// a lower one is still between allocation and this queue. relayCopy
	// appends or overflows under r.mu, and the writer cuts both structures
	// under that same lock. The local subscriber has already been delivered.
	pending   []viewerEvent
	wireQueue chan struct{}
	queueOnce sync.Once
	gapped    map[string]bool
	dropped   map[string][]viewerEvent // events that missed the queue, folded into the next snapshot
	// aside holds events that arrived after a session left the wire but
	// while an older cursor for that session is still queued. They are not
	// part of the 512 cap. The writer folds them in cursor order.
	aside []viewerEvent
	// outbound is frames already folded that still have to be written by the
	// writer, so a publish goroutine never writes the hub socket itself.
	outbound []map[string]any
	// holdWrite, when set, blocks the writer before each read so tests can fill the queue.
	holdWrite chan struct{}
}

func (a *App) remoteWatchRegistryOrNil() *remoteWatchRegistry {
	if a == nil {
		return nil
	}
	a.remoteInfraMu.RLock()
	defer a.remoteInfraMu.RUnlock()
	return a.remoteWatches
}

func newRemoteWatchRegistry(a *App) *remoteWatchRegistry {
	return &remoteWatchRegistry{
		app:     a,
		watches: map[string]map[string]time.Time{},
		protos:  map[string]map[string]int{},
		turns:   map[string]*remoteLiveTurn{},
		gapped:  map[string]bool{},
		dropped: map[string][]viewerEvent{},
	}
}

func (a *App) ensureRemoteWatchRegistry() *remoteWatchRegistry {
	if reg := a.remoteWatchRegistryOrNil(); reg != nil {
		return reg
	}
	a.remoteInfraMu.Lock()
	defer a.remoteInfraMu.Unlock()
	if a.remoteWatches == nil {
		a.remoteWatches = newRemoteWatchRegistry(a)
	}
	return a.remoteWatches
}

// --- watch management -------------------------------------------------------

func (r *remoteWatchRegistry) watch(grantID, sessionKey string) {
	r.watchWithProto(grantID, sessionKey, 1)
}

func (r *remoteWatchRegistry) watchWithProto(grantID, sessionKey string, proto int) {
	r.watchMu.Lock()
	wasWatched := r.sessionWatchedLocked(sessionKey)
	r.watchLocked(grantID, sessionKey)
	if proto < 1 {
		proto = 1
	}
	set := r.protos[grantID]
	if set == nil {
		set = map[string]int{}
		r.protos[grantID] = set
	}
	set[sessionKey] = proto
	r.watchMu.Unlock()
	if wasWatched {
		return
	}
	// Release watchMu before r.mu. unwatch and the writer take r.mu first.
	// The first watcher paints from watch_task. Seal the tail now so the
	// next coalesce frame starts after the cursor this reply claims.
	r.mu.Lock()
	if turn := r.turns[sessionKey]; turn != nil {
		r.sealPendingLocked(turn)
	}
	r.mu.Unlock()
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
	// r.mu then watchMu. The writer holds r.mu and then watchMu; the other
	// order deadlocks a watch expiry against a flush.
	r.mu.Lock()
	defer r.mu.Unlock()
	r.watchMu.Lock()
	if set := r.watches[grantID]; set != nil {
		delete(set, sessionKey)
		if len(set) == 0 {
			delete(r.watches, grantID)
		}
	}
	if set := r.protos[grantID]; set != nil {
		delete(set, sessionKey)
		if len(set) == 0 {
			delete(r.protos, grantID)
		}
	}
	still := r.sessionWatchedLocked(sessionKey)
	r.watchMu.Unlock()
	if still {
		return
	}
	turn := r.turns[sessionKey]
	if turn == nil {
		return
	}
	// An in-flight turn stays. The next controller paints it from watch_task
	// instead of seeding at 0 and gapping on the next cursor. A settled turn
	// is not returned by watch_task, so it can go.
	r.sealPendingLocked(turn)
	if turn.snapshot.Responded {
		r.dropTurnLocked(sessionKey)
	}
}

// grantsForLocked lists the non-expired watchers of one session and removes
// the dead entries it passes.
func (r *remoteWatchRegistry) grantsFor(sessionKey string) []string {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	return r.grantsForLocked(sessionKey)
}

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

func (r *remoteWatchRegistry) sessionWatched(sessionKey string) bool {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	return r.sessionWatchedLocked(sessionKey)
}

func (r *remoteWatchRegistry) sessionWatchedLocked(sessionKey string) bool {
	return len(r.grantsForLocked(sessionKey)) > 0
}

func (r *remoteWatchRegistry) sessionProto2(sessionKey string) bool {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()
	now := time.Now()
	for grantID, set := range r.watches {
		expires, ok := set[sessionKey]
		if !ok || now.After(expires) {
			continue
		}
		if r.protos[grantID][sessionKey] >= 2 {
			return true
		}
	}
	return false
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
	return shrinkLiveSnapshot(r.liveSnapshotUnlocked(sessionKey, false))
}

// foldedCursor is the highest event cursor folded into this session.
// The watch reply uses it as the seed watermark: cursors at or below are
// either in the snapshot or already on the wire. The mark survives after a
// settled turn is dropped, so the next frame does not look like a gap.
func (r *remoteWatchRegistry) foldedCursor(sessionKey string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if turn := r.turns[sessionKey]; turn != nil && turn.cursor > 0 {
		return turn.cursor
	}
	return r.highWater[sessionKey]
}

// --- relay ------------------------------------------------------------------

// relayRemoteStreamEvent is the emitAIAssistantStreamEvent hook. Unwatched
// sessions fold into the live turn and do not touch the Hub.
func (a *App) relayRemoteStreamEvent(name string, event AIAssistantStreamEvent) {
	if !viewerStreamAllowed(name) {
		return
	}
	for _, ev := range viewerEventsFromStream(name, event) {
		// A repeated progress delivery (same step republished while it runs)
		// collapses here, before cursor allocation: consumers render one row
		// per delivery, and older phone builds have no reducer-side dedup.
		// Heartbeats and control lanes pass through untouched.
		if ev.Event == "progress" && ev.Text != "" && ev.SessionKey != "" {
			if tail, ok := viewerProgressTail.Load(ev.SessionKey); ok && tail.(string) == ev.Text {
				continue
			}
			viewerProgressTail.Store(ev.SessionKey, ev.Text)
		}
		ev.CursorFrom = ev.Cursor
		a.publishViewerEvent(ev)
	}
}

// relayRemoteAssistantResponse is the emitAIAssistantResponse hook; the
// settled response mirrors to watchers as the terminal "response" event.
func (a *App) relayRemoteAssistantResponse(requestID string, resp *IMAgentResponse) {
	if resp == nil {
		return
	}
	sessionKey := strings.TrimSpace(resp.SessionKey)
	if sessionKey == "" {
		return
	}
	for _, ev := range thinkingDoneBeforeResponse(resp) {
		ev.RequestID = requestID
		ev.CursorFrom = ev.Cursor
		a.publishViewerEvent(ev)
	}
	ev, _ := viewerEventFromResponse(requestID, resp)
	ev.CursorFrom = ev.Cursor
	a.publishViewerEvent(ev)
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

// relayRemoteWorkbenchSteps is the emitCodingWorkbenchStepsUpdate hook: the
// plan-checklist snapshot the local panel just received also goes to every
// watcher, so the controller renders the same step list in sync.
func (a *App) relayRemoteWorkbenchSteps(sessionKey string, payload map[string]interface{}) {
	sessionKey = strings.TrimSpace(sessionKey)
	if a == nil || sessionKey == "" || len(payload) == 0 {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := viewerEvent{
		SessionKey: sessionKey, ProjectPath: projectPathFromSessionOwnerID(sessionKey),
		Event: "workbench_steps", Lane: "control", Workbench: raw,
	}
	a.publishViewerEvent(ev)
}

func trimRemoteStreamText(text string, limit int) string {
	if limit <= 0 || text == "" {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if cut == 0 {
		return ""
	}
	return text[:cut] + "…"
}

// trimRemoteStreamTail keeps the newest bytes of an accumulating part: the
// tail is what a thinking trail or long answer is still producing. The cut
// stays on a rune boundary. A limit that lands inside the final rune keeps
// that rune.
func trimRemoteStreamTail(text string, limit int) string {
	if limit <= 0 || text == "" {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	if cut < 0 {
		cut = 0
	}
	if cut > 0 && !utf8.RuneStart(text[cut]) {
		next := cut + 1
		for next < len(text) && !utf8.RuneStart(text[next]) {
			next++
		}
		if next < len(text) {
			cut = next
		} else {
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
		}
	}
	if cut >= len(text) {
		return ""
	}
	return "…" + text[cut:]
}

// appendCappedTail concatenates until the cap, then keeps the rune-safe tail.
func appendCappedTail(dst, add string, limit int) string {
	if add == "" {
		return dst
	}
	if limit <= 0 {
		return ""
	}
	if len(dst)+len(add) <= limit {
		return dst + add
	}
	return trimRemoteStreamTail(dst+add, limit)
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
		StreamProto int    `json:"stream_proto"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, errors.New("远控请求无效")
	}
	path, err := a.remoteControlKnownTask(req.ProjectPath)
	if err != nil {
		return nil, err
	}
	sessionKey := projectSessionOwnerID(path)
	proto := negotiateStreamProto(req.StreamProto)
	reg := a.ensureRemoteWatchRegistry()
	reg.watchWithProto(grantID, sessionKey, proto)
	live := reg.liveSnapshot(sessionKey)
	// The reply cursor claims coverage only up to what the wire writer has
	// folded: anything still queued is absent from the snapshot but will
	// arrive as a frame, so seeding past the folded mark would drop it.
	cursor := reg.foldedCursor(sessionKey)
	return map[string]any{
		"watching":     true,
		"stream_proto": proto,
		"cursor":       cursor,
		"live":         live,
		"workbench":    shrinkWorkbenchMap(a.remoteWorkbenchSnapshot(sessionKey)),
	}, nil
}

// remoteWorkbenchSnapshot reads the live coding-workbench checklist for one
// session so a watcher opening mid-plan paints the same step list. Nil when
// no handler or no workbench memory exists yet.
func (a *App) remoteWorkbenchSnapshot(sessionKey string) map[string]interface{} {
	hubClient := a.hubClient()
	if hubClient == nil {
		return nil
	}
	handler := hubClient.peekIMHandler()
	if handler == nil {
		return nil
	}
	return handler.codingWorkbenchStepsSnapshotForSession(sessionKey)
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
		reg.watchMu.Lock()
		reg.watchLocked(grantID, projectSessionOwnerID(normalizeProjectSessionPath(projectPath)))
		reg.watchMu.Unlock()
	}
}
