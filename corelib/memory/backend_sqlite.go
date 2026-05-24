package memory

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	corebm25 "github.com/RapidAI/CodeClaw/corelib/bm25"
	_ "modernc.org/sqlite"
)

// sqliteBackend implements StorageBackend using a local SQLite database
// in WAL mode. Designed for maclawsrv multi-instance deployments where
// multiple processes share the same database file on a local filesystem.
//
// Key properties:
//   - WAL mode: concurrent readers + serialized writers (no reader blocking)
//   - busy_timeout=5000: writers wait up to 5s instead of immediate SQLITE_BUSY
//   - version column: monotonically increasing, enables incremental sync via Since()
//   - soft delete: deleted_at column for sync propagation (other instances need to know)
type sqliteBackend struct {
	db   *sql.DB
	path string
}

// NewSQLiteBackend creates a SQLite backend at the given database path.
// Creates the file and tables if they don't exist.
func NewSQLiteBackend(dbPath string) (*sqliteBackend, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sqlite_backend: mkdir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: open: %w", err)
	}

	// SQLite is file-level locking. Limit to 1 open connection to serialize
	// all access and avoid SQLITE_BUSY errors within the same process.
	// Cross-process concurrency is handled by SQLite's WAL + busy_timeout.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := applyMemoryPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := createMemoryTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &sqliteBackend{db: db, path: dbPath}, nil
}

func applyMemoryPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-8000", // 8MB cache
		"PRAGMA foreign_keys=OFF",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("sqlite_backend pragma %q: %w", p, err)
		}
	}
	return nil
}

func createMemoryTables(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS memories (
    id           TEXT PRIMARY KEY,
    content      TEXT NOT NULL DEFAULT '',
    compact_form TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    category     TEXT NOT NULL DEFAULT '',
    owner_id     TEXT NOT NULL DEFAULT '',
    tags         TEXT NOT NULL DEFAULT '[]',
    entities     TEXT NOT NULL DEFAULT '[]',
    embedding    BLOB DEFAULT NULL,
    strength     REAL NOT NULL DEFAULT 1.0,
    access_count INTEGER NOT NULL DEFAULT 1,
    scope        TEXT NOT NULL DEFAULT '',
    source_type  TEXT NOT NULL DEFAULT '',
    source_url   TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    stale        INTEGER NOT NULL DEFAULT 0,
    dormant      INTEGER NOT NULL DEFAULT 0,
    superseded   INTEGER NOT NULL DEFAULT 0,
    pinned       INTEGER NOT NULL DEFAULT 0,
    level        INTEGER NOT NULL DEFAULT 0,
    version      INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    deleted_at   TEXT DEFAULT NULL,
    extra        TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_memories_version ON memories(version);
CREATE INDEX IF NOT EXISTS idx_memories_owner ON memories(owner_id) WHERE owner_id != '';
CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(content_hash) WHERE content_hash != '';
CREATE INDEX IF NOT EXISTS idx_memories_created_at ON memories(created_at);
CREATE INDEX IF NOT EXISTS idx_memories_source_url ON memories(source_url) WHERE source_url != '';

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    id UNINDEXED,
    text,
    tokenize='trigram'
);

CREATE TABLE IF NOT EXISTS memory_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO memory_meta(key, value) VALUES ('max_version', '0');
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("sqlite_backend: create tables: %w", err)
	}
	return nil
}

// --- StorageBackend interface implementation ---

