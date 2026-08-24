package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestCodingDynamicSurfaceFailsClosedWithoutDurableIdentityAndPlan(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		task:     &TaskItem{Index: 1, Title: "use dynamic helpers"},
		matchedSkills: []codingSubAgentSkillMatch{{
			Name: "formatter", RequiredArgs: []string{"input"}, StableID: "publisher:formatter", ContentDigest: "content-a", ContractDigest: "contract-a",
		}},
		matchedMCPTools: []codingSubAgentMCPToolMatch{{
			ServerID: "browser", ToolName: "screenshot", RequiredArgs: []string{"url"}, SchemaDigest: "schema-a", ContractDigest: "contract-a",
		}},
	}
	defsA := cb.BuildToolsForModelRequest("", 0)
	namesA := codingSubAgentToolDefinitionNamesForTest(defsA)
	skillA := firstCodingDynamicAliasForTest(namesA, "skill_")
	mcpA := firstCodingDynamicAliasForTest(namesA, "mcp_")
	if skillA != "" || mcpA != "" {
		t.Fatalf("unplanned dynamic aliases leaked: %v", namesA)
	}
	if containsStringForTest(namesA, "manage_skill") || containsStringForTest(namesA, "call_mcp_tool") {
		t.Fatalf("generic gateways leaked into request A: %v", namesA)
	}
	denied := cb.ExecuteToolCallWithContext("skill_stale", `{"name":"other","input":"x"}`, "call-a", agent.ToolCallExecutionContext{})
	if denied.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(denied.Result, "catalog_incomplete") {
		t.Fatalf("unplanned dynamic alias must fail closed: %#v", denied)
	}
	for _, legacyGateway := range []string{"manage_skill", "call_mcp_tool"} {
		denied := cb.ExecuteToolCallWithContext(legacyGateway, `{}`, "legacy-"+legacyGateway, agent.ToolCallExecutionContext{})
		if denied.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(denied.Result, "catalog_incomplete") {
			t.Fatalf("legacy dynamic gateway %q must be model-denied: %#v", legacyGateway, denied)
		}
	}

	defsB := cb.BuildToolsForModelRequest("", 1)
	namesB := codingSubAgentToolDefinitionNamesForTest(defsB)
	skillB := firstCodingDynamicAliasForTest(namesB, "skill_")
	if skillB != "" {
		t.Fatalf("unplanned dynamic alias survived replacement: %q", skillB)
	}
}

func TestCodingCompatibilitySystemPromptsDoNotAdvertiseUnavailableEffects(t *testing.T) {
	local := (&codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true},
		task:     &TaskItem{Index: 1, Title: "fix parser", Description: "inspect parser behavior"},
	}).BuildSystemPrompt("inspect parser behavior", true)
	for _, unavailable := range []string{"write_file", "edit_file", "edit_lines", "bash", "report_localization", codingSubAgentSpawnToolName} {
		if strings.Contains(local, unavailable) {
			t.Fatalf("local compatibility prompt advertised unavailable %q: %q", unavailable, local)
		}
	}
	for _, available := range []string{"read_file", "code_navigation", codingAgentTodoToolName} {
		if !strings.Contains(local, available) {
			t.Fatalf("local compatibility prompt omitted usable guidance %q: %q", available, local)
		}
	}

	remote := (&remoteCodingCallbacks{
		agent:       &RemoteCodingSubAgent{projectDir: "/srv/repo", workDir: "/srv/repo"},
		taskContext: "inspect parser behavior",
	}).BuildSystemPrompt("inspect parser behavior", true)
	for _, unavailable := range []string{"ssh_write_file", "ssh_edit_file", "ssh_bash", "report_localization", codingSubAgentSpawnToolName} {
		if strings.Contains(remote, unavailable) {
			t.Fatalf("remote compatibility prompt advertised unavailable %q: %q", unavailable, remote)
		}
	}
	for _, available := range []string{"ssh_read_file", "code_navigation", codingAgentTodoToolName} {
		if !strings.Contains(remote, available) {
			t.Fatalf("remote compatibility prompt omitted usable guidance %q: %q", available, remote)
		}
	}
}

func TestCodingCompatibilityTrajectorySnapshotsMatchConstrainedSurface(t *testing.T) {
	local := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true},
		task:     &TaskItem{Index: 1, Title: "implement parser"},
	}
	for _, forbidden := range []string{"write_file", "edit_file", "edit_lines", "bash", reportLocalizationToolName, codingSubAgentSpawnToolName} {
		if containsStringForTest(codingSubAgentToolDefinitionNamesForTest(local.trajectoryToolSurfaceSnapshot("implement parser")), forbidden) {
			t.Fatalf("local trajectory snapshot leaked %q", forbidden)
		}
	}

	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: "/srv/repo", workDir: "/srv/repo"}}
	for _, forbidden := range []string{"ssh_write_file", "ssh_edit_file", "ssh_bash", reportLocalizationToolName, codingSubAgentSpawnToolName} {
		if containsStringForTest(codingSubAgentToolDefinitionNamesForTest(remote.trajectoryToolSurfaceSnapshot("implement parser")), forbidden) {
			t.Fatalf("remote trajectory snapshot leaked %q", forbidden)
		}
	}
}

