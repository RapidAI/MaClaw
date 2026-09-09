// Package counterfactual implements the Phase 7 counterfactual evaluation
// pipeline as an offline, event-stream based evaluator.
//
// Trade-off: the agent runtime has no checkpoint/restore for hot-path trace
// replay, so instead of re-running a prefix checkpoint with and without
// retrieval, the evaluator pairs *recorded* traces that share a task key
// (TaskID) and differ in whether retrieval was injected. Branch A is the set
// of traces with experience_injected events, branch B the set without. This
// keeps evaluation out of the user interaction hot path entirely; results only
// flow back through Provider.UpdateUtility and governance evidence.
//
// Traces that touch side-effecting tools are not evaluable by default — the
// caller must assert SandboxDryRun (the stream came from a sandbox or dry-run
// provider) before they are compared.
package counterfactual

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// DefaultSideEffectTools is the conservative default set of tools whose
// presence in a trace makes the trace non-evaluable without sandbox/dry-run.
// Read-only toolchains (search, read, list) are preferred for evaluation.
var DefaultSideEffectTools = map[string]bool{
	"bash":          true,
	"ssh_bash":      true,
	"write_file":    true,
	"edit_file":     true,
	"apply_patch":   true,
	"delete_file":   true,
	"move_file":     true,
	"craft_tool":    true,
	"manage_skill":  true,
	"schedule_task": true,
}

// Options configures the Evaluator.
type Options struct {
	// Now supplies timestamps for evidence records. Default: time.Now.
	Now func() time.Time
	// SideEffectTools overrides DefaultSideEffectTools when non-nil.
	SideEffectTools map[string]bool
	// SandboxDryRun asserts the event stream was produced inside a sandbox or
	// by a dry-run provider, which lifts the side-effect gate.
	SandboxDryRun bool
}

// Evaluator compares paired traces from an event stream and writes
// CounterfactualRetrievalEvidence. It never touches the hot path.
type Evaluator struct {
	Opts Options
	// Provider receives utility writebacks for helpful/harmful verdicts.
	// Nil disables writeback (evidence is still returned).
	Provider lifecycle.Provider
	// Sink optionally receives counterfactual_evaluated lifecycle events.
	Sink lifecycle.EventSink
}

// traceSummary is the per-trace rollup the comparison is built from.
type traceSummary struct {
	traceID     string
	taskID      string
	hasInject   bool
	entryIDs    []string
	succeeded   bool
	decisive    bool
	steps       int
	tokens      int
	errorClass  string
	sideEffects bool
}

