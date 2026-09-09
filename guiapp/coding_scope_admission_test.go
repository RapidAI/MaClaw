package guiapp

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestScopeSelectionFailsClosedWithoutPlannerOrHostAdmission(t *testing.T) {
	tools := []MCPToolView{{Name: "unplanned", Description: "available"}}
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"scope-mcp": {entry: corelib.LocalMCPServerEntry{Name: "Scope MCP"}, running: true, tools: tools},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler: &IMMessageHandler{app: &App{localMCPManager: manager}},
	}, scopeBasedSelection: true}
	if got := cb.selectRelevantMCPToolsForTask("wording is irrelevant"); got != nil {
		t.Fatalf("scope selection exposed catalog without admission: %#v", got)
	}
}

func TestScopeSelectionUsesPlannerMCPBindingsOnly(t *testing.T) {
	tools := []MCPToolView{
		{Name: "planned_tool", Description: "opaque"},
		{Name: "unplanned_tool", Description: "opaque"},
	}
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"scope-mcp": {entry: corelib.LocalMCPServerEntry{Name: "Scope MCP"}, running: true, tools: tools},
	}}
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{
		RootTaskID: "root", ID: "plan:scope", SnapshotDigest: "snapshot:scope",
		Selections: []tool.PlannedSelection{{Provider: tool.ProviderBinding{
			Kind: "mcp", ProviderID: "scope-mcp", ImplementationID: "planned_tool",
		}}},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler: &IMMessageHandler{app: &App{localMCPManager: manager}}, staticShadowPlan: plan,
	}, scopeBasedSelection: true}
	got := cb.selectRelevantMCPToolsForTask("completely unrelated wording")
	if len(got) != 1 || got[0].ToolName != "planned_tool" {
		t.Fatalf("planner scope was not projected exactly: %#v", got)
	}
	if got[0].Score != 1 {
		t.Fatalf("planner scope result carried lexical score: %#v", got[0])
	}
}

func TestScopeSelectionMissingPlannerMCPBindingFailsClosed(t *testing.T) {
	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"scope-mcp": {entry: corelib.LocalMCPServerEntry{Name: "Scope MCP"}, running: true, tools: []MCPToolView{{Name: "other", Description: "opaque"}}},
	}}
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{
		RootTaskID: "root", ID: "plan:scope", SnapshotDigest: "snapshot:scope",
		Selections: []tool.PlannedSelection{{Provider: tool.ProviderBinding{Kind: "mcp", ProviderID: "scope-mcp", ImplementationID: "planned_tool"}}},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler: &IMMessageHandler{app: &App{localMCPManager: manager}}, staticShadowPlan: plan,
	}, scopeBasedSelection: true}
	if got := cb.selectRelevantMCPToolsForTask("anything"); got != nil {
		t.Fatalf("missing planner binding should close scope: %#v", got)
	}
}

func TestScopeSelectionUnmetPlannerFailsClosed(t *testing.T) {

	manager := &LocalMCPManager{clients: map[string]*LocalMCPClient{
		"scope-mcp": {entry: corelib.LocalMCPServerEntry{Name: "Scope MCP"}, running: true, tools: []MCPToolView{{Name: "planned_tool", Description: "opaque"}}},
	}}
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{
		RootTaskID: "root", ID: "plan:scope", SnapshotDigest: "snapshot:scope",
		Selections: []tool.PlannedSelection{{Provider: tool.ProviderBinding{Kind: "mcp", ProviderID: "scope-mcp", ImplementationID: "planned_tool"}}},
		Unmet:      []tool.UnmetNeed{{NeedID: "required-capability", ReasonCode: "catalog_incomplete"}},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		handler: &IMMessageHandler{app: &App{localMCPManager: manager}}, staticShadowPlan: plan,
	}, scopeBasedSelection: true}
	if got := cb.selectRelevantMCPToolsForTask("anything"); got != nil {
		t.Fatalf("partial planner result must close scope: %#v", got)
	}
}

