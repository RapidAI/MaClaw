package agent

import "testing"

func TestEstimateLoopInputBreakdownUsesExclusiveBuckets(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": "question"},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{map[string]interface{}{"id": "tc1"}}},
		map[string]interface{}{"role": "tool", "tool_call_id": "tc1", "content": "large result"},
	}
	tools := []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "bash"}}}
	got := EstimateLoopInputBreakdown(conversation, tools)
	if got.SystemPromptTokens <= 0 || got.HistoryTokens <= 0 || got.ToolResultTokens <= 0 || got.ToolDefinitionTokens <= 0 {
		t.Fatalf("missing bucket: %+v", got)
	}
	wantTotal := got.SystemPromptTokens + got.HistoryTokens + got.ToolResultTokens + got.ToolDefinitionTokens
	if got.TotalEstimatedTokens != wantTotal {
		t.Fatalf("total=%d want=%d: %+v", got.TotalEstimatedTokens, wantTotal, got)
	}
	toolMessageTokens := EstimateConversationTokens([]interface{}{conversation[3]})
	if got.ToolResultTokens >= toolMessageTokens {
		t.Fatalf("tool result bucket includes protocol envelope: bucket=%d whole_message=%d", got.ToolResultTokens, toolMessageTokens)
	}
	if got.HistoryTokens <= EstimateConversationTokens(conversation[1:3]) {
		t.Fatalf("history bucket omitted tool protocol envelope: %+v", got)
	}
}

func TestRecordLoopInputBreakdownAdvancesSnapshot(t *testing.T) {
	before := CurrentLoopInputBreakdownStats()
	RecordLoopInputBreakdown(LoopInputBreakdown{SystemPromptTokens: 1, ToolDefinitionTokens: 2, HistoryTokens: 3, ToolResultTokens: 4, TotalEstimatedTokens: 10})
	after := CurrentLoopInputBreakdownStats()
	if after.Requests != before.Requests+1 || after.TotalEstimatedTokens != before.TotalEstimatedTokens+10 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}
