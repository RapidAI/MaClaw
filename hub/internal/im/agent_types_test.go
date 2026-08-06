package im

import "testing"

func TestAgentResponseToGenericResponseHidesLLMTelemetry(t *testing.T) {
	response := (&AgentResponse{
		Text: "这是模型的实际回复。",
		Fields: []ResponseField{
			{Label: "recording_title", Value: "项目复盘"},
			{Label: "API tokens", Value: "2 个待更新"},
			{Label: "diagnostic", Value: "private", Internal: true},
			{Label: "Input tokens", Value: "100"},
			{Label: "Route model", Value: "glm-5"},
			{Label: "Thinking", Value: "enabled"},
			{Label: "Turn", Value: "fast · glm-5 · 100→20"},
			{Label: "Route reason", Value: "short request"},
		},
	}).ToGenericResponse()

	if len(response.Fields) != 2 || response.Fields[0].Label != "recording_title" || response.Fields[1].Label != "API tokens" {
		t.Fatalf("end-user fields = %#v, want only business fields", response.Fields)
	}
	if got := response.ToFallbackText(); got != "这是模型的实际回复。\nrecording_title: 项目复盘\nAPI tokens: 2 个待更新" {
		t.Fatalf("fallback text = %q, want reply without LLM telemetry", got)
	}
}

func TestAgentResponseCarriesDeferredVoiceCount(t *testing.T) {
	response := (&AgentResponse{Text: "完整结果", PendingVoiceParts: 3}).ToGenericResponse()
	if response.PendingVoiceParts != 3 {
		t.Fatalf("pending voice parts=%d, want 3", response.PendingVoiceParts)
	}
}

func TestFilterOutInternalLLMTelemetryFieldsMatchesLabelsCaseInsensitively(t *testing.T) {
	fields := filterOutInternalLLMTelemetryFields([]ResponseField{
		{Label: " ROUTE SOURCE ", Value: "primary"},
		{Label: "Cache read tokens", Value: "80"},
		{Label: "session_est_cost_rmb", Value: "0.1"},
		{Label: "Result", Value: "ok"},
	})

	if len(fields) != 1 || fields[0].Label != "Result" {
		t.Fatalf("filtered fields = %#v, want only Result", fields)
	}
}
