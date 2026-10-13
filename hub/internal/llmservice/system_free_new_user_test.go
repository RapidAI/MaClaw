package llmservice

import (
	"testing"
	"time"
)

func TestRegistryIgnoringSystemFreeNewUserLimitLeavesGUICardIntact(t *testing.T) {
	now := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: SystemFreeServiceGroupID, AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{SystemFreeServiceGroupID},
			PeriodLimits:    CreditPeriodLimits{Daily: 10},
		},
		Grants: []Grant{
			{ID: "alice-card", UserID: "alice", Email: "alice@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
			{ID: "bob-card", UserID: "bob", Email: "bob@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
			{ID: "alice-other", UserID: "alice", Email: "alice@example.com", ServiceGroupID: "coding", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		},
		BillingReservations: []BillingReservation{{RequestID: "hold-1"}},
	}
	if got := ApplyCreditUsageToRegistryForUserID(reg, "alice", "alice@example.com", []string{SystemFreeServiceGroupID}, 10, now); got != 10 {
		t.Fatalf("gui usage = %v, want 10", got)
	}
	allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroupsForUserID(reg, "alice", "alice@example.com", []string{SystemFreeServiceGroupID}, now)
	if allowed || code != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("exhausted gui card allowed=%v code=%q", allowed, code)
	}

	view := RegistryIgnoringSystemFreeNewUserLimit(reg, "alice", "alice@example.com")
	if view == reg {
		t.Fatal("billing view aliases the stored registry")
	}
	if reg.Grants[0].Frozen || reg.Grants[1].Frozen || reg.Grants[2].Frozen {
		t.Fatalf("stored grants were frozen: %+v", reg.Grants)
	}
	if !view.Grants[0].Frozen {
		t.Fatal("alice system-free card was not ignored")
	}
	if view.Grants[1].Frozen || view.Grants[2].Frozen {
		t.Fatalf("unrelated cards were frozen: %+v", view.Grants)
	}
	view.BillingReservations = append(view.BillingReservations, BillingReservation{RequestID: "hold-2"})
	if len(reg.BillingReservations) != 1 {
		t.Fatalf("reservation copy aliases the stored slice: %+v", reg.BillingReservations)
	}

	if got := ApplyCreditUsageToRegistryForUserID(view, "alice", "alice@example.com", []string{SystemFreeServiceGroupID}, 4, now); got != 0 {
		t.Fatalf("non-gui usage = %v, want 0", got)
	}
	if reg.Grants[0].PeriodUsage.Daily.CreditsUsed != 10 {
		t.Fatalf("stored card usage changed: %+v", reg.Grants[0].PeriodUsage)
	}
	allowed, _, code, _, _, _, _ = BillingEligibilityForServiceGroupsForUserID(view, "alice", "alice@example.com", []string{SystemFreeServiceGroupID}, now)
	if !allowed || code != "" {
		t.Fatalf("non-gui system-free eligibility allowed=%v code=%q", allowed, code)
	}
	allowed, _, code, _, _, _, _ = BillingEligibilityForServiceGroupsForUserID(reg, "bob", "bob@example.com", []string{SystemFreeServiceGroupID}, now)
	if !allowed || code != "" {
		t.Fatalf("other user's card eligibility allowed=%v code=%q", allowed, code)
	}
}
