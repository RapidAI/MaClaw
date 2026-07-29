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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// hubClientJSONMaxBytes is the default for getJSON (search + download).
// Download paths need MaxSkillPackageDownloadBytes for base64 file maps.
const hubClientJSONMaxBytes = MaxSkillPackageDownloadBytes
const hubClientSearchJSONMaxBytes = MaxSkillHubSearchJSONBytes

// ────────────────────────────────────────────────────────────────────────────
// Unified search result
// ────────────────────────────────────────────────────────────────────────────

// HubSearchResult is the unified search result returned by all search sources.
// Consumers map this to their own display types (views.SkillSearchResult,
// MixedSkillSearchResult, etc.).
type HubSearchResult struct {
	ID                           string                 `json:"id"`
	SkillID                      string                 `json:"skill_id,omitempty"` // publisher.skill-name stable identifier
	Name                         string                 `json:"name"`
	Description                  string                 `json:"description"`
	Version                      string                 `json:"version"`
	Author                       string                 `json:"author"`
	TrustLevel                   string                 `json:"trust_level"`
	AvgRating                    float64                `json:"avg_rating"`
	Downloads                    int                    `json:"downloads"`
	Score                        float64                `json:"score"`
	Source                       string                 `json:"source"`          // "skillhub", "clawhub", "github"
	CapabilityType               string                 `json:"capability_type"` // "skill" or "mcp" (empty defaults to "skill")
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

	// GitHub-specific fields (empty for other sources).
	RepoURL    string `json:"repo_url,omitempty"`
	FilePath   string `json:"file_path,omitempty"`
	InstallRef string `json:"install_ref,omitempty"` // JSON-serialized GitHubSkillCandidate
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

// HubSearchSourceStatus reports what happened for one search source.
// Used so empty merged results are not mistaken for "no skills exist"
// when one or more sources failed.
type HubSearchSourceStatus struct {
	Source  string `json:"source"`
	Queried bool   `json:"queried"`
	OK      bool   `json:"ok"`
	Count   int    `json:"count"`
	Error   string `json:"error,omitempty"`
	Skipped string `json:"skipped,omitempty"` // not allowed / empty query / cancelled
}

// HubSearchReport is the multi-source search outcome with per-source diagnostics.
type HubSearchReport struct {
	Results  []HubSearchResult       `json:"results"`
	Sources  []HubSearchSourceStatus `json:"sources"`
	Degraded bool                    `json:"degraded"` // true if any queried source failed
}

// FormatDegradedNote returns a short operator/LLM-facing note, or "".
func (r HubSearchReport) FormatDegradedNote() string {
	if r.Degraded {
		var failed []string
		for _, s := range r.Sources {
			if s.Queried && !s.OK && s.Error != "" {
				failed = append(failed, fmt.Sprintf("%s: %s", s.Source, s.Error))
			}
		}
		if len(failed) == 0 {
			return "search degraded: one or more sources failed"
		}
		return "search degraded (partial source failure): " + strings.Join(failed, "; ")
	}
	// All sources skipped or empty query — not degraded network, but helpful.
	return ""
}

// SearchAll queries SkillHub + ClawHub + GitHub and returns merged results.
// Results are ordered: SkillHub first, then ClawHub, then GitHub.
// Errors from individual sources are non-fatal (skipped); use
// SearchAllFilteredReport for per-source diagnostics.
func (c *HubClient) SearchAll(ctx context.Context, skillHubURL, query string) []HubSearchResult {
	return c.SearchAllFiltered(ctx, skillHubURL, query, nil)
}

// SearchAllFiltered queries allowed sources and returns merged results.
// allowedSources filters which sources to query. nil/empty = all sources.
// Valid source values: "skillhub", "clawhub", "github".
func (c *HubClient) SearchAllFiltered(ctx context.Context, skillHubURL, query string, allowedSources []string) []HubSearchResult {
	return c.SearchAllFilteredReport(ctx, skillHubURL, query, allowedSources).Results
}

// SearchAllFilteredReport is like SearchAllFiltered but includes per-source status.
func (c *HubClient) SearchAllFilteredReport(ctx context.Context, skillHubURL, query string, allowedSources []string) HubSearchReport {
	report := HubSearchReport{}
	query = strings.TrimSpace(query)

	appendSource := func(st HubSearchSourceStatus, hits []HubSearchResult) {
		if st.Queried && !st.OK {
			report.Degraded = true
		}
		report.Sources = append(report.Sources, st)
		report.Results = append(report.Results, hits...)
	}

	if isSourceAllowed("skillhub", allowedSources) {
		if query == "" {
			appendSource(HubSearchSourceStatus{Source: "skillhub", Skipped: "empty query"}, nil)
		} else if strings.TrimSpace(skillHubURL) == "" {
			appendSource(HubSearchSourceStatus{Source: "skillhub", Skipped: "no skillhub url"}, nil)
		} else {
			hits, err := c.searchSkillHub(ctx, skillHubURL, query)
			st := HubSearchSourceStatus{Source: "skillhub", Queried: true, Count: len(hits), OK: err == nil}
			if err != nil {
				st.Error = err.Error()
				st.OK = false
			}
			appendSource(st, hits)
		}
	} else {
		appendSource(HubSearchSourceStatus{Source: "skillhub", Skipped: "not allowed"}, nil)
	}

	if isSourceAllowed("clawhub", allowedSources) {
		if query == "" {
			appendSource(HubSearchSourceStatus{Source: "clawhub", Skipped: "empty query"}, nil)
		} else {
			hits, err := c.searchClawHub(ctx, query)
			st := HubSearchSourceStatus{Source: "clawhub", Queried: true, Count: len(hits), OK: err == nil}
			if err != nil {
				st.Error = err.Error()
				st.OK = false
			}
			appendSource(st, hits)
		}
	} else {
		appendSource(HubSearchSourceStatus{Source: "clawhub", Skipped: "not allowed"}, nil)
	}

	// GitHub search uses its own HTTP client with a 30s timeout (inside
	// GitHubSearcher). Check ctx before starting to avoid wasted work.
	if isSourceAllowed("github", allowedSources) {
		if query == "" {
			appendSource(HubSearchSourceStatus{Source: "github", Skipped: "empty query"}, nil)
		} else if ctx != nil && ctx.Err() != nil {
			appendSource(HubSearchSourceStatus{Source: "github", Queried: true, OK: false, Error: ctx.Err().Error()}, nil)
		} else {
			hits, err := c.searchGitHub(query)
			st := HubSearchSourceStatus{Source: "github", Queried: true, Count: len(hits), OK: err == nil}
			if err != nil {
				st.Error = err.Error()
				st.OK = false
			}
			// Empty GitHub with no error is still OK (no matches).
			appendSource(st, hits)
		}
	} else {
		appendSource(HubSearchSourceStatus{Source: "github", Skipped: "not allowed"}, nil)
	}

	return report
}

// isSourceAllowed checks if a source is in the allowed list.
// Returns true when allowedSources is nil or empty (all allowed).
func isSourceAllowed(source string, allowedSources []string) bool {
	if len(allowedSources) == 0 {
		return true
	}
	source = normalizeHubSearchSource(source)
	for _, s := range allowedSources {
		if normalizeHubSearchSource(s) == source {
			return true
		}
	}
	return false
}

func normalizeHubSearchSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "enterprise", "hub", "enterprisehub", "enterprise_hub":
		return "enterprise_hub"
	case "claw_hub":
		return "clawhub"
	case "git_hub":
		return "github"
	case "zip", "local_upload":
		return "local"
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
}

