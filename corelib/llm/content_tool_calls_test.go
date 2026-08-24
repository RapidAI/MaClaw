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
