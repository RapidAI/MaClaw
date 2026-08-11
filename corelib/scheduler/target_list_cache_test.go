package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestTargetListCacheGetOrLoad(t *testing.T) {
	t.Parallel()
	c := NewTargetListCache(50 * time.Millisecond)
	loads := 0
	load := func() ([]TargetRef, error) {
		loads++
		return []TargetRef{{Kind: DeliveryKindGroup, ID: "g1", Name: "A"}}, nil
	}
	r1, err := c.GetOrLoad("lansenger", load)
	if err != nil || len(r1) != 1 || loads != 1 {
		t.Fatalf("first: loads=%d r=%v err=%v", loads, r1, err)
	}
	r2, err := c.GetOrLoad("lansenger", load)
	if err != nil || loads != 1 {
		t.Fatalf("cached: loads=%d err=%v", loads, err)
	}
	if r2[0].ID != "g1" {
		t.Fatalf("%+v", r2)
	}
	// Mutating returned slice must not poison cache.
	r2[0].ID = "mutated"
	r3, _ := c.GetOrLoad("lansenger", load)
	if r3[0].ID != "g1" {
		t.Fatalf("cache poisoned: %+v", r3)
	}

	time.Sleep(60 * time.Millisecond)
	_, err = c.GetOrLoad("lansenger", load)
	if err != nil || loads != 2 {
		t.Fatalf("after ttl: loads=%d err=%v", loads, err)
	}

	c.Invalidate("lansenger")
	_, err = c.GetOrLoad("lansenger", func() ([]TargetRef, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected load error")
	}
}

func TestTargetListCacheInvalidatePrefixKeepsOtherProfiles(t *testing.T) {
	t.Parallel()
	c := NewTargetListCache(time.Minute)
	c.Put("lansenger:support", []TargetRef{{ID: "support-group"}})
	c.Put("lansenger:sales", []TargetRef{{ID: "sales-group"}})
	c.Put("weixin", []TargetRef{{ID: "owner"}})
	c.InvalidatePrefix("lansenger:support")
	if _, ok := c.Get("lansenger:support"); ok {
		t.Fatal("support cache entry survived invalidation")
	}
	if refs, ok := c.Get("lansenger:sales"); !ok || len(refs) != 1 || refs[0].ID != "sales-group" {
		t.Fatalf("sales cache = %#v ok=%v", refs, ok)
	}
	if refs, ok := c.Get("weixin"); !ok || len(refs) != 1 || refs[0].ID != "owner" {
		t.Fatalf("weixin cache = %#v ok=%v", refs, ok)
	}
}

func TestTargetListCacheAliasesShareSlot(t *testing.T) {
	t.Parallel()
	c := NewTargetListCache(time.Minute)
	loads := 0
	load := func() ([]TargetRef, error) {
		loads++
		return []TargetRef{{Kind: DeliveryKindGroup, ID: "g1"}}, nil
	}
	if _, err := c.GetOrLoad("lansenger", load); err != nil {
		t.Fatal(err)
	}
	// Chinese alias must hit the same cache entry (not re-load).
	if _, err := c.GetOrLoad("蓝信", load); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("alias should share cache slot, loads=%d", loads)
	}
	c.Invalidate("蓝信")
	if _, err := c.GetOrLoad("lansenger", load); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("invalidate via alias should clear slot, loads=%d", loads)
	}
}
