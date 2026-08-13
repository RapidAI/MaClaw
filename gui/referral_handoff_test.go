package main

import "testing"

func TestReferralHandoffFromArgsAcceptsOnlySafeLaunchURLs(t *testing.T) {
	valid := "maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fhub.example.test"
	got := referralHandoffFromArgs([]string{"MaClaw", valid})
	if got.Handoff != "0123456789abcdef" || got.HubURL != "https://hub.example.test" {
		t.Fatalf("valid handoff=%#v", got)
	}
	loopback := "maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=http%3A%2F%2F127.0.0.1%3A8080"
	if got := referralHandoffFromArgs([]string{loopback}); got.HubURL != "http://127.0.0.1:8080" {
		t.Fatalf("loopback handoff=%#v", got)
	}
	for _, raw := range []string{
		"maclaw://other?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fhub.example.test",
		"other://onboarding?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fhub.example.test",
		"maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=http%3A%2F%2Fevil.example.test",
		"maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fuser%3Apass%40hub.example.test",
		"maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fhub.example.test%2Fredirect",
		"maclaw://onboarding?referral_handoff=0123456789abcdef?hub_url=https%3A%2F%2Fhub.example.test%3Fnext%3Devil",
		"maclaw://onboarding?referral_handoff=too-short?hub_url=https%3A%2F%2Fhub.example.test",
		"maclaw://onboarding?referral_handoff=0123456789abcdef%20bad?hub_url=https%3A%2F%2Fhub.example.test",
		"maclaw://onboarding?referral_handoff=0123456789abcdef",
	} {
		if got := referralHandoffFromArgs([]string{raw}); got.Handoff != "" || got.HubURL != "" {
			t.Fatalf("unsafe launch %q accepted as %#v", raw, got)
		}
	}
}
