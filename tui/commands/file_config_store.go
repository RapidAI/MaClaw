package commands

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// FileConfigStore keeps the historical name while sharing the GUI's SQLite store.
type FileConfigStore struct {
	store *config.SQLiteStore
}

// NewFileConfigStore creates a ConfigStore rooted at dataDir.
func NewFileConfigStore(dataDir string) *FileConfigStore {
	return &FileConfigStore{store: config.NewSQLiteStore(dataDir)}
}

func (s *FileConfigStore) LoadConfig() (corelib.AppConfig, error) {
	return s.store.LoadConfig()
}

func (s *FileConfigStore) SaveConfig(cfg corelib.AppConfig) error {
	return s.store.SaveConfig(cfg)
}
