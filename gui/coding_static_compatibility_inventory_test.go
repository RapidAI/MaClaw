package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestCodingStaticCompatibilityInventoryCoversRenderedLocalSurface(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true},
		task:     &TaskItem{Title: "implement a feature", Description: "update the repository"},
	}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools("work")) {
		if !codingStaticCompatibilityInventoryHas(codingStaticCompatibilityHostLocal, name) {
			t.Fatalf("local rendered compatibility tool %q is missing inventory ownership", name)
		}
	}
}

func TestCodingStaticCompatibilityInventoryHasOneCompleteOwnerPerTool(t *testing.T) {
	seen := make(map[string]struct{}, len(codingStaticCompatibilityInventory))
	for _, item := range codingStaticCompatibilityInventory {
		key := item.HostKind + "\x00" + item.Name
		if item.HostKind == "" || item.Name == "" || item.Capability == "" || item.Effect == "" || item.BindingScope == "" {
			t.Fatalf("incomplete static compatibility inventory item: %#v", item)
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate static compatibility inventory owner %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestCodingStaticCompatibilityInventoryCoversAllPostureAndRoleSurfaces(t *testing.T) {
	for _, requestKind := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, codingRequestImplementation} {
		for _, role := range []codingSubAgentRole{"", codingRoleWorker, codingRoleExplorer, codingRoleReviewer} {
			t.Run(string(requestKind)+"/"+string(role), func(t *testing.T) {
				cb := &codingSubAgentCallbacks{
					subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true, nestDepth: 1, role: role},
					task:     &TaskItem{Title: "coding work", Description: "exercise surface", RequestKind: requestKind},
				}
				for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 0)) {
					if !codingStaticCompatibilityInventoryHas(codingStaticCompatibilityHostLocal, name) {
						t.Fatalf("local kind=%s role=%s rendered tool %q has no inventory owner", requestKind, role, name)
					}
				}
			})
		}
	}

	for _, requestKind := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, codingRequestImplementation} {
		for _, role := range []codingSubAgentRole{"", codingRoleWorker, codingRoleExplorer, codingRoleReviewer} {
			t.Run("remote/"+string(requestKind)+"/"+string(role), func(t *testing.T) {
				remote := &RemoteCodingSubAgent{nestDepth: 1, role: role}
				switch requestKind {
				case codingRequestInquiry:
					remote.readOnlyInquiry = true
				case codingRequestOperational:
					remote.operationalRequest = true
				}
				cb := &remoteCodingCallbacks{agent: remote}
				for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 0)) {
					if !codingStaticCompatibilityInventoryHas(codingStaticCompatibilityHostRemote, name) {
						t.Fatalf("remote kind=%s role=%s rendered tool %q has no inventory owner", requestKind, role, name)
					}
				}
			})
		}
	}
}

func TestCodingStaticCompatibilityModelSurfaceExcludesUncorrelatedLocalEffects(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true},
		task:     &TaskItem{Title: "implement", Description: "change source", RequestKind: codingRequestImplementation},
	}
	assertCodingStaticCompatibilitySurfaceHasOnlyReadOrControlPlane(t, codingStaticCompatibilityHostLocal, cb.BuildToolsForModelRequest("implement", 0))
	for _, name := range []string{"edit_file", "edit_lines", "write_file", "bash", "download_file"} {
		result := cb.ExecuteToolCallWithContext(name, `{}`, "blocked-"+name, agent.ToolCallExecutionContext{})
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
			t.Fatalf("uncorrelated local effect %q reached dispatcher: %#v", name, result)
		}
	}
	result := cb.ExecuteToolCallWithContext(codingSubAgentSpawnToolName, `{"task":"inspect"}`, "blocked-spawn", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("uncorrelated local spawn reached dispatcher: %#v", result)
	}
	result = cb.ExecuteToolCallWithContext(reportLocalizationToolName, `{}`, "blocked-localization", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("uncorrelated local localization report reached dispatcher: %#v", result)
	}
}

