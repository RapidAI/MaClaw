package llmpool

import "testing"

func TestCapabilityBillingMultiplierDefaults(t *testing.T) {
	cases := []struct {
		model string
		want  float64
	}{
		{model: "auto", want: 1},
		{model: "", want: 1},
		{model: "low", want: 0.5},
		{model: OfficialTierLow, want: 0.5},
		{model: "mid", want: 1},
		{model: OfficialTierMid, want: 1},
		{model: "high", want: 2},
		{model: OfficialTierHigh, want: 2},
		{model: "gpt-4o", want: 1},
	}
	for _, tc := range cases {
		if got := CapabilityBillingMultiplier(tc.model, 0); got != tc.want {
			t.Fatalf("default %q = %v, want %v", tc.model, got, tc.want)
		}
	}
	if got := CapabilityBillingMultiplier(OfficialTierHigh, 3); got != 3 {
		t.Fatalf("explicit high = %v, want 3", got)
	}
	if got := CapabilityBillingMultiplier(OfficialTierLow, -1); got != 0.5 {
		t.Fatalf("invalid low = %v, want default 0.5", got)
	}
}

func TestGroupCapabilityBillingMultiplierUsesStoredBand(t *testing.T) {
	group := &ServiceGroup{Models: []ModelConfig{
		{Name: "auto", BillingMultiplier: 1},
		{Name: OfficialTierLow, BillingMultiplier: 0.25},
		{Name: OfficialTierHigh},
	}}
	if got := GroupCapabilityBillingMultiplier(group, "low"); got != 0.25 {
		t.Fatalf("low = %v, want stored 0.25", got)
	}
	if got := GroupCapabilityBillingMultiplier(group, "auto"); got != 1 {
		t.Fatalf("auto = %v, want 1", got)
	}
	if got := GroupCapabilityBillingMultiplier(group, "high"); got != 2 {
		t.Fatalf("unset high = %v, want default 2", got)
	}
	both := &ServiceGroup{Models: []ModelConfig{
		{Name: OfficialTierLow, BillingMultiplier: 0.5},
		{Name: "low", BillingMultiplier: 0.25},
	}}
	if got := GroupCapabilityBillingMultiplier(both, "low"); got != 0.25 {
		t.Fatalf("exact low = %v, want 0.25", got)
	}
	if got := GroupCapabilityBillingMultiplier(both, OfficialTierLow); got != 0.5 {
		t.Fatalf("exact official-low = %v, want 0.5", got)
	}
}

func TestCanonicalClientModelAliases(t *testing.T) {
	if got := CanonicalClientModel(" LOW "); got != OfficialTierLow {
		t.Fatalf("low alias = %q", got)
	}
	if got := CanonicalClientModel("auto"); got != "auto" {
		t.Fatalf("auto = %q", got)
	}
	if got := NormalizeOfficialTier("mid"); got != OfficialTierMid {
		t.Fatalf("mid tier = %q", got)
	}
	if got := QualityForOfficialTier("high"); got != QualityHigh {
		t.Fatalf("high quality = %q", got)
	}
	if got := NormalizeOfficialTier("gpt-4o"); got != "" {
		t.Fatalf("concrete model tier = %q", got)
	}
}
