package agentservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestBotModeKeepsContainerToolsAndDropsHostSystem(t *testing.T) {
	bot := &coreAgentCallbacks{
		runtimeRequest: ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}},
	}
	kept := map[string]bool{}
	for _, tool := range bot.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("read_file", "read the host", nil),
		functionToolDefinition("write_file", "write the host", nil),
		functionToolDefinition("bash", "host shell", nil),
		functionToolDefinition("craft_tool", "host script", nil),
		functionToolDefinition("screenshot", "host screen", nil),
		functionToolDefinition("browser", "host browser", nil),
		functionToolDefinition("desktop", "cloud desktop", nil),
		functionToolDefinition("web_search", "host search", nil),
		functionToolDefinition("ask_user", "ask", nil),
	}) {
		kept[tooldef.Name(tool)] = true
	}
	for _, name := range []string{"read_file", "write_file", "bash", "craft_tool", "screenshot", "browser"} {
		if kept[name] {
			t.Fatalf("bot kept host tool %s: %#v", name, kept)
		}
	}
	if !kept["desktop"] || !kept["web_search"] || !kept["web_fetch"] || !kept["ask_user"] {
		t.Fatalf("bot lost a container tool: %#v", kept)
	}
	employee := &coreAgentCallbacks{}
	if employee.botUsesContainer() || employee.botMissesContainer("read_file") || employee.botMissesContainer("bash") {
		t.Fatal("digital employee was moved onto the container")
	}
	same := employee.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("read_file", "read the host", nil),
		functionToolDefinition("bash", "host shell", nil),
		functionToolDefinition("web_search", "host search", nil),
	})
	if len(same) != 3 {
		t.Fatalf("digital employee surface changed: %#v", same)
	}
	phased := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "execute"}}
	if !phased.botUsesContainer() || !phased.botMissesContainer("craft_tool") {
		t.Fatal("execute phase did not stay inside the container")
	}
}

func TestBotWebSearchUsesTheDesktopNetwork(t *testing.T) {
	previous := DesktopWeb
	t.Cleanup(func() { DesktopWeb = previous })
	var gotKind, gotTenant, gotUser, gotQuery, gotURL string
	var gotChars int
	DesktopWeb = func(_ context.Context, tenantID, userID, kind, query, rawURL string, maxChars int) (string, error) {
		gotKind, gotTenant, gotUser, gotQuery, gotURL, gotChars = kind, tenantID, userID, query, rawURL, maxChars
		return "from desktop", nil
	}
	bot := &coreAgentCallbacks{
		principal: Principal{TenantID: "maclaw-tenant", UserID: "service-account"},
		runtimeRequest: ExecuteRequest{Instance: Instance{Metadata: map[string]string{
			"hub_bot": "1", "hub_user_id": "alice", "hub_tenant_id": "tenant-a",
		}}},
	}
	if out := bot.executeWebSearch(map[string]interface{}{"query": "gcc"}); out != "from desktop" || gotKind != "search" || gotQuery != "gcc" || gotTenant != "tenant-a" || gotUser != "alice" {
		t.Fatalf("search out=%q kind=%s query=%s tenant=%s user=%s", out, gotKind, gotQuery, gotTenant, gotUser)
	}
	unbound := &coreAgentCallbacks{
		principal:      Principal{TenantID: "maclaw-tenant", UserID: "service-account"},
		runtimeRequest: ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}},
	}
	if out := unbound.executeWebSearch(map[string]interface{}{"query": "gcc"}); !strings.Contains(out, "not bound") || gotUser != "alice" {
		t.Fatalf("unbound search out=%q user=%s", out, gotUser)
	}
	if out := bot.executeWebFetch(map[string]interface{}{"url": "https://example.com", "max_chars": 32}); out != "from desktop" || gotKind != "fetch" || gotURL != "https://example.com" || gotChars != 32 {
		t.Fatalf("fetch out=%q kind=%s url=%s chars=%d", out, gotKind, gotURL, gotChars)
	}
	DesktopWeb = nil
	if out := bot.executeWebSearch(map[string]interface{}{"query": "gcc"}); !strings.Contains(out, "desktop network") {
		t.Fatalf("bot fell through to the host: %s", out)
	}
	employee := &coreAgentCallbacks{principal: Principal{TenantID: "tenant-a", UserID: "alice"}}
	if employee.botUsesContainer() {
		t.Fatal("digital employee entered the container")
	}
}

