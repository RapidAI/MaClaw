package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// Runtime withdrawal of the managed semantic coding family.
//
// Every other migrated family can be rolled back by dialing the shared-loop
// strangler down: a family that stops taking the shared loop stops being
// served by the managed surface. Coding cannot, because a capability-managed
// turn has a grant-bound executor only on the shared loop and the dispatcher
// therefore ignores the strangler for it. That left exactly one way to
// withdraw the largest migrated family — delete its rule and ship a build.
//
// The dial closes that gap without touching what the strangler means.

// semanticCodingFamilyLabels are the labels routed to
// semanticCodingCapabilityRule.
//
// They are listed rather than derived from "labels sharing that rule variable"
// because those two sets are equal today by coincidence, not by contract. A
// future family that reused the same capability list must not be dragged out
// of service by a dial that names the coding family.
var semanticCodingFamilyLabels = []intent.IntentLabel{
	intent.LabelCoding,
	intent.LabelBugFix,
	intent.LabelMaintenance,
}

func semanticCodingFamilyPercent(h *IMMessageHandler) int {
	cfg := corelib.AppConfig{}
	if h != nil && h.app != nil {
		if loaded, err := h.app.LoadConfig(); err == nil {
			cfg = loaded
		}
	}
	percent, _ := doctor.ResolveSemanticCodingPercent(cfg)
	return percent
}

// semanticCodingFamilyOpen reports whether the managed coding family applies to
// this user. Sticky by userID, and independent of the shared-loop canary
// bucket so the two dials do not withdraw the same population first.
func semanticCodingFamilyOpen(h *IMMessageHandler, userID string) bool {
	return doctor.SemanticCodingCanaryAllows(userID, semanticCodingFamilyPercent(h))
}

// semanticWithdrawnCapabilityLabel returns the first label in the
// classification whose family has a reviewed rule but is withdrawn from this
// user by a runtime dial.
//
// A withdrawn family must behave exactly like a never-migrated one: the turn
// fails closed on the unmapped-label path. Withdrawal is a statement about the
// managed surface being unfit right now; it is not permission to reopen the
// keyword/name router that surface replaced. The pre-migration behavior of a
// coding turn was refusal, and that is what withdrawal restores.
//
// The label scan runs before the dial is resolved so that non-coding turns
// never pay for a config read.
func semanticWithdrawnCapabilityLabel(h *IMMessageHandler, userID string, result intent.ClassificationResult) (intent.IntentLabel, bool) {
	present := intent.IntentLabel("")
	for _, label := range result.Labels() {
		for _, coding := range semanticCodingFamilyLabels {
			if label == coding {
				present = label
				break
			}
		}
		if present != "" {
			break
		}
	}
	if present == "" {
		return "", false
	}
	if semanticCodingFamilyOpen(h, userID) {
		return "", false
	}
	return present, true
}

// semanticLogCodingWithdrawal records a withdrawal at the point it changes an
// outcome. A safety valve nobody can see having fired is a valve that gets
// left closed.
func semanticLogCodingWithdrawal(h *IMMessageHandler, userID string, label intent.IntentLabel) {
	log.Printf("[semantic] coding family withdrawn owner=%q label=%q percent=%d (turn refused as unmapped)",
		userID, label, semanticCodingFamilyPercent(h))
}
