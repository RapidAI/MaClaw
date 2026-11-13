package botmgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type memDesktop struct {
	used, limit int64
}

func (m *memDesktop) Open(context.Context, string, string) (string, error) {
	return "http://dockerd.example/vnc", nil
}
func (m *memDesktop) Stop(context.Context, string, string) error { return nil }
func (m *memDesktop) ContainerMemory(context.Context, string, string) (int64, int64, error) {
	return m.used, m.limit, nil
}

func TestMemoryPressureQueuesTheRunUntilItRecovers(t *testing.T) {
	if !memoryPressured(90, 100, 0.85) || memoryPressured(80, 100, 0.85) {
		t.Fatal("threshold")
	}
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"打开报表","ack":"好。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	mem := &memDesktop{used: 90, limit: 100}
	svc.Desktop = mem
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-mem")
	if err != nil || !adm.Queued || adm.Ack != desktopMemoryLine {
		t.Fatalf("queued = %+v %v", adm, err)
	}
	fake.mu.Lock()
	if len(fake.posts) != 0 {
		t.Fatalf("started under pressure: %v", fake.posts)
	}
	fake.mu.Unlock()
	mem.used = 10
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 1 || !strings.Contains(fake.posts[0], "打开报表") {
		t.Fatalf("resumed posts=%v", fake.posts)
	}
}

func TestMemoryParkedMessageGivesUp(t *testing.T) {
	fake := &intakeSrv{intake: `{"kind":"new","instruction":"打开报表","ack":"好。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.Desktop = &memDesktop{used: 90, limit: 100}
	when := time.Now()
	svc.Now = func() time.Time { return when }
	prev := desktopMemoryQueueMax
	desktopMemoryQueueMax = time.Minute
	t.Cleanup(func() { desktopMemoryQueueMax = prev })
	adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开报表", "execute", "turn-mem-wait")
	if err != nil || !adm.Queued {
		t.Fatalf("queued = %+v %v", adm, err)
	}
	when = when.Add(2 * time.Minute)
	svc.drainDesktopQueue("tenant-a", "alice", "bot_alice")
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-mem-wait")
	if view.State != "gave_up" || view.Outcome != desktop.MemoryBusyOutcome {
		t.Fatalf("view = %+v", view)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.posts) != 0 {
		t.Fatalf("gave up but still started: %v", fake.posts)
	}
}
