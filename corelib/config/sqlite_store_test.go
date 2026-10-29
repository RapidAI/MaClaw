package config

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configdb"
)

func TestSQLiteStoreMigratesEveryFieldAndIgnoresBackupAfterward(t *testing.T) {
	dataDir := t.TempDir()
	legacy := corelib.AppConfigDefaults()
	legacy.Language = "en"
	legacy.RemoteEmail = "person@example.com"
	legacy.MaclawLLMUrl = "https://llm.example/v1"
	legacy.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name: "provider", URL: "https://provider.example/v1", Key: "secret", Model: "model",
	}}
	legacy.NLSkills = []corelib.NLSkillEntry{{Name: "skill-one", Description: "preserved"}}
	legacy.Projects = []corelib.ProjectConfig{{Id: "project-one", Name: "Project One", Path: "D:/project"}}
	legacy.ModelRoutes = map[string]corelib.ModelRouteConfig{"reasoning": {Model: "reasoning-model"}}
	legacy.NetworkAllowlist = []string{"example.com"}
	legacyData, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(jsonPath, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	var want corelib.AppConfig
	if err := json.Unmarshal(legacyData, &want); err != nil {
		t.Fatal(err)
	}

	store := NewSQLiteStore(dataDir)
	got, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("migrate and load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("migrated config mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("legacy JSON still exists after migration: %v", err)
	}
	backupPath := jsonPath + ConfigMigrationBackupSuffix
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("migration backup: %v", err)
	}
	assertConfigSchema(t, store.DBPath())

	if err := os.Remove(backupPath); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewSQLiteStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("load without legacy backup: %v", err)
	}
	if !reflect.DeepEqual(restarted, want) {
		t.Fatalf("restart without JSON changed config\ngot:  %#v\nwant: %#v", restarted, want)
	}
}

