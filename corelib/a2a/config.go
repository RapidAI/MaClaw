package a2a

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func NewHubClientFromConfig(cfg corelib.AppConfig, opts ...HubClientOption) (*HubClient, error) {
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return nil, fmt.Errorf("hub URL is not configured")
	}
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	if token == "" {
		return nil, fmt.Errorf("hub token is not configured")
	}
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	if machineID == "" {
		machineID = strings.TrimSpace(cfg.RemoteClientID)
	}
	allOpts := append([]HubClientOption{WithHubBearerToken(token), WithHubMachineID(machineID)}, opts...)
	return NewHubClient(cfg.RemoteHubURL, allOpts...)
}

func BuildGroupProfileFromConfig(cfg corelib.AppConfig, now time.Time) (GroupProfile, error) {
	gd := cfg.GroupDiscussion
	agentID := strings.TrimSpace(cfg.RemoteMachineID)
	if agentID == "" {
		agentID = strings.TrimSpace(cfg.RemoteClientID)
	}
	if agentID == "" {
		return GroupProfile{}, fmt.Errorf("remote machine id is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	displayName := strings.TrimSpace(cfg.MaclawRoleName)
	if displayName == "" {
		displayName = strings.TrimSpace(gd.DisplayName)
	}
	if displayName == "" {
		displayName = strings.TrimSpace(cfg.RemoteNickname)
	}
	if displayName == "" {
		displayName = "MaClaw"
	}
	availability := strings.TrimSpace(gd.Availability)
	if availability == "" {
		availability = "available"
	}
	profile := GroupProfile{
		AgentID:         agentID,
		DisplayName:     displayName,
		Skills:          append([]string(nil), gd.Skills...),
		Description:     strings.TrimSpace(gd.Description),
		ModelClass:      groupModelClassFromConfig(cfg),
		Languages:       append([]string(nil), gd.Languages...),
		SecurityGroupID: strings.TrimSpace(gd.SecurityGroupID),
		Discoverable:    gd.Enabled && gd.Discoverable,
		Available:       availability == "available",
		UpdatedAt:       now,
	}
	if gd.CrossAgentExperienceEnabled() {
		profile.ContributionScore = gd.ContributionScore
		profile.ContributionEvidence = gd.ContributionEvidence
	}
	return profile.DiscoveryView(gd.ModelVisibility), nil
}

func groupModelClassFromConfig(cfg corelib.AppConfig) string {
	visibility := strings.TrimSpace(cfg.GroupDiscussion.ModelVisibility)
	if visibility == "hidden" {
		return ""
	}
	provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.MaclawLLMModel)
	}
	if visibility == "provider_alias" && provider != "" {
		return "provider:" + provider
	}
	if strings.TrimSpace(cfg.MaclawLLMUrl) != "" || strings.TrimSpace(cfg.MaclawLLMModel) != "" || provider != "" {
		return "llm-ready"
	}
	return ""
}
