package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// maxSnapshotFileSize is the maximum file size (2MB) for snapshot capture.
	maxSnapshotFileSize = 2 * 1024 * 1024

	// defaultMaxSnapshotFiles is the default cap on files per task.
	defaultMaxSnapshotFiles = 50
)

// fileSnapshot holds pre-modification content for a single file.
type fileSnapshot struct {
	Content    string    // UTF-8 file content (empty if capture failed)
	Error      string    // non-empty if capture failed: "permission_denied", "file_too_large", "not_found", "binary"
	CapturedAt time.Time // when the snapshot was taken
}

// FileSnapshotStore holds pre-modification file content for a single task execution.
// It is safe for concurrent access.
type FileSnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]fileSnapshot // keyed by absolute path
	maxFiles  int                     // cap at defaultMaxSnapshotFiles per task
}

// NewFileSnapshotStore creates a new snapshot store with the given file cap.
// If maxFiles <= 0, defaultMaxSnapshotFiles is used.
func NewFileSnapshotStore(maxFiles int) *FileSnapshotStore {
	if maxFiles <= 0 {
		maxFiles = defaultMaxSnapshotFiles
	}
	return &FileSnapshotStore{
		snapshots: make(map[string]fileSnapshot),
		maxFiles:  maxFiles,
	}
}

// CaptureSnapshots reads files from disk and stores their content.
// Each file path is resolved relative to projectPath to get an absolute path.
// Skips files > 2MB, binary files, and files that cannot be read.
// Limited to maxFiles entries.
func (s *FileSnapshotStore) CaptureSnapshots(projectPath string, filePaths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, fp := range filePaths {
		// Stop if we've reached the cap.
		if len(s.snapshots) >= s.maxFiles {
			return
		}

		// Resolve to absolute path.
		absPath := fp
		if !filepath.IsAbs(fp) {
			absPath = filepath.Join(projectPath, fp)
		}
		absPath = filepath.Clean(absPath)

		// Skip if already captured.
		if _, exists := s.snapshots[absPath]; exists {
			continue
		}

		snap := s.captureFile(absPath)
		s.snapshots[absPath] = snap
	}
}

// captureFile reads a single file and returns a snapshot.
// Must be called with s.mu held.
func (s *FileSnapshotStore) captureFile(absPath string) fileSnapshot {
	now := time.Now()

	// Stat the file first to check size.
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fileSnapshot{Error: "not_found", CapturedAt: now}
		}
		if errors.Is(err, fs.ErrPermission) {
			return fileSnapshot{Error: "permission_denied", CapturedAt: now}
		}
		// Other stat errors — treat as not found.
		return fileSnapshot{Error: "not_found", CapturedAt: now}
	}

	// Check file size limit.
	if info.Size() > maxSnapshotFileSize {
		return fileSnapshot{Error: "file_too_large", CapturedAt: now}
	}

	// Read file content.
	content, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fileSnapshot{Error: "permission_denied", CapturedAt: now}
		}
		return fileSnapshot{Error: "not_found", CapturedAt: now}
	}

	// Check for binary content.
	if IsBinaryFile(content) {
		return fileSnapshot{Error: "binary", CapturedAt: now}
	}

	return fileSnapshot{
		Content:    string(content),
		CapturedAt: now,
	}
}

// GetSnapshot returns the pre-modification content for a file path.
// The path should be an absolute path.
func (s *FileSnapshotStore) GetSnapshot(absPath string) (fileSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap, ok := s.snapshots[absPath]
	return snap, ok
}

// Clear resets the store for the next task.
func (s *FileSnapshotStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshots = make(map[string]fileSnapshot)
}

// Len returns the number of snapshots currently stored.
func (s *FileSnapshotStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.snapshots)
}
