package guiapp

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// maybeQuarantineLearnedSkill applies failure-based auto-quarantine to a
// machine-generated skill entry after a failed run. It returns true when the
// entry was just disabled. Call it after incrementing FailureCount.
//
// The disabled guard makes this idempotent: a skill already quarantined (or
// manually disabled) is left untouched, so later failures neither spam the log
// nor overwrite the stored quarantine reason with a new one.
func maybeQuarantineLearnedSkill(entry *corelib.NLSkillEntry, execErr error) bool {
	if entry == nil || !cskill.ShouldQuarantineLearnedSkill(entry) {
		return false
	}
	if normalizeSkillEntryStatus(entry.Status) == skillEntryStatusDisabled {
		return false
	}
	entry.Status = string(skillEntryStatusDisabled)
	entry.LastError = cskill.FormatQuarantineError(entry.FailureCount, execErr)
	log.Printf("[skill-quarantine] learned skill %q disabled after %d failed runs (no lifetime success)", entry.Name, entry.FailureCount)
	return true
}
