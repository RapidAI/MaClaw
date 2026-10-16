package llmservice

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestEffectiveDispatchWeightScalesTokenBankMembersOnly(t *testing.T) {
	t.Cleanup(resetTokenBankAutoPause)
	resetTokenBankAutoPause()

	cold := &llmpool.ProviderConfig{ID: TokenBankMemberID("share-cold", "m"), DispatchWeight: 1}
	for i := 0; i < 4; i++ {
		noteMemberAttempt(cold.ID, 500, time.Millisecond, "upstream", "")
	}
	if got := effectiveDispatchWeight(cold); got != 1 {
		t.Fatalf("cold weight = %d, want the stored weight before 5 samples", got)
	}

	healthy := &llmpool.ProviderConfig{ID: TokenBankMemberID("share-healthy", "m"), DispatchWeight: 1}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(healthy.ID, 200, time.Millisecond, "", "")
	}
	if got := effectiveDispatchWeight(healthy); got != 100 {
		t.Fatalf("healthy weight = %d, want 100", got)
	}

	slow := &llmpool.ProviderConfig{ID: TokenBankMemberID("share-slow", "m"), DispatchWeight: 1}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(slow.ID, 200, 6*time.Second, "", "")
	}
	if got := effectiveDispatchWeight(slow); got != 50 {
		t.Fatalf("slow healthy weight = %d, want 50", got)
	}

	failing := &llmpool.ProviderConfig{ID: TokenBankMemberID("share-failing", "m"), DispatchWeight: 2}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(failing.ID, 500, time.Millisecond, "upstream", "")
	}
	if got := effectiveDispatchWeight(failing); got != 2 {
		t.Fatalf("failing weight = %d, want floor 1 times stored 2", got)
	}

	plain := &llmpool.ProviderConfig{ID: "plain-provider", DispatchWeight: 3}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(plain.ID, 500, time.Millisecond, "upstream", "")
	}
	if got := effectiveDispatchWeight(plain); got != 3 {
		t.Fatalf("non-member weight = %d, want the stored weight", got)
	}

	equal := []*llmpool.ProviderConfig{
		{ID: TokenBankMemberID("share-eq-a", "m"), DispatchWeight: 1},
		{ID: TokenBankMemberID("share-eq-b", "m"), DispatchWeight: 1},
	}
	if arrayMembersUseWeight(equal) {
		t.Fatal("cold equal token-bank members used weighted dispatch")
	}

	weighted := []*llmpool.ProviderConfig{healthy, failing}
	counts := map[string]int{}
	for i := 0; i < 102; i++ {
		ordered := orderArrayMembers("health-pool-p1", weighted)
		if len(ordered) == 0 || ordered[0] == nil {
			t.Fatalf("order = %#v", ordered)
		}
		counts[ordered[0].ID]++
	}
	if counts[healthy.ID] <= counts[failing.ID] {
		t.Fatalf("health picks = %#v, want the healthy member more often", counts)
	}
}
