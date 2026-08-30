package tool

import (
	"fmt"
	"testing"
)

// Local wording is not an execution-routing authority. Sensitive conditional
// tools (ssh, browser, screenshot, IM delivery, craft_tool) start filtered and
// are promoted only by a semantic classifier for the current user message, or
// after actual successful use via ActivateSessionTool. Benign conditional
// tools (web_search, generate_pdf, office) are score-eligible and may be
// routed by retrieval score alone.
//
// gui_observe/gui_verify are not conditional tools at all: they are deferred
// tools, gated in production by the deferred-activation filter upstream of the
// router (filterInactiveDeferredTools), so they are not asserted here.
//
// These cases were previously asserted against matchConditionalKeepRules and
// MatchConditionalTools, two stubs that returned an empty keep set and were
// never consulted by Route. Asserting the property there could not fail, so the
// wording now goes through the live router instead.
func TestRouterLocalWordingNeverActivatesConditionalTools(t *testing.T) {
	for _, tc := range []struct {
		name     string
		message  string
		rejected []string
	}{
		{
			name:     "document delivery wording",
			message:  "send this report to me",
			rejected: []string{"craft_tool", "send_file", "send_to_im", "im_message", "open"},
		},
		{
			name:     "ssh wording",
			message:  "login to the server",
			rejected: []string{"ssh"},
		},
		{
			name:     "screenshot wording",
			message:  "take a screenshot and send it to me",
			rejected: []string{"craft_tool", "send_file", "send_to_im", "im_message", "open"},
		},
		// Recalled memory can surface context, but pinning execution tools from
		// lexical mentions in past summaries is too error-prone to allow.
		{
			name:     "recalled browser memory",
			message:  "Previous server check mentioned a Chrome browser process using CPU.",
			rejected: []string{"browser", "ssh"},
		},
		{
			name:     "recalled ssh memory",
			message:  "Previous task connected to api.rapidai.tech and checked Docker containers.",
			rejected: []string{"ssh", "browser"},
		},
		{
			name:     "browser-like memory",
			message:  "User wanted to build a web game whose page opens directly.",
			rejected: []string{"browser"},
		},
		{
			name:     "desktop gui memory",
			message:  "Previous task observed a desktop window and typed text.",
			rejected: []string{"browser"},
		},
		{
			name:     "mixed automation memory",
			message:  "browser automation, gui recording, and remote server notes",
			rejected: []string{"browser", "ssh"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routed := routeLocalWording(tc.message)
			for _, name := range tc.rejected {
				if routed[name] {
					t.Errorf("local wording %q activated conditional tool %q", tc.message, name)
				}
			}
		})
	}
}

// routeLocalWording routes one message through a router with no classifier and
// returns the resulting tool names. The extra tools reproduce the budget regime
// the conditional filter runs under.
func routeLocalWording(message string) map[string]bool {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("ssh", "通过 SSH 连接服务器"),
		makeToolDef("web_search", "搜索网页"),
		makeToolDef("send_file", "发送文件"),
		makeToolDef("send_to_im", "发送消息"),
		makeToolDef("im_message", "IM 消息"),
		makeToolDef("open", "打开文件"),
		makeToolDef("craft_tool", "生成内容"),
		makeToolDef("browser", "浏览器自动化工具"),
		makeToolDef("screenshot", "截取屏幕"),
		makeToolDef("gui_observe", "观察桌面窗口"),
		makeToolDef("gui_verify", "校验桌面窗口"),
	)
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	routed := make(map[string]bool)
	for _, item := range router.Route(message, tools) {
		routed[ExtractToolName(item)] = true
	}
	return routed
}

func TestIsFailClosedConditionalTool(t *testing.T) {
	if !IsFailClosedConditionalTool("ssh") {
		t.Fatal("ssh must be fail-closed")
	}
	if !IsFailClosedConditionalTool("screenshot") {
		t.Fatal("screenshot must be fail-closed")
	}
	if IsFailClosedConditionalTool("web_search") {
		t.Fatal("score-eligible web_search must not be fail-closed")
	}
	if IsFailClosedConditionalTool("bash") {
		t.Fatal("core bash is not a conditional tool")
	}
}
