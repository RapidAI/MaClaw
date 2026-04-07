package main

import (
	"context"
	"strings"
	"testing"
)

func TestSkillRunnerExecuteStepWithContext_CraftToolAcceptsLegacyInstructions(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "legacy task",
		},
	}, "")
	if err != nil && strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("craft_tool should not fall through unknown action: %v", err)
	}
}

func TestSkillRunnerExecuteStepWithContext_CraftToolMissingTask(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}
