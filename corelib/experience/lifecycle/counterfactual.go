package lifecycle

import "time"

// CounterfactualVerdict is the outcome of a paired-branch comparison between
// runs with retrieval and runs where retrieval was suppressed.
type CounterfactualVerdict string

const (
	// CounterfactualRetrievalHelpful means the with-retrieval branch ended
	// measurably better than the suppressed branch.
	CounterfactualRetrievalHelpful CounterfactualVerdict = "retrieval_helpful"
	// CounterfactualRetrievalHarmful means the with-retrieval branch ended
	// measurably worse than the suppressed branch.
	CounterfactualRetrievalHarmful CounterfactualVerdict = "retrieval_harmful"
	// CounterfactualNoDifference means both branches ended comparably.
	CounterfactualNoDifference CounterfactualVerdict = "no_difference"
	// CounterfactualNotEvaluable means the trace pair was rejected by the
	// evaluator gates (side effects without sandbox/dry-run, missing pair, …).
	CounterfactualNotEvaluable CounterfactualVerdict = "not_evaluable"
)

// CounterfactualBranchOutcome summarizes one branch of a paired comparison.
type CounterfactualBranchOutcome struct {
	Succeeded  bool   `json:"succeeded"`
	Steps      int    `json:"steps,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
}

// CounterfactualRetrievalEvidence is the durable record a counterfactual
// evaluation writes into the experience lifecycle. It is evaluation-pipeline
// evidence only: it updates utility/governance and never touches the hot path.
type CounterfactualRetrievalEvidence struct {
	TraceID          string                     `json:"trace_id,omitempty"`
	TaskID           string                     `json:"task_id,omitempty"`
	PairKey          string                     `json:"pair_key,omitempty"`
	PrefixStepIndex  int                        `json:"prefix_step_index,omitempty"`
	WithRetrieval    CounterfactualBranchOutcome `json:"with_retrieval"`
	WithoutRetrieval CounterfactualBranchOutcome `json:"without_retrieval"`
	Verdict          CounterfactualVerdict      `json:"verdict"`
	EntryIDs         []string                   `json:"entry_ids,omitempty"`
	// Mode records how the evidence was produced. "offline_event_stream" is
	// the default: the evaluator compares recorded traces instead of replaying
	// checkpoints, because the agent runtime has no checkpoint/restore yet.
	Mode        string    `json:"mode,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	EvaluatedAt time.Time `json:"evaluated_at,omitempty"`
}
