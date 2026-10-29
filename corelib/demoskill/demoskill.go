// Package demoskill turns a recorded "demonstrate and narrate" session on the
// bot's own desktop into a skill that guides the agent loop.
//
// First principle (Daniel): 能持续推进任务，交付结果. A demo skill is intent and
// guidance, never a coordinate replay. At execution the bot keeps reasoning,
// adapts when the screen differs, checks each step, replans on a mismatch and
// keeps going until the deliverable is verified with evidence. It stops only
// when it truly needs the user's information or choice.
//
// The whole feature is behind MACLAW_DEMO_SKILL (default off).
package demoskill

import (
	"os"
	"strings"
	"time"
)

// EnvFlag is the feature flag. Only "1", "true", "on", "yes" enable it.
const EnvFlag = "MACLAW_DEMO_SKILL"

// Enabled reports whether demo skills are switched on.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvFlag))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// Event kinds in a recorded timeline.
const (
	KindScreenshot = "screenshot" // Ref points at the stored image; Text may hold OCR/caption
	KindInput      = "input"      // Action: click/type/key/scroll/drag; Text: typed text or key
	KindFocus      = "focus"      // App / Window that got focus
	KindNarration  = "narration"  // what the user said or typed as a note
)

// Event is one timestamped entry of a demonstration.
type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Action string    `json:"action,omitempty"`
	Text   string    `json:"text,omitempty"`
	App    string    `json:"app,omitempty"`
	Window string    `json:"window,omitempty"`
	X      int       `json:"x,omitempty"`
	Y      int       `json:"y,omitempty"`
	Ref    string    `json:"ref,omitempty"`
}

// Timeline is a whole recording.
type Timeline struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Events    []Event   `json:"events"`
}

// Param is a variable input of the skill (what changes between runs).
type Param struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Step is one intent with the observable check that says it is done.
type Step struct {
	Intent string `json:"intent"`
	Check  string `json:"check"`
	Hint   string `json:"hint,omitempty"`  // how the user did it; a hint, not a script
	Wait   bool   `json:"wait,omitempty"`  // long-running: patrol until Check holds
	Risky  string `json:"risky,omitempty"` // "publish" | "payment" | "" -> existing review/ask rules apply
}

// Skill is a distilled demonstration.
type Skill struct {
	Name          string   `json:"name"`
	Goal          string   `json:"goal"`
	Triggers      []string `json:"triggers,omitempty"`
	Preconditions []string `json:"preconditions,omitempty"`
	Params        []Param  `json:"params,omitempty"`
	Steps         []Step   `json:"steps"`
	Deliverable   string   `json:"deliverable"`
	Evidence      string   `json:"evidence,omitempty"` // how to prove the deliverable
	SourceDemo    string   `json:"source_demo,omitempty"`
}
