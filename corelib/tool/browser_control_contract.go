package tool

import "strings"

// The closed browser action set lives here, not in either host, for the same
// reason BuildVerifyTasks does: it answers "which actions may
// browser.control.web perform". Two copies of that answer would drift, and a
// host that renders one set while enforcing another is the 2026-10-10 failure
// again -- a model shown an unlabelled string guessed "open" and was refused
// with no hint about which name would have worked.

// BrowserControlActions is the closed set of actions browser.control.web
// exposes, and the enum both hosts render into the invocation schema. It is
// documentation for the model, not a boundary: the canonicalizer does not read
// enum, so every host must still call BrowserControlActionAllowed before
// performing the action.
func BrowserControlActions() []string {
	return []string{"navigate", "snapshot"}
}

// BrowserControlActionAllowed is the boundary the enum only advertises. Exact
// match: hosts lowercase and trim the action before calling.
func BrowserControlActionAllowed(action string) bool {
	for _, allowed := range BrowserControlActions() {
		if action == allowed {
			return true
		}
	}
	return false
}

// BrowserControlNeedsURL reports whether an action requires a url. Folding
// this into the action set keeps the two facts that must agree -- which
// actions exist and which of them take a url -- in one place.
func BrowserControlNeedsURL(action string) bool {
	return strings.TrimSpace(action) == "navigate"
}

// BrowserControlLeavesAnEffect reports whether an action can change state
// outside the browser, and so whether a lost answer must be reported as an
// unobserved outcome rather than a definite failure.
//
// This belongs with the action set for the same reason NeedsURL does: it is
// the third answer to "what does this action do", and a host that kept its own
// copy would disagree with the set it renders the moment an action is added.
func BrowserControlLeavesAnEffect(action string) bool {
	return BrowserControlNeedsURL(action)
}

// BrowserControlActionHint renders the set for a rejection message, so a model
// that ignored the enum still learns the valid names from its first failure
// and can retry inside the turn.
func BrowserControlActionHint() string {
	return "allowed: " + strings.Join(BrowserControlActions(), ", ")
}