func TestCodingStaticCompatibilityModelSurfaceExcludesUncorrelatedRemoteEffects(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	assertCodingStaticCompatibilitySurfaceHasOnlyReadOrControlPlane(t, codingStaticCompatibilityHostRemote, cb.BuildToolsForModelRequest("implement", 0))
	for _, name := range []string{"ssh_write_file", "ssh_edit_file", "ssh_bash", "download_file"} {
		result := cb.ExecuteToolCallWithContext(name, `{}`, "blocked-"+name, agent.ToolCallExecutionContext{})
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
			t.Fatalf("uncorrelated remote effect %q reached dispatcher: %#v", name, result)
		}
	}
	result := cb.ExecuteToolCallWithContext(codingSubAgentSpawnToolName, `{"task":"inspect"}`, "blocked-spawn", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("uncorrelated remote spawn reached dispatcher: %#v", result)
	}
	result = cb.ExecuteToolCallWithContext(reportLocalizationToolName, `{}`, "blocked-localization", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("uncorrelated remote localization report reached dispatcher: %#v", result)
	}
}

func TestCorrelatedRemoteCodingSurfaceIncludesEffectfulTools(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{correlatedRemoteExecution: true}}
	names := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("implement", 0))
	for _, want := range []string{"ssh_read_file", "ssh_write_file", "ssh_edit_file", "ssh_bash", "ssh_list_dir"} {
		if !containsStringForTest(names, want) {
			t.Fatalf("correlated remote surface omitted %s: %v", want, names)
		}
	}
	// Even with the full surface, effectful calls remain correlation-bound.
	epoch := cb.BeginToolSurfaceEpoch(0)
	got := cb.ExecuteToolCallWithContext("ssh_bash", `{"command":"true"}`, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if got.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(got.Result, "static_response_correlation_missing") {
		t.Fatalf("uncorrelated effectful dispatch was not rejected: %#v", got)
	}
}

func TestCodingStaticCompatibilityInventoryCoversRenderedRemoteSurface(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools("work")) {
		if !codingStaticCompatibilityInventoryHas(codingStaticCompatibilityHostRemote, name) {
			t.Fatalf("remote rendered compatibility tool %q is missing inventory ownership", name)
		}
	}
}

func TestCodingStaticCompatibilityRequestSurfacePreservesFrozenHorizonHostTools(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"computer_observe", "computer_done"} {
		if err := reg.Register(RegisteredTool{Name: name, Description: name, InputSchema: map[string]interface{}{}}); err != nil {
			t.Fatal(err)
		}
	}
	sa := NewCodingSubAgent(&IMMessageHandler{registry: reg}, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	sa.SetHorizonPosture([]string{"computer_observe", "computer_done"})
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "inspect desktop"}}
	names := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("inspect", 0))
	if !containsStringForTest(names, "computer_observe") || !containsStringForTest(names, "computer_done") {
		t.Fatalf("horizon surface=%v", names)
	}
	if result := cb.ExecuteToolCallWithContext("computer_observe", `{}`, "call-1", agent.ToolCallExecutionContext{}); strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("rendered Horizon host tool was rejected by static fence: %#v", result)
	}
	if result := cb.ExecuteToolCallWithContext("computer_click", `{}`, "call-2", agent.ToolCallExecutionContext{}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("unrendered Horizon host tool result=%#v", result)
	}
}

func TestCodingStaticCompatibilityRequestSurfaceRejectsStaleAndPostureRemovedTools(t *testing.T) {
	task := &TaskItem{Title: "implement", Description: "change source"}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}, task: task}
	_ = cb.BuildToolsForModelRequest("implement", 0)
	if result := cb.ExecuteToolCallWithContext("not_rendered", `{}`, "call-1", agent.ToolCallExecutionContext{}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("invented local name result=%#v", result)
	}
	task.RequestKind = codingRequestInquiry
	_ = cb.BuildToolsForModelRequest("inspect", 1)
	if result := cb.ExecuteToolCallWithContext("write_file", `{"path":"x","content":"x"}`, "call-2", agent.ToolCallExecutionContext{}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("stale local write result=%#v", result)
	}
	if result := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "call-3", agent.ToolCallExecutionContext{}); strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("rendered local read was rejected by surface fence: %#v", result)
	}
}

