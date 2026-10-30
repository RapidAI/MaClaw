package botmgmt

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type resumeDesktopFake struct {
	mu      sync.Mutex
	replies []map[string]any
	calls   int
}

func (f *resumeDesktopFake) Open(context.Context, string, string) (string, error) { return "", nil }
func (f *resumeDesktopFake) Stop(context.Context, string, string) error           { return nil }
func (f *resumeDesktopFake) Ctl(_ context.Context, _, _ string, req map[string]any) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, _ := req["watch"].(bool); !w || req["op"] != desktop.CtlBotState {
		panic("watcher must only send bot_state watch")
	}
	f.calls++
	if len(f.replies) == 0 {
		return map[string]any{"takeover": false}, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

// An automatic hand back (signed in) resumes the run exactly once, with the
// continuation the 交还 button sends; a person's own hand back does not.
func TestHubResumesRunAfterAutoHandBack(t *testing.T) {
	oldEvery, oldAdmit := desktopResumeEvery, desktopResumeAdmit
	defer func() { desktopResumeEvery, desktopResumeAdmit = oldEvery, oldAdmit }()
	desktopResumeEvery = time.Millisecond
	desktopResumeWatches.mu.Lock()
	desktopResumeWatches.resumed = map[string]time.Time{}
	desktopResumeWatches.mu.Unlock()
	type admit struct{ user, bot, msg, phase, key string }
	got := make(chan admit, 4)
	desktopResumeAdmit = func(_ *Service, _ context.Context, tenantID, userID, botID, content, phase, key string) error {
		got <- admit{userID, botID, content, phase, key}
		return nil
	}
	resume := map[string]any{"takeover": false, "resume": map[string]any{"since": float64(1761867000), "trigger": "signed_in"}}
	fake := &resumeDesktopFake{replies: []map[string]any{{"takeover": true}, resume, resume}}
	s := &Service{Desktop: fake}
	user := desktop.BotDesktopUser("u1", "bot_abc")
	s.NoteDesktopCtl("t", user, map[string]any{"op": desktop.CtlBotState}) // not a begin: no watcher
	s.NoteDesktopCtl("t", user, map[string]any{"op": desktop.CtlTakeoverBegin})
	s.NoteDesktopCtl("t", user, map[string]any{"op": desktop.CtlTakeoverBegin}) // one watcher per display
	select {
	case a := <-got:
		if a.user != "u1" || a.bot != "bot_abc" || a.phase != "execute" || a.key != "handback-bot_abc-1761867000" {
			t.Fatalf("admit %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no continuation")
	}
	// The same takeover reported again (a second watcher) never doubles it.
	s.resumeAfterHandBack("t", user, "t\x00"+user, map[string]any{"since": float64(1761867000), "trigger": "signed_in"})
	select {
	case a := <-got:
		t.Fatalf("second continuation %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
	// Person hands back: takeover closes without a resume, nothing admitted.
	fake2 := &resumeDesktopFake{replies: []map[string]any{{"takeover": true}, {"takeover": false, "resume": nil}}}
	s2 := &Service{Desktop: fake2}
	s2.NoteDesktopCtl("t", desktop.BotDesktopUser("u1", "bot_xyz"), map[string]any{"op": desktop.CtlTakeoverBegin})
	select {
	case a := <-got:
		t.Fatalf("manual hand back must not resume twice: %+v", a)
	case <-time.After(100 * time.Millisecond):
	}
	// The owner's own desktop has no takeover state: no watcher.
	s.NoteDesktopCtl("t", "u1", map[string]any{"op": desktop.CtlTakeoverBegin})
}
