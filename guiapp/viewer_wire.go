package guiapp

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

const remoteStreamQueueCap = 512

func (r *remoteWatchRegistry) wireReady() bool {
	if r == nil || r.app == nil {
		return false
	}
	if r.sink != nil {
		return true
	}
	return r.app.hubClient() != nil
}

func (r *remoteWatchRegistry) ensureWireQueue() {
	r.queueOnce.Do(func() {
		r.wireQueue = make(chan struct{}, 1)
		go r.writeLoop()
	})
}

func (r *remoteWatchRegistry) relayCopy(ev viewerEvent) {
	if r == nil || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	// Watched sessions take the wire queue. Everyone else still folds into the
	// turn so a controller that opens mid-round gets the text and a cursor
	// the next frame can follow. That fold must not enter the 512 queue:
	// unwatched chats would push a watched session into overflow.
	if r.wireReady() && r.sessionWatched(ev.SessionKey) {
		r.ensureWireQueue()
		r.mu.Lock()
		if len(r.pending) >= remoteStreamQueueCap {
			r.noteGapLocked(ev)
		} else {
			r.pending = append(r.pending, ev)
		}
		r.mu.Unlock()
		select {
		case r.wireQueue <- struct{}{}:
		default:
		}
		return
	}
	r.remember(ev)
}

// remember folds one event into the live turn and does not start the writer
// unless an older cursor for this session is still queued. Folding the new
// cursor first lets watch_task seed past that queued event, and the reducer
// then drops it. The queued tail waits in aside, outside the 512 cap.
// Unwatched tokens stay in the turn's pending buffer with no coalesce timer.
func (r *remoteWatchRegistry) remember(ev viewerEvent) {
	r.mu.Lock()
	if r.sessionBusyLocked(ev.SessionKey) {
		r.noteAsideLocked(ev)
		r.mu.Unlock()
		r.wakeWriter()
		return
	}
	frames := r.foldLocked(ev)
	if !r.sessionWatched(ev.SessionKey) {
		if turn := r.turns[ev.SessionKey]; turn != nil && turn.snapshot.Responded {
			r.dropTurnLocked(ev.SessionKey)
		}
		frames = nil
	} else if len(frames) > 0 && r.wireReady() {
		r.outbound = append(r.outbound, frames...)
		frames = nil
		r.mu.Unlock()
		r.wakeWriter()
		return
	}
	r.mu.Unlock()
	for _, frame := range frames {
		r.deliver(frame)
	}
}

// sessionBusyLocked reports whether this session still has a cursor the
// writer has not folded. Caller holds r.mu.
func (r *remoteWatchRegistry) sessionBusyLocked(session string) bool {
	if len(r.aside) > 0 {
		for i := range r.aside {
			if r.aside[i].SessionKey == session {
				return true
			}
		}
	}
	if len(r.dropped[session]) > 0 {
		return true
	}
	for i := range r.pending {
		if r.pending[i].SessionKey == session {
			return true
		}
	}
	return false
}

// noteAsideLocked parks an event behind a cursor the writer still owns.
// Adjacent body or thinking deltas collapse the same way as queue drops.
// Caller holds r.mu.
func (r *remoteWatchRegistry) noteAsideLocked(ev viewerEvent) {
	// Merge only into this session's latest parked event, and only when no
	// queued cursor sits after it. A re-watch can enqueue a higher cursor
	// between two parked tails; folding the merged text after that cursor
	// would swap the body.
	for i := len(r.aside) - 1; i >= 0; i-- {
		if r.aside[i].SessionKey != ev.SessionKey {
			continue
		}
		if !r.sessionQueuedAfterLocked(ev.SessionKey, r.aside[i].Cursor) && mergeDroppedTail(&r.aside[i], ev) {
			return
		}
		break
	}
	r.aside = append(r.aside, ev)
}

func (r *remoteWatchRegistry) sessionQueuedAfterLocked(session string, cursor uint64) bool {
	for i := range r.pending {
		if r.pending[i].SessionKey == session && r.pending[i].Cursor >= cursor {
			return true
		}
	}
	for _, ev := range r.dropped[session] {
		if ev.Cursor >= cursor {
			return true
		}
	}
	return false
}

func (r *remoteWatchRegistry) wakeWriter() {
	r.ensureWireQueue()
	select {
	case r.wireQueue <- struct{}{}:
	default:
	}
}

// noteGapLocked records an event the queue could not take. Caller holds r.mu.
// Adjacent body or thinking deltas collapse into one tail so a stalled writer
// does not retain every token until the next cut.
func (r *remoteWatchRegistry) noteGapLocked(ev viewerEvent) {
	if r.gapped == nil {
		r.gapped = map[string]bool{}
	}
	if r.dropped == nil {
		r.dropped = map[string][]viewerEvent{}
	}
	r.gapped[ev.SessionKey] = true
	list := r.dropped[ev.SessionKey]
	if n := len(list); n > 0 && mergeDroppedTail(&list[n-1], ev) {
		r.dropped[ev.SessionKey] = list
		return
	}
	r.dropped[ev.SessionKey] = append(list, ev)
}

func mergeDroppedTail(prev *viewerEvent, ev viewerEvent) bool {
	if prev.SessionKey != ev.SessionKey || prev.RequestID != ev.RequestID || prev.Event != ev.Event {
		return false
	}
	limit := 0
	switch ev.Event {
	case "token":
		if strings.HasPrefix(ev.Text, rejectedRoundContentToken) || strings.HasPrefix(prev.Text, rejectedRoundContentToken) {
			return false
		}
		limit = remoteStreamPartTextCap
	case "thinking":
		if ev.State != "delta" || prev.State != "delta" {
			return false
		}
		limit = remoteStreamPartThinkingCap
	default:
		return false
	}
	if prev.CursorFrom == 0 {
		prev.CursorFrom = prev.Cursor
	}
	prev.Text = trimRemoteStreamTail(prev.Text+ev.Text, limit)
	if prev.Text != "" {
		prev.Pieces = []string{prev.Text}
	} else {
		prev.Pieces = nil
	}
	if ev.Cursor > prev.Cursor {
		prev.Cursor = ev.Cursor
	}
	return true
}

