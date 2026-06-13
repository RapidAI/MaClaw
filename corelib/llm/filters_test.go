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

func TestStripAllExtraRemovesDetailsThinkingBlock(t *testing.T) {
	input := "visible before\n<details>\n<summary>思考过程</summary>\nhidden reasoning\n</details>\nvisible after"
	got := StripAllExtra(input)
	if strings.Contains(got, "<details") || strings.Contains(got, "hidden reasoning") || strings.Contains(got, "思考过程") {
		t.Fatalf("StripAllExtra() leaked hidden details block: %q", got)
	}
	if got != "visible before\nvisible after" {
		t.Fatalf("StripAllExtra() = %q", got)
	}
}

func TestStripAllExtraRemovesAngleArrayToolCallTail(t *testing.T) {
	input := "visible before\n<tool_call[]>\n{\"name\":\"write_file\",\"arguments\":{\"file_path\":\"e:\\\\CRM\\\\docs\\\\technical-design.md\"}}"
	got := StripAllExtra(input)
	if strings.Contains(got, "<tool_call") || strings.Contains(got, "write_file") || strings.Contains(got, "technical-design.md") {
		t.Fatalf("StripAllExtra() leaked tool call tail: %q", got)
	}
	if got != "visible before" {
		t.Fatalf("StripAllExtra() = %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesSplitDetailsThinkingBlock(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("visible before\n<det")
	filter.Write("ails>\n<summary>思考过程</summary>\nhidden")
	filter.Write(" reasoning\n</details>\nvisible after")
	filter.Flush()
	got := out.String()
	if strings.Contains(got, "<details") || strings.Contains(got, "hidden reasoning") || strings.Contains(got, "思考过程") {
		t.Fatalf("filtered output leaked hidden details block: %q", got)
	}
	if got != "visible before\n\nvisible after" {
		t.Fatalf("filtered output = %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesSplitAngleArrayToolCall(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("visible before\n<tool")
	filter.Write("_call[]>\n{\"name\":\"write_file\"")
	filter.Write(",\"arguments\":{\"file_path\":\"e:\\\\CRM\\\\docs\\\\technical-design.md\"}}")
	filter.Flush()
	got := out.String()
	if strings.Contains(got, "<tool_call") || strings.Contains(got, "write_file") || strings.Contains(got, "technical-design.md") {
		t.Fatalf("filtered output leaked tool call: %q", got)
	}
	if got != "visible before\n" {
		t.Fatalf("filtered output = %q", got)
	}
}

func TestContentToolCallDeltaFilterFlushesPlainTextThroughDetailsFilter(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("hello <det")
	filter.Flush()
	if got := out.String(); got != "hello <det" {
		t.Fatalf("filtered output = %q, want partial non-tag text", got)
	}
}
