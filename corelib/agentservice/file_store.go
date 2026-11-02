package agentservice

// FileStore keeps the service control-plane state durable for the default
// "file" backend. It persists to SQLite (normalized per-entity tables with
// indexed lookup columns, same engine as the "sqlite" backend); the given
// store.json is only the pre-migration file: a fresh database imports it
// once and it is then removed. All Store, run-lifecycle, and run-event
// semantics come from the embedded SQLiteStore.

import (
	"os"
	"path/filepath"
	"strings"
)

type FileStore struct {
	*SQLiteStore
}

func NewFileStore(path string) (*FileStore, error) {
	jsonPath, dbPath := fileStorePaths(path)
	dbExisted := fileExists(dbPath)
	sqlite, err := NewSQLiteStoreWithLegacy(dbPath, jsonPath)
	if err != nil {
		return nil, err
	}
	if !dbExisted && jsonPath != "" && fileExists(jsonPath) {
		// Fresh database: initializeRows imported the legacy file above.
		removeFileStoreJSON(jsonPath)
	}
	return &FileStore{SQLiteStore: sqlite}, nil
}

// fileStorePaths maps the legacy JSON path to its database path: a .json
// file swaps extension, a directory hosts store.db, anything else is
// already a database path.
func fileStorePaths(path string) (jsonPath, dbPath string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "", filepath.Join(path, "store.db")
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return path, strings.TrimSuffix(path, filepath.Ext(path)) + ".db"
	}
	return "", path
}

func removeFileStoreJSON(jsonPath string) {
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = os.Remove(jsonPath + ".tmp")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
