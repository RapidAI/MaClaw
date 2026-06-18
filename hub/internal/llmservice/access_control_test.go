package llmservice

import (
	"context"
	"testing"
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
