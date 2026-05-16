package corelib

import (
	"testing"
	"time"
)

func TestNormalizeDigitalEmployeeAuthorizationStates(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	future := now.AddDate(1, 0, 0).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)

	cases := []struct {
		name       string
		auth       DigitalEmployeeAuthorization
		wantActive bool
		wantReason string
		wantQuota  int
	}{
		{name: "active", auth: DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: future}, wantActive: true, wantReason: "", wantQuota: 2},
		{name: "disabled", auth: DigitalEmployeeAuthorization{Quota: 2, Enabled: false, ExpiresAt: future}, wantActive: false, wantReason: "disabled", wantQuota: 2},
		{name: "quota zero", auth: DigitalEmployeeAuthorization{Quota: 0, Enabled: true, ExpiresAt: future}, wantActive: false, wantReason: "quota_zero", wantQuota: 0},
		{name: "not subscribed", auth: DigitalEmployeeAuthorization{Quota: 2, Enabled: true}, wantActive: false, wantReason: "not_subscribed", wantQuota: 2},
		{name: "expired", auth: DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: past}, wantActive: false, wantReason: "expired", wantQuota: 2},
		{name: "invalid expiry", auth: DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: "not-a-date"}, wantActive: false, wantReason: "expired", wantQuota: 2},
		{name: "negative quota", auth: DigitalEmployeeAuthorization{Quota: -5, Enabled: true, ExpiresAt: future}, wantActive: false, wantReason: "quota_zero", wantQuota: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeDigitalEmployeeAuthorization(tc.auth, now)
			if got.Active != tc.wantActive || got.Reason != tc.wantReason || got.Quota != tc.wantQuota {
				t.Fatalf("NormalizeDigitalEmployeeAuthorization(%+v) = %+v, want active=%v reason=%q quota=%d", tc.auth, got, tc.wantActive, tc.wantReason, tc.wantQuota)
			}
		})
	}
}

func TestNormalizeDigitalEmployeeAuthorizationPreservesExplicitInactiveReason(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	got := NormalizeDigitalEmployeeAuthorization(DigitalEmployeeAuthorization{Quota: 2, Enabled: false, Reason: "admin_disabled"}, now)
	if got.Active || got.Reason != "admin_disabled" {
		t.Fatalf("expected explicit disabled reason to be preserved, got %+v", got)
	}
}
