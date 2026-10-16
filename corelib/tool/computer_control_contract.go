package tool

import "strings"

// The closed desktop action set lives in the shared contract for the reason
// the browser one does: it answers "which actions may computer.control.desktop
// perform", and two hosts answering separately would drift.
//
// This set has not yet produced an incident the way the browser one did -- no
// invented computer-use action name appears in the logs -- but it carries the
// same shape: a bare string with no enum, so a model has to guess the verb.
// It is closed here so that guess cannot become the next trusted_*_rejected.

// ComputerControlActions is the closed set of actions
// computer.control.desktop exposes, and the enum both hosts render into the
// invocation schema. It is documentation for the model, not a boundary: the
// canonicalizer does not read enum, so every host must still call
// ComputerControlActionAllowed before performing the action.
func ComputerControlActions() []string {
	return []string{"observe", "click", "done"}
}

// ComputerControlActionAllowed is the boundary the enum only advertises. Exact
// match: hosts lowercase and trim the action before calling.
func ComputerControlActionAllowed(action string) bool {
	for _, allowed := range ComputerControlActions() {
		if action == allowed {
			return true
		}
	}
	return false
}

// ComputerControlActionHint renders the set for a rejection message, so a
// model that ignored the enum still learns the valid names from its first
// failure and can retry inside the turn.
func ComputerControlActionHint() string {
	return "allowed: " + strings.Join(ComputerControlActions(), ", ")
}
