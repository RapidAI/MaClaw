package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// leaseClock is a settable clock for the screen lease.
type leaseClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *leaseClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *leaseClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func init() { desktopScreenLeaseOn = false }

func leaseTestEnv(t *testing.T) (*screenLeases, *leaseClock) {
	t.Helper()
	desktopScreenLeaseOn = true
	t.Cleanup(func() { desktopScreenLeaseOn = false })
	clock := &leaseClock{now: time.Date(2026, 11, 12, 6, 30, 0, 0, time.UTC)}
	oldNow, oldTick := screenLeaseNow, screenLeaseTick
	oldIdle, oldHold, oldStale, oldWait := screenLeaseIdle, screenLeaseMaxHold, screenLeaseStale, screenLeaseMaxWait
	screenLeaseNow, screenLeaseTick = clock.Now, 5*time.Millisecond
	screenLeaseIdle, screenLeaseMaxHold, screenLeaseStale, screenLeaseMaxWait = 2*time.Minute, 15*time.Minute, 20*time.Minute, 30*time.Minute
	t.Cleanup(func() {
		screenLeaseNow, screenLeaseTick = oldNow, oldTick
		screenLeaseIdle, screenLeaseMaxHold, screenLeaseStale, screenLeaseMaxWait = oldIdle, oldHold, oldStale, oldWait
	})
	return &screenLeases{leases: map[string]*screenLease{}, waiting: map[string]string{}}, clock
}

type leaseResult struct {
	release func()
	note    string
	err     error
}

func leaseAsync(m *screenLeases, ctx context.Context, key, run string) (<-chan leaseResult, <-chan struct{}) {
	out := make(chan leaseResult, 1)
	waiting := make(chan struct{}, 1)
	go func() {
		rel, _, note, err := m.acquire(ctx, key, run, func(string, int) { waiting <- struct{}{} })
		out <- leaseResult{rel, note, err}
	}()
	return out, waiting
}

func leaseExpectBlocked(t *testing.T, ch <-chan leaseResult) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("got the screen while another run holds it: %+v", r)
	case <-time.After(40 * time.Millisecond):
	}
}

func leaseExpect(t *testing.T, ch <-chan leaseResult) leaseResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("never got the screen")
	}
	return leaseResult{}
}

func TestScreenLeaseClassifiesSteps(t *testing.T) {
	for _, a := range []string{"screenshot", "click", "type", "navigate", "probe", "app_run", "computer", "batch", "publish", "script", "window", "rich_fill", "someday_new_action"} {
		if !desktopScreenLeaseNeeded(a, nil) {
			t.Errorf("%s needs the screen", a)
		}
	}
	for _, a := range []string{"bash", "open_screen", "file_read", "file_write", "file_edit", "file_list", "install", "deliver", "job_status", "goal_check", "say"} {
		if desktopScreenLeaseNeeded(a, map[string]any{"command": "python3 train.py"}) {
			t.Errorf("%s does not need the screen", a)
		}
	}
	if !desktopScreenLeaseNeeded("bash", map[string]any{"command": "libreoffice report.odt &"}) && desktopBashLaunchesGUI("libreoffice report.odt &") {
		t.Error("a bash that opens a GUI program needs the screen")
	}
}

