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

// Developer mode exempts a skill install from the skill *source* allowlist.
// The network level is a separate boundary and still applies — see
// TestEnforceConfigDeveloperModeDoesNotOverrideNetworkLevel for that half.
func TestEnforceConfigDeveloperModeAllowsAllSkillInstalls(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "developer",
		NetworkLevel:           "full",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}

	// github is not in SkillSourcesAllowed; developer mode must not care.
	if ok, reason := EnforceConfig(cfg, "manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); !ok {
		t.Fatalf("developer mode should allow github skill install, reason=%q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "search_and_install_skill", map[string]interface{}{"source": "clawhub", "query": "demo"}); !ok {
		t.Fatalf("developer mode should allow any skill search/install source, reason=%q", reason)
	}
	// Without developer mode the same source is refused, proving the exemption
	// above is doing the work rather than the allowlist being permissive.
	strict := cfg
	strict.SecurityPolicyMode = "standard"
	if ok, _ := EnforceConfig(strict, "search_and_install_skill", map[string]interface{}{"source": "clawhub", "query": "demo"}); ok {
		t.Fatal("non-developer mode should enforce the skill source allowlist")
	}
	// A non-skill network tool is governed by the network level alone. The
	// level here is "full", so it passes; the point of the assertion is that
	// the developer exemption never becomes a general network bypass.
	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); !ok {
		t.Fatalf("web_fetch should follow the network level (full) even in developer mode, reason=%q", reason)
	}
	restricted := cfg
	restricted.NetworkLevel = "none"
	if ok, reason := EnforceConfig(restricted, "web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("developer mode must not weaken the network level for non-skill tools, allowed=%v reason=%q", ok, reason)
	}
}

// Developer mode used to `return true` at the top of EnforceConfig for a skill
// install, which skipped the network level along with the source allowlist. The
// result was that the one tool guaranteed to reach the network was the one tool
// network_level could not stop: under "none" — the org-wide shutdown —
// web_fetch was refused while search_and_install_skill still reached clawhub.
//
// The exemption answers "may this skill come from an unlisted source", never
// "may this process open a socket", so the network level must survive it. This
// is reachable from the Hub as well as locally: SecurityPolicyMode is the
// Hub-managed guardrail_mode, and "developer" is one of its accepted values.
func TestEnforceConfigDeveloperModeDoesNotOverrideNetworkLevel(t *testing.T) {
	base := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "developer",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}

	// network_level "none" is the organization-wide shutdown.
	none := base
	none.NetworkLevel = "none"
	for _, tc := range []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"search_and_install_skill", "search_and_install_skill", map[string]interface{}{"source": "clawhub", "query": "demo"}},
		{"manage_skill install", "manage_skill", map[string]interface{}{"action": "install", "source": "github", "repo_url": "https://github.com/acme/skill"}},
	} {
		if ok, reason := EnforceConfig(none, tc.tool, tc.args); ok {
			t.Errorf("developer mode %s allowed under network_level=none, want denied (reason=%q)", tc.name, reason)
		}
	}

	// A domain scope the skill source is not listed in must also stop the
	// install: clawhub is absent from the single allowlist entry below.
	allowlist := base
	allowlist.NetworkLevel = "allowlist"
	allowlist.NetworkAllowlist = []string{"hub.example.com"}
	if ok, reason := EnforceConfig(allowlist, "search_and_install_skill", map[string]interface{}{"source": "clawhub", "query": "demo"}); ok {
		t.Errorf("developer mode search_and_install_skill allowed outside the allowlist, want denied (reason=%q)", reason)
	}

	// And a local (zip) install needs no network, so "none" must not block it.
	local := map[string]interface{}{"action": "install", "source": "local", "zip_base64": "UEsDBA=="}
	if ok, reason := EnforceConfig(none, "manage_skill", local); !ok {
		t.Errorf("developer mode local skill install denied under network_level=none, want allowed (reason=%q)", reason)
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

func TestNameLevelRejection(t *testing.T) {
	centralized := corelib.AppConfig{
		HubSecurityCentralized: true,
		SandboxMode:            "os",
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   false,
	}
	for _, name := range []string{"bash", "send_file", "send_to_im", "screenshot"} {
		if ok, reason := NameLevelRejection(centralized, name); !ok || reason == "" {
			t.Fatalf("%s must be name-level rejected under %v, got ok=%v", name, centralized, ok)
		}
	}
	// Argument-dependent and unaffected tools never report a name-level rejection.
	for _, name := range []string{"web_fetch", "im_message", "read_file", "office", "generate_pdf"} {
		if ok, _ := NameLevelRejection(centralized, name); ok {
			t.Fatalf("%s must not be name-level rejected", name)
		}
	}
	// Without centralization or with the feature enabled, nothing is rejected.
	if ok, _ := NameLevelRejection(corelib.AppConfig{SandboxMode: "os"}, "bash"); ok {
		t.Fatal("non-centralized config must not reject bash at name level")
	}
	if ok, _ := NameLevelRejection(corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "none"}, "bash"); ok {
		t.Fatal("sandbox_mode=none must not reject bash")
	}
	open := corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "os", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if ok, _ := NameLevelRejection(open, "send_file"); ok {
		t.Fatal("file outbound enabled must not reject send_file")
	}
	if ok, _ := NameLevelRejection(open, "screenshot"); ok {
		t.Fatal("image outbound enabled must not reject screenshot")
	}
}

