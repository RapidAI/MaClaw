package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// SkillSearchResult is a unified search result for agent tool use.
type SkillSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "skillmarket", "skillhub", "github"
}

// ResolveHubCenterURL returns the configured HubCenter URL (exported wrapper).
func ResolveHubCenterURL() string {
	return resolveHubCenterURL()
}

// SearchSkillMarket queries the HubCenter SkillMarket search API.
func SearchSkillMarket(baseURL, query string, topN int) ([]SkillSearchResult, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("hubcenter URL empty")
	}
	endpoint := fmt.Sprintf("%s/api/v1/skillmarket/search?q=%s&top_n=%d",
		baseURL, url.QueryEscape(query), topN)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var raw struct {
		Results []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, err
	}

	var out []SkillSearchResult
	for _, r := range raw.Results {
		out = append(out, SkillSearchResult{Name: r.Name, Description: r.Description, Source: "skillmarket"})
	}
	return out, nil
}

// SearchSkillHub queries the HubCenter SkillHub search API and returns results.
func SearchSkillHub(query string) ([]SkillSearchResult, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	if hubURL == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}

	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1",
		hubURL, url.QueryEscape(query))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result hubSearchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}

	var out []SkillSearchResult
	for _, s := range result.Skills {
		out = append(out, SkillSearchResult{Name: s.Name, Description: s.Description, Source: "skillhub"})
	}
	return out, nil
}