func TestSQLiteStoreMissingFieldsUseJSONDefaults(t *testing.T) {
	dataDir := t.TempDir()
	legacy := []byte(`{"language":"en","vector_search_enabled":false}`)
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	var want corelib.AppConfig
	if err := json.Unmarshal(legacy, &want); err != nil {
		t.Fatal(err)
	}
	store := NewSQLiteStore(dataDir)
	got, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partial config defaults mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
	if got.Language != "en" || got.VectorSearchEnabled || !got.ASREnabled || !got.TTSEnabled {
		t.Fatalf("explicit/default values not preserved: %#v", got)
	}

	db, err := sql.Open("sqlite", store.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM config_entries WHERE key = 'asr_enabled'`); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewSQLiteStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.ASREnabled {
		t.Fatal("missing newly introduced field did not receive the JSON default")
	}
}

func TestSQLiteStoreSaveRoundTripAndExternalUpdate(t *testing.T) {
	store := NewSQLiteStore(t.TempDir())
	cfg := corelib.AppConfigDefaults()
	cfg.RemoteEmail = "before@example.com"
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "before"}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg.RemoteEmail = "after@example.com"
	cfg.NLSkills[0].Name = "after"
	cfg.ASREnabled = false
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, err := NewSQLiteStore(filepath.Dir(store.DBPath())).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.RemoteEmail != cfg.RemoteEmail || len(got.NLSkills) != 1 || got.NLSkills[0].Name != "after" {
		t.Fatalf("database and saved config differ: %#v", got)
	}
	if got.ASREnabled {
		t.Fatal("false value did not overwrite the previously persisted true value")
	}

	db, err := sql.Open("sqlite", store.DBPath()+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE config_entries SET value_json = '"external@example.com"', updated_at = updated_at + 1 WHERE key = 'remote_email'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	external, err := NewSQLiteStore(filepath.Dir(store.DBPath())).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if external.RemoteEmail != "external@example.com" {
		t.Fatalf("external update = %q", external.RemoteEmail)
	}
}

func TestSQLiteStoreKeepsDirectReadKeysAcrossSave(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"language":"en","permission":{"rules":[{"tool":"bash","effect":"deny"}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewSQLiteStore(dir)
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" {
		t.Fatalf("language = %q", cfg.Language)
	}
	raw, ok, err := configdb.ReadRaw(dir, "permission")
	if err != nil || !ok || !bytes.Contains(raw, []byte(`"bash"`)) {
		t.Fatalf("permission after migration = %s ok=%v err=%v", raw, ok, err)
	}
	cfg.Language = "zh"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, ok, err = configdb.ReadRaw(dir, "permission")
	if err != nil || !ok || !bytes.Contains(raw, []byte(`"bash"`)) {
		t.Fatalf("permission after save = %s ok=%v err=%v", raw, ok, err)
	}
}

func TestSQLiteStoreUnchangedSaveKeepsRowTimestamps(t *testing.T) {
	store := NewSQLiteStore(t.TempDir())
	cfg := corelib.AppConfig{Language: "en", RemoteEmail: "person@example.com"}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", store.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var before int64
	if err := db.QueryRow(`SELECT updated_at FROM config_entries WHERE key='language'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := db.QueryRow(`SELECT updated_at FROM config_entries WHERE key='language'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("unchanged language row rewritten: before=%d after=%d", before, after)
	}
	cfg.RemoteEmail = "next@example.com"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	var languageAfter int64
	var email string
	if err := db.QueryRow(`SELECT updated_at FROM config_entries WHERE key='language'`).Scan(&languageAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT value_json FROM config_entries WHERE key='remote_email'`).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if languageAfter != before || email != `"next@example.com"` {
		t.Fatalf("partial save language=%d email=%s", languageAfter, email)
	}
}

func TestSQLiteStoreExplicitFalseSurvivesOmitEmptyDefault(t *testing.T) {
	store := NewSQLiteStore(t.TempDir())
	cfg := corelib.AppConfigDefaults()
	cfg.PreferBetaChannel = true
	cfg.PetEnabled = true
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.PreferBetaChannel = false
	cfg.PetEnabled = false
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := NewSQLiteStore(filepath.Dir(store.DBPath())).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.PreferBetaChannel || got.PetEnabled {
		t.Fatalf("explicit false was restored to default: beta=%v pet=%v", got.PreferBetaChannel, got.PetEnabled)
	}
}

func TestSQLiteStoreSaveMatchesLegacyJSONDefaults(t *testing.T) {
	store := NewSQLiteStore(t.TempDir())
	input := corelib.AppConfig{Language: "en"}
	legacyJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var want corelib.AppConfig
	if err := json.Unmarshal(legacyJSON, &want); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(input); err != nil {
		t.Fatal(err)
	}
	got, err := NewSQLiteStore(filepath.Dir(store.DBPath())).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SQLite save changed legacy JSON default semantics\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestSQLiteStoreMigrationFailureFallsBackToJSONOnly(t *testing.T) {
	dataDir := t.TempDir()
	jsonPath := filepath.Join(dataDir, "config.json")
	legacy := []byte(`{"language":"en","remote_email":"fallback@example.com"}`)
	if err := os.WriteFile(jsonPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dataDir, "config.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewSQLiteStore(dataDir)
	got, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("fallback must not prevent startup: %v", err)
	}
	if got.RemoteEmail != "fallback@example.com" || !store.UsingLegacyJSON() {
		t.Fatalf("fallback config/store state = %#v, %v", got, store.UsingLegacyJSON())
	}
	got.RemoteEmail = "fallback-updated@example.com"
	if err := store.SaveConfig(got); err != nil {
		t.Fatalf("save through JSON fallback: %v", err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted corelib.AppConfig
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.RemoteEmail != got.RemoteEmail {
		t.Fatalf("fallback save = %q, want %q", persisted.RemoteEmail, got.RemoteEmail)
	}
	if _, err := os.Stat(jsonPath + ConfigMigrationBackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("failed migration unexpectedly created a backup: %v", err)
	}
}

func assertConfigSchema(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(config_entries)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"key": "TEXT", "value_json": "TEXT", "updated_at": "INTEGER", "value_hash": "TEXT"}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("schema columns = %#v, want %#v", columns, want)
	}
	var updatedAt int64
	if err := db.QueryRow(`SELECT updated_at FROM config_entries LIMIT 1`).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt <= 0 {
		t.Fatalf("updated_at = %d", updatedAt)
	}
}
