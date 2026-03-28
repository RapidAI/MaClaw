// Package configfile provides atomic file write utilities for tool configuration files.

package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ClaudeOnboardingOptions controls which flags are written to ~/.claude.json.
type ClaudeOnboardingOptions struct {
	// ConfigFileName is the basename of the config file (e.g. ".claude.json").
	ConfigFileName string
	// LogTag is used for log messages (e.g. "claude", "codebuddy").
	LogTag string
	// ProjectPath, if non-empty, adds a trust entry for this project.
	ProjectPath string
	// ApiKey, if non-empty, is added to customApiKeyResponses.approved.
	ApiKey string
}

// EnsureClaudeOnboarding ensures that a Claude Code (or fork) user-level
// config file contains the flags that mark onboarding as finished, including
// the bypass-permissions TOS acceptance.
//
// The function is idempotent — it only adds missing keys and never removes
// existing user preferences.
//
// logFn is an optional callback for diagnostic messages; pass nil to suppress.
func EnsureClaudeOnboarding(opts ClaudeOnboardingOptions, logFn func(string)) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	configPath := filepath.Join(home, opts.ConfigFileName)
	tag := opts.LogTag
	if tag == "" {
		tag = "claude"
	}

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if logFn != nil {
				logFn(fmt.Sprintf("[%s-onboarding] backed up corrupt %s to %s", tag, configPath, backupPath))
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

	// Accept the bypass-permissions TOS so that --dangerously-skip-permissions
	// does not show an interactive confirmation dialog on every launch.
	// Claude Code stores this as "bypassPermissionsModeAccepted" in the
	// user-level config file.
	if !isTruthy(existing["bypassPermissionsModeAccepted"]) {
		existing["bypassPermissionsModeAccepted"] = true
		changed = true
	}

	if existing["theme"] == nil || strings.TrimSpace(fmt.Sprint(existing["theme"])) == "" {
		existing["theme"] = "dark"
		changed = true
	}

	if opts.ProjectPath != "" {
		if EnsureProjectTrust(existing, opts.ProjectPath) {
			changed = true
		}
	}

	if opts.ApiKey != "" {
		if EnsureCustomApiKeyApproved(existing, opts.ApiKey) {
			changed = true
		}
	}

	if !changed {
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] config already complete, no changes needed", tag))
		}
	} else {
		out, err := json.MarshalIndent(existing, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}

		if err := AtomicWrite(configPath, out); err != nil {
			return fmt.Errorf("write %s: %w", configPath, err)
		}

		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] updated %s with onboarding flags", tag, configPath))
		}
	}

	// Install the anti-premature-exit stop hook for Claude Code.
	// This hook intercepts Claude Code's exit attempts and checks whether
	// a todo list exists with incomplete items, preventing premature exit
	// on complex tasks. Inspired by the Ralph architecture.
	if err := ensureClaudeStopHook(home, tag, logFn); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] stop hook install warning: %v", tag, err))
		}
		// Non-fatal: don't block session creation if hook install fails.
	}

	// Install the security gateway hooks (PreToolUse + PostToolUse).
	if err := EnsureClaudeSecurityHook(home, "", tag, logFn); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] security hook install warning: %v", tag, err))
		}
		// Non-fatal: don't block session creation if hook install fails.
	}

	return nil
}

// EnsureProjectTrust adds a trust entry for the given project path in
// the "projects" map of a Claude Code config.  Returns true if the config
// was modified.
func EnsureProjectTrust(config map[string]any, projectPath string) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(projectPath))

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
		config["projects"] = projects
	}

	for key, val := range projects {
		normalizedKey := filepath.ToSlash(filepath.Clean(key))
		if normalizedKey == normalizedPath {
			entry, ok := val.(map[string]any)
			if ok && isTruthy(entry["hasTrustDialogAccepted"]) {
				return false
			}
			if entry == nil {
				entry = map[string]any{}
			}
			entry["hasTrustDialogAccepted"] = true
			projects[key] = entry
			return true
		}
	}

	projects[normalizedPath] = map[string]any{
		"allowedTools":           []any{},
		"hasTrustDialogAccepted": true,
	}
	return true
}

