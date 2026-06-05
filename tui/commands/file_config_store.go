package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// FileConfigStore implements config.ConfigStore with a local JSON file.
type FileConfigStore struct {
	path string
}

// NewFileConfigStore creates a file-backed ConfigStore.
func NewFileConfigStore(dataDir string) *FileConfigStore {
	return &FileConfigStore{path: filepath.Join(dataDir, "config.json")}
}

// LoadConfig loads config from disk.
func (s *FileConfigStore) LoadConfig() (corelib.AppConfig, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return corelib.AppConfigDefaults(), nil
		}
		return corelib.AppConfig{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return corelib.AppConfigDefaults(), nil
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return corelib.AppConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// SaveConfig writes config atomically so Windows updates can replace an existing file.
func (s *FileConfigStore) SaveConfig(cfg corelib.AppConfig) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := fileutil.AtomicWriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
