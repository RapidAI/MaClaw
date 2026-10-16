package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestDebugRunnerUpSignal(t *testing.T) {
	r := intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true, RunnerUp: intent.LabelSSH, RunnerUpScore: 0.83}
	t.Logf("signal=%v label=%v floor=%v", classificationHasSSHSignal(r), classificationHasLabel(r, intent.LabelSSH), intent.EmbeddingLookupMinScore)
	t.Logf("managed=%v meetsFloor=%v belowFloor=%v", imSemanticIntentIsManaged(r), semanticClassificationMeetsResolverFloor(r), semanticClassificationPlansBelowResolverFloor(r))
	h := &IMMessageHandler{registry: NewToolRegistry()}
	t.Logf("connectPublished=%v anyPublished=%v", semanticTrustedSSHConnectPublished(h), semanticTrustedSSHAnyPublished(h))
}
