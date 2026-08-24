package llmpool

import (
	"testing"
	"time"
)

func TestWRRSchedulerEqualWeightsAlternate(t *testing.T) {
	sched := NewWRRScheduler()
	members := []WRRMember{
		{ID: "a", Weight: 10, Sequence: 1},
		{ID: "b", Weight: 10, Sequence: 2},
	}
	first := sched.Next("x1", members)
	second := sched.Next("x1", members)
	if first != "a" {
		t.Fatalf("first = %q, want a", first)
	}
	if second != "b" {
		t.Fatalf("second = %q, want b", second)
	}
}

func TestWRRSchedulerWeightedDistribution(t *testing.T) {
	sched := NewWRRScheduler()
	members := []WRRMember{
		{ID: "big", Weight: 100, Sequence: 1},
		{ID: "mid", Weight: 10, Sequence: 2},
		{ID: "small", Weight: 10, Sequence: 3},
	}
	counts := map[string]int{}
	for i := 0; i < 120; i++ {
		counts[sched.Next("x1", members)]++
	}
	if counts["big"] != 100 || counts["mid"] != 10 || counts["small"] != 10 {
		t.Fatalf("counts = %#v, want 100/10/10", counts)
	}
}

func TestWRRSchedulerResetsWhenWeightChanges(t *testing.T) {
	sched := NewWRRScheduler()
	before := []WRRMember{{ID: "a", Weight: 1, Sequence: 1}, {ID: "b", Weight: 1, Sequence: 2}}
	if got := sched.Next("x1", before); got != "a" {
		t.Fatalf("first = %q, want a", got)
	}
	after := []WRRMember{{ID: "a", Weight: 10, Sequence: 1}, {ID: "b", Weight: 1, Sequence: 2}}
	if got := sched.Next("x1", after); got != "a" {
		t.Fatalf("after weight change first pick = %q, want a (reset)", got)
	}
}

func TestWRRSchedulerIsolatesDifferentPools(t *testing.T) {
	sched := NewWRRScheduler()
	ab := []WRRMember{{ID: "a", Weight: 1, Sequence: 1}, {ID: "b", Weight: 1, Sequence: 2}}
	if got := sched.Next("auto\x1ex1|s0|t1", ab); got != "a" {
		t.Fatalf("auto first = %q, want a", got)
	}
	if got := sched.Next("gpt-4\x1ex1|s0|t1", ab); got != "a" {
		t.Fatalf("gpt-4 first = %q, want a (other pool must not share state)", got)
	}
	if got := sched.Next("auto\x1ex1|s0|t1", ab); got != "b" {
		t.Fatalf("auto second = %q, want b", got)
	}
}

func TestWRRSchedulerResetsWhenMembershipChanges(t *testing.T) {
	sched := NewWRRScheduler()
	ab := []WRRMember{{ID: "a", Weight: 1, Sequence: 1}, {ID: "b", Weight: 1, Sequence: 2}}
	if got := sched.Next("x1", ab); got != "a" {
		t.Fatalf("ab first = %q, want a", got)
	}
	if got := sched.Next("x1", []WRRMember{{ID: "b", Weight: 1, Sequence: 2}}); got != "b" {
		t.Fatalf("shrunk membership = %q, want b", got)
	}
	if got := sched.Next("x1", ab); got != "a" {
		t.Fatalf("restored membership = %q, want a (reset so recovered member is probed first)", got)
	}
}

func TestAnnotateProviderLBGroups(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	got := AnnotateProviderLBGroups([]ProviderLBInput{
		{ID: "a", Billing: ProviderBillingPolicy{CreditMultiplier: 1}},
		{ID: "b", Billing: ProviderBillingPolicy{CreditMultiplier: 1}},
		{ID: "c", Paused: true, Billing: ProviderBillingPolicy{CreditMultiplier: 1}},
		{ID: "d", Billing: ProviderBillingPolicy{CreditMultiplier: 2}},
	}, now)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].LBGroup != "x1" || got[0].LBGroupSize != 3 || !got[0].LBEligible {
		t.Fatalf("a = %#v", got[0])
	}
	if got[2].LBGroup != "x1" || got[2].LBEligible {
		t.Fatalf("paused c = %#v", got[2])
	}
	if got[3].LBGroup != "" || got[3].LBGroupSize != 1 {
		t.Fatalf("solo d = %#v", got[3])
	}
}

