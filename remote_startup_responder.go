package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// startupAutoResponder watches the first N seconds of a PTY session's
// output for interactive startup prompts (theme selection, confirmation
// dialogs, etc.) and automatically sends the appropriate keystrokes so
// the user doesn't have to deal with them manually.
//
// It is designed to be called from runOutputLoop for every raw-output
// chunk.  Once the startup window expires or the session enters normal
// interactive mode, the responder becomes a no-op.
type startupAutoResponder struct {
	mu        sync.Mutex
	session   *RemoteSession
	app       *App
	startedAt time.Time

	// How long after session creation we keep trying to auto-respond.
	window time.Duration

	// Track what we've already sent so we don't repeat.
	sentKeys map[string]bool

	// Once we detect the session is past startup, stop checking.
	done bool

	// Accumulated text from raw output for pattern matching.
	// Capped to prevent unbounded growth during the startup window.
	accum strings.Builder
	accumLen int
}

func newStartupAutoResponder(app *App, session *RemoteSession) *startupAutoResponder {
	return &startupAutoResponder{
		session:   session,
		app:       app,
		startedAt: session.CreatedAt,
		window:    30 * time.Second,
		sentKeys:  map[string]bool{},
		// Disabled by default — the onboarding pre-check in
		// ensureClaudeOnboardingComplete should handle startup prompts.
		// Enable only if the pre-check is insufficient for some edge case.
		done: true,
	}
}

// feed is called with each batch of raw output lines.  It checks for
// known startup prompts and sends the appropriate response.
func (r *startupAutoResponder) feed(rawLines []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.done {
		return
	}

	// Past the startup window — stop checking.
	if time.Since(r.startedAt) > r.window {
		r.done = true
		return
	}

	for _, line := range rawLines {
		lower := strings.ToLower(line)
		// Cap accumulator at 8KB to prevent unbounded growth
		if r.accumLen+len(lower)+1 <= 8192 {
			r.accum.WriteString(lower)
			r.accum.WriteString(" ")
			r.accumLen += len(lower) + 1
		}
	}

	accumulated := r.accum.String()

	// Check each known startup pattern and respond.
	for _, pattern := range startupPatterns {
		if r.sentKeys[pattern.id] {
			continue
		}
		if pattern.match(accumulated) {
			r.sentKeys[pattern.id] = true
			r.app.log(fmt.Sprintf("[startup-responder] session=%s, matched=%q, sending=%q",
				r.session.ID, pattern.id, pattern.response))
			go r.sendResponse(pattern.response, pattern.delay)
		}
	}

	// If we see signs that the session is in normal interactive mode,
	// mark as done.
	if r.detectNormalMode(accumulated) {
		r.done = true
		r.app.log(fmt.Sprintf("[startup-responder] session=%s, detected normal mode, stopping", r.session.ID))
	}
}

