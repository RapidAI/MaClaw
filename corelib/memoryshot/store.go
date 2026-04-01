package memoryshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store manages snapshot persistence to disk.
type Store struct {
	mu       sync.RWMutex
	baseDir  string
	latestPath string
	backupDir  string
}

// NewStore creates a new Store with the given base directory.
// The directory will be created if it doesn't exist.
func NewStore(baseDir string) (*Store, error) {
	absPath, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("memoryshot: resolve path: %w", err)
	}

	latestPath := filepath.Join(absPath, "latest.json")
	backupDir := filepath.Join(absPath, "backup")

	// Create directories
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("memoryshot: create dir: %w", err)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("memoryshot: create backup dir: %w", err)
	}

	return &Store{
		baseDir:    absPath,
		latestPath: latestPath,
		backupDir:  backupDir,
	}, nil
}

// Save persists the snapshot to disk.
// This will:
// 1. Move existing latest.json to backup
// 2. Write new snapshot to latest.json
func (s *Store) Save(snapshot *Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("memoryshot: cannot save nil snapshot")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update metadata
	snapshot.Version = CurrentVersion
	snapshot.SavedAt = time.Now()

	// Move existing latest to backup (if exists and has content)
	if info, err := os.Stat(s.latestPath); err == nil && info.Size() > 0 {
		if err := s.rotateBackup(); err != nil {
			// Log but don't fail - we can still save the new snapshot
			fmt.Fprintf(os.Stderr, "[memoryshot] backup rotation failed: %v\n", err)
		}
	}

	// Write new snapshot
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("memoryshot: marshal snapshot: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tempPath := s.latestPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("memoryshot: write temp file: %w", err)
	}

	if err := os.Rename(tempPath, s.latestPath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("memoryshot: rename temp file: %w", err)
	}

	return nil
}

// Load reads the latest snapshot from disk.
// Returns nil if no snapshot exists.
func (s *Store) Load() (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.latestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No snapshot yet
		}
		return nil, fmt.Errorf("memoryshot: read file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil // Empty file
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		// Backup corrupted file and return nil
		backupPath := s.latestPath + ".corrupt." + time.Now().Format("20060102_150405")
		_ = os.WriteFile(backupPath, data, 0644)
		return nil, fmt.Errorf("memoryshot: corrupt snapshot backed up to %s: %w", backupPath, err)
	}

	return &snapshot, nil
}

// Clear removes the latest snapshot.
// This should be called after successful restore to prevent stale data.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.latestPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memoryshot: clear snapshot: %w", err)
	}

	return nil
}

// Exists returns true if a snapshot exists.
func (s *Store) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(s.latestPath)
	return err == nil && info.Size() > 0
}

// rotateBackup moves the current latest.json to the backup directory.
func (s *Store) rotateBackup() error {
	// Generate backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(s.backupDir, fmt.Sprintf("snapshot_%s.json", timestamp))

	// Copy to backup
	data, err := os.ReadFile(s.latestPath)
	if err != nil {
		return fmt.Errorf("read latest for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	// Clean up old backups
	return s.cleanupOldBackups()
}

// cleanupOldBackups removes old backups, keeping only MaxBackups most recent.
func (s *Store) cleanupOldBackups() error {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return fmt.Errorf("read backup dir: %w", err)
	}

	// Filter and sort backup files
	var backups []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "snapshot_") && strings.HasSuffix(entry.Name(), ".json") {
			backups = append(backups, entry)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		infoI, _ := backups[i].Info()
		infoJ, _ := backups[j].Info()
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Remove excess backups
	if len(backups) > MaxBackups {
		for _, backup := range backups[MaxBackups:] {
			path := filepath.Join(s.backupDir, backup.Name())
			if err := os.Remove(path); err != nil {
				fmt.Fprintf(os.Stderr, "[memoryshot] failed to remove old backup %s: %v\n", path, err)
			}
		}
	}

	return nil
}

// ListBackups returns a list of available backup files.
func (s *Store) ListBackups() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("memoryshot: read backup dir: %w", err)
	}

	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "snapshot_") && strings.HasSuffix(entry.Name(), ".json") {
			backups = append(backups, entry.Name())
		}
	}

	return backups, nil
}

// LoadBackup loads a specific backup by filename.
func (s *Store) LoadBackup(filename string) (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Security: prevent directory traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return nil, fmt.Errorf("memoryshot: invalid backup filename")
	}

	path := filepath.Join(s.backupDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("memoryshot: read backup: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("memoryshot: unmarshal backup: %w", err)
	}

	return &snapshot, nil
}

// DefaultDataDir returns the default data directory path.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".maclaw/data"
	}
	return filepath.Join(home, ".maclaw", "data")
}

// DefaultStore creates a Store at the default location (~/.maclaw/data/memoryshot).
func DefaultStore() (*Store, error) {
	dataDir := DefaultDataDir()
	return NewStore(filepath.Join(dataDir, "memoryshot"))
}
