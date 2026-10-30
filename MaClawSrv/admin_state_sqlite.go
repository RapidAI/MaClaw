package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

// adminLegacySeal reports whether this save may record the import and remove
// the pre-migration file. A readable file that has not been copied stays, so
// the next open can still import it. A file this store cannot import may be sealed.
func adminLegacySeal(db *sql.DB, dataRoot, markerKey, jsonName string) bool {
	done, err := adminMarkerSet(db, markerKey)
	if err == nil && done {
		return true
	}
	return !fileutil.LegacyImportPending(filepath.Join(adminStateDir(dataRoot), jsonName), func(data []byte) bool {
		return adminLegacyAccept(jsonName, data)
	})
}

// adminLegacyAccept matches the document readLegacyAdmin unmarshals for jsonName.
// An unknown name stays pending so a save does not seal a document it cannot name.
func adminLegacyAccept(jsonName string, data []byte) bool {
	switch jsonName {
	case "admin_bootstrap.json":
		var dest adminBootstrapState
		return json.Unmarshal(data, &dest) == nil
	case "admin_users.json":
		var dest []adminUserRecord
		return json.Unmarshal(data, &dest) == nil
	case "admin_sessions.json":
		var dest []adminSessionRecord
		return json.Unmarshal(data, &dest) == nil
	case "admin_runtime_config.json":
		var dest adminRuntimeConfig
		return json.Unmarshal(data, &dest) == nil
	case "sandbox_profiles.json":
		var dest sandboxProfilesFile
		return json.Unmarshal(data, &dest) == nil
	case "admin_service_config_draft.json":
		var dest adminServiceConfigDraft
		return json.Unmarshal(data, &dest) == nil
	case adminDatabaseProfilesIdemFile:
		var dest map[string]adminDatabaseProfileIdemRecord
		return json.Unmarshal(data, &dest) == nil
	default:
		return true
	}
}

func adminStateDBPath(dataRoot string) string {
	return filepath.Join(adminStateDir(dataRoot), "admin_state.db")
}

