package httpapi

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestBuildCenterRankingRowsFiltersUIDAndClampsDuration(t *testing.T) {
	rows := buildCenterRankingRows([]*store.HubUserUsageDaily{
		{HubID: "hub_a", TenantID: "tenant_a", UserEmail: "u_1774182684297100200", DurationSeconds: 999999},
		{HubID: "hub_a", TenantID: "tenant_a", UserEmail: "slow@example.com", DurationSeconds: 999999},
		{HubID: "hub_a", TenantID: "tenant_a", UserEmail: "fast@example.com", InputTokens: 10, OutputTokens: 5, DurationSeconds: 60},
		{HubID: "hub_a", TenantID: "tenant_a", UserEmail: "bad@example.com", InputTokens: -10, OutputTokens: -5, DurationSeconds: -60},
	}, "duration", 10, int64(2*time.Hour/time.Second))

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %#v", len(rows), rows)
	}
	if rows[0].UserEmail != "slow@example.com" {
		t.Fatalf("first email = %q, want slow@example.com", rows[0].UserEmail)
	}
	if rows[0].DurationSeconds != int64(2*time.Hour/time.Second) {
		t.Fatalf("clamped duration = %d, want %d", rows[0].DurationSeconds, int64(2*time.Hour/time.Second))
	}
	if rows[1].UserEmail != "fast@example.com" || rows[1].DurationRank != 2 || rows[1].TokenRank != 1 {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
	if rows[2].UserEmail != "bad@example.com" || rows[2].DurationSeconds != 0 || rows[2].TotalTokens != 0 {
		t.Fatalf("unexpected sanitized row: %#v", rows[2])
	}
}

func TestCenterRankingEmailFilterRejectsMalformedEmails(t *testing.T) {
	for _, tc := range []struct {
		email string
		want  bool
	}{
		{email: "user@example.com", want: true},
		{email: " User@Example.com ", want: true},
		{email: "u_1774182684297100200", want: false},
		{email: "foo@", want: false},
		{email: "@example.com", want: false},
		{email: "foo @example.com", want: false},
		{email: "foo@@example.com", want: false},
		{email: "", want: false},
	} {
		if got := isCenterRankingEmail(tc.email); got != tc.want {
			t.Fatalf("isCenterRankingEmail(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func TestNormalizeCenterUsageValues(t *testing.T) {
	if got := nonNegativeCenterUsageValue(-10); got != 0 {
		t.Fatalf("negative usage = %d, want 0", got)
	}
	if got := normalizeCenterDailyDurationSeconds(25 * 60 * 60); got != int64(24*60*60) {
		t.Fatalf("daily duration = %d, want 86400", got)
	}
	if got := normalizeCenterDailyDurationSeconds(-1); got != 0 {
		t.Fatalf("negative duration = %d, want 0", got)
	}
}
func TestValidCenterSyncDayRange(t *testing.T) {
	if !validCenterSyncDayRange("2026-06-24", "2026-06-24") {
		t.Fatal("same-day range should be valid")
	}
	if validCenterSyncDayRange("2026-06-25", "2026-06-24") {
		t.Fatal("inverted range should be invalid")
	}
	if validCenterSyncDayRange("bad", "2026-06-24") {
		t.Fatal("bad start day should be invalid")
	}
}
