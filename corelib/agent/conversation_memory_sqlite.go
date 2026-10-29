package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const conversationSQLiteSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT '',
    isolation_key TEXT NOT NULL DEFAULT '',
    detached_path TEXT NOT NULL DEFAULT '',
    active_branch_tip TEXT NOT NULL DEFAULT '',
    last_access_ns INTEGER NOT NULL DEFAULT 0,
    created_at_ns INTEGER NOT NULL DEFAULT 0,
    updated_at_ns INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_entries (
    session_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    parent_id TEXT NOT NULL DEFAULT '',
    branch_info_json TEXT NOT NULL DEFAULT '{}',
    entry_json TEXT NOT NULL,
    entry_order INTEGER NOT NULL DEFAULT 0,
    created_at_ns INTEGER NOT NULL DEFAULT 0,
    updated_at_ns INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, node_id),
    FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS conversation_entries_parent
    ON conversation_entries(session_id, parent_id);
CREATE INDEX IF NOT EXISTS conversation_entries_updated
    ON conversation_entries(session_id, updated_at_ns);
CREATE INDEX IF NOT EXISTS conversation_entries_walk
    ON conversation_entries(session_id, node_id, parent_id);
CREATE INDEX IF NOT EXISTS conversation_entries_order
    ON conversation_entries(session_id, entry_order, node_id);
CREATE TABLE IF NOT EXISTS conversation_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
`

// conversationMetaLegacyImportKey marks that the one-shot legacy JSON import
// committed. Deciding "already imported" from the database file's existence
// strands legacy sessions when the process dies after the file is created but
// before the import transaction commits: the next open would skip the import
// while the untouched JSON files are never read again.
const conversationMetaLegacyImportKey = "legacy_import"

type conversationSQLiteStore struct {
	db   *sql.DB
	path string
}

type legacyConversationMigration struct {
	sessions map[string]persistedSession
	detached map[string]string
	files    []string
}

func conversationSQLitePath(storePath string) string {
	ext := filepath.Ext(storePath)
	return strings.TrimSuffix(storePath, ext) + ".db"
}

func openConversationSQLite(storePath string) (*conversationSQLiteStore, error) {
	dbPath := conversationSQLitePath(storePath)
	_, statErr := os.Stat(dbPath)
	newDB := os.IsNotExist(statErr)
	if statErr != nil && !newDB {
		return nil, statErr
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &conversationSQLiteStore{db: db, path: dbPath}
	fail := func(err error) (*conversationSQLiteStore, error) {
		_ = db.Close()
		if newDB {
			_ = os.Remove(dbPath)
			_ = os.Remove(dbPath + "-wal")
			_ = os.Remove(dbPath + "-shm")
		}
		return nil, err
	}
	if _, err := db.Exec(conversationSQLiteSchema); err != nil {
		return fail(err)
	}
	imported, err := sqliteLegacyImportDone(db)
	if err != nil {
		return fail(err)
	}
	if !imported {
		migration, err := readLegacyConversationMigration(storePath)
		if err != nil {
			return fail(err)
		}
		if err := store.importLegacy(migration); err != nil {
			return fail(err)
		}
		for _, path := range migration.files {
			if err := os.Rename(path, path+".migrated.bak"); err != nil && !os.IsNotExist(err) {
				log.Printf("[ConversationMemory] migrated legacy file backup failed path=%q err=%v", path, err)
			}
		}
	}
	return store, nil
}

func sqliteLegacyImportDone(db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM conversation_meta WHERE key=?`, conversationMetaLegacyImportKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value != "", nil
}