func openAdminStateDB(dataRoot string) (*sql.DB, error) {
	dir := adminStateDir(dataRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", adminStateDBPath(dataRoot)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	if _, err := db.Exec(adminStateSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

const adminStateSchema = `
CREATE TABLE IF NOT EXISTS admin_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS admin_bootstrap (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  version INTEGER NOT NULL DEFAULT 0,
  initialized INTEGER NOT NULL DEFAULT 0,
  initialized_at TEXT NOT NULL DEFAULT '',
  initialized_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_users (
  position INTEGER NOT NULL,
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  locale TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  last_login_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_sessions (
  position INTEGER NOT NULL,
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  username TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  revoked_at TEXT NOT NULL DEFAULT '',
  remote_ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_runtime_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  sandbox_mode TEXT NOT NULL DEFAULT '',
  sandbox_strict INTEGER,
  updated_at TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_sandbox_profiles (
  profile_key TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  backend TEXT NOT NULL DEFAULT '',
  network TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_sandbox_profile_paths (
  profile_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  position INTEGER NOT NULL,
  path TEXT NOT NULL,
  PRIMARY KEY (profile_key, kind, position)
);
CREATE TABLE IF NOT EXISTS admin_sandbox_profile_limits (
  profile_key TEXT NOT NULL,
  limit_key TEXT NOT NULL,
  value_kind TEXT NOT NULL,
  value_text TEXT NOT NULL,
  PRIMARY KEY (profile_key, limit_key)
);
CREATE TABLE IF NOT EXISTS admin_service_draft (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  updated_at TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS admin_service_draft_values (
  key TEXT PRIMARY KEY,
  value_kind TEXT NOT NULL,
  value_text TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS admin_database_idem (
  scope_key TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL,
  result BLOB NOT NULL,
  created_at TEXT NOT NULL DEFAULT ''
);`

func adminMarkerSet(db *sql.DB, key string) (bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM admin_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func setAdminMarker(tx *sql.Tx, key string) error {
	_, err := tx.Exec(`INSERT INTO admin_meta(key, value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key)
	return err
}

func markAdminImported(db *sql.DB, key string) error {
	_, err := db.Exec(`INSERT INTO admin_meta(key, value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key)
	return err
}

func removeAdminJSON(dataRoot, name string) {
	_ = os.Remove(filepath.Join(adminStateDir(dataRoot), name))
}

// readLegacyAdmin reports whether a legacy file was present. A missing file
// is present=false. A corrupt file returns the read error.
func readLegacyAdmin(dataRoot, name string, dest any) (bool, error) {
	err := readAdminJSON(dataRoot, name, dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

type storedAdminUser struct {
	id   string
	user adminUserRecord
}

type storedAdminSession struct {
	id      string
	session adminSessionRecord
}

// storedAdminUsers assigns the primary key before duplicate removal. An empty
// id keeps the original index, so compaction cannot collide with a real "~N" id.
// A repeated id stays in its first slot and takes the later record.
func storedAdminUsers(users []adminUserRecord) []storedAdminUser {
	pos := make(map[string]int, len(users))
	out := make([]storedAdminUser, 0, len(users))
	for i, user := range users {
		id := user.ID
		if id == "" {
			id = "~" + strconv.Itoa(i)
		}
		row := storedAdminUser{id: id, user: user}
		if at, ok := pos[id]; ok {
			out[at] = row
			continue
		}
		pos[id] = len(out)
		out = append(out, row)
	}
	return out
}

// storedAdminSessions keeps the later copy of an id where that copy already
// stands. Prune walks from the end and drops the oldest rows, so putting the
// surviving session back in the first slot would drop the one just refreshed.
// An empty id is still the original index, and that key is not compacted.
func storedAdminSessions(sessions []adminSessionRecord) []storedAdminSession {
	last := make(map[string]int, len(sessions))
	ids := make([]string, len(sessions))
	for i, session := range sessions {
		id := session.ID
		if id == "" {
			id = "~" + strconv.Itoa(i)
		}
		ids[i] = id
		last[id] = i
	}
	out := make([]storedAdminSession, 0, len(last))
	for i, session := range sessions {
		if last[ids[i]] != i {
			continue
		}
		out = append(out, storedAdminSession{id: ids[i], session: session})
	}
	return out
}

func syntheticAdminRowID(id string) bool {
	if len(id) < 2 || id[0] != '~' {
		return false
	}
	for _, r := range id[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func formatAdminTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseAdminTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func encodeAdminAny(value any) (string, string) {
	switch typed := value.(type) {
	case nil:
		return "null", ""
	case string:
		return "string", typed
	case bool:
		if typed {
			return "bool", "1"
		}
		return "bool", "0"
	case float64:
		return "number", strconv.FormatFloat(typed, 'g', -1, 64)
	case float32:
		return "number", strconv.FormatFloat(float64(typed), 'g', -1, 64)
	case int:
		return "number", strconv.Itoa(typed)
	case int64:
		return "number", strconv.FormatInt(typed, 10)
	case json.Number:
		return "number", typed.String()
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return "string", ""
		}
		return "json", string(raw)
	}
}

func decodeAdminAny(kind, text string) any {
	switch kind {
	case "null":
		return nil
	case "string":
		return text
	case "bool":
		return text == "1" || text == "true"
	case "number":
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return float64(parsed)
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return text
		}
		return parsed
	case "json":
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return text
		}
		return value
	default:
		return text
	}
}

func loadAdminBootstrapState(dataRoot string) (adminBootstrapState, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return adminBootstrapState{}, err
	}
	defer db.Close()
	done, err := adminMarkerSet(db, "bootstrap")
	if err != nil {
		return adminBootstrapState{}, err
	}
	if !done {
		var legacy adminBootstrapState
		present, readErr := readLegacyAdmin(dataRoot, "admin_bootstrap.json", &legacy)
		if readErr != nil {
			state, err := readAdminBootstrap(db)
			if err != nil {
				return adminBootstrapState{}, err
			}
			if state == (adminBootstrapState{}) {
				return adminBootstrapState{}, readErr
			}
			return state, nil
		}
		if present {
			if err := writeAdminBootstrap(db, legacy, true); err != nil {
				return adminBootstrapState{}, err
			}
			removeAdminJSON(dataRoot, "admin_bootstrap.json")
			return legacy, nil
		}
		if err := markAdminImported(db, "bootstrap"); err != nil {
			return adminBootstrapState{}, err
		}
	}
	return readAdminBootstrap(db)
}

func saveAdminBootstrapState(dataRoot string, state adminBootstrapState) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "bootstrap", "admin_bootstrap.json")
	if err := writeAdminBootstrap(db, state, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "admin_bootstrap.json")
	}
	return nil
}

func writeAdminBootstrap(db *sql.DB, state adminBootstrapState, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	initialized := 0
	if state.Initialized {
		initialized = 1
	}
	if _, err := tx.Exec(`INSERT INTO admin_bootstrap(id, version, initialized, initialized_at, initialized_by) VALUES(1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET version = excluded.version, initialized = excluded.initialized, initialized_at = excluded.initialized_at, initialized_by = excluded.initialized_by`,
		state.Version, initialized, formatAdminTime(state.InitializedAt), state.InitializedBy); err != nil {
		_ = tx.Rollback()
		return err
	}
	if mark {
		if err := setAdminMarker(tx, "bootstrap"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminBootstrap(db *sql.DB) (adminBootstrapState, error) {
	var state adminBootstrapState
	var initialized int
	var initializedAt string
	err := db.QueryRow(`SELECT version, initialized, initialized_at, initialized_by FROM admin_bootstrap WHERE id = 1`).Scan(&state.Version, &initialized, &initializedAt, &state.InitializedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return adminBootstrapState{}, nil
	}
	if err != nil {
		return adminBootstrapState{}, err
	}
	state.Initialized = initialized != 0
	state.InitializedAt = parseAdminTime(initializedAt)
	return state, nil
}

func loadAdminUsers(dataRoot string) ([]adminUserRecord, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	users, imported, err := importAdminUsers(db, dataRoot)
	if err != nil {
		return nil, err
	}
	if imported {
		return users, nil
	}
	return readAdminUsers(db)
}

func importAdminUsers(db *sql.DB, dataRoot string) ([]adminUserRecord, bool, error) {
	done, err := adminMarkerSet(db, "users")
	if err != nil || done {
		return nil, false, err
	}
	var legacy []adminUserRecord
	present, readErr := readLegacyAdmin(dataRoot, "admin_users.json", &legacy)
	if readErr != nil {
		users, err := readAdminUsers(db)
		if err != nil {
			return nil, false, err
		}
		if len(users) == 0 {
			return nil, false, readErr
		}
		return users, true, nil
	}
	if !present {
		return nil, false, markAdminImported(db, "users")
	}
	if err := writeAdminUsers(db, legacy, true); err != nil {
		return nil, false, err
	}
	stored, err := readAdminUsers(db)
	if err != nil {
		return nil, false, err
	}
	removeAdminJSON(dataRoot, "admin_users.json")
	return stored, true, nil
}

func saveAdminUsers(dataRoot string, users []adminUserRecord) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "users", "admin_users.json")
	if err := writeAdminUsers(db, users, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "admin_users.json")
	}
	return nil
}

func writeAdminUsers(db *sql.DB, users []adminUserRecord, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_users`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO admin_users(position, id, username, display_name, role, status, locale, password_hash, created_at, updated_at, last_login_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	// The stored id is fixed before compaction. An empty id uses its original
	// index, and a repeated id keeps that slot with the later record. The old
	// JSON list could contain both; failing the primary key here dropped login.
	for i, row := range storedAdminUsers(users) {
		user := row.user
		if _, err := stmt.Exec(i, row.id, user.Username, user.DisplayName, user.Role, user.Status, user.Locale, user.PasswordHash, formatAdminTime(user.CreatedAt), formatAdminTime(user.UpdatedAt), formatAdminTime(user.LastLoginAt)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if err := setAdminMarker(tx, "users"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminUsers(db *sql.DB) ([]adminUserRecord, error) {
	rows, err := db.Query(`SELECT id, username, display_name, role, status, locale, password_hash, created_at, updated_at, last_login_at FROM admin_users ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []adminUserRecord{}
	for rows.Next() {
		var user adminUserRecord
		var created, updated, last string
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Status, &user.Locale, &user.PasswordHash, &created, &updated, &last); err != nil {
			return nil, err
		}
		if syntheticAdminRowID(user.ID) {
			user.ID = ""
		}
		user.CreatedAt = parseAdminTime(created)
		user.UpdatedAt = parseAdminTime(updated)
		user.LastLoginAt = parseAdminTime(last)
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadAdminSessions(dataRoot string) ([]adminSessionRecord, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	sessions, imported, err := importAdminSessions(db, dataRoot)
	if err != nil {
		return nil, err
	}
	if imported {
		return sessions, nil
	}
	return readAdminSessions(db)
}

func importAdminSessions(db *sql.DB, dataRoot string) ([]adminSessionRecord, bool, error) {
	done, err := adminMarkerSet(db, "sessions")
	if err != nil || done {
		return nil, false, err
	}
	var legacy []adminSessionRecord
	present, readErr := readLegacyAdmin(dataRoot, "admin_sessions.json", &legacy)
	if readErr != nil {
		sessions, err := readAdminSessions(db)
		if err != nil {
			return nil, false, err
		}
		if len(sessions) == 0 {
			return nil, false, readErr
		}
		return sessions, true, nil
	}
	if !present {
		return nil, false, markAdminImported(db, "sessions")
	}
	if err := writeAdminSessions(db, legacy, true); err != nil {
		return nil, false, err
	}
	stored, err := readAdminSessions(db)
	if err != nil {
		return nil, false, err
	}
	removeAdminJSON(dataRoot, "admin_sessions.json")
	return stored, true, nil
}

func saveAdminSessions(dataRoot string, sessions []adminSessionRecord) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "sessions", "admin_sessions.json")
	if err := writeAdminSessions(db, sessions, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "admin_sessions.json")
	}
	return nil
}

