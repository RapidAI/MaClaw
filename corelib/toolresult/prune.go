package toolresult

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// PruneResult reports what a prune pass removed.
type PruneResult struct {
	RemovedFiles int
	FreedBytes   int64
	RemovedDirs  int
}

// PruneOlderThan deletes spilled handle files under root whose modification
// time is older than maxAge, then removes now-empty session directories
// (never the root itself). An empty root resolves to
// maclawpath.ToolResultsDir(). A missing root is not an error.
//
// Only handle payloads (.txt/.enc) are ever removed — the same filter
// GetStoreStats uses. Anything else (most importantly the .store.key that
// decrypts every encrypted handle) is never touched: the key file is written
// once and never modified, so it is always the oldest file in the store and
// an age-only prune would destroy every encrypted handle's read-back on the
// first pass.
//
// Handles are written atomically at creation and never modified afterwards,
// so ModTime is a safe age proxy: a file older than maxAge cannot belong to
// an in-flight spill. Deleting an old handle only removes the lossless
// read-back capability of very old sessions — conversation previews remain.
func PruneOlderThan(root string, maxAge time.Duration) (PruneResult, error) {
	var result PruneResult
	if maxAge <= 0 {
		return result, nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = maclawpath.ToolResultsDir()
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}

	cutoff := time.Now().Add(-maxAge)
	var dirs []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep pruning the rest
		}
		if d.IsDir() {
			if path != root {
				dirs = append(dirs, path)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		// Only handle payloads are prunable. Belt and braces: the suffix
		// filter matches GetStoreStats, and the explicit key-file skip keeps
		// the store key safe even if its name ever gains a matching suffix.
		if d.Name() == storeKeyFile {
			return nil
		}
		if ext := filepath.Ext(d.Name()); ext != ".txt" && ext != encryptedSuffix {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return nil
		}
		result.RemovedFiles++
		result.FreedBytes += info.Size()
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}

	// Remove empty session directories, deepest first. os.Remove fails on
	// non-empty directories, which is exactly the keep condition.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		if err := os.Remove(dir); err == nil {
			result.RemovedDirs++
		}
	}

	if result.RemovedFiles > 0 {
		invalidateStoreStats(root)
	}
	return result, nil
}