func (r *remoteWatchRegistry) writeLoop() {
	for {
		if r.holdWrite != nil {
			if _, ok := <-r.holdWrite; !ok {
				r.holdWrite = nil
			}
		}
		if _, ok := <-r.wireQueue; !ok {
			return
		}
		r.mu.Lock()
		// The coalesce tail holds lower cursors than anything still queued.
		// Emit it before this drain's frames, on this same goroutine.
		due := r.consumeDueFlushesLocked()
		frames, snaps := r.drainLocked()
		outbound := r.outbound
		r.outbound = nil
		r.mu.Unlock()
		// Shrink and the workbench read run after the queue lock. Disk persist
		// holds the same per-user lock the checklist read needs, and the shrink
		// walks the whole turn; neither may sit inside r.mu.
		for i := range snaps {
			snaps[i] = r.finishSnapshot(snaps[i])
			frames = append(frames, r.fanoutLocked(snaps[i])...)
		}
		if len(due) > 0 {
			frames = append(due, frames...)
		}
		// remember folded outbound before it released the session gate. This
		// drain can already hold a higher cursor of the same session. Those
		// frames meet only here, so the lower cursor has to be inserted
		// before that higher one. Appending delivers the higher cursor
		// first: the reducer gaps and does not advance, then the lower
		// frame applies and clears the gap, and the skipped token is gone.
		frames = mergeOutboundFrames(frames, outbound)
		for _, frame := range frames {
			r.deliver(frame)
		}
	}
}

// mergeOutboundFrames places frames folded on the publish goroutine ahead of
// any same-session frame in this deliver batch with a higher cursor. outbound
// is already in admission order. A cursor of 0 is left in place: proto 1
// frames have none. Cursors are per session, so another session keeps the
// position this batch already gave it.
func mergeOutboundFrames(frames, outbound []map[string]any) []map[string]any {
	if len(outbound) == 0 {
		return frames
	}
	if len(frames) == 0 {
		return outbound
	}
	bySession := map[string][]map[string]any{}
	for _, frame := range outbound {
		session := viewerText(frame["session_key"])
		bySession[session] = append(bySession[session], frame)
	}
	used := map[string]int{}
	out := make([]map[string]any, 0, len(frames)+len(outbound))
	for _, frame := range frames {
		session := viewerText(frame["session_key"])
		rest := bySession[session]
		i := used[session]
		cursor := viewerNum(frame["cursor"])
		for i < len(rest) && viewerNum(rest[i]["cursor"]) > 0 && viewerNum(rest[i]["cursor"]) < cursor {
			out = append(out, rest[i])
			i++
		}
		used[session] = i
		out = append(out, frame)
	}
	seen := map[string]int{}
	for _, frame := range outbound {
		session := viewerText(frame["session_key"])
		seen[session]++
		if seen[session] <= used[session] {
			continue
		}
		out = append(out, frame)
	}
	return out
}

// drainLocked cuts the queue and the overflow list together. Caller holds r.mu.
// Snapshot events are returned unenveloped so the caller can shrink them
// after releasing r.mu.
func (r *remoteWatchRegistry) drainLocked() ([]map[string]any, []viewerEvent) {
	queued := r.pending
	r.pending = nil
	dropped := flattenDropped(r.dropped)
	r.dropped = map[string][]viewerEvent{}
	aside := r.aside
	r.aside = nil
	r.gapped = map[string]bool{}
	fold, snapshot := planOverflowBatch(queued, dropped)
	fold = insertAside(fold, aside)
	frames, snaps := r.emitFoldLocked(fold, snapshot, dropped)
	// A response parked in aside settles the turn only when the writer folds
	// it. Drop after the snapshot is taken so the resync still carries the
	// final text.
	r.dropSettledUnwatchedLocked(fold)
	return frames, snaps
}

