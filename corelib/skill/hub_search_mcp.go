package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SearchMCPFiltered searches external sources for MCP Server configurations.
// allowedSources filters which sources to query. nil/empty = all allowed sources.
// Valid source values: "clawhub", "github".
// Note: "skillhub" is not searched for MCP (SkillHub only hosts Skills).
func (c *HubClient) SearchMCPFiltered(ctx context.Context, query string, allowedSources []string) []HubSearchResult {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	var results []HubSearchResult

	if isSourceAllowed("clawhub", allowedSources) && ctx.Err() == nil {
		results = append(results, c.SearchMCPClawHub(ctx, query)...)
	}

	if isSourceAllowed("github", allowedSources) && ctx.Err() == nil {
		results = append(results, c.SearchMCPGitHub(ctx, query)...)
	}

	return results
}

// SearchMCPClawHub searches ClawHub for MCP Server configurations.
// Returns nil on any error (non-fatal).
func (c *HubClient) SearchMCPClawHub(ctx context.Context, query string) []HubSearchResult {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&type=mcp&page=1",
		ClawHubMirrorURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var raw clawHubMCPSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	results := make([]HubSearchResult, 0, len(raw.Skills))
	for _, s := range raw.Skills {
		results = append(results, HubSearchResult{
			ID:             s.ID,
			Name:           s.Name,
			Description:    s.Description,
			Version:        s.Version,
			Author:         s.Author,
			Source:         "clawhub",
			CapabilityType: "mcp",
			InstallRef:     s.ID,
		})
	}
	return results
}

// SearchMCPGitHub searches GitHub for MCP Server configurations using
// the Repository Search API with topic:mcp-server.
// Returns nil on any error (non-fatal).
func (c *HubClient) SearchMCPGitHub(ctx context.Context, query string) []HubSearchResult {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	// Search GitHub repositories with topic:mcp-server
	searchQuery := fmt.Sprintf("%s topic:mcp-server", query)
	endpoint := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&per_page=10",
		url.QueryEscape(searchQuery))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.githubToken != "" {
		req.Header.Set("Authorization", "token "+c.githubToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var raw ghMCPRepoSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	results := make([]HubSearchResult, 0, len(raw.Items))
	for _, repo := range raw.Items {
		results = append(results, HubSearchResult{
			ID:             repo.FullName,
			Name:           repo.Name,
			Description:    repo.Description,
			Author:         repo.Owner.Login,
			Source:         "github",
			CapabilityType: "mcp",
			RepoURL:        repo.HTMLURL,
			InstallRef:     repo.FullName,
		})
	}
	return results
}

// --- Response types for MCP search ---

type clawHubMCPSearchResponse struct {
	Skills []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Author      string `json:"author"`
	} `json:"skills"`
}

type ghMCPRepoSearchResponse struct {
	Items []struct {
		Name        string `json:"name"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"items"`
}
