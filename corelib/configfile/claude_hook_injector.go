package configfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const securityHookMarker = "maclaw-security-gateway"

// EnsureClaudeSecurityHook creates or updates the maclaw-security.json hook
// configuration in ~/.claude/hooks/. The hook defines PreToolUse (security-check)
// and PostToolUse (audit-record) hooks.
//
// The function is idempotent: if the file already contains the marker comment,
// it is left untouched.
//
// If maclawBinary is empty, os.Executable() is used to infer the path,
// falling back to "maclaw-tool" (relying on PATH).
//
// Write failures are logged but do not return an error, to avoid blocking
// the onboarding flow.
func EnsureClaudeSecurityHook(home, maclawBinary, tag string, logFn func(string)) error {
	hooksDir := filepath.Join(home, ".claude", "hooks")
	hookPath := filepath.Join(hooksDir, "maclaw-security.json")

	// Idempotency check: if file exists and contains our marker, skip.
	if data, err := os.ReadFile(hookPath); err == nil {
		if strings.Contains(string(data), securityHookMarker) {
			if logFn != nil {
				logFn(fmt.Sprintf("[%s-onboarding] security hook already installed, skipping", tag))
			}
			return nil
		}
	}

	// Resolve binary path
	if maclawBinary == "" {
		if exe, err := os.Executable(); err == nil {
			maclawBinary = exe
		} else {
			maclawBinary = "maclaw-tool"
			if logFn != nil {
				logFn(fmt.Sprintf("[%s-onboarding] cannot determine executable path, using 'maclaw-tool'", tag))
			}
		}
	}

	// Build hook JSON content
	hookContent := fmt.Sprintf(`{
  "_comment": "%s: Auto-injected by maclaw onboarding",
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": "%s security-check --mode standard"
      }
    ],
    "PostToolUse": [
      {
        "type": "command",
        "command": "%s audit-record"
      }
    ]
  }
}
`, securityHookMarker, escapeJSON(maclawBinary), escapeJSON(maclawBinary))

	// Create hooks directory if needed
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] security hook: cannot create hooks dir: %v", tag, err))
		}
		return nil // non-fatal
	}

	// Write the hook file
	if err := AtomicWrite(hookPath, []byte(hookContent)); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[%s-onboarding] security hook: write failed: %v", tag, err))
		}
		return nil // non-fatal
	}

	if logFn != nil {
		logFn(fmt.Sprintf("[%s-onboarding] installed security hook at %s", tag, hookPath))
	}
	return nil
}

// escapeJSON escapes backslashes and double quotes for embedding in a JSON string value.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
