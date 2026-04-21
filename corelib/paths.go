package corelib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	workspaceDirOnce sync.Once
	workspaceDirPath string

	// customWorkspace is the user-configured working directory.
	// Lazily loaded from ~/.maclaw/config.json on first EffectiveWorkspaceDir()
	// call, and updated at runtime via SetWorkspaceDir().
	customWorkspaceMu  sync.RWMutex
	customWorkspaceDir string
	customWorkspaceSet bool         // true after SetWorkspaceDir or lazy init
	lazyLoadOnce       sync.Once    // ensures config file is read at most once
)

// WorkspaceDir returns ~/.maclaw/workspace — the built-in default working
// directory for agent tasks (bash, craft_tool, etc.). The directory is created
// on first call and the result is cached for subsequent calls.
//
// For the user-configurable working directory, use EffectiveWorkspaceDir().
func WorkspaceDir() string {
	workspaceDirOnce.Do(func() {
		home, _ := os.UserHomeDir()
		workspaceDirPath = filepath.Join(home, ".maclaw", "workspace")
		_ = os.MkdirAll(workspaceDirPath, 0o755)
	})
	return workspaceDirPath
}

// SetWorkspaceDir sets the user-configured working directory at runtime.
// Called by GUI/TUI when the user changes the setting via config save.
// Pass empty string to revert to the built-in default.
func SetWorkspaceDir(dir string) {
	dir = strings.TrimSpace(dir)
	customWorkspaceMu.Lock()
	customWorkspaceDir = dir
	customWorkspaceSet = true
	customWorkspaceMu.Unlock()
	// Mark lazy load as done so it won't re-read the config file.
	lazyLoadOnce.Do(func() {})
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
}

// EffectiveWorkspaceDir returns the user-configured working directory if set,
// otherwise falls back to the built-in default (~/.maclaw/workspace).
//
// On first call, it lazily reads working_directory from ~/.maclaw/config.json
// so that all entry points (GUI, TUI, tests) get the correct value without
// requiring explicit SetWorkspaceDir() wiring at startup.
func EffectiveWorkspaceDir() string {
	// Lazy init: read config file at most once. If SetWorkspaceDir was
	// called before this (e.g. GUI startup), lazyLoadOnce is already done.
	lazyLoadOnce.Do(func() {
		dir := loadWorkingDirFromConfig()
		customWorkspaceMu.Lock()
		if !customWorkspaceSet {
			customWorkspaceDir = dir
			customWorkspaceSet = true
		}
		customWorkspaceMu.Unlock()
		if dir != "" {
			_ = os.MkdirAll(dir, 0o755)
		}
	})

	customWorkspaceMu.RLock()
	dir := customWorkspaceDir
	customWorkspaceMu.RUnlock()
	if dir != "" {
		return dir
	}
	return WorkspaceDir()
}

// loadWorkingDirFromConfig reads the working_directory field from
// ~/.maclaw/config.json. Returns empty string on any error.
func loadWorkingDirFromConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".maclaw", "config.json"))
	if err != nil {
		return ""
	}
	var partial struct {
		WorkingDirectory string `json:"working_directory"`
	}
	if json.Unmarshal(data, &partial) != nil {
		return ""
	}
	return strings.TrimSpace(partial.WorkingDirectory)
}