// Regression: generate_pdf performs no network I/O (the doc generator renders
// Markdown locally), so a report whose prose merely mentions a source URL must
// not trip the intranet/allowlist network gate. The production failure was the
// host-owned auto PDF for a weather report whose content cited weather sites.
func TestEnforceConfigAllowlistIgnoresProseEmbeddedURLsInOfflineTools(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	prose := map[string]interface{}{
		"title":   "南京天气报告",
		"content": "数据来源：中国天气网 https://weather.com.cn 与 https://example.org/report。今日多云 25-32°C。",
	}
	for _, name := range []string{"generate_pdf", "write_file", "office"} {
		if ok, reason := EnforceConfig(cfg, name, prose); !ok {
			t.Fatalf("%s with prose-embedded URLs blocked reason=%q; offline tools must not be network-gated by payload text", name, reason)
		}
	}
	// Network level "none" behaves the same for offline tools.
	cfg.NetworkLevel = "none"
	if ok, reason := EnforceConfig(cfg, "generate_pdf", prose); !ok {
		t.Fatalf("generate_pdf with prose URLs blocked under level=none reason=%q", reason)
	}
}

// `open` hands its target to the user's own system handler: the user sees the
// destination and nothing flows back into the model. It is therefore not the
// silent egress the network scope exists to bound. Before 2026-10-11 it was
// gated by the allowlist like web_fetch, which rejected it for *every* target
// — including local file paths — whenever the Hub shipped allowlist mode.
func TestEnforceConfigUserVisibleHandoffToolStaysUsable(t *testing.T) {
	// Allowlist mode, and the degenerate case that caused the incident: an
	// allowlist with no entry at all.
	for _, allowlist := range [][]string{{"api.example.com"}, nil} {
		cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: allowlist, FileOutboundEnabled: true, ImageOutboundEnabled: true}

		if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": "https://app.uned.es/soid/email-registro"}); !ok {
			t.Fatalf("open URL blocked under allowlist=%v reason=%q; handoff must stay usable", allowlist, reason)
		}
		// A local target was never a network action in the first place.
		if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": `D:\reports\weather.pdf`}); !ok {
			t.Fatalf("open local path blocked under allowlist=%v reason=%q", allowlist, reason)
		}
	}

	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "intranet", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": "https://app.uned.es/soid"}); !ok {
		t.Fatalf("open URL blocked under intranet reason=%q", reason)
	}

	// Non-HTTP targets resolve to a local handler (mail client, file viewer)
	// and are never network endpoints.
	for _, target := range []string{"mailto:someone@example.com", `file:///C:/reports/q3.pdf`} {
		if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": target}); !ok {
			t.Fatalf("open non-HTTP target %q blocked reason=%q", target, reason)
		}
	}

	// A policy that disables networking outright still covers handoff: the
	// organization said no network, and a URL target is network.
	cfg.NetworkLevel = "none"
	if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": "https://app.uned.es/soid"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("open URL allowed under level=none allowed=%v reason=%q", ok, reason)
	}
	// ...but a local file has nothing to do with the network, at any level.
	if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": `/Users/me/notes.md`}); !ok {
		t.Fatalf("open local path blocked under level=none reason=%q", reason)
	}
}