func (b *sqliteBackend) LoadAll() ([]Entry, error) {
	rows, err := b.db.Query(`SELECT id, content, compact_form, content_hash, category, owner_id,
		tags, entities, embedding, strength, access_count, scope, source_type, source_url,
		title, stale, dormant, superseded, pinned, level, version, created_at, updated_at, extra
		FROM memories WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: load all: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			log.Printf("[sqlite_backend] WARNING: skip corrupt row: %v", err)
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (b *sqliteBackend) SaveEntry(entry *Entry) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, err := sqliteNextVersionTx(tx)
	if err != nil {
		return err
	}
	entry.Version = version
	if err := sqliteSaveEntryTx(tx, entry); err != nil {
		return err
	}
	if err := sqliteUpsertFTSTx(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *sqliteBackend) UpdateEntry(entry *Entry) error {
	// UpdateEntry is the same as SaveEntry (INSERT OR REPLACE) with a new version.
	return b.SaveEntry(entry)
}

func (b *sqliteBackend) UpdateEntries(entries []*Entry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, entry := range entries {
		version, err := sqliteNextVersionTx(tx)
		if err != nil {
			return err
		}
		entry.Version = version
		if err := sqliteSaveEntryTx(tx, entry); err != nil {
			return err
		}
		if err := sqliteUpsertFTSTx(tx, entry); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqliteBackend) UpdateEntriesAndDeleteIDs(entries []*Entry, deleteIDs []string) error {
	if len(entries) == 0 && len(deleteIDs) == 0 {
		return nil
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, entry := range entries {
		version, err := sqliteNextVersionTx(tx)
		if err != nil {
			return err
		}
		entry.Version = version
		if err := sqliteSaveEntryTx(tx, entry); err != nil {
			return err
		}
		if err := sqliteUpsertFTSTx(tx, entry); err != nil {
			return err
		}
	}
	for _, id := range deleteIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		version, err := sqliteNextVersionTx(tx)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`UPDATE memories SET deleted_at = ?, version = ? WHERE id = ?`, now, version, id); err != nil {
			return err
		}
		if err := sqliteDeleteFTSTx(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *sqliteBackend) DeleteEntry(id string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, err := sqliteNextVersionTx(tx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`UPDATE memories SET deleted_at = ?, version = ? WHERE id = ?`, now, version, id); err != nil {
		return err
	}
	if err := sqliteDeleteFTSTx(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *sqliteBackend) Since(version int64) ([]Entry, []string, error) {
	// Modified entries (created or updated after version, not deleted).
	rows, err := b.db.Query(`SELECT id, content, compact_form, content_hash, category, owner_id,
		tags, entities, embedding, strength, access_count, scope, source_type, source_url,
		title, stale, dormant, superseded, pinned, level, version, created_at, updated_at, extra
		FROM memories WHERE version > ? AND deleted_at IS NULL ORDER BY version ASC`, version)
	if err != nil {
		return nil, nil, fmt.Errorf("sqlite_backend: since modified: %w", err)
	}
	defer rows.Close()

	var modified []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			continue
		}
		modified = append(modified, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Deleted entries after version.
	delRows, err := b.db.Query(`SELECT id FROM memories WHERE version > ? AND deleted_at IS NOT NULL ORDER BY version ASC`, version)
	if err != nil {
		return modified, nil, fmt.Errorf("sqlite_backend: since deleted: %w", err)
	}
	defer delRows.Close()

	var deletedIDs []string
	for delRows.Next() {
		var id string
		if err := delRows.Scan(&id); err == nil {
			deletedIDs = append(deletedIDs, id)
		}
	}

	return modified, deletedIDs, delRows.Err()
}

// SearchTextIDs returns IDs matching the SQLite FTS index. It is intentionally
// a prefilter helper; the Store still owns final ranking and prompt selection.
type sqliteTextFilter struct {
	OwnerID     string
	Category    Category
	ProjectPath string
	Since       time.Time
	Until       time.Time
}

func (b *sqliteBackend) SearchTextIDs(query string, limit int) ([]string, error) {
	ftsQuery := sqliteFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := b.db.Query(`SELECT id FROM memories_fts WHERE memories_fts MATCH ? LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: fts search: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) > 0 {
		return ids, err
	}
	return b.searchTextIDsLikeFallback(query, limit)
}

func (b *sqliteBackend) SearchTextIDsFiltered(query string, filter sqliteTextFilter, limit int) ([]string, error) {
	ftsQuery := sqliteFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	where, args := sqliteTextFilterWhere(filter)
	args = append([]any{ftsQuery}, args...)
	args = append(args, limit)
	rows, err := b.db.Query("SELECT f.id FROM memories_fts f JOIN memories m ON m.id = f.id WHERE f.text MATCH ?"+where+" LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: filtered fts search: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) > 0 {
		return ids, err
	}
	return b.searchTextIDsLikeFallbackFiltered(query, filter, limit)
}

