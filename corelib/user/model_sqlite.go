package user

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type userModelStore struct {
	db   *sql.DB
	path string
}

func userModelDBPath(jsonPath string) string {
	ext := filepath.Ext(jsonPath)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(jsonPath, ext) + ".db"
	}
	return jsonPath + ".db"
}

func openUserModelStore(jsonPath string) (*userModelStore, error) {
	dbPath := userModelDBPath(jsonPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create user model directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open user model database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS user_profile_dimensions (
    name TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    user_confirmed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS user_profile_evidence (
    dimension_name TEXT NOT NULL,
    position INTEGER NOT NULL,
    observation TEXT NOT NULL,
    timestamp_unix_nano INTEGER NOT NULL,
    source TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(dimension_name, position)
);
CREATE INDEX IF NOT EXISTS idx_user_profile_evidence_time ON user_profile_evidence(timestamp_unix_nano);
CREATE TABLE IF NOT EXISTS user_profile_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create user model schema: %w", err)
	}
	store := &userModelStore{db: db, path: dbPath}
	if err := importLegacyUserProfile(db, jsonPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func importLegacyUserProfile(db *sql.DB, jsonPath string) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_profile_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return fmt.Errorf("read user model import marker: %w", err)
	}
	if imported > 0 {
		return nil
	}
	profile := Profile{}
	data, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read user model file: %w", err)
	}
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &profile)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin user model import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeUserProfile(tx, profile); err != nil {
		return fmt.Errorf("import user model: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO user_profile_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return fmt.Errorf("mark user model import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user model import: %w", err)
	}
	return nil
}

func loadUserProfile(db *sql.DB) (Profile, error) {
	profile := Profile{}
	rows, err := db.Query(`SELECT name, value, confidence, user_confirmed FROM user_profile_dimensions`)
	if err != nil {
		return profile, fmt.Errorf("load user dimensions: %w", err)
	}
	for rows.Next() {
		var name string
		var dimension Dimension
		var confirmed int
		if err := rows.Scan(&name, &dimension.Value, &dimension.Confidence, &confirmed); err != nil {
			_ = rows.Close()
			return profile, fmt.Errorf("scan user dimension: %w", err)
		}
		dimension.UserConfirmed = confirmed != 0
		if target := profileDimension(&profile, name); target != nil {
			*target = dimension
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return profile, fmt.Errorf("read user dimensions: %w", err)
	}
	_ = rows.Close()
	evidenceRows, err := db.Query(`SELECT dimension_name, observation, timestamp_unix_nano, source
		FROM user_profile_evidence ORDER BY dimension_name, position`)
	if err != nil {
		return profile, fmt.Errorf("load user evidence: %w", err)
	}
	defer evidenceRows.Close()
	for evidenceRows.Next() {
		var name, source string
		var evidence Evidence
		var timestamp int64
		if err := evidenceRows.Scan(&name, &evidence.Observation, &timestamp, &source); err != nil {
			return profile, fmt.Errorf("scan user evidence: %w", err)
		}
		if timestamp != 0 {
			evidence.Timestamp = time.Unix(0, timestamp)
		}
		evidence.Source = source
		if dimension := profileDimension(&profile, name); dimension != nil {
			dimension.Evidence = append(dimension.Evidence, evidence)
		}
	}
	if err := evidenceRows.Err(); err != nil {
		return profile, fmt.Errorf("read user evidence: %w", err)
	}
	return profile, nil
}

func saveUserProfile(db *sql.DB, profile Profile) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin user model save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeUserProfile(tx, profile); err != nil {
		return fmt.Errorf("write user model: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user model: %w", err)
	}
	return nil
}

func writeUserProfile(tx *sql.Tx, profile Profile) error {
	if _, err := tx.Exec(`DELETE FROM user_profile_evidence`); err != nil {
		return err
	}
	for _, name := range []string{"communication_style", "technical_level", "preferred_languages", "domain_expertise", "work_patterns", "tool_preferences"} {
		dimension := profileDimension(&profile, name)
		confirmed := 0
		if dimension.UserConfirmed {
			confirmed = 1
		}
		if _, err := tx.Exec(`INSERT INTO user_profile_dimensions(name, value, confidence, user_confirmed)
			VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value,
			confidence=excluded.confidence, user_confirmed=excluded.user_confirmed`,
			name, dimension.Value, dimension.Confidence, confirmed); err != nil {
			return err
		}
		for position, evidence := range dimension.Evidence {
			var timestamp int64
			if !evidence.Timestamp.IsZero() {
				timestamp = evidence.Timestamp.UnixNano()
			}
			if _, err := tx.Exec(`INSERT INTO user_profile_evidence(
				dimension_name, position, observation, timestamp_unix_nano, source) VALUES(?,?,?,?,?)`,
				name, position, evidence.Observation, timestamp, evidence.Source); err != nil {
				return err
			}
		}
	}
	return nil
}

func profileDimension(profile *Profile, name string) *Dimension {
	if profile == nil {
		return nil
	}
	switch name {
	case "communication_style":
		return &profile.CommunicationStyle
	case "technical_level":
		return &profile.TechnicalLevel
	case "preferred_languages":
		return &profile.PreferredLanguages
	case "domain_expertise":
		return &profile.DomainExpertise
	case "work_patterns":
		return &profile.WorkPatterns
	case "tool_preferences":
		return &profile.ToolPreferences
	default:
		return nil
	}
}