// IsSourceAllowed is the exported version for use by consumers.
func IsSourceAllowed(source string, allowedSources []string) bool {
	return isSourceAllowed(source, allowedSources)
}

// SearchSkillHub queries the SkillHub API.
// Returns nil on any error (non-fatal). Prefer SearchAllFilteredReport for diagnostics.
func (c *HubClient) SearchSkillHub(ctx context.Context, hubURL, query string) []HubSearchResult {
	hits, _ := c.searchSkillHub(ctx, hubURL, query)
	return hits
}

func (c *HubClient) searchSkillHub(ctx context.Context, hubURL, query string) ([]HubSearchResult, error) {
	if hubURL == "" || query == "" {
		return nil, fmt.Errorf("skillhub: missing hub url or query")
	}
	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1",
		hubURL, url.QueryEscape(query))

	var raw skillHubSearchResponse
	if err := c.getJSONLimited(ctx, endpoint, &raw, hubClientSearchJSONMaxBytes); err != nil {
		return nil, err
	}

	results := make([]HubSearchResult, 0, len(raw.Skills))
	for _, s := range raw.Skills {
		results = append(results, HubSearchResult{
			ID:                           s.ID,
			SkillID:                      s.SkillID,
			Name:                         s.Name,
			Description:                  s.Description,
			Version:                      s.Version,
			Author:                       s.Author,
			TrustLevel:                   s.TrustLevel,
			AvgRating:                    s.AvgRating,
			Downloads:                    s.Downloads,
			Source:                       "skillhub",
			ProductKind:                  s.ProductKind,
			IsMaclawApp:                  s.IsMaclawApp || strings.EqualFold(strings.TrimSpace(s.ProductKind), "maclaw_app_skill"),
			MaclawAppID:                  s.MaclawAppID,
			MaclawAppName:                s.MaclawAppName,
			MaclawAppDescription:         s.MaclawAppDescription,
			MaclawAppCategory:            s.MaclawAppCategory,
			MaclawAppIcon:                s.MaclawAppIcon,
			MaclawAppInputMode:           s.MaclawAppInputMode,
			MaclawAppOutputModes:         append([]string(nil), s.MaclawAppOutputModes...),
			MaclawAppDefinitionSHA256:    s.MaclawAppDefinitionSHA256,
			MaclawAppTestEvidence:        cloneMaclawAppTestEvidence(s.MaclawAppTestEvidence),
			ArtifactContractRequired:     s.ArtifactContractRequired,
			ArtifactContractOutputModes:  append([]string(nil), s.ArtifactContractOutputModes...),
			ArtifactContractPresentation: s.ArtifactContractPresentation,
		})
	}
	return results, nil
}

