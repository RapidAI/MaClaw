package guiapp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type noteFakeHub struct {
	*fakeMBHub
	nmu     sync.Mutex
	items   []DesktopBotCrossNote
	reports []string
}

// mailbox mimics Hub: notes to the primary (bot_1) stay until acked.
func (h *noteFakeHub) mailbox(ctx context.Context, ack []string) ([]DesktopBotCrossNote, error) {
	h.nmu.Lock()
	defer h.nmu.Unlock()
	for _, id := range ack {
		for i := range h.items {
			if h.items[i].ID == id {
				h.items[i].AckedAt = "now"
			}
		}
	}
	out := []DesktopBotCrossNote{}
	for _, n := range h.items {
		if n.Target == "bot_1" && n.AckedAt == "" {
			out = append(out, n)
		}
	}
	return out, nil
}
func (h *noteFakeHub) report(ctx context.Context, from, target, topic, text, replyTo string, arts []string) (DesktopBotCrossNote, error) {
	h.nmu.Lock()
	defer h.nmu.Unlock()
	h.reports = append(h.reports, from+"|"+target+"|"+topic+"|"+replyTo+"|"+text)
	return DesktopBotCrossNote{ID: "r"}, nil
}

func waitWake(t *testing.T, ch chan MultiBotWake) MultiBotWake {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("no wake")
	}
	return MultiBotWake{}
}

func TestMultiBotReportWakesIdleMainAndQueuesWhenBusy(t *testing.T) {
	base, _ := newTestMultiBot(t)
	base.close()
	hub := &noteFakeHub{fakeMBHub: base.hub.(*fakeMBHub)}
	wakes := make(chan MultiBotWake, 4)
	mb := newMultiBot(hub, t.TempDir(), func(name string, data ...interface{}) {
		if name == "multibot-wake" {
			wakes <- data[0].(MultiBotWake)
		}
	})
	ctx := context.Background()
	_ = mb.refresh(ctx, true)
	// Sent while the GUI was closed: replayed on the first read.
	hub.items = []DesktopBotCrossNote{{ID: "old", Kind: "report", From: "bot_2", Target: "bot_1", Topic: "done", Text: "旧的"}}
	mb.pollReports(ctx)
	if w := waitWake(t, wakes); !strings.Contains(w.Prompt, "旧的") {
		t.Fatalf("backlog not replayed: %s", w.Prompt)
	}

	hub.nmu.Lock()
	hub.items = append(hub.items, DesktopBotCrossNote{ID: "n1", Kind: "report", From: "bot_2", Target: "bot_1", Topic: "artifact", Text: "截图好了", Artifacts: []string{"/home/desktop/Desktop/s.png"}, RunID: "run_7"},
		DesktopBotCrossNote{ID: "n2", Kind: "report", From: "bot_1", Target: "bot_2", Topic: "info", Text: "to peer"})
	hub.nmu.Unlock()
	mb.pollReports(ctx)
	mb.pollReports(ctx) // no double delivery; acks the first read
	w := waitWake(t, wakes)
	if !strings.HasPrefix(w.Prompt, "[队友消息") || !strings.Contains(w.Prompt, "截图好了") || !strings.Contains(w.Prompt, "s.png") || strings.Contains(w.Prompt, "旧的") || strings.Contains(w.Prompt, "to peer") {
		t.Fatalf("prompt = %s", w.Prompt)
	}
	if !strings.Contains(w.Display, "发布员") || len(w.IDs) != 1 {
		t.Fatalf("wake = %+v", w)
	}

	end := mb.beginSelfTurn(w.Prompt)
	hub.nmu.Lock()
	hub.items = append(hub.items, DesktopBotCrossNote{ID: "n3", Kind: "report", From: "bot_2", Target: "bot_1", Topic: "question", Text: "要发布吗?"})
	hub.nmu.Unlock()
	mb.pollReports(ctx)
	select {
	case w := <-wakes:
		t.Fatalf("woke while busy: %+v", w)
	case <-time.After(2 * time.Second):
	}
	end()
	if w := waitWake(t, wakes); !strings.Contains(w.Prompt, "要发布吗") {
		t.Fatalf("queued wake = %s", w.Prompt)
	}
}

