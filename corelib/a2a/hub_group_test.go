package a2a

import (
	"strings"
	"testing"
	"time"
)

func TestGroupEnvelopeCurrentHubValidation(t *testing.T) {
	now := time.Date(2026, 5, 3, 1, 2, 3, 0, time.UTC)
	env := NewGroupEnvelope("env-1", GroupMessageConsultationRequest, "maclaw-a", now)
	env.Request = &GroupConsultationRequest{ID: "req-1", FromID: "maclaw-a", Question: "How should we split this refactor?"}
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("ValidateCurrentHub returned error: %v", err)
	}
	if env.Scope != GroupScopeCurrentHub {
		t.Fatalf("scope = %q, want %q", env.Scope, GroupScopeCurrentHub)
	}
}

func TestGroupEnvelopeRejectsCrossHubScope(t *testing.T) {
	env := NewGroupEnvelope("env-1", GroupMessageProfile, "maclaw-a", time.Now())
	env.Scope = "agentnet"
	env.Profile = &GroupProfile{AgentID: "maclaw-a", Discoverable: true, Available: true}
	err := env.ValidateCurrentHub()
	if err == nil || !strings.Contains(err.Error(), GroupScopeCurrentHub) {
		t.Fatalf("expected current-hub scope error, got %v", err)
	}
}

func TestGroupProfileDiscoveryViewHidesModelWhenConfigured(t *testing.T) {
	profile := GroupProfile{
		AgentID:         " maclaw-a ",
		DisplayName:     " Builder ",
		Description:     " Code review and refactoring ",
		ModelClass:      " frontier ",
		SecurityGroupID: " team-a ",
		Discoverable:    true,
		Available:       true,
	}
	view := profile.DiscoveryView("hidden")
	if view.ModelClass != "" {
		t.Fatalf("ModelClass = %q, want hidden", view.ModelClass)
	}
	if view.AgentID != "maclaw-a" || view.DisplayName != "Builder" || view.SecurityGroupID != "team-a" {
		t.Fatalf("profile was not trimmed: %+v", view)
	}
}

func TestShouldAutoAcceptGroupInvitation(t *testing.T) {
	inv := GroupInvitation{ToID: "maclaw-b", Role: GroupRoleSpeak, Trusted: false, SecurityGroupID: "team-a"}
	if !ShouldAutoAcceptGroupInvitation("ask_always", true, "team-a", inv) {
		t.Fatal("same security group should auto-accept when explicitly allowed")
	}
	if ShouldAutoAcceptGroupInvitation("ask_always", false, "team-a", inv) {
		t.Fatal("ask_always should not auto-accept without same-security-group allowance")
	}
	if !ShouldAutoAcceptGroupInvitation("trusted_auto", false, "team-b", GroupInvitation{Trusted: true, Role: GroupRoleSpeak}) {
		t.Fatal("trusted_auto should auto-accept trusted invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("auto_trusted", false, "team-b", GroupInvitation{Trusted: true, Role: GroupRoleSpeak}) {
		t.Fatal("auto_trusted should auto-accept trusted invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("observe_auto", false, "team-b", GroupInvitation{Role: GroupRoleObserve}) {
		t.Fatal("observe_auto should auto-accept observe-only invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("observe_only_auto", false, "team-b", GroupInvitation{Role: GroupRoleObserve}) {
		t.Fatal("observe_only_auto should auto-accept observe-only invitations")
	}
	if ShouldAutoAcceptGroupInvitation("reject_all", true, "team-a", inv) {
		t.Fatal("reject_all should override same-security-group auto-accept")
	}
}

func TestGroupProfileDiscoveryViewClampsContributionScore(t *testing.T) {
	profile := GroupProfile{AgentID: "maclaw-a", ContributionScore: 1.5, ContributionEvidence: -2}
	view := profile.DiscoveryView("")
	if view.ContributionScore != 1 || view.ContributionEvidence != 0 {
		t.Fatalf("contribution fields = score %.2f evidence %d, want clamped", view.ContributionScore, view.ContributionEvidence)
	}
}
