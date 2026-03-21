package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	AvgRating   float64  `json:"avg_rating"`
	RatingCount int      `json:"rating_count"`
}

// cachedSearchResult holds a cached search response with expiry.
type cachedSearchResult struct {
	results   []HubSkillMeta
	expiresAt time.Time
}

const (
	maxCacheEntries = 100
	maxDownloadSize = 1 << 20 // 1 MB
)

// SkillHubClient queries the hub's own SkillHub API for skill search, download, and recommendations.
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

// hubBaseURL returns the hubcenter's API base URL from app config.
func (c *SkillHubClient) hubBaseURL() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if url == "" {
		url = defaultRemoteHubCenterURL
	}
	return strings.TrimRight(url, "/")
}

// hubSkillSearchResult mirrors the hub's SkillSearchResult JSON.
type hubSkillSearchResult struct {
	Skills []hubSkillItem `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

// hubSkillItem mirrors the hub's HubSkillMeta JSON.
type hubSkillItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"`
	Downloads   int      `json:"downloads"`
	AvgRating   float64  `json:"avg_rating"`
	RatingCount int      `json:"rating_count"`
}

// hubSkillFull mirrors the hub's HubSkillFull JSON for download.
type hubSkillFull struct {
	hubSkillItem
	Triggers     []string           `json:"triggers"`
	Steps        []hubSkillStep     `json:"steps"`
	Manifest     hubSkillManifest   `json:"manifest,omitempty"`
	Files        map[string]string  `json:"files,omitempty"`          // path → base64 content
	AgentSkillMD string             `json:"agent_skill_md,omitempty"` // SKILL.md content
}

// hubSkillManifest mirrors the hub's SkillManifest.
type hubSkillManifest struct {
	MinMaclawVersion string              `json:"min_maclaw_version,omitempty"`
	RequiredMCP      []string            `json:"required_mcp,omitempty"`
	Permissions      []string            `json:"permissions,omitempty"`
	Dependencies     []hubSkillDependency `json:"dependencies,omitempty"`
	Compatibility    string              `json:"compatibility,omitempty"`
}

// hubSkillDependency mirrors the hub's SkillDependency.
type hubSkillDependency struct {
	Type    string `json:"type"`              // "pip", "npm", "brew", "apt", "binary"
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type hubSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

func hubItemToMeta(item hubSkillItem, hubURL string) HubSkillMeta {
	return HubSkillMeta{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		Tags:        item.Tags,
		Version:     item.Version,
		Author:      item.Author,
		TrustLevel:  item.TrustLevel,
		Downloads:   item.Downloads,
		HubURL:      hubURL,
		AvgRating:   item.AvgRating,
		RatingCount: item.RatingCount,
	}
}

// Search queries the hub's SkillHub API and returns matching skills.
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

	base := c.hubBaseURL()
	if base == "" {
		return nil, fmt.Errorf("hub URL not configured")
	}

	endpoint := base + "/api/v1/skills/search?q=" + url.QueryEscape(query) + "&page=1"
	var result hubSkillSearchResult
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return nil, fmt.Errorf("搜索 SkillHub 失败: %v", err)
	}

	skills := make([]HubSkillMeta, 0, len(result.Skills))
	for _, item := range result.Skills {
		skills = append(skills, hubItemToMeta(item, base))
	}

	c.cacheResults(query, skills)
	return skills, nil
}

// File packaging constraints.
const (
	maxSingleFileSize = 256 << 10 // 256 KB
	maxTotalFileSize  = 1 << 20   // 1 MB
)

// allowedFileExts is the whitelist of file extensions for packaged files.
var allowedFileExts = map[string]bool{
	".sh": true, ".py": true, ".js": true, ".yaml": true,
	".yml": true, ".json": true, ".txt": true, ".md": true,
}

// Install downloads a Skill from the hub, extracts bundled files to
// ~/.maclaw/skills/<name>/, installs declared dependencies, and converts
// the skill to an NLSkillEntry.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	base := c.hubBaseURL()
	if base == "" {
		return nil, fmt.Errorf("hub URL not configured")
	}

	endpoint := base + "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	var full hubSkillFull
	if err := c.getJSON(ctx, endpoint, &full); err != nil {
		return nil, fmt.Errorf("下载 Skill 失败: %v", err)
	}

	steps := make([]NLSkillStep, 0, len(full.Steps))
	for _, s := range full.Steps {
		steps = append(steps, NLSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		})
	}

	status := "active"

	// Extract bundled files to ~/.maclaw/skills/<name>/
	if len(full.Files) > 0 {
		if err := c.extractFiles(full.Name, full.Files); err != nil {
			// Non-fatal: mark as needs_setup but continue.
			status = "needs_setup"
		}
	}

	// Install declared dependencies.
	if len(full.Manifest.Dependencies) > 0 {
		if err := c.installDependencies(full.Manifest.Dependencies); err != nil {
			status = "needs_setup"
		}
	}

	return &NLSkillEntry{
		Name:          full.Name,
		Description:   full.Description,
		Triggers:      full.Triggers,
		Steps:         steps,
		Status:        status,
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: base,
		HubSkillID:    full.ID,
		HubVersion:    full.Version,
		TrustLevel:    full.TrustLevel,
	}, nil
}

