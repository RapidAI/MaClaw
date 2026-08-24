package llmservice

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func deepSeekOffPeakPolicy() llmpool.ProviderBillingPolicy {
	return llmpool.ProviderBillingPolicy{
		ProviderID:       "deepseek",
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}
}

func TestBillableCreditMultiplierLocalScheduleUsesRequestStart(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := deepSeekOffPeakPolicy()
	offPeak := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	if got := BillableCreditMultiplier(nil, "deepseek", offPeak, &policy, nil, 0, ""); got != 0.5 {
		t.Fatalf("off-peak start got %v, want 0.5", got)
	}
	peak := time.Date(2026, 8, 17, 12, 0, 0, 0, loc)
	if got := BillableCreditMultiplier(nil, "deepseek", peak, &policy, nil, 0, ""); got != 1 {
		t.Fatalf("peak start got %v, want 1", got)
	}
}

func TestBillableCreditMultiplierOfficialPrefersUpstreamAppliedRate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	model := &AuthorizedModel{CreditMultiplier: 2}
	got := BillableCreditMultiplier(model, MaClawOfficialProviderID, started, nil, []llmpool.ProviderBillingPolicy{deepSeekOffPeakPolicy()}, 0.5, "")
	if got != 0.5 {
		t.Fatalf("official applied header got %v, want 0.5 without local route remultiply", got)
	}
}

func TestBillableCreditMultiplierOfficialFallsBackToSyncedPolicyAtRequestStart(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	got := BillableCreditMultiplier(nil, MaClawOfficialProviderID, started, nil, []llmpool.ProviderBillingPolicy{deepSeekOffPeakPolicy()}, 0, "")
	if got != 0.5 {
		t.Fatalf("official synced policy got %v, want 0.5", got)
	}
}

func TestBillableCreditMultiplierOfficialSelectsProviderPolicyWhenHeaderMissing(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	official := []llmpool.ProviderBillingPolicy{
		{ProviderID: "openai", Timezone: "Asia/Shanghai", CreditMultiplier: 1},
		deepSeekOffPeakPolicy(),
	}
	got := BillableCreditMultiplier(nil, MaClawOfficialProviderID, started, nil, official, 0, "deepseek")
	if got != 0.5 {
		t.Fatalf("official deepseek policy got %v, want 0.5", got)
	}
	got = BillableCreditMultiplier(nil, MaClawOfficialProviderID, started, nil, official, 0, "")
	if got != 1 {
		t.Fatalf("disagreeing official policies without provider id got %v, want 1", got)
	}
}