// insertAside places events that waited out a queued prefix into cursor
// order within their session. Aside cursors are not drops: the caller still
// emits them. fold is not appended to; an empty-drop plan aliases the queued
// slice.
func insertAside(fold, aside []viewerEvent) []viewerEvent {
	if len(aside) == 0 {
		return fold
	}
	bySession := map[string][]viewerEvent{}
	for _, ev := range aside {
		bySession[ev.SessionKey] = append(bySession[ev.SessionKey], ev)
	}
	used := map[string]int{}
	out := make([]viewerEvent, 0, len(fold)+len(aside))
	for _, ev := range fold {
		rest := bySession[ev.SessionKey]
		i := used[ev.SessionKey]
		for i < len(rest) && rest[i].Cursor > 0 && rest[i].Cursor < ev.Cursor {
			out = append(out, rest[i])
			i++
		}
		used[ev.SessionKey] = i
		out = append(out, ev)
	}
	seen := map[string]int{}
	for _, ev := range aside {
		seen[ev.SessionKey]++
		if seen[ev.SessionKey] <= used[ev.SessionKey] {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// dropSettledUnwatchedLocked releases a turn watch_task will not serve.
// The folded cursor stays in highWater. Caller holds r.mu. Runs after the
// resync snapshot is copied.
func (r *remoteWatchRegistry) dropSettledUnwatchedLocked(fold []viewerEvent) {
	seen := map[string]bool{}
	for _, ev := range fold {
		if seen[ev.SessionKey] {
			continue
		}
		seen[ev.SessionKey] = true
		turn := r.turns[ev.SessionKey]
		if turn == nil || !turn.snapshot.Responded || r.sessionWatched(ev.SessionKey) {
			continue
		}
		r.dropTurnLocked(ev.SessionKey)
	}
}

func (r *remoteWatchRegistry) deliver(frame map[string]any) {
	if r.sink != nil {
		r.sink(frame)
		return
	}
	if hub := r.app.hubClient(); hub != nil {
		hub.sendRemoteControlStream(frame)
	}
}

// planOverflowBatch merges queued events with drops in cursor order.
// snapshot is true only for a full queue whose drops are a strict suffix,
// so a short batch that still holds a lower cursor cannot publish a resync
// past it. Dropped events are never placed ahead of a lower queued cursor.
func planOverflowBatch(queued, dropped []viewerEvent) (fold []viewerEvent, snapshot bool) {
	return planOverflowBatchCap(queued, dropped, remoteStreamQueueCap)
}

func planOverflowBatchCap(queued, dropped []viewerEvent, cap int) (fold []viewerEvent, snapshot bool) {
	if len(dropped) == 0 {
		return queued, false
	}
	pendingDrop := map[string][]viewerEvent{}
	for _, ev := range dropped {
		pendingDrop[ev.SessionKey] = append(pendingDrop[ev.SessionKey], ev)
	}
	used := map[string]int{}
	for _, ev := range queued {
		rest := pendingDrop[ev.SessionKey]
		i := used[ev.SessionKey]
		for i < len(rest) && rest[i].Cursor > 0 && rest[i].Cursor < ev.Cursor {
			fold = append(fold, rest[i])
			i++
		}
		used[ev.SessionKey] = i
		fold = append(fold, ev)
	}
	seen := map[string]int{}
	for _, ev := range dropped {
		seen[ev.SessionKey]++
		if seen[ev.SessionKey] <= used[ev.SessionKey] {
			continue
		}
		fold = append(fold, ev)
	}
	snapshot = len(dropped) > 0 && len(queued) >= cap && dropCursorsAreSuffix(queued, dropped)
	return fold, snapshot
}

func dropCursorsAreSuffix(queued, dropped []viewerEvent) bool {
	maxQueued := map[string]uint64{}
	hasQueued := map[string]bool{}
	for _, ev := range queued {
		if !hasQueued[ev.SessionKey] || ev.Cursor > maxQueued[ev.SessionKey] {
			maxQueued[ev.SessionKey] = ev.Cursor
		}
		hasQueued[ev.SessionKey] = true
	}
	for _, ev := range dropped {
		if ev.Cursor == 0 {
			return false
		}
		if hasQueued[ev.SessionKey] && maxQueued[ev.SessionKey] >= ev.Cursor {
			return false
		}
	}
	return true
}

func flattenDropped(dropped map[string][]viewerEvent) []viewerEvent {
	if len(dropped) == 0 {
		return nil
	}
	sessions := make([]string, 0, len(dropped))
	for session := range dropped {
		sessions = append(sessions, session)
	}
	sort.Strings(sessions)
	var out []viewerEvent
	for _, session := range sessions {
		out = append(out, dropped[session]...)
	}
	return out
}

// emitFoldLocked folds in plan order. Dropped events update the turn and are
// not sent again. A resync snapshot is emitted only when the plan says the
// queue no longer holds a lower cursor. Caller holds r.mu.
func (r *remoteWatchRegistry) emitFoldLocked(fold []viewerEvent, snapshot bool, dropped []viewerEvent) ([]map[string]any, []viewerEvent) {
	if len(dropped) == 0 {
		var frames []map[string]any
		for _, ev := range fold {
			frames = append(frames, r.foldLocked(ev)...)
		}
		return frames, nil
	}
	isDrop := map[string]map[uint64]bool{}
	var sessions []string
	seenSession := map[string]bool{}
	for _, ev := range dropped {
		if isDrop[ev.SessionKey] == nil {
			isDrop[ev.SessionKey] = map[uint64]bool{}
		}
		isDrop[ev.SessionKey][ev.Cursor] = true
		if !seenSession[ev.SessionKey] {
			seenSession[ev.SessionKey] = true
			sessions = append(sessions, ev.SessionKey)
		}
	}
	sort.Strings(sessions)
	var frames []map[string]any
	for _, ev := range fold {
		got := r.foldLocked(ev)
		if isDrop[ev.SessionKey] == nil || !isDrop[ev.SessionKey][ev.Cursor] {
			frames = append(frames, got...)
		}
	}
	var snaps []viewerEvent
	if snapshot {
		for _, session := range sessions {
			snaps = append(snaps, r.snapshotFrameLocked(session))
		}
	}
	return frames, snaps
}

func (r *remoteWatchRegistry) foldLocked(ev viewerEvent) []map[string]any {
	var frames []map[string]any
	turn := r.turnForLocked(ev.SessionKey, ev.RequestID)
	if ev.Cursor > turn.cursor {
		turn.cursor = ev.Cursor
		turn.snapshot.Cursor = ev.Cursor
	}
	if turn.cursor > 0 {
		if r.highWater == nil {
			r.highWater = map[string]uint64{}
		}
		if turn.cursor > r.highWater[ev.SessionKey] {
			r.highWater[ev.SessionKey] = turn.cursor
		}
	}
	window := remoteStreamTokenWindow
	if r.sessionProto2(ev.SessionKey) {
		window = 80 * time.Millisecond
	}
	switch ev.Event {
	case "token":
		if strings.HasPrefix(ev.Text, rejectedRoundContentToken) {
			frames = append(frames, r.flushOpenLocked(turn)...)
			r.dropTrailingTextPartsLocked(turn)
			// Copy before fanout. Pieces share the caller's backing array.
			text := visibleRejectToken(ev.Text)
			body := strings.TrimPrefix(text, rejectedRoundContentToken)
			if body != "" {
				turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "text", Seq: ev.Seq, Text: body})
			}
			ev.Text = text
			if len(ev.Pieces) > 0 {
				cleaned := make([]string, len(ev.Pieces))
				for i, piece := range ev.Pieces {
					if strings.HasPrefix(piece, rejectedRoundContentToken) {
						cleaned[i] = visibleRejectToken(piece)
					} else {
						cleaned[i] = textutil.SanitizeVisibleChatText(piece)
					}
				}
				ev.Pieces = cleaned
			}
			frames = append(frames, r.fanoutLocked(ev)...)
			return frames
		}
		r.bufferContentLocked(turn, ev, window)
		return frames
	case "thinking":
		if ev.State == "done" {
			frames = append(frames, r.flushOpenLocked(turn)...)
			r.sealThinkingLocked(turn, ev.Text)
			frames = append(frames, r.fanoutLocked(ev)...)
			return frames
		}
		if ev.State == "started" {
			frames = append(frames, r.flushOpenLocked(turn)...)
			turn.thinkingOpen = true
			turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "thinking", Seq: ev.Seq})
			frames = append(frames, r.fanoutLocked(ev)...)
			return frames
		}
		if !r.sessionWatched(ev.SessionKey) {
			// No watcher is waiting on a per-delta copy. Stage the piece and
			// join it once, when the turn is sealed or snapshotted.
			r.stageThinkingLocked(turn, ev)
			return frames
		}
		if turn.thinkStaged {
			r.materializeStagedThinkingLocked(turn)
		}
		part := r.openThinkingPartLocked(turn)
		if part != nil {
			part.Text = appendCappedTail(part.Text, ev.Text, remoteStreamPartThinkingCap)
		}
		if !r.sessionProto2(ev.SessionKey) {
			frames = append(frames, r.flushOpenLocked(turn)...)
			frames = append(frames, r.fanoutLocked(ev)...)
			return frames
		}
		r.bufferThinkingLocked(turn, ev, window)
		return frames
	case "progress":
		frames = append(frames, r.flushOpenLocked(turn)...)
		text := ev.Text
		part := remoteStreamPart{Kind: "progress", Seq: ev.Seq, Text: text}
		if ev.Coding != nil {
			coding := *ev.Coding
			part.Coding = &coding
		}
		turn.snapshot.Parts = append(turn.snapshot.Parts, part)
		r.trimProgressLocked(turn)
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	case "heartbeat":
		// A keepalive during the coalesce window still owns a higher cursor.
		// The buffered tail has to leave first or the reducer drops this
		// frame and then gaps on every later one.
		frames = append(frames, r.flushOpenLocked(turn)...)
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	case "response":
		frames = append(frames, r.flushOpenLocked(turn)...)
		if turn.snapshot.RequestID == "" || turn.snapshot.RequestID == ev.RequestID {
			turn.snapshot.StreamDone = true
			turn.snapshot.Responded = true
			// The response frame itself may be the one the queue dropped.
			// The resync snapshot is the copy the remote still receives,
			// so the final has to live in parts.
			r.settleResponseTextLocked(turn, ev)
		}
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	case "stream_done":
		frames = append(frames, r.flushOpenLocked(turn)...)
		turn.snapshot.StreamDone = true
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	case "new_round", "round_started":
		frames = append(frames, r.flushOpenLocked(turn)...)
		if ev.Event == "round_started" && (turn.snapshot.RequestID != ev.RequestID || turn.snapshot.Responded) {
			fresh := newRemoteLiveTurn(ev.SessionKey, ev.RequestID)
			fresh.cursor = ev.Cursor
			fresh.snapshot.Cursor = ev.Cursor
			fresh.snapshot.DisplayText = ev.DisplayText
			r.turns[ev.SessionKey] = fresh
			turn = fresh
		} else if ev.DisplayText != "" {
			turn.snapshot.DisplayText = ev.DisplayText
		}
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	default:
		frames = append(frames, r.flushOpenLocked(turn)...)
		frames = append(frames, r.fanoutLocked(ev)...)
		return frames
	}
}

