package tool

import "strings"

// The closed repo-mutate action set lives here, not in either host, for the
// reason BuildVerifyTasks and the browser/desktop sets do: it answers "which
// actions may repo.mutate.vcs perform". The GUI surface and the agent-service
// surface both serve the capability, so two copies of this answer would drift.

// RepoMutateActions is the closed set of actions repo.mutate.vcs exposes, and
// the enum both hosts render into the invocation schema. It is documentation
// for the model, not a boundary: the canonicalizer does not read enum, so
// every host must still call RepoMutateActionAllowed before performing the
// action.
func RepoMutateActions() []string {
	return []string{"commit", "push"}
}

// RepoMutateActionAllowed is the boundary the enum only advertises. Exact
// match: hosts lowercase and trim the action before calling.
func RepoMutateActionAllowed(action string) bool {
	for _, allowed := range RepoMutateActions() {
		if action == allowed {
			return true
		}
	}
	return false
}

// RepoMutateNeedsMessage reports whether an action requires a commit message.
// The schema declares message optional because push needs none, so which
// action pairs with a message lives in this contract and nowhere else.
func RepoMutateNeedsMessage(action string) bool {
	return strings.TrimSpace(action) == "commit"
}

// RepoMutateActionHint renders the set for a rejection message, so a model
// that ignored the enum still learns the valid names from its first failure
// and can retry inside the turn.
func RepoMutateActionHint() string {
	return "allowed: " + strings.Join(RepoMutateActions(), ", ")
}
