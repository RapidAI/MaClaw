package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FormatRecallEntryForTool renders a memory entry for explicit recall tools.
// It keeps the recalled summary compact while preserving a drill-down path to
// the original source when one is available.
func FormatRecallEntryForTool(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] %s", string(e.Category), e.Content)

	if e.SourceType != "" || e.SourceURL != "" {
		parts := make([]string, 0, 2)
		if e.SourceType != "" {
			parts = append(parts, "source_type="+e.SourceType)
		}
		if e.SourceURL != "" {
			parts = append(parts, "source_url="+e.SourceURL)
		}
		fmt.Fprintf(&b, "\n  source: %s", strings.Join(parts, ", "))
	}

	if e.SourceURL != "" && LooksLikeFilePath(e.SourceURL) {
		fmt.Fprintf(&b, "\n  drill_down: use read_file with path=%q to inspect the full source/evidence", e.SourceURL)
	}

	if derived := formatDerivedMemoryMetadata(e); derived != "" {
		fmt.Fprintf(&b, "\n  derived: %s", derived)
	}

	return b.String()
}

func formatDerivedMemoryMetadata(e Entry) string {
	parts := make([]string, 0, 3)
	if e.DerivedKind != "" {
		parts = append(parts, "kind="+e.DerivedKind)
	}
	if len(e.EvidenceIDs) > 0 {
		parts = append(parts, "evidence_ids="+formatEvidenceIDList(e.EvidenceIDs, 5))
	}
	if boundary := formatMemoryBoundary(e.Boundary); boundary != "" {
		parts = append(parts, "boundary={"+boundary+"}")
	}
	return strings.Join(parts, ", ")
}

func formatEvidenceIDList(ids []string, limit int) string {
	if len(ids) == 0 {
		return ""
	}
	if limit <= 0 || len(ids) <= limit {
		return strings.Join(ids, ",")
	}
	return strings.Join(ids[:limit], ",") + fmt.Sprintf("(+%d)", len(ids)-limit)
}

func formatMemoryBoundary(boundary *MemoryBoundary) string {
	if boundary == nil {
		return ""
	}
	parts := make([]string, 0, 8)
	if boundary.ProjectPath != "" {
		parts = append(parts, "project_path="+boundary.ProjectPath)
	}
	if boundary.OwnerID != "" {
		parts = append(parts, "owner_id="+boundary.OwnerID)
	}
	if boundary.TaskType != "" {
		parts = append(parts, "task_type="+boundary.TaskType)
	}
	if boundary.Workflow != "" {
		parts = append(parts, "workflow="+boundary.Workflow)
	}
	if boundary.Toolchain != "" {
		parts = append(parts, "toolchain="+boundary.Toolchain)
	}
	if boundary.SourceScope != "" {
		parts = append(parts, "source_scope="+boundary.SourceScope)
	}
	if boundary.Since != nil {
		parts = append(parts, "since="+boundary.Since.UTC().Format(time.RFC3339))
	}
	if boundary.Until != nil {
		parts = append(parts, "until="+boundary.Until.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, ",")
}

// FormatRecallEntryForPrompt renders a compact memory line for automatic prompt
// injection. It favors the compact form and keeps source hints short so prompt
// context remains lightweight while preserving a path to full evidence.
func FormatRecallEntryForPrompt(e Entry, maxRunes int) string {
	text := e.CompactForm
	if text == "" {
		text = e.Content
	}
	if maxRunes > 0 {
		runes := []rune(text)
		if len(runes) > maxRunes {
			text = string(runes[:maxRunes]) + "..."
		}
	}

	line := fmt.Sprintf("- [%s] %s", string(e.Category), text)
	if e.SourceURL == "" {
		return line
	}

	line += fmt.Sprintf(" (source: %s", e.SourceURL)
	if LooksLikeFilePath(e.SourceURL) {
		line += "; full: read_file"
	}
	line += ")"
	return line
}

// FormatRecallTraceForTool renders compact retrieval diagnostics for debug
// output. It exposes counts and IDs only, keeping recalled content separate.
func FormatRecallTraceForTool(trace RecallTrace) string {
	if strings.TrimSpace(trace.Query) == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Recall trace: query=%q", trace.Query)
	if trace.Category != "" {
		fmt.Fprintf(&b, " category=%s", trace.Category)
	}
	if trace.ProjectPath != "" {
		fmt.Fprintf(&b, " project=%q", trace.ProjectPath)
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "  tokens: query_tokens=%v bm25_tokens=%v entities=%v\n", trace.QueryTokens, trace.BM25Tokens, trace.Entities)
	fmt.Fprintf(&b, "  hits: bm25_hits=%d vector_hits=%d semantic_hits=%d candidates=%d results=%v\n",
		trace.BM25Hits, trace.VectorHits, trace.SemanticHits, trace.CandidateCount, trace.ResultEntryIDs)
	if len(trace.SourceCounts) > 0 {
		keys := make([]string, 0, len(trace.SourceCounts))
		for key := range trace.SourceCounts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", key, trace.SourceCounts[key]))
		}
		fmt.Fprintf(&b, "  source_counts: %s\n", strings.Join(parts, ", "))
	}
	return b.String()
}