func cloneMaclawAppTestEvidence(e *MaclawAppTestEvidence) *MaclawAppTestEvidence {
	if e == nil {
		return nil
	}
	copy := *e
	return &copy
}

// SearchClawHub queries the ClawHub China mirror.
// Returns nil on any error (non-fatal). Prefer SearchAllFilteredReport for diagnostics.
func (c *HubClient) SearchClawHub(ctx context.Context, query string) []HubSearchResult {
	hits, _ := c.searchClawHub(ctx, query)
	return hits
}

func (c *HubClient) searchClawHub(ctx context.Context, query string) ([]HubSearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("clawhub: empty query")
	}
	endpoint := ClawHubMirrorURL + "/api/v1/search?q=" + url.QueryEscape(query)

	var raw clawHubSearchResponse
	if err := c.getJSONLimited(ctx, endpoint, &raw, hubClientSearchJSONMaxBytes); err != nil {
		return nil, err
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
	return results, nil
}

// SearchGitHub queries GitHub for skill definition files (skill.yaml, SKILL.md).
// Uses the GitHubSearcher already in this package.
// The token is resolved dynamically on each call (not cached from construction)
// so that runtime changes to GITHUB_TOKEN are picked up.
// Returns nil on any error (non-fatal). Prefer SearchAllFilteredReport for diagnostics.
func (c *HubClient) SearchGitHub(query string) []HubSearchResult {
	hits, _ := c.searchGitHub(query)
	return hits
}

func (c *HubClient) searchGitHub(query string) ([]HubSearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("github: empty query")
	}
	gs := NewGitHubSearcher(ResolveGitHubToken())
	candidates, err := gs.SearchGitHub(query)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
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
	return results, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Install — download + convert to NLSkillEntry
