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

// lifeSrv is a MaClawSrv stand-in whose runs finish when the test says so,
// and whose message posts get distinct run ids.
type lifeSrv struct {
	turnKeySrv
	mu      sync.Mutex
	intake  string
	delay   time.Duration
	posts   []string
	cancels []string
	supps   []string
}

func (s *lifeSrv) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-intake"):
		s.mu.Lock()
		body, delay := s.intake, s.delay
		s.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if body == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/supplement"):
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.supps = append(s.supps, r.URL.Path+" "+string(raw))
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"accepted":true,"state":"queued"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
		s.mu.Lock()
		s.cancels = append(s.cancels, r.URL.Path)
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			ClientMessageID string `json:"client_message_id"`
			Content         string `json:"content"`
		}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.posts = append(s.posts, body.Content+"|"+body.ClientMessageID)
		s.mu.Unlock()
		runID := "run_" + body.ClientMessageID
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"async":true,"run":{"id":"` + runID + `","status":"running"}}`))
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
		s.mu.Lock()
		status, content := s.status, s.content
		s.mu.Unlock()
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if status == "" {
			status = "running"
		}
		if status == "running" {
			_, _ = w.Write([]byte(`{"id":"` + id + `","status":"running"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + id + `","status":"` + status + `","desktop_ready":true,"message":{"content":"` + content + `"}}`))
	default:
		s.handler(w, r)
	}
}

func (s *lifeSrv) posted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.posts...)
}

func waitPosts(t *testing.T, fake *lifeSrv, n int) []string {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		got := fake.posted()
		if len(got) >= n {
			return got
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("posts = %v, want %d", fake.posted(), n)
	return nil
}

func TestParkedMessageStartsWhenASlotFrees(t *testing.T) {
	fake := &lifeSrv{intake: `{"kind":"new","instruction":"","ack":"先记着。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.BotParallelRuns = 1
	svc.queueDrainOff = true
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-a")
	if err != nil || first.RunID != "run_turn-a" || first.Queued {
		t.Fatalf("first = %+v %v", first, err)
	}
	parked, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "再开一个", "execute", "turn-b")
	if err != nil || !parked.Queued || parked.Held || parked.Position != 1 {
		t.Fatalf("parked = %+v %v", parked, err)
	}
	if len(fake.posted()) != 1 {
		t.Fatalf("posted while full: %v", fake.posted())
	}
	fake.mu.Lock()
	fake.status = "succeeded"
	fake.content = "报表好了"
	fake.mu.Unlock()
	got := waitPosts(t, fake, 2)
	if !strings.Contains(got[1], "再开一个") {
		t.Fatalf("slot did not start the parked message: %v", got)
	}
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-b")
	if (view.State != "running" && view.State != "done") || view.RunID == "" {
		t.Fatalf("after start view = %+v", view)
	}
}

func TestSupplementAppliesToTheRunningRunOnly(t *testing.T) {
	fake := &lifeSrv{intake: `{"kind":"new","instruction":"导出订单","ack":"导出。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "导出订单", "execute", "turn-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "另写一封信", "execute", "turn-b")
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.intake = `{"kind":"supplement","run_id":"` + first.RunID + `","instruction":"只要前五条","ack":"好，只留前五条。","pace":"direct"}`
	fake.mu.Unlock()
	got, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "只要前五条", "execute", "turn-s")
	if err != nil || got.Kind != "supplement" || got.RunID != first.RunID {
		t.Fatalf("supplement = %+v %v", got, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.supps) != 1 || !strings.Contains(fake.supps[0], first.RunID) || !strings.Contains(fake.supps[0], "只要前五条") || strings.Contains(fake.supps[0], "run_turn-b") {
		t.Fatalf("supps = %v", fake.supps)
	}
	if len(fake.posts) != 2 {
		t.Fatalf("supplement started a run: %v", fake.posts)
	}
}

func TestStopCancelsOnlyTheTargetRun(t *testing.T) {
	fake := &lifeSrv{intake: `{"kind":"new","instruction":"","ack":"开始。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "导出订单", "execute", "turn-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "另写一封信", "execute", "turn-b")
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.intake = `{"kind":"stop","run_id":"` + first.RunID + `","ack":"停下导出。","pace":"direct"}`
	fake.mu.Unlock()
	stopped, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "停下导出", "execute", "turn-stop")
	if err != nil || stopped.Kind != "stop" || stopped.RunID != first.RunID {
		t.Fatalf("stop = %+v %v", stopped, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.cancels) != 1 || !strings.Contains(fake.cancels[0], first.RunID) || strings.Contains(fake.cancels[0], second.RunID) {
		t.Fatalf("cancels = %v, first=%s second=%s", fake.cancels, first.RunID, second.RunID)
	}
}

func TestIntakeTimeoutBecomesANewTaskOfTheUsersWords(t *testing.T) {
	prev := desktopIntakeBudget
	desktopIntakeBudget = 40 * time.Millisecond
	t.Cleanup(func() { desktopIntakeBudget = prev })
	fake := &lifeSrv{intake: `{"kind":"stop","run_id":"run_x","ack":"停","pace":"direct"}`, delay: time.Second}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	start := time.Now()
	got, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "", "turn-t")
	if err != nil || got.Kind != "discuss" || got.RunID != "" || time.Since(start) > time.Second {
		t.Fatalf("timeout = %+v %v after %v", got, err, time.Since(start))
	}
	if posts := fake.posted(); len(posts) != 0 {
		t.Fatalf("timeout started a run: %v", posts)
	}
}

func TestParkedQueueSurvivesRestartAndStaysBounded(t *testing.T) {
	fake := &lifeSrv{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.queueDrainOff = true
	past := time.Now().Add(-7 * time.Hour)
	svc.Now = func() time.Time { return past }
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-old", Content: "过期的那件", Phase: "execute", Kind: "new", Instruction: "过期的那件",
	}); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return time.Now() }
	if view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-old"); view.State != "missing" {
		t.Fatalf("expired view = %+v", view)
	}
	for i := 0; i < desktopQueueMax; i++ {
		if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
			ClientMessageID: "turn-n" + itoaLife(i),
			Content:         "第" + itoaLife(i) + "件",
			Phase:           "execute",
			Kind:            "new",
			Instruction:     "第" + itoaLife(i) + "件",
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if _, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot_alice", desktopQueueItem{
		ClientMessageID: "turn-overflow", Content: "装不下", Phase: "execute", Kind: "new", Instruction: "装不下",
	}); err != ErrDesktopQueueFull {
		t.Fatalf("overflow err = %v", err)
	}
	restarted := NewService(svc.System)
	restarted.HTTP = srv.Client()
	restarted.queueDrainOff = true
	restarted.BotParallelRuns = desktopQueueMax
	restarted.Desktop = svc.Desktop
	restarted.admitPollInterval = svc.admitPollInterval
	restarted.admitReadyGrace = svc.admitReadyGrace
	view := restarted.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-n0")
	if view.State != "queued" || view.Position != 1 {
		t.Fatalf("after restart view = %+v", view)
	}
	if got := restarted.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-old"); got.State != "missing" {
		t.Fatalf("expired row came back: %+v", got)
	}
	restarted.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	posts := fake.posted()
	if len(posts) != desktopQueueMax || !strings.Contains(posts[0], "第0件") {
		t.Fatalf("drained posts = %v", posts)
	}
}

func itoaLife(n int) string {
	if n == 0 {
		return "0"
	}
	var d [8]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}
