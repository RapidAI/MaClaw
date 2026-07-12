package toolresult

import (
	"fmt"
	"sync"
	"sync/atomic"
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
	Projects       int64            `json:"projects"`
	Spills         int64            `json:"spills"`
	OriginalBytes  int64            `json:"original_bytes"`
	PreviewBytes   int64            `json:"preview_bytes"`
	SavedBytes     int64            `json:"saved_bytes"`
	SavedPercent   int              `json:"saved_percent,omitempty"`
	ByToolSaved    map[string]int64 `json:"by_tool_saved,omitempty"`
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
