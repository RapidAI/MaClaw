package cloudagent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestPublishTurnReasoningFillsEmptyLaneOnce(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindSession)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	host := &agentHost{rt: rt, inst: inst, task: "task-1", request: "req-1"}
	rt.publishTurnReasoning(host, agent.LoopResult{Reasoning: "先核对天气接口"})
	rt.publishTurnReasoning(host, agent.LoopResult{Reasoning: "不要重复"})
	host.OnToken("\x01后来的增量")
	rt.publish(inst, "task-1", "req-1", "response", "晴", nil)

	mu.Lock()
	defer mu.Unlock()
	var events []string
	for _, body := range bodies {
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatal(err)
		}
		events = append(events, fmtString(frame["event"])+":"+fmtString(frame["text"]))
	}
	want := []string{"thinking:先核对天气接口", "thinking:后来的增量", "thinking:先核对天气接口后来的增量", "stream_done:", "response:晴"}
	if len(events) != len(want) {
		t.Fatalf("events = %v", events)
	}
	for i, got := range events {
		if got != want[i] {
			t.Fatalf("events = %v", events)
		}
	}
}

func TestPublishTurnReasoningAppendsUnstreamedSuffix(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindSession)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	host := &agentHost{rt: rt, inst: inst, task: "task-1", request: "req-1"}
	host.OnToken("\x01先 ")
	rt.publishTurnReasoning(host, agent.LoopResult{Reasoning: "先核对"})
	rt.publishTurnReasoning(host, agent.LoopResult{Reasoning: "另一段"})
	rt.publish(inst, "task-1", "req-1", "response", "晴", nil)

	mu.Lock()
	defer mu.Unlock()
	var events []string
	for _, body := range bodies {
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatal(err)
		}
		events = append(events, fmtString(frame["event"])+":"+fmtString(frame["text"]))
	}
	want := []string{"thinking:先 ", "thinking:核对", "thinking:先 核对", "stream_done:", "response:晴"}
	if len(events) != len(want) {
		t.Fatalf("events = %v", events)
	}
	for i, got := range events {
		if got != want[i] {
			t.Fatalf("events = %v", events)
		}
	}
}

func TestPublishTurnReasoningUsesHistoryWhenLiveLaneEmpty(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindSession)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	host := &agentHost{rt: rt, inst: inst, task: "task-1", request: "req-1"}
	rt.publishTurnReasoning(host, agent.LoopResult{HistoryDelta: []agent.ConversationEntry{{
		Role:             "assistant",
		ReasoningContent: "历史里的思考",
	}}})
	rt.publish(inst, "task-1", "req-1", "response", "晴", nil)

	mu.Lock()
	defer mu.Unlock()
	var events []string
	for _, body := range bodies {
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatal(err)
		}
		events = append(events, fmtString(frame["event"])+":"+fmtString(frame["text"]))
	}
	want := []string{"thinking:历史里的思考", "thinking:历史里的思考", "stream_done:", "response:晴"}
	if len(events) != len(want) {
		t.Fatalf("events = %v", events)
	}
	for i, got := range events {
		if got != want[i] {
			t.Fatalf("events = %v", events)
		}
	}
}

func TestStreamPublishesViewerFrameWithoutProjectPath(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	var secrets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		secrets = append(secrets, r.Header.Get("X-MaClaw-Admin-Secret"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	rt.publish(inst, "task-1", "req-1", "progress", "running", nil)
	rt.publish(inst, "task-1", "req-1", "progress", "running", nil)
	rt.publish(inst, "task-1", "req-1", "token", "hi", nil)
	rt.publish(inst, "task-1", "req-1", "round_started", "改 README", map[string]any{"display_text": "改 README"})
	requestID, created, err := inst.book.Begin("cm_round", "task-client", "带上身份", time.Now())
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin %v %v %s", err, created, requestID)
	}
	rt.publish(inst, "task-client", requestID, "round_started", "带上身份", nil)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 4 {
		t.Fatalf("frames = %d %v", len(bodies), bodies)
	}
	for _, secret := range secrets {
		if secret != "admin-secret-value" {
			t.Fatalf("secret = %q", secret)
		}
	}
	var progress map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &progress); err != nil {
		t.Fatal(err)
	}
	if progress["grant_id"] != "grant-cloud" || progress["session_key"] != "cloud-agent:"+spec.ID+":task-1" || progress["project_path"] != "" || progress["cursor"] != float64(1) {
		t.Fatalf("progress = %s", bodies[0])
	}
	var token map[string]any
	if err := json.Unmarshal([]byte(bodies[1]), &token); err != nil {
		t.Fatal(err)
	}
	if token["event"] != "token" || token["text"] != "hi" || token["cursor"] != float64(2) || token["proto"] != float64(2) || token["project_path"] != "" {
		t.Fatalf("token = %s", bodies[1])
	}
	if strings.Contains(bodies[1], "ai-assistant-foreground-round-started") {
		t.Fatalf("foreground round leaked: %s", bodies[1])
	}
	var started map[string]any
	if err := json.Unmarshal([]byte(bodies[2]), &started); err != nil {
		t.Fatal(err)
	}
	if started["display_text"] != "〔云端〕改 README" || started["cursor"] != float64(3) {
		t.Fatalf("round = %s", bodies[2])
	}
	if _, ok := started["client_message_id"]; ok {
		t.Fatalf("unbound round carried a client id: %s", bodies[2])
	}
	var admitted map[string]any
	if err := json.Unmarshal([]byte(bodies[3]), &admitted); err != nil {
		t.Fatal(err)
	}
	if admitted["client_message_id"] != "cm_round" || admitted["request_id"] != requestID || admitted["event"] != "round_started" {
		t.Fatalf("admitted round = %s", bodies[3])
	}
}

