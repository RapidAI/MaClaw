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
