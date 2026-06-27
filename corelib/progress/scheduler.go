package progress

import (
	"math"
	"strings"
)

// ScheduleAction is the dispatch decision for a message arriving during an active agent loop.
type ScheduleAction int

const (
	// ActionMerge injects the message into the current agent loop as supplementary context.
	ActionMerge ScheduleAction = iota
	// ActionQueue queues the message for processing after the current task completes.
	// This replaces the former ActionInsert and ActionEnqueue which had identical
	// runtime behavior (Handled=false, Queued=true).
	ActionQueue
	// ActionReplace abandons the current task and processes the new message.
	ActionReplace
	// ActionStatusQuery returns current progress without interrupting the task.
	ActionStatusQuery
)

// String returns a human-readable name for the action.
func (a ScheduleAction) String() string {
	switch a {
	case ActionMerge:
		return "merge"
	case ActionQueue:
		return "queue"
	case ActionReplace:
		return "replace"
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
//	Non-neg + Short     Merge                          Queue                          StatusQuery
//	Non-neg + Medium    Merge                          Queue                          Queue
//	Non-neg + Long      Merge                          Queue                          Queue
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

	// When both signals agree (high relevance + same domain), strong Merge.
	if relHigh && domainMatch {
		return ScheduleDecision{
			Action:     ActionMerge,
			Confidence: 0.85,
			Reason:     "high relevance + same domain → supplement",
		}
	}

	// High relevance alone (different domain or unknown domain) → Merge with lower confidence.
	// "颜色改红色" has high relevance to "开发游戏" even if domain match is unknown.
	if relHigh {
		return ScheduleDecision{
			Action:     ActionMerge,
			Confidence: 0.70,
			Reason:     "high relevance → likely supplement",
		}
	}

	// Domain match alone (relevance unavailable or mid-range) → depends on length.
	// Short same-domain message → likely supplement ("用C++").
	// Long same-domain message → could be a new task in the same domain.
	if domainMatch {
		if s.IsShort || s.IsMedium {
			return ScheduleDecision{
				Action:     ActionMerge,
				Confidence: 0.60,
				Reason:     "same domain + short/medium → likely supplement",
			}
		}
		// Long + same domain + no relevance data → ambiguous, queue to be safe.
		return ScheduleDecision{
			Action:     ActionQueue,
			Confidence: 0.45,
			Reason:     "same domain + long + no relevance → ambiguous",
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
		// Medium or long + low relevance → new task, queue it.
		return ScheduleDecision{
			Action:     ActionQueue,
			Confidence: confidenceFromSignals(relLow, false, s),
			Reason:     "low relevance + different domain → new task",
		}
	}

	// Fallback: middle-ground relevance, no strong signals → queue.
	return ScheduleDecision{
		Action:     ActionQueue,
		Confidence: 0.40,
		Reason:     "ambiguous signals → queue for safety",
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

// CharOverlapRatio computes the ratio of shared content between two strings
// using bigram overlap (Sørensen–Dice coefficient). This measures structural
// similarity at the character level — how much of the text is literally the same.
//
// Returns a value in [0, 1]:
//   - 1.0 = identical strings (after normalization)
//   - 0.7+ = near-identical with minor edits (typo fix, number change)
//   - 0.3–0.6 = some shared phrases but substantially different content
//   - 0.0 = completely different
//
// This is deliberately NOT semantic similarity (which embeddings measure).
// Two messages about the same topic with different wording have low CharOverlapRatio
// but high embedding cosine. A corrected restatement (same sentence with one
// number changed) has high CharOverlapRatio AND high embedding cosine.
//
// The distinction matters for interrupt scheduling: high embedding cosine alone
// could be either "supplement" or "correction". High CharOverlapRatio specifically
// indicates "correction" — the user re-sent the same message with minor edits.
func CharOverlapRatio(a, b string) float64 {
	// Normalize: trim whitespace, collapse internal spaces.
	a = normalizeForOverlap(a)
	b = normalizeForOverlap(b)

	if a == b {
		return 1.0
	}
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Convert to rune slices for CJK-correct bigram extraction.
	ra := []rune(a)
	rb := []rune(b)

	if len(ra) < 2 || len(rb) < 2 {
		// Too short for meaningful bigram comparison — cannot reliably
		// distinguish correction from different content.
		return 0.0
	}

	// Build bigram multisets.
	bigramsA := bigramMultiset(ra)
	bigramsB := bigramMultiset(rb)

	// Dice coefficient = 2 * |intersection| / (|A| + |B|)
	intersection := 0
	for bg, countA := range bigramsA {
		if countB, ok := bigramsB[bg]; ok {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}

	total := len(ra) - 1 + len(rb) - 1 // number of bigrams in A + B
	if total == 0 {
		return 0.0
	}
	return float64(2*intersection) / float64(total)
}

// bigramMultiset extracts character bigrams from a rune slice and returns
// their frequency counts.
func bigramMultiset(runes []rune) map[[2]rune]int {
	m := make(map[[2]rune]int, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		m[[2]rune{runes[i], runes[i+1]}]++
	}
	return m
}

// normalizeForOverlap trims and collapses internal whitespace for fair comparison.
func normalizeForOverlap(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
