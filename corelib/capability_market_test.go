package corelib

import "testing"

func TestDecideCapabilityInstallEnterpriseOnlyInstallCreatesImportForFreeExternal(t *testing.T) {
	decision := DecideCapabilityInstall(CapabilityInstallDecisionInput{
		Policy:            DefaultCapabilityMarketPolicy(),
		Source:            CapabilitySourceHubCenter,
		Pricing:           CapabilityPricingFree,
		ExternalInstallOK: true,
	})
	if decision.Action != CapabilityInstallCreateImportRequest {
		t.Fatalf("Action = %q, want %q", decision.Action, CapabilityInstallCreateImportRequest)
	}
	if decision.Reason != "enterprise_only_install" {
		t.Fatalf("Reason = %q", decision.Reason)
	}
}

func TestDecideCapabilityInstallPaidExternalCreatesPurchaseRequest(t *testing.T) {
	decision := DecideCapabilityInstall(CapabilityInstallDecisionInput{
		Policy:             DefaultCapabilityMarketPolicy(),
		Source:             CapabilitySourceHubCenter,
		Pricing:            CapabilityPricingPaid,
		ExternalPurchaseOK: true,
	})
	if decision.Action != CapabilityInstallCreatePurchaseRequest {
		t.Fatalf("Action = %q, want %q", decision.Action, CapabilityInstallCreatePurchaseRequest)
	}
}

func TestDecideCapabilityInstallEnterpriseSearchBlocksExternal(t *testing.T) {
	policy := DefaultCapabilityMarketPolicy()
	v := true
	policy.EnterpriseOnlySearch = &v
	decision := DecideCapabilityInstall(CapabilityInstallDecisionInput{
		Policy:  policy,
		Source:  CapabilitySourceHubCenter,
		Pricing: CapabilityPricingFree,
	})
	if decision.Action != CapabilityInstallBlocked || decision.Reason != "enterprise_only_search" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecideCapabilityUpdateRules(t *testing.T) {
	policy := DefaultCapabilityMarketPolicy()
	cases := []struct {
		name       string
		source     string
		pricing    string
		autoUpdate bool
		policyName string
	}{
		{"enterprise", CapabilitySourceEnterpriseHub, CapabilityPricingPaid, true, "auto_update_approved"},
		{"hubcenter free", CapabilitySourceHubCenter, CapabilityPricingFree, true, "auto_update"},
		{"hubcenter paid", CapabilitySourceHubCenter, CapabilityPricingPaid, false, "require_license_and_purchase_policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideCapabilityUpdate(CapabilityUpdateDecisionInput{Policy: policy, Source: tc.source, Pricing: tc.pricing})
			if decision.AutoUpdate != tc.autoUpdate || decision.Policy != tc.policyName {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestAdminMarketplaceSearchSources(t *testing.T) {
	hubSources := AdminMarketplaceSearchSources(CapabilityMarketplaceHostHub)
	wantHub := []string{CapabilitySourceHubCenter, CapabilitySourceClawHub, CapabilitySourceGitHub}
	if len(hubSources) != len(wantHub) {
		t.Fatalf("hub sources = %#v", hubSources)
	}
	for i := range wantHub {
		if hubSources[i] != wantHub[i] {
			t.Fatalf("hub sources = %#v, want %#v", hubSources, wantHub)
		}
	}
	if !AdminMarketplaceCanSearchSource(CapabilityMarketplaceHostHub, CapabilitySourceHubCenter) {
		t.Fatal("hub admin should be able to search hubcenter")
	}
	if AdminMarketplaceCanSearchSource(CapabilityMarketplaceHostHubCenter, CapabilitySourceHubCenter) {
		t.Fatal("hubcenter admin should not search hubcenter as an external source")
	}
	if !AdminMarketplaceCanSearchSource(CapabilityMarketplaceHostHubCenter, CapabilitySourceGitHub) {
		t.Fatal("hubcenter admin should be able to search github")
	}
}