func TestNextCodingStaticCompatibilityRevisionOnlyAdvancesOnNameSetChange(t *testing.T) {
	first := map[string]struct{}{"read_file": {}, "todo_write": {}}
	if got := nextCodingStaticCompatibilityRevision(0, nil, first); got != 1 {
		t.Fatalf("first install revision=%d", got)
	}
	if got := nextCodingStaticCompatibilityRevision(1, first, map[string]struct{}{"todo_write": {}, "read_file": {}}); got != 1 {
		t.Fatalf("identical names must keep revision, got %d", got)
	}
	if got := nextCodingStaticCompatibilityRevision(1, first, map[string]struct{}{"read_file": {}}); got != 2 {
		t.Fatalf("name-set replacement revision=%d", got)
	}
}

func TestInstallCodingStaticCompatibilitySurfaceReusesNameSet(t *testing.T) {
	first := map[string]struct{}{"read_file": {}, "todo_write": {}}
	rev, names := installCodingStaticCompatibilitySurface(0, nil, first)
	if rev != 1 || len(names) != 2 {
		t.Fatalf("first install rev=%d names=%v", rev, names)
	}
	same := map[string]struct{}{"todo_write": {}, "read_file": {}}
	rev, kept := installCodingStaticCompatibilitySurface(rev, names, same)
	if rev != 1 {
		t.Fatalf("same names rev=%d", rev)
	}
	names["probe"] = struct{}{}
	if _, ok := kept["probe"]; !ok {
		t.Fatal("identical names must keep the previous map rather than installing a copy")
	}
	next := map[string]struct{}{"read_file": {}}
	rev, replaced := installCodingStaticCompatibilitySurface(rev, names, next)
	if rev != 2 || len(replaced) != 1 {
		t.Fatalf("name-set replacement rev=%d names=%v", rev, replaced)
	}
	if _, ok := replaced["probe"]; ok {
		t.Fatal("replacement must install the new name set")
	}
}

func TestCodingStaticCompatibilitySurfaceObservationTracksActualReplacement(t *testing.T) {
	task := &TaskItem{Title: "implement", Description: "change source", RequestKind: codingRequestImplementation}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: t.TempDir()}, task: task}
	first := cb.BuildToolsForModelRequest("implement", 0)
	observationA := cb.lastStaticCompatibilitySurfaceObservation()
	if observationA.HostKind != codingStaticCompatibilityHostLocal || observationA.StaticRevision != 1 || observationA.Posture != string(codingRequestImplementation) {
		t.Fatalf("first local observation=%#v", observationA)
	}
	if !sameCodingStaticSurfaceNames(observationA.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(first)) {
		t.Fatalf("observation names=%v rendered=%v", observationA.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(first))
	}
	if observationA.ShadowState != "not_prepared" || observationA.PlanID != "" || observationA.RootTaskID != "" || observationA.TurnID != "" {
		t.Fatalf("unbound compatibility surface claimed a shadow plan: %#v", observationA)
	}
	task.RequestKind = codingRequestInquiry
	second := cb.BuildToolsForModelRequest("inspect", 1)
	observationB := cb.lastStaticCompatibilitySurfaceObservation()
	if observationB.StaticRevision != 2 || observationB.Posture != string(codingRequestInquiry) {
		t.Fatalf("replacement local observation=%#v", observationB)
	}
	if !sameCodingStaticSurfaceNames(observationB.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(second)) {
		t.Fatalf("replacement observation names=%v rendered=%v", observationB.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(second))
	}
	if containsStringForTest(observationB.RenderedToolNames, "write_file") {
		t.Fatalf("inquiry observation retained removed write capability: %#v", observationB)
	}
}

