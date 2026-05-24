package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// jsonFileBackend implements StorageBackend using JSON files (single file or
// partitioned). This is the default backend for GUI/TUI single-instance mode.
// It preserves the existing persistence behavior: debounced async writes via
// a background goroutine.
type jsonFileBackend struct {
	path    string
	partMgr *partitionManager

	// Async persistence (mirrors the original Store.persistLoop behavior)
	dirty    bool
	dirtyGen uint64
	saveCh   chan struct{}
	stopCh   chan struct{}
}

// NewJSONFileBackend creates a JSON file backend at the given path.
// The path should point to the legacy memories.json location; partition
// files are stored in the same directory.
func NewJSONFileBackend(path string) (*jsonFileBackend, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("json_backend: resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("json_backend: create dir: %w", err)
	}

	b := &jsonFileBackend{
		path:    absPath,
		partMgr: newPartitionManager(dir),
		saveCh:  make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
	}

	go b.persistLoop()
	return b, nil
}

// LoadAll loads entries from partition files or the legacy single JSON file.
func (b *jsonFileBackend) LoadAll() ([]Entry, error) {
	// Try partition files first.
	if entries, ok := b.partMgr.loadPartitions(); ok {
		b.partMgr.enable()
		log.Printf("[json_backend] loaded %d entries from partition files", len(entries))
		return entries, nil
	}

	// Fall back to legacy single file.
	data, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // fresh install
		}
		return nil, fmt.Errorf("json_backend: read file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		backupPath := b.path + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backupPath, data, 0o644)
		log.Printf("[json_backend] WARNING: corrupted memory file backed up to %s, starting with empty memory", backupPath)
		return nil, nil
	}

	// Migrate to partitions when large enough.
	const migrationThreshold = 100
	if len(entries) >= migrationThreshold {
		if err := b.partMgr.migrateFromLegacy(entries, b.path); err != nil {
			log.Printf("[json_backend] WARNING: partition migration failed: %v, continuing with legacy mode", err)
		}
	}

	return entries, nil
}

// SaveEntry signals that entries have changed and need to be flushed.
// The actual write happens asynchronously via persistLoop.
// Note: The JSON backend does not use per-entry persistence; it flushes
// the entire entries slice. The Store calls FlushAll after mutations.
func (b *jsonFileBackend) SaveEntry(entry *Entry) error {
	b.signalSave()
	return nil
}

// UpdateEntry signals that entries have changed.
func (b *jsonFileBackend) UpdateEntry(entry *Entry) error {
	b.signalSave()
	return nil
}

func (b *jsonFileBackend) UpdateEntries(entries []*Entry) error {
	b.signalSave()
	return nil
}

func (b *jsonFileBackend) UpdateEntriesAndDeleteIDs(entries []*Entry, deleteIDs []string) error {
	b.signalSave()
	return nil
}

// DeleteEntry signals that entries have changed.
func (b *jsonFileBackend) DeleteEntry(id string) error {
	b.signalSave()
	return nil
}

// Since is not supported by the JSON backend.
func (b *jsonFileBackend) Since(version int64) ([]Entry, []string, error) {
	return nil, nil, nil
}

// MaxVersion is not supported by the JSON backend.
func (b *jsonFileBackend) MaxVersion() (int64, error) {
	return 0, nil
}

// SupportsSync returns false — JSON backend does not support cross-instance sync.
func (b *jsonFileBackend) SupportsSync() bool {
	return false
}

// Close stops the persist loop.
func (b *jsonFileBackend) Close() error {
	close(b.stopCh)
	return nil
}

// FlushAll writes all entries to disk. Called by the Store when it needs
// to persist the current state (the JSON backend needs the full entries slice
// because it doesn't track individual changes).
func (b *jsonFileBackend) FlushAll(entries []Entry) error {
	if b.partMgr != nil && b.partMgr.isEnabled() {
		b.partMgr.markAllDirty()
		_, err := b.partMgr.flushDirty(entries)
		return err
	}

	// Legacy single-file flush.
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("json_backend: marshal: %w", err)
	}
	return fileutil.AtomicWriteFile(b.path, data, 0o644)
}

// Path returns the file path for this backend.
func (b *jsonFileBackend) Path() string {
	return b.path
}

// --- Internal async persistence ---

func (b *jsonFileBackend) signalSave() {
	atomic.AddUint64(&b.dirtyGen, 1)
	b.dirty = true
	select {
	case b.saveCh <- struct{}{}:
	default:
	}
}

func (b *jsonFileBackend) persistLoop() {
	for {
		select {
		case <-b.stopCh:
			return
		case <-b.saveCh:
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-b.stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
			// Drain any additional signals that arrived during the wait.
			select {
			case <-b.saveCh:
			default:
			}
			// Note: The actual flush is triggered by the Store calling FlushAll.
			// The persist loop here just provides the debounce timing signal.
			// The Store's Flush() method will call b.FlushAll(entries).
			b.dirty = false
		}
	}
}
