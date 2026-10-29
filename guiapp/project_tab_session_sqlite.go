package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const sessionDBFileName = "sessions.db"

func (p *ProjectTabSessionPersist) withSessionDB(fn func(*sql.DB) error) error {
	if p == nil {
		return fmt.Errorf("session persist is nil")
	}
	// Hold the database lock for the whole call. Close waits for an in-flight
	// save instead of dropping the file underneath it, and a save that arrives
	// after Close does not reopen the file.
	p.dbMu.Lock()
	defer p.dbMu.Unlock()
	db, dir, err := p.sessionDBLocked()
	if err != nil {
		return err
	}
	if err := p.importLegacySessionFiles(db, dir); err != nil {
		return err
	}
	return fn(db)
}

// sessionDBLocked returns the process-lifetime database. The caller holds dbMu.
func (p *ProjectTabSessionPersist) sessionDBLocked() (*sql.DB, string, error) {
	if p.dbClosed {
		return nil, "", fmt.Errorf("session persist is closed")
	}
	dir, err := p.sessionsDir()
	if err != nil {
		return nil, "", err
	}
	if p.db != nil && p.dbDir == dir {
		return p.db, dir, nil
	}
	if p.db != nil {
		_ = p.db.Close()
		p.db = nil
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, sessionDBFileName)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, "", fmt.Errorf("open session db: %w", err)
	}
	db.SetMaxOpenConns(1)
	// Tests remove the temp directory as soon as the test returns. An idle
	// connection would still hold the file, so tests drop it after each call.
	// The process keeps one connection so a conversation save does not reopen
	// the database.
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if err := configureSessionDB(db); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	p.db = db
	p.dbDir = dir
	return db, dir, nil
}

// Close releases the database file. A replaced data directory and process
// shutdown both call it; tests can call it before their temp dir is removed.
func (p *ProjectTabSessionPersist) Close() error {
	if p == nil {
		return nil
	}
	p.dbMu.Lock()
	defer p.dbMu.Unlock()
	p.dbClosed = true
	if p.db == nil {
		return nil
	}
	err := p.db.Close()
	p.db = nil
	p.dbDir = ""
	return err
}

func configureSessionDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tab_index (
    position INTEGER NOT NULL,
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    project_path TEXT NOT NULL DEFAULT '',
    agent_mode TEXT NOT NULL DEFAULT '',
    remote_host TEXT NOT NULL DEFAULT '',
    remote_safety TEXT NOT NULL DEFAULT '',
    last_active_at INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tab_sessions (
    tab_id TEXT PRIMARY KEY,
    project_path TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    conversation_json TEXT NOT NULL DEFAULT '[]',
    conversation_count INTEGER NOT NULL DEFAULT 0,
    conversation_cleared_at INTEGER NOT NULL DEFAULT 0,
    scroll_top INTEGER NOT NULL DEFAULT 0,
    input_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    last_active_at TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tab_sessions_project ON tab_sessions(project_path);
CREATE TABLE IF NOT EXISTS conversation_view_archives (
    tab_id TEXT PRIMARY KEY,
    messages_json TEXT NOT NULL,
    message_count INTEGER NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("session db schema: %w", err)
	}
	return ensureSessionConversationCount(db)
}

// ensureSessionConversationCount adds the stored message count used to rank
// sessions. Parsing every conversation JSON on that read is the expensive part.
func ensureSessionConversationCount(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(tab_sessions)`)
	if err != nil {
		return fmt.Errorf("session db columns: %w", err)
	}
	hasCount := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "conversation_count" {
			hasCount = true
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if hasCount {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE tab_sessions ADD COLUMN conversation_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("session db conversation count: %w", err)
	}
	if _, err := db.Exec(`UPDATE tab_sessions SET conversation_count = COALESCE(json_array_length(conversation_json), 0)`); err != nil {
		return fmt.Errorf("session db conversation count backfill: %w", err)
	}
	return nil
}

func (p *ProjectTabSessionPersist) importLegacySessionFiles(db *sql.DB, dir string) error {
	p.importMu.Lock()
	defer p.importMu.Unlock()
	if p.legacyImported {
		return nil
	}
	var imported string
	err := db.QueryRow(`SELECT value FROM session_meta WHERE key = 'legacy_json_imported'`).Scan(&imported)
	if err == nil {
		p.legacyImported = true
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read session import marker: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if data, readErr := os.ReadFile(filepath.Join(dir, sessionIndexFileName)); readErr == nil && len(data) > 0 {
		var index TabIndex
		if json.Unmarshal(data, &index) != nil {
			log.Printf("[session-persist] corrupted index file, skipping legacy import")
		} else if err := insertTabIndex(tx, &index); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read sessions dir: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || skipSessionDirEntry(name) {
			continue
		}
		path := filepath.Join(dir, name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read session %s: %w", name, readErr)
		}
		var session TabSessionData
		if json.Unmarshal(data, &session) != nil {
			log.Printf("[session-persist] corrupted session file %s, ignoring", name)
			continue
		}
		if strings.TrimSpace(session.TabID) == "" {
			session.TabID = strings.TrimSuffix(name, ".json")
		}
		info, _ := entry.Info()
		updated := time.Now()
		if info != nil {
			updated = info.ModTime()
		}
		if err := upsertTabSession(tx, &session, updated); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isConversationViewArchiveFile(name) {
			continue
		}
		tabID := strings.TrimSuffix(name, conversationViewArchiveSuffix)
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return fmt.Errorf("read conversation archive %s: %w", name, readErr)
		}
		messages, count, ok := decodeConversationViewArchive(data)
		if !ok {
			continue
		}
		if err := upsertConversationArchive(tx, tabID, messages, count); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO session_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	p.legacyImported = true
	return nil
}

func insertTabIndex(tx *sql.Tx, index *TabIndex) error {
	if _, err := tx.Exec(`DELETE FROM tab_index`); err != nil {
		return err
	}
	if index == nil {
		return nil
	}
	for i, entry := range index.Tabs {
		archived := 0
		if entry.Archived {
			archived = 1
		}
		if _, err := tx.Exec(`INSERT INTO tab_index(position, id, type, title, project_path, agent_mode, remote_host, remote_safety, last_active_at, archived)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			i, entry.ID, entry.Type, entry.Title, entry.ProjectPath, entry.AgentMode, entry.RemoteHost, entry.RemoteSafety, entry.LastActiveAt, archived); err != nil {
			return fmt.Errorf("insert tab index %s: %w", entry.ID, err)
		}
	}
	return nil
}

func marshalTabConversation(session *TabSessionData) (string, int, error) {
	if session == nil || strings.TrimSpace(session.TabID) == "" {
		return "", 0, fmt.Errorf("save session: tab_id is required")
	}
	conversation := session.Conversation
	if conversation == nil {
		conversation = []interface{}{}
	}
	raw, err := json.Marshal(conversation)
	if err != nil {
		return "", 0, fmt.Errorf("marshal session %s: %w", session.TabID, err)
	}
	return string(raw), len(conversation), nil
}

func upsertTabSession(tx *sql.Tx, session *TabSessionData, updated time.Time) error {
	raw, count, err := marshalTabConversation(session)
	if err != nil {
		return err
	}
	return writeTabSession(tx, session, raw, count, updated)
}

