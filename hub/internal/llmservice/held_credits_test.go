package llmservice

import (
	"testing"
	"time"
)

func TestCreditGrantSummariesExposeHeldCredits(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "official", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"official"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
		},
		Grants: []Grant{{
			ID:             "card",
			Email:          "user@example.com",
			ServiceGroupID: "official",
			Source:         "new_user_limit_card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
		}},
		BillingReservations: []BillingReservation{{
			RequestID:       "req-held-1",
			Email:           "user@example.com",
			ServiceGroupIDs: []string{"official"},
			Credits:         12.5,
			ExpiresAt:       now.Add(time.Hour),
			CreatedAt:       now,
			SentAt:          now,
		}},
	}
	owner := newUserAccountRef("", "user@example.com")
	items := creditGrantSummariesForOwner(reg, owner, now)
	if len(items) != 1 {
		t.Fatalf("grant summaries = %d, want 1", len(items))
	}
	if got := items[0].HeldCredits; got != 12.5 {
		t.Fatalf("HeldCredits = %v, want 12.5", got)
	}
	if got := HeldBillingCreditsForServiceGroupsForUserID(reg, "", "user@example.com", []string{"official"}, now); got != 12.5 {
		t.Fatalf("HeldBillingCreditsForServiceGroupsForUserID = %v, want 12.5", got)
	}
	// Admission subtracts the same hold from the window remaining.
	if got := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"official"}, now); got != 987.5 {
		t.Fatalf("AvailableCreditsForServiceGroups = %v, want 987.5", got)
	}
	// A terminal (already released) reservation must not report a hold.
	reg.BillingReservations[0].Status = BillingReservationUsageUnresolved
	if got := HeldBillingCreditsForServiceGroupsForUserID(reg, "", "user@example.com", []string{"official"}, now); got != 0 {
		t.Fatalf("held after terminal status = %v, want 0", got)
	}
}
