package intent

// head_hint_fix.go — local head / Layer 2 hint fix (2026-10-07 replacement audit).
//
// With the Qwen3 embedder most remaining accepted-intent errors on the
// old-339 set came from Layer 2, not from the local head (§21.3/§22.3):
//
//  1. Weak hint veto: when the local head is below threshold, ClassifyContext
//     keeps an unconfirmed search/live_data/office L2 guess as a degraded
//     hint (lookupHintOrUnknownFromL2). If the head's own top-1 is a
//     DIFFERENT label, the two local signals disagree, so the turn collapses
//     to unknown (the L2 guess stays as RunnerUp evidence) instead of acting
//     on the hint.
//  2. Composite override: a confident L2 declared composite (e.g.
//     live_data + document_generate) is replaced by the head's single label
//     when the head picks a different primary at or above its strong
//     (sensitive) threshold max(Tau, TauS).
//
// Flag MACLAW_INTENT_HEAD_HINT_FIX: "0"/"off"/"false" disables, "1"/"on"/
// "true" forces on, unset → on for the Qwen3 embedder family only (Gemma
// behaviour is unchanged by default).

import (
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// HeadHintFixEnv toggles the local head / L2 hint fix.
const HeadHintFixEnv = "MACLAW_INTENT_HEAD_HINT_FIX"

// headHintFixEnabled resolves the flag for the live embedder.
func headHintFixEnabled(emb embedding.Embedder) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(HeadHintFixEnv))) {
	case "0", "off", "false", "no":
		return false
	case "1", "on", "true", "yes":
		return true
	}
	return embedding.EmbedderFamily(emb) == embedding.FamilyQwen3
}

// vetoL2HintByHead collapses a kept degraded L2 hint to unknown when the
// local head's (sub-threshold) top-1 names a different label. result is the
// output of lookupHintOrUnknownFromL2; anything already unknown is returned
// unchanged.
func vetoL2HintByHead(result ClassificationResult, headLabel string, headProb float64) (ClassificationResult, bool) {
	if headLabel == "" || result.Primary == LabelUnknown || result.Primary == LabelAmbiguous {
		return result, false
	}
	if IntentLabel(headLabel) == result.Primary {
		return result, false
	}
	replaced := ClassificationResult{
		Primary:       LabelUnknown,
		Confidence:    0.30,
		Layer:         2,
		Reason:        fmt.Sprintf("%s; local head disagrees (head=%s %.2f) → unknown", result.Reason, headLabel, headProb),
		Degraded:      true,
		RunnerUp:      result.Primary,
		RunnerUpScore: result.Confidence,
	}
	// The admitted action hint reads this vector. A fresh struct would drop it.
	carryLayer2QueryEvidence(&replaced, result)
	return replaced, true
}

// isDeclaredCompositeResult reports whether r carries a taxonomy-declared
// composite pair (primary + at least one declared secondary).
func isDeclaredCompositeResult(r ClassificationResult) bool {
	for _, sec := range r.Secondary {
		if sec != r.Primary && declaredCompositeIntentPair(r.Primary, sec) {
			return true
		}
	}
	return false
}

// overrideCompositeByHead returns the head's single-label verdict when a
// confident L2 declared composite disagrees with a strong head top-1.
func overrideCompositeByHead(l2 ClassificationResult, headLabel string, headProb, strongThreshold float64) (ClassificationResult, bool) {
	if headLabel == "" || headLabel == string(LabelUnknown) || headLabel == string(LabelAmbiguous) {
		return l2, false
	}
	if !isDeclaredCompositeResult(l2) || IntentLabel(headLabel) == l2.Primary {
		return l2, false
	}
	if strongThreshold <= 0 || headProb < strongThreshold {
		return l2, false
	}
	replaced := ClassificationResult{
		Primary:    IntentLabel(headLabel),
		Confidence: headProb,
		Layer:      3,
		Reason:     fmt.Sprintf("local head overrides L2 composite: head=%s (%.3f ≥ %.2f), l2=%s+%v (%.3f)", headLabel, headProb, strongThreshold, l2.Primary, l2.Secondary, l2.Confidence),
	}
	carryLayer2QueryEvidence(&replaced, l2)
	return replaced, true
}
