package llmservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestFilterProvidersForTenantKeepsBuiltinProviderCaseInsensitive(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_acme", &TenantAuthorizationStatus{TenantID: "tenant_acme", AllowExternalProviders: false})

	got := FilterProvidersForTenant(context.Background(), ac, "tenant_acme", []ModelServiceModel{
		{Name: "official", ProviderIDs: []string{" MaClaw_Official "}},
		{Name: "external", ProviderIDs: []string{"provider-a"}},
	})

	if len(got) != 1 || got[0].Name != "official" {
		t.Fatalf("filtered providers = %#v, want only official", got)
	}
}

func TestApplyLLMComputePayloadKeepsCacheWhenEmpty(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.ApplyLLMComputePayload([]byte(`{"provider_billing":[{"provider_id":"deepseek","timezone":"Asia/Shanghai","credit_multiplier":1,"credit_multiplier_schedule":[{"days":[1,2,3,4,5],"start":"00:30","end":"08:30","multiplier":0.5}]}]}`))
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "deepseek" || len(got[0].CreditMultiplierSchedule) != 1 {
		t.Fatalf("official billing = %#v", got)
	}
	ac.ApplyLLMComputePayload([]byte(`{"tenants":{}}`))
	got = ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "deepseek" {
		t.Fatalf("empty payload wiped billing: %#v", got)
	}
	ac.ApplyLLMComputePayload([]byte(`not-json`))
	if len(ac.OfficialProviderBilling()) != 1 {
		t.Fatal("invalid payload wiped billing")
	}
}

func TestUpdateOfficialProviderBillingIsolatesStoredSchedule(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	src := []llmpool.ProviderBillingPolicy{{
		ProviderID: "deepseek",
		Timezone:   "Asia/Shanghai",
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}}
	ac.UpdateOfficialProviderBilling(src)
	src[0].ProviderID = "mutated"
	src[0].CreditMultiplierSchedule[0].Multiplier = 9
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "deepseek" || got[0].CreditMultiplierSchedule[0].Multiplier != 0.5 {
		t.Fatalf("stored billing mutated: %#v", got)
	}
	ac.UpdateOfficialProviderBilling(nil)
	if len(ac.OfficialProviderBilling()) != 1 {
		t.Fatal("empty update wiped billing")
	}
}

func TestSeedOfficialBillingIfEmptyDoesNotOverwriteLiveCatalog(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateOfficialProviderBilling([]llmpool.ProviderBillingPolicy{{
		ProviderID:       "live",
		CreditMultiplier: 0.8,
	}})
	ac.SeedOfficialBillingIfEmpty([]byte(`{"provider_billing":[{"provider_id":"stale","credit_multiplier":0.2}]}`))
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "live" || got[0].CreditMultiplier != 0.8 {
		t.Fatalf("seed overwrote live catalog: %#v", got)
	}
}

func TestSeedOfficialBillingIfEmptyFillsEmptyCache(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.SeedOfficialBillingIfEmpty([]byte(`{"provider_billing":[{"provider_id":"seeded","credit_multiplier":0.5}]}`))
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "seeded" || got[0].CreditMultiplier != 0.5 {
		t.Fatalf("seed = %#v", got)
	}
}

func TestCacheTenantAuthorizationDoesNotTouchOfficialBilling(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateOfficialProviderBilling([]llmpool.ProviderBillingPolicy{{
		ProviderID:       "live",
		CreditMultiplier: 0.8,
	}})
	ac.CacheTenantAuthorization("tenant_acme", &TenantAuthorizationStatus{
		TenantID: "tenant_acme",
		ProviderBilling: []llmpool.ProviderBillingPolicy{{
			ProviderID:       "stale",
			CreditMultiplier: 0.2,
		}},
	})
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "live" || got[0].CreditMultiplier != 0.8 {
		t.Fatalf("cached heartbeat overwrote billing: %#v", got)
	}
}

func TestUpdateFromHeartbeatReplacesOfficialBilling(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateOfficialProviderBilling([]llmpool.ProviderBillingPolicy{{
		ProviderID:       "old",
		CreditMultiplier: 0.8,
	}})
	ac.UpdateFromHeartbeat("tenant_acme", &TenantAuthorizationStatus{
		TenantID: "tenant_acme",
		ProviderBilling: []llmpool.ProviderBillingPolicy{{
			ProviderID:       "new",
			CreditMultiplier: 0.5,
		}},
	})
	got := ac.OfficialProviderBilling()
	if len(got) != 1 || got[0].ProviderID != "new" || got[0].CreditMultiplier != 0.5 {
		t.Fatalf("live query did not replace billing: %#v", got)
	}
}
