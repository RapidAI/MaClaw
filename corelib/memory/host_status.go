package memory

import (
	"sort"
	"strings"
	"time"
)

// HostStatusCatRow is one category row in host-facing status projections.
type HostStatusCatRow struct {
	Category string  `json:"category"`
	Count    int     `json:"count"`
	Percent  float64 `json:"percent"`
}

// HostStatusData is the structured memory status used by GUI/TUI/server hosts.
type HostStatusData struct {
	TotalEntries    int                `json:"total_entries"`
	MaxCapacity     int                `json:"max_capacity"`
	CapacityPercent float64            `json:"capacity_percent"`
	ArchivedEntries int                `json:"archived_entries"`
	StaleEntries    int                `json:"stale_entries"`
	PinnedEntries   int                `json:"pinned_entries"`
	EmbedderActive  bool               `json:"embedder_active"`
	NoEmbedding     int                `json:"no_embedding"`
	OldestEntry     string             `json:"oldest_entry,omitempty"`
	NewestEntry     string             `json:"newest_entry,omitempty"`
	Categories      []HostStatusCatRow `json:"categories"`
}

// ListEntriesForHost returns active memory entries for host management UIs.
func (s *Store) ListEntriesForHost(category Category, keyword string) []Entry {
	if s == nil {
		return nil
	}
	return s.List(category, keyword)
}

// ListArchiveEntriesForHost returns archived memory entries for host management UIs.
func (s *Store) ListArchiveEntriesForHost(category Category, keyword string) []Entry {
	if s == nil {
		return nil
	}
	return s.ListArchive(category, keyword)
}

// SearchEntriesForHost returns ranked memory search results for host UIs and CLIs.
func (s *Store) SearchEntriesForHost(category Category, keyword string, limit int) []Entry {
	if s == nil {
		return nil
	}
	return s.Search(category, keyword, limit)
}

// StatsForHost returns aggregate store statistics for host tools.
func (s *Store) StatsForHost() StoreStats {
	if s == nil {
		return StoreStats{Categories: map[Category]int{}}
	}
	return s.Stats()
}

// FlushForHost persists pending memory changes for host workflows that need a
// durability boundary before reading derived projections.
func (s *Store) FlushForHost() error {
	if s == nil {
		return nil
	}
	return s.Flush()
}

// RecentArtifactTitlesForHost returns display-ready titles for recently created
// task artifacts. Hosts use this projection instead of scanning memory entries
// and duplicating artifact title rules.
func (s *Store) RecentArtifactTitlesForHost(since time.Time, limit int) []string {
	if s == nil || limit <= 0 {
		return nil
	}
	entries := s.EntriesSince(since)
	titles := make([]string, 0, limit)
	for _, entry := range entries {
		if MapToCanonical(entry.Category) != CategoryTaskArtifact {
			continue
		}
		title := artifactTitleForHost(entry)
		if title == "" {
			continue
		}
		titles = append(titles, title)
		if len(titles) >= limit {
			break
		}
	}
	return titles
}

func artifactTitleForHost(entry Entry) string {
	title := strings.TrimSpace(entry.Title)
	if title == "" {
		title = strings.TrimSpace(entry.Content)
	}
	if title == "" {
		return ""
	}
	runes := []rune(strings.ReplaceAll(title, "\n", " "))
	if len(runes) > 50 {
		return string(runes[:50]) + "..."
	}
	return string(runes)
}

// HealthReportForHost returns an aggregate health projection for host UIs.
func (s *Store) HealthReportForHost() *HealthReport {
	if s == nil {
		return &HealthReport{}
	}
	return s.HealthReport()
}

// StatusForHost returns the common structured memory status projection used by
// host UI surfaces. It centralizes category percentages and stable sorting.
func (s *Store) StatusForHost() *HostStatusData {
	if s == nil {
		return &HostStatusData{MaxCapacity: 2000, Categories: []HostStatusCatRow{}}
	}
	hr := s.HealthReport()
	data := &HostStatusData{
		TotalEntries:    hr.ActiveEntries,
		MaxCapacity:     hr.MaxCapacity,
		CapacityPercent: hr.CapacityPercent,
		ArchivedEntries: hr.ArchivedEntries,
		StaleEntries:    hr.StaleEntries,
		PinnedEntries:   hr.PinnedEntries,
		EmbedderActive:  hr.EmbedderActive,
		NoEmbedding:     hr.NoEmbedding,
		OldestEntry:     hr.OldestEntry,
		NewestEntry:     hr.NewestEntry,
		Categories:      []HostStatusCatRow{},
	}
	total := hr.ActiveEntries
	if total == 0 {
		total = 1
	}
	for category, count := range hr.CategoryCounts {
		data.Categories = append(data.Categories, HostStatusCatRow{
			Category: category,
			Count:    count,
			Percent:  float64(count) / float64(total) * 100,
		})
	}
	sort.SliceStable(data.Categories, func(i, j int) bool {
		if data.Categories[i].Count != data.Categories[j].Count {
			return data.Categories[i].Count > data.Categories[j].Count
		}
		return data.Categories[i].Category < data.Categories[j].Category
	})
	return data
}