// Evaluate groups the event stream by trace, pairs traces by task key, and
// returns one CounterfactualRetrievalEvidence per comparable pair key.
// Non-evaluable pairs are returned with Verdict=not_evaluable so callers can
// audit why a task was excluded; they produce no utility writeback.
func (e *Evaluator) Evaluate(ctx context.Context, events []lifecycle.Event) []lifecycle.CounterfactualRetrievalEvidence {
	now := time.Now
	if e != nil && e.Opts.Now != nil {
		now = e.Opts.Now
	}
	sideEffectTools := DefaultSideEffectTools
	if e != nil && e.Opts.SideEffectTools != nil {
		sideEffectTools = e.Opts.SideEffectTools
	}
	sandboxDryRun := e != nil && e.Opts.SandboxDryRun

	traces := map[string]*traceSummary{}
	var traceOrder []string
	for _, event := range events {
		traceID := strings.TrimSpace(event.TraceID)
		if traceID == "" {
			continue
		}
		summary, ok := traces[traceID]
		if !ok {
			summary = &traceSummary{traceID: traceID, taskID: strings.TrimSpace(event.TaskID)}
			traces[traceID] = summary
			traceOrder = append(traceOrder, traceID)
		}
		if summary.taskID == "" {
			summary.taskID = strings.TrimSpace(event.TaskID)
		}
		switch event.EventType {
		case lifecycle.EventExperienceInjected:
			if len(event.EntryIDs) > 0 {
				summary.hasInject = true
				summary.entryIDs = mergeEvidenceIDs(summary.entryIDs, event.EntryIDs)
			}
			summary.tokens += event.TokenCost
		case lifecycle.EventToolCallFinished:
			if event.StepIndex+1 > summary.steps {
				summary.steps = event.StepIndex + 1
			}
			if sideEffectTools[strings.ToLower(strings.TrimSpace(event.ToolName))] {
				summary.sideEffects = true
			}
			if !summary.decisive {
				outcome := strings.ToLower(strings.TrimSpace(event.Outcome))
				switch outcome {
				case "success", "succeeded", "ok", "passed":
					summary.succeeded, summary.decisive = true, true
				case "failure", "failed", "error":
					summary.succeeded, summary.decisive = false, true
				}
				if summary.errorClass == "" {
					summary.errorClass = strings.TrimSpace(event.ErrorClass)
				}
			}
		case lifecycle.EventTaskSucceeded:
			summary.succeeded, summary.decisive = true, true
		case lifecycle.EventTaskFailed:
			summary.succeeded, summary.decisive = false, true
			if summary.errorClass == "" {
				summary.errorClass = strings.TrimSpace(event.ErrorClass)
			}
		}
	}

	// Pair traces by task key. Without a shared key there is no counterfactual
	// pair to compare, so those traces are skipped silently.
	type pairGroup struct {
		with    []*traceSummary
		without []*traceSummary
	}
	groups := map[string]*pairGroup{}
	for _, traceID := range traceOrder {
		summary := traces[traceID]
		key := summary.taskID
		if key == "" {
			continue
		}
		group, ok := groups[key]
		if !ok {
			group = &pairGroup{}
			groups[key] = group
		}
		if summary.hasInject {
			group.with = append(group.with, summary)
		} else {
			group.without = append(group.without, summary)
		}
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []lifecycle.CounterfactualRetrievalEvidence
	for _, key := range keys {
		if ctx != nil && ctx.Err() != nil {
			return out
		}
		group := groups[key]
		if len(group.with) == 0 || len(group.without) == 0 {
			continue
		}
		evidence := lifecycle.CounterfactualRetrievalEvidence{
			TraceID:     group.with[0].traceID,
			TaskID:      key,
			PairKey:     key,
			Mode:        "offline_event_stream",
			EvaluatedAt: now(),
		}
		for _, summary := range group.with {
			evidence.EntryIDs = mergeEvidenceIDs(evidence.EntryIDs, summary.entryIDs)
		}
		if !sandboxDryRun && (branchHasSideEffects(group.with) || branchHasSideEffects(group.without)) {
			evidence.Verdict = lifecycle.CounterfactualNotEvaluable
			evidence.Reason = "side_effect_without_sandbox"
			out = append(out, evidence)
			continue
		}
		withOutcome := aggregateBranch(group.with)
		withoutOutcome := aggregateBranch(group.without)
		evidence.WithRetrieval = withOutcome
		evidence.WithoutRetrieval = withoutOutcome
		evidence.Verdict = compareBranches(group.with, group.without)
		evidence.Reason = verdictReason(evidence.Verdict)
		out = append(out, evidence)
		e.writeBack(ctx, evidence)
	}
	return out
}

// writeBack routes the verdict into utility/governance only. Helpful verdicts
// mark the injected entries helpful; harmful verdicts mark them harmful. The
// current task execution is never modified.
func (e *Evaluator) writeBack(ctx context.Context, evidence lifecycle.CounterfactualRetrievalEvidence) {
	if e == nil {
		return
	}
	if e.Sink != nil {
		e.Sink.RecordExperienceEvent(lifecycle.Event{
			TaskID:    evidence.TaskID,
			EventType: lifecycle.EventCounterfactualEvaluated,
			EntryIDs:  evidence.EntryIDs,
			Outcome:   string(evidence.Verdict),
			Reason:    evidence.Mode,
			CreatedAt: evidence.EvaluatedAt,
		})
	}
	if e.Provider == nil {
		return
	}
	var helpful, harmful, success bool
	switch evidence.Verdict {
	case lifecycle.CounterfactualRetrievalHelpful:
		helpful, success = true, true
	case lifecycle.CounterfactualRetrievalHarmful:
		harmful = true
	default:
		return
	}
	for _, entryID := range evidence.EntryIDs {
		_ = e.Provider.UpdateUtility(ctx, lifecycle.UtilityUpdate{
			EntryID:      entryID,
			TraceID:      evidence.TraceID,
			Helpful:      helpful,
			Harmful:      harmful,
			Success:      success,
			Reason:       "counterfactual:" + evidence.PairKey,
			EvidenceType: string(lifecycle.EventCounterfactualEvaluated),
		})
	}
}

func branchHasSideEffects(branch []*traceSummary) bool {
	for _, summary := range branch {
		if summary.sideEffects {
			return true
		}
	}
	return false
}

// aggregateBranch collapses a branch's traces into one representative outcome:
// majority success, average steps, total injection tokens.
func aggregateBranch(branch []*traceSummary) lifecycle.CounterfactualBranchOutcome {
	var decisive, successes, steps, tokens int
	errorClass := ""
	for _, summary := range branch {
		if !summary.decisive {
			continue
		}
		decisive++
		if summary.succeeded {
			successes++
		}
		steps += summary.steps
		tokens += summary.tokens
		if errorClass == "" && !summary.succeeded {
			errorClass = summary.errorClass
		}
	}
	outcome := lifecycle.CounterfactualBranchOutcome{ErrorClass: errorClass, Tokens: tokens}
	if decisive > 0 {
		outcome.Succeeded = successes*2 >= decisive
		outcome.Steps = steps / decisive
	}
	return outcome
}

// compareBranches derives the verdict from per-branch success rates.
func compareBranches(with, without []*traceSummary) lifecycle.CounterfactualVerdict {
	withRate, withN := branchSuccessRate(with)
	withoutRate, withoutN := branchSuccessRate(without)
	if withN == 0 || withoutN == 0 {
		return lifecycle.CounterfactualNotEvaluable
	}
	switch {
	case withRate > withoutRate:
		return lifecycle.CounterfactualRetrievalHelpful
	case withRate < withoutRate:
		return lifecycle.CounterfactualRetrievalHarmful
	default:
		return lifecycle.CounterfactualNoDifference
	}
}

func branchSuccessRate(branch []*traceSummary) (rate float64, decisive int) {
	var successes int
	for _, summary := range branch {
		if !summary.decisive {
			continue
		}
		decisive++
		if summary.succeeded {
			successes++
		}
	}
	if decisive == 0 {
		return 0, 0
	}
	return float64(successes) / float64(decisive), decisive
}

func verdictReason(verdict lifecycle.CounterfactualVerdict) string {
	switch verdict {
	case lifecycle.CounterfactualRetrievalHelpful:
		return "with_retrieval branch succeeded more often than suppressed branch"
	case lifecycle.CounterfactualRetrievalHarmful:
		return "with_retrieval branch succeeded less often than suppressed branch"
	case lifecycle.CounterfactualNoDifference:
		return "both branches ended comparably"
	default:
		return "pair rejected by evaluator gates"
	}
}

func mergeEvidenceIDs(dst, src []string) []string {
	for _, id := range src {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		duplicate := false
		for _, existing := range dst {
			if existing == id {
				duplicate = true
				break
			}
		}
		if !duplicate {
			dst = append(dst, id)
		}
	}
	return dst
}
