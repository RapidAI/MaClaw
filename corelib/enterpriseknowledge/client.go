// Package enterpriseknowledge implements Hub→client one-way digital-asset sync
// and local search for enterprise libraries (enterprise_knowledge.db).
//
// Used by GUI, MaClawSrv, and agentservice auto-recall. Each Client is scoped
// to a single data directory (GUI: ~/.maclaw/data; MaClawSrv: per-user data dir).
package enterpriseknowledge

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	_ "modernc.org/sqlite"
)

// Library is a local enterprise library state row.
type Library struct {
	LibraryID       string `json:"library_id"`
	Name            string `json:"name"`
	LastRev         int64  `json:"last_rev"`
	AccessState     string `json:"access_state"`
	ACLFingerprint  string `json:"acl_fingerprint"`
	LastSyncAt      string `json:"last_sync_at"`
	LastError       string `json:"last_error"`
	UserSyncEnabled bool   `json:"user_sync_enabled"`
	HubSyncEnabled  bool   `json:"hub_sync_enabled"`
}

// Client holds open handles for one data directory.
type Client struct {
	dataDir string
	mu      sync.Mutex
	store   *knowledge.SQLiteStore
	meta    *sql.DB
	closed  bool
}

// KnowledgeDBPath returns the enterprise knowledge SQLite path under dataDir.
func KnowledgeDBPath(dataDir string) string {
	return filepath.Join(dataDir, "enterprise_knowledge.db")
}

// MetaDBPath returns the library-state SQLite path under dataDir.
func MetaDBPath(dataDir string) string {
	return filepath.Join(dataDir, "enterprise_library_state.db")
}

