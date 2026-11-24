package cloudagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func putCoding(t *testing.T, rt *Runtime, loopback string) Spec {
	t.Helper()
	spec := Spec{
		ID:          newID("cag_"),
		TenantID:    "tenant-a",
		OwnerUserID: "user-a",
		Name:        "coding",
		Kind:        KindCoding,
		HubOrigin:   "https://hub.example",
		HubLoopback: loopback,
	}
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	// jailRoot needs a checked-out repo: any workspace dir with a .git dir.
	repo := filepath.Join(instanceDir(rt, spec), "workspace", "repo", ".git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return spec
}

func sendRaw(t *testing.T, rt *Runtime, id, body string) json.RawMessage {
	t.Helper()
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_send_message", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func queuedAck(t *testing.T, raw json.RawMessage) (queueID, taskID string) {
	t.Helper()
	var ack struct {
		Accepted bool   `json:"accepted"`
		Queued   bool   `json:"queued"`
		QueueID  string `json:"queue_id"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted || !ack.Queued || ack.QueueID == "" {
		t.Fatalf("queued ack: %s err=%v", raw, err)
	}
	return ack.QueueID, ack.TaskID
}

func waitHits(t *testing.T, g *llmGate, want int32) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if g.hits.Load() >= want {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("model calls = %d, want %d", g.hits.Load(), want)
}

func TestCodingSendWhileRunningIsQueued(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-hold", "first coding turn")
	waitCall(t, g)

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-next","task_id":"`+taskID+`","text":"second coding turn"}`)
	queueID, queuedTask := queuedAck(t, raw)
	if queuedTask != taskID {
		t.Fatalf("queued task=%s want %s", queuedTask, taskID)
	}
	// The same client id stays parked: no second row, no second model call.
	raw = sendRaw(t, rt, spec.ID, `{"client_message_id":"client-next","task_id":"`+taskID+`","text":"second coding turn"}`)
	again, _ := queuedAck(t, raw)
	if again != queueID {
		t.Fatalf("dedup queue id=%s want %s", again, queueID)
	}
	if g.hits.Load() != 1 {
		t.Fatalf("queued send still called the model: %d", g.hits.Load())
	}

	release()
	waitHits(t, g, 2)
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		text = transcript(t, rt, spec.ID, taskID)
		if strings.Count(text, "cloud-session-ok") >= 2 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(text, "first coding turn") || !strings.Contains(text, "second coding turn") || strings.Count(text, "cloud-session-ok") != 2 {
		t.Fatalf("transcript=%s", text)
	}
	if status, err := rt.TurnStatus(spec.ID, requestID); err != nil || status != "finished" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := inst.book.PeekOldest(); ok {
		t.Fatal("queue did not drain")
	}
}

// A send without task_id still belongs to the running task: it parks under
// it and drains into the same transcript instead of opening a new task.
func TestCodingBareSendParksUnderRunningTask(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	_, taskID := send(t, rt, spec.ID, "client-hold", "hold this coding turn")
	waitCall(t, g)

	// No task_id in the body — the running task must adopt the parked row.
	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-bare","text":"bare parked line"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != taskID {
		t.Fatalf("queued task=%s want %s", queuedTask, taskID)
	}

	release()
	waitHits(t, g, 2)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		text := transcript(t, rt, spec.ID, taskID)
		if strings.Contains(text, "bare parked line") && strings.Count(text, "cloud-session-ok") >= 2 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("bare parked line never ran on task %s", taskID)
}

// An approval answer arriving while a coding turn is still running is both
// recorded against the pending fingerprint and parked like any other send.
func TestCodingApprovalAnswerQueuedWhileBusy(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	_, taskID := send(t, rt, spec.ID, "client-hold", "hold this coding turn")
	waitCall(t, g)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const fp = "fp-push-gate"
	inst.book.prepareAsk(taskID, fp)

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-approve","task_id":"`+taskID+`","text":"仅这一次"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != taskID {
		t.Fatalf("queued task=%s want %s", queuedTask, taskID)
	}
	decision, ok := inst.book.lookup(taskID, fp)
	if !ok || string(decision) != "once" {
		t.Fatalf("decision=%q ok=%v", decision, ok)
	}
	if g.hits.Load() != 1 {
		t.Fatalf("queued approval still called the model: %d", g.hits.Load())
	}

	release()
	waitHits(t, g, 2)
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		text = transcript(t, rt, spec.ID, taskID)
		if strings.Contains(text, "仅这一次") && strings.Count(text, "cloud-session-ok") >= 2 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(text, "仅这一次") {
		t.Fatalf("approval answer missing from transcript: %s", text)
	}
}

