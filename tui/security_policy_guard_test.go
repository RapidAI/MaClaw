package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func TestEnforceClientSecurityPolicyBlocksHubDisabledOutbound(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, FileOutboundEnabled: false, ImageOutboundEnabled: false, NetworkLevel: "full"}

	if ok, reason := enforceClientSecurityPolicy(cfg, "send_file", map[string]interface{}{"path": "report.pdf"}); ok || !strings.Contains(reason, "file outbound") {
		t.Fatalf("send_file allowed=%v reason=%q, want file outbound rejection", ok, reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "send_to_im", map[string]interface{}{"path": "report.pdf"}); ok || !strings.Contains(reason, "file outbound") {
		t.Fatalf("send_to_im allowed=%v reason=%q, want file outbound rejection", ok, reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "screenshot", nil); ok || !strings.Contains(reason, "image outbound") {
		t.Fatalf("screenshot allowed=%v reason=%q, want image outbound rejection", ok, reason)
	}
}

func TestEnforceClientSecurityPolicyBlocksUnsandboxedBash(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "os", NetworkLevel: "full", FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := enforceClientSecurityPolicy(cfg, "bash", map[string]interface{}{"command": "echo ok"}); ok || !strings.Contains(reason, "sandbox") {
		t.Fatalf("bash allowed=%v reason=%q, want sandbox rejection", ok, reason)
	}
}

func TestEnforceClientSecurityPolicyNetworkLevels(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("web_fetch allowed=%v reason=%q, want network rejection", ok, reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "open", map[string]interface{}{"target": "https://example.com"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("open URL allowed=%v reason=%q, want network rejection", ok, reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "search_and_install_skill", map[string]interface{}{"query": "deploy"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("skill search allowed=%v reason=%q, want network rejection", ok, reason)
	}

	cfg.NetworkLevel = "intranet"
	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "http://10.0.0.8/status"}); !ok {
		t.Fatalf("intranet URL blocked reason=%q", reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "intranet") {
		t.Fatalf("public URL allowed=%v reason=%q, want intranet rejection", ok, reason)
	}
}

func TestEnforceClientSecurityPolicyNetworkAllowlist(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com", "*.corp.local"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "https://api.example.com/v1"}); !ok {
		t.Fatalf("allowlisted host blocked reason=%q", reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "https://svc.corp.local/status"}); !ok {
		t.Fatalf("allowlisted wildcard host blocked reason=%q", reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": "https://example.org"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("non-allowlisted URL allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
}

func TestEnforceClientSecurityPolicyNoCentralizedAllowsLocal(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: false, SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: false, ImageOutboundEnabled: false}

	if ok, reason := enforceClientSecurityPolicy(cfg, "send_file", map[string]interface{}{"path": "report.pdf"}); !ok {
		t.Fatalf("local mode should allow, reason=%q", reason)
	}
}

func TestScriptedCommandSecurityToolMapsNetworkCommands(t *testing.T) {
	name, args, ok := scriptedCommandSecurityTool("skillhub", []string{"search", "deploy"})
	if !ok || name != "search_and_install_skill" || args["source"] != nil {
		t.Fatalf("skillhub search mapped to %q %#v ok=%v, want deferred source", name, args, ok)
	}

	name, args, ok = scriptedCommandSecurityTool("skillhub", []string{"install-github", "https://github.com/acme/skill"})
	if !ok || name != "manage_skill" || args["source"] != "github" {
		t.Fatalf("skillhub install-github mapped to %q %#v ok=%v", name, args, ok)
	}

	name, args, ok = scriptedCommandSecurityTool("mcp", []string{"call-tool", "--url", "https://mcp.example/rpc"})
	if !ok || name != "web_fetch" || args["url"] != "https://mcp.example/rpc" {
		t.Fatalf("mcp call-tool mapped to %q %#v ok=%v", name, args, ok)
	}

	name, args, ok = scriptedCommandSecurityTool("mcp", []string{"add", "--name", "local", "--command", "node", "--args", "server.js,--port,3000"})
	if !ok || name != "bash" || args["command"] != "node server.js --port 3000" {
		t.Fatalf("mcp local add mapped to %q %#v ok=%v", name, args, ok)
	}

	name, args, ok = scriptedCommandSecurityTool("mcp", []string{"add", "--name", "remote", "--url=https://mcp.example/rpc"})
	if !ok || name != "web_fetch" || args["url"] != "https://mcp.example/rpc" {
		t.Fatalf("mcp remote add mapped to %q %#v ok=%v", name, args, ok)
	}

	if _, _, ok := scriptedCommandSecurityTool("skill", []string{"list"}); ok {
		t.Fatal("local skill list should not map to network guard")
	}
}

func TestEnforceScriptedCommandSecurityBlocksLocalMCPUnderSandboxPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := commands.NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		SandboxMode:            "os",
		NetworkLevel:           "full",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	args := []string{"add", "--name", "local", "--command", "node", "--args", "server.js"}
	if err := enforceScriptedCommandSecurity("mcp", args); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("local mcp add err=%v, want sandbox rejection", err)
	}
}

func TestEnforceScriptedCommandSecurityAppliesIndependentSkillSourcePolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := commands.NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: false,
		SkillSourcesAllowed:    []string{"skillhub"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := enforceScriptedCommandSecurity("skillhub", []string{"install-github", "https://github.com/acme/skill"}); err == nil || !strings.Contains(err.Error(), "skill source") {
		t.Fatalf("github scripted install err=%v, want independent skill source rejection", err)
	}
	if err := enforceScriptedCommandSecurity("skillhub", []string{"install", "demo"}); err != nil {
		t.Fatalf("skillhub scripted install blocked: %v", err)
	}
}

func TestSkillSearchPolicySourceAllowsGithubOnlySearch(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: false, SkillSourcesAllowed: []string{"github"}}

	if got := tuiSkillSearchPolicySource(cfg); got != "github" {
		t.Fatalf("tuiSkillSearchPolicySource() = %q, want github", got)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "search_and_install_skill", tuiSkillSearchPolicyArgs(cfg, "deploy")); !ok {
		t.Fatalf("github-only TUI search should be allowed with selected source, reason=%q", reason)
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "manage_skill", map[string]interface{}{"action": "search", "query": "deploy"}); !ok {
		t.Fatalf("manage_skill search should defer source filtering, reason=%q", reason)
	}
}