func TestCodingStaticCompatibilitySurfaceObservationAuditsDesensitizedPlanReference(t *testing.T) {
	audit, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	prepared, err := prepareCodingStaticShadowPlan(codingStaticExecutionEnvelope{
		Identity: identity, Workspace: codingStaticWorkspaceBinding{WorkspaceHandle: "host-issued-handle", HostKind: "local"},
		Posture: codingRequestInquiry, Role: codingRoleWorker,
	}, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{auditLog: audit}
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:                   &IMMessageHandler{app: app},
			projectPath:               t.TempDir(),
			dynamicInvocationIdentity: identity,
			staticShadowPlan:          &prepared,
		},
		task: &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	_ = cb.BuildToolsForModelRequest("inspect", 0)
	entries, err := audit.Query(security.AuditFilter{ToolName: "coding_static_surface"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("surface audit entries=%#v err=%v", entries, err)
	}
	entry := entries[0]
	if entry.PolicyAction != security.PolicyAudit || entry.RiskLevel != security.RiskLow || entry.Source != "coding_subagent" {
		t.Fatalf("surface audit policy=%#v", entry)
	}
	if entry.Arguments["root_task_id"] != "root" || entry.Arguments["turn_id"] != "turn" || entry.Arguments["plan_id"] != prepared.Plan.ID || entry.Arguments["shadow_state"] != "prepared" {
		t.Fatalf("surface audit arguments=%#v", entry.Arguments)
	}
	if _, leaked := entry.Arguments["workspace_handle"]; leaked {
		t.Fatalf("surface audit leaked workspace binding: %#v", entry.Arguments)
	}
	if _, leaked := entry.Arguments["project_path"]; leaked {
		t.Fatalf("surface audit leaked project path: %#v", entry.Arguments)
	}
	legacyOnly, ok := entry.Arguments["legacy_only_capabilities"].([]interface{})
	if !ok || len(legacyOnly) == 0 {
		t.Fatalf("surface audit omitted legacy-vs-shadow capability difference: %#v", entry.Arguments)
	}
	if _, leaked := entry.Arguments["workspace_handle"]; leaked {
		t.Fatalf("surface audit leaked workspace binding: %#v", entry.Arguments)
	}
}

func TestCodingStaticCompatibilityCapabilityDifferenceUsesCapabilitiesNotNames(t *testing.T) {
	prepared, err := prepareCodingStaticShadowPlan(testCodingStaticEnvelope("workspace", codingRequestInquiry), nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	definitions := []map[string]interface{}{
		agent.ToolDef("Glob", "", map[string]interface{}{}, nil),
		agent.ToolDef("ripgrep", "", map[string]interface{}{}, nil),
		agent.ToolDef("read_file", "", map[string]interface{}{}, nil),
		agent.ToolDef("git_diff", "", map[string]interface{}{}, nil),
	}
	legacyOnly, shadowOnly := codingStaticCompatibilityCapabilityDifference(codingStaticCompatibilityHostLocal, definitions, prepared.Plan)
	if len(legacyOnly) != 0 || len(shadowOnly) != 0 {
		t.Fatalf("equivalent capability classes diff legacy=%v shadow=%v", legacyOnly, shadowOnly)
	}
	definitions = append(definitions, agent.ToolDef("write_file", "", map[string]interface{}{}, nil))
	legacyOnly, shadowOnly = codingStaticCompatibilityCapabilityDifference(codingStaticCompatibilityHostLocal, definitions, prepared.Plan)
	if !containsStringForTest(legacyOnly, string(tool.CapabilityFSWriteLocal)) || len(shadowOnly) != 0 {
		t.Fatalf("write compatibility gap legacy=%v shadow=%v", legacyOnly, shadowOnly)
	}
}

func TestCodingStaticCompatibilityRequestSurfaceRejectsUnrenderedLocalAliases(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	_ = cb.BuildToolsForModelRequest("inspect", 0)
	for _, alias := range []string{"grep_search", "search_files", "READ_FILE"} {
		result := cb.ExecuteToolCallWithContext(alias, `{}`, "call-"+alias, agent.ToolCallExecutionContext{})
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
			t.Fatalf("unrendered local alias %q result=%#v", alias, result)
		}
	}
}

func TestCodingStaticCompatibilityRequestEpochRejectsSupersededLocalResponse(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	epochA := cb.BeginToolSurfaceEpoch(0)
	if epochA == "" {
		t.Fatal("first local static request did not receive an in-process epoch")
	}
	_ = cb.BuildToolsForModelRequest("inspect again", 1)
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochB == "" || epochA == epochB {
		t.Fatalf("local replacement epochs A=%q B=%q", epochA, epochB)
	}
	stale := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "old-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "static_surface_unavailable") {
		t.Fatalf("superseded local request reached legacy dispatcher: %#v", stale)
	}
	current := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "current-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochB})
	if strings.Contains(current.Result, "static_surface_unavailable") {
		t.Fatalf("current local request was rejected by its own epoch: %#v", current)
	}
}

