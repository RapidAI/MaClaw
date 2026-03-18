package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HubSkillMeta is the client-side Skill metadata returned from SkillHub searches.
type HubSkillMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"`
	Downloads   int      `json:"downloads"`
	HubURL      string   `json:"hub_url"`
}

// cachedSearchResult holds a cached search response with expiry.
type cachedSearchResult struct {
	results   []HubSkillMeta
	expiresAt time.Time
}

const (
	maxCacheEntries = 100
	primaryHubURL   = "https://clawskillhub.com"
	maxDownloadSize = 1 << 20 // 1 MB
)

// SkillHubClient queries ClawSkillHub for skill search, download, and recommendations.
type SkillHubClient struct {
	app      *App
	mu       sync.RWMutex
	cache    map[string]cachedSearchResult
	cacheTTL time.Duration
	recIndex []HubSkillMeta
	client   *http.Client
}

// NewSkillHubClient creates a new SkillHubClient with default settings.
func NewSkillHubClient(app *App) *SkillHubClient {
	return &SkillHubClient{
		app:      app,
		cache:    make(map[string]cachedSearchResult),
		cacheTTL: 5 * time.Minute,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Search queries ClawSkillHub and returns matching skills.
func (c *SkillHubClient) Search(ctx context.Context, query string) ([]HubSkillMeta, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Check cache first.
	c.mu.RLock()
	if cached, ok := c.cache[query]; ok && time.Now().Before(cached.expiresAt) {
		results := cached.results
		c.mu.RUnlock()
		return results, nil
	}
	c.mu.RUnlock()

	skills, err := c.searchSkillHubSpace(ctx, primaryHubURL, query)
	if err != nil {
		return nil, fmt.Errorf("搜索 ClawSkillHub 失败: %v", err)
	}

	c.cacheResults(query, skills)
	return skills, nil
}

// Install downloads a Skill from ClawSkillHub and converts it to an NLSkillEntry.
// The hubURL parameter is accepted for API compatibility but ignored — all
// downloads go through primaryHubURL.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	return c.downloadSkillHubSpace(ctx, skillID, primaryHubURL)
}

// CheckUpdate checks whether a Skill has a newer version available on ClawSkillHub.
// Returns the updated meta if a newer version exists, nil otherwise.
func (c *SkillHubClient) CheckUpdate(ctx context.Context, skillID string, currentVersion string) (*HubSkillMeta, error) {
	detail, _, err := c.fetchSkillDetail(ctx, skillID, primaryHubURL)
	if err != nil || detail == nil {
		return nil, nil
	}

	latestVersion := ""
	if len(detail.Versions) > 0 {
		latestVersion = detail.Versions[0].Version
	}
	if latestVersion == "" || latestVersion == currentVersion {
		return nil, nil
	}

	meta := skillHubSpaceToMeta(*detail, primaryHubURL)
	meta.Version = latestVersion
	return &meta, nil
}

// RefreshRecommendations fetches trending skills and caches them.
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	skills, err := c.fetchSkillHubSpaceTrending(ctx, primaryHubURL)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.recIndex = skills
	c.mu.Unlock()
	return nil
}

// GetRecommendations returns the locally cached recommendation list (thread-safe).
func (c *SkillHubClient) GetRecommendations() []HubSkillMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]HubSkillMeta, len(c.recIndex))
	copy(result, c.recIndex)
	return result
}

// ---------------------------------------------------------------------------
// ClawSkillHub (clawskillhub.com / skillhub.space) adapter
// ---------------------------------------------------------------------------

// skillHubSpaceItem 是 clawskillhub.com 返回的单个 Skill 结构
type skillHubSpaceItem struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	Description     string   `json:"description"`
	IsVerified      bool     `json:"isVerified"`
	Stars           int      `json:"stars"`
	Downloads       int      `json:"downloads"`
	Tags            []string `json:"tags"`
	ValidationScore int      `json:"validationScore"`
	Owner           struct {
		Handle string `json:"handle"`
	} `json:"owner"`
	Versions []struct {
		Version string `json:"version"`
	} `json:"versions,omitempty"`
}

func skillHubSpaceToMeta(item skillHubSpaceItem, hubURL string) HubSkillMeta {
	trust := "community"
	if item.IsVerified {
		trust = "verified"
	}
	return HubSkillMeta{
		ID:          item.Slug,
		Name:        item.Name,
		Description: item.Description,
		Author:      item.Owner.Handle,
		TrustLevel:  trust,
		Downloads:   item.Downloads,
		Tags:        item.Tags,
		HubURL:      hubURL,
	}
}

// searchSkillHubSpace queries GET /api/skills?search=xxx&limit=20
func (c *SkillHubClient) searchSkillHubSpace(ctx context.Context, hubURL, query string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/skills?search=" + url.QueryEscape(query) + "&limit=20"
	var items []skillHubSpaceItem
	if err := c.getJSON(ctx, endpoint, &items); err != nil {
		return nil, err
	}
	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, skillHubSpaceToMeta(item, hubURL))
	}
	return skills, nil
}

