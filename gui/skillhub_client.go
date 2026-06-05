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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
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
	maxDownloadSize = 5 << 20 // Skill JSON includes base64 file content; 1 MB packages expand beyond 1 MB on the wire.
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

func (c *SkillHubClient) selectBaseURL(ctx context.Context) (string, []string, error) {
	base, discovered, err := c.app.resolveHubCenterBaseURLCached(ctx, c.client)
	if err != nil {
		return "", nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return base, discovered, nil
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
	Triggers     []string          `json:"triggers"`
	Steps        []hubSkillStep    `json:"steps"`
	Manifest     hubSkillManifest  `json:"manifest,omitempty"`
	Files        map[string]string `json:"files,omitempty"`          // path -> base64 content
	AgentSkillMD string            `json:"agent_skill_md,omitempty"` // SKILL.md content
}

// hubSkillManifest mirrors the hub's SkillManifest.
type hubSkillManifest struct {
	MinMaclawVersion string               `json:"min_maclaw_version,omitempty"`
	RequiredMCP      []string             `json:"required_mcp,omitempty"`
	Permissions      []string             `json:"permissions,omitempty"`
	Dependencies     []hubSkillDependency `json:"dependencies,omitempty"`
	Compatibility    string               `json:"compatibility,omitempty"`
}

// hubSkillDependency mirrors the hub's SkillDependency.
type hubSkillDependency struct {
	Type    string `json:"type"` // "pip", "npm", "brew", "apt", "binary"
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type hubSkillStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`
	Name      string                 `json:"name,omitempty"`
	Condition string                 `json:"condition,omitempty"`
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

	path := "/api/v1/skills/search?q=" + url.QueryEscape(query) + "&page=1"
	var result hubSkillSearchResult
	base, _, err := c.getJSON(ctx, path, &result)
	if err != nil {
		return nil, fmt.Errorf("search SkillHub failed: %v", err)
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
// ~/.maclaw/data/skills/<name>/, and converts the skill to an NLSkillEntry.
// Dependency installation is intentionally deferred until after security scan.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*corelib.NLSkillEntry, error) {
	return c.InstallToDir(ctx, skillID, hubURL, "")
}

// InstallToDir downloads a Skill and extracts bundled files to targetDir.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<name>/.
// The returned entry's SkillDir is set to the actual extraction directory.
// It must not perform dependency installation before the caller scans the skill.
func (c *SkillHubClient) InstallToDir(ctx context.Context, skillID, hubURL, targetDir string) (*corelib.NLSkillEntry, error) {
	path := "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	var full hubSkillFull
	base, _, err := c.getJSONFromExplicitHubURL(ctx, hubURL, path, &full)
	if err != nil {
		return nil, fmt.Errorf("download skill %q from %s failed: %v", skillID, strings.TrimRight(hubURL, "/"), err)
	}

	steps := make([]corelib.NLSkillStep, 0, len(full.Steps))
	for _, s := range full.Steps {
		steps = append(steps, corelib.NLSkillStep{
			Action:    s.Action,
			Params:    s.Params,
			OnError:   s.OnError,
			Name:      s.Name,
			Condition: s.Condition,
		})
	}

	status := skillEntryStatusActive
	installSkillDir := targetDir
	if installSkillDir == "" && full.Name != "" {
		if skillsRoot, err := skill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, full.Name)
		}
	}
	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}

	// Extract bundled files to targetDir (or default skills dir).
	if len(full.Files) > 0 {
		if err := c.extractFiles(full.Name, full.Files, targetDir); err != nil {
			// Non-fatal: mark as needs_setup but continue.
			status = skillEntryStatusNeedsSetup
		}
	}

	// Skills downloaded from the configured hub (official store) should be
	// treated as "trusted" rather than "community". The hub server currently
	// hardcodes all published skills to "community", which causes the risk
	// assessor to escalate their risk level and block legitimate installs.
	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		Name:          full.Name,
		Description:   full.Description,
		Triggers:      full.Triggers,
		Steps:         steps,
		Status:        status.String(),
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        skillEntrySourceHub.String(),
		SourceProject: base,
		HubSkillID:    full.ID,
		HubVersion:    full.Version,
		TrustLevel:    trustLevel,
		SkillDir:      installSkillDir,
	}, nil
}

func craftToolStepsFromBundledSkillFiles(files map[string]string, skillDir string) []corelib.NLSkillStep {
	if len(files) == 0 {
		return nil
	}
	for _, key := range []string{"SKILL.md", "skill.md"} {
		b64, ok := files[key]
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(decoded) == 0 {
			continue
		}
		params := map[string]interface{}{
			"instructions":      string(decoded),
			"verification_mode": "artifact_optional",
			"register_policy":   "manual",
		}
		if strings.TrimSpace(skillDir) != "" {
			params["working_dir"] = skillDir
		}
		return []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: params,
		}}
	}
	return nil
}

// extractFiles writes bundled files (base64-encoded) to the specified targetDir.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<name>/.
// Validates extension whitelist, size limits, and path safety.
func (c *SkillHubClient) extractFiles(skillName string, files map[string]string, targetDir string) error {
	skillDir := targetDir
	if skillDir == "" {
		skillsRoot, err := skill.PrimarySkillsDir()
		if err != nil {
			return err
		}
		skillDir = filepath.Join(skillsRoot, skillName)
	}
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

		// Sanitize path to prevent directory traversal and absolute paths.
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
	path := "/api/v1/skills/" + url.PathEscape(skillID)
	var item hubSkillItem
	base, _, err := c.getJSON(ctx, path, &item)
	if err != nil {
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
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
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
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

// Publish publishes a local skill to the hub's SkillHub.
func (c *SkillHubClient) Publish(ctx context.Context, full hubSkillFull) error {
	base, discovered, err := c.selectBaseURL(ctx)
	if err != nil {
		return err
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
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

// RefreshRecommendations fetches popular skills and caches them.
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	var items []hubSkillItem
	base, _, err := c.getJSON(ctx, "/api/v1/skills/popular", &items)
	if err != nil {
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

func (c *SkillHubClient) getJSON(ctx context.Context, path string, dest interface{}) (string, []string, error) {
	return c.app.getHubCenterJSON(ctx, c.client, path, maxDownloadSize, dest)
}

func (c *SkillHubClient) getJSONFromExplicitHubURL(ctx context.Context, hubURL string, path string, dest interface{}) (string, []string, error) {
	base := strings.TrimSpace(hubURL)
	if base == "" {
		return c.getJSON(ctx, path, dest)
	}
	if authHeader := c.enterpriseHubAuthHeaderForBase(base); authHeader != "" {
		return c.getJSONFromExplicitHubURLWithAuth(ctx, base, path, dest, authHeader)
	}
	return c.app.getHubCenterJSONFromCandidates(ctx, c.client, []string{base}, path, maxDownloadSize, dest)
}

func (c *SkillHubClient) enterpriseHubAuthHeaderForBase(base string) string {
	if c == nil || c.app == nil {
		return ""
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	configured := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if configured == "" || !strings.EqualFold(configured, base) {
		return ""
	}
	if token := strings.TrimSpace(cfg.RemoteViewerToken); token != "" {
		return "Bearer " + token
	}
	return ""
}

func (c *SkillHubClient) getJSONFromExplicitHubURLWithAuth(ctx context.Context, base, path string, dest interface{}, authHeader string) (string, []string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", "MaClaw/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", []string{base}, fmt.Errorf("request hub JSON %s%s failed (%d): %s", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := readHubCenterJSONBody(resp.Body, maxDownloadSize)
	if err != nil {
		return "", []string{base}, fmt.Errorf("read hub JSON %s%s failed: %w", base, path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return "", []string{base}, fmt.Errorf("decode hub JSON %s%s failed after %d bytes: %w", base, path, len(data), err)
	}
	return base, []string{base}, nil
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
