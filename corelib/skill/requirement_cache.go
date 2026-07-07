package skill

// requirement_cache.go implements dependency installation state caching.
//
// After all requirements are satisfied (CheckAll returns 0 violations after Fix),
// a lightweight marker file is written to the skill directory. On subsequent
// runs, if the marker is still valid (dependency sources haven't changed),
// the entire CheckAll + Fix pipeline is skipped — saving 1-5 seconds per run.
//
// Invalidation triggers:
//   - skill.yaml / SKILL.md modification (mtime change)
//   - requirements.txt / pyproject.toml / package.json modification
//   - Manual deletion of the marker file
//   - Marker older than 24 hours (staleness cap)
//
// The marker stores a hash of all dependency-relevant file mtimes. If any
// source file's mtime changes, the hash mismatches and re-checking is triggered.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	depsCacheFileName = ".maclaw_deps_ok"
	depsCacheMaxAge   = 24 * time.Hour
)

// DepsInstallCacheValid returns true if the dependency installation cache
// is still valid for the given skill directory. When true, callers can skip
// the entire CheckAll + Fix pipeline.
func DepsInstallCacheValid(skillDir string) bool {
	if skillDir == "" {
		return false
	}
	markerPath := filepath.Join(skillDir, depsCacheFileName)
	markerInfo, err := os.Stat(markerPath)
	if err != nil {
		return false // marker doesn't exist
	}
	// Staleness cap: older than 24h → re-check
	if time.Since(markerInfo.ModTime()) > depsCacheMaxAge {
		return false
	}
	// Read the stored hash
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return false
	}
	storedHash := strings.TrimSpace(string(data))
	if storedHash == "" {
		return false
	}
	// Compute current hash and compare
	currentHash := computeDepsSourceHash(skillDir)
	if currentHash != storedHash {
		log.Printf("[requirement-cache] deps hash mismatch for %s (stored=%s current=%s), re-checking",
			skillDir, storedHash[:8], currentHash[:8])
		return false
	}
	return true
}

// WriteDepsInstallCache writes the dependency cache marker after successful
// requirement satisfaction. Should be called after CheckAll + Fix returns
// zero error-level violations.
func WriteDepsInstallCache(skillDir string) {
	if skillDir == "" {
		return
	}
	hash := computeDepsSourceHash(skillDir)
	if hash == "" {
		return
	}
	markerPath := filepath.Join(skillDir, depsCacheFileName)
	if err := os.WriteFile(markerPath, []byte(hash+"\n"), 0644); err != nil {
		log.Printf("[requirement-cache] failed to write deps cache: %v", err)
	}
}

// InvalidateDepsInstallCache removes the cache marker, forcing re-check on next run.
func InvalidateDepsInstallCache(skillDir string) {
	if skillDir == "" {
		return
	}
	os.Remove(filepath.Join(skillDir, depsCacheFileName))
}

// computeDepsSourceHash computes a hash from the mtimes of all dependency-relevant
// files in the skill directory, plus the Python executable identity. Any change
// to these inputs invalidates the cache.
func computeDepsSourceHash(skillDir string) string {
	var parts []string

	// Files that affect dependency resolution
	depFiles := []string{
		"skill.yaml",
		"SKILL.md",
		"skill.md",
		"requirements.txt",
		"pyproject.toml",
		"package.json",
		"package-lock.json",
	}

	for _, name := range depFiles {
		path := filepath.Join(skillDir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue // file doesn't exist, skip
		}
		parts = append(parts, fmt.Sprintf("%s:%d", name, info.ModTime().UnixNano()))
	}

	// Include Python executable identity — if Python changes (system→bundled,
	// version upgrade), packages installed in the old environment aren't available.
	python := findPythonExecutable()
	if python != "" {
		parts = append(parts, "python:"+python)
	}

	if len(parts) == 0 {
		return "" // no dependency-relevant files found
	}

	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:])
}
