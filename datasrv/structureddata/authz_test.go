package structureddata

import (
	"reflect"
	"testing"
)

func TestParseAPIKeyPoliciesNormalizesConfiguredScopes(t *testing.T) {
	raw := `[
		{
			"id": " agent-key ",
			"key": " 012345678901234567890123 ",
			"tenant_id": " tenant_1 ",
			"user_id": " agent_1 ",
			"role": " DATA_AUDITOR ",
			"allowed_domains": ["Sales", " sales ", ""],
			"allowed_datasets": ["Sales.Orders", "sales.orders", "sales.Customers"],
			"allowed_actions": ["Sales.CreateOrder"],
			"allowed_views": ["Sales.Pipeline"],
			"allowed_reports": ["Sales.Forecast"],
			"allowed_dashboards": ["Sales.Exec"],
			"allow_raw_data": true,
			"allow_sensitive": true
		},
		{
			"user_id": " skipped_without_secret ",
			"allowed_domains": ["finance"]
		},
		{
			"key": " 987654321098765432109876 ",
			"user_id": " fallback_id "
		}
	]`
	policies, err := ParseAPIKeyPolicies(raw)
	if err != nil {
		t.Fatalf("ParseAPIKeyPolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("ParseAPIKeyPolicies returned %d policies, want 2: %#v", len(policies), policies)
	}
	first := policies[0]
	if first.ID != "agent-key" || first.Key != "012345678901234567890123" || first.TenantID != "tenant_1" || first.UserID != "agent_1" || first.Role != "data_auditor" {
		t.Fatalf("first policy was not trimmed/normalized: %#v", first)
	}
	if !reflect.DeepEqual(first.AllowedDomains, []string{"sales"}) {
		t.Fatalf("allowed domains=%#v, want sales", first.AllowedDomains)
	}
	if !reflect.DeepEqual(first.AllowedDatasets, []string{"sales.orders", "sales.customers"}) {
		t.Fatalf("allowed datasets=%#v, want normalized unique datasets", first.AllowedDatasets)
	}
	if !reflect.DeepEqual(first.AllowedActions, []string{"sales.createorder"}) ||
		!reflect.DeepEqual(first.AllowedViews, []string{"sales.pipeline"}) ||
		!reflect.DeepEqual(first.AllowedReports, []string{"sales.forecast"}) ||
		!reflect.DeepEqual(first.AllowedDashboards, []string{"sales.exec"}) {
		t.Fatalf("scoped resources were not normalized: %#v", first)
	}
	if !first.AllowRawData || !first.AllowSensitive {
		t.Fatalf("boolean scope flags were not preserved: %#v", first)
	}
	if policies[1].ID != "fallback_id" || policies[1].Key != "987654321098765432109876" {
		t.Fatalf("policy without id should fall back to user_id and trim key: %#v", policies[1])
	}
}

func TestParseAPIKeyPoliciesAllowsEmptyInput(t *testing.T) {
	policies, err := ParseAPIKeyPolicies("   ")
	if err != nil {
		t.Fatalf("ParseAPIKeyPolicies empty input returned error: %v", err)
	}
	if policies != nil {
		t.Fatalf("ParseAPIKeyPolicies empty input=%#v, want nil", policies)
	}
}