func (b *sqliteBackend) searchTextIDsLikeFallback(query string, limit int) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	pattern := "%" + query + "%"
	rows, err := b.db.Query(`SELECT id FROM memories
		WHERE deleted_at IS NULL AND (content LIKE ? OR compact_form LIKE ? OR title LIKE ? OR tags LIKE ? OR entities LIKE ?)
		LIMIT ?`, pattern, pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: text fallback search: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (b *sqliteBackend) searchTextIDsLikeFallbackFiltered(query string, filter sqliteTextFilter, limit int) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	where, args := sqliteTextFilterWhere(filter)
	pattern := "%" + strings.ToLower(query) + "%"
	args = append([]any{pattern, pattern, pattern, pattern, pattern}, args...)
	args = append(args, limit)
	rows, err := b.db.Query("SELECT id FROM memories m WHERE deleted_at IS NULL AND (LOWER(content) LIKE ? OR LOWER(compact_form) LIKE ? OR LOWER(title) LIKE ? OR LOWER(tags) LIKE ? OR LOWER(entities) LIKE ?)"+where+" LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite_backend: filtered text fallback search: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func sqliteTextFilterWhere(filter sqliteTextFilter) (string, []any) {
	var where strings.Builder
	args := make([]any, 0, 8)
	where.WriteString(" AND m.deleted_at IS NULL")
	if filter.OwnerID != "" {
		where.WriteString(" AND (m.owner_id = '' OR m.owner_id = ?)")
		args = append(args, filter.OwnerID)
	}
	if filter.Category != "" {
		where.WriteString(" AND m.category = ?")
		args = append(args, string(filter.Category))
	}
	if !filter.Since.IsZero() {
		where.WriteString(" AND m.created_at >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if !filter.Until.IsZero() {
		where.WriteString(" AND m.created_at <= ?")
		args = append(args, filter.Until.UTC().Format(time.RFC3339Nano))
	}
	if project := strings.ToLower(strings.TrimSpace(filter.ProjectPath)); project != "" {
		like := "%" + project + "%"
		where.WriteString(" AND (LOWER(m.source_url) LIKE ? OR LOWER(m.tags) LIKE ?)")
		args = append(args, like, like)
	}
	return where.String(), args
}

func (b *sqliteBackend) upsertFTS(entry *Entry) error {
	if entry == nil || strings.TrimSpace(entry.ID) == "" {
		return nil
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqliteUpsertFTSTx(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *sqliteBackend) deleteFTS(id string) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqliteDeleteFTSTx(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func sqliteUpsertFTSTx(tx *sql.Tx, entry *Entry) error {
	if entry == nil || strings.TrimSpace(entry.ID) == "" {
		return nil
	}
	if err := sqliteDeleteFTSTx(tx, entry.ID); err != nil {
		return err
	}
	if !entry.IsActive() {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO memories_fts(id, text) VALUES (?, ?)`, entry.ID, sqliteFTSText(*entry))
	if err != nil {
		return fmt.Errorf("sqlite_backend: fts upsert: %w", err)
	}
	return nil
}

func sqliteDeleteFTSTx(tx *sql.Tx, id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM memories_fts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite_backend: fts delete: %w", err)
	}
	return nil
}

func sqliteFTSText(entry Entry) string {
	text := entryToDoc(entry).Text
	if entry.Title != "" {
		text += " " + entry.Title
	}
	// Add the same lexical fallback tokens used by the in-memory BM25 layer so
	// CJK phrase queries can hit even when SQLite's tokenizer keeps long runs.
	if tokens := corebm25.Tokenize(text); len(tokens) > 0 {
		text += " " + strings.Join(tokens, " ")
	}
	return text
}

func sqliteFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	tokens := append([]string{query}, corebm25.Tokenize(query)...)
	seen := make(map[string]struct{}, len(tokens))
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 3 {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		parts = append(parts, "\""+strings.ReplaceAll(token, "\"", "\"\"")+"\"")
		if len(parts) >= 12 {
			break
		}
	}
	return strings.Join(parts, " OR ")
}

func (b *sqliteBackend) MaxVersion() (int64, error) {
	var val string
	err := b.db.QueryRow(`SELECT value FROM memory_meta WHERE key = 'max_version'`).Scan(&val)
	if err != nil {
		return 0, err
	}
	var v int64
	fmt.Sscanf(val, "%d", &v)
	return v, nil
}

func (b *sqliteBackend) SupportsSync() bool {
	return true
}

func (b *sqliteBackend) Close() error {
	if b.db == nil {
		return nil
	}
	return b.db.Close()
}

// --- Internal helpers ---

func (b *sqliteBackend) nextVersion() (int64, error) {
	// Use a single UPDATE ... RETURNING to atomically increment and read.
	// This avoids the TOCTOU race of separate UPDATE + SELECT.
	// Fallback for SQLite versions without RETURNING: use a transaction.
	var val int64
	err := b.db.QueryRow(`UPDATE memory_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'max_version' RETURNING CAST(value AS INTEGER)`).Scan(&val)
	if err == nil {
		return val, nil
	}
	// Fallback: transaction-based increment (for older SQLite without RETURNING).
	tx, err := b.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE memory_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'max_version'`)
	if err != nil {
		return 0, err
	}

	var valStr string
	err = tx.QueryRow(`SELECT value FROM memory_meta WHERE key = 'max_version'`).Scan(&valStr)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	fmt.Sscanf(valStr, "%d", &val)
	return val, nil
}

func sqliteNextVersionTx(tx *sql.Tx) (int64, error) {
	var val int64
	err := tx.QueryRow(`UPDATE memory_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'max_version' RETURNING CAST(value AS INTEGER)`).Scan(&val)
	if err == nil {
		return val, nil
	}
	if _, err := tx.Exec(`UPDATE memory_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'max_version'`); err != nil {
		return 0, err
	}
	var valStr string
	if err := tx.QueryRow(`SELECT value FROM memory_meta WHERE key = 'max_version'`).Scan(&valStr); err != nil {
		return 0, err
	}
	fmt.Sscanf(valStr, "%d", &val)
	return val, nil
}

func sqliteSaveEntryTx(tx *sql.Tx, entry *Entry) error {
	tagsJSON, _ := json.Marshal(entry.Tags)
	entitiesJSON, _ := json.Marshal(entry.Entities)
	extraJSON := marshalExtra(entry)
	embBlob := encodeEmbedding(entry.Embedding)
	const q = `INSERT OR REPLACE INTO memories
		(id, content, compact_form, content_hash, category, owner_id, tags, entities,
		 embedding, strength, access_count, scope, source_type, source_url, title,
		 stale, dormant, superseded, pinned, level, version, created_at, updated_at, extra)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := tx.Exec(q,
		entry.ID, entry.Content, entry.CompactForm, entry.ContentHash,
		string(entry.Category), entry.OwnerID,
		string(tagsJSON), string(entitiesJSON), embBlob,
		entry.Strength, entry.AccessCount, string(entry.Scope),
		entry.SourceType, entry.SourceURL, entry.Title,
		boolToInt(entry.Stale),
		boolToInt(entry.Status == StatusDormant),
		boolToInt(entry.Status == StatusSuperseded),
		boolToInt(entry.Pinned), int(entry.Level), entry.Version,
		entry.CreatedAt.UTC().Format(time.RFC3339Nano),
		entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
		string(extraJSON),
	)
	return err
}

// scanEntry reads a row into an Entry. The scanner interface matches both
// *sql.Row and *sql.Rows.
func scanEntry(scanner interface{ Scan(...interface{}) error }) (Entry, error) {
	var e Entry
	var tagsJSON, entitiesJSON, extraJSON string
	var embBlob []byte
	var category, scope, sourceType, sourceURL string
	var stale, dormant, superseded, pinned, level int
	var version int64
	var createdAt, updatedAt string

	err := scanner.Scan(
		&e.ID, &e.Content, &e.CompactForm, &e.ContentHash,
		&category, &e.OwnerID,
		&tagsJSON, &entitiesJSON, &embBlob,
		&e.Strength, &e.AccessCount, &scope,
		&sourceType, &sourceURL, &e.Title,
		&stale, &dormant, &superseded, &pinned, &level,
		&version, &createdAt, &updatedAt, &extraJSON,
	)
	if err != nil {
		return Entry{}, err
	}

	e.Category = Category(category)
	e.Scope = Scope(scope)
	e.SourceType = sourceType
	e.SourceURL = sourceURL
	e.Stale = stale != 0
	e.Pinned = pinned != 0
	e.Level = TemporalLevel(level)
	e.Version = version
	e.Embedding = decodeEmbeddingBlob(embBlob)

	// Reconstruct Status from dormant/superseded columns.
	switch {
	case superseded != 0:
		e.Status = StatusSuperseded
	case dormant != 0:
		e.Status = StatusDormant
	default:
		e.Status = StatusActive
	}

	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		e.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		e.UpdatedAt = t
	}

	_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
	_ = json.Unmarshal([]byte(entitiesJSON), &e.Entities)
	unmarshalExtra(&e, extraJSON)

	return e, nil
}

// --- Embedding binary encoding ---

func encodeEmbedding(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func decodeEmbeddingBlob(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}

// --- Extra field JSON packing ---

type entryExtra struct {
	RelatedIDs   []string          `json:"related_ids,omitempty"`
	RelatedEdges []RelatedEdge     `json:"related_edges,omitempty"`
	Versions     []VersionSnapshot `json:"versions,omitempty"`
	Interval     *TimeInterval     `json:"interval,omitempty"`
	ParentID     string            `json:"parent_id,omitempty"`
	ChildIDs     []string          `json:"child_ids,omitempty"`
	ValidAt      *time.Time        `json:"valid_at,omitempty"`
	InvalidAt    *time.Time        `json:"invalid_at,omitempty"`
	Stability    *StabilityMeta    `json:"stability_meta,omitempty"`
	EvidenceIDs  []string          `json:"evidence_ids,omitempty"`
	DerivedKind  string            `json:"derived_kind,omitempty"`
	Boundary     *MemoryBoundary   `json:"boundary,omitempty"`
	// Note: Status is NOT stored here — it's derived from the dormant/superseded columns.
}

func marshalExtra(e *Entry) []byte {
	ex := entryExtra{
		RelatedIDs:   e.RelatedIDs,
		RelatedEdges: e.RelatedEdges,
		Versions:     e.Versions,
		Interval:     e.Interval,
		ParentID:     e.ParentID,
		ChildIDs:     e.ChildIDs,
		ValidAt:      e.ValidAt,
		InvalidAt:    e.InvalidAt,
		Stability:    e.Stability,
		EvidenceIDs:  e.EvidenceIDs,
		DerivedKind:  e.DerivedKind,
		Boundary:     e.Boundary,
	}
	data, _ := json.Marshal(ex)
	return data
}

func unmarshalExtra(e *Entry, data string) {
	if data == "" || data == "{}" {
		return
	}
	var ex entryExtra
	if err := json.Unmarshal([]byte(data), &ex); err != nil {
		return
	}
	e.RelatedIDs = ex.RelatedIDs
	e.RelatedEdges = ex.RelatedEdges
	e.Versions = ex.Versions
	e.Interval = ex.Interval
	e.ParentID = ex.ParentID
	e.ChildIDs = ex.ChildIDs
	e.ValidAt = ex.ValidAt
	e.InvalidAt = ex.InvalidAt
	e.Stability = ex.Stability
	e.EvidenceIDs = ex.EvidenceIDs
	e.DerivedKind = ex.DerivedKind
	e.Boundary = ex.Boundary
}

// --- Utility ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