// The lease is the run's across its screen steps; another run waits in FIFO
// order and gets it when the holder ends.
func TestScreenLeaseHoldsAcrossStepsAndQueuesFIFO(t *testing.T) {
	m, _ := leaseTestEnv(t)
	ctx := context.Background()
	relA, _, _, err := m.acquire(ctx, "k", "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	relA()
	// A's next step goes straight through (no other run waits yet).
	relA2, waited, _, err := m.acquire(ctx, "k", "A", nil)
	if err != nil || waited != 0 {
		t.Fatalf("holder's next step must not wait: %v %v", waited, err)
	}
	bCh, bWait := leaseAsync(m, ctx, "k", "B")
	<-bWait
	cCh, cWait := leaseAsync(m, ctx, "k", "C")
	<-cWait
	leaseExpectBlocked(t, bCh)
	if holder, pos, ok := m.waitingFor("C"); !ok || holder != "A" || pos != 2 {
		t.Fatalf("C waiting: holder=%q pos=%d ok=%v", holder, pos, ok)
	}
	relA2()
	// A just used the screen: a short gap between A's steps keeps it A's.
	relA3, waited, _, _ := m.acquire(ctx, "k", "A", nil)
	if waited != 0 {
		t.Fatal("holder mid-flow must keep the screen")
	}
	relA3()
	leaseExpectBlocked(t, bCh)
	m.endRun("A", "run_end")
	b := leaseExpect(t, bCh)
	if b.err != nil || !strings.Contains(b.note, "another task") {
		t.Fatalf("B: %+v", b)
	}
	leaseExpectBlocked(t, cCh)
	b.release()
	m.endRun("B", "run_end")
	c := leaseExpect(t, cCh)
	if c.err != nil {
		t.Fatal(c.err)
	}
	c.release()
	m.endRun("C", "run_end")
	if len(m.leases) != 0 || len(m.waiting) != 0 {
		t.Fatalf("state left behind: %d leases %d waiting", len(m.leases), len(m.waiting))
	}
}

// A holder doing non-screen work (or thinking) past the idle time yields to a
// waiter; when it needs the screen again it queues and is told to look again.
func TestScreenLeasePassesOnWhenHolderIdle(t *testing.T) {
	m, clock := leaseTestEnv(t)
	ctx := context.Background()
	relA, _, _, _ := m.acquire(ctx, "k", "A", nil)
	relA()
	bCh, bWait := leaseAsync(m, ctx, "k", "B")
	<-bWait
	clock.Add(30 * time.Second)
	leaseExpectBlocked(t, bCh)
	clock.Add(2 * time.Minute)
	b := leaseExpect(t, bCh)
	if b.err != nil {
		t.Fatal(b.err)
	}
	aCh, aWait := leaseAsync(m, ctx, "k", "A")
	<-aWait
	b.release()
	clock.Add(3 * time.Minute)
	a := leaseExpect(t, aCh)
	if a.err != nil || !strings.Contains(a.note, "check the screen") {
		t.Fatalf("A back: %+v", a)
	}
	a.release()
}

// A run that keeps stepping is passed over at max hold, but only between steps.
func TestScreenLeaseMaxHoldOnlyBetweenSteps(t *testing.T) {
	m, clock := leaseTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	relA, _, _, _ := m.acquire(ctx, "k", "A", nil)
	bCh, bWait := leaseAsync(m, ctx, "k", "B")
	<-bWait
	for i := 0; i < 14; i++ {
		clock.Add(time.Minute)
		relA()
		var waited time.Duration
		relA, waited, _, _ = m.acquire(ctx, "k", "A", nil)
		if waited != 0 {
			t.Fatal("holder stepping steadily keeps the screen before max hold")
		}
	}
	clock.Add(2 * time.Minute)
	// 16 minutes held, a step in flight: not taken mid-step.
	leaseExpectBlocked(t, bCh)
	relA()
	b := leaseExpect(t, bCh)
	if b.err != nil {
		t.Fatal(b.err)
	}
	b.release()
}

// A step that hangs (or a run that crashed mid-step) does not keep the screen
// forever; its late release does not touch the new holder.
func TestScreenLeaseStaleStepRecovered(t *testing.T) {
	m, clock := leaseTestEnv(t)
	ctx := context.Background()
	relA, _, _, _ := m.acquire(ctx, "k", "A", nil)
	bCh, bWait := leaseAsync(m, ctx, "k", "B")
	<-bWait
	clock.Add(19 * time.Minute)
	leaseExpectBlocked(t, bCh)
	clock.Add(2 * time.Minute)
	b := leaseExpect(t, bCh)
	if b.err != nil {
		t.Fatal(b.err)
	}
	relA() // the hung step finally returns
	m.mu.Lock()
	holder, inflight := m.leases["k"].holder, m.leases["k"].inflight
	m.mu.Unlock()
	if holder != "B" || inflight != 1 {
		t.Fatalf("late release changed the new holder: %s %d", holder, inflight)
	}
	b.release()
}

// Cancel while waiting, failure of the holder's step, and the end of a
// waiting run all leave the queue clean.
func TestScreenLeaseCancelAndFailure(t *testing.T) {
	m, clock := leaseTestEnv(t)
	relA, _, _, _ := m.acquire(context.Background(), "k", "A", nil)
	ctx, cancel := context.WithCancel(context.Background())
	bCh, bWait := leaseAsync(m, ctx, "k", "B")
	<-bWait
	cancel()
	if r := leaseExpect(t, bCh); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("cancelled wait: %+v", r)
	}
	if _, _, ok := m.waitingFor("B"); ok {
		t.Fatal("cancelled waiter still queued")
	}
	// C waits; its run ends (failed) before it gets the screen.
	cCh, cWait := leaseAsync(m, context.Background(), "k", "C")
	<-cWait
	m.endRun("C", "run_end")
	if r := leaseExpect(t, cCh); r.err == nil || !strings.Contains(r.err.Error(), "screen_busy") {
		t.Fatalf("ended waiter: %+v", r)
	}
	// The holder's step fails: the release still runs (deferred by the caller),
	// and the run's end frees the screen at once for D.
	dCh, dWait := leaseAsync(m, context.Background(), "k", "D")
	<-dWait
	relA()
	relA() // idempotent
	m.endRun("A", "run_end")
	d := leaseExpect(t, dCh)
	if d.err != nil {
		t.Fatal(d.err)
	}
	d.release()
	// Wait limit: E gives up with an error the model can act on (D's step
	// is long but not yet stale).
	screenLeaseStale = time.Hour
	eCh, eWait := leaseAsync(m, context.Background(), "k", "E")
	<-eWait
	relD, _, _, _ := m.acquire(context.Background(), "k", "D", nil)
	if q := desktopScreenQueue("E"); q != desktopScreenQueueSentence {
		t.Fatalf("queue before timeout %q", q)
	}
	clock.Add(31 * time.Minute)
	if r := leaseExpect(t, eCh); r.err == nil || !strings.Contains(r.err.Error(), "screen_busy") || !strings.Contains(r.err.Error(), desktopScreenQueueSentence) {
		t.Fatalf("wait limit: %+v", r)
	}
	if _, ok := desktopScreenHalts.Load("E"); !ok {
		t.Fatal("timeout did not end the run")
	}
	relD()
}

