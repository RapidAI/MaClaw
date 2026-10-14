package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Production 2026-10-14 13:36 "给znsoft@gmail.com 发信测试，是否被拒收":
// bare L2 mailbox_send 0.80 → local head abstains → unknown 0.30; merge with
// the previous turn reads mailbox_read 0.85 → unknown; the open read residue
// then froze the plan to message.read.mailbox and the send never happened.
func TestBareL2SendSurvivesReadResidueAndMerge(t *testing.T) {
	bare := intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true,
		Reason:   "embedding ambiguous; tree classification unavailable (l2=mailbox_send conf=0.80)",
		RunnerUp: intent.LabelMailboxSend, RunnerUpScore: 0.80,
	}
	hint, ok := semanticBareL2ActionHint(bare)
	if !ok || hint.Primary != intent.LabelMailboxSend {
		t.Fatalf("hint=%+v ok=%v", hint, ok)
	}
	// (c) a merge that flips the send into the previous turn's read is rejected.
	mergedRead := intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.85, Layer: 3}
	if !semanticMergeDropsBareActionHint(&hint, mergedRead) {
		t.Fatal("merge onto mailbox_read must be rejected")
	}
	mergedSend := intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: 0.9, Layer: 3}
	if semanticMergeDropsBareActionHint(&hint, mergedSend) {
		t.Fatal("merge keeping mailbox_send must be accepted")
	}
	// (a) an open read-mailbox residue must not override the send.
	readResidue := semanticSessionResidue{Status: semanticResidueOpen, Needs: []tool.CapabilityNeed{{Capability: tool.CapabilityMessageReadMailbox, Required: true}}}
	if !semanticBareActionHintOverridesResidue(&hint, readResidue) {
		t.Fatal("send hint must override a read-mailbox residue")
	}
	// Same obligation (open send) keeps the residue continuation.
	sendResidue := semanticSessionResidue{Status: semanticResidueOpen, Needs: []tool.CapabilityNeed{{Capability: tool.CapabilityMessageSendMailbox, Required: true}}}
	if semanticBareActionHintOverridesResidue(&hint, sendResidue) {
		t.Fatal("send hint must not override an open send residue")
	}
	// (b) the admitted hint is a managed plan that carries the send capability.
	if !imSemanticIntentIsManaged(hint) {
		t.Fatal("admitted hint must be a managed route")
	}
	found := false
	for _, c := range semanticRuleCapabilities(hint) {
		found = found || c == tool.CapabilityMessageSendMailbox
	}
	if !found {
		t.Fatalf("plan capabilities %v lack message.send.mailbox", semanticRuleCapabilities(hint))
	}
}

func TestBareL2HintIgnoresWeakOrReadOnlyOrConfidentTurns(t *testing.T) {
	cases := []intent.ClassificationResult{
		{Primary: intent.LabelUnknown, Degraded: true, RunnerUp: intent.LabelMailboxSend, RunnerUpScore: 0.70},     // weak
		{Primary: intent.LabelUnknown, Degraded: true, RunnerUp: intent.LabelMailboxRead, RunnerUpScore: 0.90},     // read-only
		{Primary: intent.LabelMailboxRead, Confidence: 0.9, RunnerUp: intent.LabelMailboxSend, RunnerUpScore: 0.9}, // decided
	}
	for i, c := range cases {
		if _, ok := semanticBareL2ActionHint(c); ok {
			t.Fatalf("case %d admitted", i)
		}
	}
}
