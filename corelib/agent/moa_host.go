package agent

import (
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// MoAHost is an optional LoopCallbacks extension for Mixture-of-Agents.
// When implemented and active, RunLoop fans out tool-less reference models
// before the aggregator (tool-bearing) stream call.
type MoAHost interface {
	// PrepareMoA returns whether MoA should run this iteration.
	// toolsSeen: conversation already has tool results / tool_calls.
	// fanoutsRan: how many reference fan-outs already happened this loop.
	// When active=true, preset is the resolved council; progress is a short UI string.
	PrepareMoA(iteration int, toolsSeen bool, fanoutsRan int) (active bool, preset moa.ResolvedPreset, progress string)
}

// MoABudgetGate is an optional host extension for daily LLM budget precheck (PR3).
// When AllowMoAFanOut returns false, RunLoop skips reference models and continues
// with aggregator-only for that iteration (MoA session still active).
type MoABudgetGate interface {
	// AllowMoAFanOut reports whether remaining daily budget can cover nRefs advisor
	// calls plus one aggregator round. reason is a short progress/log string when !ok.
	AllowMoAFanOut(nRefs int) (ok bool, reason string)
}
