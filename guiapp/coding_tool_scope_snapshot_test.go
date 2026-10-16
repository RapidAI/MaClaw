package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCodingToolScopeSnapshotStableAcrossOrdering(t *testing.T) {
	first := &codingSubAgentCallbacks{
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:b", QualifiedID: "b"}, {StableID: "skill:a", QualifiedID: "a"}},
		matchedSkillsSelected:   true,
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "srv", ToolName: "lookup", ContractDigest: "v1"}},
		matchedMCPToolsSelected: true,
	}
	second := &codingSubAgentCallbacks{
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a", QualifiedID: "a"}, {StableID: "skill:b", QualifiedID: "b"}},
		matchedSkillsSelected:   true,
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "srv", ToolName: "lookup", ContractDigest: "v1"}},
		matchedMCPToolsSelected: true,
	}
	first.finalizeCodingToolScopeSnapshot()
	second.finalizeCodingToolScopeSnapshot()
	if first.toolSnapshotID == "" || first.toolSnapshotID != second.toolSnapshotID {
		t.Fatalf("snapshot IDs differ for equivalent sets: %q vs %q", first.toolSnapshotID, second.toolSnapshotID)
	}
}

func TestCodingToolScopeSnapshotChangesWhenAdmittedToolChanges(t *testing.T) {
	base := &codingSubAgentCallbacks{
		matchedSkillsSelected:   true,
		matchedMCPToolsSelected: true,
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a"}},
	}
	changed := &codingSubAgentCallbacks{
		matchedSkillsSelected:   true,
		matchedMCPToolsSelected: true,
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:b"}},
	}
	base.finalizeCodingToolScopeSnapshot()
	changed.finalizeCodingToolScopeSnapshot()
	if base.toolSnapshotID == changed.toolSnapshotID {
		t.Fatalf("snapshot ID did not change after admitted tool changed: %q", base.toolSnapshotID)
	}
}

func TestCodingToolScopeSnapshotRequiresBothSelections(t *testing.T) {
	c := &codingSubAgentCallbacks{matchedSkillsSelected: true, matchedSkills: []codingSubAgentSkillMatch{{StableID: "skill:a"}}}
	c.finalizeCodingToolScopeSnapshot()
	if c.toolSnapshotID != "" {
		t.Fatalf("snapshot finalized before MCP selection: %q", c.toolSnapshotID)
	}
}

func TestCodingToolScopeSnapshotAdoptsPlannerDigest(t *testing.T) {
	c := &codingSubAgentCallbacks{}
	if !c.adoptCodingToolSnapshotDigest("catalog-digest-v2") || c.codingToolSnapshotID() != "catalog-digest-v2" {
		t.Fatalf("planner digest was not adopted: %q", c.codingToolSnapshotID())
	}
	if c.adoptCodingToolSnapshotDigest("catalog-digest-v3") {
		t.Fatal("conflicting planner digest was accepted")
	}
}

func TestCodingToolScopeSnapshotPlannerDigestWinsOverFallback(t *testing.T) {
	c := &codingSubAgentCallbacks{
		matchedSkillsSelected:   true,
		matchedMCPToolsSelected: true,
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a"}},
	}
	c.finalizeCodingToolScopeSnapshot()
	if !strings.HasPrefix(c.codingToolSnapshotID(), "toolsnap:") {
		t.Fatalf("expected deterministic fallback snapshot, got %q", c.codingToolSnapshotID())
	}
	if !c.adoptCodingToolSnapshotDigest("planner-catalog-v4") || c.codingToolSnapshotID() != "planner-catalog-v4" {
		t.Fatalf("planner digest did not replace fallback: %q", c.codingToolSnapshotID())
	}
	c.finalizeCodingToolScopeSnapshot()
	if c.codingToolSnapshotID() != "planner-catalog-v4" {
		t.Fatalf("finalize replaced adopted planner digest: %q", c.codingToolSnapshotID())
	}
	if c.adoptCodingToolSnapshotDigest("planner-catalog-v5") {
		t.Fatal("conflicting planner digest was accepted after fallback replacement")
	}
}

func TestCodingToolScopeSnapshotBindsPolicyIdentity(t *testing.T) {
	newCallback := func(policy codingruntime.PolicySnapshot) *codingSubAgentCallbacks {
		return &codingSubAgentCallbacks{
			subagent:                &CodingSubAgent{runtimeAttempt: &codingruntime.Attempt{Policy: policy}},
			matchedSkillsSelected:   true,
			matchedMCPToolsSelected: true,
			matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a", ContractDigest: "contract-v1"}},
			matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "server", ToolName: "read", ContractDigest: "contract-v1"}},
		}
	}
	first := newCallback(codingruntime.PolicySnapshot{Digest: "policy-a", Mode: "local", ProjectRoot: "repo", ReadOnly: true})
	second := newCallback(codingruntime.PolicySnapshot{Digest: "policy-b", Mode: "local", ProjectRoot: "repo", ReadOnly: true})
	first.finalizeCodingToolScopeSnapshot()
	second.finalizeCodingToolScopeSnapshot()
	if first.codingToolSnapshotID() == "" || second.codingToolSnapshotID() == "" {
		t.Fatal("policy-bound snapshots were not finalized")
	}
	if first.codingToolSnapshotID() == second.codingToolSnapshotID() {
		t.Fatalf("policy identity did not change snapshot: %q", first.codingToolSnapshotID())
	}
}

