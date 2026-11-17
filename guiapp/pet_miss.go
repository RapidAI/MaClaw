package guiapp

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// Absence is measured from the last click, drag, or task pose. A glance does
// not end it: the complaint is the hover sentence, and clearing it on the
// pointer visit would erase the line before it could be read. Five minutes
// is the first complaint, half an hour the sulk, two hours the pout. A pet
// that has already reached a rearing stage says it one step more plainly.
const (
	petMissAfter     = 5 * time.Minute
	petMissSulkAfter = 30 * time.Minute
	petMissPoutAfter = 2 * time.Hour
)

// petMissBondLevel is how many rearing stages this visible time has reached,
// capped at 2 so the wording has a new voice, a familiar voice, and a
// long-companion voice. Later stages share the long-companion voice.
func petMissBondLevel(loadedMS int64, stages []petpack.PetPackRearingStage) int {
	n := 0
	for _, stage := range stages {
		if stage.AfterMS > 0 && loadedMS >= stage.AfterMS {
			n++
		}
	}
	if n > 2 {
		return 2
	}
	return n
}

// petMissIntensity is 0 while she is still accompanied. Past the first
// threshold it follows how long she has been left, and a reared pet is one
// step more obvious until the pout.
func petMissIntensity(away time.Duration, bond int) int {
	absence := 0
	switch {
	case away >= petMissPoutAfter:
		absence = 3
	case away >= petMissSulkAfter:
		absence = 2
	case away >= petMissAfter:
		absence = 1
	}
	if absence == 0 {
		return 0
	}
	if bond > 0 && absence < 3 {
		absence++
	}
	return absence
}

// petMissLine is the one hardcoded complaint. Empty means she has nothing to
// say about being left. Duty text replaces it while there is real work.
func petMissLine(intensity, bond int) string {
	if intensity <= 0 {
		return ""
	}
	if intensity > 3 {
		intensity = 3
	}
	if bond < 0 {
		bond = 0
	}
	if bond > 2 {
		bond = 2
	}
	lines := [3][3]string{
		{"好久没理我了", "你好久没来看我", "我还在这儿等你"},
		{"你是不是把我忘了", "你都不来看我", "一起这么久，你倒忘了我"},
		{"我在这儿等了好久", "我等了好久，你还是没来", "你把我晾了这么久"},
	}
	return lines[intensity-1][bond]
}

// petMissFeel is the face and idle mood held for the whole complaint.
// The body clip only announces the change. A pack that did not author the
// clip keeps its own face. Empty means the complaint is over.
func petMissFeel(intensity int) (expression, gaze, mood string) {
	switch intensity {
	case 1:
		return "expr_wait", "gaze_center", "tired"
	case 2:
		return "expr_sulk", "gaze_center", "tired"
	case 3:
		return "expr_pout", "gaze_center", "tired"
	default:
		return "", "", ""
	}
}

// petMissClip is the body clip for that intensity. The names are pack clips
// played by the host. They are not new performer events. An unknown clip
// falls back to the long_idle event.
func petMissClip(intensity int) string {
	switch intensity {
	case 1:
		return "react_long_idle_rest"
	case 2:
		return "react_long_idle_sulk"
	case 3:
		return "react_long_idle_pout"
	default:
		return ""
	}
}

// petShownPresence keeps a work sentence ahead of the complaint.
func petShownPresence(duty, miss string) string {
	if duty != "" {
		return duty
	}
	return miss
}

// petMissOccupied is true while she is not waiting to be noticed. A work
// sentence, including one that leaves her on the idle pose, is still the
// task talking. Listening, thinking, speaking, and the short done or alert
// poses are the same: the wait starts again only after they end. Quiet is
// not one of those poses.
func petMissOccupied(dutyLine, runtimeState string) bool {
	if strings.TrimSpace(dutyLine) != "" {
		return true
	}
	switch petpack.NormalizeState(runtimeState) {
	case petpack.StateListening, petpack.StateThinking, petpack.StateSpeaking, petpack.StateDone, petpack.StateAlert:
		return true
	default:
		return false
	}
}
