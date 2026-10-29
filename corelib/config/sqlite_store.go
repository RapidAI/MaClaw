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
		if cfg, jsonErr := s.loadLegacyJSON(); jsonErr == nil && s.legacySourceExists() {
			s.legacyFallback.Store(true)
			log.Printf("[config] SQLite load failed; using legacy JSON only: %v", err)
			return cfg, nil
		}
		return corelib.AppConfig{}, err
	}
	if !s.legacySourceExists() {
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
	if len(entries) == 0 {
		// Another process may have claimed the JSON (renamed it away) without
		// having committed its import yet. Decoding an empty entry set would
		// surface defaults over the real legacy config, so keep the legacy
		// snapshot; a later load retries the import once rows land.
		s.legacyFallback.Store(true)
		log.Printf("[config] SQLite migration produced no rows yet; using legacy JSON only")
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
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// The JSON was already moved aside (crash after backup, or a racing
		// process claimed it). Recover the import source from the backup.
		data, err = os.ReadFile(s.backupPath())
		if err != nil {
			return err
		}
	}
	entries, err := decodeLegacyEntries(data)
	if err != nil {
		return err
	}
	if err := configdb.InsertEntriesAbsent(s.dbPath, entries); err != nil {
		return err
	}
	// A concurrent full save must not be clobbered, and an empty result here
	// means the rows have not landed yet (claim race); both are surfaced as an
	// error so the caller falls back to the legacy snapshot instead of
	// decoding defaults over real configuration.
	landed, err := configdb.LoadEntries(s.dbPath)
	if err != nil {
		return err
	}
	if len(landed) == 0 && len(entries) > 0 {
		return errors.New("config migration rows not visible yet")
	}
	if fileExists(s.jsonPath) {
		s.backupLegacyJSON()
	}
	return nil
}

func (s *SQLiteStore) backupPath() string {
	return s.jsonPath + ConfigMigrationBackupSuffix
}

// legacySourceExists reports whether any legacy JSON source (live file or
// migration backup) is present, so fallback/recovery paths can distinguish
// "no legacy data" from "legacy data moved aside".
func (s *SQLiteStore) legacySourceExists() bool {
	return fileExists(s.jsonPath) || fileExists(s.backupPath())
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
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// The live file may have been moved aside by a (racing or crashed)
		// migration; the backup holds the same document and is a valid source.
		data, err = os.ReadFile(s.backupPath())
	}
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

// obsoleteLegacyConfigKeys are residues that must not survive migration. The
// desktop-only hardware gateway token predates Hub's device pairing protocol;
// the GUI scrubs it from the legacy JSON on load, and the backup rename would
// otherwise move that scrub target out from under it, leaking the token into
// the database where nothing removes it.
var obsoleteLegacyConfigKeys = map[string]bool{
	"hardware_gateway_token": true,
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
		if key == "" || obsoleteLegacyConfigKeys[key] || !json.Valid(value) {
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