func (r *remoteWatchRegistry) bufferContentLocked(turn *remoteLiveTurn, ev viewerEvent, window time.Duration) {
	if len(turn.pending) == 0 {
		turn.pendingSeq = ev.Seq
		turn.pendingFrom = ev.Cursor
		turn.pendingBytes = 0
		turn.pendingClipped = false
	}
	if ev.Cursor > turn.pendingTo {
		turn.pendingTo = ev.Cursor
	}
	if ev.Text != "" {
		turn.pending = append(turn.pending, ev.Text)
		turn.pendingBytes += len(ev.Text)
	}
	if !r.sessionWatched(turn.snapshot.SessionKey) {
		turn.pending = clipPendingPieces(turn.pending, &turn.pendingBytes, &turn.pendingClipped, remoteStreamPartTextCap)
		return
	}
	r.armFlushLocked(turn, window)
}

func (r *remoteWatchRegistry) bufferThinkingLocked(turn *remoteLiveTurn, ev viewerEvent, window time.Duration) {
	if len(turn.pendingThink) == 0 {
		turn.pendingThinkSeq = ev.Seq
		turn.pendingThinkFrom = ev.Cursor
		turn.pendingThinkBytes = 0
		turn.pendingThinkClipped = false
	}
	if ev.Cursor > turn.pendingThinkTo {
		turn.pendingThinkTo = ev.Cursor
	}
	if ev.Text != "" {
		turn.pendingThink = append(turn.pendingThink, ev.Text)
		turn.pendingThinkBytes += len(ev.Text)
	}
	r.armFlushLocked(turn, window)
}

// stageThinkingLocked records a reasoning delta that nobody is watching.
// The open part stays untouched until seal, so a long thought does not
// copy its whole text on every token. A proto-2 window that already copied
// its pieces onto the part is dropped here; that text is not staged twice.
func (r *remoteWatchRegistry) stageThinkingLocked(turn *remoteLiveTurn, ev viewerEvent) {
	if !turn.thinkStaged && len(turn.pendingThink) > 0 {
		if r.openThinkingPartLocked(turn) == nil {
			turn.thinkStaged = true
		} else {
			turn.pendingThink = nil
			turn.pendingThinkBytes = 0
			turn.pendingThinkClipped = false
			turn.pendingThinkFrom = 0
			turn.pendingThinkTo = 0
		}
	}
	if len(turn.pendingThink) == 0 {
		turn.pendingThinkSeq = ev.Seq
		turn.pendingThinkFrom = ev.Cursor
		turn.pendingThinkBytes = 0
		turn.pendingThinkClipped = false
	}
	if ev.Cursor > turn.pendingThinkTo {
		turn.pendingThinkTo = ev.Cursor
	}
	if ev.Text != "" {
		turn.pendingThink = append(turn.pendingThink, ev.Text)
		turn.pendingThinkBytes += len(ev.Text)
	}
	turn.thinkStaged = true
	turn.pendingThink = clipPendingPieces(turn.pendingThink, &turn.pendingThinkBytes, &turn.pendingThinkClipped, remoteStreamPartThinkingCap)
}

// clipPendingPieces drops the oldest pieces until the buffer fits the cap.
// Each piece is valid UTF-8, so the cut stays on a rune boundary. The caller
// keeps a clipped flag and prefixes the joined text with an ellipsis.
func clipPendingPieces(pieces []string, nbytes *int, clipped *bool, limit int) []string {
	if limit <= 0 {
		*nbytes = 0
		*clipped = len(pieces) > 0
		return nil
	}
	if *nbytes <= limit {
		return pieces
	}
	*clipped = true
	for len(pieces) > 1 && *nbytes > limit {
		*nbytes -= len(pieces[0])
		pieces = pieces[1:]
	}
	if len(pieces) == 1 && len(pieces[0]) > limit {
		pieces[0] = trimRemoteStreamTail(pieces[0], limit)
		*nbytes = len(pieces[0])
	}
	return append([]string(nil), pieces...)
}

func (r *remoteWatchRegistry) armFlushLocked(turn *remoteLiveTurn, window time.Duration) {
	if turn.flushTimer != nil {
		return
	}
	if !r.sessionWatched(turn.snapshot.SessionKey) {
		return
	}
	session := turn.snapshot.SessionKey
	turn.flushTimer = time.AfterFunc(window, func() {
		r.coalesceDue(session)
	})
}

// coalesceDue hands an elapsed coalesce window to the writer. Delivering
// from this timer races a higher cursor that the writer is already sending.
func (r *remoteWatchRegistry) coalesceDue(session string) {
	r.mu.Lock()
	turn := r.turns[session]
	if turn == nil || (len(turn.pending) == 0 && len(turn.pendingThink) == 0) {
		r.mu.Unlock()
		return
	}
	if r.wireReady() {
		turn.flushDue = true
		r.mu.Unlock()
		r.wakeWriter()
		return
	}
	frames := r.flushSessionLocked(session)
	r.mu.Unlock()
	for _, frame := range frames {
		r.deliver(frame)
	}
}

