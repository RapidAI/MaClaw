package guiapp

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
)

// ---------------------------------------------------------------------------
// Project Tab Session Persistence
//
// Manages project tab sessions in SQLite (~/.maclaw/data/sessions/sessions.db).
// _index.json, tab_{id}.json and *.view-archive.json are the pre-migration
// files. The first open copies them in once and leaves them in place.
// ---------------------------------------------------------------------------

const (
	// sessionsSubDir is the subdirectory under the maclaw data dir for sessions.
	sessionsSubDir = "sessions"

	// sessionIndexFileName is the name of the tab list index file.
	sessionIndexFileName = "_index.json"

	// defaultStaleThreshold is the default duration after which inactive
	// sessions are considered stale and eligible for cleanup (30 days).
	defaultStaleThreshold = 30 * 24 * time.Hour
)

// TabIndexEntry represents a single tab entry in the _index.json file.
type TabIndexEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	ProjectPath string `json:"projectPath,omitempty"`
	// AgentMode and RemoteSafety let restored tabs render the right coding
	// environment before the first status refresh completes.
	AgentMode    string `json:"agentMode,omitempty"`
	RemoteHost   string `json:"remoteHost,omitempty"`
	RemoteSafety string `json:"remoteSafety,omitempty"`
	LastActiveAt int64  `json:"lastActiveAt"`
	Archived     bool   `json:"archived"`
}

// TabIndex is the top-level structure of _index.json.
type TabIndex struct {
	Tabs []TabIndexEntry `json:"tabs"`
}

// TabSessionData represents the persisted state of a single project tab session.
type TabSessionData struct {
	TabID       string `json:"tab_id"`
	ProjectPath string `json:"project_path"`
	// WorkingDir is an optional tab-local override. Empty means this tab follows
	// the main assistant's current working directory.
	WorkingDir   string        `json:"working_dir,omitempty"`
	Conversation []interface{} `json:"conversation"`
	// ConversationClearedAt is a millisecond timestamp fence. A delayed
	// frontend snapshot that predates this clear must not resurrect history.
	ConversationClearedAt int64  `json:"conversation_cleared_at,omitempty"`
	ScrollTop             int    `json:"scroll_top"`
	InputText             string `json:"input_text"`
	CreatedAt             string `json:"created_at"`
	LastActiveAt          string `json:"last_active_at"`
}

// ProjectTabSessionPersist handles reading and writing project tab sessions
// to disk. It is safe for concurrent use.
type ProjectTabSessionPersist struct {
	mu             sync.RWMutex
	importMu       sync.Mutex
	legacyImported bool
	baseDir        string
	dbMu           sync.Mutex
	db             *sql.DB
	dbDir          string
	dbClosed       bool
}

// NewProjectTabSessionPersist creates a new persistence handler.
func NewProjectTabSessionPersist() *ProjectTabSessionPersist {
	return &ProjectTabSessionPersist{}
}

func NewProjectTabSessionPersistForBaseDir(baseDir string) *ProjectTabSessionPersist {
	return &ProjectTabSessionPersist{baseDir: baseDir}
}

// sessionsDir returns the absolute path to the sessions directory,
// creating it if it does not exist.
func (p *ProjectTabSessionPersist) sessionsDir() (string, error) {
	baseDir := strings.TrimSpace(p.baseDir)
	if baseDir == "" {
		baseDir = corelib.MaclawBaseDir()
	}
	dir := filepath.Join(baseDir, "data", sessionsSubDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create sessions dir: %w", err)
	}
	return dir, nil
}

// sessionFilePath returns the absolute path to a tab's legacy session file.
func (p *ProjectTabSessionPersist) sessionFilePath(tabID string) (string, error) {
	dir, err := p.sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tabID+".json"), nil
}

// ---------------------------------------------------------------------------
// Index operations
// ---------------------------------------------------------------------------

