package tool

import "testing"

func TestValidateInvocationScopeSnapshot(t *testing.T) {
	if err := ValidateInvocationScopeSnapshot(InvocationScope{ToolSnapshotID: "toolsnap-a"}, "toolsnap-a"); err != nil {
		t.Fatalf("matching snapshot rejected: %v", err)
	}
	for _, scope := range []InvocationScope{{}, {ToolSnapshotID: "toolsnap-old"}} {
		if err := ValidateInvocationScopeSnapshot(scope, "toolsnap-new"); err == nil {
			t.Fatalf("stale scope accepted: %#v", scope)
		}
	}
}