// consumeDueFlushesLocked emits coalesce tails whose window elapsed.
// Caller holds r.mu. The frames are older than anything still in the queue.
func (r *remoteWatchRegistry) consumeDueFlushesLocked() []map[string]any {
	var frames []map[string]any
	for session, turn := range r.turns {
		if turn == nil || !turn.flushDue {
			continue
		}
		turn.flushDue = false
		frames = append(frames, r.flushSessionLocked(session)...)
	}
	return frames
}

func (r *remoteWatchRegistry) flushSessionLocked(session string) []map[string]any {
	turn := r.turns[session]
	if turn == nil {
		return nil
	}
	if !r.sessionWatched(session) {
		// The coalesce window closed after the watcher left. Keep the tail
		// in the turn. Dropping it makes the next watch_task cursor skip
		// those bytes and the reducer treats every later frame as a gap.
		r.sealPendingLocked(turn)
		return nil
	}
	return r.flushOpenLocked(turn)
}

// flushOpenLocked closes both coalesce buffers in cursor order. The later
// lane must not leave before the earlier one: the reducer would drop it
// and then gap on every frame after that hole.
func (r *remoteWatchRegistry) flushOpenLocked(turn *remoteLiveTurn) []map[string]any {
	if turn == nil {
		return nil
	}
	thinkFirst := len(turn.pending) > 0 && len(turn.pendingThink) > 0 && !turn.thinkStaged && turn.pendingThinkFrom < turn.pendingFrom
	if thinkFirst {
		frames := r.flushThinkingLocked(turn)
		return append(frames, r.flushContentLocked(turn)...)
	}
	frames := r.flushContentLocked(turn)
	return append(frames, r.flushThinkingLocked(turn)...)
}

// sealPendingLocked commits coalesced text into the turn and disarms the
// timer without putting another frame on the wire.
func (r *remoteWatchRegistry) sealPendingLocked(turn *remoteLiveTurn) {
	if turn == nil {
		return
	}
	if text := pendingContentText(turn); text != "" {
		if n := len(turn.snapshot.Parts); n > 0 && turn.snapshot.Parts[n-1].Kind == "text" {
			part := &turn.snapshot.Parts[n-1]
			part.Text = appendCappedTail(part.Text, text, remoteStreamPartTextCap)
		} else {
			turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "text", Seq: turn.pendingSeq, Text: text})
		}
	}
	turn.pending = nil
	turn.pendingFrom = 0
	turn.pendingTo = 0
	turn.pendingBytes = 0
	turn.pendingClipped = false
	r.materializeStagedThinkingLocked(turn)
	if turn.flushTimer != nil {
		turn.flushTimer.Stop()
		turn.flushTimer = nil
	}
}

func pendingContentText(turn *remoteLiveTurn) string {
	if turn == nil || len(turn.pending) == 0 {
		return ""
	}
	text := strings.Join(turn.pending, "")
	if turn.pendingClipped && !strings.HasPrefix(text, "…") {
		text = "…" + text
	}
	return trimRemoteStreamTail(text, remoteStreamPartTextCap)
}

func pendingThinkingText(turn *remoteLiveTurn) string {
	if turn == nil || len(turn.pendingThink) == 0 {
		return ""
	}
	text := strings.Join(turn.pendingThink, "")
	if turn.pendingThinkClipped && !strings.HasPrefix(text, "…") {
		text = "…" + text
	}
	return trimRemoteStreamTail(text, remoteStreamPartThinkingCap)
}

func applyThinkingDelta(parts []remoteStreamPart, seq uint64, text string) []remoteStreamPart {
	if text == "" {
		return parts
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Kind != "thinking" || parts[i].Done {
			continue
		}
		if parts[i].Text == "" {
			parts[i].Text = text
		} else {
			parts[i].Text = appendCappedTail(parts[i].Text, text, remoteStreamPartThinkingCap)
		}
		return parts
	}
	return append(parts, remoteStreamPart{Kind: "thinking", Seq: seq, Text: text})
}

func (r *remoteWatchRegistry) materializeStagedThinkingLocked(turn *remoteLiveTurn) {
	if turn == nil || !turn.thinkStaged {
		return
	}
	if text := pendingThinkingText(turn); text != "" {
		turn.snapshot.Parts = applyThinkingDelta(turn.snapshot.Parts, turn.pendingThinkSeq, text)
	}
	turn.pendingThink = nil
	turn.pendingThinkFrom = 0
	turn.pendingThinkTo = 0
	turn.pendingThinkBytes = 0
	turn.pendingThinkClipped = false
	turn.thinkStaged = false
}

func (r *remoteWatchRegistry) flushContentLocked(turn *remoteLiveTurn) []map[string]any {
	if turn == nil || len(turn.pending) == 0 {
		return nil
	}
	text := pendingContentText(turn)
	seq := turn.pendingSeq
	pieces := append([]string(nil), turn.pending...)
	cursorFrom := turn.pendingFrom
	cursor := turn.pendingTo
	turn.pending = nil
	turn.pendingFrom = 0
	turn.pendingTo = 0
	turn.pendingBytes = 0
	turn.pendingClipped = false
	if turn.flushTimer != nil && len(turn.pendingThink) == 0 {
		turn.flushTimer.Stop()
		turn.flushTimer = nil
	}
	if text == "" {
		return nil
	}
	if n := len(turn.snapshot.Parts); n > 0 && turn.snapshot.Parts[n-1].Kind == "text" {
		part := &turn.snapshot.Parts[n-1]
		part.Text = appendCappedTail(part.Text, text, remoteStreamPartTextCap)
	} else {
		turn.snapshot.Parts = append(turn.snapshot.Parts, remoteStreamPart{Kind: "text", Seq: seq, Text: text})
	}
	ev := viewerEvent{
		SessionKey: turn.snapshot.SessionKey, ProjectPath: turn.snapshot.ProjectPath,
		RequestID: turn.snapshot.RequestID, Event: "token", Lane: "content",
		Seq: seq, Text: text, Pieces: pieces, Cursor: cursor, CursorFrom: cursorFrom,
	}
	return r.fanoutLocked(ev)
}

