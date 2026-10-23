package botmgmt

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeAdmits struct {
	mu   sync.Mutex
	got  []string // bot|key|content
	busy int
}

func (f *fakeAdmits) admit(ctx context.Context, tenantID, userID, botID, content, phase, key string) (BotAdmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy > 0 {
		f.busy--
		return BotAdmission{}, ErrDesktopBusy
	}
	f.got = append(f.got, botID+"|"+key+"|"+content)
	return BotAdmission{Accepted: true, RunID: "run_" + key}, nil
}

func (f *fakeAdmits) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func dispatchFixture(t *testing.T) (*Service, *fakeAdmits) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_pub", Name: "发布员", OwnerUserID: "alice", InstanceID: "inst_pub", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	f := &fakeAdmits{}
	svc.BotDispatch = true
	svc.dispatchAdmit = f.admit
	return svc, f
}

// GUI closed: the server-side main bot delegates, Hub starts the teammate,
// the run end lands in main's mailbox and wakes main server-side.
func TestHubRunsCrossBotTaskWithoutGUI(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "发布员", Text: "导出本月订单", Key: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "发布员", Text: "导出本月订单", Key: "k1"})
	if err != nil || again.ID != d.ID || !again.Duplicate {
		t.Fatalf("idempotent delegate: %+v %v", again, err)
	}
	waitFor(t, func() bool { return len(f.list()) == 1 })
	if got := f.list()[0]; got != "bot_pub|xdel-"+d.ID+"|导出本月订单" {
		t.Fatalf("admit = %q", got)
	}
	// The GUI may not start it a second time.
	if _, claimed, _ := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_pub", d.ID); claimed {
		t.Fatal("GUI won a claim Hub already started")
	}
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		n := findCrossNote(notes, d.ID)
		return n != nil && n.RunID == "run_xdel-"+d.ID
	})
	svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_xdel-"+d.ID, "succeeded", Reply{Text: "导出好了"}, nil)
	waitFor(t, func() bool { return len(f.list()) == 2 })
	if got := f.list()[1]; got[:len("bot_main|xwake-")] != "bot_main|xwake-" {
		t.Fatalf("main not woken: %q", got)
	}
	// One message wakes once.
	svc.kickDispatch("tenant-a", "alice")
	time.Sleep(100 * time.Millisecond)
	if n := len(f.list()); n != 2 {
		t.Fatalf("woke again: %v", f.list())
	}
}

// GUI open: the main bot's mail is left to the GUI; a peer is still woken.
func TestHubLeavesMainWakeToLiveGUIAndRetriesBusyDesktop(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	if _, err := svc.BotMailbox(ctx, "tenant-a", "alice", "main", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_pub", BotCrossNote{Topic: "done", Text: "好了"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if len(f.list()) != 0 {
		t.Fatalf("woke main while its GUI reads: %v", f.list())
	}
	f.mu.Lock()
	f.busy = 1
	f.mu.Unlock()
	if _, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", "bot_main", BotCrossNote{Kind: "report", Target: "bot_pub", Topic: "reply", Text: "用第一个"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
	for _, n := range notes {
		if n.Target == "bot_pub" && n.WokenAt != "" {
			t.Fatal("busy wake was not undone")
		}
	}
	svc.kickDispatch("tenant-a", "alice") // the run-end kick
	waitFor(t, func() bool { return len(f.list()) == 1 })
}

func TestHubHoldsDestructiveDelegateForThePerson(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", "bot_main", BotCrossNote{Kind: "delegate", Target: "bot_pub", Text: "删除旧订单"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		n := findCrossNote(notes, d.ID)
		return n != nil && n.HeldAt != ""
	})
	for _, got := range f.list() {
		if got[:len("bot_pub")] == "bot_pub" {
			t.Fatalf("destructive order started: %v", got)
		}
	}
	if _, claimed, _ := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_pub", d.ID); !claimed {
		t.Fatal("the person's GUI must still be able to start it after confirming")
	}
}

func TestReportIdempotencyKey(t *testing.T) {
	svc, _ := dispatchFixture(t)
	svc.BotDispatch = false
	ctx := context.Background()
	a, _ := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_pub", BotCrossNote{Topic: "progress", Text: "50%", Key: "run1:a"})
	b, _ := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_pub", BotCrossNote{Topic: "progress", Text: "50% again", Key: "run1:a"})
	if a.ID == "" || b.ID != a.ID || !b.Duplicate {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

// A delegate saved while dispatch was off (a Hub that then restarted) is
// started by the boot scan of the persisted roster on the next Hub.
func TestHubBootScanResumesPendingDelegate(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	svc.kickDispatch("tenant-a", "alice") // puts alice on the roster
	waitFor(t, func() bool { return len(svc.loadDispatchRoster(ctx)) == 1 })
	svc.BotDispatch = false
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "发布员", Text: "整理文件", Key: "boot"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(f.list()) != 0 {
		t.Fatal("started with dispatch off")
	}
	// "Restart": a new Service over the same store.
	next := &Service{System: svc.System, Directory: svc.Directory, Desktop: svc.Desktop, Now: svc.Now, BotDispatch: true, dispatchAdmit: f.admit}
	if n := next.ResumeBotDispatch(ctx); n != 1 {
		t.Fatalf("resumed %d users", n)
	}
	waitFor(t, func() bool { return len(f.list()) == 1 })
	if got := f.list()[0]; got != "bot_pub|xdel-"+d.ID+"|整理文件" {
		t.Fatalf("admit = %q", got)
	}
}
