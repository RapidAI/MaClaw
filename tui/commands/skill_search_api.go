package commands

// skill_search_api.go provides exported search functions for agent tool use.
// These delegate to the shared corelib/skill.HubClient for actual HTTP logic.
//
// All search functions include multi-node failover via ResolveHubCenterWithFailover
// and persist the selection via the shared HubCenterSelectionCache singleton.
//
// Architecture:
//   - SharedHubCenterCache: package-level singleton for write throttling
//   - SharedHubCenterPersister: package-level singleton for config persistence
//   - ResolveHubCenterWithFailover: shared failover logic used by all callers
//   - HubCenterPersister: exported type for TUI app to use the same persister

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillSearchResult is a unified search result for agent tool use.
type SkillSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "skillhub", "clawhub", "github"
}

// --- Shared HubCenter infrastructure (singleton pattern) ---

var (
	sharedCacheOnce     sync.Once
	sharedCache         *remote.HubCenterSelectionCache
	sharedPersisterOnce sync.Once
	sharedPersister     *HubCenterPersister
)

// SharedHubCenterCache returns the package-level singleton cache.
// This ensures write throttling works across all callers in the same process.
func SharedHubCenterCache() *remote.HubCenterSelectionCache {
	sharedCacheOnce.Do(func() {
		sharedCache = remote.NewHubCenterSelectionCache(60 * time.Second)
	})
	return sharedCache
}

// SharedHubCenterPersister returns the package-level singleton persister.
// This is the single source of truth for HubCenter URL persistence.
func SharedHubCenterPersister() *HubCenterPersister {
	sharedPersisterOnce.Do(func() {
		sharedPersister = NewHubCenterPersister(ResolveDataDir())
	})
	return sharedPersister
}

// HubCenterPersister implements remote.HubCenterPersister for TUI/CLI.
// It reads/writes HubCenter URLs from/to the shared config.json file.
// Exported so TUI app can use the same persister type.
type HubCenterPersister struct {
	dataDir string
}

// NewHubCenterPersister creates a new persister for the given data directory.
func NewHubCenterPersister(dataDir string) *HubCenterPersister {
	return &HubCenterPersister{dataDir: dataDir}
}

func (p *HubCenterPersister) LoadHubCenterURLs() (string, []string) {
	store := NewFileConfigStore(p.dataDir)
	cfg, err := store.LoadConfig()
	if err != nil {
		return "", nil
	}
	return cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs
}

func (p *HubCenterPersister) SaveHubCenterURLs(preferred string, discovered []string) error {
	store := NewFileConfigStore(p.dataDir)
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	// Defense-in-depth: never persist a loopback address as the primary
	// HubCenter URL. The upstream RememberSelectionThrottled should already
	// filter these, but guard here as well.
	if preferred != "" && !remote.IsLoopbackURL(preferred) {
		cfg.RemoteHubCenterURL = preferred
	}
	cfg.RemoteHubCenterURLs = discovered
	return store.SaveConfig(cfg)
}

// --- Shared failover logic ---

