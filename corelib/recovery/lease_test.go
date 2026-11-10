package recovery

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Separate Store instances on one root (as separate processes would have)
// racing for the same goal: exactly one attempt is leased.
func TestAcquireIsExclusiveAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := New(DefaultConfig(), NewStore(root))
			if l, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish", Consequential: true}); err == nil && l != nil {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("%d attempts leased at once", won.Load())
	}
	e := New(DefaultConfig(), NewStore(root))
	if g := e.Status(sc, "g"); g.Count != 1 || g.Lease == nil {
		t.Fatalf("reserved %d, lease %v", g.Count, g.Lease)
	}
}

// Real separate processes (this test binary re-run as a helper).
func TestAcquireIsExclusiveAcrossProcesses(t *testing.T) {
	if os.Getenv("RECOVERY_HELPER_ROOT") != "" {
		t.Skip("helper")
	}
	root := t.TempDir()
	var outs []*exec.Cmd
	var bufs []*strings.Builder
	for i := 0; i < 6; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperAcquire$")
		cmd.Env = append(os.Environ(), "RECOVERY_HELPER_ROOT="+root)
		b := &strings.Builder{}
		cmd.Stdout, cmd.Stderr = b, b
		if err := cmd.Start(); err != nil {
			t.Skipf("cannot spawn: %v", err)
		}
		outs, bufs = append(outs, cmd), append(bufs, b)
	}
	won := 0
	for i, c := range outs {
		_ = c.Wait()
		if strings.Contains(bufs[i].String(), "LEASED") {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d processes leased the same goal", won)
	}
	files, _ := filepath.Glob(filepath.Join(root, "*.json"))
	for _, f := range files {
		if _, err := readScope(f); err != nil {
			t.Fatalf("ledger corrupt after concurrent writers: %v", err)
		}
	}
}

func TestHelperAcquire(t *testing.T) {
	root := os.Getenv("RECOVERY_HELPER_ROOT")
	if root == "" {
		t.Skip("only as a helper process")
	}
	e := New(DefaultConfig(), NewStore(root))
	if l, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish", Consequential: true}); err == nil && l != nil {
		fmt.Println("LEASED")
	} else {
		fmt.Println("REFUSED", err)
	}
}

func TestDeadHolderBeforeAndAfterSideEffect(t *testing.T) {
	e, c := newEngine(t, DefaultConfig())
	l, _, err := e.Acquire(sc, AttemptRequest{Goal: "pre", Method: "publish", Consequential: true})
	if err != nil {
		t.Fatal(err)
	}
	// While the holder beats, nobody else gets the goal, however long it runs.
	for i := 0; i < 5; i++ {
		c.t = c.t.Add(time.Minute)
		if err := e.Beat(sc, "pre", l.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.Acquire(sc, AttemptRequest{Goal: "pre", Method: "publish"}); err == nil {
			t.Fatal("second attempt while the first is alive")
		}
	}
	// Holder dies before any side effect: reclaimed as interrupted.
	c.t = c.t.Add(e.Cfg.LeaseTTL + time.Second)
	l2, g, err := e.Acquire(sc, AttemptRequest{Goal: "pre", Method: "publish"})
	if err != nil || l2 == nil || g.Count != 2 || !strings.Contains(g.Attempts[len(g.Attempts)-1].Error, "interrupted before any side effect") {
		t.Fatalf("pre-side-effect crash not reclaimed: %v %+v", err, g)
	}
	// The dead holder's late result and its side-effect handshake are refused.
	if err := e.SideEffect(sc, "pre", l.ID); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lost lease allowed a side effect: %v", err)
	}
	if v := e.Finish(sc, l.ID, Observation{Goal: "pre", OK: true, Complete: true}); v.Kind != KindStale || e.Status(sc, "pre").State == StateDone {
		t.Fatalf("stale result recorded: %+v", v)
	}
	// After the side effect began, a dead holder stops the goal for good.
	l3, _, _ := e.Acquire(sc, AttemptRequest{Goal: "post", Method: "publish", Consequential: true})
	if err := e.SideEffect(sc, "post", l3.ID); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(e.Cfg.LeaseTTL + time.Second)
	if _, _, err := e.Acquire(sc, AttemptRequest{Goal: "post", Method: "publish"}); err == nil || !strings.Contains(err.Error(), "never repeated") {
		t.Fatalf("post-side-effect crash retried: %v", err)
	}
	if g := e.Status(sc, "post"); g.State != StateStopped {
		t.Fatalf("state %s", g.State)
	}
	// A stopped goal does not expire away with the TTL.
	c.t = c.t.Add(e.Cfg.TTL * 3)
	if g := e.Status(sc, "post"); g == nil || g.State != StateStopped {
		t.Fatal("unconfirmed lockout expired")
	}
}

