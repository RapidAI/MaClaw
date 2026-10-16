package llmservice

import "testing"

func TestParseProviderTestToolCallsReadsResponsesFunctionCall(t *testing.T) {
	body := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":""}]},{"type":"function_call","name":"ping","arguments":"{\"ok\":true}"}]}`)
	calls := ParseProviderTestToolCalls(body)
	if len(calls) != 1 || calls[0].Name != "ping" || calls[0].Arguments != `{"ok":true}` {
		t.Fatalf("calls = %+v", calls)
	}
}