func readLegacyConversationMigration(storePath string) (legacyConversationMigration, error) {
	migration := legacyConversationMigration{
		sessions: map[string]persistedSession{},
		detached: map[string]string{},
	}
	seen := map[string]struct{}{}
	readSnapshot := func(path string) (memorySnapshot, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return memorySnapshot{}, err
		}
		var snapshot memorySnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return memorySnapshot{}, fmt.Errorf("parse legacy conversation %s: %w", path, err)
		}
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			migration.files = append(migration.files, path)
		}
		return snapshot, nil
	}
	if _, err := os.Stat(storePath); err == nil {
		snapshot, err := readSnapshot(storePath)
		if err != nil {
			return migration, err
		}
		for userID, session := range snapshot.Sessions {
			migration.sessions[userID] = normalizeLegacyPersistedSession(session, snapshot.SchemaVersion)
		}
	} else if !os.IsNotExist(err) {
		return migration, err
	}

	indexPath := storePath + ".detached.json"
	var indexedPaths []string
	if data, err := os.ReadFile(indexPath); err == nil {
		var index detachedSessionIndex
		if err := json.Unmarshal(data, &index); err != nil {
			return migration, fmt.Errorf("parse detached index: %w", err)
		}
		migration.files = append(migration.files, indexPath)
		for userID, path := range index.Sessions {
			userID, path = strings.TrimSpace(userID), strings.TrimSpace(path)
			if userID == "" || path == "" {
				continue
			}
			migration.detached[userID] = path
			indexedPaths = append(indexedPaths, path)
		}
	} else if !os.IsNotExist(err) {
		return migration, err
	}
	transcriptPaths, _ := filepath.Glob(filepath.Join(filepath.Dir(storePath), "session-transcripts", "*.json"))
	paths := append(indexedPaths, transcriptPaths...)
	sort.Strings(paths)
	processed := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := processed[path]; ok {
			continue
		}
		processed[path] = struct{}{}
		snapshot, err := readSnapshot(path)
		if err != nil {
			return migration, err
		}
		for userID, session := range snapshot.Sessions {
			session = normalizeLegacyPersistedSession(session, snapshot.SchemaVersion)
			current, exists := migration.sessions[userID]
			if !exists || persistedSessionReplaces(current, session) {
				migration.sessions[userID] = session
			}
			if _, indexed := migration.detached[userID]; !indexed {
				migration.detached[userID] = path
			}
		}
	}
	return migration, nil
}

func persistedSessionReplaces(current, candidate persistedSession) bool {
	if candidate.LastAccess.IsZero() {
		return false
	}
	return current.LastAccess.IsZero() || candidate.LastAccess.After(current.LastAccess)
}

func normalizeLegacyPersistedSession(session persistedSession, schemaVersion int) persistedSession {
	entries := session.Entries
	if snapshotEntriesNeedSanitize(schemaVersion) {
		entries, _ = sanitizeConversationEntriesForPersistence(entries)
	}
	entries = repairOrphanedToolEntries(entries)
	entries = DeduplicateAdjacentAssistantEntries(entries)
	if len(entries) > 0 {
		needsNodeIDs := false
		for _, entry := range entries {
			if entry.ID == "" {
				needsNodeIDs = true
				break
			}
		}
		if needsNodeIDs {
			tree := NewConversationTreeWithTip(entries, session.ActiveBranchTipID)
			session.Entries = tree.AllConversationEntries()
			session.ActiveBranchTipID = tree.TipID()
		} else {
			session.Entries = entries
			tipFound := false
			for _, entry := range entries {
				if entry.ID == session.ActiveBranchTipID {
					tipFound = true
					break
				}
			}
			if !tipFound {
				session.ActiveBranchTipID = entries[len(entries)-1].ID
			}
		}
	} else {
		session.Entries = nil
		session.ActiveBranchTipID = ""
	}
	if strings.TrimSpace(session.InFlightTask) != "" && session.InFlightSetAt.IsZero() {
		session.InFlightSetAt = session.LastAccess
	}
	if session.UnfinishedSlot != nil {
		session.UnfinishedSlot.LegacySessionID = nil
	}
	for _, slot := range session.RecoverySlots {
		if slot != nil {
			slot.LegacySessionID = nil
		}
	}
	return session
}

