package guiapp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestCodingSpilledToolResultReaderPagesOmittedMiddle(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "implement feature", Description: "update the repository"},
	}
	before := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 0))
	if containsStringForTest(before, "read_tool_result") {
		t.Fatal("read_tool_result must stay off the coding surface until a result spills")
	}
	denied := cb.ExecuteToolCallWithContext("read_tool_result", `{"id":"missing"}`, "call-0", agent.ToolCallExecutionContext{})
	if denied.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(denied.Result, "static_surface_unavailable") {
		t.Fatalf("unspilled reader = %#v", denied)
	}

	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("build output line %03d", i)
	}
	lines[100] = "SENTINEL_MIDDLE_UNIQUE"
	raw := strings.Join(lines, "\n")
	projected := cb.ProjectToolResult("bash", agent.ToolExecutionResult{Result: raw, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.Contains(projected, toolresult.HandleFooterMarker) {
		t.Fatalf("bash preview was not spilled:\n%s", projected)
	}
	if strings.Contains(projected, "SENTINEL_MIDDLE_UNIQUE") {
		t.Fatal("adaptive preview kept the omitted middle")
	}
	id := spilledHandleID(projected)
	if id == "" {
		t.Fatalf("missing handle id:\n%s", projected)
	}

	after := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 1))
	if !containsStringForTest(after, "read_tool_result") {
		t.Fatalf("spilled handle must list read_tool_result, got %v", after)
	}
	got := cb.ExecuteToolCallWithContext("read_tool_result", fmt.Sprintf(`{"id":%q}`, id), "call-1", agent.ToolCallExecutionContext{})
	if got.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(got.Result, "SENTINEL_MIDDLE_UNIQUE") {
		t.Fatalf("read back = %#v", got)
	}
}

func TestRemoteCodingSpilledToolResultReaderPagesSSHOutput(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}}
	before := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 0))
	if containsStringForTest(before, "read_tool_result") {
		t.Fatal("read_tool_result must stay off the remote surface until a result spills")
	}
	raw := strings.Repeat("status line\n", 1200) + "SENTINEL_REMOTE_UNIQUE\n" + strings.Repeat("listen line\n", 1200)
	projected := cb.ProjectToolResult("ssh_bash", agent.ToolExecutionResult{Result: raw, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.Contains(projected, toolresult.HandleFooterMarker) {
		t.Fatalf("ssh preview was not spilled: len=%d preview=%d", len(raw), len(projected))
	}
	id := spilledHandleID(projected)
	if id == "" {
		t.Fatalf("missing handle id:\n%s", projected)
	}
	after := codingSubAgentToolDefinitionNamesForTest(cb.BuildToolsForModelRequest("work", 1))
	if !containsStringForTest(after, "read_tool_result") {
		t.Fatalf("spilled handle must list read_tool_result, got %v", after)
	}
	got := cb.ExecuteToolCallWithContext("read_tool_result", fmt.Sprintf(`{"id":%q}`, id), "call-1", agent.ToolCallExecutionContext{})
	if got.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(got.Result, "SENTINEL_REMOTE_UNIQUE") {
		t.Fatalf("read back = %#v", got)
	}
}

func spilledHandleID(preview string) string {
	for _, line := range strings.Split(preview, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "id: "))
		}
	}
	return ""
}
