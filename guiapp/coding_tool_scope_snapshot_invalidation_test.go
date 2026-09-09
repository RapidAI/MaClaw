package guiapp

import "testing"

func TestInvalidateCodingToolScopeSnapshotAllowsRebuild(t *testing.T) {
	c := &codingSubAgentCallbacks{
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a"}},
		matchedSkillsSelected:   true,
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "srv", ToolName: "tool", ContractDigest: "v1"}},
		matchedMCPToolsSelected: true,
	}
	c.finalizeCodingToolScopeSnapshot()
	if c.toolSnapshotID == "" {
		t.Fatal("expected initial snapshot")
	}
	c.invalidateCodingToolScopeSnapshotForReason("registry_changed")
	if c.toolSnapshotID != "" || c.matchedSkillsSelected || c.matchedMCPToolsSelected || len(c.matchedSkills) != 0 || len(c.matchedMCPTools) != 0 {
		t.Fatalf("snapshot was not invalidated: %#v", c)
	}
}

func TestNotifyCodingCapabilityChangedInvalidatesSubscribers(t *testing.T) {
	c := &codingSubAgentCallbacks{toolSnapshotID: "toolsnap-live"}
	id := subscribeCodingToolScope(c)
	defer unsubscribeCodingToolScope(id)
	(&App{}).NotifyCodingCapabilityChanged("mcp_health_changed")
	if c.codingToolSnapshotID() != "" {
		t.Fatalf("subscriber snapshot was not invalidated: %q", c.codingToolSnapshotID())
	}
}

func TestNotifyCodingCapabilityChangedForTenantFiltersSubscribers(t *testing.T) {
	a := &codingSubAgentCallbacks{toolSnapshotID: "a"}
	b := &codingSubAgentCallbacks{toolSnapshotID: "b"}
	a.subagent = &CodingSubAgent{dynamicInvocationIdentity: &trustedCodingInvocationIdentity{TenantID: "tenant-a"}}
	b.subagent = &CodingSubAgent{dynamicInvocationIdentity: &trustedCodingInvocationIdentity{TenantID: "tenant-b"}}
	ida, idb := subscribeCodingToolScope(a), subscribeCodingToolScope(b)
	defer unsubscribeCodingToolScope(ida)
	defer unsubscribeCodingToolScope(idb)
	(&App{}).NotifyCodingCapabilityChangedForTenant("tenant-a", "registry_changed")
	if a.codingToolSnapshotID() != "" || b.codingToolSnapshotID() != "b" {
		t.Fatalf("tenant filtering failed: a=%q b=%q", a.codingToolSnapshotID(), b.codingToolSnapshotID())
	}
}