func (s *conversationSQLiteStore) importLegacy(migration legacyConversationMigration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	userIDs := make([]string, 0, len(migration.sessions))
	for userID := range migration.sessions {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	for _, userID := range userIDs {
		session := migration.sessions[userID]
		if staleProjectSession(userID, session, now) {
			continue
		}
		if err := writeSQLiteSession(tx, userID, session, migration.detached[userID], nil, true); err != nil {
			return err
		}
	}
	for userID, path := range migration.detached {
		if _, ok := migration.sessions[userID]; ok {
			continue
		}
		session := persistedSession{}
		if err := writeSQLiteSession(tx, userID, session, path, nil, true); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO conversation_meta(key, value) VALUES(?, ?)`, conversationMetaLegacyImportKey, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func staleProjectSession(userID string, session persistedSession, now time.Time) bool {
	if !strings.HasPrefix(userID, "desktop-user:") || len(userID) <= len("desktop-user:") {
		return false
	}
	hasRecovery := unfinishedSlotIsPendingDecision(session.UnfinishedSlot) || len(session.RecoverySlots) > 0 || strings.TrimSpace(session.InFlightTask) != ""
	return !hasRecovery && !session.LastAccess.IsZero() && now.Sub(session.LastAccess) > 30*24*time.Hour
}

func sessionProjectID(userID string) string {
	if strings.HasPrefix(userID, "desktop-user:") {
		return strings.TrimPrefix(userID, "desktop-user:")
	}
	return ""
}

func sessionLastAccessNS(session persistedSession) int64 {
	if session.LastAccess.IsZero() {
		return 0
	}
	return session.LastAccess.UnixNano()
}

func writeSQLiteSession(tx *sql.Tx, userID string, session persistedSession, detachedPath string, baseline []ConversationEntry, writeEntries bool) error {
	var plan sqliteEntryPlan
	if writeEntries {
		var err error
		plan, err = planSQLiteEntries(session.Entries, baseline)
		if err != nil {
			return fmt.Errorf("conversation session %q: %w", userID, err)
		}
	}
	_, err := writePlannedSQLiteSession(tx, userID, session, detachedPath, plan, nil, false, writeEntries)
	return err
}

func writePlannedSQLiteSession(tx *sql.Tx, userID string, session persistedSession, detachedPath string, plan sqliteEntryPlan, orders map[string]int, ordersKnown, writeEntries bool) (map[string]int, error) {
	metadata := session
	metadata.Entries = nil
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixNano()
	lastAccess := sessionLastAccessNS(session)
	_, err = tx.Exec(`
INSERT INTO sessions(session_id, project_id, isolation_key, detached_path, active_branch_tip,
    last_access_ns, created_at_ns, updated_at_ns, metadata_json)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET
    project_id=excluded.project_id, isolation_key=excluded.isolation_key,
    detached_path=excluded.detached_path, active_branch_tip=excluded.active_branch_tip,
    last_access_ns=excluded.last_access_ns, updated_at_ns=excluded.updated_at_ns,
    metadata_json=excluded.metadata_json`,
		userID, sessionProjectID(userID), detachedPath, detachedPath, session.ActiveBranchTipID,
		lastAccess, lastAccess, now, string(metadataJSON))
	if err != nil || !writeEntries || (len(plan.upserts) == 0 && len(plan.deletes) == 0) {
		if err != nil || !ordersKnown {
			return nil, err
		}
		return orders, err
	}
	for _, id := range plan.deletes {
		if _, err := tx.Exec(`DELETE FROM conversation_entries WHERE session_id=? AND node_id=?`, userID, id); err != nil {
			return nil, err
		}
	}
	baselineOrder := cloneEntryOrders(orders)
	if !ordersKnown {
		baselineOrder = map[string]int{}
	}
	if len(plan.upserts) == 0 {
		for _, id := range plan.deletes {
			delete(baselineOrder, id)
		}
		if !ordersKnown {
			return nil, nil
		}
		return baselineOrder, nil
	}
	maxOrder := 0
	for _, order := range baselineOrder {
		if order > maxOrder {
			maxOrder = order
		}
	}
	if !ordersKnown {
		orderRows, err := tx.Query(`SELECT node_id, entry_order FROM conversation_entries INDEXED BY conversation_entries_order WHERE session_id=?`, userID)
		if err != nil {
			return nil, err
		}
		for orderRows.Next() {
			var nodeID string
			var order int
			if err := orderRows.Scan(&nodeID, &order); err != nil {
				_ = orderRows.Close()
				return nil, err
			}
			baselineOrder[nodeID] = order
			if order > maxOrder {
				maxOrder = order
			}
		}
		if err := orderRows.Close(); err != nil {
			return nil, err
		}
	}
	for _, id := range plan.deletes {
		delete(baselineOrder, id)
	}
	for _, entry := range plan.upserts {
		order, ok := baselineOrder[entry.id]
		if !ok {
			maxOrder++
			order = maxOrder
			baselineOrder[entry.id] = order
		}
		created := entry.created
		if created == 0 {
			created = now
		}
		branchInfo, _ := json.Marshal(map[string]interface{}{"root": entry.parentID == ""})
		if _, err := tx.Exec(`
INSERT INTO conversation_entries(session_id, node_id, parent_id, branch_info_json,
    entry_json, entry_order, created_at_ns, updated_at_ns)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(session_id,node_id) DO UPDATE SET
    parent_id=excluded.parent_id, branch_info_json=excluded.branch_info_json,
    entry_json=excluded.entry_json, entry_order=excluded.entry_order,
    updated_at_ns=excluded.updated_at_ns`,
			userID, entry.id, entry.parentID, string(branchInfo), entry.payload, order, created, now); err != nil {
			return nil, err
		}
	}
	return baselineOrder, nil
}

func cloneEntryOrders(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for id, order := range in {
		out[id] = order
	}
	return out
}

func (cm *ConversationMemory) sqliteActive() bool {
	return cm != nil && cm.sqlite != nil && !cm.legacyJSON && conversationSQLitePath(cm.storePath) == cm.sqlite.path
}

func (cm *ConversationMemory) loadFromSQLite() error {
	rows, err := cm.sqlite.db.Query(`SELECT session_id, detached_path, metadata_json FROM sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	detached := map[string]string{}
	now := time.Now()
	for rows.Next() {
		var userID, detachedPath, metadataJSON string
		if err := rows.Scan(&userID, &detachedPath, &metadataJSON); err != nil {
			return err
		}
		var session persistedSession
		if err := json.Unmarshal([]byte(metadataJSON), &session); err != nil {
			return fmt.Errorf("decode SQLite session %q: %w", userID, err)
		}
		if staleProjectSession(userID, session, now) {
			cm.markSessionDeleted(userID)
			continue
		}
		if detachedPath != "" {
			detached[userID] = detachedPath
		}
		sh := cm.shardNoWait(userID)
		sh.mu.Lock()
		sh.sessions[userID] = conversationSessionFromPersisted(session)
		sh.sessions[userID].entriesLoaded = false
		sh.sessions[userID].entries = nil
		sh.mu.Unlock()
	}
	if err := rows.Err(); err != nil {
		return err
	}
	cm.detachedMu.Lock()
	cm.detached = detached
	cm.detachedMu.Unlock()
	if cm.promoteRecoverableCheckpoints(now) == 0 {
		cm.expireStaleInFlightTasks(now, InFlightTaskLease)
	}
	return nil
}

func (s *conversationSQLiteStore) loadEntries(userID string) ([]ConversationEntry, map[string]int, error) {
	rows, err := s.db.Query(`SELECT entry_order, entry_json FROM conversation_entries WHERE session_id=? ORDER BY entry_order, node_id`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var entries []ConversationEntry
	orders := map[string]int{}
	for rows.Next() {
		var order int
		var payload string
		if err := rows.Scan(&order, &payload); err != nil {
			return nil, nil, err
		}
		var entry ConversationEntry
		if err := json.Unmarshal([]byte(payload), &entry); err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry)
		if entry.ID != "" {
			orders[entry.ID] = order
		}
	}
	return entries, orders, rows.Err()
}

func (s *conversationSQLiteStore) loadEntry(userID, nodeID string) (ConversationEntry, bool, error) {
	var payload string
	err := s.db.QueryRow(`SELECT entry_json FROM conversation_entries WHERE session_id=? AND node_id=?`, userID, nodeID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationEntry{}, false, nil
	}
	if err != nil {
		return ConversationEntry{}, false, err
	}
	var entry ConversationEntry
	if err := json.Unmarshal([]byte(payload), &entry); err != nil {
		return ConversationEntry{}, false, err
	}
	return entry, true, nil
}

func (s *conversationSQLiteStore) loadActiveBranch(userID, tipID string) ([]ConversationEntry, error) {
	if tipID == "" {
		latest, err := s.latestEntryID(userID)
		if err != nil || latest == "" {
			return nil, err
		}
		tipID = latest
	}
	entries, err := s.loadBranchFromTip(userID, tipID)
	if err != nil || len(entries) > 0 {
		return entries, err
	}
	latest, err := s.latestEntryID(userID)
	if err != nil || latest == "" || latest == tipID {
		return nil, err
	}
	return s.loadBranchFromTip(userID, latest)
}

func (s *conversationSQLiteStore) latestEntryID(userID string) (string, error) {
	var tipID string
	err := s.db.QueryRow(`SELECT node_id FROM conversation_entries INDEXED BY conversation_entries_order WHERE session_id=? ORDER BY entry_order DESC, node_id DESC LIMIT 1`, userID).Scan(&tipID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tipID, err
}

const conversationBranchFromTipSQL = `
WITH RECURSIVE branch AS (
    SELECT node_id, parent_id, 0 AS depth,
        char(31) || node_id || char(31) AS path
    FROM conversation_entries INDEXED BY conversation_entries_walk
    WHERE session_id=? AND node_id=?
    UNION ALL
    SELECT e.node_id, e.parent_id, branch.depth+1,
        branch.path || e.node_id || char(31)
    FROM conversation_entries e INDEXED BY conversation_entries_walk
    JOIN branch ON e.session_id=? AND e.node_id=branch.parent_id
    WHERE branch.parent_id<>'' AND branch.depth < 100000
      AND instr(branch.path, char(31) || e.node_id || char(31))=0
)
SELECT e.entry_json
FROM branch
JOIN conversation_entries e ON e.session_id=? AND e.node_id=branch.node_id
ORDER BY branch.depth DESC`

func (s *conversationSQLiteStore) loadBranchFromTip(userID, tipID string) ([]ConversationEntry, error) {
	rows, err := s.db.Query(conversationBranchFromTipSQL, userID, tipID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ConversationEntry
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var entry ConversationEntry
		if err := json.Unmarshal([]byte(payload), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (cm *ConversationMemory) ensureSessionEntriesLoaded(userID string) {
	if !cm.sqliteActive() {
		return
	}
	sh := cm.shardNoWait(userID)
	sh.mu.Lock()
	s := sh.sessions[userID]
	if s == nil || s.entriesLoaded {
		sh.mu.Unlock()
		return
	}
	sh.mu.Unlock()
	entries, orders, err := cm.sqlite.loadEntries(userID)
	if err != nil {
		cm.loadBroken.Store(true)
		log.Printf("[ConversationMemory] SQLite entry load failed user=%q err=%v", userID, err)
		return
	}
	persisted := append([]ConversationEntry(nil), entries...)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s = sh.sessions[userID]
	if s == nil || s.entriesLoaded {
		return
	}
	s.entries = entries
	s.persistedEntries = persisted
	s.entryOrder = orders
	s.entriesLoaded = true
}

func (cm *ConversationMemory) ensureAllSessionEntriesLoaded() {
	if !cm.sqliteActive() {
		return
	}
	var userIDs []string
	for _, sh := range cm.shards {
		sh.mu.RLock()
		for userID := range sh.sessions {
			userIDs = append(userIDs, userID)
		}
		sh.mu.RUnlock()
	}
	for _, userID := range userIDs {
		cm.ensureSessionEntriesLoaded(userID)
	}
}

func (cm *ConversationMemory) markSessionDeleted(userID string) {
	if cm == nil || cm.storePath == "" {
		return
	}
	cm.persistStateMu.Lock()
	if cm.deletedSessions == nil {
		cm.deletedSessions = map[string]struct{}{}
	}
	cm.deletedSessions[userID] = struct{}{}
	cm.dirty = true
	cm.persistStateMu.Unlock()
	cm.signalPersist()
}

func (cm *ConversationMemory) markSQLiteSessionDirty(userID string) {
	cm.persistStateMu.Lock()
	if cm.dirtySessions == nil {
		cm.dirtySessions = map[string]struct{}{}
	}
	cm.dirtySessions[userID] = struct{}{}
	cm.dirty = true
	cm.persistStateMu.Unlock()
	cm.signalPersist()
}

type sqlitePendingSession struct {
	userID       string
	session      persistedSession
	baseline     []ConversationEntry
	writeEntries bool
	plan         sqliteEntryPlan
	orders       map[string]int
	ordersKnown  bool
	savedOrders  map[string]int
}

type sqliteEntryPlan struct {
	upserts []sqlitePlannedEntry
	deletes []string
}

type sqlitePlannedEntry struct {
	id       string
	parentID string
	payload  string
	created  int64
}

func planSQLiteEntries(entries, baseline []ConversationEntry) (sqliteEntryPlan, error) {
	baselineByID := make(map[string]ConversationEntry, len(baseline))
	for _, entry := range baseline {
		baselineByID[entry.ID] = entry
	}
	var plan sqliteEntryPlan
	currentIDs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return sqliteEntryPlan{}, fmt.Errorf("conversation entry is missing a node ID")
		}
		currentIDs[entry.ID] = struct{}{}
		if old, ok := baselineByID[entry.ID]; ok && reflect.DeepEqual(old, entry) {
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil {
			return sqliteEntryPlan{}, err
		}
		plan.upserts = append(plan.upserts, sqlitePlannedEntry{
			id:       entry.ID,
			parentID: entry.ParentID,
			payload:  string(payload),
			created:  entry.Timestamp * int64(time.Millisecond),
		})
	}
	for id := range baselineByID {
		if _, ok := currentIDs[id]; ok {
			continue
		}
		plan.deletes = append(plan.deletes, id)
	}
	return plan, nil
}

func (cm *ConversationMemory) flushSQLiteDirty() error {
	return cm.flushSQLiteDirtyForUser("")
}

func (cm *ConversationMemory) flushSQLiteDirtyForUser(onlyUser string) error {
	if !cm.persistentLoadFinished() {
		return errPersistentLoadPending
	}
	if cm.loadBroken.Load() {
		return fmt.Errorf("conversation SQLite store is in a failed load state")
	}
	cm.persistStateMu.Lock()
	dirty := cm.dirty
	dirtySessions := cm.dirtySessions
	deletedSessions := cm.deletedSessions
	globalDirty := dirty && len(dirtySessions) == 0 && len(deletedSessions) == 0
	if onlyUser != "" {
		dirtySessions = map[string]struct{}{}
		deletedSessions = map[string]struct{}{}
		if _, ok := cm.dirtySessions[onlyUser]; ok || globalDirty {
			dirtySessions[onlyUser] = struct{}{}
			delete(cm.dirtySessions, onlyUser)
		}
		if _, ok := cm.deletedSessions[onlyUser]; ok {
			deletedSessions[onlyUser] = struct{}{}
			delete(cm.deletedSessions, onlyUser)
		}
		dirty = len(dirtySessions) > 0 || len(deletedSessions) > 0
		cm.dirty = globalDirty || len(cm.dirtySessions) > 0 || len(cm.deletedSessions) > 0
	} else {
		cm.dirty = false
		cm.dirtySessions = nil
		cm.deletedSessions = nil
	}
	cm.persistStateMu.Unlock()
	if !dirty && len(dirtySessions) == 0 && len(deletedSessions) == 0 {
		return nil
	}
	restore := func(err error) error {
		cm.persistStateMu.Lock()
		cm.dirty = true
		if cm.dirtySessions == nil {
			cm.dirtySessions = map[string]struct{}{}
		}
		for userID := range dirtySessions {
			cm.dirtySessions[userID] = struct{}{}
		}
		if cm.deletedSessions == nil {
			cm.deletedSessions = map[string]struct{}{}
		}
		for userID := range deletedSessions {
			cm.deletedSessions[userID] = struct{}{}
		}
		cm.persistStateMu.Unlock()
		return err
	}
	if dirty && len(dirtySessions) == 0 && len(deletedSessions) == 0 {
		dirtySessions = map[string]struct{}{}
		for _, sh := range cm.shards {
			sh.mu.RLock()
			for userID := range sh.sessions {
				dirtySessions[userID] = struct{}{}
			}
			sh.mu.RUnlock()
		}
	}
	detached := cm.copyDetached()
	pending := make([]sqlitePendingSession, 0, len(dirtySessions))
	for userID := range dirtySessions {
		sh := cm.shardNoWait(userID)
		sh.mu.RLock()
		session := sh.sessions[userID]
		if session != nil {
			item := sqlitePendingSession{
				userID:       userID,
				session:      persistedFromSession(session),
				baseline:     append([]ConversationEntry(nil), session.persistedEntries...),
				writeEntries: session.entriesLoaded,
				orders:       cloneEntryOrders(session.entryOrder),
				ordersKnown:  session.entryOrder != nil,
			}
			if !item.writeEntries {
				item.session.Entries = nil
			}
			pending = append(pending, item)
		}
		sh.mu.RUnlock()
	}
	for i := range pending {
		if !pending[i].writeEntries {
			continue
		}
		plan, err := planSQLiteEntries(pending[i].session.Entries, pending[i].baseline)
		if err != nil {
			return restore(fmt.Errorf("conversation session %q: %w", pending[i].userID, err))
		}
		pending[i].plan = plan
	}
	tx, err := cm.sqlite.db.Begin()
	if err != nil {
		return restore(err)
	}
	for userID := range deletedSessions {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE session_id=?`, userID); err != nil {
			_ = tx.Rollback()
			return restore(err)
		}
	}
	for i := range pending {
		saved, err := writePlannedSQLiteSession(tx, pending[i].userID, pending[i].session, detached[pending[i].userID], pending[i].plan, pending[i].orders, pending[i].ordersKnown, pending[i].writeEntries)
		if err != nil {
			_ = tx.Rollback()
			return restore(err)
		}
		pending[i].savedOrders = saved
	}
	if err := tx.Commit(); err != nil {
		return restore(err)
	}
	for _, item := range pending {
		if !item.writeEntries {
			continue
		}
		sh := cm.shardNoWait(item.userID)
		sh.mu.Lock()
		if session := sh.sessions[item.userID]; session != nil {
			session.persistedEntries = append([]ConversationEntry(nil), item.session.Entries...)
			if item.savedOrders != nil {
				session.entryOrder = item.savedOrders
			}
		}
		sh.mu.Unlock()
	}
	return nil
}

func (cm *ConversationMemory) bindSQLiteDetachedSession(userID, path string) error {
	cm.detachedMu.Lock()
	previous, had := cm.detached[userID]
	if cm.detached == nil {
		cm.detached = map[string]string{}
	}
	cm.detached[userID] = path
	cm.detachedMu.Unlock()
	sh := cm.shardNoWait(userID)
	sh.mu.RLock()
	session := persistedSession{}
	if current := sh.sessions[userID]; current != nil {
		session = persistedFromSession(current)
	}
	sh.mu.RUnlock()
	tx, err := cm.sqlite.db.Begin()
	if err == nil {
		err = writeSQLiteSession(tx, userID, session, path, nil, false)
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		cm.detachedMu.Lock()
		if had {
			cm.detached[userID] = previous
		} else {
			delete(cm.detached, userID)
		}
		cm.detachedMu.Unlock()
		return err
	}
	return nil
}

func loadSQLiteSeparateSessionEntries(path, userID string) ([]ConversationEntry, error) {
	dir := filepath.Dir(path)
	candidates := []string{
		filepath.Join(dir, "conversation.db"),
		filepath.Join(dir, "ai_assistant_conversation.db"),
		filepath.Join(filepath.Dir(dir), "conversation.db"),
		filepath.Join(filepath.Dir(dir), "ai_assistant_conversation.db"),
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*conversation*.db"))
	parentMatches, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "*conversation*.db"))
	candidates = append(candidates, matches...)
	candidates = append(candidates, parentMatches...)
	seen := map[string]struct{}{}
	for _, dbPath := range candidates {
		if _, ok := seen[dbPath]; ok {
			continue
		}
		seen[dbPath] = struct{}{}
		if info, err := os.Stat(dbPath); err != nil || info.IsDir() {
			continue
		}
		db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
		if err != nil {
			continue
		}
		store := &conversationSQLiteStore{db: db, path: dbPath}
		var boundPath string
		err = db.QueryRow(`SELECT detached_path FROM sessions WHERE session_id=?`, userID).Scan(&boundPath)
		if err == nil && (boundPath == path || boundPath == "") {
			entries, _, loadErr := store.loadEntries(userID)
			_ = db.Close()
			return entries, loadErr
		}
		_ = db.Close()
	}
	return nil, nil
}
