package corelib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/tool"
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

func init() {
	// Wire up BaseDirFunc so corelib/tool can resolve log paths through
	// MaclawBaseDir() without importing the parent corelib package.
	tool.BaseDirFunc.Store(func() string { return MaclawBaseDir() })
}

// WorkspaceDir returns <MaclawBaseDir>/workspace — the built-in default working
// directory for agent tasks (bash, craft_tool, etc.). The directory is created
// on first call and the result is cached for subsequent calls.
//
// For the user-configurable working directory, use EffectiveWorkspaceDir().
func WorkspaceDir() string {
	workspaceDirOnce.Do(func() {
		workspaceDirPath = filepath.Join(MaclawBaseDir(), "workspace")
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
	fields := loadConfigFields()
	return fields.WorkingDirectory
}

// loadDataDirFromConfig reads the data_dir field from ~/.maclaw/config.json.
// Returns empty string on any error or if not set.
func loadDataDirFromConfig() string {
	fields := loadConfigFields()
	return fields.DataDir
}

// configFields holds the subset of config.json fields needed by paths.go.
type configFields struct {
	WorkingDirectory string
	DataDir          string
}

// loadConfigFields reads working_directory and data_dir from ~/.maclaw/config.json
// in a single file read. Returns zero struct on any error.
func loadConfigFields() configFields {
	home, err := os.UserHomeDir()
	if err != nil {
		return configFields{}
	}
	data, err := os.ReadFile(filepath.Join(home, ".maclaw", "config.json"))
	if err != nil {
		return configFields{}
	}
	var partial struct {
		WorkingDirectory string `json:"working_directory"`
		DataDir          string `json:"data_dir"`
	}
	if json.Unmarshal(data, &partial) != nil {
		return configFields{}
	}
	return configFields{
		WorkingDirectory: strings.TrimSpace(partial.WorkingDirectory),
		DataDir:          strings.TrimSpace(partial.DataDir),
	}
}

var (
	dataDirOnce    sync.Once
	dataDirPath    string
	dataDirMu      sync.RWMutex
	defaultBaseDir string
	defaultBaseMu  sync.Once
)

// MaclawDefaultBaseDir returns ~/.maclaw — the default base directory.
// The result is cached after first call.
func MaclawDefaultBaseDir() string {
	defaultBaseMu.Do(func() {
		home, _ := os.UserHomeDir()
		defaultBaseDir = filepath.Join(home, ".maclaw")
	})
	return defaultBaseDir
}

// MaclawBaseDir returns the effective maclaw base directory for all data
// (memories, logs, skills, conversations, etc.). If the user configured a
// custom data_dir in config.json, that path is returned. Otherwise returns
// ~/.maclaw. The result is cached after first call.
//
// If the configured data_dir is not accessible (e.g. removable disk unplugged,
// network path unavailable), falls back to ~/.maclaw and logs a warning.
//
// config.json always stays at ~/.maclaw/config.json regardless of this value.
//
// ALL code that needs a path under the maclaw data directory MUST use this
// function as the single source of truth. Do NOT hardcode ~/.maclaw paths.
func MaclawBaseDir() string {
	dataDirOnce.Do(func() {
		dir := loadDataDirFromConfig()
		if dir != "" {
			// Verify the configured path is accessible.
			if err := os.MkdirAll(dir, 0o755); err != nil {
				// Path not accessible — fall back to default.
				// This handles removable disks, network paths, permission issues.
				fmt.Fprintf(os.Stderr, "[MaclawBaseDir] WARNING: configured data_dir %q not accessible (%v), falling back to default\n", dir, err)
				dir = ""
			}
		}
		if dir == "" {
			dir = MaclawDefaultBaseDir()
		}
		dataDirMu.Lock()
		dataDirPath = dir
		dataDirMu.Unlock()
	})
	dataDirMu.RLock()
	defer dataDirMu.RUnlock()
	return dataDirPath
}

// SetMaclawBaseDir overrides the cached base dir at runtime (for tests).
func SetMaclawBaseDir(dir string) {
	dataDirOnce.Do(func() {}) // mark as done
	dataDirMu.Lock()
	dataDirPath = dir
	dataDirMu.Unlock()
}

// MaclawLogsDir returns the logs directory under the effective base dir.
// This is a convenience function for corelib packages that need log paths.
func MaclawLogsDir() string {
	return filepath.Join(MaclawBaseDir(), "logs")
}
