// Package eval is the internal sample-set harness for the long-horizon
// supervisor (design doc docs/design/longhorizon-harness-plan-zh.md, §9 P5).
// It is intentionally small — not OSWorld — and exercises only the
// supervisor's pure-function path in corelib/longhorizon: manager plan
// parsing, episode context assembly, audit report parsing and the completion
// gate. All three roles are served by a scripted stub LLM; no real model,
// tool, workspace, or browser is involved.
//
// Dataset format (one JSON file per category under data/):
//
//	{
//	  "category": "completion_gate",          // stable slug, used in reports
//	  "title": "...",                         // human readable category name
//	  "design_ref": "longhorizon-harness-plan-zh.md §9 P5",
//	  "samples": [ Sample, ... ]
//	}
//
// Sample fields:
//
//	id / title / notes   identity; notes document the design mapping.
//	user_goal            the task goal handed to the manager.
//	max_rounds           outer round budget; defaults to
//	                     longhorizon.DefaultMaxRounds when zero.
//	manager_script       scripted raw manager replies, one per manager call.
//	executor_script      scripted executor claim strings ("status=passed\n…"),
//	                     one per executor episode.
//	auditor_script       scripted raw auditor replies, one per audit.
//	probe_script         scripted mechanical probe digests, one per round;
//	                     an empty entry means "no probe available".
//	expected             assertions evaluated after the run:
//	  final_status       terminal TaskState.Status (done | blocked | …).
//	  completed          exact TaskState.Completed value.
//	  manager_nexts      exact sequence of manager Next values observed.
//	  rounds             exact number of executed executor rounds.
//
// When any script is exhausted the runner repeats its last entry, so
// round-limit samples stay compact; an empty script is a sample bug.
package eval

// DatasetFile is the top-level document of one data/*.json file.
type DatasetFile struct {
	Category  string   `json:"category"`
	Title     string   `json:"title"`
	DesignRef string   `json:"design_ref"`
	Samples   []Sample `json:"samples"`
}

// Sample is one evaluation case. See the package doc for the full format.
type Sample struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Notes          string       `json:"notes,omitempty"`
	UserGoal       string       `json:"user_goal"`
	MaxRounds      int          `json:"max_rounds,omitempty"`
	ManagerScript  []string     `json:"manager_script"`
	ExecutorScript []string     `json:"executor_script,omitempty"`
	AuditorScript  []string     `json:"auditor_script,omitempty"`
	ProbeScript    []string     `json:"probe_script,omitempty"`
	Expected       ExpectedSpec `json:"expected"`
}

// ExpectedSpec holds all assertions evaluated against the run result.
type ExpectedSpec struct {
	FinalStatus  string   `json:"final_status"`
	Completed    bool     `json:"completed"`
	ManagerNexts []string `json:"manager_nexts,omitempty"`
	Rounds       int      `json:"rounds"`
}
