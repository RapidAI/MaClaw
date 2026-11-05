package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestFileHopJITResultStoreRoundTripAndScopeIsolation(t *testing.T) {
	store := newFileHopJITResultStore(t.TempDir())
	scope := tool.InvocationScope{RootTaskID: "run", PlanID: "plan", SessionID: "session", TurnID: "step", PrincipalID: "user"}
	if err := store.SaveHopJITResult(scope, "selection", "request", "结果值"); err != nil {
		t.Fatal(err)
	}
	value, found, err := store.LoadHopJITResult(scope, "selection", "request")
	if err != nil || !found || value != "结果值" {
		t.Fatalf("value=%q found=%v err=%v", value, found, err)
	}
	other := scope
	other.PrincipalID = "other"
	if _, found, err := store.LoadHopJITResult(other, "selection", "request"); err != nil || found {
		t.Fatalf("cross-scope result found=%v err=%v", found, err)
	}
	if _, found, err := store.LoadHopJITResult(scope, "selection", "other-request"); err != nil || found {
		t.Fatalf("different request result found=%v err=%v", found, err)
	}
}
