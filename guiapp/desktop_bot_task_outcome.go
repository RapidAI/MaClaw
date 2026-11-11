package guiapp

import "strings"

// desktopTaskOutcomeOf keeps a known task outcome from Hub and drops anything
// else (an unknown value is absent, never a status).
func desktopTaskOutcomeOf(v ...string) string {
	if len(v) == 0 {
		return ""
	}
	switch o := strings.TrimSpace(v[0]); o {
	case "done", "partial", "blocked", "not_submitted", "unconfirmed", "awaiting_user", "running":
		return o
	}
	return ""
}

// desktopTaskOutcomeNotDone: a cloud-desktop run finished but its task did not.
func desktopTaskOutcomeNotDone(outcome string) bool {
	o := desktopTaskOutcomeOf(outcome)
	return o != "" && o != "done"
}

// desktopTaskOutcomeNeedsReport: a finished run whose task stopped short,
// not a handed-off job the watcher still owns.
func desktopTaskOutcomeNeedsReport(outcome string) bool {
	o := desktopTaskOutcomeOf(outcome)
	return o != "" && o != "done" && o != "running"
}
