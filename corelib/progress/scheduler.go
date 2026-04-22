package progress

import (
	"math"
)

// ScheduleAction is the dispatch decision for a message arriving during an active agent loop.
type ScheduleAction int

const (
	// ActionMerge injects the message into the current agent loop as supplementary context.
	ActionMerge ScheduleAction = iota
	// ActionInsert pauses the current task, processes the new message, then resumes.
	ActionInsert
	// ActionReplace abandons the current task and processes the new message.
	ActionReplace
	// ActionEnqueue queues the message for processing after the current task completes.
	ActionEnqueue
	// ActionStatusQuery returns current progress without interrupting the task.
	ActionStatusQuery
)

// String returns a human-readable name for the action.
func (a ScheduleAction) String() string {
	switch a {
	case ActionMerge:
		return "merge"
	case ActionInsert:
		return "insert"
	case ActionReplace:
		return "replace"
	case ActionEnqueue:
		return "enqueue"
	case ActionStatusQuery:
		return "status_query"
	default:
		return "unknown"
	}
}

// ScheduleDecision is the output of the message scheduler.
type ScheduleDecision struct {
	Action     ScheduleAction
	Confidence float64 // [0,1] — how confident the scheduler is in this decision
	Reason     string  // human-readable explanation for debugging
}

// ScheduleInput bundles the three orthogonal signals used for dispatch decisions.
type ScheduleInput struct {
	// Relevance is the semantic similarity between the new message and the
	// current task description. Range [0,1]. Computed via embedding cosine.
	// -1 means unavailable (embedding not loaded).
	Relevance float64

	// DomainMatch is true when the new message's intent domain matches
	// the current task's intent domain (e.g. both are "Coding").
	DomainMatch bool

	// Structure contains syntactic features of the new message.
	Structure StructureSignal
}

// Relevance thresholds — initial values, to be calibrated with labeled data.
const (
	highRelevanceThreshold = 0.60
	lowRelevanceThreshold  = 0.30
)

// Schedule makes a dispatch decision based on three orthogonal signals:
// semantic relevance, intent domain match, and message structure.
//
// Decision matrix:
//
//	                    High relevance + Same domain   Low relevance + Diff domain   Low relevance + No clear intent
//	Negation            Replace                        Replace                        Replace
//	Non-neg + Short     Merge                          Enqueue                        StatusQuery
//	Non-neg + Medium    Merge                          Insert                         Insert
//	Non-neg + Long      Merge                          Insert                         Insert
//
// When relevance is unavailable (-1), the scheduler falls back to
// domain match + structure only, which is a graceful degradation.
func Schedule(input ScheduleInput) ScheduleDecision {
	s := input.Structure
	rel := input.Relevance
	domainMatch := input.DomainMatch

	// Classify relevance into high/low/unknown.
	relHigh := rel >= highRelevanceThreshold
	relLow := rel >= 0 && rel < lowRelevanceThreshold
	relUnknown := rel < 0 // embedding unavailable

	// When relevance is unknown, use domain match as a proxy.
	if relUnknown {
		relHigh = domainMatch
		relLow = !domainMatch
	}

	// --- Row 1: Negation structure → Replace ---
	if s.HasNegation {
		// Short negation with high relevance could be "不要红色" (modify, not cancel).
		// But for safety, we still treat negation as Replace — the user can override
		// via the desktop menu. For IM, AMBIGUOUS cases get a one-time confirmation.
		if relHigh && !s.IsShort {
			// "不要用Python，改用C++" — high relevance + negation + medium/long
			// This is more likely a modification than a cancellation.
			return ScheduleDecision{
				Action:     ActionMerge,
				Confidence: 0.65,
				Reason:     "negation + high relevance + non-short → likely modification",
			}
		}
		return ScheduleDecision{
			Action:     ActionReplace,
			Confidence: confidenceFromSignals(relLow, true, s),
			Reason:     "negation structure detected",
		}
	}

	// --- Row 2-4: Non-negation ---

	// High relevance + same domain → Merge (supplementary info).
	if relHigh || domainMatch {
		return ScheduleDecision{
			Action:     ActionMerge,
			Confidence: confidenceFromSignals(relHigh, false, s),
			Reason:     "high relevance or same domain → supplement",
		}
	}

	// Low relevance + different domain.
	if relLow || !domainMatch {
		if s.IsShort {
			// Very short + low relevance + no negation → likely "?" or status check.
			return ScheduleDecision{
				Action:     ActionStatusQuery,
				Confidence: 0.60,
				Reason:     "short + low relevance → status query",
			}
		}
		// Medium or long + low relevance → new task, insert it.
		return ScheduleDecision{
			Action:     ActionInsert,
			Confidence: confidenceFromSignals(relLow, false, s),
			Reason:     "low relevance + different domain → new task",
		}
	}

	// Fallback: middle-ground relevance, no strong signals → enqueue.
	return ScheduleDecision{
		Action:     ActionEnqueue,
		Confidence: 0.40,
		Reason:     "ambiguous signals → enqueue for safety",
	}
}

// confidenceFromSignals computes a confidence score from signal strength.
func confidenceFromSignals(strongRelevance, hasNegation bool, s StructureSignal) float64 {
	base := 0.50

	if strongRelevance {
		base += 0.20
	}
	if hasNegation {
		base += 0.15
	}
	if s.IsShort {
		base += 0.05 // short messages are less ambiguous
	}

	return math.Min(base, 0.95)
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns -1 if either vector is nil or empty.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return -1
	}

	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
