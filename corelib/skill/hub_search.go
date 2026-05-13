package skill

// hub_search.go provides a unified multi-source skill search and install client.
//
// This is the SINGLE implementation of skill search/install HTTP logic.
// All consumers (GUI, TUI UI, TUI agent tool, TUI CLI) use this package.
// Adding a new search source or changing an API format requires changing
// only this file.
//
// Sources (in priority order):
//   1. SkillHub  — /api/v1/skills/search
//   2. ClawHub   — cn.clawhub-mirror.com/api/v1/search
//   3. GitHub    — via GitHubSearcher (already in this package)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ────────────────────────────────────────────────────────────────────────────
// Constants — single source of truth for all URLs
// ────────────────────────────────────────────────────────────────────────────

// ClawHubMirrorURL is the China mirror for ClawHub skill search.
// Used by all search and install paths.
const ClawHubMirrorURL = "https://cn.clawhub-mirror.com"

// ────────────────────────────────────────────────────────────────────────────
// Unified search result
// ────────────────────────────────────────────────────────────────────────────

// HubSearchResult is the unified search result returned by all search sources.
// Consumers map this to their own display types (views.SkillSearchResult,
// MixedSkillSearchResult, etc.).
type HubSearchResult struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Author      string  `json:"author"`
	TrustLevel  string  `json:"trust_level"`
	AvgRating   float64 `json:"avg_rating"`
	Downloads   int     `json:"downloads"`
	Score       float64 `json:"score"`
	Source      string  `json:"source"` // "skillhub", "clawhub", "github"

	// GitHub-specific fields (empty for other sources).
	RepoURL    string `json:"repo_url,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	InstallRef string `json:"install_ref,omitempty"` // JSON-serialized GitHubSkillCandidate
}

// ────────────────────────────────────────────────────────────────────────────
// HubClient — unified search + install
// ────────────────────────────────────────────────────────────────────────────

// HubClient provides multi-source skill search and install.
// It is safe for concurrent use.
type HubClient struct {
	httpClient  *http.Client
	userAgent   string
	githubToken string // optional GitHub token for higher rate limits
}

// NewHubClient creates a HubClient with sensible defaults.
// Uses ResolveGitHubToken() which checks GITHUB_TOKEN env var first,
// then falls back to the built-in default token.
func NewHubClient() *HubClient {
	return &HubClient{
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		userAgent:   "MaClaw/1.0",
		githubToken: ResolveGitHubToken(),
	}
}

// defaultClient is a package-level singleton that reuses TCP connections
// across searches. Initialized lazily on first call to DefaultHubClient().
var (
	defaultClient     *HubClient
	defaultClientOnce sync.Once
)

// DefaultHubClient returns a shared HubClient singleton that reuses HTTP
// connections. Prefer this over NewHubClient() for repeated searches.
func DefaultHubClient() *HubClient {
	defaultClientOnce.Do(func() {
		defaultClient = &HubClient{
			httpClient: &http.Client{
				Timeout: 15 * time.Second,
				Transport: &http.Transport{
					MaxIdleConns:        10,
					MaxIdleConnsPerHost: 5,
					IdleConnTimeout:     90 * time.Second,
				},
			},
			userAgent:   "MaClaw/1.0",
			githubToken: ResolveGitHubToken(),
		}
	})
	return defaultClient
}

// ────────────────────────────────────────────────────────────────────────────
// Search — multi-source aggregation
// ────────────────────────────────────────────────────────────────────────────

// SearchAll queries SkillHub + ClawHub + GitHub and returns merged results.
// Results are ordered: SkillHub first, then ClawHub, then GitHub.
// Errors from individual sources are non-fatal (silently skipped).
func (c *HubClient) SearchAll(ctx context.Context, skillHubURL, query string) []HubSearchResult {
	return c.SearchAllFiltered(ctx, skillHubURL, query, nil)
}

// SearchAllFiltered queries allowed sources and returns merged results.
// allowedSources filters which sources to query. nil/empty = all sources.
// Valid source values: "skillhub", "clawhub", "github".
func (c *HubClient) SearchAllFiltered(ctx context.Context, skillHubURL, query string, allowedSources []string) []HubSearchResult {
	var results []HubSearchResult
	if isSourceAllowed("skillhub", allowedSources) {
		results = append(results, c.SearchSkillHub(ctx, skillHubURL, query)...)
	}
	if isSourceAllowed("clawhub", allowedSources) {
		results = append(results, c.SearchClawHub(ctx, query)...)
	}

	// GitHub search uses its own HTTP client with a 30s timeout (inside
	// GitHubSearcher). Check ctx before starting to avoid wasted work.
	if isSourceAllowed("github", allowedSources) && ctx.Err() == nil {
		results = append(results, c.SearchGitHub(query)...)
	}
	return results
}

// isSourceAllowed checks if a source is in the allowed list.
// Returns true when allowedSources is nil or empty (all allowed).
func isSourceAllowed(source string, allowedSources []string) bool {
	if len(allowedSources) == 0 {
		return true
	}
	for _, s := range allowedSources {
		if s == source {
			return true
		}
	}
	return false
}

// IsSourceAllowed is the exported version for use by consumers.
func IsSourceAllowed(source string, allowedSources []string) bool {
	return isSourceAllowed(source, allowedSources)
}

// SearchSkillHub queries the SkillHub API.
// Returns nil on any error (non-fatal).
func (c *HubClient) SearchSkillHub(ctx context.Context, hubURL, query string) []HubSearchResult {
	if hubURL == "" || query == "" {
		return nil
	}
	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1",
		hubURL, url.QueryEscape(query))

	var raw skillHubSearchResponse
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return nil
	}

	results := make([]HubSearchResult, 0, len(raw.Skills))
	for _, s := range raw.Skills {
		results = append(results, HubSearchResult{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Version:     s.Version,
			Author:      s.Author,
			TrustLevel:  s.TrustLevel,
			AvgRating:   s.AvgRating,
			Downloads:   s.Downloads,
			Source:      "skillhub",
		})
	}
	return results
}

// SearchClawHub queries the ClawHub China mirror.
// Returns nil on any error (non-fatal).
func (c *HubClient) SearchClawHub(ctx context.Context, query string) []HubSearchResult {
	if query == "" {
		return nil
	}
	endpoint := ClawHubMirrorURL + "/api/v1/search?q=" + url.QueryEscape(query)

	var raw clawHubSearchResponse
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return nil
	}

	results := make([]HubSearchResult, 0, len(raw.Results))
	for _, r := range raw.Results {
		name := r.DisplayName
		if name == "" {
			name = r.Slug
		}
		results = append(results, HubSearchResult{
			ID:          r.Slug,
			Name:        name,
			Description: r.Summary,
			Version:     r.Version,
			Score:       r.Score,
			TrustLevel:  "community",
			Source:      "clawhub",
		})
	}
	return results
}

// SearchGitHub queries GitHub for skill definition files (skill.yaml, SKILL.md).
// Uses the GitHubSearcher already in this package.
// The token is resolved dynamically on each call (not cached from construction)
// so that runtime changes to GITHUB_TOKEN are picked up.
// Returns nil on any error (non-fatal).
func (c *HubClient) SearchGitHub(query string) []HubSearchResult {
	if query == "" {
		return nil
	}
	gs := NewGitHubSearcher(ResolveGitHubToken())
	candidates, err := gs.SearchGitHub(query)
	if err != nil || len(candidates) == 0 {
		return nil
	}

	results := make([]HubSearchResult, 0, len(candidates))
	for _, cand := range candidates {
		installRef, _ := json.Marshal(cand)
		name := cand.RepoFullName
		if cand.FilePath != "" {
			name = cand.RepoFullName + " · " + cand.FilePath
		}
		results = append(results, HubSearchResult{
			ID:          cand.RepoFullName,
			Name:        name,
			Description: cand.Description,
			Downloads:   cand.Stars,
			TrustLevel:  "community",
			Source:      "github",
			RepoURL:     cand.RepoURL,
			FilePath:    cand.FilePath,
			InstallRef:  string(installRef),
		})
	}
	return results
}

// ────────────────────────────────────────────────────────────────────────────
// Install — download + convert to NLSkillEntry
// ────────────────────────────────────────────────────────────────────────────

// DownloadSkillHub downloads a skill from SkillHub and returns an NLSkillEntry
// ready for local registration. Does NOT extract files or install dependencies
// (caller is responsible for that if needed).
func (c *HubClient) DownloadSkillHub(ctx context.Context, hubURL, skillID string) (*corelib.NLSkillEntry, error) {
	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download",
		hubURL, url.PathEscape(skillID))

	var full skillHubDownloadResponse
	if err := c.getJSON(ctx, endpoint, &full); err != nil {
		return nil, fmt.Errorf("下载 Skill 失败: %w", err)
	}

	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		Name:          full.Name,
		Description:   full.Description,
		Triggers:      full.Triggers,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    skillID,
		HubVersion:    full.Version,
		TrustLevel:    trustLevel,
	}, nil
}

// DownloadClawHub downloads a skill from ClawHub mirror and returns an
// NLSkillEntry with a craft_tool step using the SKILL.md content.
func (c *HubClient) DownloadClawHub(ctx context.Context, slug string) (*corelib.NLSkillEntry, error) {
	endpoint := ClawHubMirrorURL + "/api/v1/skills/" + url.PathEscape(slug)

	var raw clawHubSkillResponse
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return nil, fmt.Errorf("下载 ClawHub Skill 失败: %w", err)
	}

	name := raw.Skill.DisplayName
	if name == "" {
		name = raw.Skill.Slug
	}

	skillMD := raw.MetaContent.SkillMD
	if skillMD == "" {
		return nil, fmt.Errorf("skill %s has no SKILL.md content", slug)
	}

	return &corelib.NLSkillEntry{
		Name:        name,
		Description: raw.Skill.Summary,
		Triggers:    []string{raw.Skill.Slug},
		Steps: []corelib.NLSkillStep{
			{
				Action: "craft_tool",
				Params: map[string]interface{}{
					"instructions":      skillMD,
					"verification_mode": "artifact_optional",
					"register_policy":   "manual",
				},
			},
		},
		Status:     "active",
		CreatedAt:  time.Now().Format(time.RFC3339),
		Source:     "clawhub",
		TrustLevel: "community",
	}, nil
}

// DownloadGitHub downloads a skill from GitHub using the InstallRef
// (JSON-serialized GitHubSkillCandidate) and returns an NLSkillEntry.
func (c *HubClient) DownloadGitHub(ctx context.Context, installRef string) (*corelib.NLSkillEntry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var cand GitHubSkillCandidate
	if err := json.Unmarshal([]byte(installRef), &cand); err != nil {
		return nil, fmt.Errorf("invalid GitHub install ref: %w", err)
	}
	gs := NewGitHubSearcher(ResolveGitHubToken())
	entry, err := gs.ImportFromCandidate(cand)
	if err != nil {
		return nil, fmt.Errorf("GitHub import failed: %w", err)
	}
	entry.Source = "github"
	entry.SourceProject = cand.RepoURL
	entry.Status = "active"
	entry.CreatedAt = time.Now().Format(time.RFC3339)
	entry.TrustLevel = "community"
	return entry, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Internal: HTTP helper + response types
// ────────────────────────────────────────────────────────────────────────────

func (c *HubClient) getJSON(ctx context.Context, endpoint string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dest)
}

// ── SkillHub response types ──

type skillHubSearchResponse struct {
	Skills []skillHubItem `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

type skillHubItem struct {
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

type skillHubDownloadResponse struct {
	skillHubItem
	Triggers []string `json:"triggers"`
}

// ── ClawHub response types ──

type clawHubSearchResponse struct {
	Results []clawHubSearchItem `json:"results"`
}

type clawHubSearchItem struct {
	Slug        string  `json:"slug"`
	DisplayName string  `json:"displayName"`
	Summary     string  `json:"summary"`
	Version     string  `json:"version"`
	Score       float64 `json:"score"`
	UpdatedAt   int64   `json:"updatedAt"`
}

type clawHubSkillResponse struct {
	Skill struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Summary     string `json:"summary"`
	} `json:"skill"`
	MetaContent struct {
		SkillMD string `json:"skillMd"`
	} `json:"metaContent"`
}
