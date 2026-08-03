package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// HubSkillMeta is the client-side Skill metadata returned from SkillHub searches.
type HubSkillMeta struct {
	ID                           string                 `json:"id"`
	Name                         string                 `json:"name"`
	Description                  string                 `json:"description"`
	Tags                         []string               `json:"tags"`
	Version                      string                 `json:"version"`
	Author                       string                 `json:"author"`
	TrustLevel                   string                 `json:"trust_level"`
	Downloads                    int                    `json:"downloads"`
	HubURL                       string                 `json:"hub_url"`
	AvgRating                    float64                `json:"avg_rating"`
	RatingCount                  int                    `json:"rating_count"`
	ProductKind                  string                 `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                   `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                 `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                 `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                 `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                 `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                 `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                 `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                   `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                 `json:"artifact_contract_presentation,omitempty"`
}

type MaclawAppTestEvidence struct {
	RunID                 string         `json:"run_id,omitempty"`
	VerifiedAt            string         `json:"verified_at,omitempty"`
	DefinitionFingerprint string         `json:"definition_fingerprint,omitempty"`
	ArtifactPresent       bool           `json:"artifact_present,omitempty"`
	ArtifactName          string         `json:"artifact_name,omitempty"`
	OutputCount           int            `json:"output_count,omitempty"`
	PrimaryResult         string         `json:"primary_result,omitempty"`
	ResultPayload         map[string]any `json:"result_payload,omitempty"`
}

// cachedSearchResult holds a cached search response with expiry.
type cachedSearchResult struct {
	results   []HubSkillMeta
	expiresAt time.Time
}

const (
	maxCacheEntries = 100
	// Search/list/metadata only — keep tight so a runaway endpoint cannot pin RAM.
	maxSearchJSONSize = skill.MaxSkillHubSearchJSONBytes
	// Skill install JSON includes base64 file maps; multi-asset packages
	// (templates, fonts, SVGs) routinely exceed 5 MiB on the wire.
	maxDownloadSize = skill.MaxSkillPackageDownloadBytes
)

// SkillHubClient queries the hub's own SkillHub API for skill search, download, and recommendations.
type SkillHubClient struct {
	app           *App
	mu            sync.RWMutex
	cache         map[string]cachedSearchResult
	cacheTTL      time.Duration
	recIndex      []HubSkillMeta
	client        *http.Client // 10s timeout for search/metadata APIs
	installClient *http.Client // long timeout for multi-asset skill package downloads
}

// NewSkillHubClient creates a new SkillHubClient with default settings.
func NewSkillHubClient(app *App) *SkillHubClient {
	return &SkillHubClient{
		app:      app,
		cache:    make(map[string]cachedSearchResult),
		cacheTTL: 5 * time.Minute,
		client:   &http.Client{Timeout: 10 * time.Second},
		// installClient uses a longer timeout for multi-asset skill packages
		// (up to MaxSkillPackageDownloadBytes) on slow networks.
		installClient: &http.Client{Timeout: 180 * time.Second},
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
	ID                           string                 `json:"id"`
	SkillID                      string                 `json:"skill_id,omitempty"`
	SemVer                       string                 `json:"semver,omitempty"`
	Name                         string                 `json:"name"`
	Description                  string                 `json:"description"`
	Tags                         []string               `json:"tags"`
	Version                      string                 `json:"version"`
	Author                       string                 `json:"author"`
	TrustLevel                   string                 `json:"trust_level"`
	Downloads                    int                    `json:"downloads"`
	AvgRating                    float64                `json:"avg_rating"`
	RatingCount                  int                    `json:"rating_count"`
	ProductKind                  string                 `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                   `json:"is_maclaw_app,omitempty"`
	MaclawAppID                  string                 `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                 `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                 `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                 `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                 `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                 `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                   `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                 `json:"artifact_contract_presentation,omitempty"`
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
		ID:                           item.ID,
		Name:                         item.Name,
		Description:                  item.Description,
		Tags:                         item.Tags,
		Version:                      item.Version,
		Author:                       item.Author,
		TrustLevel:                   item.TrustLevel,
		Downloads:                    item.Downloads,
		HubURL:                       hubURL,
		AvgRating:                    item.AvgRating,
		RatingCount:                  item.RatingCount,
		ProductKind:                  item.ProductKind,
		IsMaclawApp:                  item.IsMaclawApp || strings.EqualFold(strings.TrimSpace(item.ProductKind), "maclaw_app_skill"),
		MaclawAppID:                  item.MaclawAppID,
		MaclawAppName:                item.MaclawAppName,
		MaclawAppDescription:         item.MaclawAppDescription,
		MaclawAppCategory:            item.MaclawAppCategory,
		MaclawAppIcon:                item.MaclawAppIcon,
		MaclawAppInputMode:           item.MaclawAppInputMode,
		MaclawAppOutputModes:         append([]string(nil), item.MaclawAppOutputModes...),
		MaclawAppDefinitionSHA256:    item.MaclawAppDefinitionSHA256,
		MaclawAppTestEvidence:        cloneSkillHubMaclawAppTestEvidence(item.MaclawAppTestEvidence),
		ArtifactContractRequired:     item.ArtifactContractRequired,
		ArtifactContractOutputModes:  append([]string(nil), item.ArtifactContractOutputModes...),
		ArtifactContractPresentation: item.ArtifactContractPresentation,
	}
}

func cloneSkillHubMaclawAppTestEvidence(e *MaclawAppTestEvidence) *MaclawAppTestEvidence {
	if e == nil {
		return nil
	}
	copy := *e
	return &copy
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
	return c.InstallToDirWithIntegrity(ctx, skillID, hubURL, targetDir, "", "")
}

func (c *SkillHubClient) InstallToDirWithIntegrity(ctx context.Context, skillID, hubURL, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	path := "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	base, _, data, err := c.getBytesFromExplicitHubURL(ctx, hubURL, path, maxDownloadSize)
	if err != nil {
		return nil, fmt.Errorf("download skill %q from %s failed: %v", skillID, strings.TrimRight(hubURL, "/"), err)
	}
	if err := verifyDownloadedSkillPackageSHA256(data, expectedSHA256); err != nil {
		return nil, err
	}
	var trustedFingerprints []string
	if c != nil && c.app != nil {
		trustedFingerprints = c.app.trustedSkillPackageKeyFingerprints()
	}
	if err := verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, expectedSignature, trustedFingerprints); err != nil {
		return nil, err
	}
	// Shared materialisation with TUI/agentservice (steps/files/agent_skill_md).
	entry, err := skill.ParseSkillHubDownloadJSON(data, skill.HubDownloadOptions{
		HubURL:    base,
		SkillID:   skillID,
		Source:    skillEntrySourceHub.String(),
		TargetDir: targetDir,
	})
	if err != nil {
		return nil, fmt.Errorf("decode skill %q from %s failed after %d bytes: %w", skillID, strings.TrimRight(hubURL, "/"), len(data), err)
	}
	if strings.TrimSpace(entry.Status) == "" {
		entry.Status = skillEntryStatusActive.String()
	}
	return entry, nil
}

// extractBundledSkillFiles delegates to the shared corelib extractor so GUI,
// TUI, and agentservice enforce the same path-safety and atomic-write rules.
func extractBundledSkillFiles(skillName string, files map[string]string, targetDir string) error {
	return skill.ExtractSkillPackageFiles(skillName, files, targetDir)
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
	if authHeader := c.publishAuthHeader(); authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

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
	return c.app.getHubCenterJSON(ctx, c.client, path, maxSearchJSONSize, dest)
}

func (c *SkillHubClient) getJSONFromExplicitHubURL(ctx context.Context, hubURL string, path string, dest interface{}) (string, []string, error) {
	base := strings.TrimSpace(hubURL)
	if base == "" {
		return c.getJSON(ctx, path, dest)
	}
	if authHeader := c.enterpriseHubAuthHeaderForBase(base); authHeader != "" {
		return c.getJSONFromExplicitHubURLWithAuth(ctx, base, path, dest, authHeader)
	}
	return c.app.getHubCenterJSONFromCandidates(ctx, c.client, []string{base}, path, maxSearchJSONSize, dest)
}

func (c *SkillHubClient) getBytesFromExplicitHubURL(ctx context.Context, hubURL string, path string, limit int64) (string, []string, []byte, error) {
	base := strings.TrimSpace(hubURL)
	// Use installClient (long timeout) for multi-asset package downloads.
	downloadClient := c.installClient
	if downloadClient == nil {
		downloadClient = c.client
	}
	if base == "" {
		return c.app.getHubCenterBytes(ctx, downloadClient, path, limit)
	}
	if authHeader := c.enterpriseHubAuthHeaderForBase(base); authHeader != "" {
		return c.getBytesFromExplicitHubURLWithAuth(ctx, base, path, limit, authHeader)
	}
	base = strings.TrimRight(base, "/")
	return c.app.getHubCenterBytesFromCandidates(ctx, downloadClient, []string{base}, path, limit)
}

func (c *SkillHubClient) getBytesFromExplicitHubURLWithAuth(ctx context.Context, base, path string, limit int64, authHeader string) (string, []string, []byte, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", "MaClaw/1.0")
	// Use installClient (long timeout) for multi-asset package downloads.
	httpClient := c.installClient
	if httpClient == nil {
		httpClient = c.client
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", []string{base}, nil, fmt.Errorf("request hub bytes %s%s failed (%d): %s", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := readLimitedHubCenterBodyWithLength(resp.Body, resp.ContentLength, limit)
	if err != nil {
		return "", []string{base}, nil, fmt.Errorf("read hub bytes %s%s failed: %w", base, path, err)
	}
	return base, []string{base}, data, nil
}

// publishAuthHeader returns the skillmarket session token used to authenticate
// skill publishes, mirroring SkillMarketClient.skillMarketAuthHeader.
func (c *SkillHubClient) publishAuthHeader() string {
	if c == nil || c.app == nil {
		return ""
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	if token := strings.TrimSpace(cfg.SkillMarketSessionToken); token != "" {
		return "Bearer " + token
	}
	if token := strings.TrimSpace(cfg.RemoteViewerToken); token != "" {
		return "Bearer " + token
	}
	return ""
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
	data, err := readHubCenterJSONBody(resp.Body, maxSearchJSONSize)
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