func (r *remoteWatchRegistry) flushThinkingLocked(turn *remoteLiveTurn) []map[string]any {
	if turn == nil || len(turn.pendingThink) == 0 {
		return nil
	}
	if turn.thinkStaged {
		r.materializeStagedThinkingLocked(turn)
		return nil
	}
	text := strings.Join(turn.pendingThink, "")
	seq := turn.pendingThinkSeq
	cursorFrom := turn.pendingThinkFrom
	cursor := turn.pendingThinkTo
	turn.pendingThink = nil
	turn.pendingThinkFrom = 0
	turn.pendingThinkTo = 0
	turn.pendingThinkBytes = 0
	turn.pendingThinkClipped = false
	if turn.flushTimer != nil && len(turn.pending) == 0 {
		turn.flushTimer.Stop()
		turn.flushTimer = nil
	}
	ev := viewerEvent{
		SessionKey: turn.snapshot.SessionKey, ProjectPath: turn.snapshot.ProjectPath,
		RequestID: turn.snapshot.RequestID, Event: "thinking", Lane: "reasoning",
		State: "delta", Seq: seq, Text: text, Cursor: cursor, CursorFrom: cursorFrom,
	}
	return r.fanoutLocked(ev)
}

func (r *remoteWatchRegistry) sealThinkingLocked(turn *remoteLiveTurn, text string) {
	if turn == nil {
		return
	}
	for i := len(turn.snapshot.Parts) - 1; i >= 0; i-- {
		part := &turn.snapshot.Parts[i]
		if part.Kind == "thinking" && !part.Done {
			part.Done = true
			if text != "" {
				part.Text = text
			}
			break
		}
	}
	turn.thinkingOpen = false
}

func (r *remoteWatchRegistry) openThinkingPartLocked(turn *remoteLiveTurn) *remoteStreamPart {
	for i := len(turn.snapshot.Parts) - 1; i >= 0; i-- {
		part := &turn.snapshot.Parts[i]
		if part.Kind == "thinking" && !part.Done {
			return part
		}
	}
	return nil
}

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
	if turn == nil || (requestID != "" && turn.snapshot.RequestID != "" && turn.snapshot.RequestID != requestID && turn.snapshot.Responded) {
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

func (r *remoteWatchRegistry) fanoutLocked(ev viewerEvent) []map[string]any {
	grants := r.grantsFor(ev.SessionKey)
	if len(grants) == 0 {
		return nil
	}
	var out []map[string]any
	for _, grantID := range grants {
		proto := 1
		r.watchMu.Lock()
		if set := r.protos[grantID]; set != nil && set[ev.SessionKey] >= 2 {
			proto = 2
		}
		r.watchMu.Unlock()
		frame := ev.envelope(grantID, proto)
		if frame == nil {
			continue
		}
		out = append(out, frame)
	}
	return out
}

func (ev viewerEvent) envelope(grantID string, proto int) map[string]any {
	if proto < 2 && (ev.Event == "card" || ev.Event == "snapshot") {
		return nil
	}
	payload := map[string]any{
		"grant_id":    grantID,
		"session_key": ev.SessionKey,
		"request_id":  ev.RequestID,
		"event":       ev.Event,
	}
	if ev.ProjectPath != "" {
		payload["project_path"] = ev.ProjectPath
	}
	if ev.Seq > 0 {
		payload["seq"] = ev.Seq
	}
	if ev.Text != "" && ev.Event != "heartbeat" {
		payload["text"] = ev.Text
	}
	if ev.State != "" {
		payload["state"] = ev.State
	}
	if ev.DisplayText != "" {
		payload["display_text"] = ev.DisplayText
	}
	if ev.Replace {
		payload["replace"] = true
	}
	if len(ev.Response) > 0 && ev.Event == "response" {
		payload["response"] = json.RawMessage(ev.Response)
	}
	if len(ev.Workbench) > 0 {
		payload["workbench"] = json.RawMessage(ev.Workbench)
	}
	if proto >= 2 {
		payload["proto"] = 2
		if ev.Cursor > 0 {
			payload["cursor"] = ev.Cursor
		}
		from := ev.CursorFrom
		if from == 0 {
			from = ev.Cursor
		}
		if from > 0 {
			payload["cursor_from"] = from
		}
		if ev.Lane != "" {
			payload["lane"] = ev.Lane
		}
		if len(ev.Pieces) > 0 {
			payload["pieces"] = ev.Pieces
		}
		if ev.Coding != nil {
			payload["coding"] = ev.Coding
		}
		if ev.Terminal != nil {
			payload["terminal"] = ev.Terminal
		}
		if ev.Kind != "" {
			payload["kind"] = ev.Kind
		}
		if len(ev.Confirmation) > 0 {
			payload["confirmation"] = json.RawMessage(ev.Confirmation)
		}
		if len(ev.Actions) > 0 {
			payload["actions"] = json.RawMessage(ev.Actions)
		}
		if ev.AsError {
			payload["as_error"] = true
		}
		if ev.Truncated {
			payload["truncated"] = true
		}
		if ev.Resync {
			payload["resync"] = true
		}
		if ev.Live != nil {
			payload["live"] = ev.Live
		}
	}
	if ev.Event == "response" {
		payload = shrinkResponseEnvelope(payload)
		return payload
	}
	if ev.Event == "thinking" && ev.State == "done" {
		payload = shrinkThinkingDone(payload)
	}
	if raw, err := json.Marshal(payload); err == nil && len(raw) > remoteStreamEnvelopeSkip {
		if ev.Event == "progress" {
			delete(payload, "text")
			if raw, err = json.Marshal(payload); err == nil && len(raw) <= remoteStreamEnvelopeSkip {
				return payload
			}
		}
		log.Printf("[remote-control] stream event %s dropped: %d bytes over limit", ev.Event, len(raw))
		return nil
	}
	return payload
}

func shrinkResponseEnvelope(payload map[string]any) map[string]any {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) <= remoteStreamEnvelopeSkip {
		return payload
	}
	if term, ok := payload["terminal"].(*viewerTerminal); ok && term != nil {
		clone := *term
		clone.Reasoning = ""
		payload["terminal"] = &clone
		term = &clone
		raw, _ = json.Marshal(payload)
		text := []rune(term.Text)
		for len(raw) > remoteStreamEnvelopeSkip && len(text) > 0 {
			// Shorten from the tail: keep the prefix and drop the end.
			next := len(text) / 2
			if next >= len(text) {
				next = len(text) - 1
			}
			if next < 0 {
				next = 0
			}
			text = text[:next]
			term.Text = string(text)
			payload["terminal"] = term
			raw, _ = json.Marshal(payload)
		}
	}
	if resp, ok := payload["response"].(json.RawMessage); ok && len(raw) > remoteStreamEnvelopeSkip {
		var body map[string]any
		if json.Unmarshal(resp, &body) == nil {
			body["reasoning"] = ""
			if text, _ := body["text"].(string); text != "" {
				runes := []rune(text)
				for {
					encoded, _ := json.Marshal(body)
					payload["response"] = json.RawMessage(encoded)
					raw, _ = json.Marshal(payload)
					if len(raw) <= remoteStreamEnvelopeSkip || len(runes) == 0 {
						break
					}
					next := len(runes) / 2
					if next >= len(runes) {
						next = len(runes) - 1
					}
					if next < 0 {
						next = 0
					}
					runes = runes[:next]
					body["text"] = string(runes)
				}
			}
		}
	}
	return payload
}

