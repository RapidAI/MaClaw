package v2

import "testing"

func TestDatabaseQueryAllowedInRestrictedWorkflowPhases(t *testing.T) {
	query := map[string]interface{}{"action": "query"}
	write := map[string]interface{}{"action": "execute"}
	for _, policy := range []ToolFilterPolicy{ToolPolicyDocOnly, ToolPolicyPlanning, ToolPolicyOpsControlled} {
		if !IsToolAllowedByPolicy(policy, "database") || !IsToolAllowedByPolicy(policy, "database_query") {
			t.Fatalf("policy %s should expose database query", policy)
		}
		if err := ValidateToolCallByPolicy(policy, "database", query); err != nil {
			t.Fatalf("query under %s: %v", policy, err)
		}
		if err := ValidateToolCallByPolicy(policy, "database", write); err == nil {
			t.Fatalf("execute under %s should be denied", policy)
		}
	}
	if err := ValidateToolCallByPolicy(ToolPolicyFull, "database", write); err != nil {
		t.Fatalf("full policy should allow execute: %v", err)
	}
}
