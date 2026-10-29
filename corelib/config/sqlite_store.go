package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configdb"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

const ConfigMigrationBackupSuffix = ".migrated.bak"

type SQLiteStore struct {
	jsonPath       string
	dbPath         string
	legacyFallback atomic.Bool
}

func NewSQLiteStore(dataDir string) *SQLiteStore {
	return NewSQLiteStoreForJSONPath(configdb.JSONPath(dataDir))
}

func NewSQLiteStoreForJSONPath(jsonPath string) *SQLiteStore {
	return &SQLiteStore{
		jsonPath: jsonPath,
		dbPath:   ConfigDBPathForJSON(jsonPath),
	}
}

func (s *SQLiteStore) DBPath() string {
	return s.dbPath
}

func (s *SQLiteStore) JSONPath() string {
	return s.jsonPath
}

func (s *SQLiteStore) UsingLegacyJSON() bool {
	return s.legacyFallback.Load()
}

func (s *SQLiteStore) StoragePath() string {
	if s.UsingLegacyJSON() {
		return s.jsonPath
	}
	if _, err := os.Stat(s.dbPath); err == nil {
		return s.dbPath
	}
	if _, err := os.Stat(s.jsonPath); err == nil {
		return s.jsonPath
	}
	return s.dbPath
}

func (s *SQLiteStore) LoadConfig() (corelib.AppConfig, error) {
	if s.UsingLegacyJSON() {
		return s.loadLegacyJSON()
	}
	entries, err := configdb.LoadEntries(s.dbPath)
	if err == nil && len(entries) > 0 {
		return decodeConfigEntries(entries)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if cfg, jsonErr := s.loadLegacyJSON(); jsonErr == nil && fileExists(s.jsonPath) {
			s.legacyFallback.Store(true)
			log.Printf("[config] SQLite load failed; using legacy JSON only: %v", err)
			return cfg, nil
		}
		return corelib.AppConfig{}, err
	}
	if !fileExists(s.jsonPath) {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return corelib.AppConfig{}, err
		}
		return corelib.AppConfigDefaults(), nil
	}
	legacy, err := s.loadLegacyJSON()
	if err != nil {
		return corelib.AppConfig{}, err
	}
	if err := s.migrateLegacyJSON(); err != nil {
		s.legacyFallback.Store(true)
		log.Printf("[config] SQLite migration failed; using legacy JSON only: %v", err)
		return legacy, nil
	}
	entries, err = configdb.LoadEntries(s.dbPath)
	if err != nil {
		s.legacyFallback.Store(true)
		log.Printf("[config] SQLite migration verification failed; using legacy JSON only: %v", err)
		return legacy, nil
	}
	return decodeConfigEntries(entries)
}

func (s *SQLiteStore) SaveConfig(cfg corelib.AppConfig) error {
	corelib.ApplyZhipuCodingConfigMigration(&cfg)
	if s.UsingLegacyJSON() {
		return configfile.AtomicWriteJSON(s.jsonPath, cfg)
	}
	entries, err := encodeConfigEntries(cfg)
	if err != nil {
		return err
	}
	if err := configdb.ReplaceManagedEntries(s.dbPath, entries, appConfigJSONKeys()); err != nil {
		if fileExists(s.jsonPath) {
			s.legacyFallback.Store(true)
			log.Printf("[config] SQLite save failed; using legacy JSON only: %v", err)
			return configfile.AtomicWriteJSON(s.jsonPath, cfg)
		}
		return err
	}
	if fileExists(s.jsonPath) {
		s.backupLegacyJSON()
	}
	return nil
}

func (s *SQLiteStore) migrateLegacyJSON() error {
	data, err := os.ReadFile(s.jsonPath)
	if err != nil {
		return err
	}
	entries, err := decodeLegacyEntries(data)
	if err != nil {
		return err
	}
	if err := configdb.SaveEntries(s.dbPath, entries); err != nil {
		return err
	}
	s.backupLegacyJSON()
	return nil
}