// ResolveHubCenterWithFailover performs multi-node discovery + probe and returns
// the best reachable HubCenter URL. This is the single source of truth for
// failover logic — all callers (TUI app, agent tool, CLI commands) use this.
//
// Parameters:
//   - cfg: AppConfig containing HubCenter URL configuration
//   - currentURL: the currently configured URL to start with
//   - cache: optional cache for write throttling (use SharedHubCenterCache() if nil)
//   - persister: optional persister for config persistence (use SharedHubCenterPersister() if nil)
//
// Returns the resolved URL (may be same as currentURL if no failover needed).
func ResolveHubCenterWithFailover(cfg corelib.AppConfig, currentURL string, cache *remote.HubCenterSelectionCache, persister remote.HubCenterPersister) string {
	if cache == nil {
		cache = SharedHubCenterCache()
	}
	if persister == nil {
		persister = SharedHubCenterPersister()
	}

	urls := cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
	if len(urls) <= 1 {
		return currentURL // single URL, no failover needed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	probeClient := &http.Client{Timeout: 3 * time.Second}
	var ordered []string

	// Try discovery first (gossip protocol).
	if ordered = remote.DiscoverHubCenterURLs(ctx, probeClient, urls, currentURL); len(ordered) > 0 {
		currentURL = ordered[0]
	} else {
		// Fallback to direct probe if discovery fails.
		if ordered = remote.SelectBestCenter(ctx, probeClient, urls, currentURL); len(ordered) > 0 {
			currentURL = ordered[0]
		}
	}

	// Persist the failover result (shared with GUI via config.json).
	if len(ordered) > 0 {
		cache.RememberSelectionThrottled(persister, currentURL, ordered)
	}

	return currentURL
}

// --- Exported API functions ---

// ResolveHubCenterURL returns the configured HubCenter URL (exported wrapper).
func ResolveHubCenterURL() string {
	return resolveHubCenterURL()
}

// SearchSkillMarket queries SkillHub and returns results.
// Kept for backward compatibility — delegates to the shared HubClient.
// Includes multi-node failover via ResolveHubCenterWithFailover.
func SearchSkillMarket(baseURL, query string, topN int) ([]SkillSearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Multi-node failover using shared infrastructure.
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	if ok, reason := clientsecurity.EnforceConfig(cfg, "search_and_install_skill", skillSearchAPIArgs(cfg, query, "skillhub", baseURL)); !ok {
		return nil, fmt.Errorf("%s", reason)
	}
	resolvedURL := ResolveHubCenterWithFailover(cfg, baseURL, nil, nil)

	client := skill.DefaultHubClient()
	results := client.SearchSkillHub(ctx, resolvedURL, query)

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

// SearchSkillHub queries SkillHub + ClawHub + GitHub and returns merged results.
// Uses the shared corelib/skill.HubClient with multi-node failover.
func SearchSkillHub(query string) ([]SkillSearchResult, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Multi-node failover using shared infrastructure.
	hubURL = ResolveHubCenterWithFailover(cfg, hubURL, nil, nil)

	client := skill.DefaultHubClient()
	// Filter by allowed sources from local config.
	allowedSources, err := skillSearchAPIAllowedSourcesForPolicy(cfg, query, hubURL)
	if err != nil {
		return nil, err
	}
	results := client.SearchAllFiltered(ctx, hubURL, query, allowedSources)

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

func skillSearchAPIAllowedSourcesForPolicy(cfg corelib.AppConfig, query, hubURL string) ([]string, error) {
	if clientsecurity.IsDeveloperMode(cfg) {
		return nil, nil
	}
	candidates := []string{"skillhub", "clawhub", "github"}
	allowed := make([]string, 0, len(candidates))
	var blocked []string
	for _, source := range candidates {
		if !skill.IsSourceAllowed(source, cfg.SkillSourcesAllowed) {
			continue
		}
		if ok, reason := clientsecurity.EnforceConfig(cfg, "search_and_install_skill", skillSearchAPIArgs(cfg, query, source, hubURL)); !ok {
			blocked = append(blocked, source+": "+reason)
			continue
		}
		allowed = append(allowed, source)
	}
	if len(allowed) == len(candidates) && len(cfg.SkillSourcesAllowed) == 0 {
		return nil, nil
	}
	if len(allowed) == 0 && len(blocked) > 0 {
		return nil, fmt.Errorf("skill search blocked by security policy: %s", strings.Join(blocked, "; "))
	}
	return allowed, nil
}

func skillSearchAPIArgs(cfg corelib.AppConfig, query, source, hubURL string) map[string]interface{} {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "skillhub"
	}
	args := map[string]interface{}{"query": query, "source": source}
	switch normalizeSkillSearchAPISource(source) {
	case "github":
		args["url"] = "https://github.com"
	case "clawhub":
		args["url"] = skill.ClawHubMirrorURL
	default:
		if strings.TrimSpace(hubURL) == "" {
			hubURL = cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
		}
		args["hub_url"] = hubURL
	}
	return args
}

func firstAllowedSkillSearchSource(cfg corelib.AppConfig) string {
	if clientsecurity.IsDeveloperMode(cfg) {
		return "skillhub"
	}
	for _, source := range cfg.SkillSourcesAllowed {
		if strings.TrimSpace(source) != "" {
			return source
		}
	}
	return "skillhub"
}

func normalizeSkillSearchAPISource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "claw_hub":
		return "clawhub"
	case "git_hub":
		return "github"
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
}
