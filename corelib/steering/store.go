package steering

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store loads, caches, and queries steering files from user-level and
// project-level directories. It is safe for concurrent use.
type Store struct {
	userDir    string // ~/.maclaw/steering/
	projectDir string // <project>/.maclaw/steering/

	mu       sync.RWMutex
	files    []File
	lastScan time.Time
}

// rescanInterval is the minimum time between directory re-scans.
// Steering files are checked lazily: if more than this duration has passed
// since the last scan, Resolve() triggers a re-scan. This avoids the
// complexity of fsnotify while providing near-instant hot-reload for the
// typical conversation cadence (messages are seconds to minutes apart).
const rescanInterval = 30 * time.Second

// NewStore creates a Store that loads steering files from the given directories.
// Either directory may be empty (disabled).
func NewStore(userDir, projectDir string) *Store {
	return &Store{
		userDir:    userDir,
		projectDir: projectDir,
	}
}

// Load scans both directories, parses all .md files, merges by name
// (project-level overrides user-level for same-name files), and sorts
// by priority. This is called automatically by Resolve() when the cache
// is stale, but can also be called explicitly.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() error {
	var userFiles, projectFiles []File

	if s.userDir != "" {
		userFiles = scanDir(s.userDir, ScopeUser)
	}
	if s.projectDir != "" {
		projectFiles = scanDir(s.projectDir, ScopeProject)
	}

	// Merge: project-level files override user-level files with the same name.
	merged := mergeFiles(userFiles, projectFiles)

	// Enforce MaxTotalFiles.
	if len(merged) > MaxTotalFiles {
		log.Printf("[steering] WARNING: %d files found, limiting to %d", len(merged), MaxTotalFiles)
		merged = merged[:MaxTotalFiles]
	}

	// Sort by priority (lower first), then by name for stability.
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Priority != merged[j].Priority {
			return merged[i].Priority < merged[j].Priority
		}
		return merged[i].Name < merged[j].Name
	})

	s.files = merged
	s.lastScan = time.Now()

	if len(merged) > 0 {
		log.Printf("[steering] loaded %d files (user=%d, project=%d)",
			len(merged), len(userFiles), len(projectFiles))
	}
	return nil
}

// Resolve returns the steering files that should be injected for the current
// conversation turn. It handles:
//   - Lazy re-scan when cache is stale (>30s since last scan)
//   - Inclusion mode filtering (always/fileMatch/contextMatch/manual)
//   - Token budget enforcement (per-file cap + total cap)
//   - Dynamic budget scaling for small context models
func (s *Store) Resolve(ctx ResolveContext) []File {
	s.ensureFresh()

	s.mu.RLock()
	allFiles := s.files
	s.mu.RUnlock()

	if len(allFiles) == 0 {
		return nil
	}

	// Collect candidates matching the current context.
	candidates := s.collectCandidates(allFiles, ctx)
	if len(candidates) == 0 {
		return nil
	}

	// Already sorted by priority from loadLocked.
	// Apply token budget.
	budget := effectiveBudget(ctx.EffectiveContextTokens)
	return applyBudget(candidates, budget)
}

// FileCount returns the number of loaded steering files.
func (s *Store) FileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
}

// ensureFresh triggers a re-scan if the cache is stale.
func (s *Store) ensureFresh() {
	s.mu.RLock()
	stale := time.Since(s.lastScan) > rescanInterval
	s.mu.RUnlock()

	if stale {
		if err := s.Load(); err != nil {
			log.Printf("[steering] re-scan error: %v", err)
		}
	}
}

// collectCandidates filters files by inclusion mode against the context.
func (s *Store) collectCandidates(files []File, ctx ResolveContext) []File {
	msgLower := strings.ToLower(strings.TrimSpace(ctx.UserMessage))

	// Build manual ref lookup.
	manualSet := make(map[string]bool, len(ctx.ManualRefs))
	for _, ref := range ctx.ManualRefs {
		manualSet[strings.ToLower(ref)] = true
	}

	// Count always files for MaxAlwaysFiles enforcement.
	alwaysCount := 0

	var result []File
	for _, f := range files {
		switch f.Inclusion {
		case InclusionAlways:
			if alwaysCount >= MaxAlwaysFiles {
				log.Printf("[steering] always-file limit reached (%d), skipping %q", MaxAlwaysFiles, f.Name)
				continue
			}
			alwaysCount++
			result = append(result, f)

		case InclusionFileMatch:
			if matchesFilePattern(f.FileMatchPattern, ctx.ContextFiles) {
				result = append(result, f)
			}

		case InclusionContextMatch:
			if matchesContextKeywords(f.ContextKeywords, msgLower) {
				result = append(result, f)
			}

		case InclusionManual:
			// Match by filename without .md extension.
			nameKey := strings.TrimSuffix(strings.ToLower(f.Name), ".md")
			if manualSet[nameKey] || manualSet[f.Name] {
				result = append(result, f)
			}
		}
	}
	return result
}

