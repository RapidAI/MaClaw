package cloudagent

import (
	"bytes"
	"encoding/json"
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
	RequestID   string         `json:"request_id"`
	SessionKey  string         `json:"session_key"`
	ProjectPath string         `json:"project_path"`
	DisplayText string         `json:"display_text,omitempty"`
	Parts       []livePart     `json:"parts"`
	StreamDone  bool           `json:"stream_done"`
	Responded   bool           `json:"responded"`
	StartedAt   int64          `json:"started_at,omitempty"`
	Cursor      uint64         `json:"cursor,omitempty"`
	Terminal    map[string]any `json:"terminal,omitempty"`
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
	timer        *time.Timer
	requestID    string
	display      string
	body         strings.Builder
	responded    bool
	done         bool
	started      int64
	progress     []livePart
	terminal     map[string]any
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
	rt.noteLive(lane, requestID, event, text, extra)
	if event == "token" {
		if text == "" {
			rt.streamMu.Unlock()
			return
		}
		lane.token.WriteString(text)
		lane.tokenReq = requestID
		if streamCoalesce <= 0 {
			payload := rt.takeToken(lane, inst.meta.ID, taskID)
			rt.streamMu.Unlock()
			rt.postStream(inst, grants, payload)
			return
		}
		if lane.timer == nil {
			lane.timer = time.AfterFunc(streamCoalesce, func() {
				rt.flushToken(inst, taskID)
			})
		}
		rt.streamMu.Unlock()
		return
	}
	if lane.timer != nil {
		lane.timer.Stop()
		lane.timer = nil
	}
	pending := rt.takeToken(lane, inst.meta.ID, taskID)
	body := rt.nextFrame(lane, inst.meta.ID, taskID, requestID, event, text, extra)
	rt.streamMu.Unlock()
	if pending != nil {
		rt.postStream(inst, grants, pending)
	}
	rt.postStream(inst, grants, body)
}

func (rt *Runtime) flushToken(inst *instance, taskID string) {
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
	var payload []byte
	if lane != nil {
		payload = rt.takeToken(lane, inst.meta.ID, taskID)
	}
	rt.streamMu.Unlock()
	if payload != nil && len(grants) > 0 {
		rt.postStream(inst, grants, payload)
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

func (rt *Runtime) takeToken(lane *streamLane, instanceID, taskID string) []byte {
	if lane == nil || lane.token.Len() == 0 {
		return nil
	}
	text := lane.token.String()
	req := lane.tokenReq
	lane.token.Reset()
	return rt.nextFrame(lane, instanceID, taskID, req, "token", text, nil)
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
	if lane == nil || (event == "token" && text == "") {
		return
	}
	if requestID != "" {
		lane.requestID = requestID
	}
	switch event {
	case "round_started":
		lane.body.Reset()
		lane.responded = false
		lane.done = false
		lane.progress = nil
		lane.terminal = nil
		lane.started = time.Now().UnixMilli()
		display := text
		if extra != nil {
			if value, ok := extra["display_text"].(string); ok && strings.TrimSpace(value) != "" {
				display = value
			}
		}
		lane.display = cloudDisplay(display)
	case "token":
		lane.body.WriteString(text)
	case "response":
		lane.responded = true
		lane.done = true
		lane.terminal = confirmationTerminal(extra)
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
			if lane.body.Len() > 0 {
				snap.Parts = append(snap.Parts, livePart{Kind: "text", Seq: 1, Text: lane.body.String()})
			}
			snap.Parts = append(snap.Parts, lane.progress...)
			snap.Terminal = lane.terminal
		}
		rt.streamMu.Unlock()
	}
	if snap.Parts == nil {
		snap.Parts = []livePart{}
	}
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
			continue
		}
		_ = resp.Body.Close()
	}
}