// ────────────────────────────────────────────────────────────────────────────

// HubDownloadOptions controls how a SkillHub download payload is materialised.
type HubDownloadOptions struct {
	// HubURL is recorded as SourceProject.
	HubURL string
	// SkillID is the download path id / fallback name.
	SkillID string
	// Source is the NLSkillEntry.Source value (default "hub").
	Source string
	// TargetDir overrides the extraction directory. Empty means
	// PrimarySkillsDir/<installName>. Ignored when SkipExtract is true.
	TargetDir string
	// SkipExtract builds the entry without writing files to disk (steps are
	// still synthesised from in-memory SKILL.md when needed).
	SkipExtract bool
}

// DownloadSkillHub downloads a skill from SkillHub and returns an NLSkillEntry
// ready for local registration.
//
// The download payload may contain:
//   - steps: executable step definitions
//   - files: path → base64 content map (extracted under PrimarySkillsDir)
//   - agent_skill_md: markdown skill body (converted via ParseMarkdownSkill)
//
// Bundled files are extracted so TUI/agentservice install paths match GUI.
// Dependency installation is intentionally deferred until after security scan.
func (c *HubClient) DownloadSkillHub(ctx context.Context, hubURL, skillID string) (*corelib.NLSkillEntry, error) {
	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download",
		strings.TrimRight(strings.TrimSpace(hubURL), "/"), url.PathEscape(skillID))

	var full skillHubDownloadResponse
	if err := c.getJSON(ctx, endpoint, &full); err != nil {
		return nil, fmt.Errorf("下载 Skill 失败: %w", err)
	}
	return entryFromSkillHubDownload(full, HubDownloadOptions{
		HubURL:  hubURL,
		SkillID: skillID,
		Source:  "hub",
	})
}

// ParseSkillHubDownloadJSON converts a raw SkillHub/SkillMarket download JSON
// body into an NLSkillEntry. This is the shared materialisation path for GUI,
// TUI, and agentservice so steps/files/agent_skill_md stay consistent.
func ParseSkillHubDownloadJSON(data []byte, opts HubDownloadOptions) (*corelib.NLSkillEntry, error) {
	var full skillHubDownloadResponse
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, fmt.Errorf("decode skill download payload: %w", err)
	}
	return entryFromSkillHubDownload(full, opts)
}

