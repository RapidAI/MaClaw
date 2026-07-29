package main

import "testing"

func TestSkillStepActionCallMCPToolDoesNotUseManagedProcessEnv(t *testing.T) {
	if skillStepActionCallMCPTool.UsesManagedProcessEnv() {
		t.Fatal("call_mcp_tool must not pin global process env lock; MCP calls are owner-scoped and arg-based")
	}
}

func TestDisabledLegacySkillStepsDoNotUseManagedProcessEnv(t *testing.T) {
	for _, action := range []skillStepActionKind{
		skillStepActionCreateSession,
		skillStepActionSendInput,
		skillStepActionSendAndObserve,
		skillStepActionControlSession,
	} {
		if action.UsesManagedProcessEnv() {
			t.Fatalf("%s must not pin global process env lock; legacy session steps are rejected", action)
		}
	}
}
