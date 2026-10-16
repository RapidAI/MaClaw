package agentruntime

import "testing"

func TestParameterRejectionGuidancePreservesDetails(t *testing.T) {
	if got := ParameterRejectionGuidance("[system rejected] parameter_unknown_field: unexpected"); got != "[system rejected] parameter_unknown_field: unexpected The call was refused before execution, so the tool remains available: do not retry the same arguments, call it again with arguments that match its rendered parameter schema exactly (some channel adapters take no arguments: {}), and do not ask the user to re-authorize tools." {
		t.Fatalf("detailed rejection=%q", got)
	}
	if got := ParameterRejectionGuidance("[system rejected] parameter_schema_invalid"); got != "[system rejected] parameter_schema_invalid. The call was refused before execution, so the tool remains available: do not retry the same arguments, call it again with arguments that match its rendered parameter schema exactly (some channel adapters take no arguments: {}), and do not ask the user to re-authorize tools." {
		t.Fatalf("generic rejection=%q", got)
	}
	if got := ParameterRejectionGuidance("ok"); got != "ok" {
		t.Fatalf("non-rejection=%q", got)
	}
}
