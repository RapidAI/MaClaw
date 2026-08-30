package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// Regression: the local desktop coding workbench runs in-process behind a
// host-verified ingress. RunLoop mints a fresh surface epoch per request and
// attaches the provider ResponseID before dispatch, so the S0.5 uncorrelated
// containment must not strip the effectful static families there — otherwise
// every implementation task can only answer with a plan and the workbench
// produces nothing.

func correlatedLocalCodingCallbacksForTest(t *testing.T, correlated bool) *codingSubAgentCallbacks {
	t.Helper()
	return &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			projectPath:              t.TempDir(),
			fullEnvironment:          true,
			correlatedLocalExecution: correlated,
		},
		task: &TaskItem{Title: "implement hello world", Description: "create hello.cpp", RequestKind: codingRequestImplementation},
	}
}

func TestCorrelatedLocalCodingSurfaceRendersEffectfulTools(t *testing.T) {
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	names := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("implement", 0))
	for _, wanted := range []string{"write_file", "edit_file", "bash"} {
		found := false
		for _, name := range names {
			if name == wanted {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("correlated local surface is missing effectful tool %q (rendered: %v)", wanted, names)
		}
	}
	if prompt := cb.BuildSystemPrompt("implement", true); strings.Contains(prompt, "Coding compatibility mode") {
		t.Fatalf("correlated local surface must not render the read-only compatibility prompt")
	}
}

func TestUncorrelatedLocalCodingSurfaceStaysReadOnly(t *testing.T) {
	cb := correlatedLocalCodingCallbacksForTest(t, false)
	assertCodingStaticCompatibilitySurfaceHasOnlyReadOrControlPlane(t, codingStaticCompatibilityHostLocal, cb.BuildToolsForModelRequest("implement", 0))
	if prompt := cb.BuildSystemPrompt("implement", true); !strings.Contains(prompt, "Coding compatibility mode") {
		t.Fatalf("uncorrelated local surface must keep the compatibility prompt")
	}
}

