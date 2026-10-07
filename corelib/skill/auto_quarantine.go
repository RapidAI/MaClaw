package skill

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
)

// LearnedSkillQuarantineThreshold is how many recorded failures a learned
// skill may accumulate with zero lifetime successes before it is quarantined.
//
// Learned skills are machine-generated artifacts distilled from session
// histories. One that has NEVER succeeded is defective by construction —
// leaving it runnable just burns LLM retries on every trigger (a defective
// PDF skill once failed seven consecutive runs before anyone noticed).
// Skills with at least one lifetime success are excluded: for those a failure
// streak is an environment problem, and self-repair is the right handler.
const LearnedSkillQuarantineThreshold = 3

// ShouldQuarantineLearnedSkill reports whether a failed run should quarantine
// the skill (set status "disabled"). Call it after incrementing FailureCount.
func ShouldQuarantineLearnedSkill(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	if !corelib.IsLearnedSource(entry.Source) {
		return false
	}
	if entry.SuccessCount > 0 {
		return false
	}
	return entry.FailureCount >= LearnedSkillQuarantineThreshold
}

// FormatQuarantineError builds the LastError text stored on a quarantined
// skill so both the UI and self-repair see why it was disabled.
func FormatQuarantineError(failures int, runErr error) string {
	if runErr == nil {
		return fmt.Sprintf("auto-quarantined: learned skill failed %d runs without ever succeeding", failures)
	}
	return fmt.Sprintf("auto-quarantined: learned skill failed %d runs without ever succeeding; last error: %s", failures, runErr.Error())
}