// entryFromSkillHubDownload converts a full SkillHub download payload into an
// NLSkillEntry, extracting bundled files and synthesizing steps when needed.
func entryFromSkillHubDownload(full skillHubDownloadResponse, opts HubDownloadOptions) (*corelib.NLSkillEntry, error) {
	hubURL := strings.TrimSpace(opts.HubURL)
	skillID := strings.TrimSpace(opts.SkillID)
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "hub"
	}

	// Prefer explicit agent_skill_md when present (market agent format).
	if md := strings.TrimSpace(full.AgentSkillMD); md != "" {
		entry, err := ParseMarkdownSkill(md, MarkdownSkillOptions{
			NameFallback:        firstNonEmptyString(full.Name, skillID),
			DescriptionFallback: full.Description,
			Source:              source,
			SourceProject:       hubURL,
			TrustLevel:          full.TrustLevel,
			Triggers:            full.Triggers,
		})
		if err != nil {
			return nil, fmt.Errorf("parse agent_skill_md: %w", err)
		}
		entry.SkillID = firstNonEmptyString(full.SkillID, entry.SkillID)
		entry.HubSkillID = firstNonEmptyString(full.ID, skillID)
		entry.HubVersion = full.Version
		entry.Version = firstNonEmptyString(full.SemVer, entry.Version)
		if pub, _, ok := ParseSkillID(entry.SkillID); ok {
			entry.Publisher = pub
		}
		return entry, nil
	}

	steps := make([]corelib.NLSkillStep, 0, len(full.Steps))
	for _, s := range full.Steps {
		action := strings.TrimSpace(s.Action)
		if action == "" {
			continue
		}
		steps = append(steps, corelib.NLSkillStep{
			Action:    action,
			Params:    s.Params,
			OnError:   s.OnError,
			Name:      s.Name,
			Condition: s.Condition,
		})
	}

	installName := firstNonEmptyString(full.Name, skillID)
	installSkillDir := strings.TrimSpace(opts.TargetDir)
	if installSkillDir == "" && installName != "" && !opts.SkipExtract {
		if skillsRoot, err := PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, installName)
		}
	}

	if len(steps) == 0 {
		steps = craftToolStepsFromHubFiles(full.Files, installSkillDir)
	}
	// A MaClaw App package is an installable app definition, not an executable
	// Skill. It deliberately has no runtime steps or SKILL.md: maclaw.app.json
	// is discovered after extraction by the Apps panel. Keep the ordinary Skill
	// validation strict, but allow a valid app definition through this shared
	// materialisation path so SkillMarket app cards do not fail as empty Skills.
	maclawAppMeta, hasMaclawAppDefinition := maclawAppDefinitionFromHubFiles(full.Files)
	// Some app packages wrap a normal, executable Skill and include an app
	// definition alongside its steps. Only a package with no executable
	// definition of its own is an instruction-only app container.
	isStandaloneMaclawAppPackage := hasMaclawAppDefinition && len(steps) == 0

	if !opts.SkipExtract && len(full.Files) > 0 {
		if err := extractHubBundledFiles(installName, full.Files, installSkillDir); err != nil {
			return nil, fmt.Errorf("extract bundled files for skill %q: %w", installName, err)
		}
	}

	if len(steps) == 0 && !isStandaloneMaclawAppPackage {
		return nil, fmt.Errorf("skill %s has no steps, no agent_skill_md, and no SKILL.md in files", firstNonEmptyString(full.Name, skillID))
	}

	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		// Official store downloads are treated as trusted so risk assessor
		// does not escalate legitimate hub packages to community.
		trustLevel = "trusted"
	}

	entry := &corelib.NLSkillEntry{
		SkillID:       full.SkillID,
		Name:          firstNonEmptyString(full.Name, skillID),
		Description:   full.Description,
		Triggers:      full.Triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        source,
		SourceProject: hubURL,
		HubSkillID:    firstNonEmptyString(full.ID, skillID),
		HubVersion:    full.Version,
		Version:       full.SemVer,
		TrustLevel:    trustLevel,
		SkillDir:      installSkillDir,
	}
	if isStandaloneMaclawAppPackage {
		// "instruction" marks this container entry as intentionally
		// non-executable while keeping it visible to the app-manifest discovery
		// and installed-skill metadata paths.
		entry.Type = "instruction"
		if strings.TrimSpace(entry.Description) == "" {
			entry.Description = maclawAppMeta.Description
		}
		if len(entry.Triggers) == 0 {
			entry.Triggers = []string{maclawAppMeta.Name}
		}
	}
	if pub, _, ok := ParseSkillID(entry.SkillID); ok {
		entry.Publisher = pub
	}
	return entry, nil
}

