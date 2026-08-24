package agentservice

import (
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// replaySurface builds the smallest surface that can answer "what did this
// call already do", with one selection parked in the given terminal state.
func replaySurface(t *testing.T, state coretool.PlanExecutionState) (*coreDynamicSemanticSurface, string) {
	t.Helper()
	store := coretool.NewMemoryPlanExecutionStore()
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	selectionID := "selection:one"
	if _, acquired, err := store.Acquire(coretool.PlanExecutionRecord{Scope: scope, SelectionID: selectionID, StartedAt: time.Now().UTC()}); err != nil || !acquired {
		t.Fatalf("acquire=%v err=%v", acquired, err)
	}
	if state != coretool.PlanExecutionRunning {
		if _, err := store.Complete(scope, selectionID, state, "result-digest", "reason_code", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	return &coreDynamicSemanticSurface{routing: DynamicSemanticRouting{ExecutionStore: store}, scope: scope}, selectionID
}

// A dispatched-but-unconfirmed operation has no rejection prefix, because it
// is not a rejection. Deriving the verdict from that text told the model an
// effect had landed while its receipt was still outstanding.
func TestReplayedResultDoesNotCallAPendingOperationASuccess(t *testing.T) {
	surface, selectionID := replaySurface(t, coretool.PlanExecutionAwaitingReceipt)
	result := surface.dynamicSemanticReplayedResult(selectionID, "delivery accepted for dispatch")
	if result.Succeeded {
		t.Fatalf("pending operation replayed as a success: %#v", result)
	}
	if !result.AwaitingReceipt {
		t.Fatalf("pending operation lost its awaiting-receipt evidence: %#v", result)
	}
}

// Unknown must survive replay whichever way its text happens to lean. Both
// directions were live at different layers: corelib marks its own unknowns
// with the rejection prefix and so replayed them as definite failures, while
// any unmarked text replayed as a definite success.
func TestReplayedResultKeepsUnknownWhicheverWayTheTextLeans(t *testing.T) {
	for _, text := range []string{"[system rejected] host_ssh_execution_unknown", "the push may or may not have landed"} {
		surface, selectionID := replaySurface(t, coretool.PlanExecutionUnknown)
		result := surface.dynamicSemanticReplayedResult(selectionID, text)
		if result.Succeeded || !result.Unknown {
			t.Fatalf("unknown outcome %q replayed as %#v", text, result)
		}
	}
}

// The executor recognizes two failure prefixes; the replay text check only
// ever knew one, so a recorded "error:" failure came back as a success.
func TestReplayedResultDoesNotPromoteARecordedFailure(t *testing.T) {
	surface, selectionID := replaySurface(t, coretool.PlanExecutionFailed)
	if result := surface.dynamicSemanticReplayedResult(selectionID, "error: provider refused"); result.Succeeded {
		t.Fatalf("recorded failure replayed as a success: %#v", result)
	}
}

func TestReplayedResultStillReplaysASuccess(t *testing.T) {
	surface, selectionID := replaySurface(t, coretool.PlanExecutionSucceeded)
	result := surface.dynamicSemanticReplayedResult(selectionID, "lookup result")
	if !result.Succeeded || result.Unknown || result.AwaitingReceipt {
		t.Fatalf("success replay=%#v", result)
	}
}

// Reading the verdict is not merely as good as remembering it, it is better:
// a trusted receipt can settle an operation after the first attempt returned,
// and only the execution row knows that.
func TestReplayedResultSeesALateReceipt(t *testing.T) {
	surface, selectionID := replaySurface(t, coretool.PlanExecutionUnknown)
	if _, err := surface.routing.ExecutionStore.SettleAwaitingReceipt(surface.scope, selectionID, coretool.PlanExecutionSucceeded, "receipt-digest", "gateway_accepted", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// The frozen text still says "we could not observe this", which is what
	// the first attempt honestly reported. The receipt has since said
	// otherwise, and only the execution row knows it.
	result := surface.dynamicSemanticReplayedResult(selectionID, "[system rejected] host_ssh_execution_unknown")
	if !result.Succeeded || result.Unknown {
		t.Fatalf("late receipt replay=%#v", result)
	}
}

// With no durable verdict to read, text is all that is left. It must still
// refuse the shapes that are certainly not success.
func TestReplayedResultFallbackStillRefusesTheObviousNonSuccesses(t *testing.T) {
	surface := &coreDynamicSemanticSurface{}
	if result := surface.dynamicSemanticReplayedResult("selection:one", "[system unknown] host_ssh_timeout"); result.Succeeded || !result.Unknown {
		t.Fatalf("fallback unknown=%#v", result)
	}
	if result := surface.dynamicSemanticReplayedResult("selection:one", "error: provider refused"); result.Succeeded {
		t.Fatalf("fallback error=%#v", result)
	}
	if result := surface.dynamicSemanticReplayedResult("selection:one", "lookup result"); !result.Succeeded {
		t.Fatalf("fallback success=%#v", result)
	}
}
