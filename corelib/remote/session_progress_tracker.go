package remote

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// ProgressStep represents a single tool action step detected from output.
type ProgressStep struct {
	StepNumber int    // 1-based step counter
	Action     string // e.g. "Write", "Bash", "Read", "TodoWrite"
	Target     string // e.g. file path or command
}

// SessionProgress holds per-session progress tracking state.
type SessionProgress struct {
	StepCount   int            // total steps so far
	CurrentStep *ProgressStep  // most recent step (nil if none)
	Steps       []ProgressStep // recent steps (capped at maxRecentSteps)
}

// ProgressTracker extracts tool_use steps from output and maintains
// per-session step counters. Thread-safe.
type ProgressTracker struct {
	mu       sync.Mutex
	sessions map[string]*SessionProgress
}

// NewProgressTracker creates a new ProgressTracker.
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		sessions: make(map[string]*SessionProgress),
	}
}

// toolActionIcons maps tool names to display icons.
var toolActionIcons = map[string]string{
	"Write": "📝", "Edit": "✏️", "MultiEdit": "✏️",
	"Read": "📖", "Bash": "⚡", "TodoWrite": "📝",
	"TodoRead": "📖", "ListDir": "📂", "Grep": "🔍",
	"Glob": "🔍", "WebSearch": "🌐", "WebFetch": "🌐",
	"AskUserQuestion": "❓", "Task": "📋",
}

// knownToolNames is the set of tool names we track as steps.
var knownToolNames = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true,
	"Read": true, "Bash": true, "TodoWrite": true,
	"TodoRead": true, "ListDir": true, "Grep": true,
	"Glob": true, "WebSearch": true, "WebFetch": true,
	"AskUserQuestion": true, "Task": true,
}

// ptyToolLinePattern matches Claude Code PTY output lines that indicate
// a tool action. The tool name is captured in group 1, the target in group 2.
// Examples:
//
//	✏️  Write src/main.go
//	⚡ Bash npm run build
//	► Read package.json
var ptyToolLinePattern = regexp.MustCompile(
	`(?:^|\s)(Write|Edit|MultiEdit|Read|Bash|TodoWrite|TodoRead|ListDir|Grep|Glob|WebSearch|WebFetch|Task)\s+(.+)$`,
)

// RecordSDKToolUse records a tool_use event from SDK structured messages.
func (t *ProgressTracker) RecordSDKToolUse(sessionID, toolName string, input interface{}) {
	if !knownToolNames[toolName] {
		return
	}
	target := extractToolTarget(toolName, input)
	t.recordStep(sessionID, toolName, target)
}

// ConsumeLine checks a raw output line for tool action patterns (PTY mode).
// Returns true if a step was detected.
func (t *ProgressTracker) ConsumeLine(sessionID, line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	m := ptyToolLinePattern.FindStringSubmatch(trimmed)
	if len(m) < 3 {
		return false
	}
	t.recordStep(sessionID, m[1], strings.TrimSpace(m[2]))
	return true
}

// GetProgress returns a snapshot of the current progress for a session.
// Returns nil if no steps have been recorded.
func (t *ProgressTracker) GetProgress(sessionID string) *SessionProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.sessions[sessionID]
	if !ok {
		return nil
	}
	// Deep copy to avoid sharing mutable state.
	cp := SessionProgress{
		StepCount: p.StepCount,
		Steps:     make([]ProgressStep, len(p.Steps)),
	}
	copy(cp.Steps, p.Steps)
	if p.CurrentStep != nil {
		s := *p.CurrentStep
		cp.CurrentStep = &s
	}
	return &cp
}

const maxTargetRunes = 60

// FormatProgress returns a human-readable progress string for a session.
// Returns empty string if no steps recorded.
func (t *ProgressTracker) FormatProgress(sessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.sessions[sessionID]
	if !ok || p.StepCount == 0 || p.CurrentStep == nil {
		return ""
	}
	step := p.CurrentStep
	icon := toolActionIcons[step.Action]
	if icon == "" {
		icon = "🔧"
	}
	target := truncateTarget(step.Target, maxTargetRunes)
	if target != "" {
		return fmt.Sprintf("%s 第 %d 步: %s %s", icon, step.StepNumber, step.Action, target)
	}
	return fmt.Sprintf("%s 第 %d 步: %s", icon, step.StepNumber, step.Action)
}

// Reset clears progress for a session. Call when a session exits.
func (t *ProgressTracker) Reset(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, sessionID)
}

// Close clears all tracked sessions.
func (t *ProgressTracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions = make(map[string]*SessionProgress)
}

const maxRecentSteps = 20

func (t *ProgressTracker) recordStep(sessionID, action, target string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, ok := t.sessions[sessionID]
	if !ok {
		p = &SessionProgress{}
		t.sessions[sessionID] = p
	}
	p.StepCount++
	step := ProgressStep{
		StepNumber: p.StepCount,
		Action:     action,
		Target:     target,
	}
	p.CurrentStep = &step
	p.Steps = append(p.Steps, step)
	if len(p.Steps) > maxRecentSteps {
		p.Steps = p.Steps[len(p.Steps)-maxRecentSteps:]
	}
}

// truncateTarget shortens a target string to maxRunes, using rune count
// for correct handling of multi-byte characters (e.g. Chinese paths).
func truncateTarget(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return "..." + string(runes[len(runes)-(maxRunes-3):])
}

func extractToolTarget(toolName string, input interface{}) string {
	m, ok := input.(map[string]interface{})
	if !ok {
		return ""
	}
	switch toolName {
	case "Write", "Edit", "MultiEdit", "Read", "TodoWrite", "TodoRead":
		if fp, ok := m["file_path"].(string); ok {
			return fp
		}
		if fp, ok := m["path"].(string); ok {
			return fp
		}
	case "Bash":
		if cmd, ok := m["command"].(string); ok {
			if utf8.RuneCountInString(cmd) > 80 {
				return string([]rune(cmd)[:77]) + "..."
			}
			return cmd
		}
	case "ListDir", "Glob", "Grep":
		if p, ok := m["path"].(string); ok {
			return p
		}
		if p, ok := m["pattern"].(string); ok {
			return p
		}
	}
	return ""
}
