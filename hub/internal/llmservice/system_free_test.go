package llmservice

import "testing"

func TestEnsureSystemFreeServiceGroupCreatesAndPinsDefault(t *testing.T) {
	reg := &Registry{}
	if !EnsureSystemFreeServiceGroup(reg) {
		t.Fatal("expected change")
	}
	g := reg.FindModelServiceGroup(SystemFreeServiceGroupID)
	if g == nil {
		t.Fatal("system-free missing")
	}
	if g.AccessPolicy != AccessPolicyFree {
		t.Fatalf("policy = %q, want free", g.AccessPolicy)
	}
	if reg.SystemDefaultServiceGroupID != SystemFreeServiceGroupID {
		t.Fatalf("system default = %q", reg.SystemDefaultServiceGroupID)
	}
	if !systemFreeHasUsableModel(g) {
		t.Fatal("expected usable model")
	}
	// second call is idempotent
	if EnsureSystemFreeServiceGroup(reg) {
		t.Fatal("second ensure should not change")
	}
}

func TestEnsureSystemFreeRepairsPolicyAndEmptyProviders(t *testing.T) {
	reg := &Registry{
		SystemDefaultServiceGroupID: "other",
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           SystemFreeServiceGroupID,
			Name:         "SF",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto"}},
		}, {
			ID:   "other",
			Name: "Other",
		}},
	}
	if !EnsureSystemFreeServiceGroup(reg) {
		t.Fatal("expected change")
	}
	g := reg.FindModelServiceGroup(SystemFreeServiceGroupID)
	if g.AccessPolicy != AccessPolicyFree {
		t.Fatalf("policy = %q", g.AccessPolicy)
	}
	if len(normalizeProviderIDs(&g.Models[0])) == 0 {
		t.Fatal("expected default provider restored")
	}
	if reg.SystemDefaultServiceGroupID != SystemFreeServiceGroupID {
		t.Fatalf("default = %q", reg.SystemDefaultServiceGroupID)
	}
}

func TestProtectSystemFreeOnSaveRestoresDeletedGroup(t *testing.T) {
	old := &Registry{ModelServiceGroups: []ModelServiceGroup{SystemFreeTemplate()}}
	old.SystemDefaultServiceGroupID = SystemFreeServiceGroupID
	next := &Registry{
		SystemDefaultServiceGroupID: "paid",
		ModelServiceGroups: []ModelServiceGroup{{
			ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired,
		}},
	}
	ProtectSystemFreeOnSave(next, old)
	if next.FindModelServiceGroup(SystemFreeServiceGroupID) == nil {
		t.Fatal("system-free must be restored")
	}
	if next.SystemDefaultServiceGroupID != SystemFreeServiceGroupID {
		t.Fatalf("default = %q", next.SystemDefaultServiceGroupID)
	}
	g := next.FindModelServiceGroup(SystemFreeServiceGroupID)
	if g.AccessPolicy != AccessPolicyFree {
		t.Fatalf("policy = %q", g.AccessPolicy)
	}
}

func TestProtectSystemFreeOnSaveKeepsProviderEdits(t *testing.T) {
	old := &Registry{ModelServiceGroups: []ModelServiceGroup{SystemFreeTemplate()}}
	next := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:           SystemFreeServiceGroupID,
		Name:         "System Free Custom",
		AccessPolicy: AccessPolicyGrantRequired, // must be forced free
		Models: []ModelServiceModel{{
			Name:        "auto",
			ProviderIDs: []string{"deepseek"},
		}},
	}}}
	ProtectSystemFreeOnSave(next, old)
	g := next.FindModelServiceGroup(SystemFreeServiceGroupID)
	if g.AccessPolicy != AccessPolicyFree {
		t.Fatalf("policy = %q", g.AccessPolicy)
	}
	if g.Name != "System Free Custom" {
		t.Fatalf("name = %q", g.Name)
	}
	ids := normalizeProviderIDs(&g.Models[0])
	if len(ids) != 1 || ids[0] != "deepseek" {
		t.Fatalf("providers = %#v", ids)
	}
}

func TestEvaluateSystemFreeStatusReady(t *testing.T) {
	reg := &Registry{}
	EnsureSystemFreeServiceGroup(reg)
	st := EvaluateSystemFreeStatus(reg, nil)
	if !st.Ready {
		t.Fatalf("want ready, got reasons=%v", st.Reasons)
	}
	// missing local provider for non-builtin route
	reg.ModelServiceGroups[0].Models = []ModelServiceModel{{
		Name: "auto", ProviderIDs: []string{"missing-local"},
	}}
	st = EvaluateSystemFreeStatus(reg, map[string]struct{}{})
	if st.Ready {
		t.Fatal("should not be ready with unconfigured local provider")
	}
	st = EvaluateSystemFreeStatus(reg, map[string]struct{}{"missing-local": {}})
	if !st.Ready {
		t.Fatalf("should be ready with configured local provider, reasons=%v", st.Reasons)
	}
}

func TestIsProtectedServiceGroupID(t *testing.T) {
	if !IsProtectedServiceGroupID(SystemFreeServiceGroupID) {
		t.Fatal("system-free should be protected")
	}
	if !IsProtectedServiceGroupID(DefaultModelServiceGroupID) {
		t.Fatal("default should be protected")
	}
	if !IsProtectedServiceGroupID(MaClawOfficialServiceGroupID) {
		t.Fatal("maclaw official group should be protected")
	}
	if IsProtectedServiceGroupID("custom-group") {
		t.Fatal("custom should not be protected")
	}
}