// LoadIndex reads the tab list. Returns an empty TabIndex (not an error) when
// no tabs have been stored.
func (p *ProjectTabSessionPersist) LoadIndex() (*TabIndex, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.loadIndexDB()
}

// SaveIndex replaces the tab list.
func (p *ProjectTabSessionPersist) SaveIndex(index *TabIndex) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveIndexDB(index)
}

// ---------------------------------------------------------------------------
// Session operations
// ---------------------------------------------------------------------------

// LoadSession reads a single tab's session. Returns nil (not an error) if the
// session does not exist.
func (p *ProjectTabSessionPersist) LoadSession(tabID string) (*TabSessionData, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.loadSessionDB(tabID)
}

func parseSessionLastActiveAt(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil && !parsed.IsZero() {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil && !parsed.IsZero() {
		return parsed, true
	}
	return time.Time{}, false
}

// sessionOutranksLatest decides which snapshot is the paper's transcript.
// A later activity time wins. When two files share a second-precision stamp,
// an explicit clear wins over leftover text, and otherwise the fuller copy wins
// so an unflushed tail is not hidden by the shorter snapshot.
func sessionOutranksLatest(session, latest *TabSessionData, sessionLen, latestLen int, activeAt, latestAt time.Time, hasTimestamp, latestHasTimestamp bool) bool {
	if latest == nil {
		return true
	}
	if hasTimestamp != latestHasTimestamp {
		return hasTimestamp
	}
	if activeAt.After(latestAt) {
		return true
	}
	if !hasTimestamp || !activeAt.Equal(latestAt) {
		return false
	}
	sessionCleared := session.ConversationClearedAt > 0 && sessionLen == 0
	latestCleared := latest.ConversationClearedAt > 0 && latestLen == 0
	if sessionCleared != latestCleared {
		return sessionCleared
	}
	return sessionLen > latestLen
}

// LoadLatestSessionForProject returns the most recently active persisted tab
// session for projectPath. A task can be opened from the history list after its
// tab was closed, so archived index entries remain eligible: closing a tab is
// not the same as deleting its task or its conversation.
//
// This deliberately reads every stored session header instead of trusting
// only the tab index, then loads the winning conversation. A session write
// can outlive an interrupted index update, and that durable conversation is
// still the best recovery source for the task.
func (p *ProjectTabSessionPersist) LoadLatestSessionForProject(projectPath string) (*TabSessionData, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if p == nil || projectPath == "" {
		return nil, nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	heads, err := p.listSessionHeadsDB()
	if err != nil {
		return nil, err
	}

	var latest *sessionHead
	var latestAt time.Time
	var latestHasTimestamp bool
	for i := range heads {
		head := &heads[i]
		if normalizeProjectSessionPath(head.data.ProjectPath) != projectPath {
			continue
		}
		activeAt, hasTimestamp := parseSessionLastActiveAt(head.data.LastActiveAt)
		if !hasTimestamp {
			activeAt = head.updated
		}
		// A real saved LastActiveAt is stronger than the row's write time. The
		// latter is only a legacy fallback and can be newer after a copied or
		// repaired session, despite containing older conversation content.
		var latestSession *TabSessionData
		latestLen := 0
		if latest != nil {
			latestSession = &latest.data
			latestLen = latest.convLen
		}
		if sessionOutranksLatest(&head.data, latestSession, head.convLen, latestLen, activeAt, latestAt, hasTimestamp, latestHasTimestamp) {
			latest = head
			latestAt = activeAt
			latestHasTimestamp = hasTimestamp
		}
	}
	if latest == nil {
		return nil, nil
	}
	return p.loadSessionDB(latest.data.TabID)
}

// ClearProjectSessionConversations removes transcript snapshots from every
// persisted tab belonging to projectPath while preserving their tab metadata.
// A user clearing a task conversation must survive a restart: otherwise the
// history-recovery fallback can revive a transcript that was intentionally
// discarded.
func (p *ProjectTabSessionPersist) ClearProjectSessionConversations(projectPath string) (int, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if p == nil || projectPath == "" {
		return 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	cleared, err := p.clearProjectSessionsDB(projectPath, now.UnixMilli(), now.UTC().Format(time.RFC3339Nano))
	if dir, dirErr := p.sessionsDir(); dirErr == nil {
		removeConversationViewArchive(dir, taskCopyArchiveTabID(projectPath))
	}
	return cleared, err
}

// ClearSessionConversation clears exactly one tab's durable transcript. It is
// used by the tab-level clear action; project-wide clearing uses
// ClearProjectSessionConversations instead.
func (p *ProjectTabSessionPersist) ClearSessionConversation(tabID string) error {
	tabID = strings.TrimSpace(tabID)
	if p == nil || tabID == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if err := p.clearSessionDB(tabID, now.UnixMilli(), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if dir, dirErr := p.sessionsDir(); dirErr == nil {
		removeConversationViewArchive(dir, tabID)
	}
	return nil
}

// SaveSession writes a single tab's session data to disk atomically.
func (p *ProjectTabSessionPersist) SaveSession(session *TabSessionData) error {
	if session == nil || session.TabID == "" {
		return fmt.Errorf("save session: tab_id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Nanoseconds so two writes in the same wall-clock second stay ordered.
	session.LastActiveAt = time.Now().UTC().Format(time.RFC3339Nano)
	return p.upsertSessionDB(session)
}

// putSessionSnapshot stores a session exactly as given, including LastActiveAt.
// SaveSession is the normal writer and always refreshes that timestamp.
func (p *ProjectTabSessionPersist) putSessionSnapshot(session *TabSessionData) error {
	if session == nil || session.TabID == "" {
		return fmt.Errorf("save session: tab_id is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.upsertSessionDB(session)
}

// DeleteSession removes a tab's session file from disk.
func (p *ProjectTabSessionPersist) DeleteSession(tabID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.deleteSessionDB(tabID); err != nil {
		return err
	}
	if dir, dirErr := p.sessionsDir(); dirErr == nil {
		_ = os.Remove(filepath.Join(dir, tabID+".json"))
		removeConversationViewArchive(dir, tabID)
	}
	return nil
}

// DeleteProjectSessions removes every persisted project-tab session for a
// task, including stale files whose index entry was lost. This is intentionally
// different from CloseProjectTabSession: a deleted task must not be restorable.
func (p *ProjectTabSessionPersist) DeleteProjectSessions(projectPath string) (int, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if p == nil || projectPath == "" {
		return 0, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deleteProjectSessionsDB(projectPath)
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

// CleanupStale removes session files and index entries that have been inactive
// for longer than the given threshold. Pass 0 to use the default 30-day threshold.
func (p *ProjectTabSessionPersist) CleanupStale(threshold time.Duration) (int, error) {
	if threshold <= 0 {
		threshold = defaultStaleThreshold
	}

	// Load index (uses its own lock).
	index, err := p.LoadIndex()
	if err != nil {
		return 0, fmt.Errorf("cleanup: load index: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-threshold)

	var kept []TabIndexEntry
	var removed int

	for _, entry := range index.Tabs {
		lastActive := time.Unix(entry.LastActiveAt, 0)
		if lastActive.Before(cutoff) {
			session, loadErr := p.LoadSession(entry.ID)
			if loadErr != nil {
				kept = append(kept, entry)
				continue
			}
			if tabSessionHasRecoveryWork(session) {
				kept = append(kept, entry)
				continue
			}
			// Stale entry — delete session file.
			if err := p.DeleteSession(entry.ID); err != nil {
				log.Printf("[session-persist] cleanup: failed to delete session %s: %v", entry.ID, err)
				// Keep in index if deletion failed.
				kept = append(kept, entry)
				continue
			}
			removed++
			log.Printf("[session-persist] cleanup: removed stale session %s (last active: %s)",
				entry.ID, lastActive.Format("2006-01-02"))
		} else {
			kept = append(kept, entry)
		}
	}

	if removed > 0 {
		index.Tabs = kept
		if err := p.SaveIndex(index); err != nil {
			return removed, fmt.Errorf("cleanup: save index: %w", err)
		}
	}

	// Also scan for orphaned session files not in the index.
	p.cleanupOrphanedFiles(index)

	return removed, nil
}

// cleanupOrphanedFiles removes session files that exist on disk but are not
// referenced in the index. This handles cases where the index was updated
// but the file deletion failed, or manual file manipulation.
func (p *ProjectTabSessionPersist) cleanupOrphanedFiles(index *TabIndex) {
	dir, err := p.sessionsDir()
	if err != nil {
		return
	}

	// Build set of known tab IDs from index.
	known := make(map[string]bool, len(index.Tabs))
	for _, entry := range index.Tabs {
		known[entry.ID+".json"] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-defaultStaleThreshold)
	p.cleanupOrphanSessionsDB(index, cutoff)

	for _, entry := range entries {
		name := entry.Name()
		// Skip the index file and non-JSON files.
		if skipSessionDirEntry(name) {
			continue
		}
		// Skip files that are in the index.
		if known[name] {
			continue
		}
		// Check file modification time — only remove if stale.
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(dir, name)
			if err := os.Remove(filePath); err == nil {
				log.Printf("[session-persist] cleanup: removed orphaned file %s", name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func skipSessionDirEntry(name string) bool {
	return name == sessionIndexFileName || isConversationViewArchiveFile(name) || filepath.Ext(name) != ".json"
}

func removeConversationViewArchive(dir, tabID string) {
	tabID = strings.TrimSpace(tabID)
	if dir == "" || tabID == "" {
		return
	}
	_ = os.Remove(filepath.Join(dir, conversationViewArchiveFileName(tabID)))
}

func (p *ProjectTabSessionPersist) conversationViewArchivePath(tabID string) (string, error) {
	dir, err := p.sessionsDir()
	if err != nil {
		return "", err
	}
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return "", fmt.Errorf("conversation archive: tab_id is required")
	}
	return filepath.Join(dir, conversationViewArchiveFileName(tabID)), nil
}

// LoadConversationViewArchive reads the earlier turns for a tab. A missing
// archive is an empty transcript, not an error.
func (p *ProjectTabSessionPersist) LoadConversationViewArchive(tabID string) ([]interface{}, error) {
	if p == nil {
		return []interface{}{}, nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.loadConversationArchiveDB(tabID)
}

// SaveConversationViewArchive writes earlier turns. An empty slice removes them.
func (p *ProjectTabSessionPersist) SaveConversationViewArchive(tabID string, messages []interface{}) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.saveConversationArchiveDB(tabID, messages); err != nil {
		return err
	}
	if len(messages) == 0 {
		if dir, dirErr := p.sessionsDir(); dirErr == nil {
			removeConversationViewArchive(dir, tabID)
		}
	}
	return nil
}

// ConversationViewArchiveCount reports how many messages a tab archive
// holds. It reads the count metadata stamped with the archive and only
// falls back to deserializing the whole file for archives written before
// the metadata existed, so legacy files keep reporting the same number
// instead of 0.
func (p *ProjectTabSessionPersist) ConversationViewArchiveCount(tabID string) (int, error) {
	if p == nil {
		return 0, nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conversationArchiveCountDB(tabID)
}

// atomicWriteFile writes data to a temporary file then renames it to the
// target path, ensuring the file is never partially written.
// On platforms where rename-over-existing fails, falls back to remove+rename.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp_atomic_*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		// Windows (and some network FS) may refuse rename onto an existing file.
		_ = os.Remove(path)
		if err2 := os.Rename(tmpPath, path); err2 != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename temp to target: %w", err)
		}
	}

	return nil
}
