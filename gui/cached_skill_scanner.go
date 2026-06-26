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
//   - Only one concurrent scan at a time (mutex-guarded)
//   - Individual directory scan errors are skipped (logged), remaining dirs continue
type CachedSkillScanner struct {
	roots    []string
	cache    atomic.Pointer[skillCacheEntry]
	scanning atomic.Bool
	version  atomic.Uint64
	mu       sync.Mutex // guards scan execution to prevent concurrent scans
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
	entry := s.cache.Load()
	if entry != nil {
		// Mark as stale — atomic pointer swap with stale flag
		staleEntry := &skillCacheEntry{
			skills:    entry.skills,
			createdAt: entry.createdAt,
			stale:     true,
		}
		s.cache.Store(staleEntry)
	}
	s.version.Add(1)
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
// concurrent execution. The atomic flag is cleared in the goroutine's defer,
// ensuring subsequent Invalidate() calls can trigger a new scan after the
// current one completes.
func (s *CachedSkillScanner) triggerBackgroundScan() {
	if s.scanning.CompareAndSwap(false, true) {
		go func() {
			s.scan()
			// Clear the flag AFTER scan() returns (which releases the mutex).
			// This ordering ensures: if Invalidate() is called while scan() holds
			// the mutex, the CAS will fail (flag is still true), but the stale
			// marker on the cache entry will cause the next Get() to re-trigger.
			s.scanning.Store(false)
		}()
	}
}

// scan performs the actual file system scan across all roots.
// It acquires the mutex to prevent concurrent scans, skips errored directories,
// and stores the result in the atomic cache pointer.
func (s *CachedSkillScanner) scan() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var allSkills []corelib.NLSkillEntry
	seen := make(map[string]bool)

	for _, root := range s.roots {
		skills := s.scanRoot(root)
		for _, sk := range skills {
			if !seen[sk.Name] {
				seen[sk.Name] = true
				allSkills = append(allSkills, sk)
			}
		}
	}

	// Ensure non-nil slice so Get() can distinguish "scan complete with 0 results"
	// from "scan not yet started" (nil cache pointer).
	if allSkills == nil {
		allSkills = []corelib.NLSkillEntry{}
	}

	// Store the new cache entry
	entry := &skillCacheEntry{
		skills:    allSkills,
		createdAt: time.Now(),
		stale:     false,
	}
	s.cache.Store(entry)
	s.version.Add(1)
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
