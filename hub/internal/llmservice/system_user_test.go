package llmservice

import (
	"testing"
	"time"
)

func TestIsSystemLLMUser(t *testing.T) {
	if !IsSystemLLMUser("", SystemLLMUserEmail) {
		t.Fatal("sys_user email should be recognized")
	}
	if !IsSystemLLMUser(SystemLLMUserID("tenant_acme"), "") {
		t.Fatal("sys_user id should be recognized")
	}
	if IsSystemLLMUser("u_1", "user@example.com") {
		t.Fatal("ordinary user should not be treated as sys_user")
	}
}

func TestIssueNewUserLimitCardsSkipsSystemLLMUser(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:        []ModelServiceGroup{{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserBenefitMode: NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard:   NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{Daily: 10}},
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{ID: SystemLLMUserID("tenant_default"), Email: SystemLLMUserEmail}}, now); issued != 0 {
		t.Fatalf("issued = %d, want 0", issued)
	}
	if len(reg.Grants) != 0 {
		t.Fatalf("sys_user must not receive new-user grants: %#v", reg.Grants)
	}
}

func TestNeedsNewUserLimitCardBackfillSkipsSystemLLMUser(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups:        []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserBenefitMode: NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard:   NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{Daily: 10}},
	}
	if NeedsNewUserLimitCardBackfill(reg, SystemLLMUserID("tenant_default"), SystemLLMUserEmail) {
		t.Fatal("sys_user must not be backfilled with a new-user limit card")
	}
}

func TestResolveStatusForForcedServiceGroupIgnoresUserGrants(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-pro",
			Name:         "Coding Pro",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}
	status, models, err := ResolveStatusForForcedServiceGroup(reg, "coding-pro", "https://hub.example/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusForForcedServiceGroup: %v", err)
	}
	if status == nil || !status.Active || status.AuthMode != "service_group_api_key" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if len(status.ServiceGroupIDs) != 1 || status.ServiceGroupIDs[0] != "coding-pro" {
		t.Fatalf("service groups = %#v", status.ServiceGroupIDs)
	}
	if len(models) != 1 || models[0].Name != "auto" {
		t.Fatalf("models = %#v", models)
	}
	status, models, err = ResolveStatusForForcedServiceGroup(reg, "missing", "https://hub.example/api/llm/v1")
	if err != nil {
		t.Fatalf("missing group should not fail closed as an internal error: %v", err)
	}
	if status == nil || status.Active || len(models) != 0 {
		t.Fatalf("missing group should be inactive with no models, status=%#v models=%#v", status, models)
	}
}