// fetchSkillHubSpaceTrending queries GET /api/skills/trending?limit=20
func (c *SkillHubClient) fetchSkillHubSpaceTrending(ctx context.Context, hubURL string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/skills/trending?limit=20"
	var items []skillHubSpaceItem
	if err := c.getJSON(ctx, endpoint, &items); err != nil {
		return nil, err
	}
	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, skillHubSpaceToMeta(item, hubURL))
	}
	return skills, nil
}

// fetchSkillDetail searches for a skill by slug and fetches its detail (with versions).
// Returns the detail item, the owner handle, and any error.
func (c *SkillHubClient) fetchSkillDetail(ctx context.Context, skillSlug string, hubURL string) (*skillHubSpaceItem, string, error) {
	base := strings.TrimRight(hubURL, "/")

	// Search to find the owner handle for this slug.
	searchEndpoint := base + "/api/skills?search=" + url.QueryEscape(skillSlug) + "&limit=10"
	var searchItems []skillHubSpaceItem
	if err := c.getJSON(ctx, searchEndpoint, &searchItems); err != nil {
		return nil, "", err
	}

	// Require exact slug match — fallback to first result is dangerous
	// because it could install a completely unrelated skill.
	var matched *skillHubSpaceItem
	for i, item := range searchItems {
		if item.Slug == skillSlug {
			matched = &searchItems[i]
			break
		}
	}
	if matched == nil {
		return nil, "", fmt.Errorf("skill %q not found on %s", skillSlug, hubURL)
	}

	ownerHandle := matched.Owner.Handle
	slug := matched.Slug

	// Fetch detail with version list.
	detailEndpoint := base + "/api/skills/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug)
	var detail skillHubSpaceItem
	if err := c.getJSON(ctx, detailEndpoint, &detail); err != nil {
		return nil, "", fmt.Errorf("获取 skill 详情失败 %s/%s: %v", ownerHandle, slug, err)
	}

	return &detail, ownerHandle, nil
}

// downloadSkillHubSpace downloads a skill from clawskillhub.com.
// Flow: search → detail (get owner + version) → download skill.md
func (c *SkillHubClient) downloadSkillHubSpace(ctx context.Context, skillSlug string, hubURL string) (*NLSkillEntry, error) {
	detail, ownerHandle, err := c.fetchSkillDetail(ctx, skillSlug, hubURL)
	if err != nil {
		return nil, err
	}

	version := "1.0.0"
	if len(detail.Versions) > 0 {
		version = detail.Versions[0].Version
	}

	// Download skill.md content.
	base := strings.TrimRight(hubURL, "/")
	dlEndpoint := base + "/api/skills/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(detail.Slug) + "/download/" + url.PathEscape(version)

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlEndpoint, nil)
	if err != nil {
		return nil, err
	}
	dlReq.Header.Set("User-Agent", "MaClaw/1.0")

	dlResp, err := c.client.Do(dlReq)
	if err != nil {
		return nil, err
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d downloading skill %s v%s", dlResp.StatusCode, detail.Slug, version)
	}

	body, err := io.ReadAll(io.LimitReader(dlResp.Body, maxDownloadSize))
	if err != nil {
		return nil, fmt.Errorf("读取 skill 内容失败: %w", err)
	}

	skillMD := string(body)
	description := extractDescription(skillMD, detail.Description)

	trust := "community"
	if detail.IsVerified {
		trust = "verified"
	}

	return &NLSkillEntry{
		Name:          detail.Name,
		Description:   description,
		Triggers:      []string{detail.Slug},
		Steps:         []NLSkillStep{{Action: "skill_md", Params: map[string]interface{}{"content": skillMD}}},
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    detail.Slug,
		HubVersion:    version,
		TrustLevel:    trust,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getJSON performs a GET request and decodes the JSON response into dest.
func (c *SkillHubClient) getJSON(ctx context.Context, endpoint string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxDownloadSize)).Decode(dest)
}

// extractDescription extracts a description from skill.md frontmatter,
// falling back to the provided default.
func extractDescription(skillMD, fallback string) string {
	if !strings.HasPrefix(skillMD, "---") {
		return fallback
	}
	parts := strings.SplitN(skillMD, "---", 3)
	if len(parts) < 3 {
		return fallback
	}
	for _, line := range strings.Split(parts[1], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			if d := strings.TrimSpace(strings.TrimPrefix(line, "description:")); d != "" {
				return d
			}
		}
	}
	return fallback
}

// cacheResults stores search results with LRU-style eviction.
func (c *SkillHubClient) cacheResults(query string, results []HubSkillMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.cache) >= maxCacheEntries {
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expiresAt) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= maxCacheEntries {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range c.cache {
				if oldestKey == "" || v.expiresAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.expiresAt
				}
			}
			delete(c.cache, oldestKey)
		}
	}
	c.cache[query] = cachedSearchResult{
		results:   results,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
}
