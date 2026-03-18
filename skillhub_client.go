package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// HubSkillMeta is the client-side Skill metadata returned from SkillHub searches.
// It includes a HubURL field to track which Hub the result came from.
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

// hubSearchResponse is the JSON structure returned by Hub search endpoints.
type hubSearchResponse struct {
	Skills []HubSkillMeta `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

// hubDownloadResponse is the JSON structure returned by Hub download endpoints.
type hubDownloadResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Version     string                   `json:"version"`
	Author      string                   `json:"author"`
	TrustLevel  string                   `json:"trust_level"`
	Triggers    []string                 `json:"triggers"`
	Steps       []map[string]interface{} `json:"steps"`
}

const maxCacheEntries = 100 // prevent unbounded cache growth

// SkillHubClient queries multiple SkillHubs concurrently, caches results,
// and downloads/installs Skills.
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
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Search queries the built-in ClawSkillHub API and returns results.
// Config entries are mirrors for download acceleration only — search is always
// performed against the primary ClawSkillHub endpoint.
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

	// Search always uses the built-in ClawSkillHub API (the only source with a
	// real REST search endpoint). Config entries are mirrors used for download
	// acceleration only.
	const primaryHubURL = "https://clawskillhub.com"

	searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	skills, err := c.searchSkillHubSpace(searchCtx, primaryHubURL, query)
	if err != nil {
		return nil, fmt.Errorf("搜索 ClawSkillHub 失败: %v", err)
	}

	// Tag every result with the primary hub URL so Install knows the origin.
	for i := range skills {
		skills[i].HubURL = primaryHubURL
	}

	merged := skills

	// Cache the result (evict oldest entries if cache is full).
	c.mu.Lock()
	if len(c.cache) >= maxCacheEntries {
		// Evict expired entries first.
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expiresAt) {
				delete(c.cache, k)
			}
		}
		// If still full, evict the entry closest to expiry.
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
		results:   merged,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.mu.Unlock()

	return merged, nil
}

// Install downloads a Skill from the specified Hub and converts it to an NLSkillEntry.
// On failure it falls back to other Hubs sorted by latency.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	// Load config once for mirror URLs.
	cfg, _ := c.app.LoadConfig()

	// Build download order: configured mirrors first (for acceleration),
	// then the primary ClawSkillHub as fallback.
	const primaryHub = "https://clawskillhub.com"
	var hubURLs []string
	for _, entry := range cfg.SkillHubURLs {
		hubURLs = append(hubURLs, entry.URL)
	}
	// Ensure primary hub is always in the list as final fallback.
	hasPrimary := false
	for _, u := range hubURLs {
		if u == primaryHub {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		hubURLs = append(hubURLs, primaryHub)
	}

	var lastErr error
	for _, hub := range hubURLs {
		hubType := c.hubTypeFromConfig(cfg, hub)
		// Primary hub is always skillhub_space type.
		if hub == primaryHub {
			hubType = "skillhub_space"
		}
		// Skip pure mirror entries — they don't have a download API.
		if hubType == "mirror" {
			continue
		}
		var entry *NLSkillEntry
		var err error
		if hubType == "skillhub_space" {
			entry, err = c.downloadSkillHubSpace(ctx, skillID, hub)
		} else {
			entry, err = c.downloadSkill(ctx, skillID, hub)
		}
		if err != nil {
			lastErr = err
			continue
		}
		return entry, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to install skill %s: %w", skillID, lastErr)
	}
	return nil, fmt.Errorf("no hubs available to install skill %s", skillID)
}

// getHubType returns the configured type for a given hub URL.
func (c *SkillHubClient) getHubType(hubURL string) string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return c.hubTypeFromConfig(cfg, hubURL)
}

// hubTypeFromConfig looks up the hub type from a pre-loaded config.
func (c *SkillHubClient) hubTypeFromConfig(cfg AppConfig, hubURL string) string {
	for _, entry := range cfg.SkillHubURLs {
		if entry.URL == hubURL {
			return entry.Type
		}
	}
	return ""
}

// CheckUpdate checks whether a Hub Skill has a newer version available.
// It queries all configured Hubs concurrently (8-second timeout per Hub),
// returning the first result where the Hub version differs from currentVersion.
// Returns nil, nil if versions match or no Hub is reachable.
func (c *SkillHubClient) CheckUpdate(ctx context.Context, skillID string, currentVersion string) (*HubSkillMeta, error) {
	const primaryHub = "https://clawskillhub.com"

	hubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	items, err := c.searchSkillHubSpace(hubCtx, primaryHub, skillID)
	if err != nil || len(items) == 0 {
		return nil, nil
	}

	for _, item := range items {
		if item.ID == skillID && item.Version != currentVersion {
			return &item, nil
		}
	}
	return nil, nil
}

// downloadSkill fetches a single Skill from a Hub and converts it to NLSkillEntry.
func (c *SkillHubClient) downloadSkill(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	dlCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub %s returned HTTP %d for skill %s", hubURL, resp.StatusCode, skillID)
	}

	var dl hubDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&dl); err != nil {
		return nil, fmt.Errorf("failed to decode skill response: %w", err)
	}

	// Convert steps from generic maps to NLSkillStep.
	steps := make([]NLSkillStep, 0, len(dl.Steps))
	for _, raw := range dl.Steps {
		step := NLSkillStep{}
		if action, ok := raw["action"].(string); ok {
			step.Action = action
		}
		if params, ok := raw["params"].(map[string]interface{}); ok {
			step.Params = params
		}
		if onErr, ok := raw["on_error"].(string); ok {
			step.OnError = onErr
		}
		steps = append(steps, step)
	}

	entry := &NLSkillEntry{
		Name:          dl.Name,
		Description:   dl.Description,
		Triggers:      dl.Triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    dl.ID,
		HubVersion:    dl.Version,
		TrustLevel:    dl.TrustLevel,
	}

	return entry, nil
}

// selectBestHub returns Hub URLs sorted by latency (lowest first) using PingSkillHub data.
// Pings are performed concurrently for better performance with multiple hubs.
func (c *SkillHubClient) selectBestHub(skillID string) []string {
	cfg, err := c.app.LoadConfig()
	if err != nil || len(cfg.SkillHubURLs) == 0 {
		return nil
	}

	type hubLatency struct {
		url     string
		latency int64
	}

	results := make([]hubLatency, len(cfg.SkillHubURLs))
	var wg sync.WaitGroup

	for i, entry := range cfg.SkillHubURLs {
		wg.Add(1)
		go func(idx int, hubURL string) {
			defer wg.Done()
			result := c.app.PingSkillHub(hubURL)
			online, _ := result["online"].(bool)
			var ms int64
			switch v := result["ms"].(type) {
			case int64:
				ms = v
			case int:
				ms = int64(v)
			}
			if !online {
				ms = 999999
			}
			results[idx] = hubLatency{url: hubURL, latency: ms}
		}(i, entry.URL)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})

	urls := make([]string, len(results))
	for i, e := range results {
		urls[i] = e.url
	}
	return urls
}

// mergeResults deduplicates skills from multiple Hubs by Skill ID,
// keeping the result from the Hub with the lowest latency.
func mergeResults(results map[string][]hubSearchResponse, latencies map[string]int64) []HubSkillMeta {
	// bestByID tracks the best (lowest latency) skill per ID.
	type bestEntry struct {
		skill   HubSkillMeta
		latency int64
	}
	bestByID := make(map[string]bestEntry)

	for hubURL, responses := range results {
		lat := latencies[hubURL]
		for _, resp := range responses {
			for _, sk := range resp.Skills {
				existing, found := bestByID[sk.ID]
				if !found || lat < existing.latency {
					bestByID[sk.ID] = bestEntry{skill: sk, latency: lat}
				}
			}
		}
	}

	merged := make([]HubSkillMeta, 0, len(bestByID))
	for _, entry := range bestByID {
		merged = append(merged, entry.skill)
	}
	return merged
}

// RefreshRecommendations fetches popular Skills from all configured Hubs
// and merges them into the in-memory recommendation index.
// Errors from individual Hubs are silently ignored (best-effort).
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	const primaryHub = "https://clawskillhub.com"

	hubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	skills, err := c.fetchSkillHubSpaceTrending(hubCtx, primaryHub)
	if err != nil {
		return nil
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
// 适配器方法: 标准 Hub / ClawHub / ClawHub 镜像
// ---------------------------------------------------------------------------

// searchStandard 查询标准 SkillHub API
func (c *SkillHubClient) searchStandard(ctx context.Context, hubURL, query string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var sr hubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}

	for i := range sr.Skills {
		sr.Skills[i].HubURL = hubURL
	}
	return sr.Skills, nil
}

// clawHubMirrorResponse 是 topclawhubskills.com 的搜索响应格式
type clawHubMirrorResponse struct {
	OK   bool `json:"ok"`
	Data []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Summary     string `json:"summary"`
		Downloads   int    `json:"downloads"`
		Stars       int    `json:"stars"`
		OwnerHandle string `json:"owner_handle"`
		IsCertified bool   `json:"is_certified"`
		ClawHubURL  string `json:"clawhub_url"`
	} `json:"data"`
	Total int `json:"total"`
}

// searchClawHubMirror 查询 topclawhubskills.com 风格的 API 并转换为 HubSkillMeta
func (c *SkillHubClient) searchClawHubMirror(ctx context.Context, hubURL, query string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/search?q=" + url.QueryEscape(query) + "&limit=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var mr clawHubMirrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}
	if !mr.OK {
		return nil, fmt.Errorf("mirror API returned ok=false")
	}

	skills := make([]HubSkillMeta, 0, len(mr.Data))
	for _, d := range mr.Data {
		trust := "community"
		if d.IsCertified {
			trust = "certified"
		}
		skills = append(skills, HubSkillMeta{
			ID:          d.Slug,
			Name:        d.DisplayName,
			Description: d.Summary,
			Author:      d.OwnerHandle,
			TrustLevel:  trust,
			Downloads:   d.Downloads,
			HubURL:      hubURL,
		})
	}
	return skills, nil
}

// clawHubSearchResponse 是 clawhub.ai 的搜索响应格式
type clawHubSearchResponse struct {
	Results []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Summary     string `json:"summary"`
		Stats       struct {
			Downloads int `json:"downloads"`
			Stars     int `json:"stars"`
		} `json:"stats"`
		Owner struct {
			Handle string `json:"handle"`
		} `json:"owner"`
	} `json:"results"`
}

// clawHubListResponse 是 clawhub.ai 的列表响应格式
type clawHubListResponse struct {
	Items []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Summary     string `json:"summary"`
		Stats       struct {
			Downloads int `json:"downloads"`
			Stars     int `json:"stars"`
		} `json:"stats"`
		Owner struct {
			Handle string `json:"handle"`
		} `json:"owner"`
	} `json:"items"`
	NextCursor interface{} `json:"nextCursor"`
}

// searchClawHub 查询 clawhub.ai 风格的 API 并转换为 HubSkillMeta
func (c *SkillHubClient) searchClawHub(ctx context.Context, hubURL, query string) ([]HubSkillMeta, error) {
	// 先尝试 /api/v1/search?q=...
	endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var sr clawHubSearchResponse
		if err := json.NewDecoder(resp.Body).Decode(&sr); err == nil && len(sr.Results) > 0 {
			skills := make([]HubSkillMeta, 0, len(sr.Results))
			for _, r := range sr.Results {
				skills = append(skills, HubSkillMeta{
					ID:          r.Slug,
					Name:        r.DisplayName,
					Description: r.Summary,
					Author:      r.Owner.Handle,
					TrustLevel:  "community",
					Downloads:   r.Stats.Downloads,
					HubURL:      hubURL,
				})
			}
			return skills, nil
		}
	}

	// 回退: 获取列表并在客户端过滤
	listEndpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills"
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, listEndpoint, nil)
	if err != nil {
		return nil, err
	}
	listReq.Header.Set("User-Agent", "MaClaw/1.0")

	listResp, err := c.client.Do(listReq)
	if err != nil {
		return nil, err
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", listResp.StatusCode)
	}

	var lr clawHubListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&lr); err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	var skills []HubSkillMeta
	for _, item := range lr.Items {
		// 空 query 时返回全部（用于推荐列表）
		if query == "" ||
			strings.Contains(strings.ToLower(item.DisplayName), queryLower) ||
			strings.Contains(strings.ToLower(item.Summary), queryLower) ||
			strings.Contains(strings.ToLower(item.Slug), queryLower) {
			skills = append(skills, HubSkillMeta{
				ID:          item.Slug,
				Name:        item.DisplayName,
				Description: item.Summary,
				Author:      item.Owner.Handle,
				TrustLevel:  "community",
				Downloads:   item.Stats.Downloads,
				HubURL:      hubURL,
			})
		}
	}
	return skills, nil
}

// fetchClawHubMirrorPopular 从 topclawhubskills.com 获取热门 Skill
func (c *SkillHubClient) fetchClawHubMirrorPopular(ctx context.Context, hubURL string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/top-downloads?limit=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var mr clawHubMirrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}

	skills := make([]HubSkillMeta, 0, len(mr.Data))
	for _, d := range mr.Data {
		trust := "community"
		if d.IsCertified {
			trust = "certified"
		}
		skills = append(skills, HubSkillMeta{
			ID:          d.Slug,
			Name:        d.DisplayName,
			Description: d.Summary,
			Author:      d.OwnerHandle,
			TrustLevel:  trust,
			Downloads:   d.Downloads,
			HubURL:      hubURL,
		})
	}
	return skills, nil
}

// ---------------------------------------------------------------------------
// 适配器: SkillHub.space / clawskillhub.com
// API 格式:
//   搜索: GET /api/skills?search=xxx&limit=20  (返回 JSON 数组)
//   列表: GET /api/skills?sort=latest&limit=N
//   热门: GET /api/skills/trending?limit=N
//   详情: GET /api/skills/{owner}/{slug}
//   下载: GET /api/skills/{owner}/{slug}/download/{version} (返回 markdown)
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

// skillHubSpaceToMeta converts a skillHubSpaceItem to HubSkillMeta.
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

// searchSkillHubSpace 查询 clawskillhub.com 风格的 API
func (c *SkillHubClient) searchSkillHubSpace(ctx context.Context, hubURL, query string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/skills?search=" + url.QueryEscape(query) + "&limit=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var items []skillHubSpaceItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, skillHubSpaceToMeta(item, hubURL))
	}
	return skills, nil
}

// fetchSkillHubSpaceTrending 获取 clawskillhub.com 的热门 Skill
func (c *SkillHubClient) fetchSkillHubSpaceTrending(ctx context.Context, hubURL string) ([]HubSkillMeta, error) {
	endpoint := strings.TrimRight(hubURL, "/") + "/api/skills/trending?limit=20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var items []skillHubSpaceItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, skillHubSpaceToMeta(item, hubURL))
	}
	return skills, nil
}

// downloadSkillHubSpace 从 clawskillhub.com 下载 Skill 并转换为 NLSkillEntry。
// 先搜索获取 owner handle，再获取详情（含版本列表），最后下载 skill.md。
func (c *SkillHubClient) downloadSkillHubSpace(ctx context.Context, skillSlug string, hubURL string) (*NLSkillEntry, error) {
	base := strings.TrimRight(hubURL, "/")

	// 1. 搜索获取 owner handle
	searchEndpoint := base + "/api/skills?search=" + url.QueryEscape(skillSlug) + "&limit=5"
	searchReq, err := http.NewRequestWithContext(ctx, http.MethodGet, searchEndpoint, nil)
	if err != nil {
		return nil, err
	}
	searchReq.Header.Set("User-Agent", "MaClaw/1.0")

	searchResp, err := c.client.Do(searchReq)
	if err != nil {
		return nil, err
	}
	defer searchResp.Body.Close()

	if searchResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d searching for skill %s", searchResp.StatusCode, skillSlug)
	}

	var searchItems []skillHubSpaceItem
	if err := json.NewDecoder(searchResp.Body).Decode(&searchItems); err != nil {
		return nil, err
	}

	// 精确匹配 slug，否则取第一个
	var matched *skillHubSpaceItem
	for i, item := range searchItems {
		if item.Slug == skillSlug {
			matched = &searchItems[i]
			break
		}
	}
	if matched == nil && len(searchItems) > 0 {
		matched = &searchItems[0]
	}
	if matched == nil {
		return nil, fmt.Errorf("skill %s not found on %s", skillSlug, hubURL)
	}

	ownerHandle := matched.Owner.Handle
	slug := matched.Slug

	// 2. 获取详情（含版本列表）
	detailEndpoint := base + "/api/skills/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug)
	detailReq, err := http.NewRequestWithContext(ctx, http.MethodGet, detailEndpoint, nil)
	if err != nil {
		return nil, err
	}
	detailReq.Header.Set("User-Agent", "MaClaw/1.0")

	detailResp, err := c.client.Do(detailReq)
	if err != nil {
		return nil, err
	}
	defer detailResp.Body.Close()

	if detailResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching skill detail %s/%s", detailResp.StatusCode, ownerHandle, slug)
	}

	var detail skillHubSpaceItem
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		return nil, err
	}

	version := "1.0.0"
	if len(detail.Versions) > 0 {
		version = detail.Versions[0].Version
	}

	// 3. 下载 skill.md（限制 1MB 防止异常响应）
	dlEndpoint := base + "/api/skills/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug) + "/download/" + url.PathEscape(version)
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
		return nil, fmt.Errorf("HTTP %d downloading skill %s v%s", dlResp.StatusCode, slug, version)
	}

	const maxBodySize = 1 << 20 // 1 MB
	body, err := io.ReadAll(io.LimitReader(dlResp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read skill download body: %w", err)
	}

	skillMD := string(body)

	// 从 skill.md 的 frontmatter 中提取 description（优先使用更详细的描述）
	description := detail.Description
	if strings.HasPrefix(skillMD, "---") {
		parts := strings.SplitN(skillMD, "---", 3)
		if len(parts) >= 3 {
			for _, line := range strings.Split(parts[1], "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					if d := strings.TrimSpace(strings.TrimPrefix(line, "description:")); d != "" {
						description = d
					}
					break
				}
			}
		}
	}

	trust := "community"
	if detail.IsVerified {
		trust = "verified"
	}

	entry := &NLSkillEntry{
		Name:          detail.Name,
		Description:   description,
		Triggers:      []string{slug},
		Steps:         []NLSkillStep{{Action: "skill_md", Params: map[string]interface{}{"content": skillMD}}},
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    slug,
		HubVersion:    version,
		TrustLevel:    trust,
	}

	return entry, nil
}