func TestMultiBotReplyLoopGuardAndRunEvents(t *testing.T) {
	mb, hub := newTestMultiBot(t)
	wakes := make(chan MultiBotWake, 8)
	mb.emit = func(name string, data ...interface{}) {
		if name == "multibot-wake" {
			wakes <- data[0].(MultiBotWake)
		}
	}
	_ = mb.refresh(context.Background(), true)
	refused := false
	for i := 0; i < 12; i++ {
		out := mb.toolSendToBot(map[string]interface{}{"bot": "发布员", "text": "再试一次 " + string(rune('a'+i)), "reply_to": "msg_1"})
		if strings.HasPrefix(out, "not sent") {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatalf("ping-pong not capped: %v", hub.crosses)
	}
	if !strings.Contains(hub.crosses[0], "[re msg_1]") {
		t.Fatalf("reply not threaded: %s", hub.crosses[0])
	}
	mb.bus.PersonSpoke()
	later := time.Now().Add(2 * time.Minute)
	mb.bus.SetClock(func() time.Time { return later })
	if out := mb.toolSendToBot(map[string]interface{}{"bot": "发布员", "text": "person said go"}); strings.HasPrefix(out, "not sent") {
		t.Fatalf("person turn should reset: %s", out)
	}
	// A teammate waiting for the person wakes the main bot without any report call.
	hub.runs["bot_2"] = []DesktopBotActiveRun{{RunID: "run_1", Status: "waiting_for_user", Task: "发文章", Step: "扫码登录"}}
	_ = mb.refresh(context.Background(), true)
	w := waitWake(t, wakes)
	if !strings.Contains(w.Prompt, "kind=blocked") || !strings.Contains(w.Prompt, "bot_2") {
		t.Fatalf("run-finished wake = %s", w.Prompt)
	}
}

func TestMultiBotMailboxAcksAfterHandoffAndSendGoesToHub(t *testing.T) {
	base, _ := newTestMultiBot(t)
	base.close()
	hub := &noteFakeHub{fakeMBHub: base.hub.(*fakeMBHub)}
	mb := newMultiBot(hub, t.TempDir(), func(string, ...interface{}) {})
	ctx := context.Background()
	_ = mb.refresh(ctx, true)
	hub.items = []DesktopBotCrossNote{{ID: "q1", Kind: "report", From: "bot_2", Target: "bot_1", Topic: "question", Text: "标题?"}}
	mb.pollReports(ctx)
	if hub.items[0].AckedAt != "" {
		t.Fatal("acked before the next read")
	}
	mb.pollReports(ctx)
	if hub.items[0].AckedAt == "" {
		t.Fatal("not acked after hand-off")
	}
	out := mb.toolSendToBot(map[string]interface{}{"bot": "发布员", "text": "用第一个", "reply_to": "msg_1"})
	if strings.HasPrefix(out, "error") || len(hub.reports) != 1 || hub.reports[0] != "bot_1|bot_2|reply|msg_1|用第一个" {
		t.Fatalf("out=%s reports=%v crosses=%v", out, hub.reports, hub.crosses)
	}
}

type fileNoteHub struct{ *noteFakeHub }

func (h fileNoteHub) mailFile(ctx context.Context, noteID, name string) (string, string, []byte, error) {
	return name, "text/csv", []byte("a,b"), nil
}

// A file shared mid-run becomes a local artifact as soon as the message is read.
func TestMultiBotSharedFileIsLocalOnArrival(t *testing.T) {
	base, _ := newTestMultiBot(t)
	base.close()
	hub := fileNoteHub{&noteFakeHub{fakeMBHub: base.hub.(*fakeMBHub)}}
	wakes := make(chan MultiBotWake, 2)
	mb := newMultiBot(hub, t.TempDir(), func(name string, data ...interface{}) {
		if name == "multibot-wake" && len(data) > 0 {
			if w, ok := data[0].(MultiBotWake); ok {
				wakes <- w
			}
		}
	})
	ctx := context.Background()
	_ = mb.refresh(ctx, true)
	hub.items = []DesktopBotCrossNote{{ID: "f1", Kind: "report", From: "bot_2", Target: "bot_1", Topic: "artifact", Text: "导出了",
		Artifacts: []string{"/home/d/orders.csv"}, Files: []DesktopBotMailFile{{Name: "orders.csv", Size: 3}}}}
	mb.pollReports(ctx)
	w := waitWake(t, wakes)
	if !strings.Contains(w.Prompt, "/home/d/orders.csv") || !strings.Contains(w.Prompt, "orders.csv") || !strings.Contains(w.Prompt, "art_") {
		t.Fatalf("prompt lacks the local file: %s", w.Prompt)
	}
}