// The handoff exemption must not widen the hole for tools that really do fetch
// remote content into the model context.
func TestEnforceConfigHandoffExemptionDoesNotWeakenSilentFetch(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://app.uned.es/soid"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("web_fetch outside allowlist allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "web_search", map[string]interface{}{"query": "weather"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("web_search allowed=%v reason=%q, want allowlist rejection (a query is itself a remote request)", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://api.example.com/v1"}); !ok {
		t.Fatalf("allowlisted fetch blocked reason=%q", reason)
	}
}

// The trusted browser adapter streams page content back into the model, so it
// is silent egress and must obey the network gate exactly like web_fetch.
// Before the gate was wired into the semantic trusted-adapter path, the
// browser ran with no network policy at all while web_fetch was gated — the
// inverse of the risk the gate exists to bound.
func TestEnforceConfigBrowserIsGatedLikeSilentFetch(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	if ok, reason := EnforceConfig(cfg, "browser", map[string]interface{}{"url": "https://app.uned.es/soid"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("browser outside allowlist allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "browser", map[string]interface{}{"url": "https://api.example.com/v1"}); !ok {
		t.Fatalf("allowlisted browser blocked reason=%q", reason)
	}
	// A host-observed browser action is still network egress, so a total
	// shutdown closes it too.
	cfg.NetworkLevel = "none"
	if ok, reason := EnforceConfig(cfg, "browser", map[string]interface{}{"url": "https://api.example.com/v1"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("browser allowed under level=none allowed=%v reason=%q", ok, reason)
	}
}

// An allowlist with no entry denies every domain, which is the same outcome as
// level "none". The reason has to say which one it is, or nobody can tell a
// domain decision from a policy that was never completed.
func TestEnforceConfigEmptyAllowlistExplainsItself(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: nil, FileOutboundEnabled: true, ImageOutboundEnabled: true}
	_, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"})
	if !strings.Contains(reason, "allowlist") || !strings.Contains(reason, "no allowed domain") {
		t.Fatalf("empty allowlist reason=%q, want the missing-entry cause named", reason)
	}

	cfg.NetworkAllowlist = []string{"example.com"}
	if ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com"}); !ok {
		t.Fatalf("populated allowlist blocked an allowed domain reason=%q", reason)
	}
}

// A network_level this client does not recognize must not reach the
// "no restriction" ending of enforceNetworkLevel. Before this was pinned, an
// unrecognized value — a typo, or one written by a newer Hub — fell through
// every branch and returned allowed, silently granting full network access.
func TestEnforceConfigUnknownNetworkLevelIsDeniedNotOpen(t *testing.T) {
	for _, level := range []string{"allow-list", "restricted", "internal", "Allow List", "deny"} {
		cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: level, FileOutboundEnabled: true, ImageOutboundEnabled: true}
		ok, reason := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": "https://example.com/data"})
		if ok {
			t.Fatalf("level %q allowed a network tool, want denial", level)
		}
		// The message has to name the offending value, or the operator is back
		// to guessing why a client denies what the Hub thinks it permits.
		// EnforceConfig lowercases before enforcing, so match case-insensitively.
		if !strings.Contains(reason, "network_level") || !strings.Contains(strings.ToLower(reason), strings.ToLower(strings.TrimSpace(level))) {
			t.Fatalf("level %q reason=%q, want it to name the unrecognized level", level, reason)
		}
	}
}