func (s *SQLiteStore) backupLegacyJSON() {
	backupPath := s.jsonPath + ConfigMigrationBackupSuffix
	if fileExists(backupPath) {
		log.Printf("[config] migration backup already exists; retaining legacy JSON at %q", s.jsonPath)
		return
	}
	if err := os.Rename(s.jsonPath, backupPath); err != nil {
		log.Printf("[config] SQLite data committed but legacy JSON backup failed; retaining %q: %v", s.jsonPath, err)
	}
}

func (s *SQLiteStore) loadLegacyJSON() (corelib.AppConfig, error) {
	data, err := os.ReadFile(s.jsonPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

func decodeLegacyEntries(data []byte) (map[string]json.RawMessage, error) {
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse legacy config: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse legacy config fields: %w", err)
	}
	entries := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		if key == "" || !json.Valid(value) {
			continue
		}
		entries[key] = value
	}
	return entries, nil
}

var (
	appConfigKeysOnce sync.Once
	appConfigKeys     []string
)

func appConfigJSONKeys() []string {
	appConfigKeysOnce.Do(func() {
		typ := reflect.TypeOf(corelib.AppConfig{})
		keys := make([]string, 0, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			name, _, ok := configJSONName(typ.Field(i))
			if ok {
				keys = append(keys, name)
			}
		}
		appConfigKeys = keys
	})
	return appConfigKeys
}

func encodeConfigEntries(cfg corelib.AppConfig) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("marshal config fields: %w", err)
	}
	if entries == nil {
		entries = map[string]json.RawMessage{}
	}
	// json.Marshal honors omitempty, so a bool that moves from true to false
	// disappears. ReplaceEntries then deletes the old row, and UnmarshalJSON
	// puts the default back. Keep an explicit false when that default is true.
	defaults, err := cachedConfigDefaults()
	if err != nil {
		return nil, err
	}
	forceExplicitBoolFalse(cfg, defaults, entries)
	return entries, nil
}

var (
	cachedDefaults     corelib.AppConfig
	cachedDefaultsErr  error
	cachedDefaultsOnce sync.Once
)

func cachedConfigDefaults() (corelib.AppConfig, error) {
	cachedDefaultsOnce.Do(func() {
		cachedDefaultsErr = json.Unmarshal([]byte(`{}`), &cachedDefaults)
	})
	return cachedDefaults, cachedDefaultsErr
}

func forceExplicitBoolFalse(cfg, defaults corelib.AppConfig, entries map[string]json.RawMessage) {
	current := reflect.ValueOf(cfg)
	seeded := reflect.ValueOf(defaults)
	typ := current.Type()
	for i := 0; i < typ.NumField(); i++ {
		name, omitempty, ok := configJSONName(typ.Field(i))
		if !ok || !omitempty {
			continue
		}
		if _, stored := entries[name]; stored {
			continue
		}
		field := current.Field(i)
		if field.Kind() != reflect.Bool || field.Bool() || !seeded.Field(i).Bool() {
			continue
		}
		entries[name] = json.RawMessage("false")
	}
}

func configJSONName(field reflect.StructField) (string, bool, bool) {
	if field.PkgPath != "" {
		return "", false, false
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	name, rest, _ := strings.Cut(tag, ",")
	if name == "" {
		name = field.Name
	}
	omitempty := false
	for rest != "" {
		var opt string
		opt, rest, _ = strings.Cut(rest, ",")
		if opt == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, true
}

func decodeConfigEntries(entries map[string]json.RawMessage) (corelib.AppConfig, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return corelib.AppConfig{}, fmt.Errorf("marshal stored config fields: %w", err)
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return corelib.AppConfig{}, fmt.Errorf("parse stored config: %w", err)
	}
	return cfg, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func ConfigDBPathForJSON(jsonPath string) string {
	return filepath.Join(filepath.Dir(jsonPath), configdb.DatabaseFileName)
}
