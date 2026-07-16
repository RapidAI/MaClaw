package computeruse

import (
	"fmt"
	"strings"
)

// blockedWindowHints are substrings that must never be automated.
var blockedWindowHints = []string{
	"user account control",
	"uac",
	"用户账户控制",
	"用户帐户控制",
	"credential",
	"credentials",
	"windows security",
	"windows 安全",
	"security and maintenance",
	"锁屏",
	"lock screen",
	"sign in",
	"登录",
	"privacy",
	"隐私",
	"screen recording",
	"辅助功能",
	"accessibility",
}

// Policy gates computer actions.
type Policy struct {
	TargetApps      []string
	allowPixelClick bool
	Paused          bool
	Stopped         bool // hard stop; requires Reset/new session to continue
	MaxSteps        int
	stepCount       int
}

// NewPolicy builds a policy from config.
func NewPolicy(cfg Config) *Policy {
	return &Policy{
		TargetApps:      append([]string(nil), cfg.TargetApps...),
		allowPixelClick: cfg.AllowPixelClick,
		MaxSteps:        cfg.MaxSteps,
	}
}

// Pause freezes further interact actions.
func (p *Policy) Pause() {
	if p != nil {
		p.Paused = true
	}
}

// Resume clears the pause flag (no-op if Stopped).
func (p *Policy) Resume() error {
	if p == nil {
		return nil
	}
	if p.Stopped {
		return fmt.Errorf("computer use session was stopped; start a new task to continue")
	}
	p.Paused = false
	return nil
}

// Stop permanently blocks interact until Reset.
func (p *Policy) Stop() {
	if p != nil {
		p.Stopped = true
		p.Paused = true
	}
}

// Reset clears pause/stop and optionally step counter.
func (p *Policy) Reset(resetSteps bool) {
	if p == nil {
		return
	}
	p.Paused = false
	p.Stopped = false
	if resetSteps {
		p.stepCount = 0
	}
}

// IsPaused reports soft pause.
func (p *Policy) IsPaused() bool {
	return p != nil && p.Paused && !p.Stopped
}

// IsStopped reports hard stop.
func (p *Policy) IsStopped() bool {
	return p != nil && p.Stopped
}

// BeginStep increments the step counter and enforces MaxSteps / pause / stop.
func (p *Policy) BeginStep() error {
	if p == nil {
		return nil
	}
	if p.Stopped {
		return fmt.Errorf("computer use session is stopped")
	}
	if p.Paused {
		return fmt.Errorf("computer use session is paused")
	}
	p.stepCount++
	if p.MaxSteps > 0 && p.stepCount > p.MaxSteps {
		return fmt.Errorf("computer use max steps exceeded (%d)", p.MaxSteps)
	}
	return nil
}

// StepCount returns actions counted so far.
func (p *Policy) StepCount() int {
	if p == nil {
		return 0
	}
	return p.stepCount
}

// AllowClickAt checks whether a click at (x,y) with optional window title is allowed.
// windowTitle may be empty when unknown.
func (p *Policy) AllowClickAt(x, y int, windowTitle string) error {
	if p == nil {
		return nil
	}
	if p.Stopped {
		return fmt.Errorf("computer use session is stopped")
	}
	if p.Paused {
		return fmt.Errorf("computer use session is paused")
	}
	if titleBlocked(windowTitle) {
		return fmt.Errorf("refusing to interact with system/security UI: %q", windowTitle)
	}
	if len(p.TargetApps) > 0 && windowTitle != "" && !titleMatchesAny(windowTitle, p.TargetApps) {
		return fmt.Errorf("window %q is outside approved target apps %v", windowTitle, p.TargetApps)
	}
	_ = x
	_ = y
	return nil
}

// AllowPixelClick reports whether raw x,y clicks are permitted.
func (p *Policy) AllowPixelClick() bool {
	if p == nil {
		return false
	}
	return p.allowPixelClick
}

func titleBlocked(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return false
	}
	for _, h := range blockedWindowHints {
		if strings.Contains(t, strings.ToLower(h)) {
			return true
		}
	}
	return false
}

func titleMatchesAny(title string, patterns []string) bool {
	t := strings.ToLower(title)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(t, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