func shrinkThinkingDone(payload map[string]any) map[string]any {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) <= remoteStreamEnvelopeSkip {
		return payload
	}
	text, _ := payload["text"].(string)
	payload["text"] = trimRunesTail(text, 16<<10)
	payload["truncated"] = true
	return payload
}

func trimRunesTail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func (r *remoteWatchRegistry) snapshotFrameLocked(sessionKey string) viewerEvent {
	// includeResponded: a gap can drop the response frame. watch_task still
	// answers nil once the turn has settled; only this resync carries it.
	// Seal first so the 80ms coalesce timer cannot emit a token whose
	// cursor_from sits behind this snapshot. That frame is not a duplicate:
	// from a viewer that already applied the snapshot it overlaps, and from
	// a fresh reducer it looks like a gap that drops the snapshot.
	if turn := r.turns[sessionKey]; turn != nil {
		r.sealPendingLocked(turn)
	}
	snap := r.liveSnapshotUnlocked(sessionKey, true)
	ev := viewerEvent{
		SessionKey: sessionKey, ProjectPath: projectPathFromSessionOwnerID(sessionKey),
		Event: "snapshot", Lane: "control", Resync: true, Proto: 2,
		Live: snap,
	}
	if snap != nil {
		ev.RequestID = snap.RequestID
		ev.Cursor = snap.Cursor
		ev.CursorFrom = snap.Cursor
	} else {
		ev.Cursor = viewerCursorNow(sessionKey)
		ev.CursorFrom = ev.Cursor
	}
	return ev
}

// finishSnapshot attaches the checklist and shrinks the bundle once.
// Caller does not hold r.mu.
func (r *remoteWatchRegistry) finishSnapshot(ev viewerEvent) viewerEvent {
	var raw json.RawMessage
	if r != nil && r.app != nil {
		if m := r.app.remoteWorkbenchSnapshot(ev.SessionKey); len(m) > 0 {
			raw, _ = json.Marshal(m)
		}
	}
	ev.Live, ev.Workbench = shrinkSnapshotBundle(ev.Live, raw)
	if ev.Live != nil {
		ev.RequestID = ev.Live.RequestID
		ev.Cursor = ev.Live.Cursor
		ev.CursorFrom = ev.Live.Cursor
	}
	return ev
}

func responseFinalText(ev viewerEvent) string {
	if ev.Terminal == nil {
		return ""
	}
	if ev.Terminal.Text != "" {
		return ev.Terminal.Text
	}
	if ev.Terminal.TrialReflectSummary != "" {
		return ev.Terminal.TrialReflectSummary
	}
	return ev.Terminal.TraceSummary
}

// settleResponseTextLocked copies the terminal answer into the live parts
// when the streamed text run is not already that answer. The slice is new
// so a later append cannot alias the pre-response parts.
func (r *remoteWatchRegistry) settleResponseTextLocked(turn *remoteLiveTurn, ev viewerEvent) {
	final := responseFinalText(ev)
	if turn == nil || final == "" {
		return
	}
	var joined strings.Builder
	for _, part := range turn.snapshot.Parts {
		if part.Kind == "text" {
			joined.WriteString(part.Text)
		}
	}
	if joined.String() == final {
		return
	}
	next := make([]remoteStreamPart, 0, len(turn.snapshot.Parts)+1)
	for _, part := range turn.snapshot.Parts {
		if part.Kind == "text" {
			continue
		}
		next = append(next, part)
	}
	seq := ev.Seq
	if seq == 0 {
		seq = ev.Cursor
	}
	next = append(next, remoteStreamPart{Kind: "text", Seq: seq, Text: final})
	turn.snapshot.Parts = next
}

func (r *remoteWatchRegistry) liveSnapshotUnlocked(sessionKey string, includeResponded bool) *remoteLiveSnapshot {
	turn := r.turns[sessionKey]
	if turn == nil || (turn.snapshot.Responded && !includeResponded) {
		return nil
	}
	snap := turn.snapshot
	snap.Parts = append([]remoteStreamPart(nil), snap.Parts...)
	if text := pendingContentText(turn); text != "" {
		if n := len(snap.Parts); n > 0 && snap.Parts[n-1].Kind == "text" {
			snap.Parts[n-1].Text = appendCappedTail(snap.Parts[n-1].Text, text, remoteStreamPartTextCap)
		} else {
			snap.Parts = append(snap.Parts, remoteStreamPart{Kind: "text", Seq: turn.pendingSeq, Text: text})
		}
	}
	if turn.thinkStaged {
		snap.Parts = applyThinkingDelta(snap.Parts, turn.pendingThinkSeq, pendingThinkingText(turn))
	} else if text := strings.Join(turn.pendingThink, ""); text != "" {
		// A watched proto-2 window already copied each delta onto the open
		// part. pendingThink is only the coalesce buffer, so appending it
		// again would double the snapshot text.
		open := false
		for _, part := range snap.Parts {
			if part.Kind == "thinking" && !part.Done {
				open = true
				break
			}
		}
		if !open {
			snap.Parts = append(snap.Parts, remoteStreamPart{Kind: "thinking", Seq: turn.pendingThinkSeq, Text: text})
		}
	}
	snap.Cursor = turn.cursor
	return &snap
}

const remoteSnapshotMaxBytes = 96 << 10

func shrinkLiveSnapshot(snap *remoteLiveSnapshot) *remoteLiveSnapshot {
	snap, _ = shrinkSnapshotBundle(snap, nil)
	return snap
}

