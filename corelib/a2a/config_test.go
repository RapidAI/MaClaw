package a2a

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNewHubClientFromConfigRequiresCurrentHubCredentials(t *testing.T) {
	if _, err := NewHubClientFromConfig(corelib.AppConfig{}); err == nil {
		t.Fatal("expected missing hub URL error")
	}
	if _, err := NewHubClientFromConfig(corelib.AppConfig{RemoteHubURL: "https://hub.example.com"}); err == nil {
		t.Fatal("expected missing token error")
	}
	client, err := NewHubClientFromConfig(corelib.AppConfig{RemoteHubURL: "https://hub.example.com/", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-1"})
	if err != nil {
		t.Fatalf("NewHubClientFromConfig returned error: %v", err)
	}
	if client.baseURL != "https://hub.example.com" || client.token != "machine-token" || client.machineID != "machine-1" {
		t.Fatalf("client = %+v", client)
	}
}

func TestNewHubClientFromConfigFallsBackToRemoteClientID(t *testing.T) {
	client, err := NewHubClientFromConfig(corelib.AppConfig{RemoteHubURL: "https://hub.example.com/", RemoteMachineToken: "machine-token", RemoteClientID: "client-1"})
	if err != nil {
		t.Fatalf("NewHubClientFromConfig returned error: %v", err)
	}
	if client.machineID != "client-1" {
		t.Fatalf("client machineID = %q, want client-1", client.machineID)
	}
}

func TestBuildGroupProfileFromConfig(t *testing.T) {
	now := time.Date(2026, 5, 3, 1, 2, 3, 0, time.UTC)
	cfg := corelib.AppConfig{
		RemoteMachineID:          "machine-1",
		RemoteNickname:           "Desk MaClaw",
		MaclawRoleName:           "Refactor Expert",
		MaclawLLMCurrentProvider: "Hub LLM",
		GroupDiscussion: corelib.GroupDiscussionConfig{
			Enabled:              true,
			Discoverable:         true,
			Availability:         "available",
			Skills:               []string{"go", "security"},
			Languages:            []string{"zh-Hans", "en"},
			ModelVisibility:      "provider_alias",
			ContributionScore:    0.82,
			ContributionEvidence: 5,
		},
	}
	profile, err := BuildGroupProfileFromConfig(cfg, now)
	if err != nil {
		t.Fatalf("BuildGroupProfileFromConfig returned error: %v", err)
	}
	if profile.AgentID != "machine-1" || profile.DisplayName != "Refactor Expert" {
		t.Fatalf("profile identity = %+v", profile)
	}
	if !profile.Discoverable || !profile.Available {
		t.Fatalf("profile availability = %+v", profile)
	}
	if profile.ModelClass != "provider:Hub LLM" {
		t.Fatalf("ModelClass = %q", profile.ModelClass)
	}
	if len(profile.Skills) != 2 || profile.UpdatedAt != now {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.ContributionScore != 0.82 || profile.ContributionEvidence != 5 {
		t.Fatalf("contribution fields = %.2f/%d", profile.ContributionScore, profile.ContributionEvidence)
	}
}

func TestBuildGroupProfileFromConfigHidesContributionWhenCrossAgentExperienceDisabled(t *testing.T) {
	disabled := false
	cfg := corelib.AppConfig{
		RemoteMachineID: "machine-1",
		GroupDiscussion: corelib.GroupDiscussionConfig{
			Enabled:                 true,
			Discoverable:            true,
			Availability:            "available",
			UseCrossAgentExperience: &disabled,
			ContributionScore:       0.91,
			ContributionEvidence:    9,
		},
	}
	profile, err := BuildGroupProfileFromConfig(cfg, time.Time{})
	if err != nil {
		t.Fatalf("BuildGroupProfileFromConfig returned error: %v", err)
	}
	if profile.ContributionScore != 0 || profile.ContributionEvidence != 0 {
		t.Fatalf("contribution fields should be hidden when disabled: %.2f/%d", profile.ContributionScore, profile.ContributionEvidence)
	}
}

func TestBuildGroupProfileFromConfigHidesWhenDisabled(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteClientID: "client-1",
		GroupDiscussion: corelib.GroupDiscussionConfig{
			Enabled:         false,
			Discoverable:    true,
			ModelVisibility: "hidden",
		},
	}
	profile, err := BuildGroupProfileFromConfig(cfg, time.Time{})
	if err != nil {
		t.Fatalf("BuildGroupProfileFromConfig returned error: %v", err)
	}
	if profile.Discoverable || profile.ModelClass != "" {
		t.Fatalf("profile should be hidden-ish when disabled: %+v", profile)
	}
}
