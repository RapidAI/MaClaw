package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// ────────────────────────────────────────────────────────────────────────────
// Hub Skill Update Cache
//
// Caches the result of CheckHubSkillUpdates so that:
//   - enrichInstalledState (called on every search) reads from cache, zero HTTP
//   - Frontend tab switches hit cache, zero HTTP
//   - Actual HTTP requests happen at most once per TTL (10 min)
// ────────────────────────────────────────────────────────────────────────────

type hubUpdateCache struct {
	mu        sync.RWMutex
	updates   []HubSkillUpdateInfo
	byHubID   map[string]bool // hubSkillID → has update
	fetchedAt time.Time
	ttl       time.Duration
}

func newHubUpdateCache() *hubUpdateCache {
	return &hubUpdateCache{
		ttl: 10 * time.Minute,
	}
}

// get returns the cached update map (hubSkillID → true). Returns nil if cache
// is empty or expired. This is a fast read-only path for enrichInstalledState.
func (c *hubUpdateCache) get() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.byHubID == nil || time.Since(c.fetchedAt) > c.ttl {
		return nil
	}
	// Return a copy to avoid races.
	cp := make(map[string]bool, len(c.byHubID))
	for k, v := range c.byHubID {
		cp[k] = v
	}
	return cp
}

// getUpdates returns the cached update list. Returns nil, false if expired.
func (c *hubUpdateCache) getUpdates() ([]HubSkillUpdateInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.updates == nil || time.Since(c.fetchedAt) > c.ttl {
		return nil, false
	}
	cp := make([]HubSkillUpdateInfo, len(c.updates))
	copy(cp, c.updates)
	return cp, true
}

// set stores fresh update results.
// HubSkillID is read directly from each HubSkillUpdateInfo — no external
// mapping needed. This is O(N) instead of the previous O(N×M) nested loop.
func (c *hubUpdateCache) set(updates []HubSkillUpdateInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = updates
	c.byHubID = make(map[string]bool, len(updates))
	for _, u := range updates {
		if u.HubSkillID != "" {
			c.byHubID[u.HubSkillID] = true
		}
	}
	c.fetchedAt = time.Now()
}

// invalidate clears the cache (e.g. after install/update/delete).
func (c *hubUpdateCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = nil
	c.byHubID = nil
	c.fetchedAt = time.Time{}
}

// ────────────────────────────────────────────────────────────────────────────
// HubCenter URL Resolution Cache
//
// GUI uses the shared HubCenterSelectionCache from corelib/remote for write
// throttling and persistence. This eliminates the duplicate cache implementation
// that previously existed in this file.
//
// The guiHubCenterPersister implements remote.HubCenterPersister to persist
// HubCenter URL selections to config.json.
// ────────────────────────────────────────────────────────────────────────────

// guiHubCenterPersister implements remote.HubCenterPersister for GUI.
// It reads/writes HubCenter URLs from/to the App's config.
type guiHubCenterPersister struct {
	app *App
}

func newGUIHubCenterPersister(app *App) *guiHubCenterPersister {
	return &guiHubCenterPersister{app: app}
}

func (p *guiHubCenterPersister) LoadHubCenterURLs() (string, []string) {
	if p.app == nil {
		return "", nil
	}
	cfg, err := p.app.LoadConfig()
	if err != nil {
		return "", nil
	}
	return cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs
}

