package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestParseContentToolCallsDetailed_DeepSeekDSML(t *testing.T) {
	content := "好的，先查杭州最新天气，然后生成 PDF 报告给你。\n" +
		"<｜DSML｜tool_calls>\n" +
		"<｜DSML｜invoke name=\"web_search\">\n" +
		"<｜DSML｜parameter name=\"query\" string=\"true\">杭州天气预报 一周 8月20日 到 8月26日</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>\n" +
		"</｜DSML｜tool_calls>\n\n" +
		"<｜DSML｜tool_calls>\n" +
		"<｜DSML｜invoke name=\"web_search\">\n" +
		"<｜DSML｜parameter name=\"max_results\" string=\"false\">6</｜DSML｜parameter>\n" +
		"<｜DSML｜parameter name=\"query\" string=\"true\">杭州天气预报 未来一周 8月下旬</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>\n" +
		"</｜DSML｜tool_calls>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatal("expected DeepSeek DSML tool calls to parse cleanly")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "web_search" || calls[1].Function.Name != "web_search" {
		t.Fatalf("unexpected names: %#v", calls)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"query":"杭州天气预报 一周 8月20日 到 8月26日"`) {
		t.Fatalf("first arguments = %q", calls[0].Function.Arguments)
	}
	if strings.Contains(calls[1].Function.Arguments, "max_results") {
		t.Fatalf("DSML pagination must not reach web_search args: %q", calls[1].Function.Arguments)
	}
	if !strings.Contains(calls[1].Function.Arguments, `"query":"杭州天气预报 未来一周 8月下旬"`) {
		t.Fatalf("second arguments = %q", calls[1].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLDropsSearchPagination(t *testing.T) {
	content := `<｜DSML｜tool_calls>
<｜DSML｜invoke name="web_search">
<｜DSML｜parameter name="query" string="true">杭州天气</｜DSML｜parameter>
<｜DSML｜parameter name="count" string="false">5</｜DSML｜parameter>
</｜DSML｜invoke>
</｜DSML｜tool_calls>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("calls=%#v malformed=%v", calls, malformed)
	}
	if got := calls[0].Function.Arguments; got != `{"query":"杭州天气"}` {
		t.Fatalf("arguments = %q, want query only", got)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLRepeatedBars(t *testing.T) {
	// Recorded from a remote coding turn: the provider decoded the ｜DSML｜
	// token with the bar written twice and a space before the tag. The calls
	// are still the invoke elements of that token.
	content := "先看一下现有代码结构和 main.cpp 的输出方式。\n\n" +
		"<｜｜DSML｜｜ calls>\n" +
		"<｜｜DSML｜｜ invoke name=\"ssh_list_dir\">\n" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/home/znsoft/prj8</｜｜DSML｜｜ parameter>\n" +
		"</｜｜DSML｜｜ invoke>\n" +
		"<｜｜DSML｜｜ invoke name=\"ssh_read_file\">\n" +
		"<｜｜DSML｜｜ parameter name=\"path\" string=\"true\">/home/znsoft/prj8/src/main.cpp</｜｜DSML｜｜ parameter>\n" +
		"</｜｜DSML｜｜ invoke>\n" +
		"</｜｜DSML｜｜ calls>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatal("repeated-bar DSML is the same token and must parse")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %#v", calls)
	}
	if calls[0].Function.Name != "ssh_list_dir" || calls[1].Function.Name != "ssh_read_file" {
		t.Fatalf("names = %q, %q", calls[0].Function.Name, calls[1].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"/home/znsoft/prj8"`) {
		t.Fatalf("list args = %q", calls[0].Function.Arguments)
	}
	if !strings.Contains(calls[1].Function.Arguments, `"/home/znsoft/prj8/src/main.cpp"`) {
		t.Fatalf("read args = %q", calls[1].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_DSMLKeepsFullwidthBarInValue(t *testing.T) {
	content := "<｜DSML｜tool_calls>\n" +
		"<｜DSML｜invoke name=\"ssh_write_file\">\n" +
		"<｜DSML｜parameter name=\"content\" string=\"true\">列｜表</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>\n" +
		"</｜DSML｜tool_calls>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "列｜表") {
		t.Fatalf("fullwidth bar in value was rewritten: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_DSMLSpaceBeforeGt(t *testing.T) {
	content := "<｜DSML｜calls >\n" +
		"<｜DSML｜invoke name=\"ssh_list_dir\">\n" +
		"<｜DSML｜parameter name=\"path\" string=\"true\">/home/znsoft/prj8</｜DSML｜parameter >\n" +
		"</｜DSML｜invoke >\n" +
		"</｜DSML｜calls >"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "ssh_list_dir" {
		t.Fatalf("calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "/home/znsoft/prj8") {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_DSMLMentionIsNotACall(t *testing.T) {
	for _, content := range []string{
		"The <|DSML| token is documentation, not a call.",
		"The <|DSML| calls for a retry, not a tool.",
		"The <|DSML| parameter is a field.",
		"The <|DSML|invoke the helper.",
		"see < | DSML | note",
		"echo |DSML| ok",
	} {
		calls, malformed := ParseContentToolCallsDetailed(content)
		if len(calls) != 0 || malformed {
			t.Fatalf("%q parsed as a call: %#v malformed=%v", content, calls, malformed)
		}
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLSpacedASCII(t *testing.T) {
	content := `< | DSML | tool_calls>
< | DSML | invoke name="web_search">
< | DSML | parameter name="query" string="true">杭州天气</ | DSML | parameter>
</ | DSML | invoke>
</ | DSML | tool_calls>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("spaced DSML = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"query":"杭州天气"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLBareInvoke(t *testing.T) {
	content := `<｜DSML｜invoke name="web_search"><｜DSML｜parameter name="query" string="true">杭州天气</｜DSML｜parameter></｜DSML｜invoke>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("bare invoke = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLUnclosedIsMalformed(t *testing.T) {
	content := "先查天气\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"web_search\">"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if !malformed || len(calls) != 0 {
		t.Fatalf("unclosed DSML = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_ParenthesizedWebSearch(t *testing.T) {
	content := "让我们再确认一下北京天气：\n\n*web_search(query=\"北京天气 2024年1月15日\")*\n"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("calls=%#v malformed=%v", calls, malformed)
	}
	if got := calls[0].Function.Arguments; got != `{"query":"北京天气 2024年1月15日"}` {
		t.Fatalf("arguments = %q", got)
	}

	prose := "请使用 web_search 查询天气，不要把调用写在回复里。"
	if calls, malformed := ParseContentToolCallsDetailed(prose); len(calls) != 0 || malformed {
		t.Fatalf("prose mention was treated as a call: %#v malformed=%v", calls, malformed)
	}
	sample := "示例：\nread_file(path=\"main.go\")\nweb_search(query=\"北京天气\")\n*write_file(path=\"a.go\")*\n"
	if calls, malformed := ParseContentToolCallsDetailed(sample); len(calls) != 0 || malformed {
		t.Fatalf("code sample became a call: %#v malformed=%v", calls, malformed)
	}
	explained := strings.Repeat("这是一段正常说明。", 20) + "\n*web_search(query=\"只是举例\")*"
	if calls, malformed := ParseContentToolCallsDetailed(explained); len(calls) != 0 || malformed {
		t.Fatalf("wrapped lookup inside a longer reply = %#v malformed=%v", calls, malformed)
	}
	quoted := "*web_search(query=\"a)b\")*"
	calls, malformed = ParseContentToolCallsDetailed(quoted)
	if malformed || len(calls) != 1 || calls[0].Function.Arguments != `{"query":"a)b"}` {
		t.Fatalf("quoted paren = %#v malformed=%v", calls, malformed)
	}
	spaced := "我来查一下。\n* web_search(query=\"南京今天天气\") *"
	calls, malformed = ParseContentToolCallsDetailed(spaced)
	if malformed || len(calls) != 1 || calls[0].Function.Arguments != `{"query":"南京今天天气"}` {
		t.Fatalf("spaced wrapper = %#v malformed=%v", calls, malformed)
	}
	folded := "İstanbul\n*WEB_SEARCH(query=\"南京今天天气\")*"
	calls, malformed = ParseContentToolCallsDetailed(folded)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("folded name = %#v malformed=%v", calls, malformed)
	}
	bare := "*web_search(query=南京今天天气)*"
	calls, malformed = ParseContentToolCallsDetailed(bare)
	if malformed || len(calls) != 1 || calls[0].Function.Arguments != `{"query":"南京今天天气"}` {
		t.Fatalf("unquoted query = %#v malformed=%v", calls, malformed)
	}
	cut := "我来查。\n*web_search(query=\"南京"
	if calls, malformed = ParseContentToolCallsDetailed(cut); len(calls) != 0 || malformed {
		t.Fatalf("unfinished narration became an error: %#v malformed=%v", calls, malformed)
	}
}

func TestHoldContentToolCallStream_ParenthesizedWebSearch(t *testing.T) {
	content := "我来帮你查询南京今天的天气情况。\n\n*web_search(query=\"南京今天天气\")*"
	visible, hold, suppress := HoldContentToolCallStream(content, false)
	if suppress || !strings.Contains(hold, "web_search") {
		t.Fatalf("call should be held, not sticky-suppressed: visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	if strings.Contains(visible, "web_search") || strings.Contains(visible, "南京今天天气") {
		t.Fatalf("tool call leaked into visible text: %q", visible)
	}
	if !strings.Contains(visible, "我来帮你查询南京今天的天气情况。") {
		t.Fatalf("preamble dropped: %q", visible)
	}
	flushed, hold, suppress := HoldContentToolCallStream(content, true)
	if suppress || hold != "" || strings.Contains(flushed, "web_search") {
		t.Fatalf("flush should drop the narrated call: visible=%q hold=%q suppress=%v", flushed, hold, suppress)
	}
	continued := content + "\n" + strings.Repeat("这是一段正常说明。", 20)
	got, hold, suppress := HoldContentToolCallStream(continued, false)
	if suppress || !strings.Contains(got, "web_search") || !strings.Contains(got, "这是一段正常说明。") {
		t.Fatalf("text after a narrated call was swallowed: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	long := strings.Repeat("这是一段正常说明。", 20) + "\n*web_search(query=\"只是举例\")*"
	if got, hold, suppress := HoldContentToolCallStream(long, false); suppress || !strings.Contains(got, "web_search") {
		t.Fatalf("long reply hid a narrated example: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	plain := "说明\nread"
	if got, hold, suppress := HoldContentToolCallStream(plain, true); got != plain || hold != "" || suppress {
		t.Fatalf("ordinary trailing word was held: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	spaced := "我来查一下。\n* web_search(query=\"南京今天天气\") *"
	visible, hold, suppress = HoldContentToolCallStream(spaced, false)
	if suppress || strings.Contains(visible, "web_search") || !strings.Contains(hold, "web_search") {
		t.Fatalf("spaced wrapper leaked: visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	folded := "İstanbul 天气。\n*WEB_SEARCH(query=\"南京今天天气\")*"
	visible, hold, suppress = HoldContentToolCallStream(folded, false)
	if suppress || !strings.Contains(visible, "İstanbul") || !strings.Contains(strings.ToLower(hold), "web_search") {
		t.Fatalf("folded name sliced the preamble: visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	shortCut := "我来查。\n*web_search(query=\"南京"
	if got, hold, suppress := HoldContentToolCallStream(shortCut, true); got != shortCut || hold != "" || suppress {
		t.Fatalf("unfinished narration was replaced: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	longCut := strings.Repeat("这是一段正常说明。", 20) + "\n*web_search(query=\"只是举例\""
	if got, hold, suppress := HoldContentToolCallStream(longCut, true); suppress || hold != "" || !strings.Contains(got, "web_search") {
		t.Fatalf("unfinished example at the end of an answer was dropped: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	emphasis := "请使用这个名字：\n*web_search*"
	if got, hold, suppress := HoldContentToolCallStream(emphasis, true); got != emphasis || hold != "" || suppress {
		t.Fatalf("closed emphasis was dropped: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	if calls, malformed := ParseContentToolCallsDetailed(emphasis); len(calls) != 0 || malformed {
		t.Fatalf("closed emphasis became a call: %#v malformed=%v", calls, malformed)
	}
	prefix := "说明\n*web_se"
	if got, hold, suppress := HoldContentToolCallStream(prefix, false); suppress || !strings.HasSuffix(hold, "*web_se") || strings.Contains(got, "web_se") {
		t.Fatalf("open tool-name prefix was not held: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
	if got, hold, suppress := HoldContentToolCallStream(prefix, true); got != prefix || hold != "" || suppress {
		t.Fatalf("open tool-name prefix was dropped: visible=%q hold=%q suppress=%v", got, hold, suppress)
	}
}

func TestParseContentToolCallsDetailed_WebSearchDropsForgedDestination(t *testing.T) {
	content := `<tool_call>{"name":"web_search","arguments":{"query":"杭州天气","channel":"lansenger"}}</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("calls=%#v malformed=%v", calls, malformed)
	}
	if got := calls[0].Function.Arguments; got != `{"query":"杭州天气"}` {
		t.Fatalf("destination must not survive content parse: %q", got)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLDoesNotStealXMLToolCall(t *testing.T) {
	content := "Do not emit | DSML | by hand.\n<tool_call>{\"name\":\"web_search\",\"arguments\":{\"query\":\"杭州天气\"}}</tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("prose DSML mention stole XML tool call: calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLLeftoverInvokeAfterBlock(t *testing.T) {
	content := `<｜DSML｜tool_calls>
<｜DSML｜invoke name="web_search">
<｜DSML｜parameter name="query" string="true">杭州天气</｜DSML｜parameter>
</｜DSML｜invoke>
</｜DSML｜tool_calls>
<｜DSML｜invoke name="web_search">
<｜DSML｜parameter name="query" string="true">杭州一周天气</｜DSML｜parameter>
</｜DSML｜invoke>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 2 {
		t.Fatalf("leftover invoke = calls=%d malformed=%v", len(calls), malformed)
	}
}

func TestParseContentToolCallsDetailed_DeepSeekDSMLFunctionCallsAlias(t *testing.T) {
	content := `<|DSML|function_calls><|DSML|invoke name="web_search"><|DSML|parameter name="query" string="true">杭州天气</|DSML|parameter></|DSML|invoke></|DSML|function_calls>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("function_calls alias = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_CodexInvoke(t *testing.T) {
	content := `<turn: tool_call>
<invoke name="bash">
<parameter name="description" string="true">Check existing project directory</parameter>
<parameter name="command" string="true">if exist D:\gametest\15\ (dir /B D:\gametest\15\) else (echo DIRECTORY_NOT_EXIST)</parameter>
<parameter name="timeout" string="false">5000</parameter>
</invoke>
</turn>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected codex tool call to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("expected bash, got %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"command":"if exist D:\\gametest\\15\\ (dir /B D:\\gametest\\15\\) else (echo DIRECTORY_NOT_EXIST)","description":"Check existing project directory","timeout":5000}` {
		t.Fatalf("unexpected arguments JSON: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_CodexTurnFunctionEqIsNotDropped(t *testing.T) {
	content := `<turn: tool_call>
<function=bash>
<parameter=command>python gen_poster_v4.py</parameter>
</function>
</turn>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("codex-wrapped function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"python gen_poster_v4.py"}` {
		t.Fatalf("arguments = %q", got)
	}
}

func TestParseContentToolCallsDetailed_PlainToolCallSSHExecuteCommand(t *testing.T) {
	content := `步骤1：查看磁盘使用情况
TOOL_CALL
{
  "function": "ssh_execute_command",
  "args": {
    "host": "example.com",
    "port": 22,
    "username": "root",
    "password": "<redacted>",
    "command": "df -h"
  }
}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected plain tool call to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "ssh" {
		t.Fatalf("tool name = %q, want ssh", calls[0].Function.Name)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments are not JSON: %v", err)
	}
	if args["action"] != "connect" {
		t.Fatalf("action = %#v, want connect", args["action"])
	}
	if args["user"] != "root" {
		t.Fatalf("user = %#v, want root", args["user"])
	}
	if args["initial_command"] != "df -h" {
		t.Fatalf("initial_command = %#v, want df -h", args["initial_command"])
	}
	if _, ok := args["username"]; ok {
		t.Fatalf("username should be normalized away: %#v", args)
	}
}

func TestParseContentToolCallsDetailed_PlainToolCallOpenAIArgumentsString(t *testing.T) {
	content := `TOOL_CALL
{"function":{"name":"bash","arguments":"{\"command\":\"dir\",\"timeout\":5000}"}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected OpenAI-style arguments string to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir","timeout":5000}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_XMLToolCallOpenAIArgumentsString(t *testing.T) {
	content := `<tool_call>{"name":"bash","arguments":"{\"command\":\"dir\"}"}</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected XML OpenAI-style arguments string to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_XMLToolCallNestedOpenAIFunction(t *testing.T) {
	content := `<tool_call>{"function":{"name":"bash","arguments":"{\"command\":\"dir\"}"}}</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected XML nested OpenAI function to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_AngleArrayToolCallWithoutClose(t *testing.T) {
	content := `<tool_call[]>
{"name":"write_file","arguments":{"file_path":"e:\\CRM\\docs\\technical-design.md","content":"hello"}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected angle array tool call to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q, want write_file", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"file_path"`) {
		t.Fatalf("arguments missing file_path: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_GLMArgKeyToolCall(t *testing.T) {
	content := `<tool_call>write_file
<arg_key>path</arg_key>
<arg_value>C:\Users\ma139\.maclaw\workspace\gen_poster_v4.py</arg_value>
<arg_key>content</arg_key>
<arg_value>from PIL import Image
print("ok")</arg_value>
</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("GLM arg_key = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"C:\\Users\\ma139\\.maclaw\\workspace\\gen_poster_v4.py"`) {
		t.Fatalf("path missing: %q", calls[0].Function.Arguments)
	}
	if !strings.Contains(calls[0].Function.Arguments, "from PIL import Image") {
		t.Fatalf("content missing: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LongcatArgKey(t *testing.T) {
	content := "<longcat_tool_call>ssh_read_file\n" +
		"<longcat_arg_key>path</longcat_arg_key>\n" +
		"<longcat_arg_value>/home/znsoft/prj8/src/tui.cpp</longcat_arg_value>\n" +
		"<longcat_arg_key>offset</longcat_arg_key>\n" +
		"<longcat_arg_value>401</longcat_arg_value>\n" +
		"<longcat_arg_key>limit</longcat_arg_key>\n" +
		"<longcat_arg_value>400</longcat_arg_value>\n" +
		"</longcat_tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("longcat arg pairs = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "ssh_read_file" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments: %v %q", err, calls[0].Function.Arguments)
	}
	if args["path"] != "/home/znsoft/prj8/src/tui.cpp" {
		t.Fatalf("path = %#v", args["path"])
	}
	if args["offset"] != float64(401) || args["limit"] != float64(400) {
		t.Fatalf("numeric args = %#v", args)
	}
}

func TestParseContentToolCallsDetailed_LongcatJSONAndBareName(t *testing.T) {
	content := "<longcat_tool_call>\n{\"name\":\"ssh_bash\",\"arguments\":{\"command\":\"ls\"}}\n</longcat_tool_call>\n" +
		"<longcat_tool_call>ssh_list_dir</longcat_tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 2 {
		t.Fatalf("longcat json = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "ssh_bash" || calls[0].Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("first call = %s %s", calls[0].Function.Name, calls[0].Function.Arguments)
	}
	if calls[1].Function.Name != "ssh_list_dir" || calls[1].Function.Arguments != "{}" {
		t.Fatalf("second call = %s %s", calls[1].Function.Name, calls[1].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LongcatKeepsTextValues(t *testing.T) {
	content := "<longcat_tool_call>ssh_write_file\n" +
		"<longcat_arg_key>path</longcat_arg_key>\n" +
		"<longcat_arg_value>401</longcat_arg_value>\n" +
		"<longcat_arg_key>content</longcat_arg_key>\n" +
		"<longcat_arg_value>{\"a\":1}</longcat_arg_value>\n" +
		"<longcat_arg_key>query</longcat_arg_key>\n" +
		"<longcat_arg_value>[\"China only tropical province\"]</longcat_arg_value>\n" +
		"</longcat_tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("longcat text values = calls=%#v malformed=%v", calls, malformed)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments: %v %q", err, calls[0].Function.Arguments)
	}
	if args["path"] != "401" || args["content"] != `{"a":1}` || args["query"] != `["China only tropical province"]` {
		t.Fatalf("text args = %#v", args)
	}
}

func TestParseContentToolCallsDetailed_LongcatUnlistedTextKeyStaysString(t *testing.T) {
	content := "<longcat_tool_call>ssh_write_file\n" +
		"<longcat_arg_key>old_content</longcat_arg_key>\n" +
		"<longcat_arg_value>401</longcat_arg_value>\n" +
		"<longcat_arg_key>task_id</longcat_arg_key>\n" +
		"<longcat_arg_value>12</longcat_arg_value>\n" +
		"<longcat_arg_key>id</longcat_arg_key>\n" +
		"<longcat_arg_value>99</longcat_arg_value>\n" +
		"<longcat_arg_key>message</longcat_arg_key>\n" +
		"<longcat_arg_value>true</longcat_arg_value>\n" +
		"<longcat_arg_key>start_line</longcat_arg_key>\n" +
		"<longcat_arg_value>10</longcat_arg_value>\n" +
		"</longcat_tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("unlisted keys = calls=%#v malformed=%v", calls, malformed)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments: %v %q", err, calls[0].Function.Arguments)
	}
	if args["old_content"] != "401" || args["task_id"] != "12" || args["id"] != "99" || args["message"] != "true" {
		t.Fatalf("text args = %#v", args)
	}
	if args["start_line"] != float64(10) {
		t.Fatalf("start_line = %#v", args["start_line"])
	}
}

func TestParseContentToolCallsDetailed_LongcatMissingCloseDoesNotMerge(t *testing.T) {
	content := "<longcat_tool_call>ssh_read_file\n" +
		"<longcat_arg_key>path</longcat_arg_key>\n" +
		"<longcat_arg_value>/tmp/a.cpp</longcat_arg_value>\n" +
		"<longcat_tool_call>ssh_bash\n" +
		"<longcat_arg_key>command</longcat_arg_key>\n" +
		"<longcat_arg_value>ls</longcat_arg_value>\n" +
		"</longcat_tool_call>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 2 {
		t.Fatalf("split calls = %#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "ssh_read_file" || calls[1].Function.Name != "ssh_bash" {
		t.Fatalf("names = %s %s", calls[0].Function.Name, calls[1].Function.Name)
	}
	if strings.Contains(calls[0].Function.Arguments, "command") || !strings.Contains(calls[1].Function.Arguments, `"ls"`) {
		t.Fatalf("arguments merged: %s | %s", calls[0].Function.Arguments, calls[1].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LongcatInsideToolCallIsNotStolen(t *testing.T) {
	content := `<tool_call>{"name":"write_file","arguments":{"path":"notes.md","content":"<longcat_tool_call>ssh_read_file</longcat_tool_call>"}}</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "write_file" {
		t.Fatalf("stolen = %#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "longcat_tool_call") {
		t.Fatalf("file text dropped: %s", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LongcatUnclosedCompleteStillRuns(t *testing.T) {
	content := "<longcat_tool_call>ssh_read_file\n" +
		"<longcat_arg_key>path</longcat_arg_key>\n" +
		"<longcat_arg_value>/tmp/a.cpp</longcat_arg_value>\n"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "ssh_read_file" {
		t.Fatalf("unclosed complete = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"/tmp/a.cpp"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LongcatUnclosedIsMalformed(t *testing.T) {
	content := "<longcat_tool_call>ssh_read_file\n<longcat_arg_key>path</longcat_arg_key>\n<longcat_arg_value>/tmp/a"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if len(calls) != 0 || !malformed {
		t.Fatalf("unclosed incomplete = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestHoldContentToolCallStream_UnicodeBeforeMarker(t *testing.T) {
	// U+0130 lowercases to two runes, so a lowered copy would shift the cut.
	const lead = "İ"
	s := lead + "<longcat_tool_call>"
	visible, hold, suppress := HoldContentToolCallStream(s, false)
	if !suppress || hold != "" || visible != lead {
		t.Fatalf("visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
}

func TestParseContentToolCallsDetailed_LongcatWordIsNotACall(t *testing.T) {
	content := "LongCat writes longcat_tool_call in its prompt, then answers."
	calls, malformed := ParseContentToolCallsDetailed(content)
	if len(calls) != 0 || malformed {
		t.Fatalf("prose = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_QwenFunctionEq(t *testing.T) {
	content := `<function=bash>
<parameter=command>python gen_poster_v4.py</parameter>
</function>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"python gen_poster_v4.py"}` {
		t.Fatalf("arguments = %q", got)
	}
}

func TestParseContentToolCallsDetailed_ToolCallNamedInvoke(t *testing.T) {
	content := `<tool_call name="write_file"><invoke name="write_file"><parameter name="path">poster.py</parameter><parameter name="content">print(1)</parameter></invoke></tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("named invoke = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"poster.py"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_FunctionEqInsideToolCallIsNotDuplicated(t *testing.T) {
	content := `<tool_call>{"name":"write_file","arguments":{"path":"notes.md","content":"<function=bash>\n<parameter=command>dir</parameter>\n</function>"}}</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("nested function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q, want write_file only", calls[0].Function.Name)
	}
}

func TestParseContentToolCallsDetailed_FunctionEqInsideGLMArgsIsNotStolen(t *testing.T) {
	content := `<tool_call>write_file
<arg_key>path</arg_key>
<arg_value>notes.md</arg_value>
<arg_key>content</arg_key>
<arg_value>
<function=bash>
<parameter=command>dir</parameter>
</function>
</arg_value>
</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("GLM nested function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "write_file" {
		t.Fatalf("tool name = %q, want write_file not inner bash", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"notes.md"`) {
		t.Fatalf("path missing: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_MultipleFunctionEqInsideToolCall(t *testing.T) {
	content := `<tool_call>
<function=write_file>
<parameter=path>gen_poster.py</parameter>
<parameter=content>print(1)</parameter>
</function>
<function=bash>
<parameter=command>python gen_poster.py</parameter>
</function>
</tool_call>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 2 {
		t.Fatalf("paired function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "write_file" || calls[1].Function.Name != "bash" {
		t.Fatalf("tool names = %q %q", calls[0].Function.Name, calls[1].Function.Name)
	}
}

func TestParseContentToolCallsDetailed_CodexWrappedXMLFunctionEqIsNotDuplicated(t *testing.T) {
	content := `<turn: tool_call>
<tool_call>
<function=bash>
<parameter=command>python gen_poster.py</parameter>
</function>
</tool_call>
</turn>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("codex+xml function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want one bash call", calls[0].Function.Name)
	}
}

func TestParseContentToolCallsDetailed_UnclosedToolCallFunctionEqIsNotDuplicated(t *testing.T) {
	content := `<tool_call>
<function=bash>
<parameter=command>python gen_poster_v4.py</parameter>
</function>`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("unclosed tool_call + function= = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want one bash call", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"python gen_poster_v4.py"}` {
		t.Fatalf("arguments = %q", got)
	}
}

func TestParseContentToolCallsDetailed_UnclosedNameOnlyIsMalformed(t *testing.T) {
	calls, malformed := ParseContentToolCallsDetailed(`<tool_call>write_file`)
	if !malformed || len(calls) != 0 {
		t.Fatalf("name-only unclosed tool call = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_PlainToolCallToolParametersAliases(t *testing.T) {
	content := `TOOL_CALL {"tool":"bash","parameters":{"command":"dir"}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected tool/parameters aliases to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_PlainToolCallNameInputAliases(t *testing.T) {
	content := `TOOL_CALL {"name":"bash","input":{"command":"dir"}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected name/input aliases to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_BareJSONOpenAIToolCalls(t *testing.T) {
	content := `{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"dir\"}"}}]}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected bare OpenAI tool_calls JSON to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_1" {
		t.Fatalf("tool call id = %q, want call_1", calls[0].ID)
	}
	if calls[0].Type != "function" {
		t.Fatalf("tool call type = %q, want function", calls[0].Type)
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_BareJSONFunctionObject(t *testing.T) {
	content := `{"function":{"name":"bash","arguments":{"command":"dir"}}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected bare function object to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_BareJSONLegacyFunctionCall(t *testing.T) {
	content := `{"function_call":{"name":"bash","arguments":"{\"command\":\"dir\"}"}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected bare legacy function_call JSON to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
	if got := calls[0].Function.Arguments; got != `{"command":"dir"}` {
		t.Fatalf("arguments = %q, want JSON object", got)
	}
}

func TestParseContentToolCallsDetailed_BareJSONCallIDAlias(t *testing.T) {
	content := `{"call_id":"call_alias","function":{"name":"bash","arguments":{"command":"dir"}}}`
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected bare function object with call_id to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_alias" {
		t.Fatalf("tool call id = %q, want call_alias", calls[0].ID)
	}
}

func TestParseContentToolCallsDetailed_FencedBareJSONToolCall(t *testing.T) {
	content := "```json\n{\"name\":\"bash\",\"arguments\":{\"command\":\"dir\"}}\n```"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected fenced bare JSON tool call to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("tool name = %q, want bash", calls[0].Function.Name)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedLeakedGlob(t *testing.T) {
	content := "截图里高亮的是「几何图像」中 `R^n` 没进公式渲染。正在定位第16页源文件并修复。\n terc3 glob_file_search_tool\npath\nC:\\Users\\ma139\\.maclaw\\workspace\\ai-math-handbook\nglob_pattern\n**/*.{html,md,json}<|eos|>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed {
		t.Fatal("expected leaked line-oriented glob call to parse")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %#v", calls)
	}
	if calls[0].Function.Name != "glob_file_search_tool" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"C:\\Users\\ma139\\.maclaw\\workspace\\ai-math-handbook"`) {
		t.Fatalf("path missing: %q", calls[0].Function.Arguments)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"glob_pattern":"**/*.{html,md,json}"`) {
		t.Fatalf("glob_pattern missing: %q", calls[0].Function.Arguments)
	}
	if strings.Contains(calls[0].Function.Arguments, "<|eos|>") {
		t.Fatalf("special token leaked into args: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedColonArgs(t *testing.T) {
	content := "search_files\npath: src\npattern: **/*.go"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "search_files" {
		t.Fatalf("colon args = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"pattern":"**/*.go"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedWebSearch(t *testing.T) {
	content := "请稍等，我先查询实时信息。\nweb_search\nquery\n北京今天的天气\nmax_results\n5<|eos|>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("web search line call = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"query":"北京今天的天气"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedLastCallWins(t *testing.T) {
	content := "read_file\npath\nC:\\tmp\\old.md\nglob_file_search_tool\npath\nC:\\tmp\\book\nglob_pattern\n**/*.md"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "glob_file_search_tool" {
		t.Fatalf("last call = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"C:\\tmp\\book"`) {
		t.Fatalf("wanted trailing glob path, got %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedBlankValueLine(t *testing.T) {
	content := "glob_file_search_tool\npath\n\nC:\\tmp\\book\nglob_pattern\n**/*.md"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("blank value line = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"path":"C:\\tmp\\book"`) {
		t.Fatalf("path missing after blank line: %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedTrailingProseStillParses(t *testing.T) {
	content := "glob_file_search_tool\npath\nC:\\tmp\\book\nglob_pattern\n**/*.md\n接下来会读取源文件。"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "glob_file_search_tool" {
		t.Fatalf("trailing prose = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"glob_pattern":"**/*.md"`) {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_UseReadFileIsNotAToolCall(t *testing.T) {
	content := "Use read_file\npath\nto the workspace"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 0 {
		t.Fatalf("english prefix stole a tool call: calls=%#v malformed=%v", calls, malformed)
	}
}

func TestHoldContentToolCallStream_BareGlobLineIsNotSuppressed(t *testing.T) {
	content := "Next I will use\nGlob\nto find markdown files."
	visible, hold, suppress := HoldContentToolCallStream(content, true)
	if suppress || hold != "" || visible != content {
		t.Fatalf("bare Glob line suppressed prose: visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
}

func TestParseContentToolCallsDetailed_NameOnlyLeakedToolIsMalformed(t *testing.T) {
	content := "正在定位源文件。\n glob_file_search_tool"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if !malformed || len(calls) != 0 {
		t.Fatalf("name-only leaked tool = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestStripAllExtraRemovesLineOrientedGlobTail(t *testing.T) {
	input := "正在定位第16页源文件并修复。\n terc3 glob_file_search_tool\npath\nC:\\tmp\\book\nglob_pattern\n**/*.md<|eos|>"
	got := StripAllExtra(input)
	if strings.Contains(got, "glob_file_search_tool") || strings.Contains(got, "terc3") || strings.Contains(got, "glob_pattern") {
		t.Fatalf("StripAllExtra leaked line-oriented tool call: %q", got)
	}
	if got != "正在定位第16页源文件并修复。" {
		t.Fatalf("StripAllExtra() = %q", got)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedProseIsNotAToolCall(t *testing.T) {
	content := "Please use glob_file_search_tool if you need to find files by pattern."
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 0 {
		t.Fatalf("prose mention parsed as tool call: calls=%#v malformed=%v", calls, malformed)
	}
}

func TestParseContentToolCallsDetailed_LineOrientedUnparsedEOSIsMalformed(t *testing.T) {
	content := "正在定位源文件。\n glob_file_search_tool\n<path\n<|eos|>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if !malformed || len(calls) != 0 {
		t.Fatalf("unparsed leaked tool markup = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestHoldContentToolCallStream_DoesNotHoldOrdinaryGlobProse(t *testing.T) {
	for _, content := range []string{"Please use glob", "use glob", "That is global"} {
		visible, hold, suppress := HoldContentToolCallStream(content, true)
		if suppress || hold != "" || visible != content {
			t.Fatalf("ordinary prose %q was treated as a leaked tool line: visible=%q hold=%q suppress=%v", content, visible, hold, suppress)
		}
	}
}

func TestHoldContentToolCallStream_HoldsDigitJunkGlobPrefix(t *testing.T) {
	visible, hold, suppress := HoldContentToolCallStream("正在定位。\n terc3 glob", false)
	if suppress || strings.Contains(visible, "terc3") {
		t.Fatalf("digit-junk glob prefix should be held: visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	if !strings.Contains(hold, "terc3 glob") {
		t.Fatalf("expected hold of terc3 glob prefix, hold=%q", hold)
	}
}

func TestHoldContentToolCallStream_HoldsIncompleteLeakedGlobLine(t *testing.T) {
	visible, hold, suppress := HoldContentToolCallStream("正在定位第16页源文件并修复。\n terc3 glob_", false)
	if suppress {
		t.Fatal("incomplete leaked glob line should be held, not suppressed yet")
	}
	if strings.Contains(visible, "terc3") || strings.Contains(visible, "glob_") {
		t.Fatalf("incomplete leaked glob line was visible: %q hold=%q", visible, hold)
	}
	if !strings.Contains(hold, "glob_") {
		t.Fatalf("expected hold of incomplete glob line, hold=%q", hold)
	}
}

func TestFirstContentToolCallMarkerIndex_LineOrientedGlob(t *testing.T) {
	content := "正在定位第16页源文件并修复。\n terc3 glob_file_search_tool\npath\nC:\\tmp"
	idx := FirstContentToolCallMarkerIndex(content)
	if idx < 0 || !strings.Contains(content[idx:], "glob_file_search_tool") {
		t.Fatalf("marker index = %d, want leaked tool line", idx)
	}
	visible, _, suppress := HoldContentToolCallStream(content, true)
	if !suppress {
		t.Fatal("leaked glob tool call must be suppressed from the stream")
	}
	if strings.Contains(visible, "glob_file_search_tool") || strings.Contains(visible, "terc3") {
		t.Fatalf("leaked tool text visible: %q", visible)
	}
	if !strings.Contains(visible, "正在定位第16页源文件并修复。") {
		t.Fatalf("prose before leaked tool was dropped: %q", visible)
	}
}

func TestParseContentToolCallsDetailed_BareJSONDoesNotMisclassifyData(t *testing.T) {
	for _, content := range []string{
		`{"name":"Alice","city":"Beijing"}`,
		`[{"name":"Alice"}]`,
	} {
		calls, malformed := ParseContentToolCallsDetailed(content)
		if malformed {
			t.Fatalf("ordinary JSON should not be malformed: %s", content)
		}
		if len(calls) != 0 {
			t.Fatalf("ordinary JSON parsed as tool call: %#v", calls)
		}
	}
}

func TestParseContentToolCallsDetailed_Hy3SuffixedWebSearch(t *testing.T) {
	content := "直接搜索。\n" +
		"<tool_call:6124c78e>web_search<tool_sep:6124c78e>\n" +
		"<arg_key:6124c78e>query</arg_key:6124c78e>\n" +
		"<arg_value:6124c78e>北京 天气 今天 实时</arg_value:6124c78e>\n" +
		"</tool_call:6124c78e>\n" +
		"</tool_calls:6124c78e>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("hy3 web_search = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "web_search" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"query":"北京 天气 今天 实时"}` {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_Hy3OpensourceKeepsNumbers(t *testing.T) {
	content := "<tool_calls:opensource>\n" +
		"<tool_call:opensource>ssh_read_file<tool_sep:opensource>\n" +
		"<arg_key:opensource>path</arg_key:opensource>\n" +
		"<arg_value:opensource>/tmp/a</arg_value:opensource>\n" +
		"<arg_key:opensource>offset</arg_key:opensource>\n" +
		"<arg_value:opensource>401</arg_value:opensource>\n" +
		"<arg_key:opensource>limit</arg_key:opensource>\n" +
		"<arg_value:opensource>400</arg_value:opensource>\n" +
		"</tool_call:opensource>\n" +
		"</tool_calls:opensource>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 {
		t.Fatalf("hy3 opensource = calls=%#v malformed=%v", calls, malformed)
	}
	if calls[0].Function.Name != "ssh_read_file" {
		t.Fatalf("tool name = %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"/tmp/a"`) {
		t.Fatalf("path = %q", calls[0].Function.Arguments)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"offset":401`) || !strings.Contains(calls[0].Function.Arguments, `"limit":400`) {
		t.Fatalf("numeric args = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_Hy3UnclosedCompleteStillRuns(t *testing.T) {
	content := "<tool_call:ab12>web_search<tool_sep:ab12>\n" +
		"<arg_key:ab12>query</arg_key:ab12>\n" +
		"<arg_value:ab12>北京天气</arg_value:ab12>\n"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("unclosed hy3 = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "北京天气") {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_Hy3UppercaseSuffix(t *testing.T) {
	content := "<tool_call:OpenSource>web_search<tool_sep:OpenSource>\n" +
		"<arg_key:OpenSource>query</arg_key:OpenSource>\n" +
		"<arg_value:OpenSource>北京天气</arg_value:OpenSource>\n" +
		"</tool_call:OpenSource>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("uppercase suffix = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "北京天气") {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_Hy3AdjacentEmptyOpenStillRuns(t *testing.T) {
	content := "<tool_call:abc><tool_call:abc>web_search<tool_sep:abc>\n" +
		"<arg_key:abc>query</arg_key:abc>\n" +
		"<arg_value:abc>北京天气</arg_value:abc>\n" +
		"</tool_call:abc>"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("adjacent open = calls=%#v malformed=%v", calls, malformed)
	}
	if !strings.Contains(calls[0].Function.Arguments, "北京天气") {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
}

func TestParseContentToolCallsDetailed_Hy3MentionIsNotACall(t *testing.T) {
	content := "模板写成 <tool_call:opensource> 这样，后面才是参数。"
	calls, malformed := ParseContentToolCallsDetailed(content)
	if malformed || len(calls) != 0 {
		t.Fatalf("mention = calls=%#v malformed=%v", calls, malformed)
	}
}

func TestAdoptLeakedToolCalls_Hy3InReasoningDropsPrematureAnswer(t *testing.T) {
	msg := &Message{
		Content: "根据搜索到的公开预报信息：北京 26°C",
		ReasoningContent: "直接搜索。\n" +
			"<tool_call:6124c78e>web_search<tool_sep:6124c78e>\n" +
			"<arg_key:6124c78e>query</arg_key:6124c78e>\n" +
			"<arg_value:6124c78e>北京 天气 今天 实时</arg_value:6124c78e>\n" +
			"</tool_call:6124c78e>\n" +
			"</tool_calls:6124c78e>",
	}
	finish, changed := adoptLeakedToolCalls(msg, msg.Content)
	if !changed || finish != "tool_calls" {
		t.Fatalf("finish=%q changed=%v", finish, changed)
	}
	if msg.Content != "" {
		t.Fatalf("premature answer kept: %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("tool calls = %#v", msg.ToolCalls)
	}
	if msg.ReasoningContent != "直接搜索。" {
		t.Fatalf("reasoning = %q", msg.ReasoningContent)
	}
}

func TestHoldHy3ToolMarkupKeepsProse(t *testing.T) {
	visible, hold, suppress := HoldHy3ToolMarkup("直接搜索。\n<tool_cal", false)
	if suppress || hold != "<tool_cal" || visible != "直接搜索。\n" {
		t.Fatalf("partial = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	visible, hold, suppress = HoldHy3ToolMarkup("直接搜索。\n<tool_call:6124c78e>web_search", false)
	if !suppress || hold != "" || visible != "直接搜索。\n" {
		t.Fatalf("open = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
	visible, hold, suppress = HoldHy3ToolMarkup("直接搜索。\n<tool_cal", true)
	if suppress || hold != "" || visible != "直接搜索。\n" {
		t.Fatalf("flush partial = visible=%q hold=%q suppress=%v", visible, hold, suppress)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsLineOrientedGlob(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"正在定位第16页源文件并修复。\n terc3 glob_file_search_tool\npath\nC:\\tmp\\book\nglob_pattern\n**/*.md<|eos|>"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected converted tool call, got %#v", resp.Choices)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Function.Name; got != "glob_file_search_tool" {
		t.Fatalf("tool name = %q", got)
	}
	if strings.TrimSpace(resp.Choices[0].Message.Content) != "" {
		t.Fatalf("leaked tool text remained in content: %q", resp.Choices[0].Message.Content)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsPlainToolCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"TOOL_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"host\":\"example.com\",\"username\":\"root\",\"command\":\"df -h\"}}"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsBareJSONToolCalls(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"{\"tool_calls\":[{\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}]}"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsBareJSONLegacyFunctionCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"{\"function_call\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestOpenAISDKChatStreamSuppressesPlainToolCallTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"开始执行\nTOOL"},"finish_reason":null}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"host\":\"example.com\",\"username\":\"root\",\"password\":\"<redacted>\",\"command\":\"df -h\"}}"},"finish_reason":null}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	var streamed strings.Builder
	resp, _, _, err := openAISDKChatStream(context.Background(), corelib.MaclawLLMConfig{
		URL:   srv.URL + "/v1",
		Key:   "test-key",
		Model: "test-model",
	}, []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"stream":true}`), srv.Client(), func(delta string) {
		streamed.WriteString(delta)
	}, nil)
	if err != nil {
		t.Fatalf("openAISDKChatStream: %v", err)
	}
	if strings.Contains(streamed.String(), "TOOL") || strings.Contains(streamed.String(), "password") {
		t.Fatalf("streamed tool JSON leaked: %q", streamed.String())
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Function.Name; got != "ssh" {
		t.Fatalf("tool name = %q, want ssh", got)
	}
}

func TestOpenAISDKChatRawPreservesToolSchemaBody(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		captured = string(data)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "chatcmpl-test",
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{URL: srv.URL + "/v1", Key: "test-key", Model: "test-model"}
	_, body, err := BuildOpenAIChatRequestData(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name": "get_weather",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
					"required":   []interface{}{"city"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("BuildOpenAIChatRequestData: %v", err)
	}
	if strings.Contains(string(body), `"properties":{"city":{"type":"string"},"properties":{},"type":"object"}`) {
		t.Fatalf("builder changed tool schema before SDK: %s", string(body))
	}
	if _, _, err := openAISDKChatRaw(context.Background(), cfg, body, srv.Client()); err != nil {
		t.Fatalf("openAISDKChatRaw: %v", err)
	}
	if !strings.Contains(captured, `"parameters":{"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"}`) {
		t.Fatalf("SDK request changed tool schema: %s", captured)
	}
}

func TestOpenAISDKRawBodyTransportDoesNotRestoreRequestReplayForOwnerBoundRequest(t *testing.T) {
	body := []byte(`{"model":"test-model","tools":[]}`)
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.GetBody != nil {
			t.Fatal("raw-body SDK transport restored a replayable request body")
		}
		got, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(body) {
			t.Fatalf("wire body = %s, want %s", got, body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", strings.NewReader(`caller body`))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the normal bytes.Reader setup used by SDK callers. The wrapper
	// must not leave this replay hook in place after replacing the exact body.
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("caller body")), nil
	}
	request = request.WithContext(WithTransparentRequestRetriesDisabled(request.Context()))
	response, err := (openAISDKRawBodyTransport{base: base, body: body}).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestOpenAISDKUnusedStreamPathDoesNotFollowOwnerBoundRedirect(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/redirected", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	ctx := WithTransparentRequestRetriesDisabled(context.Background())
	_, _, _, err := openAISDKChatStreamUnused(ctx, corelib.MaclawLLMConfig{
		URL:   source.URL + "/v1",
		Model: "test-model",
	}, []byte(`{"model":"test-model","stream":true,"tools":[]}`), source.Client(), nil, nil)
	if err == nil {
		t.Fatal("owner-bound redirect unexpectedly completed")
	}
	if got := redirected.Load(); got != 0 {
		t.Fatalf("owner-bound SDK stream followed redirect %d times", got)
	}
}

func TestParseAnthropicResponseBody_ConvertsPlainToolCall(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"TOOL_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"host\":\"example.com\",\"username\":\"root\",\"command\":\"df -h\"}}"}],"stop_reason":"end_turn"}`)
	resp, err := parseAnthropicResponseBody(body)
	if err != nil {
		t.Fatalf("parseAnthropicResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestParseAnthropicSSEStream_ConvertsPlainToolCallAndSuppressesTokens(t *testing.T) {
	body := strings.NewReader(
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"checking\nTOOL"}}` + "\n\n" +
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"host\":\"example.com\",\"username\":\"root\",\"password\":\"<redacted>\",\"command\":\"df -h\"}}"}}` + "\n\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n",
	)
	var streamed strings.Builder
	resp, err := parseAnthropicSSEStream(body, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseAnthropicSSEStream: %v", err)
	}
	if strings.Contains(streamed.String(), "TOOL") || strings.Contains(streamed.String(), "password") {
		t.Fatalf("streamed tool JSON leaked: %q", streamed.String())
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Function.Name; got != "ssh" {
		t.Fatalf("tool name = %q, want ssh", got)
	}
}

func TestParseAnthropicSSEStream_SuppressesDetailsAndConvertsAngleArrayToolCall(t *testing.T) {
	body := strings.NewReader(
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"visible\n<det"}}` + "\n\n" +
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ails><summary>思考过程</summary>hidden</details>\n<tool"}}` + "\n\n" +
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"_call[]>\n{\"name\":\"write_file\",\"arguments\":{\"file_path\":\"e:\\\\CRM\\\\docs\\\\technical-design.md\",\"content\":\"hello\"}}"}}` + "\n\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n",
	)
	var streamed strings.Builder
	resp, err := parseAnthropicSSEStream(body, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseAnthropicSSEStream: %v", err)
	}
	streamedText := streamed.String()
	if strings.Contains(streamedText, "<details") || strings.Contains(streamedText, "hidden") || strings.Contains(streamedText, "<tool_call") || strings.Contains(streamedText, "write_file") {
		t.Fatalf("streamed hidden/tool content leaked: %q", streamedText)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	msg := resp.Choices[0].Message
	if msg.Content != "" {
		t.Fatalf("content = %q, want empty when content tool call is converted", msg.Content)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := msg.ToolCalls[0].Function.Name; got != "write_file" {
		t.Fatalf("tool name = %q, want write_file", got)
	}
	if !strings.Contains(msg.ToolCalls[0].Function.Arguments, `"file_path"`) {
		t.Fatalf("arguments missing file_path: %q", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsDeepSeekDSML(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"查一下\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"web_search\">\n<｜DSML｜parameter name=\"query\" string=\"true\">杭州天气</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.ToolCalls[0].Function.Name; got != "web_search" {
		t.Fatalf("tool name = %q", got)
	}
}

func TestParseNonStreamOpenAIResponseBody_ConvertsContentCodexToolCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"<turn: tool_call><invoke name=\"bash\"><parameter name=\"command\" string=\"true\">echo hi</parameter></invoke></turn>"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	if got := resp.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestParseNonStreamOpenAIResponseBody_MalformedContentToolCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"<turn: tool_call><invoke name=\"bash\">"},"finish_reason":"stop"}]}`)
	resp, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		t.Fatalf("ParseNonStreamOpenAIResponseBody: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != MalformedContentToolCallErrorMsg {
		t.Fatalf("content = %q, want malformed message", got)
	}
}

func TestParseSSEStream_ConvertsDeepSeekDSML(t *testing.T) {
	body := strings.NewReader(
		`data: {"choices":[{"delta":{"content":"查一下\n"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{"content":"<｜DSML｜tool_calls><｜DSML｜invoke name=\"web_search\"><｜DSML｜parameter name=\"query\" string=\"true\">杭州天气</｜DSML｜parameter><｜DSML｜parameter name=\"count\" string=\"false\">5</｜DSML｜parameter></｜DSML｜invoke></｜DSML｜tool_calls>"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n",
	)
	var streamed strings.Builder
	resp, err := parseSSEStream(body, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if got := streamed.String(); got != "查一下\n" {
		t.Fatalf("streamed DSML markup: %q", got)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
	call := resp.Choices[0].Message.ToolCalls[0]
	if call.Function.Name != "web_search" || call.Function.Arguments != `{"query":"杭州天气"}` {
		t.Fatalf("converted call = %#v", call)
	}
}

func TestParseSSEToResponse_ConvertsContentCodexToolCall(t *testing.T) {
	body := strings.NewReader(
		`data: {"choices":[{"delta":{"content":"<turn: tool_call><invoke name=\"bash\">"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{"content":"<parameter name=\"command\" string=\"true\">echo hi</parameter></invoke></turn>"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n",
	)
	var streamed strings.Builder
	resp, err := parseSSEStream(body, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if streamed.String() != "" {
		t.Fatalf("streamed raw tool XML: %q", streamed.String())
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
}

func TestParsePublicSSEToResponse_ConvertsContentCodexToolCall(t *testing.T) {
	body := []byte(
		`data: {"choices":[{"delta":{"content":"<turn: tool_call><invoke name=\"bash\"><parameter name=\"command\" string=\"true\">echo hi</parameter></invoke></turn>"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n",
	)
	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one converted tool call, got %#v", resp)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", got)
	}
}

func TestParseSSEToResponse_DoesNotDropNormalAnglePrefix(t *testing.T) {
	body := strings.NewReader(
		`data: {"choices":[{"delta":{"content":"<t"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{"content":"able>ok"},"finish_reason":null}]}` + "\n\n" +
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n",
	)
	var streamed strings.Builder
	resp, err := parseSSEStream(body, func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if got := streamed.String(); got != "<table>ok" {
		t.Fatalf("streamed = %q, want normal text", got)
	}
	if got := resp.Choices[0].Message.Content; got != "<table>ok" {
		t.Fatalf("content = %q, want normal text", got)
	}
}
