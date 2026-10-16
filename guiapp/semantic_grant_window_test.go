package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSemanticGrantWindowPassed(t *testing.T) {
	if semanticGrantWindowPassed(time.Time{}) {
		t.Fatal("zero expiry must stay on the admission path")
	}
	if semanticGrantWindowPassed(time.Now().UTC().Add(time.Hour)) {
		t.Fatal("a grant inside its window was treated as expired")
	}
	if !semanticGrantWindowPassed(time.Now().UTC().Add(-time.Second)) {
		t.Fatal("a grant past its window was left unexpired")
	}
}

func TestSemanticGrantValidateScopeKeepsTurnAndUsesSignedSnapshot(t *testing.T) {
	host := tool.InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	signed := host
	signed.ToolSnapshotID = "snap"
	if got := semanticGrantValidateScope(host, signed); got != signed {
		t.Fatalf("compatible signed scope=%+v", got)
	}
	other := signed
	other.TurnID = "other-turn"
	if got := semanticGrantValidateScope(host, other); got != host {
		t.Fatalf("different turn scope=%+v", got)
	}
}
