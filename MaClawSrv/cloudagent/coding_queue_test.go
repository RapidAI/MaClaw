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
		[]codingagent.FinalAnswerVerificationFact{{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: true}},
	)
	if strings.Contains(got, "质量审计") || strings.Contains(got, "PASS") {
		t.Fatalf("cloud final answer leaked internal audit: %q", got)
	}
	for _, want := range []string{"## 修复的问题", "修复空指针", "实际验证", "`go test ./pkg` 通过"} {
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