func TestBotKeepsTheRemoteSSHTool(t *testing.T) {
	bare := &coreAgentCallbacks{
		runtimeRequest: ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}},
	}
	bareKept := map[string]bool{}
	for _, tool := range bare.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("ssh", "semantic command only", nil),
		functionToolDefinition("bash", "host shell", nil),
		functionToolDefinition("desktop", "cloud desktop", nil),
	}) {
		bareKept[tooldef.Name(tool)] = true
	}
	if bareKept["ssh"] || bareKept["bash"] || !bareKept["desktop"] {
		t.Fatalf("ssh appeared without a configured host: %#v", bareKept)
	}

	bot := &coreAgentCallbacks{
		allowDirectSSH:         true,
		runtimeRequest:         ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}},
		dynamicSemanticManaged: true,
		dynamicSemanticSurface: &coreDynamicSemanticSurface{
			grants: map[string]coretool.InvocationGrant{
				"ssh": {AdapterName: "host_shell_execute_remote_host", SelectionID: "sel-ssh"},
			},
		},
	}
	entry, ok := agent.LookupCoreTool("ssh")
	if !ok {
		t.Fatal("ssh tool missing")
	}
	actionBefore := fmt.Sprintf("%p", entry.Properties["action"])
	kept := map[string]string{}
	for _, tool := range bot.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("read_file", "read the host", nil),
		functionToolDefinition("bash", "host shell", nil),
		functionToolDefinition("host_shell_execute_local", "local shell", nil),
		functionToolDefinition("host_shell_execute_remote_host", "bound session", nil),
		functionToolDefinition("ssh", "semantic command only", nil),
		functionToolDefinition("desktop", "cloud desktop", nil),
	}) {
		fn, _ := tool["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		kept[tooldef.Name(tool)] = desc
	}
	for _, name := range []string{"read_file", "bash", "host_shell_execute_local", "host_shell_execute_remote_host"} {
		if _, ok := kept[name]; ok {
			t.Fatalf("bot kept host tool %s", name)
		}
	}
	if !strings.Contains(kept["ssh"], "Manage remote SSH") || !strings.Contains(kept["desktop"], "cloud desktop") {
		t.Fatalf("bot ssh surface: %#v", kept)
	}
	if fmt.Sprintf("%p", entry.Properties["action"]) != actionBefore {
		t.Fatal("ssh action schema was replaced")
	}
	if !bot.IsToolAllowedForPromptProfile("ssh", agent.PromptProfileLight) {
		t.Fatal("a short bot turn hid ssh")
	}
	employee := &coreAgentCallbacks{allowDirectSSH: true}
	if employee.IsToolAllowedForPromptProfile("ssh", agent.PromptProfileLight) {
		t.Fatal("a light digital-employee turn gained ssh")
	}
	same := employee.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("ssh", "semantic command only", nil),
		functionToolDefinition("bash", "host shell", nil),
	})
	if len(same) != 2 {
		t.Fatalf("digital employee surface changed: %#v", same)
	}
	fn, _ := same[0]["function"].(map[string]interface{})
	if fn["description"] != "semantic command only" {
		t.Fatalf("digital employee ssh definition changed: %#v", fn)
	}

	bot.messageMetadata = map[string]string{"bot_phase": "plan"}
	blocked := bot.ExecuteToolStructured("ssh", `{"action":"exec","command":"nvidia-smi"}`)
	if !strings.Contains(blocked.Result, "plan phase blocks ssh exec") {
		t.Fatalf("plan exec: %+v", blocked)
	}
	listed := bot.ExecuteToolCall("ssh", `{"action":"list"}`, "call-ssh")
	if listed.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(listed.Result, "无活跃") || strings.Contains(listed.Result, "host_call") || strings.Contains(listed.Result, "plan phase blocks") {
		t.Fatalf("plan list: %+v", listed)
	}
}

func TestBotSSHWithoutAHostReachesTheSSHDenial(t *testing.T) {
	bot := &coreAgentCallbacks{
		runtimeRequest:         ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}},
		dynamicSemanticManaged: true,
		dynamicSemanticSurface: &coreDynamicSemanticSurface{
			grants: map[string]coretool.InvocationGrant{
				"ssh": {AdapterName: "host_shell_execute_remote_host", SelectionID: "sel-ssh"},
			},
		},
	}
	kept := map[string]bool{}
	for _, tool := range bot.finishToolSurface([]map[string]interface{}{
		functionToolDefinition("ssh", "semantic command only", nil),
		functionToolDefinition("host_shell_execute_remote_host", "bound session", nil),
		functionToolDefinition("desktop", "cloud desktop", nil),
	}) {
		kept[tooldef.Name(tool)] = true
	}
	if kept["ssh"] || kept["host_shell_execute_remote_host"] || !kept["desktop"] {
		t.Fatalf("unconfigured surface: %#v", kept)
	}
	out := bot.ExecuteToolCall("ssh", `{"action":"list"}`, "call-1")
	if !strings.Contains(out.Result, "no direct SSH") || strings.Contains(out.Result, "host_call") || strings.Contains(out.Result, "desktop container") {
		t.Fatalf("denial: %+v", out)
	}
}
