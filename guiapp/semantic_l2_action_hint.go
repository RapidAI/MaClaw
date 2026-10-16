package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// semanticL2ActionHintMinScore is the L2 score at which an ambiguous-but-
// leading embedding label still counts as this turn's own action evidence
// (EmbeddingConfidentMinScore band).
const semanticL2ActionHintMinScore = 0.78

// semanticBareL2ActionHint returns this turn's own side-effecting L2 leader
// when the classifier collapsed the turn to unknown (L2 ambiguous and L3
// unavailable / abstained). The collapse keeps the L2 leader in RunnerUp.
//
// Production 2026-10-14 13:36 "给znsoft@gmail.com 发信测试": L2 mailbox_send
// 0.80, local head abstained → unknown 0.30; the task-context merge then
// read the previous turn (mailbox_read) and the open residue froze the plan
// to message.read.mailbox, so the send tool was "not available this turn".
func semanticBareL2ActionHint(r intent.ClassificationResult) (intent.ClassificationResult, bool) {
	if !r.Degraded || (r.Primary != "" && r.Primary != intent.LabelUnknown && !r.Primary.IsNonCapabilityLabel()) {
		return intent.ClassificationResult{}, false
	}
	if r.RunnerUp == "" || r.RunnerUp == intent.LabelUnknown || r.RunnerUpScore < semanticL2ActionHintMinScore {
		return intent.ClassificationResult{}, false
	}
	hint := intent.ClassificationResult{
		Primary:    r.RunnerUp,
		Confidence: r.RunnerUpScore,
		Layer:      2,
		Degraded:   true,
		QueryVec:   r.QueryVec,
		Reason:     strings.TrimSpace(r.Reason + "; l2 action hint admitted"),
	}
	if !imSemanticIntentIsManaged(hint) || semanticReadOnlyGovernedLabels[hint.Primary] {
		return intent.ClassificationResult{}, false
	}
	return hint, true
}

// semanticMergeDropsBareActionHint reports whether a task-context merge
// result must be rejected because it does not carry the bare turn's own
// L2 action (a send must not become the previous turn's read).
func semanticMergeDropsBareActionHint(hint *intent.ClassificationResult, merged intent.ClassificationResult) bool {
	return hint != nil && !merged.HasLabel(hint.Primary)
}

// semanticBareActionHintOverridesResidue reports whether the bare L2 action
// is a different side effect than the open residue obligation, in which case
// session-residue continuation must not replace this turn's own intent.
func semanticBareActionHintOverridesResidue(hint *intent.ClassificationResult, residue semanticSessionResidue) bool {
	return hint != nil && semanticResidueMutatingDisjoint(*hint, residue.Needs)
}
