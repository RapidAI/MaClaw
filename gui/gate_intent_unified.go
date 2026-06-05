package main

import "github.com/RapidAI/CodeClaw/corelib/intent"

func classifyUnifiedGateIntent(uic *intent.UnifiedIntentClassifier, text, userID string) (GateIntentResult, bool) {
	if uic == nil {
		return GateIntentResult{}, false
	}
	uicResult := uic.Classify(intent.MessageContext{Text: text, UserID: userID})
	return gateIntentResultFromSemanticResult(uicResult), true
}

func gateIntentResultFromSemanticResult(uicResult intent.ClassificationResult) GateIntentResult {
	gateIntent, confidence, gap, layer, reason := uicResult.ToGateIntent()
	return GateIntentResult{
		Intent:     GateIntent(gateIntent),
		Confidence: confidence,
		Gap:        gap,
		Layer:      layer,
		Reason:     reason,
		Degraded:   uicResult.Degraded,
	}
}

func isTrustedSemanticGateResult(result GateIntentResult) bool {
	if !shouldAcceptGateResult(result) {
		return false
	}
	if result.Degraded {
		return false
	}
	switch result.Layer {
	case 2, 3, 23:
		return true
	default:
		return false
	}
}
