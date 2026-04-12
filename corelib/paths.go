package corelib

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	workspaceDirOnce sync.Once
	workspaceDirPath string
)

// WorkspaceDir returns ~/.maclaw/workspace — the default temporary working
// directory for agent tasks (bash, craft_tool, etc.). The directory is created
// on first call and the result is cached for subsequent calls.
func WorkspaceDir() string {
	workspaceDirOnce.Do(func() {
		home, _ := os.UserHomeDir()
		workspaceDirPath = filepath.Join(home, ".maclaw", "workspace")
		_ = os.MkdirAll(workspaceDirPath, 0o755)
	})
	return workspaceDirPath
}
