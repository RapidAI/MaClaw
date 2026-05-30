package clientsecurity

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEnforceConfigBlocksNestedURLWhenNetworkDisabled(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	args := map[string]interface{}{
		"tool_name": "fetch",
		"arguments": map[string]interface{}{
			"url":     "https://example.com/data",
			"headers": map[string]interface{}{"x-note": "no url here"},
		},
	}

	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", args); ok || !strings.Contains(reason, "network") {
		t.Fatalf("nested URL allowed=%v reason=%q, want network rejection", ok, reason)
	}
}

func TestEnforceConfigAllowlistMatchesNestedURL(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com", "*.corp.local"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	allowedArgs := map[string]interface{}{"arguments": map[string]interface{}{"url": "https://api.example.com/v1"}}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", allowedArgs); !ok {
		t.Fatalf("allowlisted nested URL blocked reason=%q", reason)
	}

	blockedArgs := map[string]interface{}{"arguments": []interface{}{map[string]interface{}{"url": "https://example.org"}}}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", blockedArgs); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("non-allowlisted nested URL allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
}

func TestEnforceConfigNetworkPolicyChecksSSHHost(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if ok, reason := EnforceConfig(cfg, "ssh", map[string]interface{}{"action": "connect", "host": "10.0.0.8"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("ssh allowed=%v reason=%q, want network rejection", ok, reason)
	}

	cfg.NetworkLevel = "intranet"
	if ok, reason := EnforceConfig(cfg, "ssh", map[string]interface{}{"action": "connect", "host": "10.0.0.8"}); !ok {
		t.Fatalf("intranet ssh host blocked reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "ssh", map[string]interface{}{"action": "connect", "host": "example.com"}); ok || !strings.Contains(reason, "intranet") {
		t.Fatalf("public ssh host allowed=%v reason=%q, want intranet rejection", ok, reason)
	}

	cfg.NetworkLevel = "allowlist"
	cfg.NetworkAllowlist = []string{"bastion.example.com", "*.corp.local"}
	if ok, reason := EnforceConfig(cfg, "ssh", map[string]interface{}{"action": "connect", "host": "ops.corp.local:22"}); !ok {
		t.Fatalf("allowlisted ssh host blocked reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "ssh", map[string]interface{}{"action": "connect", "host": "other.example.com"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("non-allowlisted ssh host allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
}

func TestEnforceConfigNetworkPolicyChecksNestedHosts(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	args := map[string]interface{}{
		"tool_name": "connect",
		"arguments": map[string]interface{}{"host": "db.example.com", "port": 5432},
	}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", args); ok || !strings.Contains(reason, "network") {
		t.Fatalf("nested host allowed=%v reason=%q, want network rejection", ok, reason)
	}

	cfg.NetworkLevel = "allowlist"
	cfg.NetworkAllowlist = []string{"db.example.com"}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", args); !ok {
		t.Fatalf("allowlisted nested host blocked reason=%q", reason)
	}

	args["arguments"] = map[string]interface{}{"host": "other.example.com"}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", args); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("non-allowlisted nested host allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
}

func TestEnforceConfigNetworkPolicyChecksBashNetworkCommands(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", SandboxMode: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	for _, command := range []string{
		"curl\thttps://example.com",
		"Invoke-WebRequest https://example.com",
		"git clone https://github.com/acme/skill",
		"ping example.com",
		"npm install left-pad",
	} {
		if ok, reason := EnforceConfig(cfg, "bash", map[string]interface{}{"command": command}); ok || !strings.Contains(reason, "network") {
			t.Fatalf("bash command %q allowed=%v reason=%q, want network rejection", command, ok, reason)
		}
	}
	if ok, reason := EnforceConfig(cfg, "bash", map[string]interface{}{"command": "echo ok"}); !ok {
		t.Fatalf("local bash command blocked reason=%q", reason)
	}
}

func TestEnforceConfigNoURLNonNetworkToolAllowed(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := EnforceConfig(cfg, "read_file", map[string]interface{}{"path": "README.md", "note": "https-ish but not a URL"}); !ok {
		t.Fatalf("non-network tool without URL blocked reason=%q", reason)
	}
}

func TestEnforceConfigManageSkillActionAware(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "list"}); !ok {
		t.Fatalf("manage_skill list blocked reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "skill_id": "demo"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("manage_skill install allowed=%v reason=%q, want network rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "zip", "zip_base64": "e30="}); !ok {
		t.Fatalf("local zip install should not need network, reason=%q", reason)
	}
}