func TestRemoteCodingDynamicSurfaceFailsClosedWithoutDurableIdentityAndPlan(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent:            &RemoteCodingSubAgent{},
		localExtSelected: true,
		localExtSkills: []codingSubAgentSkillMatch{{
			Name: "formatter", StableID: "publisher:formatter", ContentDigest: "content-a", ContractDigest: "contract-a",
		}},
		localExtMCP: []codingSubAgentMCPToolMatch{{
			ServerID: "browser", ToolName: "screenshot", SchemaDigest: "schema-a", ContractDigest: "contract-a",
		}},
	}
	defsA := cb.BuildToolsForModelRequest("", 0)
	namesA := codingSubAgentToolDefinitionNamesForTest(defsA)
	skillA := firstCodingDynamicAliasForTest(namesA, "skill_")
	mcpA := firstCodingDynamicAliasForTest(namesA, "mcp_")
	if skillA != "" || mcpA != "" {
		t.Fatalf("remote unplanned dynamic aliases leaked: %v", namesA)
	}
	if containsStringForTest(namesA, "manage_skill") || containsStringForTest(namesA, "call_mcp_tool") {
		t.Fatalf("remote request leaked generic gateways: %v", namesA)
	}
	denied := cb.ExecuteToolCallWithContext("mcp_stale", `{}`, "remote-denied", agent.ToolCallExecutionContext{})
	if denied.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(denied.Result, "catalog_incomplete") {
		t.Fatalf("remote unplanned alias must fail closed: %#v", denied)
	}
	for _, legacyGateway := range []string{"manage_skill", "call_mcp_tool"} {
		denied := cb.ExecuteToolCallWithContext(legacyGateway, `{}`, "remote-legacy-"+legacyGateway, agent.ToolCallExecutionContext{})
		if denied.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(denied.Result, "catalog_incomplete") {
			t.Fatalf("remote legacy dynamic gateway %q must be model-denied: %#v", legacyGateway, denied)
		}
	}
	cb.BuildToolsForModelRequest("", 1)
}

func TestCodingLegacyDynamicGatewaysCannotReachModelDispatch(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{},
		matchedSkills: []codingSubAgentSkillMatch{{
			Name: "formatter", StableID: "publisher:formatter", ContentDigest: "content-a", ContractDigest: "contract-a",
		}},
		matchedMCPTools: []codingSubAgentMCPToolMatch{{
			ServerID: "browser", ToolName: "screenshot", SchemaDigest: "schema-a", ContractDigest: "contract-a",
		}},
	}
	for _, gateway := range []string{"manage_skill", "call_mcp_tool"} {
		result := cb.ExecuteToolStructured(gateway, `{}`)
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "catalog_incomplete") {
			t.Fatalf("local model dispatch through %q = %#v, want catalog_incomplete", gateway, result)
		}
	}

	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	for _, gateway := range []string{"manage_skill", "call_mcp_tool"} {
		result := remote.ExecuteToolStructured(gateway, `{}`)
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "catalog_incomplete") {
			t.Fatalf("remote model dispatch through %q = %#v, want catalog_incomplete", gateway, result)
		}
	}
}

func TestCodingFailClosedPromptDoesNotAdvertiseLegacyDynamicGateways(t *testing.T) {
	for _, prompt := range []string{buildFullCodingEnvironmentPromptPreamble(), buildNestedFullCodingEnvironmentPromptPreamble()} {
		if strings.Contains(prompt, "manage_skill") || strings.Contains(prompt, "call_mcp_tool") {
			t.Fatalf("fail-closed prompt advertised a legacy gateway: %q", prompt)
		}
		if !strings.Contains(prompt, "受管扩展函数") {
			t.Fatalf("fail-closed prompt did not describe the actual request surface: %q", prompt)
		}
	}
}

func TestTrustedCodingIdentityOnlyResolvesFromDurableRuntimeAnchor(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", 1, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}
	if identity, ok := resolveTrustedCodingInvocationIdentity(store, request); ok || identity != nil {
		t.Fatalf("unanchored runtime unexpectedly resolved identity: %#v", identity)
	}
	wanted := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "semantic-root", TurnID: "turn"}
	if !registerTrustedCodingInvocationIdentity(store, request, wanted) {
		t.Fatal("trusted host identity registration failed")
	}
	identity, ok := resolveTrustedCodingInvocationIdentity(store, request)
	if !ok || identity == nil || identity.TenantID != "tenant" || identity.PrincipalID != "principal" || identity.SessionID != "session" || identity.RootTaskID != "semantic-root" || identity.TurnID != "turn" {
		t.Fatalf("resolved identity = %#v, ok=%v", identity, ok)
	}
	// G1 deliberately does not enable aliases: the identity is a necessary
	// ingress proof, not a substitute for catalog/plan/grant/journal migration.
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{dynamicInvocationIdentity: identity}}
	if cb.codingDynamicAliasesMayMaterialize() {
		t.Fatal("identity alone must not re-enable dynamic aliases")
	}
}

func firstCodingDynamicAliasForTest(names []string, prefix string) string {
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			return name
		}
	}
	return ""
}