// A terminal response emits the same tail the local loop does: the open
// thinking block closes with its full text, stream_done marks the stream
// end, and only then the response frame lands.
func TestStreamResponseSealsThinkingAndEndsStream(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	rt.publish(inst, "task-1", "req-1", "thinking", "reasoning", map[string]any{"state": "delta"})
	rt.publish(inst, "task-1", "req-1", "response", "answer", map[string]any{"terminal": map[string]any{"text": "answer"}})

	// A second turn settles the same way minus the thinking close.
	rt.publish(inst, "task-1", "req-2", "round_started", "继续", nil)
	rt.publish(inst, "task-1", "req-2", "response", "again", nil)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 7 {
		t.Fatalf("frames = %d %v", len(bodies), bodies)
	}
	var events, lanes []string
	var cursors []float64
	for _, body := range bodies {
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatal(err)
		}
		events = append(events, fmtString(frame["event"])+":"+fmtString(frame["state"]))
		lanes = append(lanes, fmtString(frame["lane"]))
		cursors = append(cursors, frame["cursor"].(float64))
	}
	want := []string{"thinking:delta", "thinking:done", "stream_done:", "response:", "round_started:", "stream_done:", "response:"}
	for i, got := range events {
		if got != want[i] {
			t.Fatalf("events = %v", events)
		}
	}
	wantLanes := []string{"reasoning", "reasoning", "control", "control", "control", "control", "control"}
	for i, got := range lanes {
		if got != wantLanes[i] {
			t.Fatalf("lanes = %v", lanes)
		}
	}
	for i := 1; i < len(cursors); i++ {
		if cursors[i] != cursors[i-1]+1 {
			t.Fatalf("cursors = %v", cursors)
		}
	}
	var done map[string]any
	if err := json.Unmarshal([]byte(bodies[1]), &done); err != nil {
		t.Fatal(err)
	}
	if done["text"] != "reasoning" {
		t.Fatalf("thinking done frame = %s", bodies[1])
	}
}

// Progress lines persist as they stream and the sealed thinking lands with
// the reply — with no watcher attached, so history does not depend on
// someone watching. dropStreamLane then hides the live lane from a later
// watch snapshot, which is what cloud_delete_task leans on.
func TestStreamPersistsProcessRowsAndDropLaneHidesRound(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, created, err := inst.book.Begin("cm-1", "task-1", "改 README", time.Now())
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin %v %v %s", err, created, requestID)
	}
	rt.publish(inst, "task-1", requestID, "round_started", "改 README", nil)
	rt.publish(inst, "task-1", requestID, "progress", "读取 README", nil)
	rt.publish(inst, "task-1", requestID, "progress", "读取 README", nil) // 重复进度不落库
	rt.publish(inst, "task-1", requestID, "thinking", "想一想", map[string]any{"state": "delta"})
	rt.publishTerminal(inst, "task-1", requestID, "改完了", false, nil, nil)
	// 第二次 terminal：seal 幂等，思考行替换不叠加。
	rt.publishTerminal(inst, "task-1", requestID, "改完了", false, nil, nil)

	events, err := inst.book.turnEventsByTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	list := events[requestID]
	if len(list) != 2 {
		t.Fatalf("persisted events %#v", list)
	}
	if list[0].Kind != "progress" || list[0].Text != "读取 README" || list[0].Seq != 1 {
		t.Fatalf("progress row %#v", list[0])
	}
	if list[1].Kind != "thinking" || list[1].Text != "想一想" {
		t.Fatalf("thinking row %#v", list[1])
	}

	if rt.laneThinkingText(spec.ID, "task-1") == "" {
		t.Fatal("lane thinking lost before the drop")
	}
	rt.dropStreamLane(spec.ID, "task-1")
	if got := rt.laneThinkingText(spec.ID, "task-1"); got != "" {
		t.Fatalf("lane survived the drop: %q", got)
	}
	if _, ok := rt.stream[spec.ID+"\n"+"task-1"]; ok {
		t.Fatal("lane map entry survived the drop")
	}
}
