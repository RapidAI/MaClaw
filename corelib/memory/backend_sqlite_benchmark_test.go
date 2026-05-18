package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildSQLitePrefilterBenchmarkStore(b *testing.B, n int) (*Store, *sqliteBackend) {
	b.Helper()
	dir := b.TempDir()
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		b.Fatalf("NewSQLiteBackend: %v", err)
	}
	store := &Store{bm25: newBM25Index()}
	now := time.Now().UTC()
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("generic memory note %05d about routine implementation details", i)
		tags := []string{"generic"}
		if i%100 == 0 {
			content = fmt.Sprintf("证据导航 memory note %05d opens recent artifact source and read_file drilldown", i)
			tags = []string{"证据导航", "source"}
		}
		entry := Entry{ID: fmt.Sprintf("entry-%05d", i), Content: content, Category: CategoryProjectKnowledge, Tags: tags, CreatedAt: now, UpdatedAt: now, AccessCount: 1, Strength: 1}
		if err := backend.SaveEntry(&entry); err != nil {
			b.Fatalf("SaveEntry %d: %v", i, err)
		}
		entries = append(entries, entry)
	}
	store.bm25.rebuild(entries)
	b.Cleanup(func() { _ = backend.Close() })
	return store, backend
}

func BenchmarkSQLiteFTSPrefilterThenBM25Subset(b *testing.B) {
	for _, size := range []int{2000, 10000, 50000} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			store, backend := buildSQLitePrefilterBenchmarkStore(b, size)
			query := "证据导航 read_file source"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ids, err := backend.SearchTextIDs(query, 500)
				if err != nil {
					b.Fatal(err)
				}
				allowed := make(map[string]struct{}, len(ids))
				for _, id := range ids {
					allowed[id] = struct{}{}
				}
				_ = store.bm25.scoreSubset(query, allowed)
			}
		})
	}
}

func BenchmarkMemoryBM25FullScanForSQLitePrefilterCorpus(b *testing.B) {
	for _, size := range []int{2000, 10000, 50000} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			store, _ := buildSQLitePrefilterBenchmarkStore(b, size)
			query := "证据导航 read_file source"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = store.bm25.score(query)
			}
		})
	}
}

func TestSQLiteBenchmarkStoreBuildsMemoryDB(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStoreWithMode(dir, StoreModeSQLite)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	b, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("memory.db should be a valid sqlite database: %v", err)
	}
	_ = b.Close()
}

func buildSQLiteFilteredPrefilterBenchmarkStore(b *testing.B, n int) (*Store, *sqliteBackend, sqliteTextFilter) {
	b.Helper()
	dir := b.TempDir()
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		b.Fatalf("NewSQLiteBackend: %v", err)
	}
	store := &Store{bm25: newBM25Index()}
	now := time.Now().UTC()
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		owner := fmt.Sprintf("owner-%02d", i%20)
		category := CategoryProjectKnowledge
		if i%5 == 0 {
			category = CategoryInstruction
		}
		project := fmt.Sprintf("D:/workprj/project-%02d", i%50)
		content := fmt.Sprintf("generic memory note %05d about routine implementation details", i)
		tags := []string{"generic", project}
		if i%1000 == 0 {
			owner = "owner-07"
			category = CategoryProjectKnowledge
			project = "D:/workprj/project-13"
			content = fmt.Sprintf("证据导航 memory note %05d opens recent artifact source and read_file drilldown", i)
			tags = []string{"证据导航", "source", project}
		}
		entry := Entry{
			ID:          fmt.Sprintf("filtered-entry-%05d", i),
			Content:     content,
			Category:    category,
			OwnerID:     owner,
			Tags:        tags,
			SourceURL:   "file://" + project + "/artifact.md",
			CreatedAt:   now.Add(-time.Duration(i%720) * time.Hour),
			UpdatedAt:   now,
			AccessCount: 1,
			Strength:    1,
		}
		if err := backend.SaveEntry(&entry); err != nil {
			b.Fatalf("SaveEntry %d: %v", i, err)
		}
		entries = append(entries, entry)
	}
	store.bm25.rebuild(entries)
	b.Cleanup(func() { _ = backend.Close() })
	filter := sqliteTextFilter{OwnerID: "owner-07", Category: CategoryProjectKnowledge, ProjectPath: "D:/workprj/project-13", Since: now.Add(-30 * 24 * time.Hour)}
	return store, backend, filter
}

func BenchmarkSQLiteFilteredFTSPrefilterThenBM25Subset(b *testing.B) {
	for _, size := range []int{2000, 10000, 50000} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			store, backend, filter := buildSQLiteFilteredPrefilterBenchmarkStore(b, size)
			query := "证据导航 read_file source"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ids, err := backend.SearchTextIDsFiltered(query, filter, 500)
				if err != nil {
					b.Fatal(err)
				}
				allowed := make(map[string]struct{}, len(ids))
				for _, id := range ids {
					allowed[id] = struct{}{}
				}
				_ = store.bm25.scoreSubset(query, allowed)
			}
		})
	}
}

func BenchmarkMemoryBM25FullScanWithInMemoryFilters(b *testing.B) {
	for _, size := range []int{2000, 10000, 50000} {
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			store, _, filter := buildSQLiteFilteredPrefilterBenchmarkStore(b, size)
			query := "璇佹嵁瀵艰埅 read_file source"
			byID := make(map[string]Entry, len(store.entries))
			for _, entry := range store.entries {
				byID[entry.ID] = entry
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				scores := store.bm25.score(query)
				kept := make(map[string]float64, len(scores))
				for id, score := range scores {
					if sqliteBenchmarkEntryMatchesFilter(byID[id], filter) {
						kept[id] = score
					}
				}
				_ = kept
			}
		})
	}
}
func sqliteBenchmarkEntryMatchesFilter(entry Entry, filter sqliteTextFilter) bool {
	if filter.OwnerID != "" && entry.OwnerID != "" && entry.OwnerID != filter.OwnerID {
		return false
	}
	if filter.Category != "" && entry.Category != filter.Category {
		return false
	}
	if !filter.Since.IsZero() && entry.CreatedAt.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && entry.CreatedAt.After(filter.Until) {
		return false
	}
	project := strings.ToLower(filter.ProjectPath)
	if project == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.SourceURL), project) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), project) {
			return true
		}
	}
	return false
}
