package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMCPListEmptyGuidesToTUITemplates(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	out, err := captureMCPStdout(t, func() error {
		return mcpList(nil)
	})
	if err != nil {
		t.Fatalf("mcpList error = %v", err)
	}
	if !strings.Contains(out, "maclaw-tui mcp") || !strings.Contains(out, "模板") {
		t.Fatalf("empty MCP list should guide to TUI templates:\n%s", out)
	}
	if strings.Contains(out, "mcp add --name") {
		t.Fatalf("empty MCP list should not send new users to raw add flags:\n%s", out)
	}
}

func TestMCPListJSONIncludesNextTUICommand(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	out, err := captureMCPStdout(t, func() error {
		return mcpList([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("mcpList --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("parse json %q: %v", out, err)
	}
	if info["next_tui_command"] != "maclaw-tui mcp" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
	if next, ok := info["next_action"].(string); !ok || !strings.Contains(next, "maclaw-tui mcp") {
		t.Fatalf("next_action = %#v", info["next_action"])
	}
}

func TestMCPListLocalizesEnglishOutput(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{Language: "en"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureMCPStdout(t, func() error {
		return mcpList(nil)
	})
	if err != nil {
		t.Fatalf("mcpList error = %v", err)
	}
	for _, want := range []string{
		"No MCP servers configured.",
		"Next: Run maclaw-tui mcp",
		"TUI add: maclaw-tui mcp",
		"TUI templates",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("English MCP list missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "未配置") || strings.Contains(out, "下一步") {
		t.Fatalf("English MCP list should not mix Chinese labels:\n%s", out)
	}
}

func TestMCPAddUsageGuidesToTUITemplate(t *testing.T) {
	err := mcpAdd(nil)
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "maclaw-tui mcp") || strings.Contains(err.Error(), "maclaw-tui mcp add") || !strings.Contains(err.Error(), "模板") {
		t.Fatalf("mcp add usage should guide to TUI templates: %s", err)
	}

	err = mcpAdd([]string{"--name", "server"})
	if err == nil {
		t.Fatal("expected missing endpoint/command error")
	}
	if !strings.Contains(err.Error(), "maclaw-tui mcp") || strings.Contains(err.Error(), "maclaw-tui mcp add") || !strings.Contains(err.Error(), "模板") {
		t.Fatalf("mcp add missing endpoint should guide to TUI templates: %s", err)
	}
}

func TestMCPAddRemoteAuthRequiresSecret(t *testing.T) {
	err := mcpAdd([]string{"--name", "both", "--url", "https://mcp.example/mcp", "--command", "npx"})
	if err == nil {
		t.Fatal("expected mutually exclusive endpoint/command error")
	}
	if !strings.Contains(err.Error(), "二选一") || !strings.Contains(err.Error(), "maclaw-tui mcp local") {
		t.Fatalf("endpoint/command conflict should guide to TUI choice flows: %s", err)
	}

	err = mcpAdd([]string{"--name", "remote", "--url", "https://mcp.example/mcp", "--auth", "bearer"})
	if err == nil {
		t.Fatal("expected missing secret error")
	}
	if !strings.Contains(err.Error(), "--secret") || !strings.Contains(err.Error(), "maclaw-tui mcp remote") {
		t.Fatalf("missing secret should guide to TUI remote auth setup: %s", err)
	}

	err = mcpAdd([]string{"--name", "remote", "--url", "https://mcp.example/mcp", "--auth", "unknown", "--secret", "token"})
	if err == nil {
		t.Fatal("expected unknown auth error")
	}
	if !strings.Contains(err.Error(), "none/api_key/bearer") || !strings.Contains(err.Error(), "maclaw-tui mcp remote") {
		t.Fatalf("unknown auth should show supported choices and TUI guidance: %s", err)
	}
}

func TestMCPAddHonorsHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	localErr := mcpAdd([]string{"--name", "local", "--command", "node", "--args", "server.js"})
	if localErr == nil || !strings.Contains(localErr.Error(), "sandbox") {
		t.Fatalf("local add err=%v, want sandbox rejection", localErr)
	}
	remoteErr := mcpAdd([]string{"--name", "remote", "--url", "https://mcp.example/rpc"})
	if remoteErr == nil || !strings.Contains(remoteErr.Error(), "network") {
		t.Fatalf("remote add err=%v, want network rejection", remoteErr)
	}

	loaded, err := NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.LocalMCPServers) != 0 || len(loaded.MCPServers) != 0 {
		t.Fatalf("blocked MCP persisted: local=%d remote=%d", len(loaded.LocalMCPServers), len(loaded.MCPServers))
	}
}

func TestMCPAddTrimsCommaArgs(t *testing.T) {
	got := splitMCPArgs(" server.js, --port, 3000 ,, ")
	want := []string{"server.js", "--port", "3000"}
	if len(got) != len(want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
}

func TestMCPOperationsHonorHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		SandboxMode:            "os",
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		MCPServers:             []corelib.MCPServerEntry{{Name: "remote", EndpointURL: "https://mcp.example/rpc"}},
		LocalMCPServers:        []corelib.LocalMCPServerEntry{{Name: "local", Command: "node", Args: []string{"server.js"}}},
	}
	if err := NewFileConfigStore(dataDir).SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	callErr := mcpCallTool([]string{"--server", "remote", "--tool", "fetch", "--args", `{"url":"https://example.com"}`})
	if callErr == nil || !strings.Contains(callErr.Error(), "network") {
		t.Fatalf("call-tool err=%v, want network rejection", callErr)
	}

	toolsOut, toolsErr := captureMCPStdout(t, func() error { return mcpTools(nil) })
	if toolsErr != nil {
		t.Fatalf("mcpTools error = %v", toolsErr)
	}
	if !strings.Contains(toolsOut, "blocked") || !strings.Contains(toolsOut, "network") || !strings.Contains(toolsOut, "sandbox") {
		t.Fatalf("mcpTools output should report blocked remote/local tools:\n%s", toolsOut)
	}

	healthOut, healthErr := captureMCPStdout(t, func() error { return mcpHealthCheck([]string{"--json"}) })
	if healthErr != nil {
		t.Fatalf("mcpHealthCheck error = %v", healthErr)
	}
	if !strings.Contains(healthOut, "blocked") || !strings.Contains(healthOut, "network") {
		t.Fatalf("mcpHealthCheck output should report blocked remote health check:\n%s", healthOut)
	}
}

func captureMCPStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	outBytes, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(outBytes), runErr
}
