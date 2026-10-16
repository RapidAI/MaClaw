package tool

import "strings"

// The scheduled-task status vocabulary lives in a shared contract for the
// reason the browser, desktop, and repo-mutate sets do: both hosts serving
// schedule.administer.local answer "which statuses exist" separately, and two
// copies of that answer would drift.
//
// The shape here differs from those sets in one deliberate way: aliases are
// accepted but never advertised. A model that sends "pause" or "resume" means
// the same thing as the canonical name it is aliased from, so the runtime
// keeps accepting it -- while the schema renders only the canonical names, the
// way the store spells them back.

// ScheduleStatusOptions is the canonical status vocabulary, and the enum both
// hosts render into the invocation schema. It is documentation for the model,
// not a boundary: the canonicalizer does not read enum, and the runtime
// accepts canonical names together with their aliases through
// ScheduleStatusCanonical.
func ScheduleStatusOptions() []string {
	return []string{"paused", "active"}
}

// ScheduleStatusCanonical maps a model-supplied status to the canonical value
// the record stores, mirroring what the store spells back. Empty stays
// accepted-as-empty: status is optional, and omitting it must read the same
// as sending nothing.
func ScheduleStatusCanonical(raw string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "paused", "pause":
		return "paused", true
	case "active", "resume":
		return "active", true
	case "":
		return "", true
	default:
		return "", false
	}
}

// ScheduleStatusHint renders the canonical vocabulary for a rejection message,
// so a model whose guess fell outside the aliases still learns the real names
// from its first failure and can retry inside the turn.
func ScheduleStatusHint() string {
	return "allowed: " + strings.Join(ScheduleStatusOptions(), ", ")
}