func writeAdminSessions(db *sql.DB, sessions []adminSessionRecord, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_sessions`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO admin_sessions(position, id, token_hash, user_id, username, role, created_at, expires_at, revoked_at, remote_ip, user_agent) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for i, row := range storedAdminSessions(sessions) {
		session := row.session
		if _, err := stmt.Exec(i, row.id, session.TokenHash, session.UserID, session.Username, session.Role, formatAdminTime(session.CreatedAt), formatAdminTime(session.ExpiresAt), formatAdminTime(session.RevokedAt), session.RemoteIP, session.UserAgent); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if err := setAdminMarker(tx, "sessions"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminSessions(db *sql.DB) ([]adminSessionRecord, error) {
	rows, err := db.Query(`SELECT id, token_hash, user_id, username, role, created_at, expires_at, revoked_at, remote_ip, user_agent FROM admin_sessions ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []adminSessionRecord{}
	for rows.Next() {
		var session adminSessionRecord
		var created, expires, revoked string
		if err := rows.Scan(&session.ID, &session.TokenHash, &session.UserID, &session.Username, &session.Role, &created, &expires, &revoked, &session.RemoteIP, &session.UserAgent); err != nil {
			return nil, err
		}
		if syntheticAdminRowID(session.ID) {
			session.ID = ""
		}
		session.CreatedAt = parseAdminTime(created)
		session.ExpiresAt = parseAdminTime(expires)
		session.RevokedAt = parseAdminTime(revoked)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func loadAdminRuntimeConfig(dataRoot string) (adminRuntimeConfig, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return adminRuntimeConfig{}, err
	}
	defer db.Close()
	done, err := adminMarkerSet(db, "runtime_config")
	if err != nil {
		return adminRuntimeConfig{}, err
	}
	if !done {
		var legacy adminRuntimeConfig
		present, readErr := readLegacyAdmin(dataRoot, "admin_runtime_config.json", &legacy)
		if readErr != nil {
			cfg, err := readAdminRuntime(db)
			if err != nil {
				return adminRuntimeConfig{}, err
			}
			if cfg == (adminRuntimeConfig{}) {
				return adminRuntimeConfig{}, readErr
			}
			return cfg, nil
		}
		if present {
			if err := writeAdminRuntime(db, legacy, true); err != nil {
				return adminRuntimeConfig{}, err
			}
			removeAdminJSON(dataRoot, "admin_runtime_config.json")
			return legacy, nil
		}
		if err := markAdminImported(db, "runtime_config"); err != nil {
			return adminRuntimeConfig{}, err
		}
	}
	return readAdminRuntime(db)
}

func saveAdminRuntimeConfig(dataRoot string, cfg adminRuntimeConfig) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "runtime_config", "admin_runtime_config.json")
	if err := writeAdminRuntime(db, cfg, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "admin_runtime_config.json")
	}
	return nil
}