func TestEnforceConfigSkillSourcePolicy(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "full", FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"skillhub"}}

	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("github install allowed=%v reason=%q, want source rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); ok || !strings.Contains(reason, "当前企业策略不允许") || !strings.Contains(reason, "Your organization policy does not allow") {
		t.Fatalf("github install localized denial allowed=%v reason=%q", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": "demo"}); !ok {
		t.Fatalf("skillhub install blocked reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "skillmarket", "skill_id": "demo"}); !ok {
		t.Fatalf("skillmarket alias should follow skillhub policy, reason=%q", reason)
	}
	cfg.SkillSourcesAllowed = []string{"hubcenter", "git_hub"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": "demo"}); !ok {
		t.Fatalf("hubcenter alias should allow skillhub, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "skill_id": "demo"}); !ok {
		t.Fatalf("git_hub alias should allow github, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "zip", "zip_base64": "e30="}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("zip install allowed=%v reason=%q, want source rejection without local", ok, reason)
	}
	cfg.SkillSourcesAllowed = []string{"local"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "zip", "zip_base64": "e30="}); !ok {
		t.Fatalf("local policy should allow zip install source, reason=%q", reason)
	}
	cfg.SkillSourcesAllowed = []string{corelib.CapabilitySourceEnterpriseHub}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": corelib.CapabilitySourceHubCenter, "skill_id": "demo"}); ok || !strings.Contains(reason, "当前企业策略不允许") || !strings.Contains(reason, "Your organization policy does not allow") {
		t.Fatalf("hubcenter install with enterprise-only policy allowed=%v reason=%q, want localized source rejection", ok, reason)
	}
	cfg.SkillSourcesAllowed = []string{"__none__"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "search", "query": "demo"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("block-all skill policy search allowed=%v reason=%q, want source rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "zip", "zip_base64": "e30="}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("block-all skill policy install allowed=%v reason=%q, want source rejection", ok, reason)
	}
}

func TestEnforceConfigSkillSearchDefersUnknownSource(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: false, SkillSourcesAllowed: []string{"github"}}

	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "search", "query": "deploy"}); !ok {
		t.Fatalf("source-filtered manage_skill search should defer source decision, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"query": "deploy"}); !ok {
		t.Fatalf("source-filtered search_and_install_skill should defer source decision, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"query": "deploy", "source": "skillhub"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("explicit disallowed search source allowed=%v reason=%q, want rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "skill_id": "demo"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("unknown install source allowed=%v reason=%q, want default skillhub rejection", ok, reason)
	}
}

func TestEnforceConfigSkillInstallInfersSourceFromInstallFields(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: false, SkillSourcesAllowed: []string{"skillhub"}}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "repo_url": "https://github.com/acme/weather"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("repo_url install allowed=%v reason=%q, want github source rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "raw_url": "https://raw.githubusercontent.com/acme/weather/main/SKILL.md"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("raw_url install allowed=%v reason=%q, want github source rejection", ok, reason)
	}
	cfg.SkillSourcesAllowed = []string{"github"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "repo_full_name": "acme/weather"}); !ok {
		t.Fatalf("repo_full_name install should infer github, reason=%q", reason)
	}
	cfg.SkillSourcesAllowed = []string{"skillhub"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "zip_base64": "e30="}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("zip_base64 install allowed=%v reason=%q, want local source rejection", ok, reason)
	}
	cfg.SkillSourcesAllowed = []string{"local"}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "zip_base64": "e30="}); !ok {
		t.Fatalf("zip_base64 install should infer local, reason=%q", reason)
	}
}

func TestEnforceConfigSkillSearchInfersKnownSourceEndpointForAllowlist(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com", "cn.clawhub-mirror.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"github", "clawhub"}}

	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"query": "deploy", "source": "github"}); !ok {
		t.Fatalf("github skill search should infer github.com for allowlist, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "search", "query": "deploy", "source": "clawhub"}); !ok {
		t.Fatalf("clawhub skill search should infer mirror host for allowlist, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"query": "deploy", "source": "skillhub"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("skillhub source should still be rejected by source policy, allowed=%v reason=%q", ok, reason)
	}
}

func TestEnforceConfigSkillInstallRefChecksEmbeddedURLs(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"github"}}
	args := map[string]interface{}{
		"action":      "install",
		"source":      "github",
		"install_ref": `{"repo_url":"https://github.com/acme/weather","raw_url":"https://raw.githubusercontent.com/acme/weather/main/SKILL.md"}`,
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", args); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("embedded raw URL should be checked, allowed=%v reason=%q", ok, reason)
	}
	cfg.NetworkAllowlist = append(cfg.NetworkAllowlist, "raw.githubusercontent.com")
	if ok, reason := EnforceConfig(cfg, "manage_skill", args); !ok {
		t.Fatalf("embedded GitHub URLs should be allowed, reason=%q", reason)
	}
}

