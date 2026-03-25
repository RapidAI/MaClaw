package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// ProgressStep represents a single tool action step detected from output.
type ProgressStep struct {
	StepNumber int
	Action     string
	Target     string
}

// SessionProgress holds per-session progress tracking state.
type SessionProgress struct {
	StepCount   int
	CurrentStep *ProgressStep
	Steps       []ProgressStep
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

// progressToolIcons maps tool names to display icons.
var progressToolIcons = map[string]string{
	"Write": "📝", "Edit": "✏️", "MultiEdit": "✏️",
	"Read": "📖", "Bash": "⚡", "TodoWrite": "📝",
	"TodoRead": "📖", "ListDir": "📂", "Grep": "🔍",
	"Glob": "🔍", "WebSearch": "🌐", "WebFetch": "🌐",
	"AskUserQuestion": "❓", "Task": "📋",
}

var progressKnownTools = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true,
	"Read": true, "Bash": true, "TodoWrite": true,
	"TodoRead": true, "ListDir": true, "Grep": true,
	"Glob": true, "WebSearch": true, "WebFetch": true,
	"AskUserQuestion": true, "Task": true,
}

var progressPTYPattern = regexp.MustCompile(
	`(?:^|\s)(Write|Edit|MultiEdit|Read|Bash|TodoWrite|TodoRead|ListDir|Grep|Glob|WebSearch|WebFetch|Task)\s+(.+)$`,
)

func (t *ProgressTracker) RecordSDKToolUse(sessionID, toolName string, input interface{}) {
	if !progressKnownTools[toolName] {
		return
	}
	target := extractProgressToolTarget(toolName, input)
	t.recordStep(sessionID, toolName, target)
}

func (t *ProgressTracker) ConsumeLine(sessionID, line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	m := progressPTYPattern.FindStringSubmatch(trimmed)
	if len(m) < 3 {
		return false
	}
	t.recordStep(sessionID, m[1], strings.TrimSpace(m[2]))
	return true
}

func (t *ProgressTracker) GetProgress(sessionID string) *SessionProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.sessions[sessionID]
	if !ok {
		return nil
	}
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

const progressMaxTargetRunes = 60

func (t *ProgressTracker) FormatProgress(sessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.sessions[sessionID]
	if !ok || p.StepCount == 0 || p.CurrentStep == nil {
		return ""
	}
	step := p.CurrentStep
	icon := progressToolIcons[step.Action]
	if icon == "" {
		icon = "🔧"
	}
	target := progressTruncateTarget(step.Target, progressMaxTargetRunes)
	if target != "" {
		return fmt.Sprintf("%s 第 %d 步: %s %s", icon, step.StepNumber, step.Action, target)
	}
	return fmt.Sprintf("%s 第 %d 步: %s", icon, step.StepNumber, step.Action)
}

func (t *ProgressTracker) Reset(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, sessionID)
}

const progressMaxRecentSteps = 20

func (t *ProgressTracker) recordStep(sessionID, action, target string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.sessions[sessionID]
	if !ok {
		p = &SessionProgress{}
		t.sessions[sessionID] = p
	}
	p.StepCount++
	step := ProgressStep{StepNumber: p.StepCount, Action: action, Target: target}
	p.CurrentStep = &step
	p.Steps = append(p.Steps, step)
	if len(p.Steps) > progressMaxRecentSteps {
		p.Steps = p.Steps[len(p.Steps)-progressMaxRecentSteps:]
	}
}

func progressTruncateTarget(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return "..." + string(runes[len(runes)-(maxRunes-3):])
}

func extractProgressToolTarget(toolName string, input interface{}) string {
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
