package guiapp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Badge looks used to live in the WebView localStorage key
// maclaw.desktopBotAppearance.v1. That origin is shared with chat transcripts,
// and a full quota made creating a bot fail on setItem. Hub still stores the
// look. This SQLite file is the on-disk copy for an older Hub that does not
// echo color and shape, and for the one-time move of looks saved before Hub
// kept them.

const maxDesktopBotLooksJSONBytes = 1 << 20

var (
	desktopBotLooksMu    sync.Mutex
	desktopBotColorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	desktopBotShapeAllow = map[string]bool{
		"blob": true, "cloud": true, "leaf": true, "square": true, "pill": true, "circle": true,
	}
)

// DesktopBotLook is one bot's badge color and shape.
type DesktopBotLook struct {
	Color string `json:"color"`
	Shape string `json:"shape"`
}

// DesktopBotLooksFile is one account's looks plus the bots whose current look
// was already copied to Hub. A later different look is removed with
// UnmarkDesktopBotLookMigrated. Clearing the look leaves the row in place.
type DesktopBotLooksFile struct {
	Looks    map[string]DesktopBotLook `json:"looks"`
	Migrated []string                  `json:"migrated"`
}

func (a *App) desktopBotLooksDBPath() string {
	return filepath.Join(a.GetDataDir(), "desktop_bot_appearance.db")
}

func desktopBotLookUserKey(userID string) string {
	key := strings.TrimSpace(userID)
	if key == "" {
		return "local"
	}
	return key
}

func openDesktopBotLooksDB(dbPath string) (*sql.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("desktop bot appearance database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create desktop bot appearance database directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open desktop bot appearance database: %w", err)
	}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure desktop bot appearance database: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS bot_look (
		user_key TEXT NOT NULL,
		bot_id TEXT NOT NULL,
		color TEXT NOT NULL,
		shape TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (user_key, bot_id)
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create desktop bot appearance table: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS bot_look_migrated (
		user_key TEXT NOT NULL,
		bot_id TEXT NOT NULL,
		PRIMARY KEY (user_key, bot_id)
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create desktop bot appearance migration table: %w", err)
	}
	return db, nil
}

func validDesktopBotLook(color, shape string) (string, string, bool) {
	color = strings.TrimSpace(color)
	shape = strings.TrimSpace(shape)
	if !desktopBotColorRe.MatchString(color) || !desktopBotShapeAllow[shape] {
		return "", "", false
	}
	return color, shape, true
}

func validDesktopBotID(botID string) (string, bool) {
	botID = strings.TrimSpace(botID)
	if botID == "" || len(botID) > 200 {
		return "", false
	}
	return botID, true
}

// LoadDesktopBotLooks reads this account's badge looks from SQLite.
// legacyLooksJSON and legacyMigratedJSON are the old WebView documents
// (maclaw.desktopBotAppearance.v1 and its .migrated sibling). A non-empty
// document inserts rows that are not already stored, so a leftover browser
// document cannot replace a look saved later. Invalid JSON is an error and
// leaves the database unchanged. One bad user or bot entry is skipped.
// The caller deletes the browser keys only after this returns no error.
// Empty strings only read.
func (a *App) LoadDesktopBotLooks(userID, legacyLooksJSON, legacyMigratedJSON string) (DesktopBotLooksFile, error) {
	desktopBotLooksMu.Lock()
	defer desktopBotLooksMu.Unlock()
	db, err := openDesktopBotLooksDB(a.desktopBotLooksDBPath())
	if err != nil {
		return DesktopBotLooksFile{}, err
	}
	defer db.Close()
	if err := importDesktopBotLooks(db, legacyLooksJSON, legacyMigratedJSON); err != nil {
		return DesktopBotLooksFile{}, err
	}
	return readDesktopBotLooks(db, desktopBotLookUserKey(userID))
}