func TestScreenLeaseWaitersAreFIFO(t *testing.T) {
	m, _ := leaseTestEnv(t)
	relA, _, _, err := m.acquire(context.Background(), "k", "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	bCh, bWait := leaseAsync(m, context.Background(), "k", "B")
	<-bWait
	cCh, cWait := leaseAsync(m, context.Background(), "k", "C")
	<-cWait
	relA()
	m.endRun("A", "run_end")
	b := leaseExpect(t, bCh)
	if b.err != nil {
		t.Fatal(b.err)
	}
	select {
	case got := <-cCh:
		t.Fatalf("C passed B: %+v", got)
	default:
	}
	b.release()
	m.endRun("B", "run_end")
	c := leaseExpect(t, cCh)
	if c.err != nil {
		t.Fatal(c.err)
	}
	c.release()
}

// Many runs hammering one screen: never two holders inside a step at once.
func TestScreenLeaseConcurrentMutualExclusion(t *testing.T) {
	m, _ := leaseTestEnv(t)
	screenLeaseIdle = 0 // pass on at every gap so runs interleave a lot
	var inside, maxInside, steps int32
	var wg sync.WaitGroup
	for r := 0; r < 6; r++ {
		run := fmt.Sprintf("R%d", r)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				rel, _, _, err := m.acquire(context.Background(), "k", run, nil)
				if err != nil {
					t.Error(err)
					return
				}
				n := atomic.AddInt32(&inside, 1)
				for {
					old := atomic.LoadInt32(&maxInside)
					if n <= old || atomic.CompareAndSwapInt32(&maxInside, old, n) {
						break
					}
				}
				time.Sleep(50 * time.Microsecond)
				atomic.AddInt32(&inside, -1)
				atomic.AddInt32(&steps, 1)
				rel()
			}
			m.endRun(run, "run_end")
		}()
	}
	wg.Wait()
	if maxInside != 1 || steps != 180 {
		t.Fatalf("max runs inside a screen step = %d, steps = %d", maxInside, steps)
	}
}