// applyBudget enforces per-file and total token limits.
func applyBudget(candidates []File, totalBudget int) []File {
	var result []File
	usedTokens := 0

	for _, f := range candidates {
		tokens := estimateTokens(f.Content)

		// Per-file cap.
		if tokens > MaxSingleFileTokens {
			f.Content = truncateSmart(f.Content, MaxSingleFileTokens)
			tokens = estimateTokens(f.Content)
			log.Printf("[steering] truncated %q to %d tokens (single file limit)", f.Name, tokens)
		}

		remaining := totalBudget - usedTokens
		if remaining <= 0 {
			log.Printf("[steering] budget exhausted (%d/%d), skipping %q",
				usedTokens, totalBudget, f.Name)
			break
		}

		// If file exceeds remaining budget, truncate to fit.
		if tokens > remaining {
			f.Content = truncateSmart(f.Content, remaining)
			tokens = estimateTokens(f.Content)
			log.Printf("[steering] truncated %q to %d tokens (budget remaining=%d)", f.Name, tokens, remaining)
		}

		result = append(result, f)
		usedTokens += tokens
	}
	return result
}

// scanDir reads all .md files from a directory (non-recursive).
func scanDir(dir string, scope Scope) []File {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[steering] cannot read directory %s: %v", dir, err)
		}
		return nil
	}

	var files []File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		path := filepath.Join(dir, name)
		f, err := ParseFile(path, scope)
		if err != nil {
			log.Printf("[steering] skip %s: %v", path, err)
			continue
		}
		files = append(files, *f)
	}
	return files
}

// mergeFiles merges user-level and project-level files. Project-level files
// override user-level files with the same name (if the user file is overridable).
func mergeFiles(userFiles, projectFiles []File) []File {
	// Index project files by name for O(1) lookup.
	projectByName := make(map[string]File, len(projectFiles))
	for _, f := range projectFiles {
		projectByName[f.Name] = f
	}

	// Track which project files have been consumed (either as override or skipped).
	consumed := make(map[string]bool, len(projectFiles))

	var merged []File

	// Pass 1: process user files, applying project overrides.
	for _, uf := range userFiles {
		if pf, exists := projectByName[uf.Name]; exists {
			consumed[uf.Name] = true
			if uf.Overridable {
				merged = append(merged, pf) // project wins
			} else {
				merged = append(merged, uf) // user wins (not overridable)
			}
		} else {
			merged = append(merged, uf)
		}
	}

	// Pass 2: add project-only files (no user-level counterpart).
	for _, pf := range projectFiles {
		if !consumed[pf.Name] {
			merged = append(merged, pf)
		}
	}

	return merged
}

// matchesFilePattern checks if any context file matches the glob pattern.
func matchesFilePattern(pattern string, contextFiles []string) bool {
	if pattern == "" || len(contextFiles) == 0 {
		return false
	}
	for _, cf := range contextFiles {
		matched, err := filepath.Match(pattern, filepath.Base(cf))
		if err == nil && matched {
			return true
		}
		// Also try matching against the full path for patterns like "src/*.go".
		matched, err = filepath.Match(pattern, cf)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// matchesContextKeywords checks if the user message contains any keyword.
func matchesContextKeywords(keywords []string, msgLower string) bool {
	if len(keywords) == 0 || msgLower == "" {
		return false
	}
	for _, kw := range keywords {
		if strings.Contains(msgLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// truncateSmart truncates content to fit within the given token budget,
// cutting at a smart boundary (paragraph or line break).
func truncateSmart(content string, tokenBudget int) string {
	runes := []rune(content)
	maxRunes := tokenBudget * 3 // inverse of estimateTokens
	if maxRunes <= 0 {
		return "[truncated]"
	}
	if len(runes) <= maxRunes {
		return content
	}

	const notice = "\n[truncated]"
	noticeLen := len([]rune(notice))
	cutoff := maxRunes - noticeLen
	if cutoff <= 0 {
		return notice
	}

	snippet := string(runes[:cutoff])

	// Try paragraph break.
	half := len(snippet) / 2
	if idx := strings.LastIndex(snippet, "\n\n"); idx > half {
		return snippet[:idx] + notice
	}
	// Try line break.
	if idx := strings.LastIndex(snippet, "\n"); idx > half {
		return snippet[:idx] + notice
	}
	return snippet + notice
}