func (p *guiHubCenterPersister) SaveHubCenterURLs(preferred string, discovered []string) error {
	if p.app == nil {
		return nil
	}
	return p.app.PatchConfig(func(cfg *corelib.AppConfig) {
		// Never replace a public preferred HubCenter with loopback. Allow
		// loopback preferred only when the user is already on loopback/unset
		// (local/dev failover among 127.0.0.1 test hubs).
		savePref := remote.NormalizeHubCenterURL(preferred)
		if savePref != "" {
			if !remote.IsLoopbackURL(savePref) {
				cfg.RemoteHubCenterURL = savePref
			} else {
				current := strings.TrimSpace(cfg.RemoteHubCenterURL)
				if current == "" || remote.IsLoopbackURL(current) {
					cfg.RemoteHubCenterURL = savePref
				} else {
					// Keep existing public preferred; still clean discovered list.
					savePref = remote.NormalizeHubCenterURL(cfg.RemoteHubCenterURL)
				}
			}
		} else {
			savePref = remote.NormalizeHubCenterURL(cfg.RemoteHubCenterURL)
		}

		// Defense in depth: strip non-preferred official defaults / foreign peers.
		// When savePref is public, constrain to that enrollment identity.
		// When loopback (local/dev), pass registered=nil so loopback preferred is kept.
		var registered []string
		if savePref != "" && !remote.IsLoopbackURL(savePref) {
			registered = remote.RegisteredPublicHubCenterURLs(savePref, discovered)
		}
		// Use constrained preferred (may differ if caller passed a stale base).
		constrainedPref, urls := remote.ConstrainHubCenterPersistence(registered, savePref, discovered)
		if constrainedPref != "" && !remote.IsLoopbackURL(constrainedPref) {
			cfg.RemoteHubCenterURL = constrainedPref
			savePref = constrainedPref
		}
		// Never fall back to raw discovered (would re-introduce hubs2 pollution).
		if len(urls) == 0 && savePref != "" {
			urls = []string{savePref}
		}
		// Replaces the former direct cfg.RemoteHubCenterURLs = discovered assignment.
		cfg.RemoteHubCenterURLs = urls
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Concurrent update checker
//
// Replaces the serial N+1 loop in CheckHubSkillUpdates with bounded
// concurrency (max 3 parallel requests).
// ────────────────────────────────────────────────────────────────────────────

const hubUpdateMaxConcurrency = 3

func fetchHubSkillUpdatesConcurrent(client *SkillHubClient, skills []hubSkillForUpdateCheck) []HubSkillUpdateInfo {
	if len(skills) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sem := make(chan struct{}, hubUpdateMaxConcurrency)
	var mu sync.Mutex
	var updates []HubSkillUpdateInfo
	var wg sync.WaitGroup

	for _, s := range skills {
		wg.Add(1)
		go func(skill hubSkillForUpdateCheck) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			meta, err := client.CheckUpdate(ctx, skill.hubSkillID, skill.hubVersion)
			if err != nil || meta == nil {
				return
			}
			mu.Lock()
			updates = append(updates, HubSkillUpdateInfo{
				SkillName:      skill.name,
				HubSkillID:     skill.hubSkillID,
				CurrentVersion: skill.hubVersion,
				LatestVersion:  meta.Version,
				HubURL:         meta.HubURL,
			})
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	return updates
}

type hubSkillForUpdateCheck struct {
	name       string
	hubSkillID string
	hubVersion string
}

// ────────────────────────────────────────────────────────────────────────────
// rememberHubCenterSelection write-throttle
//
// Only writes config.json when the base URL actually changes.
// ────────────────────────────────────────────────────────────────────────────

func (a *App) rememberHubCenterSelectionThrottled(base string, discovered []string) {
	if a == nil {
		return
	}
	if a.hubCenterCache == nil {
		a.hubCenterCache = remote.NewHubCenterSelectionCache(60 * time.Second)
	}
	// Ensure persister is initialized (lazy init to avoid circular dependency).
	if a.hubCenterPersister == nil {
		a.hubCenterPersister = newGUIHubCenterPersister(a)
	}
	// Delegate to the shared HubCenterSelectionCache from corelib/remote.
	// This is the single source of truth for write throttling logic.
	a.hubCenterCache.RememberSelectionThrottled(a.hubCenterPersister, base, discovered)
}

// ────────────────────────────────────────────────────────────────────────────
// resolveHubCenterBaseURLCached — cached wrapper
// ────────────────────────────────────────────────────────────────────────────

func (a *App) resolveHubCenterBaseURLCached(ctx context.Context, client *http.Client) (string, []string, error) {
	return a.resolveHubCenterBaseURLCachedWithIdentity(ctx, client, nil, nil)
}

// resolveHubCenterBaseURLCachedWithIdentity is the shared cache path.
// When registered/seeds are precomputed by the caller, skips an extra LoadConfig.
func (a *App) resolveHubCenterBaseURLCachedWithIdentity(ctx context.Context, client *http.Client, registered, seeds []string) (string, []string, error) {
	if a.hubCenterCache != nil {
		if base, all := a.hubCenterCache.Get(); base != "" {
			// Align cached entries to enrollment identity so a stale HA peer
			// (e.g. hubs2) cannot stick after the user registered elsewhere.
			if registered == nil && seeds == nil {
				registered, seeds = a.currentHubCenterIdentity()
			}
			if len(registered) > 0 || len(seeds) > 0 {
				aligned := remote.AlignHubCenterCandidates(registered, seeds, append([]string{base}, all...))
				if len(aligned) == 0 {
					a.hubCenterCache.Invalidate()
				} else {
					nextBase := remote.PickAlignedHubCenterBase(base, aligned)
					// Write back only when alignment drops/reorders peers — avoids
					// resetting cache TTL on every read of an already-clean entry.
					if nextBase != base || !remote.StringSliceEqual(aligned, all) {
						a.hubCenterCache.Set(nextBase, aligned)
					}
					return nextBase, aligned, nil
				}
			} else {
				return base, all, nil
			}
		}
	}

	var (
		base string
		all  []string
		err  error
	)
	if seeds != nil || registered != nil {
		// Caller already loaded identity — reuse seeds without a second LoadConfig
		// when we still need a full resolve (cache miss / invalidated).
		cfg, loadErr := a.LoadConfig()
		if loadErr != nil {
			return "", nil, loadErr
		}
		if len(seeds) == 0 {
			seeds = hubCenterSeedURLs(cfg)
		}
		if registered == nil {
			registered = remote.RegisteredPublicHubCenterURLs(cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs)
		}
		base, all, err = a.resolveHubCenterBaseURLWithSeeds(ctx, client, registered, seeds, cfg)
	} else {
		base, all, err = a.resolveHubCenterBaseURL(ctx, client)
	}
	if err != nil {
		return "", nil, err
	}

	if a.hubCenterCache != nil {
		a.hubCenterCache.Set(base, all)
	}
	return base, all, nil
}

// ────────────────────────────────────────────────────────────────────────────
// DefaultRemoteHubCenterURLs re-export for use in this package
// ────────────────────────────────────────────────────────────────────────────

var defaultRemoteHubCenterURLs = remote.DefaultRemoteHubCenterURLs

// ────────────────────────────────────────────────────────────────────────────
// getCachedHubUpdates — fast path for enrichInstalledState
// ────────────────────────────────────────────────────────────────────────────

func (a *App) getCachedHubUpdates() map[string]bool {
	if a == nil || a.hubUpdCache == nil {
		return nil
	}
	return a.hubUpdCache.get()
}

// refreshHubUpdateCacheAsync triggers a background refresh of the update cache.
// Non-blocking — the caller does not wait for the result.
func (a *App) refreshHubUpdateCacheAsync() {
	go func() {
		a.ensureSkillHubClient()
		if a.skillHubClient == nil || a.skillExecutor == nil {
			return
		}

		skills := a.skillExecutor.loadSkills()
		var checks []hubSkillForUpdateCheck
		for _, s := range skills {
			if normalizeSkillEntrySource(s.Source) == skillEntrySourceHub && s.HubSkillID != "" {
				checks = append(checks, hubSkillForUpdateCheck{
					name:       s.Name,
					hubSkillID: s.HubSkillID,
					hubVersion: s.HubVersion,
				})
			}
		}

		updates := fetchHubSkillUpdatesConcurrent(a.skillHubClient, checks)
		if a.hubUpdCache != nil {
			a.hubUpdCache.set(updates)
		}
		log.Printf("[hub-update-cache] refreshed: %d updates found for %d hub skills", len(updates), len(checks))
	}()
}