func writeTabSession(exec interface {
	Exec(string, ...any) (sql.Result, error)
}, session *TabSessionData, conversationJSON string, count int, updated time.Time) error {
	if updated.IsZero() {
		updated = time.Now()
	}
	_, err := exec.Exec(`INSERT INTO tab_sessions(tab_id, project_path, working_dir, conversation_json, conversation_count, conversation_cleared_at, scroll_top, input_text, created_at, last_active_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tab_id) DO UPDATE SET
			project_path = excluded.project_path,
			working_dir = excluded.working_dir,
			conversation_json = excluded.conversation_json,
			conversation_count = excluded.conversation_count,
			conversation_cleared_at = excluded.conversation_cleared_at,
			scroll_top = excluded.scroll_top,
			input_text = excluded.input_text,
			created_at = excluded.created_at,
			last_active_at = excluded.last_active_at,
			updated_at = excluded.updated_at`,
		session.TabID, session.ProjectPath, session.WorkingDir, conversationJSON, count, session.ConversationClearedAt, session.ScrollTop, session.InputText, session.CreatedAt, session.LastActiveAt, updated.UnixNano())
	if err != nil {
		return fmt.Errorf("save session %s: %w", session.TabID, err)
	}
	return nil
}

func upsertConversationArchive(tx *sql.Tx, tabID string, messages []interface{}, count int) error {
	raw, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("marshal conversation archive %s: %w", tabID, err)
	}
	_, err = tx.Exec(`INSERT INTO conversation_view_archives(tab_id, messages_json, message_count) VALUES(?,?,?)
		ON CONFLICT(tab_id) DO UPDATE SET messages_json = excluded.messages_json, message_count = excluded.message_count`,
		tabID, string(raw), count)
	if err != nil {
		return fmt.Errorf("save conversation archive %s: %w", tabID, err)
	}
	return nil
}

func decodeConversationViewArchive(data []byte) ([]interface{}, int, bool) {
	var document conversationViewArchiveDocument
	if json.Unmarshal(data, &document) != nil || len(document.Messages) == 0 {
		return nil, 0, false
	}
	count := len(document.Messages)
	if document.Count != nil {
		count = *document.Count
	}
	return document.Messages, count, true
}