func TestSkillSearchPolicySourceWorksWithGithubAllowlist(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"github"}}

	if ok, reason := enforceClientSecurityPolicy(cfg, "search_and_install_skill", tuiSkillSearchPolicyArgs(cfg, "deploy")); !ok {
		t.Fatalf("github-only TUI search should infer github.com for allowlist, reason=%q", reason)
	}
}

func TestSkillSearchPolicyArgsCarriesSkillHubURLForAllowlist(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"hub.example.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"skillhub"}, RemoteHubCenterURL: "https://hub.example.com"}

	args := tuiSkillSearchPolicyArgs(cfg, "deploy")
	if args["hub_url"] != "https://hub.example.com" {
		t.Fatalf("tuiSkillSearchPolicyArgs hub_url = %#v", args["hub_url"])
	}
	if ok, reason := enforceClientSecurityPolicy(cfg, "search_and_install_skill", args); !ok {
		t.Fatalf("skillhub-only TUI search should pass configured hub URL, reason=%q", reason)
	}
}

func TestSkillSearchPolicyFiltersBlockedSourcesAndKeepsAllowedFallback(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"skillhub", "clawhub", "github"}}

	sources, err := tuiAllowedSkillSearchSourcesForPolicy(cfg, "deploy")
	if err != nil {
		t.Fatalf("tuiAllowedSkillSearchSourcesForPolicy() error = %v", err)
	}
	if len(sources) != 1 || sources[0] != "github" {
		t.Fatalf("allowed sources = %#v, want github only", sources)
	}
}

func TestDeveloperModeAllowsAnySkillSourceAndRecordsRiskOnly(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "developer",
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}
	if err := commands.NewFileConfigStore(dataDir).SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if !tuiSkillSourceAllowedByPolicy(cfg, "github") {
		t.Fatal("developer mode should allow github skill source")
	}
	if sources := tuiAllowedSkillSearchSources(cfg); sources != nil {
		t.Fatalf("developer mode search sources = %#v, want nil/all", sources)
	}
	if err := enforceScriptedCommandSecurity("skillhub", []string{"install-github", "https://github.com/acme/skill"}); err != nil {
		t.Fatalf("developer scripted github install blocked: %v", err)
	}
}
