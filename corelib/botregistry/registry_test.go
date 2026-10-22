package botregistry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixed() func() time.Time {
	t0 := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	return func() time.Time { return t0 }
}

func newReg() *Registry {
	r := New()
	r.SetClock(fixed())
	r.SetSelf("local:main")
	r.Upsert(Bot{ID: "local:main", Name: "主Bot", Primary: true, Location: LocationLocal, Capabilities: append(LocalCapabilities(), CapCoordinator)})
	r.Upsert(Bot{ID: "bot_2", Name: "发布员", Location: LocationRemote, Capabilities: RemoteCapabilities()})
	return r
}

func kinds(evs []Event) []EventKind {
	out := []EventKind{}
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func TestListPutsSelfFirstAndCopies(t *testing.T) {
	r := newReg()
	bots := r.List()
	if len(bots) != 2 || bots[0].ID != "local:main" || !bots[0].Self {
		t.Fatalf("list = %+v", bots)
	}
	bots[0].Capabilities[0] = "hacked"
	if b, _ := r.Get("local:main"); b.Capabilities[0] != CapFileRead {
		t.Fatal("List leaked internal slice")
	}
}

func TestTransitionsRaiseEvents(t *testing.T) {
	r := newReg()
	start := r.LastSeq()
	b, _ := r.Get("bot_2")
	ApplyRuns(&b, []RunView{{RunID: "run_1", Status: "running", Task: "发文章", Step: "打开后台"}})
	r.Upsert(b)
	ApplyRuns(&b, []RunView{{RunID: "run_1", Status: "running", Task: "发文章", WaitingForUser: true, Step: "需要扫码登录"}})
	r.Upsert(b)
	ApplyRuns(&b, []RunView{{RunID: "run_1", Status: "running", Task: "发文章", Step: "发布中"}})
	r.Upsert(b)
	ApplyRuns(&b, nil)
	r.Upsert(b)
	got := kinds(r.Events(start))
	want := []EventKind{EventStarted, EventNeedsUser, EventStarted, EventFinished}
	if len(got) != len(want) {
		t.Fatalf("events = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	last := r.Events(start)[3]
	if last.RunID != "run_1" || last.Task != "发文章" {
		t.Fatalf("finished event lost run/task: %+v", last)
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	r := newReg()
	var seen []Event
	stop := r.Subscribe(func(e Event) { seen = append(seen, e) })
	r.Notify(EventMessage, "bot_2", "hi")
	stop()
	r.Notify(EventMessage, "bot_2", "again")
	if len(seen) != 1 || seen[0].Detail != "hi" {
		t.Fatalf("seen = %+v", seen)
	}
}

func TestPauseHoldResume(t *testing.T) {
	r := newReg()
	if r.Hold("bot_2", "x") {
		t.Fatal("held while not paused")
	}
	r.Pause("bot_2")
	if b, _ := r.Get("bot_2"); b.State != StatePaused {
		t.Fatalf("state = %s", b.State)
	}
	// A refresh from Hub keeps the pause.
	r.Upsert(Bot{ID: "bot_2", Name: "发布员", Location: LocationRemote, State: StateIdle})
	if !r.Hold("bot_2", "order 1") || !r.Hold("bot_2", "order 2") {
		t.Fatal("hold failed")
	}
	held, ok := r.Resume("bot_2")
	if !ok || len(held) != 2 || held[0] != "order 1" {
		t.Fatalf("held = %v", held)
	}
	if b, _ := r.Get("bot_2"); b.Paused || b.State != StateIdle {
		t.Fatalf("after resume: %+v", b)
	}
}

func TestResolve(t *testing.T) {
	r := newReg()
	if got, ok := r.Resolve("发布员"); !ok || got[0].ID != "bot_2" {
		t.Fatal("name")
	}
	if got, ok := r.Resolve("all"); !ok || len(got) != 1 {
		t.Fatal("all excludes self")
	}
	if got, ok := r.Resolve("self"); !ok || got[0].ID != "local:main" {
		t.Fatal("self")
	}
	r.Upsert(Bot{ID: "bot_3", Name: "发布员"})
	if _, ok := r.Resolve("发布员"); ok {
		t.Fatal("ambiguous name resolved")
	}
}

func TestPruneKeepsLocal(t *testing.T) {
	r := newReg()
	r.Prune(map[string]bool{})
	if _, ok := r.Get("bot_2"); ok {
		t.Fatal("remote bot not pruned")
	}
	if _, ok := r.Get("local:main"); !ok {
		t.Fatal("self pruned")
	}
}

func TestApplyRunsPriorities(t *testing.T) {
	b := Bot{}
	ApplyRuns(&b, []RunView{{Status: "queued"}, {Status: "queued"}})
	if b.State != StateQueued || b.Queue != 2 {
		t.Fatalf("%+v", b)
	}
	ApplyRuns(&b, []RunView{{RunID: "r", Status: "running", Stale: true}})
	if b.State != StateError || b.LastError == "" {
		t.Fatalf("%+v", b)
	}
}

func TestPolicy(t *testing.T) {
	main := Bot{ID: "m", Primary: true}
	other := Bot{ID: "o"}
	if d := Check(other, main, ActionStop, ""); d.Allowed {
		t.Fatal("non-primary controlled another bot")
	}
	if d := Check(other, other, ActionStop, ""); !d.Allowed {
		t.Fatal("bot cannot stop itself")
	}
	if d := Check(main, other, ActionAssign, "把这个文件删除"); !d.NeedsConfirm {
		t.Fatal("destructive assign not confirmed")
	}
	if d := Check(main, other, ActionAssign, "Publish the article to the blog"); !d.Allowed || d.NeedsConfirm {
		t.Fatalf("plain assign: %+v", d)
	}
	if d := Check(main, Bot{ID: "o", State: StateWaitingUser}, ActionStop, ""); !d.NeedsConfirm {
		t.Fatal("stop while waiting on person not confirmed")
	}
	if d := Check(Bot{}, other, ActionRead, ""); !d.Allowed {
		t.Fatal("read denied")
	}
}

func TestOutboundPath(t *testing.T) {
	for _, p := range []string{"/home/a/.ssh/config", "C:/Users/a/.env", "/x/id_rsa", "/x/server.pem", "/x/.aws/credentials"} {
		if CheckOutboundPath(p, 10) == nil {
			t.Fatalf("%s allowed", p)
		}
	}
	if err := CheckOutboundPath("/home/a/report.docx", 10); err != nil {
		t.Fatal(err)
	}
	if CheckOutboundPath("/home/a/big.zip", TransferMax+1) == nil {
		t.Fatal("too big allowed")
	}
}

func TestArtifacts(t *testing.T) {
	dir := t.TempDir()
	s, err := NewArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.PutBytes("../../evil/报告.txt", "text/plain", "bot_2", "run_1", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(a.Path)) != dir || a.Name != "报告.txt" || a.Size != 5 {
		t.Fatalf("%+v", a)
	}
	local := filepath.Join(dir, "in.csv")
	_ = os.WriteFile(local, []byte("a,b"), 0o600)
	b, err := s.PutFile(local, "local:main")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Get(b.ID); !ok || got.Path != local {
		t.Fatal("get")
	}
	if len(s.List()) != 2 {
		t.Fatal("list")
	}
	secret := filepath.Join(dir, ".env")
	_ = os.WriteFile(secret, []byte("K=V"), 0o600)
	if _, err := s.PutFile(secret, "x"); err == nil {
		t.Fatal("secret accepted")
	}
}
