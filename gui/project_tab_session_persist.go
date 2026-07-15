package main

import (
	"encoding/json"
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
// Manages disk read/write for project tab sessions. Each tab's conversation
// state is stored as an individual JSON file (tab_{id}.json) alongside a
// shared index file (_index.json) that tracks all known tabs.
//
// Session directory: ~/.maclaw/data/sessions/
//
// File layout:
//   ~/.maclaw/data/sessions/_index.json       — Tab list index
//   ~/.maclaw/data/sessions/tab_{id}.json     — Individual session files
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
	ID           string `json:"id"`
	Type         string `json:"type"`
	Title        string `json:"title"`
	ProjectPath  string `json:"projectPath,omitempty"`
	LastActiveAt int64  `json:"lastActiveAt"`
	Archived     bool   `json:"archived"`
}

// TabIndex is the top-level structure of _index.json.
type TabIndex struct {
	Tabs []TabIndexEntry `json:"tabs"`
}

// TabSessionData represents the persisted state of a single project tab session.
type TabSessionData struct {
	TabID        string        `json:"tab_id"`
	ProjectPath  string        `json:"project_path"`
	Conversation []interface{} `json:"conversation"`
	ScrollTop    int           `json:"scroll_top"`
	InputText    string        `json:"input_text"`
	CreatedAt    string        `json:"created_at"`
	LastActiveAt string        `json:"last_active_at"`
}

// ProjectTabSessionPersist handles reading and writing project tab sessions
// to disk. It is safe for concurrent use.
type ProjectTabSessionPersist struct {
	mu      sync.RWMutex
	baseDir string
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

// indexPath returns the absolute path to _index.json.
func (p *ProjectTabSessionPersist) indexPath() (string, error) {
	dir, err := p.sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionIndexFileName), nil
}

// sessionFilePath returns the absolute path to a tab's session file.
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

// LoadIndex reads the tab list index from disk. Returns an empty TabIndex
// (not an error) if the file does not exist.
func (p *ProjectTabSessionPersist) LoadIndex() (*TabIndex, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	path, err := p.indexPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TabIndex{Tabs: []TabIndexEntry{}}, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}

	var index TabIndex
	if err := json.Unmarshal(data, &index); err != nil {
		log.Printf("[session-persist] corrupted index file, resetting: %v", err)
		return &TabIndex{Tabs: []TabIndexEntry{}}, nil
	}

	return &index, nil
}

// SaveIndex writes the tab list index to disk atomically (write to temp then rename).
func (p *ProjectTabSessionPersist) SaveIndex(index *TabIndex) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	path, err := p.indexPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	return atomicWriteFile(path, data)
}

// ---------------------------------------------------------------------------
// Session operations
// ---------------------------------------------------------------------------

// LoadSession reads a single tab's session data from disk. Returns nil (not
// an error) if the session file does not exist.
func (p *ProjectTabSessionPersist) LoadSession(tabID string) (*TabSessionData, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	path, err := p.sessionFilePath(tabID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session %s: %w", tabID, err)
	}

	var session TabSessionData
	if err := json.Unmarshal(data, &session); err != nil {
		log.Printf("[session-persist] corrupted session file %s, ignoring: %v", tabID, err)
		return nil, nil
	}

	return &session, nil
}

// SaveSession writes a single tab's session data to disk atomically.
func (p *ProjectTabSessionPersist) SaveSession(session *TabSessionData) error {
	if session == nil || session.TabID == "" {
		return fmt.Errorf("save session: tab_id is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	path, err := p.sessionFilePath(session.TabID)
	if err != nil {
		return err
	}

	// Update last_active_at timestamp.
	session.LastActiveAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session %s: %w", session.TabID, err)
	}

	return atomicWriteFile(path, data)
}

// DeleteSession removes a tab's session file from disk.
func (p *ProjectTabSessionPersist) DeleteSession(tabID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	path, err := p.sessionFilePath(tabID)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session %s: %w", tabID, err)
	}
	return nil
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

	for _, entry := range entries {
		name := entry.Name()
		// Skip the index file and non-JSON files.
		if name == sessionIndexFileName || filepath.Ext(name) != ".json" {
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
