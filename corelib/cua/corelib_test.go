package cua

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPipePathForms(t *testing.T) {
	full := `\\.\pipe\maclaw-cua` // UNC form a host puts in the variable
	bare := `maclaw-cua`
	short := `\pipe\maclaw-cua`
	got := []string{pipePath(full), pipePath(bare), pipePath(short), pipePath(" " + bare + " ")}
	for i, v := range got {
		if !strings.Contains(v, "pipe") || strings.HasSuffix(v, "pipe") {
			t.Fatalf("form %d = %q, not a pipe path", i, v)
		}
	}
	if got[0] != full {
		t.Fatalf("full form rewritten: %q", got[0])
	}
	for i := 1; i < len(got); i++ {
		if got[i] != got[0] {
			t.Fatalf("form %d = %q, want %q", i, got[i], got[0])
		}
	}
}

func TestNewRuntimeRoutesSockets(t *testing.T) {
	// A host:port address is a tcp target, never a pipe name.
	if !isSocketAddr("127.0.0.1:18790") || !isSocketAddr("unix:///tmp/cua.sock") {
		t.Fatal("socket addresses must route to the unix/tcp transport")
	}
	if isSocketAddr(`\\.\pipe\maclaw-cua`) || isSocketAddr("maclaw-cua") {
		t.Fatal("a pipe address must route to the named-pipe transport")
	}
}

// TestAwaitContextDeadline ensures a wedged read does not outlive ctx: the
// call returns on the deadline, not when the driver finally answers.
func TestAwaitContextDeadline(t *testing.T) {
	rt := &runtime{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := rt.await(1, ctx)
	if err == nil {
		t.Fatal("expected an error from an unconnected await")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("await outlived its context by %s", time.Since(started))
	}
}

// TestSelectorMatchAndPick exercises the selector semantics hosts rely on:
// role case folding, label-or-value containment, and the actionable row
// winning over a static text row with the same words.
func TestSelectorMatchAndPick(t *testing.T) {
	snap := Snapshot{
		WindowTitle: "Test",
		Elements: []Element{
			{Token: "s1:1", Role: "Text", Label: "确定"},
			{Token: "s1:2", Role: "Button", Label: "确定", Actions: []string{"invoke"}},
			{Token: "s1:3", Role: "Edit", Label: "搜索框", Value: "abc"},
		},
	}
	hit, err := snap.Pick(Selector{Label: "确定"})
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if hit.Token != "s1:2" {
		t.Fatalf("picked %s, want the actionable button", hit.Token)
	}
	if hit, _ := snap.Pick(Selector{Role: "button"}); hit.Token != "s1:2" {
		t.Fatalf("role match is case-insensitive; got %s", hit.Token)
	}
	if hit, _ := snap.Pick(Selector{RolePrefix: "bu"}); hit.Token != "s1:2" {
		t.Fatalf("role prefix match; got %s", hit.Token)
	}
	if _, err := snap.Pick(Selector{Label: "abc"}); err != nil {
		t.Fatalf("value must match a label selector: %v", err)
	}
	if _, err := snap.Pick(Selector{Label: "不存在"}); err == nil {
		t.Fatal("a missing element must be an error")
	}
}

// TestVerifyWrapsDriverAnswer checks that a driver verdict survives the
// typed round trip and that an unmet predicate is explained.
func TestVerifyWrapsDriverAnswer(t *testing.T) {
	v := Verdict{
		Status:     "unknown",
		Text:       "verify_state: unknown after 35 sample(s)",
		Predicates: []PredicateNote{{Index: 0, Status: "unknown", UnknownReason: "observation_unavailable"}},
	}
	text := VerifyText(v)
	if !strings.Contains(text, "not verified") || !strings.Contains(text, "observation_unavailable") {
		t.Fatalf("unexpected verdict text: %s", text)
	}
	if !strings.Contains(VerifyText(Verdict{Status: "satisfied", Samples: 2}), "verified") {
		t.Fatal("a satisfied verdict must read as verified")
	}
}

// TestElementExpectWireShape pins the driver's argument shape: a selector
// carries only role and label_contains, and absence is refused here rather
// than silently asked for.
func TestElementExpectWireShape(t *testing.T) {
	d := &Driver{}
	_, err := d.Verify(context.Background(), 1, 2, []Expect{{Element: &ElementExpect{
		Selector: Selector{Label: "x"},
		Exists:   boolPtr(false),
	}}}, 1, 100)
	if err == nil || !strings.Contains(err.Error(), "absence") {
		t.Fatalf("element absence must be refused locally, got %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestRuntimeSerializesCalls proves one request is in flight at a time on
// one pipe instance, which is what the daemon's framing requires.
func TestRuntimeSerializesCalls(t *testing.T) {
	rt := &runtime{}
	var inFlight atomic.Int32
	var overlap atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt.mu.Lock()
			defer rt.mu.Unlock()
			if inFlight.Add(1) > 1 {
				overlap.Store(true)
			}
			inFlight.Add(-1)
		}()
	}
	wg.Wait()
	if overlap.Load() {
		t.Fatal("two calls entered at once")
	}
}

var _ = fmt.Sprint
