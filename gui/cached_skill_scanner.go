package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// skillCacheEntry holds the cached skill scan results with metadata.
type skillCacheEntry struct {
	skills    []corelib.NLSkillEntry
	createdAt time.Time
	stale     bool
}

// CachedSkillScanner provides async skill scanning with TTL cache.
// It wraps skill.ScanSkillDir to avoid blocking startup and message processing.
//
// Design:
//   - Init() records roots and spawns a background scan goroutine, returns in <50ms
//   - Get() returns cached results or empty list (graceful degradation)
//   - Expired cache (>10m): returns stale data immediately + triggers background refresh
//   - Invalidate(): marks cache stale and triggers background refresh
//   - Only one concurrent scan at a time (mutex-guarded); if a mutation races an
//     in-flight scan, triggerBackgroundScan re-arms after the worker exits
//   - Individual directory scan errors are skipped (logged), remaining dirs continue
//   - RemoveByDir(): synchronously removes from cache AND records in pendingRemovals
//     so that a concurrent scan() will filter the removed dir from its results
//   - UpsertSkills(): synchronously merges imported/installed skills into cache AND
//     records them in pendingUpserts so a concurrent scan that raced the write
//     cannot drop the newly added entries when it stores its result
type CachedSkillScanner struct {
	roots    []string
	cache    atomic.Pointer[skillCacheEntry]
	scanning atomic.Bool
	version  atomic.Uint64
	mu       sync.Mutex // guards scan execution to prevent concurrent scans

	// pendingRemovals tracks SkillDir paths that have been deleted from disk
	// but may not yet be reflected in a concurrent background scan's results.
	// scan() applies these removals after completing its disk scan.
	// pendingUpserts tracks skills that were just written to disk (import/install)
	// so a concurrent scan that started before those dirs existed still surfaces them.
	removalsMu      sync.Mutex
	pendingRemovals map[string]struct{}
	pendingUpserts  map[string]corelib.NLSkillEntry
}

const skillCacheTTL = 10 * time.Minute

// Init records skill directory roots and starts a background scan.
// Returns in <50ms — no synchronous file system scanning.
func (s *CachedSkillScanner) Init(roots []string) {
	s.roots = roots
	// Use triggerBackgroundScan to properly set the scanning flag,
	// preventing a concurrent Get() from spawning a duplicate scan.
	s.triggerBackgroundScan()
}

// Get returns the cached skill list or an empty list if no cache is available.
//
// Behavior:
//   - No cache yet (scan in progress): returns empty list
//   - Cache valid (age <= 10m): returns cached results
//   - Cache expired (age > 10m): returns stale results immediately + triggers background refresh
func (s *CachedSkillScanner) Get() []corelib.NLSkillEntry {
	entry := s.cache.Load()
	if entry == nil {
		// No cache available — scan still in progress or never started
		return nil
	}

	// Check TTL
	if time.Since(entry.createdAt) > skillCacheTTL || entry.stale {
		// Cache expired or marked stale — trigger background refresh
		s.triggerBackgroundScan()
	}

	return entry.skills
}

// Invalidate marks the cache as stale and triggers a background refresh.
// The stale cache remains available for Get() callers until the new scan completes.
func (s *CachedSkillScanner) Invalidate() {
	if s == nil {
		return
	}
	// Hold removalsMu so we never overwrite a concurrent UpsertSkills/RemoveByDir
	// with a stale snapshot of the pre-mutation skill list.
	s.removalsMu.Lock()
	entry := s.cache.Load()
	if entry != nil && !entry.stale {
		s.cache.Store(&skillCacheEntry{
			skills:    entry.skills,
			createdAt: entry.createdAt,
			stale:     true,
		})
	}
	s.version.Add(1)
	s.removalsMu.Unlock()
	s.triggerBackgroundScan()
}

func (s *CachedSkillScanner) Version() uint64 {
	if s == nil {
		return 0
	}
	return s.version.Load()
}

// triggerBackgroundScan starts a background scan if one is not already running.
// Uses atomic CAS as a fast-path check, then the mutex inside scan() prevents
// concurrent execution.
//
// After a successful scan, if another mutation marked the cache stale while
// scanning was true (so a nested triggerBackgroundScan CAS failed), we
// re-arm a scan immediately. Without this, scan()'s final stale=false store
// can race a concurrent Invalidate/Upsert and leave no follow-up worker until
// the next Get()/TTL — the same class of lag as the zip-import list bug.
//
// Panic safety: a panicking scan must not re-arm from a still-stale entry, or
// we would spin forever. The next Get()/Invalidate()/UpsertSkills() will
// schedule a fresh attempt.
func (s *CachedSkillScanner) triggerBackgroundScan() {
	if s == nil {
		return
	}
	if !s.scanning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		panicked := false
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				log.Printf("[CachedSkillScanner] background scan panic: %v", r)
			}
			s.scanning.Store(false)
			if panicked {
				return
			}
			// Re-check after releasing the scanning flag (classic double-check):
			// a mutation may have set stale=true while CAS was blocked.
			if entry := s.cache.Load(); entry != nil && entry.stale {
				s.triggerBackgroundScan()
			}
		}()
		s.scan()
	}()
}