// extractFiles writes bundled files (base64-encoded) to ~/.maclaw/skills/<name>/.
// Validates extension whitelist, size limits, and path safety.
func (c *SkillHubClient) extractFiles(skillName string, files map[string]string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	skillDir := filepath.Join(home, ".maclaw", "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}

	var totalSize int64
	for relPath, b64Content := range files {
		// Validate extension.
		ext := strings.ToLower(filepath.Ext(relPath))
		if !allowedFileExts[ext] {
			continue // skip disallowed extensions silently
		}

		data, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			continue
		}

		// Size checks.
		if int64(len(data)) > maxSingleFileSize {
			continue
		}
		totalSize += int64(len(data))
		if totalSize > maxTotalFileSize {
			return fmt.Errorf("total file size exceeds 1MB limit")
		}

		// Sanitize path — prevent directory traversal and absolute paths.
		clean := filepath.ToSlash(filepath.Clean(relPath))
		if strings.Contains(clean, "..") || filepath.IsAbs(relPath) || strings.HasPrefix(clean, "/") {
			continue
		}

		dest := filepath.Join(skillDir, filepath.FromSlash(clean))
		// Double-check the resolved path is still under skillDir.
		if !strings.HasPrefix(dest, skillDir+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(dest, data, 0o644)
	}
	return nil
}

// installDependencies attempts to install declared runtime dependencies.
// Each install command has a 60-second timeout. Returns an error if any
// dependency fails; individual failures are tolerated.
func (c *SkillHubClient) installDependencies(deps []hubSkillDependency) error {
	var failures []string
	for _, dep := range deps {
		var args []string
		var bin string
		switch dep.Type {
		case "pip":
			bin = "pip"
			pkg := dep.Name
			if dep.Version != "" {
				pkg += dep.Version // e.g. "requests>=2.0"
			}
			args = []string{"install", pkg}
		case "npm":
			bin = "npm"
			pkg := dep.Name
			if dep.Version != "" {
				pkg += "@" + dep.Version
			}
			args = []string{"install", "-g", pkg}
		case "binary":
			// Just check if binary exists; can't auto-install.
			if _, err := exec.LookPath(dep.Name); err != nil {
				failures = append(failures, fmt.Sprintf("binary %q not found", dep.Name))
			}
			continue
		case "brew":
			if runtime.GOOS != "darwin" {
				continue
			}
			bin = "brew"
			args = []string{"install", dep.Name}
		case "apt":
			if runtime.GOOS != "linux" {
				continue
			}
			bin = "apt-get"
			args = []string{"install", "-y", dep.Name}
		default:
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, bin, args...)
		hideCommandWindow(cmd)
		err := cmd.Run()
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s %s: %v", dep.Type, dep.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("dependency install failures: %s", strings.Join(failures, "; "))
	}
	return nil
}

// CheckUpdate checks whether a Skill has a newer version available on the hub.
func (c *SkillHubClient) CheckUpdate(ctx context.Context, skillID string, currentVersion string) (*HubSkillMeta, error) {
	base := c.hubBaseURL()
	if base == "" {
		return nil, nil
	}

	endpoint := base + "/api/v1/skills/" + url.PathEscape(skillID)
	var item hubSkillItem
	if err := c.getJSON(ctx, endpoint, &item); err != nil {
		return nil, nil
	}

	if item.Version == "" || item.Version == currentVersion {
		return nil, nil
	}

	meta := hubItemToMeta(item, base)
	return &meta, nil
}

// Rate submits a rating for a skill to the hub.
func (c *SkillHubClient) Rate(ctx context.Context, skillID string, maclawID string, score int) error {
	base := c.hubBaseURL()
	if base == "" {
		return fmt.Errorf("hub URL not configured")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"maclaw_id": maclawID,
		"score":     score,
	})

	endpoint := base + "/api/v1/skills/" + url.PathEscape(skillID) + "/rate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d rating skill %s", resp.StatusCode, skillID)
	}
	return nil
}

// Publish publishes a local skill to the hub's SkillHub.
func (c *SkillHubClient) Publish(ctx context.Context, full hubSkillFull) error {
	base := c.hubBaseURL()
	if base == "" {
		return fmt.Errorf("hub URL not configured")
	}

	body, err := json.Marshal(full)
	if err != nil {
		return err
	}

	endpoint := base + "/api/v1/skills"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d publishing skill", resp.StatusCode)
	}
	return nil
}

// RefreshRecommendations fetches popular skills and caches them.
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	base := c.hubBaseURL()
	if base == "" {
		return nil
	}

	endpoint := base + "/api/v1/skills/popular"
	var items []hubSkillItem
	if err := c.getJSON(ctx, endpoint, &items); err != nil {
		return err
	}

	skills := make([]HubSkillMeta, 0, len(items))
	for _, item := range items {
		skills = append(skills, hubItemToMeta(item, base))
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
// Helpers
// ---------------------------------------------------------------------------

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
