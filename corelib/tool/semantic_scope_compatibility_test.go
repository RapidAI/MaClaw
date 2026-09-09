package tool

import "testing"

func TestInvocationScopesCompatibleAllowsOnlyLegacyBlankSnapshot(t *testing.T) {
	base := InvocationScope{
		RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal",
		ToolSnapshotID: "snapshot-a",
	}

	tests := []struct {
		name  string
		left  InvocationScope
		right InvocationScope
		want  bool
	}{
		{name: "same concrete", left: base, right: base, want: true},
		{name: "legacy left", left: InvocationScope{RootTaskID: base.RootTaskID, PlanID: base.PlanID, SessionID: base.SessionID, TurnID: base.TurnID, PrincipalID: base.PrincipalID}, right: base, want: true},
		{name: "legacy right", left: base, right: InvocationScope{RootTaskID: base.RootTaskID, PlanID: base.PlanID, SessionID: base.SessionID, TurnID: base.TurnID, PrincipalID: base.PrincipalID}, want: true},
		{name: "different concrete", left: base, right: func() InvocationScope { s := base; s.ToolSnapshotID = "snapshot-b"; return s }(), want: false},
		{name: "different root", left: base, right: func() InvocationScope { s := base; s.RootTaskID = "other"; return s }(), want: false},
		{name: "different plan", left: base, right: func() InvocationScope { s := base; s.PlanID = "other"; return s }(), want: false},
		{name: "different session", left: base, right: func() InvocationScope { s := base; s.SessionID = "other"; return s }(), want: false},
		{name: "different turn", left: base, right: func() InvocationScope { s := base; s.TurnID = "other"; return s }(), want: false},
		{name: "different principal", left: base, right: func() InvocationScope { s := base; s.PrincipalID = "other"; return s }(), want: false},
		{name: "whitespace snapshot is legacy", left: func() InvocationScope { s := base; s.ToolSnapshotID = "  "; return s }(), right: base, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := InvocationScopesCompatible(tc.left, tc.right); got != tc.want {
				t.Fatalf("InvocationScopesCompatible(%+v, %+v)=%v, want %v", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

func TestSameSemanticExternalEffectBindingAllowsLegacySnapshotOnly(t *testing.T) {
	left := SemanticExternalEffectOperation{
		Scope:           InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal"},
		TenantID:        "tenant",
		UserID:          "user",
		SelectionID:     "selection",
		SelectionDigest: "selection-digest",
		BindingID:       "binding",
		RequestDigest:   "request",
	}
	right := left
	right.Scope.ToolSnapshotID = "snapshot-a"
	if !sameSemanticExternalEffectBinding(left, right) {
		t.Fatal("legacy blank operation snapshot should match a concrete retry")
	}

	left.Scope.ToolSnapshotID = "snapshot-a"
	right.Scope.ToolSnapshotID = "snapshot-b"
	if sameSemanticExternalEffectBinding(left, right) {
		t.Fatal("different concrete operation snapshots must conflict")
	}
	left.Scope.ToolSnapshotID = ""
	right = left
	right.RequestDigest = "other-request"
	if sameSemanticExternalEffectBinding(left, right) {
		t.Fatal("different request digests must conflict despite legacy snapshot compatibility")
	}
}