// scan performs the actual file system scan across all roots.
// It acquires the mutex to prevent concurrent scans, skips errored directories,
// and stores the result in the atomic cache pointer.
// After scanning, it drains pendingRemovals to filter out skills that were
// deleted from disk during or just before this scan started.
func (s *CachedSkillScanner) scan() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var allSkills []corelib.NLSkillEntry
	// Case-insensitive name set (matches skillNameIdentityKey) so pending
	// upserts and multi-root scans share the same dedup rules.
	seen := make(map[string]bool)

	for _, root := range s.roots {
		skills := s.scanRoot(root)
		for _, sk := range skills {
			nameKey := skillNameIdentityKey(sk.Name)
			if nameKey != "" && seen[nameKey] {
				continue
			}
			if nameKey != "" {
				seen[nameKey] = true
			}
			allSkills = append(allSkills, sk)
		}
	}

	// Apply pending removals/upserts under removalsMu held through cache.Store so
	// concurrent RemoveByDir/UpsertSkills cannot lose a mutation between drain and store.
	s.removalsMu.Lock()
	if len(s.pendingRemovals) > 0 {
		filtered := make([]corelib.NLSkillEntry, 0, len(allSkills))
		for _, sk := range allSkills {
			if _, removed := s.pendingRemovals[skillDirIdentityKey(sk.SkillDir)]; !removed {
				filtered = append(filtered, sk)
			}
		}
		allSkills = filtered
		// Clear pending removals — they've been applied to a fresh scan.
		s.pendingRemovals = nil
		// Rebuild name set after removals so a same-name re-import pending upsert
		// is not incorrectly suppressed by the pre-removal seen entry.
		seen = make(map[string]bool, len(allSkills))
		for _, sk := range allSkills {
			if name := skillNameIdentityKey(sk.Name); name != "" {
				seen[name] = true
			}
		}
	}
	if len(s.pendingUpserts) > 0 {
		byKey := make(map[string]int, len(allSkills))
		for i, sk := range allSkills {
			if key := skillCacheIdentityKey(sk); key != "" {
				byKey[key] = i
			}
		}
		for key, sk := range s.pendingUpserts {
			if _, ok := byKey[key]; ok {
				// Prefer the on-disk scan result when present; only keep the
				// pending entry when the concurrent scan missed the new dir.
				continue
			}
			// Match disk-scan name dedup (case-insensitive): do not surface a
			// second entry for a name already discovered under another path.
			nameKey := skillNameIdentityKey(sk.Name)
			if nameKey != "" && seen[nameKey] {
				continue
			}
			allSkills = append(allSkills, sk)
			byKey[key] = len(allSkills) - 1
			if nameKey != "" {
				seen[nameKey] = true
			}
		}
		s.pendingUpserts = nil
	}

	// Ensure non-nil slice so Get() can distinguish "scan complete with 0 results"
	// from "scan not yet started" (nil cache pointer).
	if allSkills == nil {
		allSkills = []corelib.NLSkillEntry{}
	}

	// Store the new cache entry (still under removalsMu to prevent race with RemoveByDir/UpsertSkills).
	entry := &skillCacheEntry{
		skills:    allSkills,
		createdAt: time.Now(),
		stale:     false,
	}
	s.cache.Store(entry)
	s.version.Add(1)
	s.removalsMu.Unlock()
}

// scanRoot scans a single root directory for skills.
// Errors are logged and skipped — the scanner continues with remaining directories.
func (s *CachedSkillScanner) scanRoot(root string) []corelib.NLSkillEntry {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[CachedSkillScanner] panic scanning root %s: %v", root, r)
		}
	}()

	skills := skill.ScanSkillDir(root)
	return skills
}

