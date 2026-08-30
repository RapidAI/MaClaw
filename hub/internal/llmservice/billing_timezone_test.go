package llmservice

import (
	"context"
	"sync"
	"testing"
	"time"
)

type billingTimezoneSettings struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *billingTimezoneSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}

func (s *billingTimezoneSettings) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func TestGrantDailyWindowUsesPersistedIANATimezone(t *testing.T) {
	grant := Grant{BillingTimezone: "Asia/Shanghai"}
	now := time.Date(2026, time.August, 26, 15, 0, 0, 0, time.UTC) // 23:00 CST
	start := grantDayWindowStart(grant, now)
	want := time.Date(2026, time.August, 25, 16, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Fatalf("daily start = %s, want %s", start.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if end := start.AddDate(0, 0, 1); !end.Equal(time.Date(2026, time.August, 26, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily end = %s, want 2026-08-26T16:00:00Z", end.Format(time.RFC3339))
	}
}

func TestGrantDailyWindowFallsBackToUTCForLegacyGrant(t *testing.T) {
	grant := Grant{BillingTimezone: "not-a-timezone"}
	now := time.Date(2026, time.August, 26, 15, 0, 0, 0, time.UTC)
	start := grantDayWindowStart(grant, now)
	want := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	if !start.Equal(want) {
		t.Fatalf("daily start = %s, want %s", start.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestBillingTimezoneAllowsUTCButRejectsFixedOffsets(t *testing.T) {
	if got := NormalizeBillingTimezone("UTC"); got != "UTC" {
		t.Fatalf("UTC normalized to %q, want UTC", got)
	}
	if got := NormalizeBillingTimezone("Etc/UTC"); got != "UTC" {
		t.Fatalf("Etc/UTC normalized to %q, want UTC", got)
	}
	for _, value := range []string{"Local", "+08:00", "GMT+08:00", "Etc/GMT+8", ""} {
		if got := NormalizeBillingTimezone(value); got != "" {
			t.Fatalf("%q normalized to %q, want rejection", value, got)
		}
	}
}

func TestGrantCalendarWindowsHonorDST(t *testing.T) {
	grant := Grant{BillingTimezone: "America/New_York"}
	// At 2026-03-09T12:00Z New York has just moved to EDT (UTC-4).
	now := time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)
	dayStart := grantDayWindowStart(grant, now)
	if want := time.Date(2026, time.March, 9, 4, 0, 0, 0, time.UTC); !dayStart.Equal(want) {
		t.Fatalf("daily start = %s, want %s", dayStart.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if want := time.Date(2026, time.March, 9, 4, 0, 0, 0, time.UTC); !grantWeekWindowStart(grant, now).Equal(want) {
		t.Fatalf("weekly start = %s, want %s", grantWeekWindowStart(grant, now).Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if want := time.Date(2026, time.March, 1, 5, 0, 0, 0, time.UTC); !grantMonthWindowStart(grant, now).Equal(want) {
		t.Fatalf("monthly start = %s, want %s", grantMonthWindowStart(grant, now).Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestSetUserBillingTimezoneIsWriteOnce(t *testing.T) {
	settings := &billingTimezoneSettings{}
	first, err := SetUserBillingTimezone(context.Background(), settings, "user@example.com", "Asia/Shanghai")
	if err != nil || first != "Asia/Shanghai" {
		t.Fatalf("first timezone = %q, %v", first, err)
	}
	second, err := SetUserBillingTimezone(context.Background(), settings, "user@example.com", "America/Los_Angeles")
	if err != nil || second != "Asia/Shanghai" {
		t.Fatalf("second timezone = %q, %v; want original timezone", second, err)
	}
	registry, err := LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	grant := effectiveGrantForRegistry(registry, Grant{Email: "user@example.com"})
	if grant.BillingTimezone != "Asia/Shanghai" {
		t.Fatalf("effective grant timezone = %q, want Asia/Shanghai", grant.BillingTimezone)
	}
}
