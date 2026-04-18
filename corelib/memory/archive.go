package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// ArchiveStore manages cold storage for evicted memory entries.
type ArchiveStore struct {
	mu       sync.RWMutex
	entries  []Entry
	path     string
	dirty    bool
	saveCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	maxItems int
}

// NewArchiveStore creates an ArchiveStore that persists to the given path.
func NewArchiveStore(path string) (*ArchiveStore, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("archive_store: resolve path: %w", err)
	}

	a := &ArchiveStore{
		entries:  make([]Entry, 0),
		path:     absPath,
		saveCh:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		maxItems: 1000,
	}

	if err := a.load(); err != nil {
		return nil, err
	}

	go a.persistLoop()
	return a, nil
}

// Add appends entries to the archive. If the archive exceeds maxItems,
// the oldest entries (by UpdatedAt) are evicted first.
func (a *ArchiveStore) Add(entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries = append(a.entries, entries...)

	// Enforce capacity: evict oldest entries by UpdatedAt.
	if len(a.entries) > a.maxItems {
		sort.SliceStable(a.entries, func(i, j int) bool {
			return a.entries[i].UpdatedAt.Before(a.entries[j].UpdatedAt)
		})
		a.entries = a.entries[len(a.entries)-a.maxItems:]
	}

	a.dirty = true
	a.signalSave()
	return nil
}

// Remove removes and returns the entry with the given ID from the archive.
// Used for restoring entries back to active memory.
func (a *ArchiveStore) Remove(id string) (*Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i, e := range a.entries {
		if e.ID == id {
			removed := a.entries[i]
			a.entries = append(a.entries[:i], a.entries[i+1:]...)
			a.dirty = true
			a.signalSave()
			return &removed, nil
		}
	}
	return nil, fmt.Errorf("archive_store: entry %q not found", id)
}

// List returns archived entries filtered by category and keyword.
func (a *ArchiveStore) List(category Category, keyword string) []Entry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	kw := strings.ToLower(keyword)
	var result []Entry
	for _, e := range a.entries {
		if category != "" && e.Category != category {
			continue
		}
		if kw != "" && !containsKeyword(e, kw) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// FindRelevant returns archived entries that match any of the given tags or
// categories, limited to `limit` results. Used by GC to revive relevant
// archived entries.
func (a *ArchiveStore) FindRelevant(tags []string, categories []Category, limit int) []Entry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}
	catSet := make(map[Category]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	var result []Entry
	for _, e := range a.entries {
		if len(result) >= limit {
			break
		}
		// Match by category.
		if catSet[e.Category] {
			result = append(result, e)
			continue
		}
		// Match by tag overlap.
		for _, et := range e.Tags {
			if tagSet[strings.ToLower(et)] {
				result = append(result, e)
				break
			}
		}
	}
	return result
}

// Count returns the number of archived entries.
func (a *ArchiveStore) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.entries)
}

// Stop gracefully shuts down the persistence loop.
func (a *ArchiveStore) Stop() {
	a.stopOnce.Do(func() {
		a.mu.RLock()
		dirty := a.dirty
		a.mu.RUnlock()

		if dirty {
			_ = a.flush()
			a.mu.Lock()
			a.dirty = false
			a.mu.Unlock()
		}

		close(a.stopCh)
	})
}

// ---------------------------------------------------------------------------
// Persistence internals
// ---------------------------------------------------------------------------

func (a *ArchiveStore) load() error {
	dir := filepath.Dir(a.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("archive_store: create dir: %w", err)
	}

	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // silently create empty archive
		}
		return fmt.Errorf("archive_store: read file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		backupPath := a.path + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backupPath, data, 0o644)
		log.Printf("[archive_store] WARNING: corrupted archive file backed up to %s, starting with empty archive", backupPath)
		a.entries = make([]Entry, 0)
		return nil
	}
	a.entries = entries
	return nil
}

func (a *ArchiveStore) flush() error {
	a.mu.RLock()
	data, err := json.MarshalIndent(a.entries, "", "  ")
	a.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("archive_store: marshal: %w", err)
	}
	if err := fileutil.AtomicWriteFile(a.path, data, 0o644); err != nil {
		return fmt.Errorf("archive_store: write file: %w", err)
	}
	a.mu.Lock()
	a.dirty = false
	a.mu.Unlock()
	return nil
}

func (a *ArchiveStore) persistLoop() {
	for {
		select {
		case <-a.stopCh:
			return
		case <-a.saveCh:
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-a.stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-a.saveCh:
			default:
			}
			_ = a.flush()
		}
	}
}

func (a *ArchiveStore) signalSave() {
	select {
	case a.saveCh <- struct{}{}:
	default:
	}
}

// Flush writes current entries to disk immediately.
func (a *ArchiveStore) Flush() error { return a.flush() }
