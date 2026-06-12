package llm

import (
	"strings"
	"testing"
)

func TestStripXMLToolCalls_RemovesCodexToolCallBlocks(t *testing.T) {
	input := "before\n<turn: tool_call><invoke name=\"bash\"><parameter name=\"command\" string=\"true\">dir</parameter></invoke></turn>\nafter"
	got := StripXMLToolCalls(input)
	if got != "before\nafter" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
}

func TestContentToolCallDeltaFilterAllowsToolCallsExplanation(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("OpenAI tool_calls should be structured.")
	filter.Flush()
	if got := out.String(); got != "OpenAI tool_calls should be structured." {
		t.Fatalf("filtered output = %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesExplicitPlainMarker(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("checking\nTOOL")
	filter.Write("_CALL {\"name\":\"bash\",\"arguments\":{}}")
	filter.Flush()
	if got := out.String(); got != "checking\n" {
		t.Fatalf("filtered output = %q, want prefix only", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesBareJSONToolCall(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("{\"tool_calls\":[{\"function\":{\"name\":\"bash\",")
	filter.Write("\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}]}")
	filter.Flush()
	if got := out.String(); got != "" {
		t.Fatalf("filtered output = %q, want empty", got)
	}
}

func TestContentToolCallDeltaFilterAllowsOrdinaryJSON(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("{\"name\":\"Alice\"}")
	if got := out.String(); got != "" {
		t.Fatalf("ordinary JSON should be buffered until flush, got %q", got)
	}
	filter.Flush()
	if got := out.String(); got != "{\"name\":\"Alice\"}" {
		t.Fatalf("filtered output = %q, want ordinary JSON", got)
	}
}
