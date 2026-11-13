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
)

// intakeSrv is a MaClawSrv stand-in that answers bot-intake and records
// message posts and supplements.
type intakeSrv struct {
	turnKeySrv
	mu      sync.Mutex
	intake  string
	posts   []string
	discuss []string
	supps   []string
	cancels int
	pauses  int
	resumes int
}

func (s *intakeSrv) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-intake"):
		s.mu.Lock()
		body := s.intake
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
	case strings.Contains(r.URL.Path, "/bot-discuss"):
		if r.Method == http.MethodGet || strings.HasSuffix(r.URL.Path, "/executing") {
			_, _ = w.Write([]byte(`{"state":"planned","plan":{"goal":"打开报表"},"excerpts":["人: 打开报表"]}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.discuss = append(s.discuss, string(raw))
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"state":"planned","session":"bot_alice","plan":{"goal":"打开报表"},"excerpts":["人: 打开报表","助手: 先记着。"]}`))
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
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "", "turn-t")
	if err != nil || adm.Kind != "discuss" || adm.RunID != "" || time.Since(start) > time.Second {
		t.Fatalf("adm=%+v err=%v after %v", adm, err, time.Since(start))
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 0 || len(fake.discuss) != 1 || !strings.Contains(fake.discuss[0], "打开报表") {
		t.Fatalf("posts=%v discuss=%v", fake.posts, fake.discuss)
	}
}

func TestHubDiscussDoesNotStartUntilThePersonStarts(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"把报表发到站上","ack":"先把计划对齐。","pace":"direct","plan":{"goal":"把报表发到站上","constraints":["用现有的图"],"success":["文章在列表里"],"open_questions":[]}}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	talk, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "把报表发到站上", "", "turn-d")
	if err != nil || talk.Kind != "discuss" || talk.RunID != "" || talk.Discuss == nil || talk.Discuss.Plan.Goal == "" {
		t.Fatalf("talk=%+v %v", talk, err)
	}
	fake.mu.Lock()
	if len(fake.posts) != 0 {
		t.Fatalf("discussion started a run: %v", fake.posts)
	}
	fake.mu.Unlock()
	started, err := svc.AdmitDesktopMessageAct(context.Background(), "tenant-a", "alice", "bot_alice", "开始", "", "turn-go", "bot_alice", "start")
	if err != nil || started.RunID == "" || started.Kind == "discuss" {
		t.Fatalf("start=%+v %v", started, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "目标：") || !strings.Contains(fake.posts[0], "讨论摘录") {
		t.Fatalf("start post=%v", fake.posts)
	}
	if strings.Contains(fake.posts[0], `"content":"开始"`) {
		t.Fatalf("start used only the start sentence: %s", fake.posts[0])
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
	if err != nil || !held.Queued || !held.Held || held.Ack != "等你交还。" {
		t.Fatalf("held=%+v %v", held, err)
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
	if !strings.Contains(joined, "下一件") || !strings.Contains(joined, "交还后继续") {
		t.Fatalf("交还 did not release the held message: %v", fake.posts)
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
