package tool

import "strings"

// The knowledge-admin status vocabulary lives in a shared contract for the
// reason the browser, repo-mutate, and desktop sets do: both hosts serving
// knowledge.administer answered "which statuses exist" with byte-identical
// switches, and two copies of that answer would drift.
//
// Shape: alias variety. The schema renders only the canonical names; the
// runtime also accepts their imperative aliases ("disable", "delete") and the
// empty-is-unset reading.

// KnowledgeAdminStatusOptions is the canonical set of statuses the knowledge
// admin capability advertises in its invocation schema.
func KnowledgeAdminStatusOptions() []string {
	return []string{"disabled", "enabled", "deleted"}
}

// KnowledgeAdminStatusCanonical maps a model-supplied status to the value the
// record stores, including the aliases a model naturally reaches for. Empty
// stays accepted-as-empty: a status is optional to name.
func KnowledgeAdminStatusCanonical(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "disabled", "disable":
		return "disabled", true
	case "enabled", "enable", "active":
		return "enabled", true
	case "deleted", "delete":
		return "deleted", true
	case "":
		return "", true
	default:
		return "", false
	}
}

// KnowledgeAdminStatusHint renders the canonical vocabulary for a rejection
// message, so a model whose guess fell outside the aliases still learns the
// real names from its first failure and can retry inside the turn.
func KnowledgeAdminStatusHint() string {
	return "allowed: " + strings.Join(KnowledgeAdminStatusOptions(), ", ")
}
