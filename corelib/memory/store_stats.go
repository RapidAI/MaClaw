package memory

import (
	"fmt"
	"sort"
	"strings"
)

// StoreStats is a frontend-neutral summary of the memory store.
type StoreStats struct {
	Total          int                   `json:"total"`
	Active         int                   `json:"active"`
	Dormant        int                   `json:"dormant"`
	Superseded     int                   `json:"superseded"`
	WithEmbedding  int                   `json:"with_embedding"`
	WithGraphLinks int                   `json:"with_graph_links"`
	ScopeGlobal    int                   `json:"scope_global"`
	ScopeProject   int                   `json:"scope_project"`
	TierSemantic   int                   `json:"tier_semantic"`
	TierEpisodic   int                   `json:"tier_episodic"`
	Candidates     MemoryCandidateHealth `json:"candidates"`
	Theme          ThemeHealth           `json:"theme"`
	Categories     map[Category]int      `json:"categories"`
}

// Stats returns aggregate store health shared by CLI, GUI, and services.
func (s *Store) Stats() StoreStats {
	stats := StoreStats{Categories: map[Category]int{}}
	if s == nil {
		return stats
	}
	entries := s.List("", "")
	stats.Total = len(entries)
	stats.Candidates = s.MemoryCandidateHealth()
	for _, entry := range entries {
		stats.Categories[entry.Category]++
		switch entry.Status {
		case StatusDormant:
			stats.Dormant++
		case StatusSuperseded:
			stats.Superseded++
		default:
			stats.Active++
		}
		if len(entry.Embedding) > 0 {
			stats.WithEmbedding++
		}
		if len(entry.RelatedIDs) > 0 {
			stats.WithGraphLinks++
		}
		if entry.Scope == ScopeGlobal {
			stats.ScopeGlobal++
		} else {
			stats.ScopeProject++
		}
		if entry.Category.Tier() == TierSemantic {
			stats.TierSemantic++
		} else {
			stats.TierEpisodic++
		}
	}
	s.EnsureThemesUpToDate()
	stats.Theme = s.ThemeHealth()
	return stats
}

// FormatStoreStatsForTool renders store stats in a stable, human-readable form.
func FormatStoreStatsForTool(stats StoreStats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Memory Store Stats:\n")
	fmt.Fprintf(&b, "  Total entries:    %d\n", stats.Total)
	fmt.Fprintf(&b, "  Active:           %d\n", stats.Active)
	fmt.Fprintf(&b, "  Dormant:          %d\n", stats.Dormant)
	fmt.Fprintf(&b, "  Superseded:       %d\n", stats.Superseded)
	fmt.Fprintf(&b, "  Candidates:       %d (accept=%d quarantine=%d reject=%d stale=%d)\n",
		stats.Candidates.Total, stats.Candidates.Accept, stats.Candidates.Quarantine, stats.Candidates.Reject, stats.Candidates.Stale)
	fmt.Fprintf(&b, "  With embedding:   %d\n", stats.WithEmbedding)
	fmt.Fprintf(&b, "  With graph links: %d\n", stats.WithGraphLinks)
	fmt.Fprintf(&b, "  Scope global:     %d\n", stats.ScopeGlobal)
	fmt.Fprintf(&b, "  Scope project:    %d\n", stats.ScopeProject)
	fmt.Fprintf(&b, "  Tier semantic:    %d\n", stats.TierSemantic)
	fmt.Fprintf(&b, "  Tier episodic:    %d\n", stats.TierEpisodic)
	fmt.Fprintf(&b, "  Theme count:      %d\n", stats.Theme.ThemeCount)
	fmt.Fprintf(&b, "  Theme coverage:   %.2f (%d/%d)\n", stats.Theme.CoverageRate, stats.Theme.CoveredEntries, stats.Theme.ActiveEligibleEntries)
	fmt.Fprintf(&b, "  Theme isolated:   %d\n", stats.Theme.IsolatedThemes)
	fmt.Fprintf(&b, "  Categories:\n")
	categories := make([]Category, 0, len(stats.Categories))
	for category := range stats.Categories {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i] < categories[j] })
	for _, category := range categories {
		fmt.Fprintf(&b, "    %-25s %d\n", category, stats.Categories[category])
	}
	return b.String()
}

// FormatCandidateConsolidationForTool renders the offline candidate pass result.
func FormatCandidateConsolidationForTool(result CandidateConsolidationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Candidate consolidation: scanned=%d promoted=%d merged=%d rejected=%d kept=%d\n",
		result.Scanned, result.Promoted, result.Merged, result.Rejected, result.Kept)
	if len(result.Errors) > 0 {
		fmt.Fprintf(&b, "Errors: %s\n", strings.Join(result.Errors, "; "))
	}
	return b.String()
}
