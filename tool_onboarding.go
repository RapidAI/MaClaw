package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	case "gemini":
		err = ensureGeminiOnboardingComplete(app)
	case "codebuddy":
		err = ensureCodeBuddyOnboardingComplete(app, projectPath)
	default:
		// Other tools (codex, iflow, kilo, opencode, cursor) don't have
		// known first-run wizards that need pre-configuration.
		return
	}
	if err != nil && app != nil {
		app.log(fmt.Sprintf("[tool-onboarding] %s pre-check warning: %v", toolName, err))
	}
}

// ensureGeminiOnboardingComplete ensures that Gemini CLI's user-level
// settings file (~/.gemini/settings.json) contains a theme setting so
// the first-run theme selection prompt is skipped.
//
// Gemini CLI shows an interactive theme picker on first launch if no
// theme is configured.  Pre-setting a theme avoids this.
func ensureGeminiOnboardingComplete(app *App) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".gemini")
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if app != nil {
				app.log(fmt.Sprintf("[gemini-onboarding] backed up corrupt %s to %s", configPath, backupPath))
			}
			existing = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	changed := false

	// Ensure ui.theme is set to skip the theme selection prompt.
	ui, _ := existing["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
		existing["ui"] = ui
	}
	if ui["theme"] == nil || strings.TrimSpace(fmt.Sprint(ui["theme"])) == "" {
		ui["theme"] = "Default Dark"
		changed = true
	}

	// Disable auto theme switching — it polls terminal background color
	// which can cause repeated redraws in ConPTY environments.
	if ui["autoThemeSwitching"] == nil {
		ui["autoThemeSwitching"] = false
		changed = true
	}

	// Hide tips and shortcuts hints to reduce TUI noise.
	if ui["hideTips"] == nil {
		ui["hideTips"] = true
		changed = true
	}
	if ui["showShortcutsHint"] == nil {
		ui["showShortcutsHint"] = false
		changed = true
	}

	// Disable dynamic window title updates that can cause extra output.
	if ui["dynamicWindowTitle"] == nil {
		ui["dynamicWindowTitle"] = false
		changed = true
	}
	if ui["showStatusInTitle"] == nil {
		ui["showStatusInTitle"] = false
		changed = true
	}
	if ui["hideWindowTitle"] == nil {
		ui["hideWindowTitle"] = true
		changed = true
	}

	// Disable compatibility warnings that may trigger interactive prompts.
	if ui["showCompatibilityWarnings"] == nil {
		ui["showCompatibilityWarnings"] = false
		changed = true
	}

	// Disable home directory warning.
	if ui["showHomeDirectoryWarning"] == nil {
		ui["showHomeDirectoryWarning"] = false
		changed = true
	}

	// Pre-select auth type to prevent the interactive auth selection prompt
	// from blocking the ACP process.  When GEMINI_API_KEY is set, use
	// "gemini-api-key"; otherwise default to "oauth-personal".
	if existing["selectedAuthType"] == nil || strings.TrimSpace(fmt.Sprint(existing["selectedAuthType"])) == "" {
		if os.Getenv("GEMINI_API_KEY") != "" {
			existing["selectedAuthType"] = "gemini-api-key"
		} else {
			existing["selectedAuthType"] = "oauth-personal"
		}
		changed = true
	}

	if !changed {
		if app != nil {
			app.log("[gemini-onboarding] settings already complete, no changes needed")
		}
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if app != nil {
		app.log(fmt.Sprintf("[gemini-onboarding] updated %s with theme setting", configPath))
	}
	return nil
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
	return ensureClaudeCodeForkOnboarding(app, ".codebuddy.json", "codebuddy", projectPath)
}

// ensureClaudeCodeForkOnboarding is the shared implementation for Claude Code
// forks (CodeBuddy, etc.) that use the same ~/.{tool}.json config
// format with hasCompletedOnboarding, theme, and project trust entries.
//
// configFileName is the basename of the config file (e.g. ".kode.json").
// logTag is used for log messages (e.g. "kode", "codebuddy").
func ensureClaudeCodeForkOnboarding(app *App, configFileName, logTag, projectPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	configPath := filepath.Join(home, configFileName)

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if app != nil {
				app.log(fmt.Sprintf("[%s-onboarding] backed up corrupt %s to %s", logTag, configPath, backupPath))
			}
			existing = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	changed := false

	if !isTruthy(existing["hasCompletedOnboarding"]) {
		existing["hasCompletedOnboarding"] = true
		changed = true
	}

	if existing["theme"] == nil || strings.TrimSpace(fmt.Sprint(existing["theme"])) == "" {
		existing["theme"] = "dark"
		changed = true
	}

	if projectPath != "" {
		if ensureProjectTrust(existing, projectPath) {
			changed = true
		}
	}

	if !changed {
		if app != nil {
			app.log(fmt.Sprintf("[%s-onboarding] config already complete, no changes needed", logTag))
		}
		return nil
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if app != nil {
		app.log(fmt.Sprintf("[%s-onboarding] updated %s with onboarding flags", logTag, configPath))
	}
	return nil
}

// backupSuffix is the extension appended to config files when backing up
// the user's original configuration before onboarding modifications.
// Using ".cceasy.bak" to avoid collision with the ".bak" suffix used
// for corrupt-file recovery in ensureClaudeCodeForkOnboarding.
const backupSuffix = ".cceasy.bak"

// toolConfigFiles maps tool names to their config file basenames (relative
// to the user's home directory).  This is the single source of truth used
// by both toolConfigPaths and the onboarding functions.
var toolConfigFiles = map[string][]string{
	"claude":    {".claude.json"},
	"codebuddy": {".codebuddy.json"},
	"gemini":    {filepath.Join(".gemini", "settings.json")},
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

// backupToolConfigs creates backup copies of the tool's config files before
// onboarding modifies them.  It returns a restore function that copies the
// backups back, removing the backup files afterward.
//
// If a config file does not exist yet (first-run), no backup is created and
// the restore function will remove the file that onboarding created, leaving
// the system in its original state.
//
// This ensures the user's native tool configuration (auth tokens, theme
// preferences, etc.) is not permanently altered by our onboarding changes.
func backupToolConfigs(app *App, toolName string) func() {
	paths := toolConfigPaths(toolName)
	if len(paths) == 0 {
		return func() {}
	}

	type snapshot struct {
		path    string
		data    []byte // nil means file did not exist
		existed bool
	}

	var snaps []snapshot
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			// File doesn't exist or unreadable — record as non-existent.
			snaps = append(snaps, snapshot{path: p, existed: false})
		} else {
			snaps = append(snaps, snapshot{path: p, data: data, existed: true})
		}
	}

	return func() {
		for _, s := range snaps {
			if s.existed {
				// Restore original content.
				if err := os.WriteFile(s.path, s.data, 0o644); err != nil {
					if app != nil {
						app.log(fmt.Sprintf("[tool-onboarding] restore %s failed: %v", s.path, err))
					}
				} else if app != nil {
					app.log(fmt.Sprintf("[tool-onboarding] restored original %s", s.path))
				}
			} else {
				// File didn't exist before — remove the one onboarding created.
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
