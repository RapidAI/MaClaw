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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
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
	// jailRoot needs a bound checkout: the git.json binding is what makes
	// the workspace/repo directory authoritative — a bare .git is not.
	dir := instanceDir(rt, spec)
	repo := filepath.Join(dir, "workspace", "repo", ".git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGit(dir, gitSecret{Token: "bind-token-01234567", Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
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

// A gate answer parked before its plan record existed is interpreted when it
// drains, then executes the request that created the plan — never the control
// sentence itself.
func TestCodingQueuedPlanApprovalRunsOriginalRequest(t *testing.T) {
	g, srv, _ := newGate(t, officialSessionReply, false)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "task-queued-plan-gate"
	holdRequest, created, err := inst.book.Begin("client-hold", taskID, "hold this coding turn", time.Now())
	if err != nil || !created || holdRequest == "" {
		t.Fatalf("begin hold request=%s created=%v err=%v", holdRequest, created, err)
	}

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-plan-gate","task_id":"`+taskID+`","text":"按计划开始"}`)
	queuedAck(t, raw)
	const original = "用 C++ 写一个贪吃蛇"
	if err := inst.book.planSave(taskID, &planRecord{
		State:    planStatePending,
		Request:  original,
		Steps:    []planStep{{Index: 1, Title: "实现游戏", Status: "pending"}},
		Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.book.Finish(holdRequest, "held", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := rt.Recover(); err != nil {
		t.Fatal(err)
	}

	waitHits(t, g, 1)
	deadline := time.Now().Add(30 * time.Second)
	var rec *planRecord
	for time.Now().Before(deadline) {
		rec = inst.book.planLoadOrNil(taskID)
		if rec != nil && rec.State == planStateDone {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if rec == nil || rec.State != planStateDone {
		t.Fatalf("plan state=%+v", rec)
	}
	if len(rec.Steps) == 0 || rec.Steps[0].Status != "passed" {
		t.Fatalf("approved plan did not execute its first step: %+v", rec.Steps)
	}
	g.mu.Lock()
	body := ""
	if len(g.calls) > 0 {
		body = g.calls[0].Body
	}
	g.mu.Unlock()
	if !strings.Contains(body, original) {
		t.Fatalf("drained gate turn did not execute the parked request: %s", body)
	}
}

// Recovery drains rows left by a srv restart. A queued "按计划开始" is a
// control-plane decision for the already running plan, not a fresh ask that
// should settle that plan as failed.
func TestCodingRecoveredQueuedApprovalDoesNotFailRunningPlan(t *testing.T) {
	g, srv, _ := newGate(t, officialSessionReply, false)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "task-recovered-plan"
	const original = "实现一个可编译的贪吃蛇程序"
	if err := inst.book.EnsureTask(taskID, original); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(taskID, &planRecord{
		State:    planStateRunning,
		Request:  original,
		Steps:    []planStep{{Index: 1, Title: "实现游戏", Status: "running"}},
		Revision: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.book.Enqueue("client-recovered-gate", taskID, "按计划开始", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := rt.Recover(); err != nil {
		t.Fatal(err)
	}

	waitHits(t, g, 1)
	deadline := time.Now().Add(30 * time.Second)
	var rec *planRecord
	for time.Now().Before(deadline) {
		rec = inst.book.planLoadOrNil(taskID)
		if rec != nil && rec.State == planStateDone {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if rec == nil || rec.State != planStateDone {
		t.Fatalf("plan state=%+v", rec)
	}
	g.mu.Lock()
	body := ""
	if len(g.calls) > 0 {
		body = g.calls[0].Body
	}
	g.mu.Unlock()
	if !strings.Contains(body, original) {
		t.Fatalf("recovered turn did not resume the original request: %s", body)
	}
}

// A crash can preserve planStateRunning without any running step row. The
// continuation must re-arm the earliest unfinished row; otherwise settlement
// treats that row as merely untouched and skips work the user approved.
func TestCodingRecoveredPlanRearmsMissingStepCursor(t *testing.T) {
	g, srv, _ := newGate(t, officialSessionReply, false)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "task-recovered-missing-cursor"
	const original = "实现并验证贪吃蛇"
	if err := inst.book.EnsureTask(taskID, original); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(taskID, &planRecord{
		State:   planStateRunning,
		Request: original,
		Steps: []planStep{
			{Index: 1, Title: "搭建结构", Status: "passed"},
			{Index: 2, Title: "完成验证", Status: "pending"},
		},
		Revision: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.book.Enqueue("client-recovered-missing-cursor", taskID, "按计划开始", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := rt.Recover(); err != nil {
		t.Fatal(err)
	}

	waitHits(t, g, 1)
	deadline := time.Now().Add(30 * time.Second)
	var rec *planRecord
	for time.Now().Before(deadline) {
		rec = inst.book.planLoadOrNil(taskID)
		if rec != nil && rec.State == planStateDone {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if rec == nil || rec.State != planStateDone {
		t.Fatalf("plan state=%+v", rec)
	}
	if len(rec.Steps) < 2 || rec.Steps[1].Status != "passed" {
		t.Fatalf("missing cursor was not rearmed: %+v", rec.Steps)
	}
}

// An older recovery bug could close the plan as done while leaving a failed
// row behind. That inconsistent terminal record is retriable through the same
// explicit approval; a clean done plan still no-ops.
func TestCodingApprovalRetriesInconsistentDonePlan(t *testing.T) {
	g, srv, _ := newGate(t, officialSessionReply, false)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "task-inconsistent-done-plan"
	const original = "修好贪吃蛇并完成验证"
	if err := inst.book.EnsureTask(taskID, original); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(taskID, &planRecord{
		State:   planStateDone,
		Request: original,
		Steps: []planStep{
			{Index: 1, Title: "修复实现", Status: "failed"},
			{Index: 2, Title: "运行验证", Status: "skipped"},
		},
		Revision: 6,
	}); err != nil {
		t.Fatal(err)
	}
	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-retry-inconsistent","text":"按计划开始"}`)
	var ack struct {
		Accepted bool   `json:"accepted"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted {
		t.Fatalf("ack: %s err=%v", raw, err)
	}
	if ack.TaskID != taskID {
		t.Fatalf("bare gate answer task=%s want %s", ack.TaskID, taskID)
	}

	waitHits(t, g, 1)
	deadline := time.Now().Add(30 * time.Second)
	var rec *planRecord
	for time.Now().Before(deadline) {
		rec = inst.book.planLoadOrNil(taskID)
		if rec != nil && rec.State == planStateDone && len(rec.Steps) > 0 && rec.Steps[0].Status == "passed" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if rec == nil || len(rec.Steps) == 0 || rec.Steps[0].Status != "passed" {
		t.Fatalf("inconsistent plan was not retried: %+v", rec)
	}
	g.mu.Lock()
	body := ""
	if len(g.calls) > 0 {
		body = g.calls[0].Body
	}
	g.mu.Unlock()
	if !strings.Contains(body, original) {
		t.Fatalf("retry did not execute the original request: %s", body)
	}
}

// A turn started by draining the queue has no Hub accept bookkeeping. The
// runtime therefore posts a start beat immediately instead of leaving Hub
// falsely idle until the first periodic pulse.
func TestCodingTurnPostsStartHeartbeat(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putCoding(t, rt, srv.URL)
	requestID, _ := send(t, rt, spec.ID, "client-start-beat", "write a small program")
	waitCall(t, g)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		found := false
		for _, body := range g.heartbeats {
			if strings.Contains(body, requestID) && strings.Contains(body, `"finished":false`) {
				found = true
				break
			}
		}
		g.mu.Unlock()
		if found {
			release()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	release()
	t.Fatalf("no start heartbeat for %s", requestID)
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
	if len(frames) != 4 {
		t.Fatalf("frames=%d %v", len(frames), frames)
	}
	if frames[0]["event"] != "thinking" || frames[0]["state"] != "delta" || frames[0]["text"] != "thinking about it" {
		t.Fatalf("thinking frame=%v", frames[0])
	}
	if frames[1]["event"] != "token" || frames[1]["text"] != "answer text" {
		t.Fatalf("token frame=%v", frames[1])
	}
	// Coding turns report tool activity as Coding Agent Event lines — one
	// tool_started and one tool_finished per call.
	started := fmtString(frames[2]["text"])
	if frames[2]["event"] != "progress" || !strings.Contains(started, cloudCodingEventPrefix) || !strings.Contains(started, `"event":"tool_started"`) || !strings.Contains(started, `"detail":"missing_tool"`) {
		t.Fatalf("tool_started frame=%v", frames[2])
	}
	finished := fmtString(frames[3]["text"])
	if frames[3]["event"] != "progress" || !strings.Contains(finished, `"event":"tool_finished"`) || !strings.Contains(finished, `"outcome":"failed"`) {
		t.Fatalf("tool_finished frame=%v", frames[3])
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

func TestCodingSystemPromptUsesSharedFinalAnswerContract(t *testing.T) {
	dir := t.TempDir()
	bk, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer bk.Close()
	h := &agentHost{
		coding: true,
		inst:   &instance{dir: dir, meta: Spec{ID: "cag_t"}, book: bk},
	}
	prompt := h.BuildSystemPrompt("", false)
	for _, want := range []string{
		"## Final answer — coding-agent summary contract",
		"not a fixed template",
		"修复的问题",
		"优化",
		"commands actually run after the final edit",
		"Do not use internal audit headings",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("cloud coding prompt missing shared contract fragment %q:\n%s", want, prompt)
		}
	}
	session := &agentHost{inst: h.inst}
	if strings.Contains(session.BuildSystemPrompt("", false), "Final answer — coding-agent summary contract") {
		t.Fatal("non-coding cloud session prompt must not inherit the coding summary contract")
	}
}

func TestCloudFinalAnswerTextUsesSharedAuditFilterAndHostFacts(t *testing.T) {
	got := cloudFinalAnswerText(
		"已完成修复。\n\n## 质量审计\nPASS\n\n## 修复的问题\n修复空指针。",
		nil,
		[]string{"pkg/fix.go"},
		[]codingagent.FinalAnswerFileChangeFact{{Path: "pkg/fix.go", Added: 8, Removed: 2, HasCounts: true}},
		[]codingagent.FinalAnswerVerificationFact{{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: true}},
	)
	if strings.Contains(got, "质量审计") || strings.Contains(got, "PASS") {
		t.Fatalf("cloud final answer leaked internal audit: %q", got)
	}
	for _, want := range []string{"## 修复的问题", "修复空指针", "## 文件改动（1 个文件）", "| `pkg/fix.go` | 修改 | 8 | 2 |", "实际验证", "`go test ./pkg` 通过"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cloud final answer missing %q: %q", want, got)
		}
	}
}

func TestCloudVerificationFactsOnlyMarkCommandsThatSawFinalWrites(t *testing.T) {
	h := &agentHost{
		writeCount: 2,
		commands: []hostCommandRun{
			{command: "go test ./pkg", succeeded: true, writeCountAt: 2},
			{command: "go test ./pkg", succeeded: true, writeCountAt: 1},
			{command: "ls -la", succeeded: true, writeCountAt: 2},
		},
	}
	facts := h.verificationFacts()
	if len(facts) != 2 || !facts[0].AfterFinalEdit || facts[1].AfterFinalEdit {
		t.Fatalf("verification facts=%+v", facts)
	}
}

func TestCloudArgvCommandPreservesArgumentBoundaries(t *testing.T) {
	got := cloudArgvCommand([]string{"python", "my script.py", "--token", "private value"})
	want := "python 'my script.py' --token 'private value'"
	if got != want {
		t.Fatalf("argv command = %q, want %q", got, want)
	}
	if got := cloudArgvCommand([]string{"git", "status"}); got != "git status" {
		t.Fatalf("simple argv command was over-quoted: %q", got)
	}
}

func TestCloudBashMutationAdvancesWriteEpoch(t *testing.T) {
	h := &agentHost{writeCount: 1}
	h.recordBashCommand([]string{"go", "test", "./pkg"}, false)
	if h.writeCount != 1 {
		t.Fatalf("verification command must not advance write epoch: %d", h.writeCount)
	}
	h.recordBashCommand([]string{"ls", "-la"}, false)
	if h.writeCount != 1 {
		t.Fatalf("read-only command must not advance write epoch: %d", h.writeCount)
	}
	h.recordBashCommand([]string{"python", "--version"}, false)
	h.recordBashCommand([]string{"go", "version"}, false)
	if h.writeCount != 1 {
		t.Fatalf("version probes must not advance write epoch: %d", h.writeCount)
	}
	h.recordBashCommand([]string{"./scripts/generate.sh"}, false)
	if h.writeCount != 2 {
		t.Fatalf("workspace-writing script must advance write epoch: %d", h.writeCount)
	}
	facts := h.verificationFacts()
	if len(facts) != 1 || facts[0].AfterFinalEdit {
		t.Fatalf("verification before a bash mutation must not count as final evidence: %+v", facts)
	}

	h = &agentHost{}
	for _, argv := range [][]string{
		{"find", ".", "-name", "*.tmp", "-delete"},
		{"npm", "install", "test"},
		{"make", "install"},
		{"cmake", "-S", ".", "-B", "build"},
	} {
		before := h.writeCount
		h.recordBashCommand(argv, false)
		if h.writeCount != before+1 {
			t.Fatalf("%v must advance write epoch: before=%d after=%d", argv, before, h.writeCount)
		}
	}
}

func TestProducedFilesCollectsWriteTargetsAndMtimeWindow(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "stale.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fresh.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	noise := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(noise, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noise, "x.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &agentHost{root: root, started: time.Now(), inst: &instance{meta: Spec{ID: "cag_t"}}}
	h.noteFile(filepath.Join(root, "written.md"), true)
	got := h.producedFiles()
	want := map[string]bool{"fresh.txt": true, "written.md": true}
	if len(got) != len(want) {
		t.Fatalf("files %v", got)
	}
	for _, rel := range got {
		if !want[rel] {
			t.Fatalf("unexpected artifact %q in %v", rel, got)
		}
	}
}

func TestExplicitFileChangesSeparateCreatedFromModified(t *testing.T) {
	root := t.TempDir()
	h := &agentHost{root: root, inst: &instance{meta: Spec{ID: "cag_t"}}}
	h.noteFile(filepath.Join(root, "new.go"), true)
	h.noteFile(filepath.Join(root, "existing.go"), false)
	created, modified := h.explicitFileChanges()
	if len(created) != 1 || created[0] != "new.go" || len(modified) != 1 || modified[0] != "existing.go" {
		t.Fatalf("created=%v modified=%v", created, modified)
	}
}

// A spent iteration budget keeps the same conversation: the host grants
// another wave while the turn is alive, and refuses before any tool ran
// or once the turn is cancelled. The loop's stall detectors are what end
// a wedged turn, so a still-progressing one is not capped by count.
func TestExtendIterationBudgetGrantsWavesUntilStopped(t *testing.T) {
	h := &agentHost{
		inst: &instance{meta: Spec{ID: "cag_t"}},
		cfg:  corelib.MaclawLLMConfig{MaclawAgentMaxIterations: 30},
	}
	if got := h.ExtendIterationBudget(30, 0); got != 0 {
		t.Fatalf("no-tool refill = %d, want 0", got)
	}
	for wave := 1; wave <= 3; wave++ {
		if got := h.ExtendIterationBudget(30*wave, 8); got != 30 {
			t.Fatalf("wave %d refill = %d, want 30", wave, got)
		}
	}
	if h.iterationExtensions != 3 {
		t.Fatalf("iterationExtensions = %d, want 3", h.iterationExtensions)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.turn = ctx
	if got := h.ExtendIterationBudget(120, 8); got != 30 {
		t.Fatalf("live turn refill = %d, want 30", got)
	}
	cancel()
	if got := h.ExtendIterationBudget(150, 8); got != 0 {
		t.Fatalf("cancelled turn refill = %d, want 0", got)
	}
}

func TestVerificationFactsCountOnlyCommandsAfterTheFinalWrite(t *testing.T) {
	h := &agentHost{writeCount: 0}
	// Read-only review: no writes at all — every command validates the tree.
	h.commands = append(h.commands, hostCommandRun{command: "go vet ./...", succeeded: true, writeCountAt: 0})
	facts := h.verificationFacts()
	if len(facts) != 1 || !facts[0].AfterFinalEdit {
		t.Fatalf("no-edit review command must count as after-final-edit: %+v", facts)
	}

	// Write-then-verify-then-write: the mid-turn verification never saw the final tree.
	h = &agentHost{writeCount: 2}
	h.commands = append(h.commands,
		hostCommandRun{command: "go build ./...", succeeded: true, writeCountAt: 1},
		hostCommandRun{command: "go test ./...", succeeded: true, writeCountAt: 2},
	)
	facts = h.verificationFacts()
	if facts[0].AfterFinalEdit {
		t.Fatalf("verification before the last write must not count: %+v", facts[0])
	}
	if !facts[1].AfterFinalEdit {
		t.Fatalf("verification after the final write must count: %+v", facts[1])
	}
}

// A bare gate command belongs to the parked plan even when another task on
// the same instance is running. Otherwise the running-task fallback queues it
// under the wrong task and the gate remains open forever.
func TestCodingBarePlanApprovalPrefersGateOverOtherRunningTask(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const gateTask = "task-gate-parked"
	const runningTask = "task-other-running"
	if err := inst.book.EnsureTask(gateTask, "planned work"); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(gateTask, &planRecord{
		State:    planStatePending,
		Request:  "implement the planned work",
		Steps:    []planStep{{Index: 1, Title: "implement", Status: "pending"}},
		Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	holdRequest, created, err := inst.book.Begin("client-other-running", runningTask, "hold the instance", time.Now())
	if err != nil || !created || holdRequest == "" {
		t.Fatalf("begin hold request=%s created=%v err=%v", holdRequest, created, err)
	}

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-bare-gate","text":"按计划开始"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != gateTask {
		t.Fatalf("bare gate queued task=%s want %s", queuedTask, gateTask)
	}
	if rec := inst.book.planLoadOrNil(gateTask); rec == nil || rec.State != planStateApproved {
		t.Fatalf("gate state after bare approval: %+v", rec)
	}
	if _, err := inst.book.Finish(holdRequest, "held", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// Terminal records are only duplicate-click fallbacks. While another task is
// live they must not capture approval-shaped text that belongs to that task.
func TestCodingBareApprovalTextUsesRunningTaskWhenPlanIsTerminal(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const doneTask = "task-terminal-plan"
	const runningTask = "task-live"
	if err := inst.book.EnsureTask(doneTask, "finished work"); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(doneTask, &planRecord{
		State:    planStateDone,
		Request:  "finished work",
		Steps:    []planStep{{Index: 1, Title: "done", Status: "passed"}},
		Revision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	holdRequest, created, err := inst.book.Begin("client-live", runningTask, "hold the instance", time.Now())
	if err != nil || !created || holdRequest == "" {
		t.Fatalf("begin hold request=%s created=%v err=%v", holdRequest, created, err)
	}

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-live-approval-shaped","text":"按计划开始"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != runningTask {
		t.Fatalf("approval-shaped text queued task=%s want %s", queuedTask, runningTask)
	}
	if _, err := inst.book.Finish(holdRequest, "held", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// 拒绝 is also the commit gate's negative answer. A failed plan is retriable
// by 按计划开始, but it does not consume rejection while another task is live.
func TestCodingBareRejectStaysWithRunningTaskOverFailedPlan(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const failedTask = "task-failed-plan"
	const runningTask = "task-live-reject"
	if err := inst.book.EnsureTask(failedTask, "failed work"); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(failedTask, &planRecord{
		State:    planStateFailed,
		Request:  "failed work",
		Steps:    []planStep{{Index: 1, Title: "failed", Status: "failed"}},
		Revision: 3,
	}); err != nil {
		t.Fatal(err)
	}
	holdRequest, created, err := inst.book.Begin("client-live-reject", runningTask, "hold the instance", time.Now())
	if err != nil || !created || holdRequest == "" {
		t.Fatalf("begin hold request=%s created=%v err=%v", holdRequest, created, err)
	}

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-bare-reject","text":"拒绝"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != runningTask {
		t.Fatalf("bare reject queued task=%s want %s", queuedTask, runningTask)
	}
	if _, err := inst.book.Finish(holdRequest, "held", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// A rejected send must not half-apply its control-plane effect. When the
// parked-input queue is full, the pending plan gate stays open so the user can
// retry instead of losing the buttons without any execution turn.
func TestCodingQueueFullDoesNotConsumePlanGate(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const gateTask = "task-gate-queue-full"
	const runningTask = "task-live-queue-full"
	if err := inst.book.EnsureTask(gateTask, "planned work"); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(gateTask, &planRecord{
		State:    planStatePending,
		Request:  "planned work",
		Steps:    []planStep{{Index: 1, Title: "step", Status: "pending"}},
		Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	holdRequest, created, err := inst.book.Begin("client-live-queue-full", runningTask, "hold the instance", time.Now())
	if err != nil || !created || holdRequest == "" {
		t.Fatalf("begin hold request=%s created=%v err=%v", holdRequest, created, err)
	}
	for i := 0; i < queuedTurnsMax; i++ {
		if _, err := inst.book.Enqueue(newID("client_fill_"), runningTask, "filled", time.Now()); err != nil {
			t.Fatalf("fill queue %d: %v", i, err)
		}
	}

	_, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_send_message", []byte(`{"client_message_id":"client-full-gate","text":"按计划开始"}`))
	if err == nil || !strings.Contains(err.Error(), "预输入队列已满") {
		t.Fatalf("send err=%v, want queue full", err)
	}
	if rec := inst.book.planLoadOrNil(gateTask); rec == nil || rec.State != planStatePending {
		t.Fatalf("plan after rejected send=%+v, want pending", rec)
	}
	if _, err := inst.book.Finish(holdRequest, "held", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// Client-message ids make retries idempotent. A replayed approval must not
// become a fresh decision for a different plan that happens to be pending on
// the same task later.
func TestCodingReplayedApprovalDoesNotDecideNewPendingPlan(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	const gateTask = "task-replayed-gate"
	if err := inst.book.EnsureTask(gateTask, "planned work"); err != nil {
		t.Fatal(err)
	}
	oldRequest, created, err := inst.book.Begin("client-replayed-approval", gateTask, "按计划开始", time.Now())
	if err != nil || !created || oldRequest == "" {
		t.Fatalf("seed old request=%s created=%v err=%v", oldRequest, created, err)
	}
	if _, err := inst.book.Finish(oldRequest, "already handled", false, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.planSave(gateTask, &planRecord{
		State:    planStatePending,
		Request:  "new planned work",
		Steps:    []planStep{{Index: 1, Title: "new step", Status: "pending"}},
		Revision: 2,
	}); err != nil {
		t.Fatal(err)
	}

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-replayed-approval","text":"按计划开始"}`)
	var ack struct {
		Accepted  bool   `json:"accepted"`
		RequestID string `json:"request_id"`
		TaskID    string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted || ack.RequestID != oldRequest || ack.TaskID != gateTask {
		t.Fatalf("replay ack=%s err=%v want request %s task %s", raw, err, oldRequest, gateTask)
	}
	if rec := inst.book.planLoadOrNil(gateTask); rec == nil || rec.State != planStatePending {
		t.Fatalf("replayed approval changed plan: %+v", rec)
	}
}

// The finish→drain handoff is not instantaneous. A send that lands in that
// window must join the durable queue, not bypass every parked row merely
// because the previous turn has already flipped finished.
func TestCodingQueuedRowsFenceNewSendDuringDrainHandoff(t *testing.T) {
	rt := openRuntime(t)
	spec := putCoding(t, rt, "")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstQueue, err := inst.book.Enqueue("client-parked-first", "task-parked-first", "parked first", time.Now())
	if err != nil || firstQueue == "" {
		t.Fatalf("seed queue=%s err=%v", firstQueue, err)
	}
	// The handoff state is not merely "no running row": the finished row's
	// goroutine is still tracked while it unwinds. A queued row must fence the
	// late send, and the drain must wait for that teardown instead of
	// overlapping it or bypassing the parked head.
	_, cancel := context.WithCancel(context.Background())
	rt.track(spec.ID, "req-unwinding", cancel)

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-late-send","task_id":"task-late-send","text":"late send"}`)
	_, queuedTask := queuedAck(t, raw)
	if queuedTask != "task-late-send" {
		t.Fatalf("late send queued task=%s", queuedTask)
	}
	head, ok, err := inst.book.PeekOldest()
	if err != nil || !ok || head.QueueID != firstQueue || head.ClientID != "client-parked-first" {
		t.Fatalf("queue head=%+v ok=%v err=%v, want seeded row %s", head, ok, err, firstQueue)
	}
	cancel()
	rt.untrack("req-unwinding")
	waitQueueEmpty(t, rt, spec.ID, "task-late-send")
}

func TestCloudWriteFileChangeFactsMeasureNetTurnDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "demo.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{root: root}
	write := func(content string) {
		t.Helper()
		out, abs, created, added, removed, hasCounts, baseline, baselineOK := (&Runtime{}).execWriteFile(nil, root, `{"path":"demo.txt","content":`+strconvQuote(content)+`}`)
		if abs == "" || strings.Contains(out, "无效") {
			t.Fatalf("write failed: %q", out)
		}
		host.noteFileChange(abs, created, added, removed, hasCounts)
		host.noteFileBaseline(abs, baseline, baselineOK)
	}
	write("a\nx\n")
	write("a\nx\ny\n")
	facts := host.fileChangeFacts()
	if len(facts) != 1 {
		t.Fatalf("facts=%v", facts)
	}
	if !facts[0].HasCounts || facts[0].Added != 2 || facts[0].Removed != 1 {
		t.Fatalf("net turn diff = %+v, want +2 -1", facts[0])
	}
}

func TestCloudWriteFileChangeFactsCreatedFileCountsAddedOnly(t *testing.T) {
	root := t.TempDir()
	host := &agentHost{root: root, inst: &instance{}}
	out, abs, created, added, removed, hasCounts, baseline, baselineOK := (&Runtime{}).execWriteFile(nil, root, `{"path":"new.txt","content":"x"}`)
	if abs == "" || !created || strings.Contains(out, "无效") {
		t.Fatalf("write failed: %q", out)
	}
	host.noteFile(abs, created)
	host.noteFileChange(abs, created, added, removed, hasCounts)
	host.noteFileBaseline(abs, baseline, baselineOK)
	facts := host.fileChangeFacts()
	if len(facts) != 1 {
		t.Fatalf("facts=%v", facts)
	}
	if !facts[0].Created || !facts[0].HasCounts || facts[0].Added != 1 || facts[0].Removed != 0 {
		t.Fatalf("created one-line file = %+v, want created +1 -0", facts[0])
	}
}

func TestCloudWriteFileChangeFactsDeletedFileCountsRemovedLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "old.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{root: root}
	out, abs, created, added, removed, hasCounts, baseline, baselineOK := (&Runtime{}).execWriteFile(nil, root, `{"path":"old.txt","content":"a\nx\n"}`)
	if abs == "" || strings.Contains(out, "无效") {
		t.Fatalf("write failed: %q", out)
	}
	host.noteFileChange(abs, created, added, removed, hasCounts)
	host.noteFileBaseline(abs, baseline, baselineOK)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	facts := host.fileChangeFacts()
	if len(facts) != 1 || !facts[0].Deleted || !facts[0].HasCounts || facts[0].Added != 0 || facts[0].Removed != 2 {
		t.Fatalf("deleted file fact = %+v, want deleted +0 -2", facts)
	}
}

func TestCloudRepeatedWriteWithoutFirstBaselineBecomesUnknown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "demo.bin")
	// Invalid UTF-8 makes the first pre-write snapshot undiffable. The second
	// write's pre-write bytes are text, but they are intermediate bytes and must
	// not become a start-of-turn baseline.
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0o644); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{root: root}
	write := func(content string) {
		t.Helper()
		out, abs, created, added, removed, hasCounts, baseline, baselineOK := (&Runtime{}).execWriteFile(nil, root, `{"path":"demo.bin","content":`+strconvQuote(content)+`}`)
		if abs == "" || strings.Contains(out, "无效") {
			t.Fatalf("write failed: %q", out)
		}
		host.noteFileChange(abs, created, added, removed, hasCounts)
		host.noteFileBaseline(abs, baseline, baselineOK)
	}
	write("a\n")
	write("a\nb\n")
	facts := host.fileChangeFacts()
	if len(facts) != 1 {
		t.Fatalf("facts=%v", facts)
	}
	if facts[0].HasCounts || facts[0].Added != 0 || facts[0].Removed != 0 {
		t.Fatalf("repeated write without first baseline must be unknown, got %+v", facts[0])
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