func writeAdminRuntime(db *sql.DB, cfg adminRuntimeConfig, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	var strict any
	if cfg.SandboxStrict != nil {
		if *cfg.SandboxStrict {
			strict = 1
		} else {
			strict = 0
		}
	}
	if _, err := tx.Exec(`INSERT INTO admin_runtime_config(id, sandbox_mode, sandbox_strict, updated_at, updated_by, reason) VALUES(1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET sandbox_mode = excluded.sandbox_mode, sandbox_strict = excluded.sandbox_strict, updated_at = excluded.updated_at, updated_by = excluded.updated_by, reason = excluded.reason`,
		cfg.SandboxMode, strict, formatAdminTime(cfg.UpdatedAt), cfg.UpdatedBy, cfg.Reason); err != nil {
		_ = tx.Rollback()
		return err
	}
	if mark {
		if err := setAdminMarker(tx, "runtime_config"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminRuntime(db *sql.DB) (adminRuntimeConfig, error) {
	var cfg adminRuntimeConfig
	var strict sql.NullInt64
	var updated string
	err := db.QueryRow(`SELECT sandbox_mode, sandbox_strict, updated_at, updated_by, reason FROM admin_runtime_config WHERE id = 1`).Scan(&cfg.SandboxMode, &strict, &updated, &cfg.UpdatedBy, &cfg.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return adminRuntimeConfig{}, nil
	}
	if err != nil {
		return adminRuntimeConfig{}, err
	}
	if strict.Valid {
		value := strict.Int64 != 0
		cfg.SandboxStrict = &value
	}
	cfg.UpdatedAt = parseAdminTime(updated)
	return cfg, nil
}

func loadSandboxProfiles(dataRoot string) (sandboxProfilesFile, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return sandboxProfilesFile{}, err
	}
	defer db.Close()
	done, err := adminMarkerSet(db, "sandbox_profiles")
	if err != nil {
		return sandboxProfilesFile{}, err
	}
	if !done {
		var legacy sandboxProfilesFile
		present, readErr := readLegacyAdmin(dataRoot, "sandbox_profiles.json", &legacy)
		if readErr != nil {
			profiles, err := readSandboxProfiles(db)
			if err != nil {
				return sandboxProfilesFile{}, err
			}
			if len(profiles.Profiles) == 0 {
				return sandboxProfilesFile{}, readErr
			}
			return profiles, nil
		}
		if present {
			if legacy.Profiles == nil {
				legacy.Profiles = map[string]sandboxProfile{}
			}
			if err := writeSandboxProfiles(db, legacy, true); err != nil {
				return sandboxProfilesFile{}, err
			}
			removeAdminJSON(dataRoot, "sandbox_profiles.json")
			return legacy, nil
		}
		if err := markAdminImported(db, "sandbox_profiles"); err != nil {
			return sandboxProfilesFile{}, err
		}
	}
	return readSandboxProfiles(db)
}

func saveSandboxProfiles(dataRoot string, profiles sandboxProfilesFile) error {
	if profiles.Profiles == nil {
		profiles.Profiles = map[string]sandboxProfile{}
	}
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "sandbox_profiles", "sandbox_profiles.json")
	if err := writeSandboxProfiles(db, profiles, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "sandbox_profiles.json")
	}
	return nil
}