// Through the tool: two runs of one bot. The second run's screen step waits
// (status line says why) while its bash runs at once; each run's bash has
// its own TMPDIR.
func TestScreenLeaseThroughInvokeTool(t *testing.T) {
	recoveryTestEnv(t)
	t.Setenv("MACLAW_DESKTOP_TIDY", "off")
	oldCtl := desktopRemoteCtl
	desktopRemoteCtl = func(context.Context, string, string, string, map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	t.Cleanup(func() { desktopRemoteCtl = oldCtl })
	_, _ = leaseTestEnv(t)
	user := desktop.BotDesktopUser("daniel", fmt.Sprintf("bot_ls%d", time.Now().UnixNano()))
	runA := fmt.Sprintf("run_lsA%d", time.Now().UnixNano())
	runB := fmt.Sprintf("run_lsB%d", time.Now().UnixNano())
	reqA := recoveryTurn("tenant-ls", user, runA, "打开网页截图")
	reqB := recoveryTurn("tenant-ls", user, runB, "训练并打开浏览器")
	t.Cleanup(func() { desktopScreenLeaseEndRun(runA, "test"); desktopScreenLeaseEndRun(runB, "test") })
	release := make(chan struct{})
	entered := make(chan string, 4)
	var mu sync.Mutex
	commands := map[string]string{}
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		action := desktopArg(args, "action")
		if action == "bash" {
			mu.Lock()
			commands[scope.RunID] = desktopArg(args, "command")
			mu.Unlock()
			return "ok", nil
		}
		entered <- scope.RunID
		if scope.RunID == runA {
			<-release
		}
		return "screen ok", nil
	}
	aDone := make(chan error, 1)
	go func() {
		_, err := desktopRuntimeModule{}.InvokeTool(context.Background(), reqA, desktopToolName, map[string]any{"action": "probe"})
		aDone <- err
	}()
	if got := <-entered; got != runA {
		t.Fatalf("first in: %s", got)
	}
	bDone := make(chan string, 1)
	go func() {
		out, err := desktopRuntimeModule{}.InvokeTool(context.Background(), reqB, desktopToolName, map[string]any{"action": "probe"})
		if err != nil {
			out = "ERR " + err.Error()
		}
		bDone <- out
	}()
	deadline := time.Now().Add(2 * time.Second)
	for desktopScreenLeaseStatus(runB) == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := desktopScreenLeaseStatus(runB); !strings.Contains(st, "等待屏幕：另一任务正在使用") {
		t.Fatalf("status line: %q", st)
	}
	if act := loadDesktopActivity(runB); act != desktopScreenWaitPhrase {
		t.Fatalf("working row: %q", act)
	}
	// B's non-screen work is not blocked by A's screen step.
	mod := desktopRuntimeModule{}
	if _, err := mod.InvokeTool(context.Background(), reqB, desktopToolName, map[string]any{"action": "bash", "command": "python3 gen.py"}); err != nil {
		t.Fatalf("bash while waiting for the screen: %v", err)
	}
	select {
	case got := <-entered:
		t.Fatalf("%s entered the screen while A holds it", got)
	default:
	}
	close(release)
	if err := <-aDone; err != nil {
		t.Fatal(err)
	}
	desktopScreenLeaseEndRun(runA, "run_end")
	if got := <-entered; got != runB {
		t.Fatalf("second in: %s", got)
	}
	if out := <-bDone; !strings.Contains(out, "screen ok") || !strings.Contains(out, "another task of this bot") {
		t.Fatalf("B's screen step: %q", out)
	}
	if act := loadDesktopActivity(runB); act == desktopScreenWaitPhrase {
		t.Fatal("working row still says waiting")
	}
	// Each run's bash got its own TMPDIR (via the body: operateDesktopFunc is
	// replaced here, so check the scratch folders the body would use).
	a := desktopBotScratchDir(reqA.Scope)
	b := desktopBotScratchDir(reqB.Scope)
	if a == b || !strings.HasPrefix(a, "/tmp/maclaw-bot_ls") {
		t.Fatalf("scratch folders: %q %q", a, b)
	}
	_ = commands
}

// The body gives each run's bash its own TMPDIR on the local container path.
func TestScratchPerRunThroughBody(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_TIDY", "off")
	oldBash, oldSession, oldCtl := desktopRemoteBash, desktopRemoteSession, desktopRemoteCtl
	var mu sync.Mutex
	var cmds []string
	desktopRemoteBash = func(_ context.Context, _, _, command string) (string, error) {
		mu.Lock()
		cmds = append(cmds, command)
		mu.Unlock()
		return "ok", nil
	}
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:9", Display: ":99"}, nil
	}
	desktopRemoteCtl = func(context.Context, string, string, string, map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	t.Cleanup(func() { desktopRemoteBash, desktopRemoteSession, desktopRemoteCtl = oldBash, oldSession, oldCtl })
	user := desktop.BotDesktopUser("daniel", "bot_scr")
	runs := []string{"run_scrA1", "run_scrB2"}
	for _, run := range runs {
		scope := agentruntime.Scope{TenantID: "tenant-scr", UserID: user, RunID: run}
		out, err := operateDesktopBody(context.Background(), scope, map[string]any{"action": "bash", "command": "python3 gen.py > report.html"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "/tmp/maclaw-bot_scr/run-"+strings.TrimPrefix(run, "run_")) {
			t.Fatalf("note: %q", out)
		}
		finishDesktopRound("tenant-scr", user, run)
	}
	if len(cmds) != 2 || !strings.Contains(cmds[0], "export TMPDIR=/tmp/maclaw-bot_scr/run-scrA1;") || !strings.Contains(cmds[1], "export TMPDIR=/tmp/maclaw-bot_scr/run-scrB2;") {
		t.Fatalf("commands: %q", cmds)
	}
}
