package main

// GateIntent represents the five-category classification result for the Gate.
type GateIntent string

const (
	GateIntentNewProject   GateIntent = "new_project"
	GateIntentBugFix       GateIntent = "bug_fix"
	GateIntentMaintenance  GateIntent = "maintenance"
	GateIntentNonCoding    GateIntent = "non_coding"
	GateIntentContinuation GateIntent = "continuation"
	GateIntentUnknown      GateIntent = "unknown"
)

func (intent GateIntent) String() string {
	return string(intent)
}

func (intent GateIntent) IsKnown() bool {
	return intent != "" && intent != GateIntentUnknown
}
