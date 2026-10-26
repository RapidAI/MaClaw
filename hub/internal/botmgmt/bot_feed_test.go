package botmgmt

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// A server-side wake acks the main bot's mail, but the GUI that opens later
// still shows the whole exchange (delegate, the teammate's file message and
// result, and which wake run took them) until it marks them shown.
func TestServerWakeDoesNotHideMessagesFromTheGUI(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "发布员", Text: "写个文件", Key: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(f.list()) == 1 })
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		n := findCrossNote(notes, d.ID)
		return n != nil && n.RunID != ""
	})
	svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_xdel-"+d.ID, "succeeded", Reply{Text: "写好了"}, nil)
	waitFor(t, func() bool { return len(f.list()) == 2 })
	var result *BotCrossNote
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		for i := range notes {
			if notes[i].ReplyTo == d.ID && notes[i].WakeRunID != "" {
				result = &notes[i]
				return true
			}
		}
		return false
	})
	// The server-side main run takes (acks) its mail.
	if _, err := svc.BotMailbox(ctx, "tenant-a", "alice", "main", []string{result.ID}, false); err != nil {
		t.Fatal(err)
	}
	feed, err := svc.BotFeed(ctx, "tenant-a", "alice", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]BotCrossNote{}
	for _, n := range feed {
		seen[n.ID] = n
	}
	if _, ok := seen[d.ID]; !ok {
		t.Fatalf("delegate missing from the GUI feed: %+v", feed)
	}
	if r, ok := seen[result.ID]; !ok || r.AckedAt == "" || r.WakeRunID == "" {
		t.Fatalf("acked result must still be shown, with its wake run: %+v", r)
	}
	ids := []string{}
	for _, n := range feed {
		ids = append(ids, n.ID)
	}
	if rest, _ := svc.BotFeed(ctx, "tenant-a", "alice", ids, false); len(rest) != 0 {
		t.Fatalf("shown notes came back: %+v", rest)
	}
	if hist, _ := svc.BotFeed(ctx, "tenant-a", "alice", nil, true); len(hist) != len(feed) {
		t.Fatalf("history = %d, want %d", len(hist), len(feed))
	}
}

// stallSettings blocks every Set behind a gate once armed, with the caller
// still holding the service lock — the shape of a slow write mid-save.
type stallSettings struct {
	mu      sync.Mutex
	m       map[string]string
	gate    chan struct{}
	armed   bool
	entered chan struct{}
	once    sync.Once
}

func (s *stallSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.m[key]
	if !ok {
		return "", io.EOF
	}
	return value, nil
}

func (s *stallSettings) Set(_ context.Context, key, valueJSON string) error {
	s.mu.Lock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = valueJSON
	armed := s.armed
	s.mu.Unlock()
	if armed {
		s.once.Do(func() { close(s.entered) })
		<-s.gate
	}
	return nil
}

// A feed poll must not queue behind a writer holding the service lock: the
// store read, the decode, and the grant walk all run without it. (The shown
// marks are a write and still wait their turn, which this test leaves
// unexercised: shown is empty.)
func TestBotFeedDoesNotWaitForWriter(t *testing.T) {
	settings := &stallSettings{gate: make(chan struct{}), entered: make(chan struct{})}
	svc := NewService(settings)
	ctx := context.Background()
	if _, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}

	settings.mu.Lock()
	settings.armed = true
	settings.mu.Unlock()
	writerDone := make(chan error, 1)
	go func() {
		_, err := svc.CreateGrant(ctx, "tenant-a", Grant{Scope: ScopeUser, TargetID: "bob"})
		writerDone <- err
	}()
	select {
	case <-settings.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not reach the store")
	}

	feedDone := make(chan error, 1)
	go func() {
		_, err := svc.BotFeed(ctx, "tenant-a", "alice", nil, false)
		feedDone <- err
	}()
	select {
	case err := <-feedDone:
		if err != nil {
			t.Fatalf("feed behind a slow writer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("feed poll queued behind a slow writer")
	}

	close(settings.gate)
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
}