// RemoveByDir synchronously removes a skill entry from the current cache
// by its SkillDir path. This is called after a successful disk deletion
// so that subsequent Get() calls (which may return stale data while a
// background re-scan is in progress) no longer include the deleted skill.
// It also records the removal in pendingRemovals so that a concurrent
// scan() goroutine (which may have started scanning before the disk deletion)
// will filter this skill from its results.
func (s *CachedSkillScanner) RemoveByDir(skillDir string) {
	if s == nil || skillDir == "" {
		return
	}
	normalizedDir := skillDirIdentityKey(skillDir)

	// Hold removalsMu through cache.Store so a concurrent UpsertSkills/scan
	// cannot reintroduce the removed dir between pending update and store.
	s.removalsMu.Lock()
	if s.pendingRemovals == nil {
		s.pendingRemovals = make(map[string]struct{})
	}
	s.pendingRemovals[normalizedDir] = struct{}{}
	delete(s.pendingUpserts, normalizedDir)

	entry := s.cache.Load()
	if entry == nil {
		s.version.Add(1)
		s.removalsMu.Unlock()
		return
	}
	var filtered []corelib.NLSkillEntry
	removed := false
	for _, sk := range entry.skills {
		if skillDirIdentityKey(sk.SkillDir) == normalizedDir {
			removed = true
			continue
		}
		filtered = append(filtered, sk)
	}
	if removed {
		if filtered == nil {
			filtered = []corelib.NLSkillEntry{}
		}
		s.cache.Store(&skillCacheEntry{
			skills:    filtered,
			createdAt: entry.createdAt,
			stale:     entry.stale,
		})
	}
	s.version.Add(1)
	s.removalsMu.Unlock()
}

// skillCacheIdentityKey returns a stable identity for cache merge/dedup.
// Prefer SkillDir (path-stable); fall back to lower-cased name for config-only entries.
func skillCacheIdentityKey(sk corelib.NLSkillEntry) string {
	if dir := skillDirIdentityKey(sk.SkillDir); dir != "" && dir != "." {
		return dir
	}
	if name := skillNameIdentityKey(sk.Name); name != "" {
		return "name:" + name
	}
	return ""
}

// UpsertSkills merges newly imported/installed skills into the cache immediately
// so ListNLSkills/loadSkills can return them without waiting for a background rescan
// (which may take a long time when many skill directories exist).
//
// Pending upserts are also recorded so a concurrent scan() that started before the
// directories existed still includes them when it stores its result.
func (s *CachedSkillScanner) UpsertSkills(skills []corelib.NLSkillEntry) {
	if s == nil || len(skills) == 0 {
		return
	}
	cloned := cloneSkillEntries(skills)

	s.removalsMu.Lock()
	if s.pendingUpserts == nil {
		s.pendingUpserts = make(map[string]corelib.NLSkillEntry, len(cloned))
	}
	changed := false
	for _, sk := range cloned {
		key := skillCacheIdentityKey(sk)
		if key == "" {
			continue
		}
		s.pendingUpserts[key] = sk
		// A fresh install cancels any pending removal for the same directory.
		if sk.SkillDir != "" {
			delete(s.pendingRemovals, skillDirIdentityKey(sk.SkillDir))
		}
		changed = true
	}
	if !changed {
		s.removalsMu.Unlock()
		return
	}

	entry := s.cache.Load()
	var merged []corelib.NLSkillEntry
	if entry != nil {
		merged = append([]corelib.NLSkillEntry(nil), entry.skills...)
	}
	byKey := make(map[string]int, len(merged)+len(cloned))
	byName := make(map[string]int, len(merged)+len(cloned))
	for i, sk := range merged {
		if key := skillCacheIdentityKey(sk); key != "" {
			byKey[key] = i
		}
		if nameKey := skillNameIdentityKey(sk.Name); nameKey != "" {
			byName[nameKey] = i
		}
	}
	for _, sk := range cloned {
		key := skillCacheIdentityKey(sk)
		if key == "" {
			continue
		}
		if idx, ok := byKey[key]; ok {
			merged[idx] = sk
			if nameKey := skillNameIdentityKey(sk.Name); nameKey != "" {
				byName[nameKey] = idx
			}
			continue
		}
		// Replace any existing same-name row (config-only name key, or a
		// different SkillDir for the same skill name) to avoid list duplicates.
		if nameKey := skillNameIdentityKey(sk.Name); nameKey != "" {
			if idx, ok := byName[nameKey]; ok {
				oldKey := skillCacheIdentityKey(merged[idx])
				if oldKey != "" {
					delete(byKey, oldKey)
				}
				merged[idx] = sk
				byKey[key] = idx
				byName[nameKey] = idx
				continue
			}
			byName[nameKey] = len(merged)
		}
		byKey[key] = len(merged)
		merged = append(merged, sk)
	}
	if merged == nil {
		merged = []corelib.NLSkillEntry{}
	}
	createdAt := time.Now()
	if entry != nil {
		createdAt = entry.createdAt
	}
	s.cache.Store(&skillCacheEntry{
		skills:    merged,
		createdAt: createdAt,
		// Mark stale so a background rescan still runs and converges with disk.
		stale: true,
	})
	s.version.Add(1)
	s.removalsMu.Unlock()

	s.triggerBackgroundScan()
}
