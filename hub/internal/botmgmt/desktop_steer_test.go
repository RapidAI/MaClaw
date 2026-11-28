package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// intakeSrv is a MaClawSrv stand-in that answers bot-intake and records
// message posts and supplements.
type intakeSrv struct {
	turnKeySrv
	mu      sync.Mutex
	intake  string
	intakes []string
	posts   []string
	supps   []string
	cancels int
	pauses  int
	resumes int
}

func (s *intakeSrv) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-intake"):
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		body := s.intake
		s.intakes = append(s.intakes, string(raw))
		s.mu.Unlock()
		if body == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/supplement"):
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.supps = append(s.supps, string(raw))
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"accepted":true,"state":"queued"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
		s.mu.Lock()
		s.cancels++
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause"):
		s.mu.Lock()
		s.pauses++
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"run_id":"run_turn","paused":true}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resume"):
		s.mu.Lock()
		s.resumes++
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"run_id":"run_turn","paused":false}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.posts = append(s.posts, string(raw))
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_turn","status":"running"}}`))
	default:
		s.handler(w, r)
	}
}

func TestHubIntakeSupplementDoesNotStartARun(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"打开报表","ack":"我去打开报表。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-a")
	if err != nil || first.RunID == "" || first.Ack != "我去打开报表。" {
		t.Fatalf("first = %+v %v", first, err)
	}
	fake.mu.Lock()
	fake.intake = `{"kind":"supplement","run_id":"` + first.RunID + `","instruction":"只要前五条","ack":"好，只留前五条。","pace":"direct"}`
	fake.mu.Unlock()
	second, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "只要前五条", "execute", "turn-b")
	if err != nil || second.Kind != "supplement" || second.RunID != first.RunID || second.Ack != "好，只留前五条。" {
		t.Fatalf("second = %+v %v", second, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 1 || len(fake.supps) != 1 || !strings.Contains(fake.supps[0], "只要前五条") {
		t.Fatalf("posts=%d supps=%v", len(fake.posts), fake.supps)
	}
}

func TestHubIntakeTimeoutAdmitsTheWordsAsANewTask(t *testing.T) {
	prev := desktopIntakeBudget
	desktopIntakeBudget = 40 * time.Millisecond
	t.Cleanup(func() { desktopIntakeBudget = prev })
	fake := &intakeSrv{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-intake") {
			time.Sleep(time.Second)
			return
		}
		fake.handle(w, r)
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	start := time.Now()
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-t")
	if err != nil || adm.RunID == "" || time.Since(start) > time.Second {
		t.Fatalf("adm=%+v err=%v after %v", adm, err, time.Since(start))
	}
	// The intake timed out (no ack of its own), so the admit names the
	// person's own command instead of going silent.
	if !strings.Contains(adm.Ack, "打开报表") {
		t.Fatalf("expected the admit ack to name the command, got %q", adm.Ack)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "打开报表") {
		t.Fatalf("posts=%v", fake.posts)
	}
}

func TestHubExplicitNewMatterSkipsIntake(t *testing.T) {
	var intakes int
	fake := &intakeSrv{intake: `{"kind":"supplement","run_id":"run_x","instruction":"nope","ack":"no","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bot-intake") {
			intakes++
		}
		fake.handle(w, r)
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	adm, err := svc.AdmitDesktopMessageIn(context.Background(), "tenant-a", "alice", "bot_alice", "另外一件事", "execute", "turn-new", "bot_alice")
	if err != nil || adm.Kind == "supplement" {
		t.Fatalf("adm=%+v %v", adm, err)
	}
	if intakes != 0 {
		t.Fatalf("intake calls = %d", intakes)
	}
}

func TestHubStopAndPause(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"导出","ack":"导出。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "导出", "execute", "turn-a")
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.intake = `{"kind":"pause","run_id":"` + first.RunID + `","ack":"先停一下。","pace":"direct"}`
	fake.mu.Unlock()
	paused, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "先停一下", "execute", "turn-p")
	if err != nil || paused.Kind != "pause" || fake.cancels != 0 {
		fake.mu.Lock()
		t.Fatalf("pause=%+v err=%v cancels=%d pauses=%d", paused, err, fake.cancels, fake.pauses)
	}
	fake.mu.Lock()
	fake.intake = `{"kind":"stop","run_id":"` + first.RunID + `","ack":"停下了。","pace":"direct"}`
	fake.mu.Unlock()
	stopped, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "停下", "execute", "turn-s")
	if err != nil || stopped.Kind != "stop" {
		t.Fatalf("stop=%+v %v", stopped, err)
	}
	fake.mu.Lock()
	if fake.cancels != 1 || fake.pauses != 1 {
		t.Fatalf("cancels=%d pauses=%d", fake.cancels, fake.pauses)
	}
	fake.mu.Unlock()
}

func TestHubPauseUnsupportedDoesNotCancel(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"导出","ack":"导出。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause") {
			http.NotFound(w, r)
			return
		}
		fake.handle(w, r)
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "导出", "execute", "turn-a")
	if err != nil {
		t.Fatal(err)
	}
	// The run read that distinguishes "no pause route" from "no such run"
	// is the same /runs/ handler, which answers running.
	fake.mu.Lock()
	fake.intake = `{"kind":"pause","run_id":"` + first.RunID + `","ack":"先按住。","pace":"direct"}`
	fake.mu.Unlock()
	paused, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "先按住", "execute", "turn-p")
	if err != nil || paused.Kind != "pause" {
		t.Fatalf("pause=%+v %v", paused, err)
	}
	fake.mu.Lock()
	if fake.cancels != 0 {
		t.Fatalf("unsupported pause cancelled the run")
	}
	fake.mu.Unlock()
}

func TestHubHoldsWhileThePersonHasTheKeyboard(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"下一件","ack":"等你交还。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	held, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "下一件", "execute", "turn-h")
	if err != nil || !held.Queued || !held.Held {
		t.Fatalf("held=%+v %v", held, err)
	}
	if !strings.HasPrefix(held.Ack, desktopHeldAck) || !strings.Contains(held.Ack, "等你交还。") {
		t.Fatalf("held ack must say the desktop is waiting for the person, got %q", held.Ack)
	}
	fake.mu.Lock()
	if len(fake.posts) != 0 {
		t.Fatalf("posted while the person has the keyboard: %d", len(fake.posts))
	}
	fake.mu.Unlock()
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-h")
	if view.State != "held" || !view.Held || view.Position != 1 {
		t.Fatalf("view=%+v", view)
	}
	back, err := svc.AdmitDesktopMessageAct(context.Background(), "tenant-a", "alice", "bot_alice", "交还后继续", "execute", "turn-r", "", "return")
	if err != nil || back.RunID == "" {
		t.Fatalf("return=%+v %v", back, err)
	}
	if svc.personHasBotKeyboard("tenant-a", "alice", "bot_alice") {
		t.Fatal("return left the keyboard with the person")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	joined := strings.Join(fake.posts, "\n")
	if !strings.Contains(joined, "交还后继续") {
		t.Fatalf("交还 did not start the continuation: %v", fake.posts)
	}
	// The held message does not become a second execution beside the
	// continuation. It folds into the continuation's run as a supplement:
	// one handoff, one piece of work, everything the person typed while they
	// waited is input to it.
	if strings.Contains(joined, "下一件") {
		t.Fatalf("held message started a parallel execution: %v", fake.posts)
	}
	supps := strings.Join(fake.supps, "\n")
	if !strings.Contains(supps, "下一件") {
		t.Fatalf("held message did not fold into the continuation: supps=%v", fake.supps)
	}
}

func TestHubHoldKeepsTheDecidedOrder(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"目标：一份周报\n完成标准：老王收到文件","ack":"等你交还。","pace":"direct","phase":"execute"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	held, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "发给老王", "execute", "turn-spec")
	if err != nil || !held.Queued || !held.Held {
		t.Fatalf("held=%+v %v", held, err)
	}
	if _, err := svc.AdmitDesktopMessageAct(context.Background(), "tenant-a", "alice", "bot_alice", "交还", "execute", "turn-back", "", "return"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	// The held message folds into the continuation's run as a supplement,
	// and the fold must carry the decided instruction — not just the raw
	// words — or the merge would lose what the intake had already decided.
	supps := strings.Join(fake.supps, "\n")
	if !strings.Contains(supps, "完成标准：老王收到文件") {
		t.Fatalf("held fold lost the decided order: supps=%v posts=%v", fake.supps, fake.posts)
	}
}

// A handoff left unanswered past the abandonment ceiling is a dead pause,
// not a live one: the next independent task is the person's real intent, so
// it starts instead of queueing behind a prompt nobody is answering.
func TestHubStaleKeyboardHoldDoesNotBlockANewTask(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"下一件","ack":"这就去。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	now := time.Now()
	svc.Now = func() time.Time { return now }
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	// Twenty-one minutes later the person never handed the desktop back:
	// the login/payment prompt belongs to a task they walked away from.
	now = now.Add(desktopKeyboardHandoffMax + time.Minute)
	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "下一件", "execute", "turn-stale")
	if err != nil {
		t.Fatal(err)
	}
	if admission.Held || admission.Queued {
		t.Fatalf("stale handoff still held the message: %+v", admission)
	}
	if svc.personHasBotKeyboard("tenant-a", "alice", "bot_alice") {
		t.Fatal("stale hold was not voided")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) == 0 {
		t.Fatal("new task did not start once the stale hold was voided")
	}
}

// The production deadlock: a message held behind a fresh handoff must start
// on its own once that handoff ages out — the person never pressed 交还 and
// the queue patrol is the only thing still looking at it.
func TestHubHeldMessageStartsAfterTheHoldAgesOut(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"下一件","ack":"这就去。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	now := time.Now()
	svc.Now = func() time.Time { return now }
	if err := svc.HoldDesktop(context.Background(), "tenant-a", "alice", "inst_alice"); err != nil {
		t.Fatal(err)
	}
	held, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "下一件", "execute", "turn-aged")
	if err != nil || !held.Held {
		t.Fatalf("held=%+v %v", held, err)
	}
	fake.mu.Lock()
	if len(fake.posts) != 0 {
		t.Fatalf("posted while the handoff was fresh: %d", len(fake.posts))
	}
	fake.mu.Unlock()
	now = now.Add(desktopKeyboardHandoffMax + time.Minute)
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	deadline := time.Now().Add(2 * time.Second)
	for {
		fake.mu.Lock()
		posted := len(fake.posts)
		fake.mu.Unlock()
		if posted > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("held message never started after the hold aged out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}



func TestHubQueueDrainsWhenASlotFrees(t *testing.T) {
	fake := &intakeSrv{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.BotParallelRuns = 1
	svc.queueDrainOff = true
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-q",
		Content:         "排队的那件",
		Phase:           "execute",
		Kind:            "new",
		Instruction:     "排队的那件",
	}); err != nil {
		t.Fatal(err)
	}
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	fake.mu.Lock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "排队的那件") {
		t.Fatalf("posts=%v", fake.posts)
	}
	fake.mu.Unlock()
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-q")
	if view.State != "running" || view.RunID == "" {
		t.Fatalf("view=%+v", view)
	}
	raw, _ := json.Marshal(view)
	if !strings.Contains(string(raw), `"state":"running"`) {
		t.Fatalf("json=%s", raw)
	}
}

// A bot created with desktop_mode=none tells the intake its real surface
// (no_desktop), and the posted turn keeps the same marker. Without the
// first, a rewritten instruction can order a desktop the bot does not
// have; without the second, MaClawSrv still gates and prompts for one.
func TestHubNoDesktopBotMarksTheIntakeAndTheMessage(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"查北京天气","ack":"我去查。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv,
		Bot{ID: "bot_none", Name: "no-pc", InstanceID: "inst_alice", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone})
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_none", "北京天气", "execute", "turn-none")
	if err != nil || adm.RunID == "" {
		t.Fatalf("adm=%+v %v", adm, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.intakes) != 1 || !strings.Contains(fake.intakes[0], `"no_desktop":true`) {
		t.Fatalf("intake bodies=%v", fake.intakes)
	}
	if len(fake.posts) != 1 {
		t.Fatalf("posts=%v", fake.posts)
	}
	post := fake.posts[0]
	if !strings.Contains(post, `"bot_desktop":"none"`) || strings.Contains(post, "computer_helper") {
		t.Fatalf("post body=%s", post)
	}
}

func TestHubContinueOnlyWhileARunIsLive(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"continue","run_id":"run_missing","ack":"接着。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "继续", "execute", "turn-c")
	if err != nil || adm.RunID == "" || adm.Kind == "continue" {
		t.Fatalf("continue with no run = %+v %v", adm, err)
	}
	fake.mu.Lock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "继续") {
		t.Fatalf("posts=%v", fake.posts)
	}
	fake.mu.Unlock()
}

func TestHubSpecQuestionDoesNotStartARun(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"ask","instruction":"周报，收件人未定","ack":"发给谁？","told":"发给谁？","pace":"ask","phase":"plan","spec":true}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	asked, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "做一份周报", "", "turn-spec")
	if err != nil || asked.Kind != "ask" || asked.RunID != "" || asked.Reply.Text != "发给谁？" || !asked.Settled {
		t.Fatalf("ask = %+v %v", asked, err)
	}
	fake.mu.Lock()
	if len(fake.posts) != 0 {
		t.Fatalf("a specifying question started a run: %v", fake.posts)
	}
	fake.intake = `{"kind":"new","instruction":"把表格做成周报发给老王","ack":"我做成周报发给老王。","pace":"direct","phase":"execute","spec":true}`
	fake.mu.Unlock()
	ran, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "发给老王", "", "turn-go")
	if err != nil || ran.RunID == "" || ran.Kind == "ask" {
		t.Fatalf("settled = %+v %v", ran, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "把表格做成周报发给老王") || strings.Contains(fake.posts[0], `"bot_phase":"plan"`) {
		t.Fatalf("posts=%v", fake.posts)
	}
}