// craftToolStepsFromHubFiles synthesizes a craft_tool step from bundled SKILL.md.
func craftToolStepsFromHubFiles(files map[string]string, skillDir string) []corelib.NLSkillStep {
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

type maclawAppDefinitionMetadata struct {
	Name        string
	Description string
}

// maclawAppDefinitionFromHubFiles verifies that a downloaded package contains
// a structurally valid standalone MaClaw App definition. Validation here is
// deliberately narrow: extraction still validates every file path and the app
// loader performs its full contract validation before displaying the app.
func maclawAppDefinitionFromHubFiles(files map[string]string) (maclawAppDefinitionMetadata, bool) {
	encoded, ok := files["maclaw.app.json"]
	if !ok {
		return maclawAppDefinitionMetadata{}, false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return maclawAppDefinitionMetadata{}, false
	}
	var doc struct {
		Schema        string `json:"schema"`
		PrivateMarker string `json:"privateMarker"`
		App           struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"app"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return maclawAppDefinitionMetadata{}, false
	}
	if doc.Schema != "maclaw.app.v1" || doc.PrivateMarker != "x_maclaw_apps" || strings.TrimSpace(doc.App.ID) == "" || strings.TrimSpace(doc.App.Name) == "" {
		return maclawAppDefinitionMetadata{}, false
	}
	return maclawAppDefinitionMetadata{
		Name:        strings.TrimSpace(doc.App.Name),
		Description: strings.TrimSpace(doc.App.Description),
	}, true
}

// ExtractSkillPackageFiles writes a base64 path→content map under targetDir
// (or PrimarySkillsDir/<skillName>). It validates the whole package first and
// only writes after all paths are safe, so invalid packages never partially
// materialize. Shared by GUI, TUI, and agentservice.
func ExtractSkillPackageFiles(skillName string, files map[string]string, targetDir string) error {
	return extractHubBundledFiles(skillName, files, targetDir)
}

type hubBundledFile struct {
	original string
	destAbs  string
	data     []byte
}

// extractHubBundledFiles writes base64 file map under targetDir (or PrimarySkillsDir/name).
func extractHubBundledFiles(skillName string, files map[string]string, targetDir string) error {
	if len(files) == 0 {
		return fmt.Errorf("downloaded skill package contained no writable files")
	}
	skillDir := strings.TrimSpace(targetDir)
	if skillDir == "" {
		if strings.TrimSpace(skillName) == "" {
			return fmt.Errorf("skill name is required when target directory is empty")
		}
		skillsRoot, err := PrimarySkillsDir()
		if err != nil {
			return err
		}
		skillDir = filepath.Join(skillsRoot, skillName)
	}
	skillDirAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return err
	}

	rootExists := true
	if info, err := os.Lstat(skillDirAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe skill directory %q", skillDir)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		rootExists = false
	}

	// Decode + validate all paths before writing anything.
	decoded := make([]hubBundledFile, 0, len(files))
	for relPath, b64Content := range files {
		data, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			return fmt.Errorf("decode bundled file %q: %w", relPath, err)
		}
		normalized := strings.ReplaceAll(relPath, "\\", "/")
		clean := filepath.Clean(filepath.FromSlash(normalized))
		if isUnsafeHubBundledPath(normalized, clean) {
			return fmt.Errorf("unsafe bundled file path %q", relPath)
		}
		dest := filepath.Join(skillDirAbs, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil {
			return fmt.Errorf("resolve bundled file %q: %w", relPath, err)
		}
		relToRoot, err := filepath.Rel(skillDirAbs, destAbs)
		if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
			return fmt.Errorf("unsafe bundled file path %q", relPath)
		}
		decoded = append(decoded, hubBundledFile{original: relPath, destAbs: destAbs, data: data})
	}
	if len(decoded) == 0 {
		return fmt.Errorf("downloaded skill package contained no writable files")
	}

	rootCreated := false
	if !rootExists {
		if err := os.MkdirAll(filepath.Dir(skillDirAbs), 0o755); err != nil {
			return err
		}
		if err := os.Mkdir(skillDirAbs, 0o755); err != nil {
			if !os.IsExist(err) {
				return err
			}
		} else {
			rootCreated = true
		}
	}
	if info, err := os.Lstat(skillDirAbs); err != nil {
		return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, fmt.Errorf("unsafe skill directory %q", skillDir))
	}

	for _, f := range decoded {
		if err := os.MkdirAll(filepath.Dir(f.destAbs), 0o755); err != nil {
			return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, err)
		}
		if info, err := os.Lstat(f.destAbs); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
				return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, fmt.Errorf("unsafe bundled file target %q", f.original))
			}
		} else if !os.IsNotExist(err) {
			return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, err)
		}
	}
	for _, f := range decoded {
		if err := os.WriteFile(f.destAbs, f.data, 0o644); err != nil {
			return cleanupHubBundledDirOnError(skillDirAbs, rootCreated, fmt.Errorf("write bundled file %q: %w", f.original, err))
		}
	}
	return nil
}