func TestScopeSelectionUsesPlannerSkillBindingsOnly(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	entries := []corelib.NLSkillEntry{
		{Name: "planned-skill", SkillID: "skill:planned", Status: "active", Description: "opaque", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo planned"}}}},
		{Name: "unplanned-skill", SkillID: "skill:extra", Status: "active", Description: "opaque", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo extra"}}}},
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: entries}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{
		RootTaskID: "root", ID: "plan:scope", SnapshotDigest: "snapshot:scope",
		Selections: []tool.PlannedSelection{{Provider: tool.ProviderBinding{Kind: "skill", ProviderID: "skill:planned", ImplementationID: "planned-skill"}}},
	}}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{handler: &IMMessageHandler{app: app}, staticShadowPlan: plan}, scopeBasedSelection: true}
	got := cb.selectRelevantSkillsForTask(fmt.Sprintf("wording-%d", 1))
	if len(got) != 1 || got[0].Name != "planned-skill" {
		t.Fatalf("planner skill scope was not projected exactly: %#v", got)
	}
}

func TestPlannerInvalidationFencesDetachedOldPlan(t *testing.T) {
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{RootTaskID: "root", ID: "plan:old", SnapshotDigest: "snapshot:old"}}
	sa := &CodingSubAgent{staticShadowPlan: plan}
	cb := newCodingSubAgentCallbacks(sa, nil, "", "", nil)
	if !cb.ensureCodingPlannerSnapshotAdopted() || cb.codingToolSnapshotID() != "snapshot:old" {
		t.Fatalf("initial plan was not adopted: %q", cb.codingToolSnapshotID())
	}
	// Simulate the host detaching the plan before delivering its invalidation.
	sa.staticShadowPlan = nil
	cb.invalidateCodingToolScopeSnapshotForReason("registry_changed")
	// A late re-attachment of that exact object is still the stale generation.
	sa.staticShadowPlan = plan
	if !cb.ensureCodingPlannerSnapshotAdopted() {
		t.Fatal("stale detached plan should be handled without a hard error")
	}
	if got := cb.codingToolSnapshotID(); got != "" {
		t.Fatalf("detached stale plan was re-adopted: %q", got)
	}
}

func TestPlannerInvalidationFencesInPlaceDigestMutation(t *testing.T) {
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{RootTaskID: "root", ID: "plan:old", SnapshotDigest: "snapshot:old"}}
	sa := &CodingSubAgent{staticShadowPlan: plan}
	cb := newCodingSubAgentCallbacks(sa, nil, "", "", nil)
	if !cb.ensureCodingPlannerSnapshotAdopted() || cb.codingToolSnapshotID() != "snapshot:old" {
		t.Fatalf("initial plan was not adopted: %q", cb.codingToolSnapshotID())
	}
	cb.invalidateCodingToolScopeSnapshotForReason("registry_changed")
	// Mutating the old preparation in place must not turn it into a new
	// generation. A host replacement is represented by a new preparation
	// object, even if its digest happens to differ.
	plan.Plan.SnapshotDigest = "snapshot:mutated"
	sa.staticShadowPlan = plan
	if !cb.ensureCodingPlannerSnapshotAdopted() {
		t.Fatal("fenced in-place plan should be handled without a hard error")
	}
	if got := cb.codingToolSnapshotID(); got != "" {
		t.Fatalf("in-place mutation bypassed invalidation fence: %q", got)
	}
}

func TestScopeAdmissionRejectsMissingObservedSchemaIdentity(t *testing.T) {
	planner := codingScopeDynamicAdmission{
		kind: "mcp", providerID: "server", implementationID: "tool",
		schemaDigest: "schema-v2", contractDigest: "contract-v2",
	}
	hostMissing := codingScopeDynamicAdmission{
		kind: "mcp", providerID: "server", implementationID: "tool",
		contractDigest: "contract-v2",
	}
	if codingScopeAdmissionCompatible(hostMissing, planner) {
		t.Fatal("missing host schema identity matched a concrete planner binding")
	}
	if codingScopeMCPMatches(planner, codingSubAgentMCPToolMatch{ServerID: "server", ToolName: "tool", ContractDigest: "contract-v2"}) {
		t.Fatal("missing live MCP schema identity matched a concrete admission")
	}
	if !codingScopeAdmissionCompatible(planner, codingScopeDynamicAdmission{kind: "mcp", providerID: "server", implementationID: "tool"}) {
		t.Fatal("concrete host identity should remain compatible with a legacy planner binding")
	}
}
