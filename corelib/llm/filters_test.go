package llm

import (
	"strings"
	"testing"
)

func TestStripLeakedReasoningProtocol_DropsClosingTags(t *testing.T) {
	input := "让我调用web_search来获取北京天气。\n</tool_value>\n</thinking>"
	got := StripLeakedReasoningProtocol(input)
	if strings.Contains(got, "tool_value") || strings.Contains(got, "thinking>") || strings.Contains(got, "</") {
		t.Fatalf("protocol closers leaked: %q", got)
	}
	if got != "让我调用web_search来获取北京天气。" {
		t.Fatalf("StripLeakedReasoningProtocol() = %q", got)
	}
	if got := StripAllExtra(input); got != "让我调用web_search来获取北京天气。" {
		t.Fatalf("StripAllExtra() = %q", got)
	}
}

func TestStripLeakedReasoningProtocol_DropsPayloadKeepsProse(t *testing.T) {
	input := "<thinking>\n先查天气。\n<tool_name>web_search</tool_name>\n<tool_value>{\"query\":\"北京天气\"}</tool_value>\n</thinking>"
	got := StripLeakedReasoningProtocol(input)
	if strings.Contains(got, "tool_value") || strings.Contains(got, "北京天气") || strings.Contains(got, "web_search") || strings.Contains(got, "thinking") {
		t.Fatalf("tool payload leaked: %q", got)
	}
	if got != "先查天气。" {
		t.Fatalf("StripLeakedReasoningProtocol() = %q", got)
	}
}

func TestStripLeakedReasoningProtocol_KeepsLeadingIndent(t *testing.T) {
	input := "    func main() {\n    }\n</tool_value>\n</thinking>"
	got := StripLeakedReasoningProtocol(input)
	if got != "    func main() {\n    }" {
		t.Fatalf("indent changed: %q", got)
	}
	wrapped := "<thinking>\n    func main() {\n    }\n</thinking>"
	if got := StripLeakedReasoningProtocol(wrapped); got != "    func main() {\n    }" {
		t.Fatalf("wrapped indent changed: %q", got)
	}
}

func TestReasoningProtocolStreamFilter_FlushKeepsAmbiguousToolPrefix(t *testing.T) {
	var out strings.Builder
	filter := newReasoningProtocolStreamFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("use a <tool")
	filter.Flush()
	if got := out.String(); got != "use a <tool" {
		t.Fatalf("flush dropped an ambiguous tail: %q", got)
	}
}

