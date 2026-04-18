package session

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages the SQLite FTS5 index for session transcript search.
type Store struct {
	db     *sql.DB
	dbPath string
}

// SessionDocument represents a stored session transcript.
type SessionDocument struct {
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Platform  string    `json:"platform"` // "gui", "tui", "im"
	Topic     string    `json:"topic"`
	FullText  string    `json:"full_text"`
}

// SearchResult represents a single search hit.
type SearchResult struct {
	SessionID string  `json:"session_id"`
	Timestamp string  `json:"timestamp"`
	Platform  string  `json:"platform"`
	Topic     string  `json:"topic"`
	Snippet   string  `json:"snippet"`
	Rank      float64 `json:"rank"`
}

// NewStore opens or creates the FTS5 database at the given path.
// It auto-creates the directory and schema if they don't exist.
func NewStore(dbPath string) (*Store, error) {
	// Create directory if it doesn't exist.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("session store: create directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("session store: open db: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("session store: set WAL mode: %w", err)
	}

	// Create schema.
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("session store: create schema: %w", err)
	}

	return &Store{db: db, dbPath: dbPath}, nil
}

// createSchema creates the sessions table, FTS5 virtual table, and triggers.
func createSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    timestamp  TEXT NOT NULL,
    platform   TEXT NOT NULL,
    topic      TEXT DEFAULT '',
    full_text  TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
    full_text,
    topic,
    content='sessions',
    content_rowid='rowid',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS sessions_ai AFTER INSERT ON sessions BEGIN
    INSERT INTO sessions_fts(rowid, full_text, topic)
    VALUES (new.rowid, new.full_text, new.topic);
END;

CREATE TRIGGER IF NOT EXISTS sessions_ad AFTER DELETE ON sessions BEGIN
    INSERT INTO sessions_fts(sessions_fts, rowid, full_text, topic)
    VALUES ('delete', old.rowid, old.full_text, old.topic);
END;
`
	_, err := db.Exec(schema)
	return err
}

// Persist stores a session transcript in the FTS5 index.
func (s *Store) Persist(doc SessionDocument) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO sessions (session_id, timestamp, platform, topic, full_text) VALUES (?, ?, ?, ?, ?)`,
		doc.SessionID,
		doc.Timestamp.Format(time.RFC3339),
		doc.Platform,
		doc.Topic,
		doc.FullText,
	)
	if err != nil {
		return fmt.Errorf("session store: persist: %w", err)
	}
	return nil
}

// Search performs full-text search, returning ranked results up to maxResults.
// Returns a "no results found" message in the result set if no matches are found.
func (s *Store) Search(query string, maxResults int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return []SearchResult{{
			Snippet: "no results found",
		}}, nil
	}

	rows, err := s.db.Query(
		`SELECT s.session_id, s.timestamp, s.platform, s.topic,
		        snippet(sessions_fts, 0, '<b>', '</b>', '...', 32) as snippet,
		        bm25(sessions_fts) as rank
		 FROM sessions_fts f
		 JOIN sessions s ON f.rowid = s.rowid
		 WHERE sessions_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, maxResults,
	)
	if err != nil {
		return nil, fmt.Errorf("session store: search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SessionID, &r.Timestamp, &r.Platform, &r.Topic, &r.Snippet, &r.Rank); err != nil {
			return nil, fmt.Errorf("session store: scan result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session store: rows iteration: %w", err)
	}

	if len(results) == 0 {
		return []SearchResult{{
			Snippet: "no results found",
		}}, nil
	}

	return results, nil
}

// Prune removes sessions older than the given duration.
// Returns the number of sessions removed.
func (s *Store) Prune(olderThan time.Duration) (int, error) {
	threshold := time.Now().Add(-olderThan).Format(time.RFC3339)
	result, err := s.db.Exec(
		`DELETE FROM sessions WHERE timestamp < ?`,
		threshold,
	)
	if err != nil {
		return 0, fmt.Errorf("session store: prune: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("session store: prune rows affected: %w", err)
	}
	return int(n), nil
}

// SessionSummary is a lightweight view of a session (no full_text).
type SessionSummary struct {
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
	Platform  string `json:"platform"`
	Topic     string `json:"topic"`
	TextLen   int    `json:"text_len"` // length of full_text in runes
}

// ListRecent returns the most recent sessions ordered by timestamp descending.
// It does NOT load full_text to keep the response lightweight.
func (s *Store) ListRecent(limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT session_id, timestamp, platform, topic, length(full_text)
		 FROM sessions ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("session store: list recent: %w", err)
	}
	defer rows.Close()

	var results []SessionSummary
	for rows.Next() {
		var r SessionSummary
		if err := rows.Scan(&r.SessionID, &r.Timestamp, &r.Platform, &r.Topic, &r.TextLen); err != nil {
			return nil, fmt.Errorf("session store: scan summary: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetFullText returns the full transcript text for a given session ID.
func (s *Store) GetFullText(sessionID string) (string, error) {
	var text string
	err := s.db.QueryRow(
		`SELECT full_text FROM sessions WHERE session_id = ?`, sessionID).Scan(&text)
	if err != nil {
		return "", fmt.Errorf("session store: get full text: %w", err)
	}
	return text, nil
}

// Delete removes a single session by ID.
func (s *Store) Delete(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("session store: delete: %w", err)
	}
	return nil
}

// Count returns the total number of stored sessions.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	return n, err
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// ExtractTopic extracts the topic from the first user message in a full text transcript.
// It returns the first 100 characters of the first user message content.
func ExtractTopic(fullText string) string {
	// Look for the first [user] block in the serialized transcript format.
	const userMarker = "[user]\n"
	idx := strings.Index(fullText, userMarker)
	if idx == -1 {
		// Fallback: use the first 100 characters of the full text.
		return truncateToRunes(fullText, 100)
	}

	// Extract content after [user]\n until the next separator or end.
	content := fullText[idx+len(userMarker):]
	// Find the end of this block (next "---" separator or end of string).
	if sepIdx := strings.Index(content, "\n"+entrySeparator+"\n"); sepIdx != -1 {
		content = content[:sepIdx]
	} else if sepIdx := strings.Index(content, entrySeparator+"\n"); sepIdx != -1 {
		content = content[:sepIdx]
	}

	content = strings.TrimSpace(content)
	return truncateToRunes(content, 100)
}

// truncateToRunes truncates a string to at most maxRunes runes.
func truncateToRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