// Recover interrupts the leftover running turn; the parked row still runs.
func TestCodingRecoverDrainsQueue(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-hold", "hold this coding turn")
	waitCall(t, g)
	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-next","task_id":"`+taskID+`","text":"queued coding turn"}`)
	queuedAck(t, raw)

	if err := rt.Recover(); err != nil {
		t.Fatal(err)
	}
	status, err := rt.TurnStatus(spec.ID, requestID)
	if err != nil || status != "interrupted" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	release()
	waitHits(t, g, 2)
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		text = transcript(t, rt, spec.ID, taskID)
		if strings.Contains(text, "queued coding turn") && strings.Contains(text, "cloud-session-ok") {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(text, "queued coding turn") || !strings.Contains(text, "cloud-session-ok") {
		t.Fatalf("transcript=%s", text)
	}
}

func TestCloudToolStatusLine(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"bash", `{"argv":["git","status"]}`, "工具 · 执行命令 (bash)\ngit status"},
		{"read_file", `{"path":"src/a.go"}`, "工具 · 读取文件 (read_file)\nsrc/a.go"},
		{"write_file", `{"path":"b.txt"}`, "工具 · 写入文件 (write_file)\nb.txt"},
		{"git_commit", `{"message":"ship it"}`, "工具 · 提交改动 (git_commit)\nship it"},
		{"git_push", `{}`, "工具 · 推送代码 (git_push)"},
		{"other_tool", `{}`, "工具 · 调用工具 (other_tool)\nother_tool"},
	}
	for _, tc := range cases {
		if got := cloudToolStatusLine(tc.name, tc.args); got != tc.want {
			t.Fatalf("%s: %q want %q", tc.name, got, tc.want)
		}
	}
	if got := cloudToolStatusLine(" ", "{}"); got != "" {
		t.Fatalf("empty name: %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := cloudToolStatusLine("write_file", `{"path":"`+long+`"}`); !strings.HasSuffix(got, "...") {
		t.Fatalf("long detail not truncated: %d", len(got))
	}
}

// The agentHost streams reasoning sentinels as thinking events and announces
// every tool call as a progress line the viewer folds into the timeline.
func TestAgentHostStreamShapes(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var frames []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err == nil {
			mu.Lock()
			frames = append(frames, frame)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	host := &agentHost{rt: rt, inst: inst, task: "task-1", request: "req-1", coding: true}
	host.OnToken("\x01thinking about it")
	host.OnToken("answer text")
	host.ExecuteTool("missing_tool", `{}`)

	mu.Lock()
	defer mu.Unlock()
	if len(frames) != 3 {
		t.Fatalf("frames=%d %v", len(frames), frames)
	}
	if frames[0]["event"] != "thinking" || frames[0]["state"] != "delta" || frames[0]["text"] != "thinking about it" {
		t.Fatalf("thinking frame=%v", frames[0])
	}
	if frames[1]["event"] != "token" || frames[1]["text"] != "answer text" {
		t.Fatalf("token frame=%v", frames[1])
	}
	if frames[2]["event"] != "progress" || !strings.Contains(fmtString(frames[2]["text"]), "工具 · 调用工具 (missing_tool)") {
		t.Fatalf("progress frame=%v", frames[2])
	}
}

// A watch snapshot rebuilds the thinking block so a mid-turn join keeps the
// same collapse the stream showed.
func TestWatchSnapshotCarriesThinking(t *testing.T) {
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.publish(inst, "task-1", "req-1", "round_started", "想一下", map[string]any{"display_text": "想一下"})
	rt.publish(inst, "task-1", "req-1", "thinking", "reasoning part", map[string]any{"state": "delta"})
	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_watch_task", []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap liveSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Parts) != 1 || snap.Parts[0].Kind != "thinking" || snap.Parts[0].Text != "reasoning part" || snap.Parts[0].Done {
		t.Fatalf("parts=%+v", snap.Parts)
	}
	// A visible token seals the block; the next watch reports it done.
	rt.publish(inst, "task-1", "req-1", "token", "answer", nil)
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_watch_task", []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	snap = liveSnapshot{}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Parts) != 2 || snap.Parts[0].Kind != "thinking" || !snap.Parts[0].Done || snap.Parts[1].Kind != "text" {
		t.Fatalf("sealed parts=%+v", snap.Parts)
	}
}