func TestBalanceProviderRoutesGroupsByMultiplierThenScore(t *testing.T) {
	sched := NewWRRScheduler()
	got := BalanceProviderRoutes(sched, "test", []BalanceCandidate{
		{Route: DispatchProviderRoute{ProviderID: "tools-x2"}, Score: 800, ResolutionTier: 1, EffectiveMultiplier: 2, Sequence: 1, MaxConcurrency: 10},
		{Route: DispatchProviderRoute{ProviderID: "basic-x1"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 2, MaxConcurrency: 10},
		{Route: DispatchProviderRoute{ProviderID: "tools-x1"}, Score: 800, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 3, MaxConcurrency: 10},
	})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Route.ProviderID != "tools-x1" {
		t.Fatalf("first = %s, want cheap tools band", got[0].Route.ProviderID)
	}
	if got[1].Route.ProviderID != "basic-x1" {
		t.Fatalf("second = %s, want cheap basic failover", got[1].Route.ProviderID)
	}
	if got[2].Route.ProviderID != "tools-x2" {
		t.Fatalf("third = %s, want expensive tools", got[2].Route.ProviderID)
	}
}

func TestBalanceProviderRoutesWRRThenSequenceFailover(t *testing.T) {
	sched := NewWRRScheduler()
	candidates := []BalanceCandidate{
		{Route: DispatchProviderRoute{ProviderID: "aisi"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 1, MaxConcurrency: 10},
		{Route: DispatchProviderRoute{ProviderID: "oc1"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 2, MaxConcurrency: 10},
		{Route: DispatchProviderRoute{ProviderID: "oc2"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 4, MaxConcurrency: 10},
	}
	first := BalanceProviderRoutes(sched, "auto", candidates)
	second := BalanceProviderRoutes(sched, "auto", candidates)
	if first[0].Route.ProviderID != "aisi" || !first[0].FirstInBand {
		t.Fatalf("first pick = %#v, want aisi", first[0])
	}
	if first[1].Route.ProviderID != "oc1" || first[2].Route.ProviderID != "oc2" {
		t.Fatalf("first failover = %s,%s", first[1].Route.ProviderID, first[2].Route.ProviderID)
	}
	if second[0].Route.ProviderID != "oc1" {
		t.Fatalf("second pick = %s, want oc1", second[0].Route.ProviderID)
	}
	if second[1].Route.ProviderID != "aisi" || second[2].Route.ProviderID != "oc2" {
		t.Fatalf("second failover = %s,%s want aisi,oc2", second[1].Route.ProviderID, second[2].Route.ProviderID)
	}
}

func TestBalanceProviderRoutesEqualWeightIgnoresConcurrency(t *testing.T) {
	sched := NewWRRScheduler()
	candidates := []BalanceCandidate{
		{Route: DispatchProviderRoute{ProviderID: "fat"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 2, Sequence: 1, MaxConcurrency: 64},
		{Route: DispatchProviderRoute{ProviderID: "thin"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 2, Sequence: 2, MaxConcurrency: 10},
	}
	counts := map[string]int{}
	for i := 0; i < 20; i++ {
		got := BalanceProviderRoutes(sched, "equal", candidates)
		if len(got) == 0 {
			t.Fatal("empty balance result")
		}
		counts[got[0].Route.ProviderID]++
	}
	if counts["fat"] != 10 || counts["thin"] != 10 {
		t.Fatalf("counts = %#v, want 10/10 equal share in the multiplier band", counts)
	}
}

func TestBalanceProviderRoutesSkipsIneligibleWRRMembers(t *testing.T) {
	sched := NewWRRScheduler()
	candidates := []BalanceCandidate{
		{Route: DispatchProviderRoute{ProviderID: "paused"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 1, MaxConcurrency: 10, SkipWRR: true},
		{Route: DispatchProviderRoute{ProviderID: "live-a"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 2, MaxConcurrency: 10},
		{Route: DispatchProviderRoute{ProviderID: "live-b"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 3, MaxConcurrency: 10},
	}
	first := BalanceProviderRoutes(sched, "auto", candidates)
	second := BalanceProviderRoutes(sched, "auto", candidates)
	if len(first) != 3 || first[0].Route.ProviderID != "live-a" {
		t.Fatalf("first pick = %#v, want live-a (paused excluded from WRR)", first)
	}
	if first[1].Route.ProviderID != "paused" || first[2].Route.ProviderID != "live-b" {
		t.Fatalf("first failover = %s,%s want paused,live-b", first[1].Route.ProviderID, first[2].Route.ProviderID)
	}
	if !first[1].SkipWRR || first[0].SkipWRR || first[2].SkipWRR {
		t.Fatalf("SkipWRR flags = %#v, want only paused marked", first)
	}
	if second[0].Route.ProviderID != "live-b" {
		t.Fatalf("second pick = %s, want live-b", second[0].Route.ProviderID)
	}
}

func TestBalanceProviderRoutesAllSkipWRRKeepsSequence(t *testing.T) {
	sched := NewWRRScheduler()
	got := BalanceProviderRoutes(sched, "auto", []BalanceCandidate{
		{Route: DispatchProviderRoute{ProviderID: "b"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 2, SkipWRR: true},
		{Route: DispatchProviderRoute{ProviderID: "a"}, Score: 0, ResolutionTier: 1, EffectiveMultiplier: 1, Sequence: 1, SkipWRR: true},
	})
	if len(got) != 2 || got[0].Route.ProviderID != "a" || got[1].Route.ProviderID != "b" {
		t.Fatalf("order = %#v, want a then b", got)
	}
}

func TestShouldSkipFullBandMember(t *testing.T) {
	routes := []BalancedRoute{
		{Route: DispatchProviderRoute{ProviderID: "a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: DispatchProviderRoute{ProviderID: "b"}, BandKey: "x1|s0|t1"},
		{Route: DispatchProviderRoute{ProviderID: "c"}, BandKey: "x2|s0|t1", FirstInBand: true},
	}
	full := map[string]bool{"a": true}
	atCapacity := func(id string) bool { return full[id] }
	if !ShouldSkipFullBandMember(routes, 0, atCapacity) {
		t.Fatal("full a should skip when b has capacity")
	}
	full["b"] = true
	if ShouldSkipFullBandMember(routes, 0, atCapacity) {
		t.Fatal("WRR winner should queue when whole band is full")
	}
	if !ShouldSkipFullBandMember(routes, 1, atCapacity) {
		t.Fatal("failover member should not queue")
	}
}

func TestShouldSkipFullBandMemberIgnoresSkipWRRSiblings(t *testing.T) {
	routes := []BalancedRoute{
		{Route: DispatchProviderRoute{ProviderID: "chat-a"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: DispatchProviderRoute{ProviderID: "resp"}, BandKey: "x1|s0|t1", SkipWRR: true},
		{Route: DispatchProviderRoute{ProviderID: "chat-b"}, BandKey: "x1|s0|t1"},
	}
	full := map[string]bool{"chat-a": true}
	atCapacity := func(id string) bool { return full[id] }
	if !ShouldSkipFullBandMember(routes, 0, atCapacity) {
		t.Fatal("full chat-a should skip when chat-b has capacity")
	}
	full["chat-b"] = true
	if ShouldSkipFullBandMember(routes, 0, atCapacity) {
		t.Fatal("WRR winner should queue when the only remaining sibling is SkipWRR")
	}
	routes = routes[:2]
	if ShouldSkipFullBandMember(routes, 0, atCapacity) {
		t.Fatal("full WRR winner should queue when the only sibling is SkipWRR")
	}
}

func TestShouldSkipFullBandMemberAllowsSkipWRRProbeWhenNoEligibleSibling(t *testing.T) {
	routes := []BalancedRoute{
		{Route: DispatchProviderRoute{ProviderID: "live"}, BandKey: "x1|s0|t1", FirstInBand: true},
		{Route: DispatchProviderRoute{ProviderID: "open"}, BandKey: "x1|s0|t1", SkipWRR: true},
		{Route: DispatchProviderRoute{ProviderID: "other"}, BandKey: "x1|s0|t1"},
	}
	full := map[string]bool{"open": true}
	atCapacity := func(id string) bool { return full[id] }
	if !ShouldSkipFullBandMember(routes, 1, atCapacity) {
		t.Fatal("SkipWRR failover should yield to a later eligible sibling")
	}
	routes = routes[:2]
	if ShouldSkipFullBandMember(routes, 1, atCapacity) {
		t.Fatal("SkipWRR failover should still be tried so BeforeAttempt can probe")
	}
}
