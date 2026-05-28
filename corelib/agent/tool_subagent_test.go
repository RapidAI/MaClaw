package agent

import (
	"strings"
	"testing"
)

func TestToolDelegateTaskCodingWorkflowDoesNotFakeActivation(t *testing.T) {
	got := ToolDelegateTask(map[string]interface{}{
		"agent":   "coding_workflow",
		"request": "create files",
	})
	if IsSubAgentContext(got) || strings.Contains(got, "sub-agent active") || strings.Contains(got, "__SUBAGENT_CONTEXT__") {
		t.Fatalf("coding_workflow returned fake sub-agent activation: %q", got)
	}
	if !strings.Contains(got, "CodingSubAgent") || !strings.Contains(got, "requires a host") {
		t.Fatalf("coding_workflow result = %q, want explicit host executor rejection", got)
	}
}

func TestToolDelegateTaskHelpStillInjectsContext(t *testing.T) {
	got := ToolDelegateTask(map[string]interface{}{
		"agent":   "help",
		"request": "how do I use MaClaw?",
	})
	if !IsSubAgentContext(got) {
		t.Fatalf("help delegate result = %q, want sub-agent context", got)
	}
	if !strings.Contains(ExtractSubAgentContext(got), "help") {
		t.Fatalf("help delegate context = %q, want help agent context", got)
	}
}

func TestListSubAgentsDoesNotAdvertiseCodingWorkflow(t *testing.T) {
	got := ListSubAgents()
	if strings.Contains(got, "coding_workflow") {
		t.Fatalf("ListSubAgents advertised coding_workflow: %q", got)
	}
	if !strings.Contains(got, "help") {
		t.Fatalf("ListSubAgents = %q, want help listed", got)
	}
}