func writeSandboxProfiles(db *sql.DB, profiles sandboxProfilesFile, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_sandbox_profile_limits`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_sandbox_profile_paths`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_sandbox_profiles`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for key, profile := range profiles.Profiles {
		if _, err := tx.Exec(`INSERT INTO admin_sandbox_profiles(profile_key, name, backend, network, updated_at, updated_by) VALUES(?,?,?,?,?,?)`,
			key, profile.Name, profile.Backend, profile.Network, formatAdminTime(profile.UpdatedAt), profile.UpdatedBy); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := insertAdminPaths(tx, key, "readonly", profile.ReadOnlyPaths); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := insertAdminPaths(tx, key, "writable", profile.WritablePaths); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := insertAdminPaths(tx, key, "env", profile.EnvAllowlist); err != nil {
			_ = tx.Rollback()
			return err
		}
		for limitKey, value := range profile.Limits {
			kind, text := encodeAdminAny(value)
			if _, err := tx.Exec(`INSERT INTO admin_sandbox_profile_limits(profile_key, limit_key, value_kind, value_text) VALUES(?,?,?,?)`, key, limitKey, kind, text); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if mark {
		if err := setAdminMarker(tx, "sandbox_profiles"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func insertAdminPaths(tx *sql.Tx, profileKey, kind string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT INTO admin_sandbox_profile_paths(profile_key, kind, position, path) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, path := range paths {
		if _, err := stmt.Exec(profileKey, kind, i, path); err != nil {
			return err
		}
	}
	return nil
}

func readSandboxProfiles(db *sql.DB) (sandboxProfilesFile, error) {
	rows, err := db.Query(`SELECT profile_key, name, backend, network, updated_at, updated_by FROM admin_sandbox_profiles ORDER BY profile_key`)
	if err != nil {
		return sandboxProfilesFile{}, err
	}
	out := sandboxProfilesFile{Profiles: map[string]sandboxProfile{}}
	for rows.Next() {
		var key string
		var profile sandboxProfile
		var updated string
		if err := rows.Scan(&key, &profile.Name, &profile.Backend, &profile.Network, &updated, &profile.UpdatedBy); err != nil {
			_ = rows.Close()
			return sandboxProfilesFile{}, err
		}
		profile.UpdatedAt = parseAdminTime(updated)
		profile.Limits = map[string]any{}
		out.Profiles[key] = profile
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return sandboxProfilesFile{}, err
	}
	_ = rows.Close()
	pathRows, err := db.Query(`SELECT profile_key, kind, path FROM admin_sandbox_profile_paths ORDER BY profile_key, kind, position`)
	if err != nil {
		return sandboxProfilesFile{}, err
	}
	for pathRows.Next() {
		var key, kind, path string
		if err := pathRows.Scan(&key, &kind, &path); err != nil {
			_ = pathRows.Close()
			return sandboxProfilesFile{}, err
		}
		profile := out.Profiles[key]
		switch kind {
		case "readonly":
			profile.ReadOnlyPaths = append(profile.ReadOnlyPaths, path)
		case "writable":
			profile.WritablePaths = append(profile.WritablePaths, path)
		case "env":
			profile.EnvAllowlist = append(profile.EnvAllowlist, path)
		}
		out.Profiles[key] = profile
	}
	if err := pathRows.Err(); err != nil {
		_ = pathRows.Close()
		return sandboxProfilesFile{}, err
	}
	_ = pathRows.Close()
	limitRows, err := db.Query(`SELECT profile_key, limit_key, value_kind, value_text FROM admin_sandbox_profile_limits`)
	if err != nil {
		return sandboxProfilesFile{}, err
	}
	defer limitRows.Close()
	for limitRows.Next() {
		var key, limitKey, kind, text string
		if err := limitRows.Scan(&key, &limitKey, &kind, &text); err != nil {
			return sandboxProfilesFile{}, err
		}
		profile := out.Profiles[key]
		if profile.Limits == nil {
			profile.Limits = map[string]any{}
		}
		profile.Limits[limitKey] = decodeAdminAny(kind, text)
		out.Profiles[key] = profile
	}
	return out, limitRows.Err()
}

func loadAdminServiceConfigDraft(dataRoot string) (adminServiceConfigDraft, error) {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return adminServiceConfigDraft{}, err
	}
	defer db.Close()
	done, err := adminMarkerSet(db, "service_draft")
	if err != nil {
		return adminServiceConfigDraft{}, err
	}
	if !done {
		var legacy adminServiceConfigDraft
		present, readErr := readLegacyAdmin(dataRoot, "admin_service_config_draft.json", &legacy)
		if readErr != nil {
			draft, err := readAdminServiceDraft(db)
			if err != nil {
				return adminServiceConfigDraft{}, err
			}
			if len(draft.Values) == 0 && draft.UpdatedBy == "" && draft.Reason == "" && draft.UpdatedAt.IsZero() {
				return adminServiceConfigDraft{}, readErr
			}
			return draft, nil
		}
		if present {
			if legacy.Values == nil {
				legacy.Values = map[string]any{}
			}
			if err := writeAdminServiceDraft(db, legacy, true); err != nil {
				return adminServiceConfigDraft{}, err
			}
			removeAdminJSON(dataRoot, "admin_service_config_draft.json")
			return legacy, nil
		}
		if err := markAdminImported(db, "service_draft"); err != nil {
			return adminServiceConfigDraft{}, err
		}
	}
	draft, err := readAdminServiceDraft(db)
	if err != nil {
		return adminServiceConfigDraft{}, err
	}
	if draft.Values == nil {
		draft.Values = map[string]any{}
	}
	return draft, nil
}

func saveAdminServiceConfigDraft(dataRoot string, draft adminServiceConfigDraft) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "service_draft", "admin_service_config_draft.json")
	if err := writeAdminServiceDraft(db, draft, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, "admin_service_config_draft.json")
	}
	return nil
}

func clearAdminServiceConfigDraftStore(dataRoot string) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_service_draft_values`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_service_draft`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := setAdminMarker(tx, "service_draft"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	removeAdminJSON(dataRoot, "admin_service_config_draft.json")
	return nil
}

func writeAdminServiceDraft(db *sql.DB, draft adminServiceConfigDraft, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_service_draft_values`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`INSERT INTO admin_service_draft(id, updated_at, updated_by, reason) VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET updated_at = excluded.updated_at, updated_by = excluded.updated_by, reason = excluded.reason`,
		formatAdminTime(draft.UpdatedAt), draft.UpdatedBy, draft.Reason); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO admin_service_draft_values(key, value_kind, value_text) VALUES(?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for key, value := range draft.Values {
		kind, text := encodeAdminAny(value)
		if _, err := stmt.Exec(key, kind, text); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if err := setAdminMarker(tx, "service_draft"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminServiceDraft(db *sql.DB) (adminServiceConfigDraft, error) {
	var draft adminServiceConfigDraft
	var updated string
	err := db.QueryRow(`SELECT updated_at, updated_by, reason FROM admin_service_draft WHERE id = 1`).Scan(&updated, &draft.UpdatedBy, &draft.Reason)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return adminServiceConfigDraft{}, err
	}
	draft.UpdatedAt = parseAdminTime(updated)
	draft.Values = map[string]any{}
	rows, err := db.Query(`SELECT key, value_kind, value_text FROM admin_service_draft_values`)
	if err != nil {
		return adminServiceConfigDraft{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, kind, text string
		if err := rows.Scan(&key, &kind, &text); err != nil {
			return adminServiceConfigDraft{}, err
		}
		draft.Values[key] = decodeAdminAny(kind, text)
	}
	return draft, rows.Err()
}

func loadAdminDatabaseIdemRecords(dataRoot string) map[string]adminDatabaseProfileIdemRecord {
	records := map[string]adminDatabaseProfileIdemRecord{}
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return records
	}
	defer db.Close()
	done, err := adminMarkerSet(db, "database_idem")
	if err != nil {
		return records
	}
	if !done {
		legacy := map[string]adminDatabaseProfileIdemRecord{}
		present, readErr := readLegacyAdmin(dataRoot, adminDatabaseProfilesIdemFile, &legacy)
		if readErr == nil && present {
			if err := writeAdminIdem(db, legacy, true); err == nil {
				removeAdminJSON(dataRoot, adminDatabaseProfilesIdemFile)
				return legacy
			}
		}
		if readErr == nil && !present {
			_ = markAdminImported(db, "database_idem")
		}
	}
	loaded, err := readAdminIdem(db)
	if err != nil {
		return records
	}
	return loaded
}

func saveAdminDatabaseIdemRecords(dataRoot string, records map[string]adminDatabaseProfileIdemRecord) error {
	db, err := openAdminStateDB(dataRoot)
	if err != nil {
		return err
	}
	defer db.Close()
	seal := adminLegacySeal(db, dataRoot, "database_idem", adminDatabaseProfilesIdemFile)
	if err := writeAdminIdem(db, records, seal); err != nil {
		return err
	}
	if seal {
		removeAdminJSON(dataRoot, adminDatabaseProfilesIdemFile)
	}
	return nil
}

func writeAdminIdem(db *sql.DB, records map[string]adminDatabaseProfileIdemRecord, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM admin_database_idem`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO admin_database_idem(scope_key, fingerprint, status, result, created_at) VALUES(?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for key, record := range records {
		result := []byte(record.Result)
		if result == nil {
			result = []byte{}
		}
		if _, err := stmt.Exec(key, record.Fingerprint, record.Status, result, formatAdminTime(record.CreatedAt)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if err := setAdminMarker(tx, "database_idem"); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readAdminIdem(db *sql.DB) (map[string]adminDatabaseProfileIdemRecord, error) {
	rows, err := db.Query(`SELECT scope_key, fingerprint, status, result, created_at FROM admin_database_idem`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]adminDatabaseProfileIdemRecord{}
	for rows.Next() {
		var key, fingerprint, created string
		var status int
		var result []byte
		if err := rows.Scan(&key, &fingerprint, &status, &result, &created); err != nil {
			return nil, err
		}
		out[key] = adminDatabaseProfileIdemRecord{Fingerprint: fingerprint, Status: status, Result: append(json.RawMessage(nil), result...), CreatedAt: parseAdminTime(created)}
	}
	return out, rows.Err()
}