// EnsureCustomApiKeyApproved adds the given API key to the
// customApiKeyResponses.approved list.  Returns true if modified.
func EnsureCustomApiKeyApproved(config map[string]any, apiKey string) bool {
	if apiKey == "" {
		return false
	}

	responses, _ := config["customApiKeyResponses"].(map[string]any)
	if responses == nil {
		responses = map[string]any{}
		config["customApiKeyResponses"] = responses
	}

	approved, _ := responses["approved"].([]any)
	for _, v := range approved {
		if s, ok := v.(string); ok && s == apiKey {
			return false
		}
	}

	approved = append(approved, apiKey)
	responses["approved"] = approved

	if responses["rejected"] == nil {
		responses["rejected"] = []any{}
	}

	return true
}

// isTruthy checks if a JSON value is boolean true or the string "true".
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	default:
		return false
	}
}

// ensureClaudeStopHook installs a stop hook in ~/.claude/hooks/ that
// prevents Claude Code from exiting prematurely when tasks are incomplete.
//
// The hook checks for a TODO.md or todo.md file in the working directory.
// If incomplete items (lines starting with "- [ ]") exist, the hook
// returns a non-zero exit code, which tells Claude Code to continue
// working instead of exiting.
//
// This is inspired by the "Ralph" architecture for autonomous agent loops.
// The hook is idempotent — it only writes if the file doesn't exist or
// has been modified by the user (checked via a marker comment).
func ensureClaudeStopHook(home, tag string, logFn func(string)) error {
	hooksDir := filepath.Join(home, ".claude", "hooks")

	// Build the hook content once — platform-specific shell command.
	var checkCmd string
	if runtime.GOOS == "windows" {
		checkCmd = `powershell -NoProfile -Command "if ((Test-Path TODO.md) -and (Select-String -Path TODO.md -Pattern '- \[ \]' -Quiet)) { Write-Output 'Incomplete tasks in TODO.md. Complete all tasks before exiting.'; exit 1 } if ((Test-Path todo.md) -and (Select-String -Path todo.md -Pattern '- \[ \]' -Quiet)) { Write-Output 'Incomplete tasks in todo.md. Complete all tasks before exiting.'; exit 1 } exit 0"`
	} else {
		checkCmd = `sh -c 'if [ -f TODO.md ] && grep -q "\\- \\[ \\]" TODO.md 2>/dev/null; then echo "Incomplete tasks in TODO.md. Complete all tasks before exiting." && exit 1; fi; if [ -f todo.md ] && grep -q "\\- \\[ \\]" todo.md 2>/dev/null; then echo "Incomplete tasks in todo.md. Complete all tasks before exiting." && exit 1; fi; exit 0'`
	}

	hookContent := fmt.Sprintf(`{
  "_comment": "maclaw-anti-premature-exit: Prevents Claude Code from exiting when tasks are incomplete",
  "hooks": {
    "Stop": [
      {
        "type": "command",
        "command": %s
      }
    ]
  }
}
`, strconv.Quote(checkCmd))

	// Try primary path first, then fallback if user has their own stop.json.
	for _, name := range []string{"stop.json", "maclaw-stop.json"} {
		hookPath := filepath.Join(hooksDir, name)
		if data, err := os.ReadFile(hookPath); err == nil {
			if strings.Contains(string(data), "maclaw-anti-premature-exit") {
				return nil // Already installed
			}
			if name == "stop.json" {
				continue // User's custom hook — try fallback name
			}
		}

		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return fmt.Errorf("create hooks dir: %w", err)
		}
		if err := AtomicWrite(hookPath, []byte(hookContent)); err != nil {
			return fmt.Errorf("write stop hook: %w", err)
		}
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] installed anti-premature-exit stop hook at %s", tag, hookPath))
		}
		return nil
	}

	return nil
}