func TestParseSSE_SplitToolCallStillHidden(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"先查\n<tool"}}]}`,
		`data: {"choices":[{"delta":{"content":"_call>{\"name\":\"web_search\",\"arguments\":{\"query\":\"北京天气\"}}</tool_call>"}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	var streamed strings.Builder
	resp, err := parseSSEStreamWithReasoning(strings.NewReader(body), func(delta string) {
		streamed.WriteString(delta)
	}, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if strings.Contains(streamed.String(), "tool_call") || strings.Contains(streamed.String(), "北京天气") || strings.Contains(streamed.String(), "<tool") {
		t.Fatalf("visible stream leaked the tool call: %q", streamed.String())
	}
	if strings.TrimSpace(streamed.String()) != "先查" {
		t.Fatalf("visible stream = %q", streamed.String())
	}
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool call = %#v", resp.Choices[0].Message)
	}
}

func TestStripLeakedReasoningProtocol_KeepsPlainMentions(t *testing.T) {
	for _, input := range []string{
		"Let me think about the tool_value field before answering.",
		"  <thinkingish> stays, including the indent",
		"<think>hidden</think>shown",
		"2 < 3 and <div>ok</div>",
	} {
		if got := StripLeakedReasoningProtocol(input); got != input {
			t.Fatalf("plain text changed: %q -> %q", input, got)
		}
	}
}

func TestReasoningProtocolStreamFilter_UnicodeBeforeTag(t *testing.T) {
	input := "İ让我调用。\n</TOOL_VALUE>\n</Thinking>"
	if got := StripLeakedReasoningProtocol(input); got != "İ让我调用。" {
		t.Fatalf("strip = %q", got)
	}
	var out strings.Builder
	filter := newReasoningProtocolStreamFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("İ让我调用。\n</TOOL_VAL")
	filter.Write("UE>\n</Thinking>")
	filter.Flush()
	if got := strings.TrimSpace(out.String()); got != "İ让我调用。" {
		t.Fatalf("streamed = %q", out.String())
	}
}

func TestReasoningProtocolStreamFilter_KeepsOrdinaryBrackets(t *testing.T) {
	var out strings.Builder
	filter := newReasoningProtocolStreamFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("2 < 3\n<div>ok</div>\n<tool_value/>之后还在")
	filter.Flush()
	if got := out.String(); got != "2 < 3\n<div>ok</div>\n之后还在" {
		t.Fatalf("streamed = %q", got)
	}
}

func TestReasoningProtocolStreamFilter_HidesSplitClosers(t *testing.T) {
	var out strings.Builder
	filter := newReasoningProtocolStreamFilter(func(delta string) { out.WriteString(delta) })
	for _, delta := range []string{"让我调用。\n</tool_val", "ue>\n</thi", "nking>"} {
		filter.Write(delta)
	}
	filter.Flush()
	if got := strings.TrimSpace(out.String()); got != "让我调用。" {
		t.Fatalf("streamed reasoning = %q", out.String())
	}
	if strings.Contains(out.String(), "tool_value") || strings.Contains(out.String(), "thinking>") {
		t.Fatalf("streamed reasoning leaked protocol: %q", out.String())
	}
}

func TestReasoningProtocolStreamFilter_DropsToolValuePayload(t *testing.T) {
	var out strings.Builder
	filter := newReasoningProtocolStreamFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("先查。\n<tool_value>{\"query\":\"北京天气\"}</tool_value>\n然后结束")
	filter.Flush()
	if got := out.String(); got != "先查。\n\n然后结束" {
		t.Fatalf("streamed reasoning = %q", got)
	}
}

func TestParseSSE_ReasoningProtocolClosersStayOffThePanel(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"让我调用。\n</tool_val"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"ue>\n</thinking>"},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"北京天气\"}"}}]}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	var streamed strings.Builder
	resp, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if strings.Contains(streamed.String(), "tool_value") || strings.Contains(streamed.String(), "thinking>") {
		t.Fatalf("live reasoning leaked protocol: %q", streamed.String())
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "让我调用。" {
		t.Fatalf("reasoning = %q", got)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 || resp.Choices[0].Message.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("tool call = %#v", resp.Choices[0].Message.ToolCalls)
	}
}

