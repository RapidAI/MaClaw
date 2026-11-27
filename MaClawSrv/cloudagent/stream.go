package cloudagent

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type livePart struct {
	Kind string `json:"kind"`
	Seq  uint64 `json:"seq"`
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

type liveSnapshot struct {
	RequestID      string         `json:"request_id"`
	SessionKey     string         `json:"session_key"`
	ProjectPath    string         `json:"project_path"`
	DisplayText    string         `json:"display_text,omitempty"`
	Parts          []livePart     `json:"parts"`
	StreamDone     bool           `json:"stream_done"`
	Responded      bool           `json:"responded"`
	StartedAt      int64          `json:"started_at,omitempty"`
	Cursor         uint64         `json:"cursor,omitempty"`
	Terminal       map[string]any `json:"terminal,omitempty"`
	LocalFilePaths []string       `json:"local_file_paths,omitempty"`
	Workbench      map[string]any `json:"workbench,omitempty"`
	Workflow       map[string]any `json:"workflow,omitempty"`
}

// streamCoalesce batches token deltas before a viewer frame is posted.
// Tests set it to zero so a token is posted in the same call.
var streamCoalesce = 80 * time.Millisecond

var streamHTTP = &http.Client{Timeout: 5 * time.Second}

type streamLane struct {
	cursor       int
	lastProgress string
	token        bytes.Buffer
	tokenReq     string
	think        bytes.Buffer
	thinkReq     string
	timer        *time.Timer
	requestID    string
	display      string
	body         strings.Builder
	thinking     strings.Builder
	thinkOpen    bool
	responded    bool
	done         bool
	streamDone   bool
	started      int64
	progress     []livePart
	terminal     map[string]any
	files        []string
}

func (rt *Runtime) noteGrant(instanceID, grantID string) {
	instanceID = strings.TrimSpace(instanceID)
	grantID = strings.TrimSpace(grantID)
	if rt == nil || instanceID == "" || grantID == "" {
		return
	}
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	if rt.grants == nil {
		rt.grants = map[string]map[string]struct{}{}
	}
	set := rt.grants[instanceID]
	if set == nil {
		set = map[string]struct{}{}
		rt.grants[instanceID] = set
	}
	set[grantID] = struct{}{}
}

func (rt *Runtime) dropGrant(instanceID, grantID string) {
	instanceID = strings.TrimSpace(instanceID)
	grantID = strings.TrimSpace(grantID)
	if rt == nil || instanceID == "" || grantID == "" {
		return
	}
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	delete(rt.grants[instanceID], grantID)
	if len(rt.grants[instanceID]) == 0 {
		delete(rt.grants, instanceID)
	}
}

// dropInstance removes every watcher grant and stream lane of a deleted
// instance — otherwise each delete leaks the maps for the life of the process.
func (rt *Runtime) dropInstance(instanceID string) {
	if rt == nil {
		return
	}
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	delete(rt.grants, instanceID)
	prefix := instanceID + "\n"
	for key, lane := range rt.stream {
		if strings.HasPrefix(key, prefix) {
			if lane.timer != nil {
				lane.timer.Stop()
			}
			delete(rt.stream, key)
		}
	}
}

func (rt *Runtime) grantIDs(instanceID string) []string {
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	set := rt.grants[instanceID]
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// publishTerminal stores the assistant reply, then posts the response frame.
// The desktop reloads history as soon as that frame is queued, so the row
// has to be committed first. Status stays running until finishTurn: a second
// send cannot begin while the frame is still going out. finishTurn inserts
// the row only when this seal did not store it.
func (rt *Runtime) publishTerminal(inst *instance, taskID, requestID, content string, asError bool, files []string, extra map[string]any) {
	if inst != nil && inst.book != nil {
		// The round's reasoning is sealed with the reply so a page reload
		// restores the thinking block too. A retried seal replaces its row.
		if thinking := rt.laneThinkingText(inst.meta.ID, taskID); thinking != "" {
			if err := inst.book.SealThinking(requestID, taskID, thinking, time.Now()); err != nil {
				slog.Warn("cloudagent seal thinking",
					slog.String("instance", inst.meta.ID),
					slog.String("request", requestID),
					slog.Any("err", err))
			}
		}
		if _, err := inst.book.sealReply(requestID, content, asError, files, time.Now()); err != nil {
			slog.Warn("cloudagent seal reply",
				slog.String("instance", inst.meta.ID),
				slog.String("request", requestID),
				slog.Any("err", err))
		}
	}
	rt.publish(inst, taskID, requestID, "response", content, extra)
}

// laneThinkingText snapshots the round's accumulated thinking for the
// transcript. Empty when the task has no live lane.
func (rt *Runtime) laneThinkingText(instanceID, taskID string) string {
	if rt == nil {
		return ""
	}
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	lane := rt.stream[instanceID+"\n"+taskID]
	if lane == nil {
		return ""
	}
	return strings.TrimSpace(lane.thinking.String())
}

// dropStreamLane removes one task's live lane and stops its coalesce timer.
// A deleted task must not be restorable through cloud_watch_task.
func (rt *Runtime) dropStreamLane(instanceID, taskID string) {
	if rt == nil {
		return
	}
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	key := instanceID + "\n" + taskID
	if lane := rt.stream[key]; lane != nil && lane.timer != nil {
		lane.timer.Stop()
	}
	delete(rt.stream, key)
}

// publish posts one viewer event to every grant watching this instance.
// A repeated progress line is dropped before a cursor is allocated.
// project_path stays empty: the cloud page does not key on it.
func (rt *Runtime) publish(inst *instance, taskID, requestID, event, text string, extra map[string]any) {
	if rt == nil || inst == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	event = strings.TrimSpace(event)
	if taskID == "" || event == "" {
		return
	}
	grants := rt.grantIDs(inst.meta.ID)
	extra = withRoundClientMessage(inst, requestID, event, extra)
	rt.streamMu.Lock()
	key := inst.meta.ID + "\n" + taskID
	lane := rt.lane(key)
	if event == "progress" && text == lane.lastProgress {
		rt.streamMu.Unlock()
		return
	}
	if event == "progress" {
		lane.lastProgress = text
	}
	// A sealing event closes an open thinking block first — the desktop relay
	// emits thinking/done ahead of new_round, stream_done, and the terminal
	// response. Capture pre-state: noteLive clears it.
	sealThink := lane.thinkOpen && sealThinkingEvent(event)
	thinkClose := ""
	if sealThink {
		thinkClose = lane.thinking.String()
	}
	// A response also ends the stream: stream_done goes out before the
	// terminal frame, the same order the local loop produces.
	emitDone := event == "response" && !lane.streamDone
	if event == "workbench_steps" {
		// A workbench decision retires the plan confirmation card even when the
		// next execution turn is still queued. Otherwise a watch snapshot would
		// keep resurrecting actions whose backing gate is already closed.
		if terminal := lane.terminal; terminal != nil {
			if confirmation, _ := terminal["confirmation"].(map[string]any); confirmation != nil &&
				strings.HasPrefix(asString(confirmation["id"]), "plan-approval;") {
				lane.terminal = nil
			}
		}
	}
	rt.noteLive(lane, requestID, event, text, extra)
	// Process rows ride along as they stream — progress is persisted
	// regardless of watchers, so a page reload restores the how, not just
	// the what. Failures log and never break the round.
	persistProgress := event == "progress" && requestID != "" && strings.TrimSpace(text) != ""
	if event == "token" || event == "thinking" {
		if text == "" {
			rt.streamMu.Unlock()
			return
		}
		if event == "token" {
			lane.token.WriteString(text)
			lane.tokenReq = requestID
		} else {
			lane.think.WriteString(text)
			lane.thinkReq = requestID
		}
		if streamCoalesce <= 0 {
			pending := rt.takePending(lane, inst.meta.ID, taskID)
			rt.streamMu.Unlock()
			for _, frame := range pending {
				rt.postStream(inst, grants, frame)
			}
			return
		}
		if lane.timer == nil {
			lane.timer = time.AfterFunc(streamCoalesce, func() {
				rt.flushPending(inst, taskID)
			})
		}
		rt.streamMu.Unlock()
		return
	}
	if lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	pending := rt.takePending(lane, inst.meta.ID, taskID)
	if sealThink {
		pending = append(pending, rt.nextFrame(lane, inst.meta.ID, taskID, requestID, "thinking", thinkClose, map[string]any{"state": "done"}))
	}
	if emitDone {
		lane.streamDone = true
		pending = append(pending, rt.nextFrame(lane, inst.meta.ID, taskID, requestID, "stream_done", "", nil))
	}
	body := rt.nextFrame(lane, inst.meta.ID, taskID, requestID, event, text, extra)
	files := lane.files
	rt.streamMu.Unlock()
	if persistProgress && inst.book != nil {
		if err := inst.book.AppendTurnEvent(requestID, taskID, "progress", text, time.Now()); err != nil {
			slog.Warn("cloudagent turn event",
				slog.String("instance", inst.meta.ID),
				slog.String("task", taskID),
				slog.String("request", requestID),
				slog.Any("err", err))
		}
	}
	attrs := []any{
		slog.String("instance", inst.meta.ID), slog.String("task", taskID), slog.String("request", requestID),
		slog.String("event", event), slog.String("lane", eventLane(event)),
		slog.Int("frames", len(pending)+1), slog.Int("watchers", len(grants)),
		slog.Bool("sealed_thinking", sealThink), slog.Bool("stream_done", emitDone), slog.Int("text_len", len(text)),
	}
	if event == "response" && len(files) > 0 {
		attrs = append(attrs, slog.Any("artifacts", files))
	}
	slog.Debug("cloudagent publish", attrs...)
	for _, frame := range pending {
		rt.postStream(inst, grants, frame)
	}
	rt.postStream(inst, grants, body)
}

func (rt *Runtime) flushPending(inst *instance, taskID string) {
	if rt == nil || inst == nil {
		return
	}
	grants := rt.grantIDs(inst.meta.ID)
	rt.streamMu.Lock()
	lane := rt.stream[inst.meta.ID+"\n"+taskID]
	if lane != nil && lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	var pending [][]byte
	if lane != nil {
		pending = rt.takePending(lane, inst.meta.ID, taskID)
	}
	rt.streamMu.Unlock()
	if len(grants) > 0 {
		for _, frame := range pending {
			rt.postStream(inst, grants, frame)
		}
	}
}

func (rt *Runtime) lane(key string) *streamLane {
	if rt.stream == nil {
		rt.stream = map[string]*streamLane{}
	}
	lane := rt.stream[key]
	if lane == nil {
		lane = &streamLane{}
		rt.stream[key] = lane
	}
	return lane
}

// takePending drains the coalesced token and thinking buffers. Thinking is
// flushed first: a visible token seals the open block on the viewer, which
// matches the loop's real order of reasoning before answer text.
func (rt *Runtime) takePending(lane *streamLane, instanceID, taskID string) [][]byte {
	if lane == nil {
		return nil
	}
	var frames [][]byte
	if lane.think.Len() > 0 {
		frames = append(frames, rt.nextFrame(lane, instanceID, taskID, lane.thinkReq, "thinking", lane.think.String(), map[string]any{"state": "delta"}))
		lane.think.Reset()
	}
	if lane.token.Len() > 0 {
		frames = append(frames, rt.nextFrame(lane, instanceID, taskID, lane.tokenReq, "token", lane.token.String(), nil))
		lane.token.Reset()
	}
	return frames
}

// sealThinkingEvent names the events after which an open thinking block is
// over — the same set the desktop relay closes thinking on.
func sealThinkingEvent(event string) bool {
	switch event {
	case "new_round", "round_started", "stream_done", "response":
		return true
	}
	return false
}

// eventLane mirrors the desktop viewer lanes so a shared consumer sorts the
// frame the same way for local and cloud turns.
func eventLane(event string) string {
	switch event {
	case "token":
		return "content"
	case "thinking":
		return "reasoning"
	case "progress":
		return "progress"
	}
	return "control"
}

func (rt *Runtime) nextFrame(lane *streamLane, instanceID, taskID, requestID, event, text string, extra map[string]any) []byte {
	lane.cursor++
	cursor := lane.cursor
	frame := map[string]any{
		"session_key":  "cloud-agent:" + instanceID + ":" + taskID,
		"request_id":   requestID,
		"event":        event,
		"seq":          cursor,
		"proto":        2,
		"cursor":       cursor,
		"cursor_from":  cursor,
		"lane":         eventLane(event),
		"project_path": "",
		"text":         text,
	}
	if event == "round_started" {
		frame["display_text"] = cloudDisplay(text)
	}
	for key, value := range extra {
		if key == "display_text" {
			frame[key] = cloudDisplay(asString(value))
			continue
		}
		frame[key] = value
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		return nil
	}
	return raw
}

// withRoundClientMessage stamps the desktop's client message id onto
// round_started. The ack for a parked send can still be in flight when this
// frame arrives; the id is the join, not the display text.
func withRoundClientMessage(inst *instance, requestID, event string, extra map[string]any) map[string]any {
	if event != "round_started" || inst == nil || inst.book == nil {
		return extra
	}
	clientID := inst.book.clientMessageID(requestID)
	if clientID == "" {
		return extra
	}
	if extra == nil {
		extra = map[string]any{}
	}
	if _, ok := extra["client_message_id"]; !ok {
		extra["client_message_id"] = clientID
	}
	return extra
}

func cloudDisplay(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "〔") {
		return text
	}
	if text == "" {
		return "〔云端〕"
	}
	return "〔云端〕" + text
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func (rt *Runtime) noteLive(lane *streamLane, requestID, event, text string, extra map[string]any) {
	if lane == nil || ((event == "token" || event == "thinking") && text == "") {
		return
	}
	if requestID != "" {
		lane.requestID = requestID
	}
	switch event {
	case "round_started":
		lane.body.Reset()
		lane.thinking.Reset()
		lane.thinkOpen = false
		lane.responded = false
		lane.done = false
		lane.streamDone = false
		lane.progress = nil
		lane.terminal = nil
		lane.files = nil
		lane.started = time.Now().UnixMilli()
		display := text
		if extra != nil {
			if value, ok := extra["display_text"].(string); ok && strings.TrimSpace(value) != "" {
				display = value
			}
		}
		lane.display = cloudDisplay(display)
	case "token":
		lane.thinkOpen = false
		lane.body.WriteString(text)
	case "thinking":
		lane.thinking.WriteString(text)
		lane.thinkOpen = true
	case "stream_done":
		lane.thinkOpen = false
		lane.done = true
		lane.streamDone = true
	case "response":
		lane.thinkOpen = false
		lane.responded = true
		lane.done = true
		lane.terminal = confirmationTerminal(extra)
		lane.files = artifactPathsOf(extra)
		if strings.TrimSpace(text) != "" {
			lane.body.Reset()
			lane.body.WriteString(text)
		}
	case "progress":
		lane.progress = append(lane.progress, livePart{Kind: "progress", Seq: uint64(len(lane.progress) + 1), Text: text})
		if len(lane.progress) > 40 {
			lane.progress = lane.progress[len(lane.progress)-40:]
		}
	}
}

// artifactPathsOf reads the produced-file list off a response terminal so
// the watch snapshot can offer the same artifacts the stream carried.
func artifactPathsOf(extra map[string]any) []string {
	if extra == nil {
		return nil
	}
	raw, _ := extra["terminal"].(map[string]any)
	if raw == nil {
		return nil
	}
	list, _ := raw["local_file_paths"].([]any)
	if len(list) == 0 {
		if typed, ok := raw["local_file_paths"].([]string); ok && len(typed) > 0 {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if path := strings.TrimSpace(asString(item)); path != "" {
			out = append(out, path)
		}
	}
	return out
}

// confirmationTerminal keeps a push ask on the lane so a later watch can
// restore the card. A plain text terminal is not an ask.
func confirmationTerminal(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	raw, _ := extra["terminal"].(map[string]any)
	if raw == nil {
		return nil
	}
	if confirmation, _ := raw["confirmation"].(map[string]any); confirmation != nil && strings.TrimSpace(asString(confirmation["id"])) != "" {
		return raw
	}
	if strings.TrimSpace(asString(raw["ask_user_question"])) != "" || strings.TrimSpace(asString(raw["ask_user_options_json"])) != "" {
		return raw
	}
	return nil
}

// watchSnapshot is the cloud_watch_task body. An empty project path stays empty.
func (rt *Runtime) watchSnapshot(inst *instance, taskID string) (json.RawMessage, error) {
	taskID = strings.TrimSpace(taskID)
	snap := liveSnapshot{ProjectPath: ""}
	if inst != nil {
		snap.SessionKey = "cloud-agent:" + inst.meta.ID + ":" + taskID
	}
	if rt != nil && inst != nil && taskID != "" {
		rt.streamMu.Lock()
		lane := rt.stream[inst.meta.ID+"\n"+taskID]
		if lane != nil {
			snap.RequestID = lane.requestID
			snap.DisplayText = lane.display
			snap.Responded = lane.responded
			snap.StreamDone = lane.done
			snap.StartedAt = lane.started
			snap.Cursor = uint64(lane.cursor)
			if lane.thinking.Len() > 0 {
				snap.Parts = append(snap.Parts, livePart{Kind: "thinking", Seq: 1, Text: lane.thinking.String(), Done: !lane.thinkOpen})
			}
			if lane.body.Len() > 0 {
				snap.Parts = append(snap.Parts, livePart{Kind: "text", Seq: uint64(len(snap.Parts) + 1), Text: lane.body.String()})
			}
			snap.Parts = append(snap.Parts, lane.progress...)
			snap.Terminal = lane.terminal
			snap.LocalFilePaths = lane.files
		}
		rt.streamMu.Unlock()
	}
	if snap.Parts == nil {
		snap.Parts = []livePart{}
	}
	// The workbench is task state, not lane state: it is rebuilt from the
	// book so a watcher also restores it after a srv restart. The workflow
	// snapshot is the same — rebuilt from the machine's own store.
	snap.Workbench = rt.workbenchSnapshot(inst, taskID)
	snap.Workflow = rt.workflowSnapshot(inst, taskID)
	// The lane is process memory, but the plan gate is durable task state.
	// After a srv restart a still-pending gate must come back with its
	// actions; otherwise the user is left with a checklist that cannot be
	// answered except by guessing the exact command text.
	if snap.Terminal == nil && inst != nil && inst.book != nil {
		if rec := inst.book.planLoadOrNil(taskID); rec != nil && rec.State == planStatePending {
			snap.Terminal = planTerminal(taskID)
		}
	}
	instID := ""
	if inst != nil {
		instID = inst.meta.ID
	}
	slog.Debug("cloudagent watch snapshot",
		slog.String("instance", instID), slog.String("task", taskID),
		slog.String("request", snap.RequestID), slog.Bool("responded", snap.Responded),
		slog.Bool("stream_done", snap.StreamDone), slog.Int("parts", len(snap.Parts)),
		slog.Int("artifacts", len(snap.LocalFilePaths)), slog.Int("cursor", int(snap.Cursor)))
	return json.Marshal(snap)
}

func (rt *Runtime) postStream(inst *instance, grants []string, frame []byte) {
	if inst == nil || len(frame) == 0 || !hubCallable(inst.meta) || rt.adminSecret == "" {
		return
	}
	var body map[string]any
	if err := json.Unmarshal(frame, &body); err != nil {
		return
	}
	endpoint := strings.TrimRight(inst.meta.HubLoopback, "/") + "/api/internal/cloud-agents/stream"
	for _, grantID := range grants {
		body["grant_id"] = grantID
		payload, err := json.Marshal(body)
		if err != nil {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", rt.adminSecret)
		resp, err := streamHTTP.Do(req)
		if err != nil {
			rt.noteHubPostFailure(inst.meta.ID, "stream", err.Error())
			continue
		}
		if resp.StatusCode != http.StatusOK {
			rt.noteHubPostFailure(inst.meta.ID, "stream", resp.Status)
		}
		_ = resp.Body.Close()
	}
}

// noteHubPostFailure logs a hub-post failure at most once a minute. Stream
// frames and heartbeats ride over different paths, so a dead feed must not
// stay invisible — but token bursts must not flood the log either.
func (rt *Runtime) noteHubPostFailure(instanceID, path, detail string) {
	rt.streamMu.Lock()
	defer rt.streamMu.Unlock()
	if time.Since(rt.streamLogAt) < time.Minute {
		return
	}
	rt.streamLogAt = time.Now()
	log.Printf("cloud-agent %s post to hub failed for %s: %s", path, instanceID, detail)
}
