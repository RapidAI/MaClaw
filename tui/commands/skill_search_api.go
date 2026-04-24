package commands

// skill_search_api.go provides exported search functions for agent tool use.
// These delegate to the shared corelib/skill.HubClient for actual HTTP logic.

import (
	"context"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillSearchResult is a unified search result for agent tool use.
type SkillSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "skillhub", "clawhub", "github"
}

// ResolveHubCenterURL returns the configured HubCenter URL (exported wrapper).
func ResolveHubCenterURL() string {
	return resolveHubCenterURL()
}

// SearchSkillMarket queries SkillHub and returns results.
// Kept for backward compatibility — delegates to the shared HubClient.
func SearchSkillMarket(baseURL, query string, topN int) ([]SkillSearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := skill.NewHubClient()
	results := client.SearchSkillHub(ctx, baseURL, query)

	var out []SkillSearchResult
	for _, r := range results {
		out = append(out, SkillSearchResult{
			Name:        r.Name,
			Description: r.Description,
			Source:      r.Source,
		})
	}
	return out, nil
}

// SearchSkillHub queries SkillHub + ClawHub and returns merged results.
// Uses the shared corelib/skill.HubClient.
func SearchSkillHub(query string) ([]SkillSearchResult, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := skill.NewHubClient()
	results := client.SearchAll(ctx, hubURL, query)

	var out []SkillSearchResult
	for _, r := range results {
		out = append(out, SkillSearchResult{
			Name:        r.Name,
			Description: r.Description,
			Source:      r.Source,
		})
	}
	return out, nil
}