func shrinkWorkbenchMap(m map[string]interface{}) map[string]interface{} {
	if len(m) == 0 {
		return m
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m
	}
	_, shrunk := shrinkSnapshotBundle(nil, raw)
	if len(shrunk) == 0 {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if json.Unmarshal(shrunk, &out) != nil {
		return m
	}
	return out
}

func shrinkSnapshotBundle(snap *remoteLiveSnapshot, wb json.RawMessage) (*remoteLiveSnapshot, json.RawMessage) {
	if snap != nil {
		clone := *snap
		clone.Parts = append([]remoteStreamPart(nil), snap.Parts...)
		for i := range clone.Parts {
			if clone.Parts[i].Coding != nil {
				c := *clone.Parts[i].Coding
				clone.Parts[i].Coding = &c
			}
		}
		snap = &clone
		snap.Parts = clipSnapshotParts(snap.Parts)
	}
	planRunes := 1024
	wb = clipWorkbench(wb, planRunes)
	phase := 0
	prev := -1
	stalls := 0
	for {
		n := snapshotBundleBytes(snap, wb)
		if n < remoteSnapshotMaxBytes {
			return snap, wb
		}
		if n == prev {
			stalls++
			if stalls > 6 {
				return snap, wb
			}
		} else {
			stalls = 0
			prev = n
		}
		switch phase {
		case 0:
			if snap != nil && dropOldestCoding(snap) {
				continue
			}
			phase = 1
		case 1:
			if snap != nil && halveKindTail(snap, "text") {
				continue
			}
			if snap != nil && halveKindTail(snap, "thinking") {
				continue
			}
			phase = 2
		case 2:
			if planRunes > 1 {
				planRunes /= 2
				wb = clipWorkbench(wb, planRunes)
				continue
			}
			phase = 3
		case 3:
			if snap != nil {
				snap.Parts = keepTitleRow(snap.Parts)
			}
			phase = 4
		default:
			if len(wb) > 0 {
				wb = nil
				continue
			}
			return snap, wb
		}
	}
}

func snapshotBundleBytes(snap *remoteLiveSnapshot, wb json.RawMessage) int {
	ev := viewerEvent{Event: "snapshot", Resync: true, Live: snap, Workbench: wb}
	if snap != nil {
		ev.Cursor = snap.Cursor
		ev.CursorFrom = snap.Cursor
		ev.SessionKey = snap.SessionKey
		ev.ProjectPath = snap.ProjectPath
		ev.RequestID = snap.RequestID
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return 0
	}
	return len(raw)
}

func clipSnapshotParts(parts []remoteStreamPart) []remoteStreamPart {
	parts = keepLastWhere(parts, func(p remoteStreamPart) bool { return p.Kind == "progress" && p.Coding != nil }, 12)
	var plain []int
	for i, p := range parts {
		if p.Kind == "progress" && p.Coding == nil {
			plain = append(plain, i)
		}
	}
	drop := map[int]bool{}
	if len(plain) > 4 {
		for _, idx := range plain[:len(plain)-4] {
			drop[idx] = true
		}
	}
	var out []remoteStreamPart
	for i, p := range parts {
		if drop[i] {
			continue
		}
		if p.Kind == "progress" && p.Coding == nil {
			p.Text = trimRunes(p.Text, 256)
		}
		out = append(out, p)
	}
	out = keepNewestRunes(out, "text", 16<<10)
	out = keepNewestRunes(out, "thinking", 8<<10)
	return out
}

func keepLastWhere(parts []remoteStreamPart, match func(remoteStreamPart) bool, keep int) []remoteStreamPart {
	var idx []int
	for i, p := range parts {
		if match(p) {
			idx = append(idx, i)
		}
	}
	if len(idx) <= keep {
		return parts
	}
	drop := map[int]bool{}
	for _, i := range idx[:len(idx)-keep] {
		drop[i] = true
	}
	out := make([]remoteStreamPart, 0, len(parts)-len(drop))
	for i, p := range parts {
		if !drop[i] {
			out = append(out, p)
		}
	}
	return out
}

func keepNewestRunes(parts []remoteStreamPart, kind string, budget int) []remoteStreamPart {
	remain := budget
	keep := make([]bool, len(parts))
	trimAt := -1
	trimTo := 0
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Kind != kind {
			keep[i] = true
			continue
		}
		n := len([]rune(parts[i].Text))
		if remain <= 0 {
			continue
		}
		if n <= remain {
			keep[i] = true
			remain -= n
			continue
		}
		keep[i] = true
		trimAt = i
		trimTo = remain
		remain = 0
	}
	out := make([]remoteStreamPart, 0, len(parts))
	for i, p := range parts {
		if !keep[i] {
			continue
		}
		if i == trimAt {
			r := []rune(p.Text)
			if trimTo < len(r) {
				p.Text = string(r[len(r)-trimTo:])
			}
		}
		out = append(out, p)
	}
	return out
}

func dropOldestCoding(snap *remoteLiveSnapshot) bool {
	count := 0
	first := -1
	for i, p := range snap.Parts {
		if p.Kind == "progress" && p.Coding != nil {
			if first < 0 {
				first = i
			}
			count++
		}
	}
	if count <= 1 || first < 0 {
		return false
	}
	snap.Parts = append(snap.Parts[:first], snap.Parts[first+1:]...)
	return true
}

func halveKindTail(snap *remoteLiveSnapshot, kind string) bool {
	for i := len(snap.Parts) - 1; i >= 0; i-- {
		if snap.Parts[i].Kind != kind {
			continue
		}
		r := []rune(snap.Parts[i].Text)
		if len(r) < 1024 {
			return false
		}
		snap.Parts[i].Text = string(r[len(r)/2:])
		return true
	}
	return false
}

func keepTitleRow(parts []remoteStreamPart) []remoteStreamPart {
	var title remoteStreamPart
	found := false
	for _, p := range parts {
		if p.Coding == nil {
			continue
		}
		c := *p.Coding
		c.Detail, c.Summary, c.Command, c.Target = "", "", "", ""
		c.Files = nil
		c.FileChanges = nil
		title = remoteStreamPart{Kind: "progress", Seq: p.Seq, Text: trimRunes(c.Title, 80), Coding: &c}
		found = true
	}
	if !found {
		return nil
	}
	return []remoteStreamPart{title}
}

func clipWorkbench(raw json.RawMessage, planRunes int) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if planRunes <= 0 {
		delete(m, "execution_plan")
		delete(m, "requirement_restatement")
	} else {
		m["execution_plan"] = clipPlanField(m["execution_plan"], planRunes)
		m["requirement_restatement"] = clipPlanField(m["requirement_restatement"], planRunes)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

func clipPlanField(v any, n int) any {
	switch t := v.(type) {
	case string:
		return trimRunes(t, n)
	case []any:
		var b strings.Builder
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
		return trimRunes(b.String(), n)
	default:
		return v
	}
}