func TestAbandonGivesBackTheReservedAttempt(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	l, _, _ := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish"})
	e.Abandon(sc, "g", l.ID)
	if g := e.Status(sc, "g"); g.Count != 0 || g.Lease != nil {
		t.Fatalf("%+v", g)
	}
	l, _, _ = e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish"})
	_ = e.SideEffect(sc, "g", l.ID)
	e.Abandon(sc, "g", l.ID) // after a side effect: not abandonable
	if g := e.Status(sc, "g"); g.Lease == nil {
		t.Fatal("abandoned after a side effect")
	}
}

func TestCorruptLedgerFailsClosedAndIsNotOverwritten(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	e.Observe(sc, obs("g", "", false, "x", nil))
	p := e.Store.path(sc)
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish"}); !errors.Is(err, ErrLedger) {
		t.Fatalf("acquire on a corrupt ledger: %v", err)
	}
	if err := e.Gate(sc, "g", "check", ""); !errors.Is(err, ErrLedger) {
		t.Fatalf("gate on a corrupt ledger: %v", err)
	}
	if raw, _ := os.ReadFile(p); string(raw) != "{not json" {
		t.Fatal("corrupt ledger overwritten")
	}
}

func TestLineageKeepsOneTaskAcrossContinuationsAndStartsNewForNewInstruction(t *testing.T) {
	e, _ := newEngine(t, DefaultConfig())
	first, _ := e.Lineage(sc, "sess-1", "把这篇文章连同 4 张图发到 CSDN，标题用 VFlash")
	for _, m := range []string{"继续", "再试一次", "好的", "把这篇文章连同 4 张图发到 CSDN，标题用 VFlash", "ok go on please continue"} {
		if got, _ := e.Lineage(sc, "sess-1", m); got != first {
			t.Fatalf("%q started a new task: %s vs %s", m, got, first)
		}
	}
	second, _ := e.Lineage(sc, "sess-1", "另外写一篇关于 Rust 异步运行时的文章发到知乎")
	if second == first {
		t.Fatal("a new instruction stayed in the old task")
	}
	if other, _ := e.Lineage(sc, "sess-2", "继续"); other == first || other == second {
		t.Fatal("conversations share a lineage")
	}
	if bob, _ := e.Lineage("tenant\x00bob", "sess-1", "继续"); bob == second {
		// same conversation id under another person is still separate data
		if g := e.Status("tenant\x00bob", "x"); g != nil {
			t.Fatal("cross-scope data")
		}
	}
}

func TestSpecPinRefusesLoweringAndSpanBudget(t *testing.T) {
	e, c := newEngine(t, DefaultConfig())
	check := func(pinned, spec string) error {
		if spec < pinned {
			return fmt.Errorf("fewer pictures")
		}
		return nil
	}
	l, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish", Spec: "4", Check: check})
	if err != nil {
		t.Fatal(err)
	}
	e.Finish(sc, l.ID, Observation{Goal: "g", Method: "publish", Error: "x"})
	if _, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish", Strategy: "a=1;", Spec: "2", Check: check}); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("lowered requirement accepted: %v", err)
	}
	c.t = c.t.Add(e.Cfg.MaxSpan + time.Minute)
	if _, _, err := e.Acquire(sc, AttemptRequest{Goal: "g", Method: "publish", Strategy: "b=1;", Spec: "4", Check: check}); err == nil || !strings.Contains(err.Error(), "since the first attempt") {
		t.Fatalf("span budget not enforced: %v", err)
	}
}