func TestEnforceConfigChecksEmbeddedURLsInStringSlices(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}
	args := map[string]interface{}{"urls": []string{`{"raw_url":"https://raw.githubusercontent.com/acme/weather/main/SKILL.md"}`}}
	if ok, reason := EnforceConfig(cfg, "web_fetch", args); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("embedded string-slice URL should be checked, allowed=%v reason=%q", ok, reason)
	}
	cfg.NetworkAllowlist = append(cfg.NetworkAllowlist, "raw.githubusercontent.com")
	if ok, reason := EnforceConfig(cfg, "web_fetch", args); !ok {
		t.Fatalf("embedded string-slice URL should be allowed, reason=%q", reason)
	}
}

func TestEnforceConfigSkillSourcePolicyAppliesWithoutCentralizedSecurity(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: false, SkillSourcesAllowed: []string{"skillhub"}}

	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("github install allowed=%v reason=%q, want independent source rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": "demo"}); !ok {
		t.Fatalf("skillhub install blocked reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); !ok {
		t.Fatalf("non-skill policy should not apply without centralized security, reason=%q", reason)
	}
}

func TestEnforceConfigDeveloperModeAllowsAllSkillInstalls(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "developer",
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}

	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); !ok {
		t.Fatalf("developer mode should allow github skill install, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"source": "clawhub", "query": "demo"}); !ok {
		t.Fatalf("developer mode should allow any skill search/install source, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("developer skill bypass should not disable generic network policy, allowed=%v reason=%q", ok, reason)
	}
}

func TestHubManagedSecurityConfigCannotBeOverwrittenLocally(t *testing.T) {
	current := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "strict",
		SandboxMode:            "os",
		NetworkLevel:           "allowlist",
		NetworkAllowlist:       []string{"api.example.com"},
		YoloModeAllowed:        false,
		SmartRouteEnabled:      false,
		GossipEnabled:          false,
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   false,
		SkillSourcesAllowed:    []string{"skillhub"},
	}
	if blocked, reason := RejectHubManagedSecurityConfigChange(current, "network_level"); !blocked || reason == "" {
		t.Fatalf("network_level blocked=%v reason=%q, want Hub-managed rejection", blocked, reason)
	}
	if blocked, reason := RejectHubManagedSecurityConfigChange(current, "language"); blocked || reason != "" {
		t.Fatalf("language blocked=%v reason=%q, want allowed", blocked, reason)
	}

	next := corelib.AppConfig{
		HubSecurityCentralized: false,
		SecurityPolicyMode:     "developer",
		SandboxMode:            "none",
		NetworkLevel:           "full",
		NetworkAllowlist:       []string{"evil.example"},
		YoloModeAllowed:        true,
		SmartRouteEnabled:      true,
		GossipEnabled:          true,
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"github"},
		Language:               "en",
	}
	PreserveHubManagedSecurityConfig(current, &next)
	if !next.HubSecurityCentralized || next.SecurityPolicyMode != "strict" || next.SandboxMode != "os" || next.NetworkLevel != "allowlist" {
		t.Fatalf("managed scalar fields not preserved: %#v", next)
	}
	if len(next.NetworkAllowlist) != 1 || next.NetworkAllowlist[0] != "api.example.com" || len(next.SkillSourcesAllowed) != 1 || next.SkillSourcesAllowed[0] != "skillhub" {
		t.Fatalf("managed slices not preserved: allow=%v sources=%v", next.NetworkAllowlist, next.SkillSourcesAllowed)
	}
	if next.YoloModeAllowed || next.SmartRouteEnabled || next.GossipEnabled || next.FileOutboundEnabled || next.ImageOutboundEnabled {
		t.Fatalf("managed bool fields not preserved: %#v", next)
	}
	if next.Language != "en" {
		t.Fatalf("unmanaged language should remain changed, got %q", next.Language)
	}
}