func TestCodingStaticCompatibilityRequestRendererPreservesCurrentLocalEpoch(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	// This is the real RunLoop order: epoch first, then request-bound render.
	// Clearing the epoch in setStaticCompatibilitySurface made the model's own
	// response fail the static-surface execution fence.
	epochA := cb.BeginToolSurfaceEpoch(0)
	if epochA == "" {
		t.Fatal("first local request did not receive an epoch")
	}
	if rendered := cb.BuildToolsForModelRequest("inspect", 0); len(rendered) == 0 {
		t.Fatal("current local request rendered an empty surface")
	}
	if result := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "current-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA}); strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("current local response was rejected after render: %#v", result)
	}

	// A successor advances the epoch before replacing the definitions, so the
	// predecessor remains non-executable even if the same name is rendered.
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochB == "" || epochA == epochB {
		t.Fatalf("local replacement epochs A=%q B=%q", epochA, epochB)
	}
	_ = cb.BuildToolsForModelRequest("inspect again", 1)
	stale := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "stale-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "static_surface_unavailable") {
		t.Fatalf("predecessor local response reached dispatcher: %#v", stale)
	}
}

func TestCodingStaticCompatibilityQuarantinesAmbiguousLocalDelivery(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	if rendered := cb.BuildToolsForModelRequest("inspect", 0); len(rendered) == 0 {
		t.Fatal("initial local compatibility surface is empty")
	}
	cb.OnToolSurfaceAttemptFinished(agent.ToolCallExecutionContext{}, agent.ToolSurfaceAmbiguousDelivery)
	if rendered := cb.BuildToolsForModelRequest("retry", 1); len(rendered) != 0 {
		t.Fatalf("quarantined local callback rendered successor surface: %v", codingSubAgentToolDefinitionNamesForTest(rendered))
	}
	result := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "late-call", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("quarantined local late call result=%#v", result)
	}
}

func TestRemoteCodingStaticCompatibilityRequestSurfaceRejectsStaleAndInventedTools(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	_ = cb.BuildToolsForModelRequest("implement", 0)
	if result := cb.ExecuteToolCallWithContext("read_file", `{}`, "call-1", agent.ToolCallExecutionContext{}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("cross-host local name result=%#v", result)
	}
	cb.agent.readOnlyInquiry = true
	_ = cb.BuildToolsForModelRequest("inspect", 1)
	if result := cb.ExecuteToolCallWithContext("ssh_write_file", `{"path":"x","content":"x"}`, "call-2", agent.ToolCallExecutionContext{}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("stale remote write result=%#v", result)
	}
	if result := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "call-3", agent.ToolCallExecutionContext{}); strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("rendered remote read was rejected by surface fence: %#v", result)
	}
}

func TestRemoteCodingStaticCompatibilitySurfaceObservationIsExplicitlyUnplanned(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	rendered := cb.BuildToolsForModelRequest("inspect", 0)
	observation := cb.lastStaticCompatibilitySurfaceObservation()
	if observation.HostKind != codingStaticCompatibilityHostRemote || observation.StaticRevision != 1 || observation.Posture != string(codingRequestInquiry) {
		t.Fatalf("remote observation=%#v", observation)
	}
	if observation.ShadowState != "not_prepared" || observation.PlanID != "" || observation.CatalogGeneration != 0 {
		t.Fatalf("remote S2 compatibility belt claimed a local shadow plan: %#v", observation)
	}
	if !sameCodingStaticSurfaceNames(observation.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(rendered)) {
		t.Fatalf("remote observation names=%v rendered=%v", observation.RenderedToolNames, codingSubAgentToolDefinitionNamesForTest(rendered))
	}
}

func sameCodingStaticSurfaceNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, name := range left {
		seen[name] = struct{}{}
	}
	for _, name := range right {
		if _, ok := seen[name]; !ok {
			return false
		}
	}
	return true
}

func assertCodingStaticCompatibilitySurfaceHasOnlyReadOrControlPlane(t *testing.T, hostKind string, definitions []map[string]interface{}) {
	t.Helper()
	for _, name := range codingSubAgentToolDefinitionNamesForTest(definitions) {
		item, ok := codingStaticCompatibilityInventoryLookup(hostKind, name)
		if !ok {
			if isCodingStaticCompatibilityExternalDefinition(map[string]interface{}{"function": map[string]interface{}{"name": name}}) {
				continue
			}
			t.Fatalf("rendered %s tool %q has no inventory item", hostKind, name)
		}
		if !codingStaticCompatibilityItemAllowedWithoutTransportCorrelation(item) {
			t.Fatalf("rendered %s effectful compatibility tool %q item=%#v", hostKind, name, item)
		}
	}
}

func TestRemoteCodingStaticCompatibilityRequestEpochRejectsSupersededResponse(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	epochA := cb.BeginToolSurfaceEpoch(0)
	if epochA == "" {
		t.Fatal("first remote static request did not receive an in-process epoch")
	}
	_ = cb.BuildToolsForModelRequest("inspect again", 1)
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochB == "" || epochA == epochB {
		t.Fatalf("remote replacement epochs A=%q B=%q", epochA, epochB)
	}
	stale := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "old-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "static_surface_unavailable") {
		t.Fatalf("superseded remote request reached legacy dispatcher: %#v", stale)
	}
	current := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "current-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochB})
	if strings.Contains(current.Result, "static_surface_unavailable") {
		t.Fatalf("current remote request was rejected by its own epoch: %#v", current)
	}
}

func TestRemoteCodingStaticCompatibilityRequestRendererPreservesCurrentEpoch(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	epochA := cb.BeginToolSurfaceEpoch(0)
	if epochA == "" {
		t.Fatal("first remote request did not receive an epoch")
	}
	if rendered := cb.BuildToolsForModelRequest("inspect", 0); len(rendered) == 0 {
		t.Fatal("current remote request rendered an empty surface")
	}
	if result := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "current-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA}); strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("current remote response was rejected after render: %#v", result)
	}

	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochB == "" || epochA == epochB {
		t.Fatalf("remote replacement epochs A=%q B=%q", epochA, epochB)
	}
	_ = cb.BuildToolsForModelRequest("inspect again", 1)
	stale := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "stale-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "static_surface_unavailable") {
		t.Fatalf("predecessor remote response reached dispatcher: %#v", stale)
	}
}

func TestRemoteCodingBuildToolsReturnsRequestLocalDefinitions(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	first := cb.BuildTools("inspect")
	second := cb.BuildTools("inspect again")
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected remote coding definitions")
	}
	first[0]["request_local_mutation"] = true
	function, _ := first[0]["function"].(map[string]interface{})
	if function != nil {
		function["request_local_mutation"] = true
	}
	if _, leaked := second[0]["request_local_mutation"]; leaked {
		t.Fatal("top-level mutation leaked into successor remote request")
	}
	secondFunction, _ := second[0]["function"].(map[string]interface{})
	if _, leaked := secondFunction["request_local_mutation"]; leaked {
		t.Fatal("nested mutation leaked into successor remote request")
	}
}