func (p *ProjectTabSessionPersist) loadIndexDB() (*TabIndex, error) {
	index := &TabIndex{Tabs: []TabIndexEntry{}}
	err := p.withSessionDB(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT id, type, title, project_path, agent_mode, remote_host, remote_safety, last_active_at, archived FROM tab_index ORDER BY position ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var entry TabIndexEntry
			var archived int
			if err := rows.Scan(&entry.ID, &entry.Type, &entry.Title, &entry.ProjectPath, &entry.AgentMode, &entry.RemoteHost, &entry.RemoteSafety, &entry.LastActiveAt, &archived); err != nil {
				return err
			}
			entry.Archived = archived != 0
			index.Tabs = append(index.Tabs, entry)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return index, nil
}

func (p *ProjectTabSessionPersist) saveIndexDB(index *TabIndex) error {
	if index == nil {
		index = &TabIndex{}
	}
	return p.withSessionDB(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := insertTabIndex(tx, index); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}

func scanTabSession(scanner interface{ Scan(...any) error }) (*TabSessionData, int64, error) {
	var session TabSessionData
	var conversation string
	var updated int64
	if err := scanner.Scan(&session.TabID, &session.ProjectPath, &session.WorkingDir, &conversation, &session.ConversationClearedAt, &session.ScrollTop, &session.InputText, &session.CreatedAt, &session.LastActiveAt, &updated); err != nil {
		return nil, 0, err
	}
	if conversation != "" && conversation != "null" {
		if err := json.Unmarshal([]byte(conversation), &session.Conversation); err != nil {
			return nil, 0, err
		}
	}
	if session.Conversation == nil {
		session.Conversation = []interface{}{}
	}
	return &session, updated, nil
}

const tabSessionSelect = `SELECT tab_id, project_path, working_dir, conversation_json, conversation_cleared_at, scroll_top, input_text, created_at, last_active_at, updated_at FROM tab_sessions`

func (p *ProjectTabSessionPersist) loadSessionDB(tabID string) (*TabSessionData, error) {
	var session *TabSessionData
	err := p.withSessionDB(func(db *sql.DB) error {
		row := db.QueryRow(tabSessionSelect+` WHERE tab_id = ?`, tabID)
		loaded, _, err := scanTabSession(row)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read session %s: %w", tabID, err)
		}
		session = loaded
		return nil
	})
	return session, err
}

func (p *ProjectTabSessionPersist) upsertSessionDB(session *TabSessionData) error {
	// Encode before taking the database lock. A transcript can be large, and
	// shutdown waits on that lock to close the file.
	raw, count, err := marshalTabConversation(session)
	if err != nil {
		return err
	}
	updated := time.Now()
	// One statement is its own transaction. Begin and commit around it only
	// added two round trips to every conversation save.
	return p.withSessionDB(func(db *sql.DB) error {
		return writeTabSession(db, session, raw, count, updated)
	})
}

type sessionHead struct {
	data    TabSessionData
	convLen int
	updated time.Time
}

// listSessionHeadsDB ranks sessions without decoding conversation JSON.
func (p *ProjectTabSessionPersist) listSessionHeadsDB() ([]sessionHead, error) {
	var heads []sessionHead
	err := p.withSessionDB(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT tab_id, project_path, conversation_cleared_at, last_active_at, updated_at, conversation_count FROM tab_sessions`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var head sessionHead
			var updated int64
			if err := rows.Scan(&head.data.TabID, &head.data.ProjectPath, &head.data.ConversationClearedAt, &head.data.LastActiveAt, &updated, &head.convLen); err != nil {
				return err
			}
			head.updated = time.Unix(0, updated)
			heads = append(heads, head)
		}
		return rows.Err()
	})
	return heads, err
}

func (p *ProjectTabSessionPersist) sessionIDsByProject(db *sql.DB, projectPath string) ([]string, error) {
	rows, err := db.Query(`SELECT tab_id, project_path FROM tab_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) == "" || normalizeProjectSessionPath(path) != projectPath {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (p *ProjectTabSessionPersist) clearSessionDB(tabID string, clearedAt int64, lastActive string) error {
	return p.withSessionDB(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE tab_sessions SET conversation_json = '[]', conversation_count = 0, conversation_cleared_at = ?, last_active_at = ?, updated_at = ? WHERE tab_id = ?`,
			clearedAt, lastActive, time.Now().UnixNano(), tabID)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("clear session %s: %w", tabID, err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, tabID); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		return tx.Commit()
	})
}

func (p *ProjectTabSessionPersist) clearProjectSessionsDB(projectPath string, clearedAt int64, lastActive string) (int, error) {
	var cleared int
	err := p.withSessionDB(func(db *sql.DB) error {
		ids, err := p.sessionIDsByProject(db, projectPath)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		now := time.Now().UnixNano()
		for _, id := range ids {
			if _, err := tx.Exec(`UPDATE tab_sessions SET conversation_json = '[]', conversation_count = 0, conversation_cleared_at = ?, last_active_at = ?, updated_at = ? WHERE tab_id = ?`,
				clearedAt, lastActive, now, id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("clear session %s: %w", id, err)
			}
			if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, id); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if staged := taskCopyArchiveTabID(projectPath); staged != "" {
			if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, staged); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		cleared = len(ids)
		return tx.Commit()
	})
	return cleared, err
}

func (p *ProjectTabSessionPersist) deleteSessionDB(tabID string) error {
	return p.withSessionDB(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tab_sessions WHERE tab_id = ?`, tabID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete session %s: %w", tabID, err)
		}
		if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, tabID); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}

func (p *ProjectTabSessionPersist) deleteProjectSessionsDB(projectPath string) (int, error) {
	index, err := p.loadIndexDB()
	if err != nil {
		return 0, err
	}
	removed := map[string]struct{}{}
	kept := make([]TabIndexEntry, 0, len(index.Tabs))
	for _, entry := range index.Tabs {
		if normalizeProjectSessionPath(entry.ProjectPath) == projectPath {
			if strings.TrimSpace(entry.ID) != "" {
				removed[entry.ID] = struct{}{}
			}
			continue
		}
		kept = append(kept, entry)
	}
	indexChanged := len(kept) != len(index.Tabs)
	index.Tabs = kept
	staged := taskCopyArchiveTabID(projectPath)
	err = p.withSessionDB(func(db *sql.DB) error {
		ids, err := p.sessionIDsByProject(db, projectPath)
		if err != nil {
			return err
		}
		for _, id := range ids {
			removed[id] = struct{}{}
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if indexChanged {
			if err := insertTabIndex(tx, index); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		for id := range removed {
			if _, err := tx.Exec(`DELETE FROM tab_sessions WHERE tab_id = ?`, id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("delete session %s: %w", id, err)
			}
			if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, id); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if staged != "" {
			if _, err := tx.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, staged); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return 0, err
	}
	dir, dirErr := p.sessionsDir()
	if dirErr == nil {
		for id := range removed {
			_ = os.Remove(filepath.Join(dir, id+".json"))
			removeConversationViewArchive(dir, id)
		}
		if staged != "" {
			removeConversationViewArchive(dir, staged)
		}
	}
	return len(removed), nil
}

func (p *ProjectTabSessionPersist) loadConversationArchiveDB(tabID string) ([]interface{}, error) {
	messages := []interface{}{}
	err := p.withSessionDB(func(db *sql.DB) error {
		var raw string
		err := db.QueryRow(`SELECT messages_json FROM conversation_view_archives WHERE tab_id = ?`, tabID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read conversation archive %s: %w", tabID, err)
		}
		var decoded []interface{}
		if json.Unmarshal([]byte(raw), &decoded) != nil || len(decoded) == 0 {
			return nil
		}
		messages = decoded
		return nil
	})
	if messages == nil {
		messages = []interface{}{}
	}
	return messages, err
}

func (p *ProjectTabSessionPersist) saveConversationArchiveDB(tabID string, messages []interface{}) error {
	return p.withSessionDB(func(db *sql.DB) error {
		if len(messages) == 0 {
			_, err := db.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, tabID)
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := upsertConversationArchive(tx, tabID, messages, len(messages)); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}

func (p *ProjectTabSessionPersist) conversationArchiveCountDB(tabID string) (int, error) {
	var count int
	err := p.withSessionDB(func(db *sql.DB) error {
		err := db.QueryRow(`SELECT message_count FROM conversation_view_archives WHERE tab_id = ?`, tabID).Scan(&count)
		if errors.Is(err, sql.ErrNoRows) {
			count = 0
			return nil
		}
		return err
	})
	return count, err
}

func (p *ProjectTabSessionPersist) cleanupOrphanSessionsDB(index *TabIndex, cutoff time.Time) {
	if index == nil {
		return
	}
	known := make(map[string]bool, len(index.Tabs))
	for _, entry := range index.Tabs {
		known[entry.ID] = true
	}
	err := p.withSessionDB(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT tab_id, updated_at FROM tab_sessions`)
		if err != nil {
			return err
		}
		var stale []string
		for rows.Next() {
			var id string
			var updated int64
			if err := rows.Scan(&id, &updated); err != nil {
				_ = rows.Close()
				return err
			}
			if known[id] || !time.Unix(0, updated).Before(cutoff) {
				continue
			}
			stale = append(stale, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, id := range stale {
			if _, err := db.Exec(`DELETE FROM tab_sessions WHERE tab_id = ?`, id); err != nil {
				return err
			}
			if _, err := db.Exec(`DELETE FROM conversation_view_archives WHERE tab_id = ?`, id); err != nil {
				return err
			}
			log.Printf("[session-persist] cleanup: removed orphaned session %s", id)
		}
		return nil
	})
	if err != nil {
		log.Printf("[session-persist] cleanup: orphan scan failed: %v", err)
	}
}