func TestCorrelatedLocalCodingDispatchRequiresResponseCorrelation(t *testing.T) {
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	epoch := cb.BeginToolSurfaceEpoch(0)
	if epoch == "" {
		t.Fatalf("expected a static surface epoch")
	}
	_ = cb.BuildToolsForModelRequest("implement", 0)

	// Effectful dispatch without the provider ResponseID must fail closed even
	// when the surface epoch matches.
	missing := cb.ExecuteToolCallWithContext("write_file", `{}`, "call-missing", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if missing.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(missing.Result, "static_response_correlation_missing") {
		t.Fatalf("effectful dispatch without ResponseID was not correlation-rejected: %#v", missing)
	}
	// Epoch-less dispatch of an effectful tool must fail closed as well.
	noEpoch := cb.ExecuteToolCallWithContext("write_file", `{}`, "call-no-epoch", agent.ToolCallExecutionContext{ResponseID: "resp-1"})
	if noEpoch.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(noEpoch.Result, "static_response_correlation_missing") {
		t.Fatalf("effectful dispatch without epoch was not correlation-rejected: %#v", noEpoch)
	}
	// Spawn is an effectful control-plane operation and needs the same proof.
	spawn := cb.ExecuteToolCallWithContext(codingSubAgentSpawnToolName, `{"task":"x"}`, "call-spawn", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if spawn.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(spawn.Result, "static_response_correlation_missing") {
		t.Fatalf("spawn dispatch without ResponseID was not correlation-rejected: %#v", spawn)
	}
	// With both correlation values the fences open and the call reaches the
	// canonical executor (which then fails on the empty parameter contract,
	// not on any admission rejection).
	admitted := cb.ExecuteToolCallWithContext("write_file", `{}`, "call-ok", agent.ToolCallExecutionContext{SurfaceEpoch: epoch, ResponseID: "resp-1"})
	if strings.Contains(admitted.Result, "static_response_correlation_missing") || strings.Contains(admitted.Result, "static_surface_unavailable") {
		t.Fatalf("correlated effectful dispatch was rejected by an admission fence: %#v", admitted)
	}
	// Read-only tools keep their existing behavior: epoch alone is enough.
	readOnly := cb.ExecuteToolCallWithContext("read_file", `{"path":"missing"}`, "call-read", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if strings.Contains(readOnly.Result, "static_response_correlation_missing") || strings.Contains(readOnly.Result, "static_surface_unavailable") {
		t.Fatalf("read-only dispatch must not require provider correlation: %#v", readOnly)
	}
}

func TestUncorrelatedSurfaceRejectsEffectfulCallEvenWithCorrelation(t *testing.T) {
	cb := correlatedLocalCodingCallbacksForTest(t, false)
	epoch := cb.BeginToolSurfaceEpoch(0)
	_ = cb.BuildToolsForModelRequest("implement", 0)
	// The name fence runs before the correlation gate: an unrendered effectful
	// name stays a surface rejection, never a correlation question.
	result := cb.ExecuteToolCallWithContext("write_file", `{}`, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch, ResponseID: "resp-1"})
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "static_surface_unavailable") {
		t.Fatalf("uncorrelated surface must reject effectful names as unavailable: %#v", result)
	}
}

func TestApplyUncorrelatedCompatibilitySurfaceOutcome(t *testing.T) {
	contained := correlatedLocalCodingCallbacksForTest(t, false)
	correlated := correlatedLocalCodingCallbacksForTest(t, true)

	status, errMsg := applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecPassed, "", false, contained, codingSubAgentAudit{AllSearchesRun: []CodingSubAgentSearchResult{{Query: "x", Succeeded: true}}})
	if status != TaskExecFailed || !strings.Contains(errMsg, "只读兼容模式") {
		t.Fatalf("plan-only implement turn on the compatibility surface must fail visibly: status=%s err=%q", status, errMsg)
	}
	status, _ = applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecPassed, "", false, contained, codingSubAgentAudit{AllFilesCreated: []string{"hello.cpp"}})
	if status != TaskExecPassed {
		t.Fatalf("artifact evidence must keep the pass verdict")
	}
	status, _ = applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecPassed, "", true, contained, codingSubAgentAudit{})
	if status != TaskExecPassed {
		t.Fatalf("read-only inquiry on the compatibility surface is legitimate and must pass")
	}
	status, _ = applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecPassed, "", false, correlated, codingSubAgentAudit{})
	if status != TaskExecPassed {
		t.Fatalf("correlated local boundary is not subject to the compatibility verdict")
	}
	status, _ = applyUncorrelatedCompatibilitySurfaceOutcome(TaskExecFailed, "boom", false, contained, codingSubAgentAudit{})
	if status != TaskExecFailed {
		t.Fatalf("existing failure must propagate unchanged")
	}
}

func TestCodingSubAgentSessionLacksEffectEvidence(t *testing.T) {
	planOnly := &TrajectorySession{
		Kind:   "coding_subagent",
		Status: "success",
		Entries: []TrajectoryEntry{
			{Role: "tool", ToolName: "list_directory"},
			{Role: "tool", ToolName: "Glob"},
			{Role: "tool", ToolName: "todo_write"},
		},
	}
	if !codingSubAgentSessionLacksEffectEvidence(planOnly) {
		t.Fatalf("plan-only coding session must be detected as lacking effect evidence")
	}
	productive := &TrajectorySession{
		Kind:   "coding_subagent",
		Status: "success",
		Entries: []TrajectoryEntry{
			{Role: "tool", ToolName: "Glob"},
			{Role: "tool", ToolName: "write_file"},
		},
	}
	if codingSubAgentSessionLacksEffectEvidence(productive) {
		t.Fatalf("coding session with a workspace write has effect evidence")
	}
	chatSession := &TrajectorySession{Kind: "shared", Status: "success"}
	if codingSubAgentSessionLacksEffectEvidence(chatSession) {
		t.Fatalf("non-coding sessions are out of scope for this guard")
	}
}
