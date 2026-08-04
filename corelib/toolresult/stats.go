package toolresult

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Process-local compression counters (OpenSquilla-style tool-compress observability).
var (
	compressOriginalBytes atomic.Int64
	compressPreviewBytes  atomic.Int64
	compressSpills        atomic.Int64
	compressProjects      atomic.Int64

	compressMu     sync.Mutex
	compressByTool map[string]int64 // bytes saved per tool
)

// CompressionStats is a snapshot for doctor / CLI.
type CompressionStats struct {
	Projects      int64            `json:"projects"`
	Spills        int64            `json:"spills"`
	OriginalBytes int64            `json:"original_bytes"`
	PreviewBytes  int64            `json:"preview_bytes"`
	SavedBytes    int64            `json:"saved_bytes"`
	SavedPercent  int              `json:"saved_percent,omitempty"`
	ByToolSaved   map[string]int64 `json:"by_tool_saved,omitempty"`
}

// StoreStats is a read-only snapshot of durable lossless tool-result storage.
// Handles are intentionally retained because persisted conversation history may
// reference them; automatic age-based deletion would silently break read-back.
type StoreStats struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

type storeStatsCacheEntry struct {
	stats     StoreStats
	scannedAt time.Time
}

var (
	storeStatsMu         sync.Mutex
	storeStatsCache      = make(map[string]storeStatsCacheEntry)
	storeStatsGeneration = make(map[string]uint64)
)

const storeStatsCacheTTL = 5 * time.Second

// GetStoreStats reports the number and size of stored result files. Errors are
// ignored so diagnostics never interfere with an agent turn.
func GetStoreStats(root string) StoreStats {
	rootAbs, err := storeRoot(root)
	if err != nil {
		return StoreStats{}
	}
	storeStatsMu.Lock()
	if cached, ok := storeStatsCache[rootAbs]; ok && time.Since(cached.scannedAt) < storeStatsCacheTTL {
		storeStatsMu.Unlock()
		return cached.stats
	}
	generation := storeStatsGeneration[rootAbs]
	storeStatsMu.Unlock()

	// Directory walks can become slow after many durable handles accumulate.
	// Never hold the cache lock during filesystem I/O: Spill invalidation runs on
	// the agent path and must not wait behind an operator diagnostics scan.
	var out StoreStats
	_ = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if filepath.Ext(entry.Name()) != ".txt" {
			return nil
		}
		out.Files++
		out.Bytes += info.Size()
		return nil
	})
	storeStatsMu.Lock()
	// If a Spill completed during the walk, do not install a potentially stale
	// snapshot. The caller may receive its point-in-time scan, while the next call
	// is guaranteed to rescan instead of observing stale cache state.
	if storeStatsGeneration[rootAbs] == generation {
		storeStatsCache[rootAbs] = storeStatsCacheEntry{stats: out, scannedAt: time.Now()}
	}
	storeStatsMu.Unlock()
	return out
}

// RefreshStoreStats bypasses the short status cache for explicit diagnostics.
func RefreshStoreStats(root string) StoreStats {
	invalidateStoreStats(root)
	return GetStoreStats(root)
}

func invalidateStoreStats(root string) {
	rootAbs, err := storeRoot(root)
	if err != nil {
		return
	}
	storeStatsMu.Lock()
	storeStatsGeneration[rootAbs]++
	delete(storeStatsCache, rootAbs)
	storeStatsMu.Unlock()
}

// RecordProjection updates process counters after a Project/truncate.
func RecordProjection(toolName string, originalBytes, previewBytes int, spilled bool) {
	if originalBytes < 0 {
		originalBytes = 0
	}
	if previewBytes < 0 {
		previewBytes = 0
	}
	compressProjects.Add(1)
	compressOriginalBytes.Add(int64(originalBytes))
	compressPreviewBytes.Add(int64(previewBytes))
	if spilled {
		compressSpills.Add(1)
	}
	saved := originalBytes - previewBytes
	if saved <= 0 {
		return
	}
	tool := sanitizePathSegment(toolName)
	if tool == "" {
		tool = "tool"
	}
	compressMu.Lock()
	if compressByTool == nil {
		compressByTool = make(map[string]int64)
	}
	compressByTool[tool] += int64(saved)
	compressMu.Unlock()
}

// GetCompressionStats returns a process-local snapshot.
func GetCompressionStats() CompressionStats {
	st := CompressionStats{
		Projects:      compressProjects.Load(),
		Spills:        compressSpills.Load(),
		OriginalBytes: compressOriginalBytes.Load(),
		PreviewBytes:  compressPreviewBytes.Load(),
	}
	if st.OriginalBytes > st.PreviewBytes {
		st.SavedBytes = st.OriginalBytes - st.PreviewBytes
	}
	if st.OriginalBytes > 0 {
		st.SavedPercent = int((st.SavedBytes * 100) / st.OriginalBytes)
	}
	compressMu.Lock()
	if len(compressByTool) > 0 {
		st.ByToolSaved = make(map[string]int64, len(compressByTool))
		for k, v := range compressByTool {
			st.ByToolSaved[k] = v
		}
	}
	compressMu.Unlock()
	return st
}

// ResetCompressionStats clears process counters (tests / stats-reset).
func ResetCompressionStats() {
	compressProjects.Store(0)
	compressSpills.Store(0)
	compressOriginalBytes.Store(0)
	compressPreviewBytes.Store(0)
	compressMu.Lock()
	compressByTool = nil
	compressMu.Unlock()
}

// FormatCompressionLine is a one-line operator summary; empty when no data.
func FormatCompressionLine() string {
	st := GetCompressionStats()
	if st.Projects <= 0 || st.SavedBytes <= 0 {
		return ""
	}
	return fmt.Sprintf("tool-compress: saved=%s (%d%%) spills=%d projects=%d",
		formatBytes(st.SavedBytes), st.SavedPercent, st.Spills, st.Projects)
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fkB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}