func TestRemoteCodingStaticCompatibilityQuarantinesAmbiguousDelivery(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	if rendered := cb.BuildToolsForModelRequest("inspect", 0); len(rendered) == 0 {
		t.Fatal("initial remote compatibility surface is empty")
	}
	cb.OnToolSurfaceAttemptFinished(agent.ToolCallExecutionContext{}, agent.ToolSurfaceAmbiguousDelivery)
	if rendered := cb.BuildToolsForModelRequest("retry", 1); len(rendered) != 0 {
		t.Fatalf("quarantined remote callback rendered successor surface: %v", codingSubAgentToolDefinitionNamesForTest(rendered))
	}
	result := cb.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "late-call", agent.ToolCallExecutionContext{})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("quarantined remote late call result=%#v", result)
	}
}

func TestCodingStaticCompatibilityQuarantineRejectsEpochIssuedBeforeInitialRender(t *testing.T) {
	// RunLoop issues the request epoch before calling the renderer. If the
	// request becomes ambiguous while rendering/preparing fails, no previously
	// issued compatibility epoch may survive to admit a delayed tool call.
	local := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	localEpoch := local.BeginToolSurfaceEpoch(0)
	if localEpoch == "" {
		t.Fatal("local first request did not receive an epoch before render")
	}
	local.OnToolSurfaceAttemptFinished(agent.ToolCallExecutionContext{SurfaceEpoch: localEpoch}, agent.ToolSurfaceAmbiguousDelivery)
	if rendered := local.BuildToolsForModelRequest("retry", 1); len(rendered) != 0 {
		t.Fatalf("quarantined local callback rendered a successor surface: %v", codingSubAgentToolDefinitionNamesForTest(rendered))
	}
	if result := local.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "late-local", agent.ToolCallExecutionContext{SurfaceEpoch: localEpoch}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("quarantined local pre-render epoch reached dispatcher: %#v", result)
	}

	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{readOnlyInquiry: true}}
	remoteEpoch := remote.BeginToolSurfaceEpoch(0)
	if remoteEpoch == "" {
		t.Fatal("remote first request did not receive an epoch before render")
	}
	remote.OnToolSurfaceAttemptFinished(agent.ToolCallExecutionContext{SurfaceEpoch: remoteEpoch}, agent.ToolSurfaceAmbiguousDelivery)
	if rendered := remote.BuildToolsForModelRequest("retry", 1); len(rendered) != 0 {
		t.Fatalf("quarantined remote callback rendered a successor surface: %v", codingSubAgentToolDefinitionNamesForTest(rendered))
	}
	if result := remote.ExecuteToolCallWithContext("ssh_read_file", `{"path":"missing"}`, "late-remote", agent.ToolCallExecutionContext{SurfaceEpoch: remoteEpoch}); result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("quarantined remote pre-render epoch reached dispatcher: %#v", result)
	}
}

func TestCodingStaticCompatibilityRunLoopUsesFirstLocalEpochAndRebuildsAfterToolRound(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, extractToolName(definition))
		}
		surfaces = append(surfaces, names)
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_local","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"missing\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{cfg: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, projectPath: t.TempDir()},
		task:     &TaskItem{Title: "inspect", Description: "read repository", RequestKind: codingRequestInquiry},
	}
	result := agent.RunLoop(cb, "inspect", nil, server.Client())
	if result.Error != "" || result.Text != "done" || result.ToolCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(surfaces) < 2 || !containsStringForTest(surfaces[0], "read_file") || !containsStringForTest(surfaces[1], "read_file") {
		t.Fatalf("local request surfaces=%v, want first two request-bound read surfaces", surfaces)
	}
}

func TestCodingStaticCompatibilityRunLoopUsesFirstRemoteEpochAndRebuildsAfterToolRound(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, extractToolName(definition))
		}
		surfaces = append(surfaces, names)
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_remote","type":"function","function":{"name":"ssh_read_file","arguments":"{\"path\":\"missing\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		cfg: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, readOnlyInquiry: true,
	}}
	result := agent.RunLoop(cb, "inspect", nil, server.Client())
	if result.Error != "" || result.Text != "done" || result.ToolCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(surfaces) < 2 || !containsStringForTest(surfaces[0], "ssh_read_file") || !containsStringForTest(surfaces[1], "ssh_read_file") {
		t.Fatalf("remote request surfaces=%v, want first two request-bound read surfaces", surfaces)
	}
}