// Open opens (or creates) enterprise knowledge + meta DBs under dataDir.
func Open(dataDir string) (*Client, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("dataDir required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	meta, err := openMetaDB(MetaDBPath(dataDir))
	if err != nil {
		return nil, err
	}
	store, err := knowledge.NewSQLiteStore(KnowledgeDBPath(dataDir))
	if err != nil {
		_ = meta.Close()
		return nil, err
	}
	return &Client{dataDir: dataDir, store: store, meta: meta}, nil
}

// OpenMetaOnly opens only the meta DB (list/toggle without knowledge content).
func OpenMetaOnly(dataDir string) (*Client, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("dataDir required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	meta, err := openMetaDB(MetaDBPath(dataDir))
	if err != nil {
		return nil, err
	}
	return &Client{dataDir: dataDir, meta: meta}, nil
}

// Close releases handles. Safe to call multiple times.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var first error
	if c.store != nil {
		if err := c.store.Close(); err != nil && first == nil {
			first = err
		}
		c.store = nil
	}
	if c.meta != nil {
		if err := c.meta.Close(); err != nil && first == nil {
			first = err
		}
		c.meta = nil
	}
	return first
}

// DataDir returns the client data directory.
func (c *Client) DataDir() string {
	if c == nil {
		return ""
	}
	return c.dataDir
}

// Store returns the underlying knowledge store (may be nil for meta-only clients).
func (c *Client) Store() *knowledge.SQLiteStore {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.store
}

// metaDB returns the meta handle if the client is still open (snapshot under lock).
func (c *Client) metaDB() (*sql.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("client nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.meta == nil {
		return nil, fmt.Errorf("client not open")
	}
	return c.meta, nil
}

// EnsureStore opens the knowledge DB if this client was OpenMetaOnly.
func (c *Client) EnsureStore() (*knowledge.SQLiteStore, error) {
	if c == nil {
		return nil, fmt.Errorf("client nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("client closed")
	}
	if c.store != nil {
		return c.store, nil
	}
	store, err := knowledge.NewSQLiteStore(KnowledgeDBPath(c.dataDir))
	if err != nil {
		return nil, err
	}
	c.store = store
	log.Printf("[enterpriseknowledge] store opened at %s", KnowledgeDBPath(c.dataDir))
	return c.store, nil
}

// MetaDBExists reports whether the library-state file is present under dataDir
// (cheap gate before OpenMetaOnly on hot search/auto-recall paths).
func MetaDBExists(dataDir string) bool {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return false
	}
	st, err := os.Stat(MetaDBPath(dataDir))
	return err == nil && !st.IsDir() && st.Size() > 0
}

func openMetaDB(path string) (*sql.DB, error) {
	// modernc.org/sqlite: busy_timeout in ms; single connection serializes writers.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrateMetaSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrateMetaSchema(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS enterprise_library_state (
  library_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  last_rev INTEGER NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL DEFAULT '',
  acl_fingerprint TEXT NOT NULL DEFAULT '',
  access_state TEXT NOT NULL DEFAULT 'active',
  last_sync_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  user_sync_enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS enterprise_source_map (
  library_id TEXT NOT NULL,
  remote_source_id TEXT NOT NULL,
  local_source_id TEXT NOT NULL,
  PRIMARY KEY (library_id, remote_source_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_esm_local ON enterprise_source_map(local_source_id);
`); err != nil {
		return err
	}
	_, _ = db.Exec(`ALTER TABLE enterprise_library_state ADD COLUMN user_sync_enabled INTEGER NOT NULL DEFAULT 1`)
	return nil
}

// ListLibraries returns local libraries including revoked (so purge UI can clear keep_local caches).
// Search/auto-recall still filter to access_state=active only.
func (c *Client) ListLibraries() ([]Library, error) {
	return c.listLibraries(true)
}

// ListLibrariesActive returns non-revoked libraries (active + sync_disabled).
func (c *Client) ListLibrariesActive() ([]Library, error) {
	return c.listLibraries(false)
}

func (c *Client) listLibraries(includeRevoked bool) ([]Library, error) {
	meta, err := c.metaDB()
	if err != nil {
		return nil, err
	}
	q := `SELECT library_id, name, last_rev, access_state, acl_fingerprint,
		IFNULL(last_sync_at,''), last_error, COALESCE(user_sync_enabled, 1)
		FROM enterprise_library_state`
	if !includeRevoked {
		q += ` WHERE access_state <> 'revoked'`
	}
	q += ` ORDER BY CASE access_state WHEN 'active' THEN 0 WHEN 'sync_disabled' THEN 1 WHEN 'revoked' THEN 2 ELSE 3 END, name`
	rows, err := meta.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Library{}
	for rows.Next() {
		var v Library
		var userSync int
		if err := rows.Scan(&v.LibraryID, &v.Name, &v.LastRev, &v.AccessState, &v.ACLFingerprint, &v.LastSyncAt, &v.LastError, &userSync); err != nil {
			return nil, err
		}
		v.UserSyncEnabled = userSync != 0
		v.HubSyncEnabled = v.AccessState != "sync_disabled" && v.AccessState != "revoked"
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetUserSync enables/disables Hub pull for one library on this device.
func (c *Client) SetUserSync(libraryID string, enabled bool) error {
	meta, err := c.metaDB()
	if err != nil {
		return err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	val := 0
	if enabled {
		val = 1
	}
	res, err := meta.Exec(`UPDATE enterprise_library_state SET user_sync_enabled = ? WHERE library_id = ? AND access_state <> 'revoked'`, val, libraryID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("library not found: %s", libraryID)
	}
	return nil
}

// ActiveLibraryIDs returns library_ids with access_state=active.
func (c *Client) ActiveLibraryIDs(libraryID string) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	meta, err := c.metaDB()
	if err != nil {
		return ids, err
	}
	query := `SELECT library_id FROM enterprise_library_state WHERE access_state = 'active'`
	args := []any{}
	if strings.TrimSpace(libraryID) != "" {
		query += ` AND library_id = ?`
		args = append(args, strings.TrimSpace(libraryID))
	}
	rows, err := meta.Query(query, args...)
	if err != nil {
		return ids, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

// HasActiveLibraries is a cheap existence check before opening knowledge.db.
func (c *Client) HasActiveLibraries() bool {
	meta, err := c.metaDB()
	if err != nil {
		return false
	}
	var one int
	err = meta.QueryRow(`SELECT 1 FROM enterprise_library_state WHERE access_state = 'active' LIMIT 1`).Scan(&one)
	return err == nil && one == 1
}

// SearchActive searches the knowledge store and keeps only active-library sources.
func (c *Client) SearchActive(ctx context.Context, q, libraryID string) ([]knowledge.SearchResult, error) {
	if c == nil {
		return nil, fmt.Errorf("client nil")
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return []knowledge.SearchResult{}, nil
	}
	// Check meta first so empty enterprise caches avoid opening knowledge.db.
	activeIDs, err := c.ActiveLibraryIDs(libraryID)
	if err != nil {
		return nil, err
	}
	if len(activeIDs) == 0 {
		return []knowledge.SearchResult{}, nil
	}
	store, err := c.EnsureStore()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
	}
	hits, err := store.Search(ctx, knowledge.SearchOptions{Query: q, Limit: 20})
	if err != nil {
		return nil, err
	}
	// Precompute prefixes for O(hits * avg_prefix) instead of nested map scans.
	prefixes := make([]string, 0, len(activeIDs))
	for libID := range activeIDs {
		prefixes = append(prefixes, "dal_"+libID+"_")
	}
	filtered := make([]knowledge.SearchResult, 0, len(hits))
	for _, h := range hits {
		sid := h.Source.ID
		keep := false
		for _, p := range prefixes {
			if strings.HasPrefix(sid, p) {
				keep = true
				break
			}
		}
		if keep {
			filtered = append(filtered, h)
			continue
		}
		// Legacy non-namespaced rows: only when exactly one active library and no filter,
		// so multi-lib installs cannot leak unrelated content across libraries.
		if libraryID == "" && len(activeIDs) == 1 && !strings.HasPrefix(sid, "dal_") {
			filtered = append(filtered, h)
		}
	}
	return filtered, nil
}

// SeedLibraryForTest inserts a library state row (tests / local fixtures only).
func SeedLibraryForTest(c *Client, libraryID, name, accessState string, userSyncEnabled bool) error {
	meta, err := c.metaDB()
	if err != nil {
		return err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	if accessState == "" {
		accessState = "active"
	}
	userSync := 0
	if userSyncEnabled {
		userSync = 1
	}
	_, err = meta.Exec(`INSERT INTO enterprise_library_state
		(library_id, name, last_rev, access_state, last_error, user_sync_enabled)
		VALUES (?, ?, 0, ?, '', ?)
		ON CONFLICT(library_id) DO UPDATE SET name=excluded.name, access_state=excluded.access_state,
		user_sync_enabled=excluded.user_sync_enabled`,
		libraryID, name, accessState, userSync)
	return err
}

// PurgeLibrary deletes local sources and meta for a library.
// Removes mapped sources plus any remaining dal_{libraryID}_* namespaced sources.
func (c *Client) PurgeLibrary(libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	meta, err := c.metaDB()
	if err != nil {
		return err
	}
	store, err := c.EnsureStore()
	if err != nil {
		return err
	}
	rows, err := meta.Query(`SELECT local_source_id FROM enterprise_source_map WHERE library_id = ?`, libraryID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	var locals []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		locals = append(locals, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	// Close before further meta ops (MaxOpenConns(1) — do not hold the query connection).
	if err := rows.Close(); err != nil {
		return err
	}
	ctx := context.Background()
	prefix := "dal_" + libraryID + "_"
	// Namespaced IDs are removed by prefix delete (one bulk path). Non-namespaced
	// map entries (legacy) need individual deletes so they are not left behind.
	for _, id := range locals {
		if strings.HasPrefix(id, prefix) {
			continue
		}
		if err := store.DeleteSource(ctx, id); err != nil {
			return fmt.Errorf("delete mapped source %s: %w", id, err)
		}
	}
	prefixDeleted, err := store.DeleteSourcesByIDPrefix(ctx, prefix)
	if err != nil {
		return fmt.Errorf("delete namespaced sources: %w", err)
	}
	if _, err := meta.Exec(`DELETE FROM enterprise_source_map WHERE library_id = ?`, libraryID); err != nil {
		return err
	}
	res, err := meta.Exec(`DELETE FROM enterprise_library_state WHERE library_id = ?`, libraryID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 && len(locals) == 0 && prefixDeleted == 0 {
		return fmt.Errorf("library not found: %s", libraryID)
	}
	return nil
}