func TestCodingToolScopeSnapshotIgnoresPromptMetadata(t *testing.T) {
	first := &codingSubAgentCallbacks{
		task:                    &TaskItem{Title: "inspect repository", Description: "read files"},
		reqCtx:                  "first wording",
		designCtx:               "first design",
		prevOutputs:             []string{"first output"},
		matchedSkillsSelected:   true,
		matchedMCPToolsSelected: true,
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a", Description: "first description", Score: 0.1}},
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "server", ToolName: "read", Description: "first description", Score: 0.1}},
	}
	second := &codingSubAgentCallbacks{
		task:                    &TaskItem{Title: "different request", Description: "write files"},
		reqCtx:                  "second wording",
		designCtx:               "second design",
		prevOutputs:             []string{"second output"},
		matchedSkillsSelected:   true,
		matchedMCPToolsSelected: true,
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a", Description: "second description", Score: 0.9}},
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "server", ToolName: "read", Description: "second description", Score: 0.9}},
	}
	first.finalizeCodingToolScopeSnapshot()
	second.finalizeCodingToolScopeSnapshot()
	if first.codingToolSnapshotID() == "" || first.codingToolSnapshotID() != second.codingToolSnapshotID() {
		t.Fatalf("prompt metadata changed snapshot: %q vs %q", first.codingToolSnapshotID(), second.codingToolSnapshotID())
	}
}

func TestCodingToolScopeSnapshotFinalizesWhenOtherSideWasPreselected(t *testing.T) {
	c := &codingSubAgentCallbacks{
		matchedSkills:           []codingSubAgentSkillMatch{{StableID: "skill:a"}},
		matchedMCPTools:         []codingSubAgentMCPToolMatch{{ServerID: "server", ToolName: "read"}},
		matchedMCPToolsSelected: true,
	}
	// The MCP side was populated by an earlier host preparation step. The
	// skills fast path must still invoke the finalizer; otherwise both sides
	// can be selected while the production snapshot ID remains empty.
	c.ensureMatchedSkillsSelected()
	if c.codingToolSnapshotID() == "" {
		t.Fatal("snapshot was not finalized after preselected MCP side")
	}
}

func TestCodingPlannerSnapshotAdoptedAtFirstModelRequest(t *testing.T) {
	sa := &CodingSubAgent{
		handler: &IMMessageHandler{},
		staticShadowPlan: &codingStaticPlanPreparation{Plan: tool.ToolPlan{
			RootTaskID: "root", ID: "plan:root", CatalogDigest: "catalog-v1", SnapshotDigest: "plan-snapshot-v1",
		}},
	}
	cb := newCodingSubAgentCallbacks(sa, &TaskItem{Title: "inspect"}, "", "", nil)
	_ = cb.BuildToolsForModelRequest("first wording", 0)
	if got := cb.codingToolSnapshotID(); got != "plan-snapshot-v1" {
		t.Fatalf("first model request did not adopt planner snapshot: %q", got)
	}
}

func TestCodingPlannerSnapshotConflictFailsClosed(t *testing.T) {
	sa := &CodingSubAgent{
		staticShadowPlan: &codingStaticPlanPreparation{Plan: tool.ToolPlan{
			RootTaskID: "root", ID: "plan:root", SnapshotDigest: "plan-snapshot-v1",
		}},
	}
	cb := newCodingSubAgentCallbacks(sa, nil, "", "", nil)
	if !cb.ensureCodingPlannerSnapshotAdopted() || cb.codingToolSnapshotID() != "plan-snapshot-v1" {
		t.Fatalf("initial planner snapshot was not adopted: %q", cb.codingToolSnapshotID())
	}
	sa.staticShadowPlan.Plan.SnapshotDigest = "plan-snapshot-v2"
	if cb.ensureCodingPlannerSnapshotAdopted() {
		t.Fatal("conflicting planner snapshot was accepted")
	}
	if got := cb.codingToolSnapshotID(); got != "" {
		t.Fatalf("planner conflict should hide snapshot and fail closed, got %q", got)
	}
}

func TestCodingPlannerSnapshotInvalidationDoesNotReadoptStalePlan(t *testing.T) {
	plan := &codingStaticPlanPreparation{Plan: tool.ToolPlan{RootTaskID: "root", ID: "plan:root", SnapshotDigest: "plan-snapshot-v1"}}
	sa := &CodingSubAgent{staticShadowPlan: plan}
	cb := newCodingSubAgentCallbacks(sa, nil, "", "", nil)
	if !cb.ensureCodingPlannerSnapshotAdopted() {
		t.Fatal("initial planner snapshot was not adopted")
	}
	cb.invalidateCodingToolScopeSnapshotForReason("catalog_changed")
	if cb.codingToolSnapshotID() != "" {
		t.Fatalf("invalidation should clear snapshot, got %q", cb.codingToolSnapshotID())
	}
	if !cb.ensureCodingPlannerSnapshotAdopted() {
		t.Fatal("stale-plan guard should be a non-error while replacement is pending")
	}
	if cb.codingToolSnapshotID() != "" {
		t.Fatalf("stale planner plan was re-adopted after invalidation: %q", cb.codingToolSnapshotID())
	}
	sa.staticShadowPlan = &codingStaticPlanPreparation{Plan: tool.ToolPlan{RootTaskID: "root", ID: "plan:root-v2", SnapshotDigest: "plan-snapshot-v2"}}
	if !cb.ensureCodingPlannerSnapshotAdopted() || cb.codingToolSnapshotID() != "plan-snapshot-v2" {
		t.Fatalf("replacement planner plan was not adopted: %q", cb.codingToolSnapshotID())
	}
}
