package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StoreMode controls which backend and sync configuration to use.
type StoreMode string

const (
	// StoreModeJSON uses JSON file persistence (legacy, no cross-instance sync).
	// Default for GUI/TUI single-instance deployments.
	StoreModeJSON StoreMode = "json"

	// StoreModeSQLite uses SQLite WAL persistence with cross-instance sync.
	// Default for maclawsrv multi-instance deployments.
	StoreModeSQLite StoreMode = "sqlite"

	// StoreModeAuto selects SQLite if the environment variable MACLAW_MEMORY_BACKEND
	// is set to "sqlite", otherwise falls back to JSON.
	StoreModeAuto StoreMode = ""
)

// NewStoreWithMode creates a Store with the specified backend mode.
// For SQLite mode, the DB file is placed at {dir}/memory.db and sync is enabled.
// For JSON mode, the file is placed at {dir}/memories.json (legacy behavior).
//
// Parameters:
//   - dir: the directory for memory data (e.g. ~/.maclaw/memory/)
//   - mode: StoreModeJSON, StoreModeSQLite, or StoreModeAuto
//
// When mode is StoreModeAuto, it checks:
//  1. If memory.db already exists → use SQLite
//  2. If MACLAW_MEMORY_BACKEND=sqlite → use SQLite
//  3. Otherwise → use JSON
func NewStoreWithMode(dir string, mode StoreMode) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory: mkdir %s: %w", dir, err)
	}

	resolvedMode := resolveMode(dir, mode)

	switch resolvedMode {
	case StoreModeSQLite:
		return newSQLiteStore(dir)
	default:
		return newJSONStore(dir)
	}
}

func resolveMode(dir string, mode StoreMode) StoreMode {
	if mode == StoreModeSQLite || mode == StoreModeJSON {
		return mode
	}
	// Auto-detect.
	dbPath := filepath.Join(dir, "memory.db")
	if _, err := os.Stat(dbPath); err == nil {
		return StoreModeSQLite // DB already exists, use it.
	}
	if os.Getenv("MACLAW_MEMORY_BACKEND") == "sqlite" {
		return StoreModeSQLite
	}
	return StoreModeJSON
}

func newSQLiteStore(dir string) (*Store, error) {
	dbPath := filepath.Join(dir, "memory.db")

	backend, err := NewSQLiteBackend(dbPath)
	if err != nil {
		return nil, fmt.Errorf("memory: sqlite backend: %w", err)
	}

	// Load entries from SQLite.
	entries, err := backend.LoadAll()
	if err != nil {
		backend.Close()
		return nil, fmt.Errorf("memory: load from sqlite: %w", err)
	}

	// Check if there's a legacy JSON file to migrate.
	jsonPath := filepath.Join(dir, "memories.json")
	if len(entries) == 0 {
		if legacyEntries, migErr := loadLegacyJSON(jsonPath); migErr == nil && len(legacyEntries) > 0 {
			// Migrate legacy entries to SQLite.
			for i := range legacyEntries {
				if err := backend.SaveEntry(&legacyEntries[i]); err != nil {
					backend.Close()
					return nil, fmt.Errorf("memory: migrate entry %s: %w", legacyEntries[i].ID, err)
				}
			}
			entries = legacyEntries
			// Rename legacy file.
			_ = os.Rename(jsonPath, jsonPath+".migrated")
			fmt.Printf("[memory] migrated %d entries from JSON to SQLite\n", len(entries))
		}
	}

	// Also check partition files.
	if len(entries) == 0 {
		pm := newPartitionManager(dir)
		if partEntries, ok := pm.loadPartitions(); ok && len(partEntries) > 0 {
			for i := range partEntries {
				if err := backend.SaveEntry(&partEntries[i]); err != nil {
					backend.Close()
					return nil, fmt.Errorf("memory: migrate partition entry: %w", err)
				}
			}
			entries = partEntries
			// Rename partition files.
			for _, g := range partitionGroups {
				p := filepath.Join(dir, g.FileName)
				_ = os.Rename(p, p+".migrated")
			}
			fmt.Printf("[memory] migrated %d entries from partition files to SQLite\n", len(entries))
		}
	}

	// Create the Store with pre-loaded entries (skip the normal load path).
	store, err := newStoreFromEntries(dir, entries)
	if err != nil {
		backend.Close()
		return nil, err
	}

	// Wire up backend + sync.
	store.SetBackend(backend, SyncConfig{
		Enabled:    true,
		Interval:   DefaultSyncInterval,
		InstanceID: generateInstanceID(),
	})

	return store, nil
}

func newJSONStore(dir string) (*Store, error) {
	jsonPath := filepath.Join(dir, "memories.json")
	return NewStore(jsonPath)
}

// newStoreFromEntries creates a Store with pre-loaded entries, skipping
// the normal file-based load. Used by the SQLite path where entries are
// already loaded from the database.
func newStoreFromEntries(dir string, entries []Entry) (*Store, error) {
	// Use a dummy path (won't be used for persistence since backend handles it).
	dummyPath := filepath.Join(dir, "memories.json")

	s := &Store{
		entries:        entries,
		path:           dummyPath,
		saveCh:         make(chan struct{}, 1),
		stopCh:         make(chan struct{}),
		maxItems:       2000,
		bm25:           newBM25Index(),
		vecIndex:       newVectorIndex(),
		graph:          newMemoryGraph(),
		tmt:            NewTemporalTree(),
		projIndex:      NewProjectIndex(dir),
		semanticGraph:  NewSemanticGraph(),
		entityIndex:    NewEntityIndex(),
		themeManager:   NewThemeManager(),
		queryEmbCache:  make(map[string]queryEmbeddingCacheEntry),
		queryEmbFlight: make(map[string]*queryEmbeddingFlight),
	}

	// Build indices from loaded entries.
	s.rebuildDerivedIndexesLocked(false)

	// Initialize archive store.
	archivePath := filepath.Join(dir, "archive.json")
	archive, err := NewArchiveStore(archivePath)
	if err != nil {
		return nil, fmt.Errorf("memory: init archive: %w", err)
	}
	s.archive = archive

	// Note: persistLoop is NOT started here. In SQLite mode, persistence is
	// handled by the backend on each SaveEntry/UpdateEntry/DeleteEntry call.
	// The persistLoop is only needed for JSON backend mode.

	return s, nil
}

// loadLegacyJSON loads entries from a legacy memories.json file.
func loadLegacyJSON(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []Entry
	if err := jsonUnmarshalEntries(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func jsonUnmarshalEntries(data []byte, entries *[]Entry) error {
	return json.Unmarshal(data, entries)
}

// generateInstanceID creates a short random instance identifier.
func generateInstanceID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("inst-%d", os.Getpid())
	}
	return "inst-" + hex.EncodeToString(buf[:])
}
