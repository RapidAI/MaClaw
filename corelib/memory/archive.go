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

type archiveTransitionEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	IDs       []string  `json:"ids"`
	Count     int       `json:"count"`
}

// ArchiveStore manages cold storage for evicted memory entries.
type ArchiveStore struct {
	mu        sync.RWMutex
	entries   []Entry
	path      string
	auditPath string
	dirty     bool
	saveCh    chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	maxItems  int
}

// NewArchiveStore creates an ArchiveStore that persists to the given path.
func NewArchiveStore(path string) (*ArchiveStore, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("archive_store: resolve path: %w", err)
	}

	a := &ArchiveStore{
		entries:   make([]Entry, 0),
		path:      absPath,
		auditPath: absPath + ".transitions.jsonl",
		saveCh:    make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		maxItems:  1000,
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
	a.addLocked(entries)
	a.dirty = true
	a.signalSave()
	return nil
}

// AddDurable adds entries and flushes archive storage before returning. Cross
// store transitions use this so an active tombstone never precedes cold storage.
func (a *ArchiveStore) AddDurable(entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addLocked(entries)
	if err := a.flushLocked(); err != nil {
		return err
	}
	if err := a.appendTransition("archive_add_durable", entryIDs(entries)); err != nil {
		log.Printf("[archive_store] WARNING: transition audit add failed: %v", err)
	}
	return nil
}

func (a *ArchiveStore) addLocked(entries []Entry) {
	byID := make(map[string]int, len(a.entries)+len(entries))
	for i := range a.entries {
		if a.entries[i].ID != "" {
			byID[a.entries[i].ID] = i
		}
	}
	for _, entry := range entries {
		if entry.ID != "" {
			if idx, ok := byID[entry.ID]; ok {
				a.entries[idx] = entry
				continue
			}
			byID[entry.ID] = len(a.entries)
		}
		a.entries = append(a.entries, entry)
	}

	// Enforce capacity: evict oldest entries by UpdatedAt.
	if len(a.entries) > a.maxItems {
		sort.SliceStable(a.entries, func(i, j int) bool {
			return a.entries[i].UpdatedAt.Before(a.entries[j].UpdatedAt)
		})
		a.entries = a.entries[len(a.entries)-a.maxItems:]
	}
}

// RemoveIDs removes and returns all archived entries whose IDs are present.
// The returned entries follow the requested ID order; missing IDs are skipped.
func (a *ArchiveStore) RemoveIDs(ids []string) ([]Entry, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	removed := a.removeIDsLocked(ids)
	if len(removed) == 0 {
		return nil, nil
	}
	a.dirty = true
	a.signalSave()
	return removed, nil
}

// RemoveIDsDurable removes archived entries and flushes archive storage before
// returning. It is used after active restore succeeds; failure leaves a harmless
// duplicate in cold storage instead of losing active memory.
func (a *ArchiveStore) RemoveIDsDurable(ids []string) ([]Entry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	removed := a.removeIDsLocked(ids)
	if len(removed) == 0 {
		return nil, nil
	}
	if err := a.flushLocked(); err != nil {
		return nil, err
	}
	if err := a.appendTransition("archive_remove_durable", entryIDs(removed)); err != nil {
		log.Printf("[archive_store] WARNING: transition audit remove failed: %v", err)
	}
	return removed, nil
}

func (a *ArchiveStore) removeIDsLocked(ids []string) []Entry {
	requested := make(map[string]int, len(ids))
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := requested[id]; ok {
			continue
		}
		requested[id] = len(ordered)
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return nil
	}

	removedByID := make(map[string]Entry, len(ordered))
	kept := a.entries[:0]
	for _, entry := range a.entries {
		if _, ok := requested[entry.ID]; ok {
			removedByID[entry.ID] = entry
			continue
		}
		kept = append(kept, entry)
	}
	if len(removedByID) == 0 {
		return nil
	}
	a.entries = kept
	removed := make([]Entry, 0, len(removedByID))
	for _, id := range ordered {
		if entry, ok := removedByID[id]; ok {
			removed = append(removed, entry)
		}
	}
	return removed
}

// EntriesByIDs returns archived entries in requested ID order without removing
// them. Missing IDs and duplicate requests are skipped.
func (a *ArchiveStore) EntriesByIDs(ids []string) []Entry {
	if len(ids) == 0 {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	byID := make(map[string]Entry, len(a.entries))
	for _, entry := range a.entries {
		if entry.ID != "" {
			byID[entry.ID] = entry
		}
	}
	seen := make(map[string]struct{}, len(ids))
	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if entry, ok := byID[id]; ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// Remove removes and returns the entry with the given ID from the archive.
// Used for restoring entries back to active memory.
func (a *ArchiveStore) Remove(id string) (*Entry, error) {
	removed, err := a.RemoveIDs([]string{id})
	if err != nil {
		return nil, err
	}
	if len(removed) > 0 {
		return &removed[0], nil
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
// ownerID is used for multi-tenant isolation: only entries belonging to the
// specified owner (or shared entries with empty OwnerID) are returned.
func (a *ArchiveStore) FindRelevant(tags []string, categories []Category, limit int, ownerID string) []Entry {
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
		// 多租户隔离：跳过不属于该用户的记忆
		// 空 OwnerID 表示共享记忆，对所有用户可见
		if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
			continue
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

func (a *ArchiveStore) flushLocked() error {
	data, err := json.MarshalIndent(a.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("archive_store: marshal: %w", err)
	}
	if err := fileutil.AtomicWriteFile(a.path, data, 0o644); err != nil {
		return fmt.Errorf("archive_store: write file: %w", err)
	}
	a.dirty = false
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

func (a *ArchiveStore) appendTransition(action string, ids []string) error {
	if a == nil || a.auditPath == "" || action == "" || len(ids) == 0 {
		return nil
	}
	event := archiveTransitionEvent{
		Timestamp: time.Now().UTC(),
		Action:    action,
		IDs:       ids,
		Count:     len(ids),
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("archive_store: marshal transition: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.auditPath), 0o755); err != nil {
		return fmt.Errorf("archive_store: transition dir: %w", err)
	}
	f, err := os.OpenFile(a.auditPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("archive_store: transition open: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("archive_store: transition write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("archive_store: transition sync: %w", err)
	}
	return nil
}

func entryIDs(entries []Entry) []string {
	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// Flush writes current entries to disk immediately.
func (a *ArchiveStore) Flush() error { return a.flush() }