// decodeDesktopBotJSONObject accepts a JSON object. Valid JSON of any other
// shape imports nothing. Broken JSON is an error.
func decodeDesktopBotJSONObject(raw string) (map[string]json.RawMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		var probe any
		if json.Unmarshal([]byte(raw), &probe) == nil {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

func importDesktopBotLooks(db *sql.DB, looksJSON, migratedJSON string) error {
	looksJSON = strings.TrimSpace(looksJSON)
	migratedJSON = strings.TrimSpace(migratedJSON)
	if looksJSON == "" && migratedJSON == "" {
		return nil
	}
	if len(looksJSON) > maxDesktopBotLooksJSONBytes || len(migratedJSON) > maxDesktopBotLooksJSONBytes {
		return fmt.Errorf("desktop bot appearance import exceeds %d bytes", maxDesktopBotLooksJSONBytes)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin desktop bot appearance import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertLegacyLooks(tx, looksJSON, now); err != nil {
		return err
	}
	if err := insertLegacyMigrated(tx, migratedJSON); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit desktop bot appearance import: %w", err)
	}
	return nil
}

func insertLegacyLooks(tx *sql.Tx, looksJSON, now string) error {
	doc, err := decodeDesktopBotJSONObject(looksJSON)
	if err != nil {
		return fmt.Errorf("desktop bot appearance import must be a JSON object")
	}
	for userKey, rawBots := range doc {
		userKey = strings.TrimSpace(userKey)
		if userKey == "" || len(userKey) > 200 {
			continue
		}
		var bots map[string]json.RawMessage
		if json.Unmarshal(rawBots, &bots) != nil || bots == nil {
			continue
		}
		for botID, rawLook := range bots {
			botID, ok := validDesktopBotID(botID)
			if !ok {
				continue
			}
			var look DesktopBotLook
			if json.Unmarshal(rawLook, &look) != nil {
				continue
			}
			color, shape, ok := validDesktopBotLook(look.Color, look.Shape)
			if !ok {
				continue
			}
			// A row already saved wins. The browser document is only the
			// backlog from before SQLite existed.
			if _, err := tx.Exec(`INSERT INTO bot_look (user_key, bot_id, color, shape, updated_at)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(user_key, bot_id) DO NOTHING`,
				userKey, botID, color, shape, now); err != nil {
				return fmt.Errorf("import desktop bot appearance: %w", err)
			}
		}
	}
	return nil
}

func insertLegacyMigrated(tx *sql.Tx, migratedJSON string) error {
	doc, err := decodeDesktopBotJSONObject(migratedJSON)
	if err != nil {
		return fmt.Errorf("desktop bot appearance migration import must be a JSON object")
	}
	for userKey, rawIDs := range doc {
		userKey = strings.TrimSpace(userKey)
		if userKey == "" || len(userKey) > 200 {
			continue
		}
		var ids []json.RawMessage
		if json.Unmarshal(rawIDs, &ids) != nil {
			continue
		}
		for _, rawID := range ids {
			var botID string
			if json.Unmarshal(rawID, &botID) != nil {
				continue
			}
			botID, ok := validDesktopBotID(botID)
			if !ok {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO bot_look_migrated (user_key, bot_id) VALUES (?, ?)
				ON CONFLICT(user_key, bot_id) DO NOTHING`, userKey, botID); err != nil {
				return fmt.Errorf("import desktop bot appearance migration: %w", err)
			}
		}
	}
	return nil
}

func readDesktopBotLooks(db *sql.DB, userKey string) (DesktopBotLooksFile, error) {
	file := DesktopBotLooksFile{Looks: map[string]DesktopBotLook{}, Migrated: []string{}}
	rows, err := db.Query(`SELECT bot_id, color, shape FROM bot_look WHERE user_key = ? ORDER BY bot_id`, userKey)
	if err != nil {
		return DesktopBotLooksFile{}, fmt.Errorf("read desktop bot appearance: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var botID, color, shape string
		if err := rows.Scan(&botID, &color, &shape); err != nil {
			return DesktopBotLooksFile{}, fmt.Errorf("scan desktop bot appearance: %w", err)
		}
		color, shape, ok := validDesktopBotLook(color, shape)
		if !ok {
			continue
		}
		file.Looks[botID] = DesktopBotLook{Color: color, Shape: shape}
	}
	if err := rows.Err(); err != nil {
		return DesktopBotLooksFile{}, fmt.Errorf("read desktop bot appearance: %w", err)
	}
	migRows, err := db.Query(`SELECT bot_id FROM bot_look_migrated WHERE user_key = ? ORDER BY bot_id`, userKey)
	if err != nil {
		return DesktopBotLooksFile{}, fmt.Errorf("read desktop bot appearance migration: %w", err)
	}
	defer migRows.Close()
	for migRows.Next() {
		var botID string
		if err := migRows.Scan(&botID); err != nil {
			return DesktopBotLooksFile{}, fmt.Errorf("scan desktop bot appearance migration: %w", err)
		}
		if botID != "" {
			file.Migrated = append(file.Migrated, botID)
		}
	}
	if err := migRows.Err(); err != nil {
		return DesktopBotLooksFile{}, fmt.Errorf("read desktop bot appearance migration: %w", err)
	}
	return file, nil
}

// SaveDesktopBotLook upserts one bot's badge look for this account.
func (a *App) SaveDesktopBotLook(userID, botID, color, shape string) error {
	botID, ok := validDesktopBotID(botID)
	if !ok {
		return fmt.Errorf("desktop bot id is required")
	}
	color, shape, ok = validDesktopBotLook(color, shape)
	if !ok {
		return fmt.Errorf("desktop bot appearance must be a #rrggbb color and a known shape")
	}
	desktopBotLooksMu.Lock()
	defer desktopBotLooksMu.Unlock()
	db, err := openDesktopBotLooksDB(a.desktopBotLooksDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO bot_look (user_key, bot_id, color, shape, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_key, bot_id) DO UPDATE SET
			color = excluded.color, shape = excluded.shape, updated_at = excluded.updated_at`,
		desktopBotLookUserKey(userID), botID, color, shape, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("save desktop bot appearance: %w", err)
	}
	return nil
}

// ClearDesktopBotLook removes one bot's local badge look.
func (a *App) ClearDesktopBotLook(userID, botID string) error {
	botID, ok := validDesktopBotID(botID)
	if !ok {
		return fmt.Errorf("desktop bot id is required")
	}
	desktopBotLooksMu.Lock()
	defer desktopBotLooksMu.Unlock()
	db, err := openDesktopBotLooksDB(a.desktopBotLooksDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM bot_look WHERE user_key = ? AND bot_id = ?`, desktopBotLookUserKey(userID), botID); err != nil {
		return fmt.Errorf("clear desktop bot appearance: %w", err)
	}
	return nil
}

// MarkDesktopBotLookMigrated records that this bot's current look was copied to Hub.
func (a *App) MarkDesktopBotLookMigrated(userID, botID string) error {
	botID, ok := validDesktopBotID(botID)
	if !ok {
		return fmt.Errorf("desktop bot id is required")
	}
	desktopBotLooksMu.Lock()
	defer desktopBotLooksMu.Unlock()
	db, err := openDesktopBotLooksDB(a.desktopBotLooksDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO bot_look_migrated (user_key, bot_id) VALUES (?, ?)
		ON CONFLICT(user_key, bot_id) DO NOTHING`, desktopBotLookUserKey(userID), botID); err != nil {
		return fmt.Errorf("mark desktop bot appearance migrated: %w", err)
	}
	return nil
}

// UnmarkDesktopBotLookMigrated forgets a copied look so a later different one can be sent.
// Missing rows are success. The look row itself is left in place.
func (a *App) UnmarkDesktopBotLookMigrated(userID, botID string) error {
	botID, ok := validDesktopBotID(botID)
	if !ok {
		return fmt.Errorf("desktop bot id is required")
	}
	desktopBotLooksMu.Lock()
	defer desktopBotLooksMu.Unlock()
	db, err := openDesktopBotLooksDB(a.desktopBotLooksDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM bot_look_migrated WHERE user_key = ? AND bot_id = ?`, desktopBotLookUserKey(userID), botID); err != nil {
		return fmt.Errorf("unmark desktop bot appearance migrated: %w", err)
	}
	return nil
}