// An unrecognized level must be scoped like a restrictive policy, not like a
// total shutdown. Refusing it at the top of enforceNetworkLevel — the first
// implementation — denied read_file, write_file, glob and bash as well, so a
// single typo in the Hub policy bricked the whole agent, while the *most*
// restrictive legitimate level ("none") still let local tools through. A
// malformed network policy has no business touching local work.
func TestEnforceConfigUnknownNetworkLevelSparesLocalTools(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allow-list", FileOutboundEnabled: true, ImageOutboundEnabled: true}

	for _, name := range []string{"read_file", "write_file", "list_directory", "glob", "edit_file"} {
		args := map[string]interface{}{"path": "D:/notes.txt"}
		if ok, reason := EnforceConfig(cfg, name, args); !ok {
			t.Fatalf("%s denied under an unknown network level: %q, want local tools unaffected", name, reason)
		}
	}
	// The network channels the malformed policy was meant to govern stay shut.
	for _, name := range []string{"web_fetch", "web_search", "download_file"} {
		args := map[string]interface{}{"url": "https://example.com/data"}
		if ok, reason := EnforceConfig(cfg, name, args); ok {
			t.Fatalf("%s allowed under an unknown network level, want denial", name)
		} else if !strings.Contains(reason, "network_level") {
			t.Fatalf("%s reason=%q, want it to name the unrecognized level", name, reason)
		}
	}
	// Handoff is user-visible and content never returns to the model, so it is
	// not governed by a level this client cannot interpret either.
	if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": "D:/report.pdf"}); !ok {
		t.Fatalf("open of a local path denied: %q", reason)
	}
	if ok, reason := EnforceConfig(cfg, "open", map[string]interface{}{"target": "https://example.com"}); !ok {
		t.Fatalf("open of a URL denied under an unknown level: %q", reason)
	}
}

// The known levels must keep behaving exactly as before, and an unset level
// still means the product default rather than a denial.
func TestEnforceConfigKnownNetworkLevelsUnchanged(t *testing.T) {
	base := corelib.AppConfig{HubSecurityCentralized: true, FileOutboundEnabled: true, ImageOutboundEnabled: true}
	fetch := map[string]interface{}{"url": "https://example.com/data"}

	cases := []struct {
		level    string
		allow    bool
		wantWord string
	}{
		{level: "", allow: true}, // unset == documented default "full"
		{level: "full", allow: true},
		{level: "none", allow: false, wantWord: "disabled"},
		{level: "intranet", allow: false, wantWord: "intranet"},
		{level: "allowlist", allow: false, wantWord: "allowlist"},
		// Case and padding are normalized rather than treated as unknown.
		{level: "ALLOWLIST", allow: false, wantWord: "allowlist"},
		{level: " intranet ", allow: false, wantWord: "intranet"},
		{level: " FULL ", allow: true},
	}
	for _, tc := range cases {
		cfg := base
		cfg.NetworkLevel = tc.level
		ok, reason := EnforceConfig(cfg, "web_fetch", fetch)
		if ok != tc.allow {
			t.Fatalf("level %q allowed=%v reason=%q, want allowed=%v", tc.level, ok, reason, tc.allow)
		}
		if tc.wantWord != "" && !strings.Contains(reason, tc.wantWord) {
			t.Fatalf("level %q reason=%q, want it to mention %q", tc.level, reason, tc.wantWord)
		}
	}
}