func isUnsafeHubBundledPath(normalized, clean string) bool {
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(normalized, "/") {
		return true
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return true
		}
		// Windows drive letters and ADS both use ':'. Treat as non-portable.
		if strings.Contains(part, ":") {
			return true
		}
	}
	return false
}

func cleanupHubBundledDirOnError(skillDirAbs string, rootCreated bool, err error) error {
	if err != nil && rootCreated {
		_ = os.RemoveAll(skillDirAbs)
	}
	return err
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

// DownloadBySkillID downloads a skill by its publisher.name skill_id from Hub.
// This is the primary method for App dependency resolution — it uses the stable
// external identifier rather than the internal UUID.
// The optional versionConstraint parameter filters by semver range (e.g. ">=1.2.0").
func (c *HubClient) DownloadBySkillID(ctx context.Context, hubURL, skillID, versionConstraint string) (*corelib.NLSkillEntry, error) {
	if hubURL == "" || skillID == "" {
		return nil, fmt.Errorf("hubURL and skillID are required")
	}
	endpoint := fmt.Sprintf("%s/api/v1/skills/by-skill-id/%s/download",
		hubURL, url.PathEscape(skillID))
	if versionConstraint != "" {
		endpoint += "?constraint=" + url.QueryEscape(versionConstraint)
	}

	var full skillHubDownloadResponse
	if err := c.getJSON(ctx, endpoint, &full); err != nil {
		return nil, fmt.Errorf("download skill %s: %w", skillID, err)
	}

	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	entry := &corelib.NLSkillEntry{
		SkillID:       skillID,
		Name:          full.Name,
		Description:   full.Description,
		Triggers:      full.Triggers,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    full.ID,
		HubVersion:    full.Version,
		Version:       full.SemVer,
		TrustLevel:    trustLevel,
	}
	// Derive publisher from skill_id
	if pub, _, ok := ParseSkillID(skillID); ok {
		entry.Publisher = pub
	}
	return entry, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Internal: HTTP helper + response types
// ────────────────────────────────────────────────────────────────────────────

func (c *HubClient) getJSON(ctx context.Context, endpoint string, dest interface{}) error {
	// Default covers download payloads with base64 file maps.
	return c.getJSONLimited(ctx, endpoint, dest, hubClientJSONMaxBytes)
}

func (c *HubClient) getJSONLimited(ctx context.Context, endpoint string, dest interface{}, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	// Install payloads can be tens of MiB; do not inherit a short search timeout.
	if maxBytes > MaxSkillHubSearchJSONBytes {
		client = &http.Client{Timeout: 180 * time.Second, Transport: client.Transport}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := ReadLimitedHTTPBody(resp.Body, resp.ContentLength, maxBytes)
	if err != nil {
		return err
	}
	return json.NewDecoder(bytes.NewReader(data)).Decode(dest)
}

// readBoundedHubJSON is retained for unit tests.
func readBoundedHubJSON(body io.Reader, contentLength, maxBytes int64) ([]byte, error) {
	return ReadLimitedHTTPBody(body, contentLength, maxBytes)
}

// ── SkillHub response types ──

type skillHubSearchResponse struct {
	Skills []skillHubItem `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

type skillHubItem struct {
	ID                           string                 `json:"id"`
	SkillID                      string                 `json:"skill_id,omitempty"` // publisher.skill-name (new)
	SemVer                       string                 `json:"semver,omitempty"`   // semantic version (new)
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

type skillHubDownloadStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`
	Name      string                 `json:"name,omitempty"`
	Condition string                 `json:"condition,omitempty"`
}

type skillHubDownloadResponse struct {
	skillHubItem
	Triggers     []string               `json:"triggers"`
	Steps        []skillHubDownloadStep `json:"steps,omitempty"`
	Files        map[string]string      `json:"files,omitempty"`          // path → base64 content
	AgentSkillMD string                 `json:"agent_skill_md,omitempty"` // SKILL.md content
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
