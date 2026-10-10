package llm

import (
	"strings"
	"testing"
)

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