// NormalizeNetworkLevel is the shared definition of the legal set; the ingest
// path resolves through it too, so the two cannot drift.
func TestNormalizeNetworkLevel(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "", want: "full", ok: true},
		{raw: "FULL", want: "full", ok: true},
		{raw: " none ", want: "none", ok: true},
		{raw: "Intranet", want: "intranet", ok: true},
		{raw: "allowlist", want: "allowlist", ok: true},
		{raw: "allow-list", want: "", ok: false},
		{raw: "closed", want: "", ok: false},
	} {
		got, ok := NormalizeNetworkLevel(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("NormalizeNetworkLevel(%q)=(%q,%v), want (%q,%v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
	// Every level the product offers must be accepted by the normalizer,
	// otherwise the UI selector and the gate disagree.
	for _, level := range KnownNetworkLevels() {
		if got, ok := NormalizeNetworkLevel(level); !ok || got != level {
			t.Fatalf("KnownNetworkLevels lists %q but NormalizeNetworkLevel returned (%q,%v)", level, got, ok)
		}
	}
}

// The endpoint-focused gate must still catch arguments that a tool would
// plausibly consume as a network endpoint.
func TestEnforceConfigAllowlistStillChecksEndpointArgs(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true}

	// Endpoint-designated keys are checked even for tools not on the static
	// network-tool list (unknown/custom tools, MCP payloads).
	endpointArgs := map[string]interface{}{"target": "see https://evil.example.org/feed for details"}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", endpointArgs); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("endpoint-key URL allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
	// A value that is itself a URL counts regardless of the key name.
	bareURL := map[string]interface{}{"query": "https://evil.example.org"}
	if ok, reason := EnforceConfig(cfg, "custom_tool", bareURL); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("bare URL value allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
	// Command/script payloads containing URLs remain network evidence.
	scriptArgs := map[string]interface{}{"script": "invoke-webrequest https://evil.example.org/x"}
	if ok, reason := EnforceConfig(cfg, "custom_runner", scriptArgs); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("script URL allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
	// Allowlisted endpoints still pass.
	allowed := map[string]interface{}{"url": "https://api.example.com/v1"}
	if ok, reason := EnforceConfig(cfg, "call_mcp_tool", allowed); !ok {
		t.Fatalf("allowlisted endpoint URL blocked reason=%q", reason)
	}
}

// TestNormalizeNetworkLevelEmptyIsFull locks the product default: "not
// configured" is the documented "full" level, not a missing policy. The live
// gate, the config load backfill, and the persist path (which reverts an
// omitted Hub level to "") all depend on this being true, so it must not
// silently change. This is the contract that keeps a Hub policy omitting
// network_level from ever slipping into deny-everything.
func TestNormalizeNetworkLevelEmptyIsFull(t *testing.T) {
	got, ok := NormalizeNetworkLevel("")
	if got != "full" || !ok {
		t.Fatalf("NormalizeNetworkLevel(\"\") = (%q, %v), want (\"full\", true)", got, ok)
	}
	// Whitespace-only and case variants collapse the same way.
	for _, in := range []string{"  ", "\t", "FULL", "Full", "NONE", "AllowList", "INTRANET"} {
		got, ok := NormalizeNetworkLevel(in)
		if !ok {
			t.Fatalf("NormalizeNetworkLevel(%q) not recognized, want recognized", in)
		}
		want := strings.ToLower(strings.TrimSpace(in))
		if want == "" {
			want = "full"
		}
		if got != want {
			t.Fatalf("NormalizeNetworkLevel(%q) = %q, want canonical %q", in, got, want)
		}
	}
	// A typo / newer-Hub value is reported unrecognized so the gate can refuse
	// it instead of failing open.
	if _, ok := NormalizeNetworkLevel("restricted"); ok {
		t.Fatalf("NormalizeNetworkLevel(\"restricted\") recognized, want unrecognized so the gate denies it")
	}
}

// TestEnforceConfigEmptyLevelAllowsNetwork proves the gate reads an empty
// network_level as "full": a silent network tool (web_fetch) that would be
// blocked under every real restriction is allowed when the level is unset.
// This is the runtime guarantee behind the persist-path revert and the
// "默认可用" principle — the two paths must agree, or an omitted Hub level
// becomes a silent outage.
func TestEnforceConfigEmptyLevelAllowsNetwork(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	args := map[string]interface{}{"url": "https://example.com/data"}
	if ok, reason := EnforceConfig(cfg, "web_fetch", args); !ok {
		t.Fatalf("web_fetch denied with empty NetworkLevel=%q, want allowed (empty = full)", reason)
	}
}

// TestEnforceConfigNestedURLShapeIndependence guards against deciding network
// policy by Go static type.
//
// The argument scanner used to enumerate container types it knew about
// ([]interface{}, []string). The identical hostile payload then produced
// opposite verdicts depending on the slice's element type: steps built as
// []interface{} were refused under network_level=none, while steps built as
// []map[string]interface{} sailed through. desktop task_run's multi-step
// browser payload is exactly this shape, so one actions array could carry a
// navigate-to-any-domain step past an organization-wide network shutdown.
//
// Nothing guaranteed which shape arrives: it depended on how a caller happened
// to construct its arguments rather than on JSON decoding, so the safe outcome
// was luck, not design. This test pins the payload alone as the deciding
// factor; if someone reintroduces a static type switch in the scanner, these
// cases diverge again.
func TestEnforceConfigNestedURLShapeIndependence(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	const hostile = "https://evil.example.com/exfiltrate?d=context"

	shapes := map[string]map[string]interface{}{
		"[]interface{} of map": {"action": "task_run", "steps": []interface{}{
			map[string]interface{}{"action": "navigate", "params": map[string]interface{}{"url": hostile}},
		}},
		"[]map[string]interface{}": {"action": "task_run", "steps": []map[string]interface{}{
			{"action": "navigate", "params": map[string]interface{}{"url": hostile}},
		}},
		"[]map[string]string params": {"action": "task_run", "steps": []map[string]interface{}{
			{"action": "navigate", "params": map[string]string{"url": hostile}},
		}},
		"map of maps": {"action": "task_run", "steps": map[string]interface{}{
			"0": map[string]interface{}{"params": map[string]interface{}{"url": hostile}},
		}},
	}

	for name, args := range shapes {
		ok, reason := EnforceConfig(cfg, "desktop", args)
		if ok {
			t.Errorf("desktop steps shaped %s were allowed under network_level=none, want refused; a nested URL must be visible regardless of container type", name)
			continue
		}
		if !strings.Contains(reason, "network") {
			t.Errorf("desktop steps shaped %s refused with unexpected reason %q", name, reason)
		}
	}

	// The scanner must still see a nested URL when the *tool* is not itself a
	// network tool, which is how desktop lands here at all.
	if ok, _ := EnforceConfig(cfg, "web_fetch", map[string]interface{}{"arguments": []map[string]interface{}{{"url": hostile}}}); ok {
		t.Fatal("web_fetch with []map-shaped arguments must still be refused")
	}
}

// TestValueContainsNetworkEndpointIgnoresContainerType is the unit-level twin
// of the above: it pins the scanner rather than the whole gate, so a
// regression points at the exact helper.
func TestValueContainsNetworkEndpointIgnoresContainerType(t *testing.T) {
	const hostile = "https://evil.example.com/x"
	given := []interface{}{
		[]interface{}{map[string]interface{}{"url": hostile}},
		[]map[string]interface{}{{"url": hostile}},
		map[string]interface{}{"url": hostile},
		map[string]interface{}{"steps": []map[string]interface{}{{"url": hostile}}},
		map[string]map[string]string{"params": {"url": hostile}},
		map[string]interface{}{"steps": map[string]interface{}{"a": map[string]string{"url": hostile}}},
	}
	for i, v := range given {
		if !valueContainsNetworkEndpoint(v, "") {
			t.Errorf("valueContainsNetworkEndpoint case %d (%T) missed a URL; container shape must not change the verdict", i, v)
		}
	}
	// A non-network string must stay invisible, or every tool becomes a
	// network tool and local operations break under a restricted level.
	if valueContainsNetworkEndpoint(map[string]interface{}{"path": "/home/desktop/Desktop/a.c"}, "") {
		t.Fatal("a plain local path must not read as a network endpoint")
	}
}