func (r *startupAutoResponder) sendResponse(keys string, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
	r.mu.Lock()
	exec := r.session.Exec
	r.mu.Unlock()
	if exec == nil {
		return
	}
	// Send each character individually for TUI compatibility.
	for _, ch := range keys {
		if err := exec.Write([]byte(string(ch))); err != nil {
			r.app.log(fmt.Sprintf("[startup-responder] write error session=%s: %v", r.session.ID, err))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *startupAutoResponder) detectNormalMode(accumulated string) bool {
	// Claude Code shows a prompt like ">" or "❯" when ready for input.
	// Also look for typical "ready" indicators.
	normalIndicators := []string{
		"type a message",
		"send a message",
		"what can i help",
		"how can i help",
		"what would you like",
		"enter your prompt",
		"claude >",
		"tips:",
	}
	for _, indicator := range normalIndicators {
		if strings.Contains(accumulated, indicator) {
			return true
		}
	}
	return false
}

// startupPattern defines a pattern to match in startup output and the
// response to send.
type startupPattern struct {
	id       string
	match    func(accumulated string) bool
	response string        // keys to send (each char sent individually)
	delay    time.Duration // wait before sending
}

// startupPatterns is the list of known Claude Code startup prompts.
// Order matters — earlier patterns are checked first.
var startupPatterns = []startupPattern{
	{
		// Claude Code theme selection: "Choose a theme (1-6)" or similar.
		// The numbers 1-6 typically appear with descriptions like
		// "1. Dark", "2. Light", etc.
		id: "theme-selection",
		match: func(acc string) bool {
			// Look for theme-related keywords near numbered options
			hasTheme := strings.Contains(acc, "theme") ||
				strings.Contains(acc, "color scheme") ||
				strings.Contains(acc, "appearance")
			hasNumbers := strings.Contains(acc, "1.") || strings.Contains(acc, "1)")
			return hasTheme && hasNumbers
		},
		response: "1\r", // Select first theme (dark) and press Enter
		delay:    500 * time.Millisecond,
	},
	{
		// Numbered menu during startup (e.g. "1. ...", "2. ...", "3. ...")
		// that doesn't match a more specific pattern above.
		// Claude Code sometimes shows numbered options for initial config.
		id: "numbered-menu",
		match: func(acc string) bool {
			// Must have at least 3 numbered items to look like a menu
			has1 := strings.Contains(acc, "1.") || strings.Contains(acc, "1)")
			has2 := strings.Contains(acc, "2.") || strings.Contains(acc, "2)")
			has3 := strings.Contains(acc, "3.") || strings.Contains(acc, "3)")
			// Exclude false positives: version numbers, IP addresses, etc.
			// by requiring the numbers to appear in a startup-like context
			isStartup := strings.Contains(acc, "select") ||
				strings.Contains(acc, "choose") ||
				strings.Contains(acc, "pick") ||
				strings.Contains(acc, "option") ||
				strings.Contains(acc, "preference")
			return has1 && has2 && has3 && isStartup
		},
		response: "1\r", // Select first option
		delay:    500 * time.Millisecond,
	},
	{
		// "Do you want to trust this project?" or similar trust prompt.
		id: "trust-project",
		match: func(acc string) bool {
			return (strings.Contains(acc, "trust") && strings.Contains(acc, "project")) ||
				(strings.Contains(acc, "trust") && strings.Contains(acc, "folder"))
		},
		response: "y\r",
		delay:    500 * time.Millisecond,
	},
	{
		// "Yes/No" or "y/n" confirmation during startup.
		// Only match if it appears early and looks like a startup prompt.
		id: "startup-confirm-yn",
		match: func(acc string) bool {
			// Must have a yes/no prompt AND be a startup-like context
			hasYN := strings.Contains(acc, "(y/n)") || strings.Contains(acc, "[y/n]")
			isStartup := strings.Contains(acc, "welcome") ||
				strings.Contains(acc, "setup") ||
				strings.Contains(acc, "first time") ||
				strings.Contains(acc, "getting started") ||
				strings.Contains(acc, "onboarding")
			return hasYN && isStartup
		},
		response: "y\r",
		delay:    500 * time.Millisecond,
	},
	{
		// "Press Enter to continue" or "Press any key"
		id: "press-enter",
		match: func(acc string) bool {
			return strings.Contains(acc, "press enter") ||
				strings.Contains(acc, "press any key") ||
				strings.Contains(acc, "hit enter")
		},
		response: "\r",
		delay:    300 * time.Millisecond,
	},
	{
		// Login/auth prompt — send Enter to use default/existing auth
		id: "login-prompt",
		match: func(acc string) bool {
			return (strings.Contains(acc, "log in") || strings.Contains(acc, "sign in") || strings.Contains(acc, "login")) &&
				(strings.Contains(acc, "anthropic") || strings.Contains(acc, "claude"))
		},
		response: "\r",
		delay:    500 * time.Millisecond,
	},
}
