package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestOpenCodeFreeAgentRequest(t *testing.T) {
	free := corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/v1", Model: "deepseek-v4.1-flash-free"}
	if !openCodeFreeAgentRequest(free) {
		t.Fatal("free catalog model should use the OpenCode agent bridge")
	}
	alias := corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/v1", Model: "big-pickle"}
	if !openCodeFreeAgentRequest(alias) {
		t.Fatal("big-pickle is the free model glm-5.3-flash-free")
	}
	metered := corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4.1-flash"}
	if openCodeFreeAgentRequest(metered) {
		t.Fatal("metered model must stay on its own tools")
	}
	other := corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4.1-flash-free"}
	if openCodeFreeAgentRequest(other) {
		t.Fatal("non-OpenCode host must not use the bridge")
	}
	hub := corelib.MaclawLLMConfig{URL: "https://hub.mypapers.top/api/llm/v1", Model: "deepseek-v4.1-flash-free"}
	if !openCodeFreeAgentRequest(hub) {
		t.Fatal("hub free model should use the bridge")
	}
	proxy := corelib.MaclawLLMConfig{URL: "http://127.0.0.1:18086/v1", ProviderName: "OpenCode", Model: "big-pickle"}
	if !openCodeFreeAgentRequest(proxy) {
		t.Fatal("OpenCode provider pointed at CodexProxy should use the bridge")
	}
}

func TestRewriteOpenCodeAgentToolCallsOntoRenderedSurface(t *testing.T) {
	calls := []llm.ToolCall{
		{ID: "1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"python --version","description":"check"}`}},
		{ID: "2", Function: llm.ToolCallFunction{Name: "webfetch", Arguments: `{"url":"https://wttr.in/Beijing","format":"text"}`}},
		{ID: "3", Function: llm.ToolCallFunction{Name: "websearch", Arguments: `{"query":"北京天气"}`}},
		{ID: "4", Function: llm.ToolCallFunction{Name: "read", Arguments: `{"filePath":"D:\\oc-empty\\a.txt"}`}},
		{ID: "5", Function: llm.ToolCallFunction{Name: "write", Arguments: `{"filePath":"beijing-weather.pdf","content":"%PDF"}`}},
		{ID: "6", Function: llm.ToolCallFunction{Name: "execute", Arguments: `{"code":"1"}`}},
		{ID: "7", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hi"}`}},
	}
	got := rewriteOpenCodeAgentToolCalls(calls)
	want := []struct{ name, arg string }{
		{"bash", `"command":"python --version"`},
		{"web_fetch", `"url":"https://wttr.in/Beijing"`},
		{"web_search", `"query":"北京天气"`},
		{"read_file", `"path":"D:\\oc-empty\\a.txt"`},
		{"write_file", `"content":"%PDF"`},
		{"execute", `"code":"1"`},
		{"bash", `"command":"echo hi"`},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i, w := range want {
		if got[i].Function.Name != w.name || !strings.Contains(got[i].Function.Arguments, w.arg) {
			t.Fatalf("call %d = %s %s, want name %s containing %s", i, got[i].Function.Name, got[i].Function.Arguments, w.name, w.arg)
		}
	}
	if strings.Contains(got[3].Function.Arguments, "filePath") {
		t.Fatalf("read args still use filePath: %s", got[3].Function.Arguments)
	}
	if !strings.Contains(got[4].Function.Arguments, `"path":"beijing-weather.pdf"`) {
		t.Fatalf("write args = %s", got[4].Function.Arguments)
	}
}

func TestOpenCodeFreeHostToolIgnoresPlannerSurface(t *testing.T) {
	free := corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/v1", Model: "deepseek-v4.1-flash-free"}
	for _, name := range []string{"bash", "web_fetch", "web_search", "read_file", "write_file"} {
		if !OpenCodeFreeHostTool(free, name) {
			t.Fatalf("%s should run on a free OpenCode turn", name)
		}
	}
	if OpenCodeFreeHostTool(free, "execute") || OpenCodeFreeHostTool(free, "generate_pdf") {
		t.Fatal("unproven tools must stay on the planner surface")
	}
	metered := corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4.1-flash"}
	if OpenCodeFreeHostTool(metered, "bash") {
		t.Fatal("metered model must not bypass the planner")
	}
}

func TestRewriteOpenCodeShellWorkdirAndTimeout(t *testing.T) {
	name, args, ok := rewriteOpenCodeAgentToolCall("shell", `{"command":"python gen.py","workdir":"D:\\oc-empty","timeout":120000}`)
	if !ok || name != "bash" {
		t.Fatalf("name=%s ok=%v args=%s", name, ok, args)
	}
	if strings.Contains(args, `"workdir"`) || !strings.Contains(args, `"working_dir":"D:\\oc-empty"`) {
		t.Fatalf("workdir was not renamed: %s", args)
	}
	if !strings.Contains(args, `"timeout":120`) {
		t.Fatalf("timeout stayed in milliseconds: %s", args)
	}
}

func TestRewriteOpenCodeReadRange(t *testing.T) {
	name, args, ok := rewriteOpenCodeAgentToolCall("read", `{"path":"a.txt","offset":10,"limit":20}`)
	if !ok || name != "read_file" {
		t.Fatalf("name=%s ok=%v", name, ok)
	}
	if strings.Contains(args, `"offset"`) || strings.Contains(args, `"limit"`) {
		t.Fatalf("read range keys remain: %s", args)
	}
	if !strings.Contains(args, `"start_line":10`) || !strings.Contains(args, `"lines":20`) {
		t.Fatalf("read range = %s", args)
	}
}

func TestRewriteOpenCodeAgentToolCallLeavesUnmappedName(t *testing.T) {
	name, args, ok := rewriteOpenCodeAgentToolCall("execute", `{"code":"1"}`)
	if ok || name != "execute" || !strings.Contains(args, `"code"`) {
		t.Fatalf("execute was rewritten: ok=%v name=%s args=%s", ok, name, args)
	}
}
