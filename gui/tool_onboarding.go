package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// ensureToolOnboardingComplete runs pre-launch onboarding checks for the
// given tool so that first-run interactive prompts (theme selection, trust
// dialogs, safety acknowledgments, etc.) don't block the user every time
// the tool is launched from the app.
//
// Each tool has its own config file and onboarding flags.  This function
// dispatches to the appropriate handler based on the tool name.
//
// The function is idempotent — it only adds missing keys and never
// removes existing user preferences.
func ensureToolOnboardingComplete(app *App, toolName string, projectPath string) {
	var err error
	switch toolName {
	case "claude":
		err = ensureClaudeOnboardingComplete(app, projectPath)
	case "codebuddy":
		err = ensureCodeBuddyOnboardingComplete(app, projectPath)
	default:
		// Other tools (codex, iflow, kilo, opencode) don't have
		// known first-run wizards that need pre-configuration.
		return
	}
	if err != nil && app != nil {
		app.log(fmt.Sprintf("[tool-onboarding] %s pre-check warning: %v", toolName, err))
	}
}

// ensureCodeBuddyOnboardingComplete ensures that CodeBuddy CLI's user-level
// config file (~/.codebuddy.json) has onboarding marked as complete so the
// first-run login method selection prompt and any other interactive wizards
// are skipped.
//
// CodeBuddy (腾讯云代码助手) is a Claude Code fork and shares the same
// onboarding flow: hasCompletedOnboarding flag, theme selection, and
// project trust dialog.
//
// Note: In SDK mode (-p --output-format stream-json) with CODEBUDDY_API_KEY
// set, the login prompt is typically bypassed.  However, pre-setting these
// flags provides defense-in-depth for edge cases where the CLI falls back
// to interactive mode.
func ensureCodeBuddyOnboardingComplete(app *App, projectPath string) error {
	return ensureClaudeCodeForkOnboarding(app, ".codebuddy.json", "codebuddy", projectPath, "")
}

// ensureClaudeCodeForkOnboarding delegates to the shared corelib
// implementation for Claude Code forks (CodeBuddy, etc.) that use the
// same ~/.{tool}.json config format.
func ensureClaudeCodeForkOnboarding(app *App, configFileName, logTag, projectPath string, apiKey string) error {
	logFn := func(msg string) {
		if app != nil {
			app.log(msg)
		}
	}
	return configfile.EnsureClaudeOnboarding(configfile.ClaudeOnboardingOptions{
		ConfigFileName: configFileName,
		LogTag:         logTag,
		ProjectPath:    projectPath,
		ApiKey:         apiKey,
	}, logFn)
}

// ensureCustomApiKeyApproved delegates to the corelib implementation.
func ensureCustomApiKeyApproved(config map[string]any, apiKey string) bool {
	return configfile.EnsureCustomApiKeyApproved(config, apiKey)
}

// toolConfigFiles maps tool names to their config file basenames (relative
// to the user's home directory).  This is the single source of truth used
// by both toolConfigPaths and the onboarding functions.
var toolConfigFiles = map[string][]string{
	"claude":    {".claude.json"},
	"codebuddy": {".codebuddy.json"},
}

// toolConfigPaths returns the absolute config file paths that onboarding
// may modify for the given tool.  Returns nil for tools without onboarding.
func toolConfigPaths(toolName string) []string {
	files, ok := toolConfigFiles[toolName]
	if !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(home, f)
	}
	return paths
}

// ---------------------------------------------------------------------------
// Reference-counted config backup / restore
// ---------------------------------------------------------------------------
//
// Multiple concurrent sessions for the same tool share a single user-level
// config file (e.g. ~/.claude.json).  Without coordination the following
// race occurs:
//
//   Session A: backup(original) → onboarding → start
//   Session B: backup(onboarded) → onboarding → start
//   Session A exits: restore(original) — breaks Session B
//   Session B exits: restore(onboarded) — user's original config lost
//
// We solve this with a per-tool reference counter.  The first session to
// call backupToolConfigs snapshots the pre-onboarding state.  Subsequent
// sessions for the same tool just increment the counter.  Only the last
// session to call its restore function actually writes the original content
// back (or removes the file if it didn't exist before).

// configBackupState holds the shared backup state for one tool.
type configBackupState struct {
	refCount int
	snaps    []configSnapshot
}

type configSnapshot struct {
	path    string
	data    []byte // nil means file did not exist
	existed bool
}

var (
	configBackupMu     sync.Mutex
	configBackupStates = map[string]*configBackupState{} // toolName → state
)

// backupToolConfigs creates backup copies of the tool's config files before
// onboarding modifies them.  It returns a restore function that copies the
// backups back, removing the backup files afterward.
//
// The backup is reference-counted: only the first session for a given tool
// snapshots the files, and only the last session to call restore writes
// them back.  This prevents concurrent sessions from clobbering each
// other's config or losing the user's original settings.
func backupToolConfigs(app *App, toolName string) func() {
	paths := toolConfigPaths(toolName)
	if len(paths) == 0 {
		return func() {}
	}

	configBackupMu.Lock()
	defer configBackupMu.Unlock()

	state, exists := configBackupStates[toolName]
	if exists {
		// Another session already holds the backup — just bump the count.
		state.refCount++
		if app != nil {
			app.log(fmt.Sprintf("[tool-onboarding] backup refcount for %s incremented to %d", toolName, state.refCount))
		}
	} else {
		// First session — snapshot the current (pre-onboarding) state.
		var snaps []configSnapshot
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				snaps = append(snaps, configSnapshot{path: p, existed: false})
			} else {
				snaps = append(snaps, configSnapshot{path: p, data: data, existed: true})
			}
		}
		state = &configBackupState{refCount: 1, snaps: snaps}
		configBackupStates[toolName] = state
		if app != nil {
			app.log(fmt.Sprintf("[tool-onboarding] backup created for %s (refcount=1)", toolName))
		}
	}

	restored := false
	return func() {
		configBackupMu.Lock()
		defer configBackupMu.Unlock()

		if restored {
			return
		}
		restored = true

		st, ok := configBackupStates[toolName]
		if !ok {
			return
		}

		st.refCount--
		if app != nil {
			app.log(fmt.Sprintf("[tool-onboarding] backup refcount for %s decremented to %d", toolName, st.refCount))
		}

		if st.refCount > 0 {
			// Other sessions still running — don't restore yet.
			return
		}

		// Last session — restore original state.
		delete(configBackupStates, toolName)
		for _, s := range st.snaps {
			if s.existed {
				if err := os.WriteFile(s.path, s.data, 0o644); err != nil {
					if app != nil {
						app.log(fmt.Sprintf("[tool-onboarding] restore %s failed: %v", s.path, err))
					}
				} else if app != nil {
					app.log(fmt.Sprintf("[tool-onboarding] restored original %s", s.path))
				}
			} else {
				if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
					if app != nil {
						app.log(fmt.Sprintf("[tool-onboarding] cleanup %s failed: %v", s.path, err))
					}
				} else if app != nil {
					app.log(fmt.Sprintf("[tool-onboarding] removed onboarding-created %s", s.path))
				}
			}
		}
	}
}