func TestParseSSE_ToolCallXMLStillHiddenBesideProtocolFilter(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"先查。\n<tool_call>{\"name\":\"web_search\",\"arguments\":{\"query\":\"北京天气\"}}</tool_call>"}}]}`,
		"data: [DONE]",
		"",
	}, "\n")
	var streamed strings.Builder
	resp, err := parseSSEStreamWithReasoning(strings.NewReader(body), func(delta string) {
		streamed.WriteString(delta)
	}, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if strings.Contains(streamed.String(), "tool_call") || strings.Contains(streamed.String(), "北京天气") {
		t.Fatalf("visible stream leaked the tool call: %q", streamed.String())
	}
	if strings.TrimSpace(streamed.String()) != "先查。" {
		t.Fatalf("visible stream = %q", streamed.String())
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 || resp.Choices[0].Message.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("tool call = %#v content=%q", resp.Choices[0].Message.ToolCalls, resp.Choices[0].Message.Content)
	}
}

func TestStripXMLToolCalls_RemovesDeepSeekDSML(t *testing.T) {
	input := "好的，先查杭州最新天气。\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"web_search\">\n<｜DSML｜parameter name=\"query\" string=\"true\">杭州天气</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "web_search") || strings.Contains(got, "杭州天气") {
		t.Fatalf("StripXMLToolCalls leaked DSML: %q", got)
	}
	if got != "好的，先查杭州最新天气。" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
}

func TestStripXMLToolCalls_RemovesLeftoverDSMLParameter(t *testing.T) {
	input := "先查天气\n<｜DSML｜parameter name=\"query\" string=\"true\">杭州天气"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "杭州天气") {
		t.Fatalf("leftover DSML parameter leaked: %q", got)
	}
	if got != "先查天气" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
}

func TestStripXMLToolCalls_RemovesUnclosedDeepSeekDSML(t *testing.T) {
	input := "先查天气\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"web_search\">"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "web_search") {
		t.Fatalf("unclosed DSML leaked: %q", got)
	}
	if got != "先查天气" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
}

func TestStripXMLToolCalls_RemovesQwenFunctionEq(t *testing.T) {
	input := "先改海报\n<function=write_file>\n<parameter=path>poster.py</parameter>\n</function>"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "write_file") || strings.Contains(got, "poster.py") {
		t.Fatalf("function= leaked: %q", got)
	}
	if got != "先改海报" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
}

func TestFirstContentToolCallMarkerIndex_DeepSeekDSML(t *testing.T) {
	s := "好的\n<｜DSML｜tool_calls>"
	idx := FirstContentToolCallMarkerIndex(s)
	if idx < 0 || !strings.HasPrefix(s[idx:], "<") {
		t.Fatalf("marker index = %d, want DSML start", idx)
	}
	if ContentToolCallMarkerSuffixLen("好的\n<｜DS") == 0 {
		t.Fatal("partial DSML suffix must be held")
	}
}

func TestHoldContentToolCallStream_FlushDropsPartialDSMLKeepsLoneAngle(t *testing.T) {
	visible, hold, suppress := HoldContentToolCallStream("好的，先查杭州最新天气。\n<｜DSML｜", true)
	if visible != "好的，先查杭州最新天气。\n" || hold != "" || !suppress {
		t.Fatalf("partial DSML flush = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	visible, hold, suppress = HoldContentToolCallStream("2 <", true)
	if visible != "2 <" || hold != "" || suppress {
		t.Fatalf("lone angle flush = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	visible, hold, suppress = HoldContentToolCallStream("Use a tool_call", true)
	if visible != "Use a tool_call" || hold != "" || suppress {
		t.Fatalf("prose tool_call flush = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
}

func TestContentToolCallDeltaFilterDropsPartialDSMLOnFlush(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("好的，先查杭州最新天气。\n<｜DSML｜")
	filter.Flush()
	if got := out.String(); got != "好的，先查杭州最新天气。\n" {
		t.Fatalf("partial DSML leaked on flush: %q", got)
	}
}

func TestContentToolCallDeltaFilterEmitsLoneAngleOnFlush(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("2 <")
	filter.Flush()
	if got := out.String(); got != "2 <" {
		t.Fatalf("lone angle dropped on flush: %q", got)
	}
}

func TestContentToolCallDeltaFilterHoldsMultiSpaceDSMLPrefix(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("先查天气\n<   |   DS")
	if got := out.String(); got != "先查天气\n" {
		t.Fatalf("multi-space DSML prefix leaked: %q", got)
	}
	filter.Write("ML   |   parameter name=\"query\"")
	filter.Flush()
	if got := out.String(); got != "先查天气\n" {
		t.Fatalf("multi-space DSML parameter leaked: %q", got)
	}
}

func TestContentToolCallDeltaFilterHoldsSpacedDSMLPrefix(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("先查天气\n< | DS")
	if got := out.String(); got != "先查天气\n" {
		t.Fatalf("spaced DSML prefix leaked: %q", got)
	}
	filter.Write("ML | tool_calls>< | DSML | invoke name=\"web_search\">")
	filter.Flush()
	if got := out.String(); got != "先查天气\n" {
		t.Fatalf("filtered output = %q", got)
	}
}

func TestStripXMLToolCalls_RemovesRepeatedBarDSML(t *testing.T) {
	input := "先看一下。\n<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"ssh_list_dir\">\n<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/home/znsoft/prj8</｜｜DSML｜｜ parameter>\n</｜｜DSML｜｜ invoke>\n</｜｜DSML｜｜ calls>"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "ssh_list_dir") || strings.Contains(got, "prj8") {
		t.Fatalf("repeated-bar DSML leaked: %q", got)
	}
	if !strings.Contains(got, "先看一下。") {
		t.Fatalf("prose dropped: %q", got)
	}
}

func TestStripXMLToolCalls_KeepsProseAfterRepeatedBarDSML(t *testing.T) {
	input := "先看一下。\n<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"ssh_list_dir\">\n<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/tmp</｜｜DSML｜｜ parameter>\n</｜｜DSML｜｜ invoke>\n</｜｜DSML｜｜ calls>\n改完了。"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "ssh_list_dir") {
		t.Fatalf("DSML leaked: %q", got)
	}
	if got != "先看一下。\n改完了。" {
		t.Fatalf("prose around the block = %q", got)
	}
}

func TestStripXMLToolCalls_DSMLSpaceBeforeGtKeepsProse(t *testing.T) {
	input := "先看一下。\n<｜DSML｜calls >\n<｜DSML｜invoke name=\"ssh_list_dir\">\n<｜DSML｜parameter name=\"path\" string=\"true\">/tmp</｜DSML｜parameter >\n</｜DSML｜invoke >\n</｜DSML｜calls >\n改完了。"
	got := StripXMLToolCalls(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "ssh_list_dir") {
		t.Fatalf("DSML leaked: %q", got)
	}
	if got != "先看一下。\n改完了。" {
		t.Fatalf("prose around the block = %q", got)
	}
}

func TestContentToolCallDeltaFilterKeepsDSMLMention(t *testing.T) {
	for _, text := range []string{
		"The <|DSML| token is documentation, not a call.",
		"The <|DSML| calls for a retry, not a tool.",
		"The <|DSML| parameter is a field.",
		"The <|DSML|invoke the helper.",
	} {
		var out strings.Builder
		filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
		filter.Write(text)
		filter.Flush()
		if got := out.String(); got != text {
			t.Fatalf("mention suppressed: %q", got)
		}
	}
}

func TestStripXMLToolCalls_DropsTrailingPartialDSML(t *testing.T) {
	got := StripXMLToolCalls("好的，先查。\n<｜DSML｜")
	if strings.Contains(got, "DSML") || strings.Contains(got, "｜") {
		t.Fatalf("partial fence kept: %q", got)
	}
	if got != "好的，先查。" {
		t.Fatalf("prose = %q", got)
	}
	for _, input := range []string{
		"The <|DSML| token is documentation.",
		"echo |DSML| ok",
		"2 <",
	} {
		if got := StripXMLToolCalls(input); got != input {
			t.Fatalf("prose rewritten: %q -> %q", input, got)
		}
	}
}

func TestStripXMLToolCalls_KeepsBareDSMLFence(t *testing.T) {
	for _, input := range []string{
		"echo |DSML| ok",
		"see < | DSML | note",
		"记号写作｜，DSML 只是名字",
		"The <|DSML| parameter is a field.",
		"The <|DSML|invoke the helper.",
	} {
		if got := StripXMLToolCalls(input); got != input {
			t.Fatalf("prose rewritten: %q -> %q", input, got)
		}
	}
}

func TestContentToolCallDeltaFilterHoldsSplitRepeatedBarDSML(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("先看一下。\n<｜｜")
	if got := out.String(); got != "先看一下。\n" {
		t.Fatalf("fence prefix leaked: %q", got)
	}
	filter.Write("DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"ssh_list_dir\">")
	filter.Flush()
	if got := out.String(); got != "先看一下。\n" {
		t.Fatalf("split fence leaked: %q", got)
	}
}

func TestContentToolCallDeltaFilterReleasesNonDSMLBars(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("see <||")
	filter.Write(" later")
	filter.Flush()
	if got := out.String(); got != "see <|| later" {
		t.Fatalf("non-DSML bars dropped: %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesRepeatedBarDSML(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("先看一下。\n<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"ssh_list_dir\">")
	filter.Flush()
	if got := out.String(); got != "先看一下。\n" {
		t.Fatalf("repeated-bar DSML leaked: %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesDeepSeekDSML(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("好的，先查杭州最新天气，然后生成 PDF 报告给你。\n<")
	filter.Write("｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"web_search\">")
	filter.Flush()
	if got := out.String(); got != "好的，先查杭州最新天气，然后生成 PDF 报告给你。\n" {
		t.Fatalf("filtered output = %q", got)
	}
}

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

func TestStripXMLToolCalls_RemovesLongcat(t *testing.T) {
	input := "让我继续读取文件。\n<longcat_tool_call>ssh_read_file\n<longcat_arg_key>path</longcat_arg_key>\n<longcat_arg_value>/home/znsoft/prj8/src/tui.cpp</longcat_arg_value>\n</longcat_tool_call>\n"
	got := StripXMLToolCalls(input)
	if got != "让我继续读取文件。" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
	if unclosed := StripXMLToolCalls("可见\n<longcat_tool_call>ssh_read_file"); unclosed != "可见" {
		t.Fatalf("unclosed = %q", unclosed)
	}
}

func TestStripXMLToolCalls_RemovesHy3AndKeepsFollowingProse(t *testing.T) {
	input := "直接搜索。\n" +
		"<tool_calls:6124c78e>\n" +
		"<tool_call:6124c78e>web_search<tool_sep:6124c78e>\n" +
		"<arg_key:6124c78e>query</arg_key:6124c78e>\n" +
		"<arg_value:6124c78e>北京 天气 今天 实时</arg_value:6124c78e>\n" +
		"</tool_call:6124c78e>\n" +
		"</tool_calls:6124c78e>\n" +
		"查完再回答。"
	got := StripXMLToolCalls(input)
	if got != "直接搜索。\n\n查完再回答。" {
		t.Fatalf("StripXMLToolCalls() = %q", got)
	}
	if unclosed := StripXMLToolCalls("可见\n<tool_call:6124c78e>web_search<tool_sep:6124c78e>"); unclosed != "可见" {
		t.Fatalf("unclosed = %q", unclosed)
	}
	missingWrapper := "直接搜索。\n" +
		"<tool_calls:6124c78e>\n" +
		"<tool_call:6124c78e>web_search<tool_sep:6124c78e>\n" +
		"</tool_call:6124c78e>\n" +
		"查完再回答。"
	if got := StripXMLToolCalls(missingWrapper); got != "直接搜索。\n\n查完再回答。" {
		t.Fatalf("missing wrapper close = %q", got)
	}
}

func TestContentToolCallDeltaFilterSuppressesLongcat(t *testing.T) {
	var out strings.Builder
	filter := newContentToolCallDeltaFilter(func(delta string) { out.WriteString(delta) })
	filter.Write("让我继续读取文件。")
	filter.Write("<long")
	filter.Write("cat_tool_call>ssh_read_file\n")
	filter.Write("<longcat_arg_key>path</longcat_arg_key>\n")
	filter.Write("<longcat_arg_value>/home/znsoft/prj8/src/tui.cpp</longcat_arg_value>\n")
	filter.Write("</longcat_tool_call>")
	filter.Flush()
	if got := out.String(); got != "让我继续读取文件。" {
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
