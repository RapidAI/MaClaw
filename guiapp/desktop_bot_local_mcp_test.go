package guiapp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalRelayMCPHostStaysOffUntilTheServiceIsEnabled(t *testing.T) {
	prev := mcpHostEnabledForRelay
	mcpHostEnabledForRelay = func(*App) bool { return false }
	t.Cleanup(func() { mcpHostEnabledForRelay = prev })

	_, err := (&App{}).localRelayMCP(context.Background(), "maclaw", "list_tasks", "")
	if err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("off: %v", err)
	}
	// knowledge_search is read-only and rides the relay's local-access consent
	// envelope, so the MCP task-service switch must not gate it: the call may
	// still fail on an empty store, but never with the "turned off" refusal.
	_, err = (&App{}).localRelayMCP(context.Background(), "maclaw", "knowledge_search", `{"query":"x"}`)
	if err != nil && strings.Contains(err.Error(), "turned off") {
		t.Fatalf("knowledge read must not be gated by the MCP switch: %v", err)
	}
	// A configured stdio server is a different path and is not the task service.
	_, err = (&App{}).localRelayMCP(context.Background(), "files", "list", "")
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stdio: %v", err)
	}
}

type relayHostAPI struct {
	fakeMCPTasks
	seenIP string
}

func (s *relayHostAPI) mcpCreateTask(ctx context.Context, args mcpCreateArgs) (mcpTaskView, error) {
	if info, ok := mcpClientInfoFromContext(ctx); ok {
		s.seenIP = info.IP
	}
	return s.fakeMCPTasks.mcpCreateTask(ctx, args)
}

func TestMCPCallHostToolCreatesALocalProgrammingTask(t *testing.T) {
	api := &relayHostAPI{}
	ctx := context.WithValue(context.Background(), mcpClientInfoContextKey{}, mcpClientInfo{IP: "desktop-bot"})
	text, err := mcpCallHostTool(ctx, api, "create_task", `{
		"name":"登录",
		"prompt":"实现登录",
		"mode":"coding_dev",
		"working_dir":"D:\\work",
		"ssh_password":"s3cret-not-in-result",
		"wait": false
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if api.seenIP != "desktop-bot" {
		t.Fatalf("caller = %q", api.seenIP)
	}
	if api.created.Name != "登录" || api.created.Prompt != "实现登录" || api.created.Mode != "coding_dev" || api.created.WorkingDir != `D:\work` {
		t.Fatalf("args = %+v", api.created)
	}
	if api.created.SSHPassword != "s3cret-not-in-result" {
		t.Fatalf("password did not reach the task call")
	}
	if api.created.Wait {
		t.Fatal("wait defaulted on")
	}
	if !strings.Contains(text, "running") || strings.Contains(text, "s3cret-not-in-result") {
		t.Fatalf("result = %s", text)
	}
	if _, err := mcpCallHostTool(ctx, api, "browser_test", `{}`); err == nil || !strings.Contains(err.Error(), "browser_test") {
		t.Fatalf("browser: %v", err)
	}
	catalog, err := mcpCallHostTool(ctx, api, "", "")
	if err != nil || !strings.Contains(catalog, "create_task") || strings.Contains(catalog, "browser_test") {
		t.Fatalf("catalog %q err=%v", catalog, err)
	}
	if _, err := mcpCallHostTool(ctx, api, "get_task", `{}`); err == nil || !strings.Contains(err.Error(), "没有找到这个任务") {
		t.Fatalf("missing task: %v", err)
	}
}

func TestMCPCallHostToolSearchesLocalKnowledge(t *testing.T) {
	api := &relayHostAPI{}
	text, err := mcpCallHostTool(context.Background(), api, "knowledge_search", `{"query":"部署笔记","limit":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if api.knowledgeQuery != "部署笔记" || api.knowledgeLimit != 3 {
		t.Fatalf("knowledge call query=%q limit=%d", api.knowledgeQuery, api.knowledgeLimit)
	}
	if !strings.Contains(text, "本地知识") || !strings.Contains(text, "部署笔记") {
		t.Fatalf("result = %s", text)
	}
	catalog, err := mcpCallHostTool(context.Background(), api, "", "")
	if err != nil || !strings.Contains(catalog, "knowledge_search") {
		t.Fatalf("catalog %q err=%v", catalog, err)
	}
}

func TestLocalKnowledgeSearchBypassesTheMCPServiceSwitch(t *testing.T) {
	prev := mcpHostEnabledForRelay
	mcpHostEnabledForRelay = func(*App) bool { return false }
	t.Cleanup(func() { mcpHostEnabledForRelay = prev })
	// Task creation still requires the MCP service switch.
	if _, err := (&App{}).localRelayMCP(context.Background(), "maclaw", "create_task", "{}"); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("create_task should stay gated: %v", err)
	}
	// The read-only knowledge search does not: it is data access under the
	// local-access consent envelope, not the power to drive this computer.
	// An empty App has no knowledge store, so the call must fail for that
	// reason — anything except the MCP-off refusal proves the gate opened.
	_, err := (&App{}).localRelayMCP(context.Background(), "maclaw", "knowledge_search", `{"query":"x"}`)
	if err == nil {
		t.Fatal("empty App answered a knowledge search")
	}
	if strings.Contains(err.Error(), "turned off") {
		t.Fatalf("knowledge_search was gated by the MCP switch: %v", err)
	}
}

func TestMCPHostRelayRedactsAJSONPassword(t *testing.T) {
	secret := "p&ass<word!!"
	quoted, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	escaped := string(quoted[1 : len(quoted)-1])
	got := mcpHostRelayRedact(`{"text":"`+escaped+`"}`, `{"sshPassword":"`+secret+`"}`)
	if strings.Contains(got, "p&ass") || strings.Contains(got, `\u0026`) || strings.Contains(got, `\u003c`) {
		t.Fatal(got)
	}
}

func TestLocalRelayMCPKeepsOtherServersClosedWithoutFileAccess(t *testing.T) {
	prev := localRelayStdioAllowed
	localRelayStdioAllowed = func() bool { return false }
	t.Cleanup(func() { localRelayStdioAllowed = prev })
	_, err := (&App{imHandler: &IMMessageHandler{}}).localRelayMCP(context.Background(), "files", "list", "")
	if err == nil || !strings.Contains(err.Error(), "maclaw") {
		t.Fatalf("stdio: %v", err)
	}
}
