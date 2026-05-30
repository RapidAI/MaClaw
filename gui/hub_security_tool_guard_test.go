package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestHubSecurityToolGuardBlocksDisabledOutbound(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  false,
			ImageOutboundEnabled: false,
			NetworkLevel:         "full",
		},
	}
	app.hubSecurityCache.mu.Unlock()
	h := &IMMessageHandler{app: app}

	if ok, reason := h.enforceHubSecurityToolPolicy("send_file", map[string]interface{}{"path": "report.pdf"}); ok || !strings.Contains(reason, "file outbound") {
		t.Fatalf("send_file allowed=%v reason=%q, want file outbound rejection", ok, reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("screenshot", nil); ok || !strings.Contains(reason, "image outbound") {
		t.Fatalf("screenshot allowed=%v reason=%q, want image outbound rejection", ok, reason)
	}
}

func TestHubSecurityToolGuardBlocksNetworkAndUnsandboxedBash(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			SandboxMode:          "os",
			NetworkLevel:         "none",
		},
	}
	app.hubSecurityCache.mu.Unlock()
	h := &IMMessageHandler{app: app}

	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("web_fetch allowed=%v reason=%q, want network rejection", ok, reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("search_and_install_skill", map[string]interface{}{"query": "deploy"}); ok || !strings.Contains(reason, "network") {
		t.Fatalf("skill search allowed=%v reason=%q, want network rejection", ok, reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("bash", map[string]interface{}{"command": "echo ok"}); ok || !strings.Contains(reason, "sandbox") {
		t.Fatalf("bash allowed=%v reason=%q, want sandbox rejection", ok, reason)
	}
}

func TestHubSecurityToolGuardAllowsIntranetURL(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			NetworkLevel:         "intranet",
		},
	}
	app.hubSecurityCache.mu.Unlock()
	h := &IMMessageHandler{app: app}

	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "http://192.168.1.10/status"}); !ok {
		t.Fatalf("intranet URL blocked reason=%q", reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "https://example.com"}); ok || !strings.Contains(reason, "intranet") {
		t.Fatalf("public URL allowed=%v reason=%q, want intranet rejection", ok, reason)
	}
}

func TestHubSecurityToolGuardNetworkAllowlist(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			NetworkLevel:         "allowlist",
			NetworkAllowlist:     []string{"api.example.com", "*.corp.local"},
		},
	}
	app.hubSecurityCache.mu.Unlock()
	h := &IMMessageHandler{app: app}

	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "https://api.example.com/v1"}); !ok {
		t.Fatalf("allowlisted host blocked reason=%q", reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "https://svc.corp.local/status"}); !ok {
		t.Fatalf("allowlisted wildcard host blocked reason=%q", reason)
	}
	if ok, reason := h.enforceHubSecurityToolPolicy("web_fetch", map[string]interface{}{"url": "https://example.org"}); ok || !strings.Contains(reason, "allowlist") {
		t.Fatalf("non-allowlisted URL allowed=%v reason=%q, want allowlist rejection", ok, reason)
	}
}

func TestHubSecurityAppGuardBlocksDirectSkillSource(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			NetworkLevel:         "full",
			SkillSourcesAllowed:  []string{"skillhub"},
		},
	}
	app.hubSecurityCache.mu.Unlock()

	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("github direct install allowed=%v reason=%q, want source rejection", ok, reason)
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "skillmarket", "skill_id": "demo"}); !ok {
		t.Fatalf("skillmarket direct install should follow skillhub policy, reason=%q", reason)
	}
}

func TestHubSecurityAppGuardBlocksIndependentSkillSource(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		SkillSourcesAllowed: []string{"skillhub"},
	}
	app.hubSecurityCache.mu.Unlock()

	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("github direct install allowed=%v reason=%q, want independent source rejection", ok, reason)
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": "https://example.com"}); !ok {
		t.Fatalf("network policy should not apply without centralized security, reason=%q", reason)
	}
}

func TestHubSecurityAppGuardBlocksAllSkillSources(t *testing.T) {
	app := &App{policyEngine: NewPolicyEngineWithMode("standard")}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity:    false,
		SkillSourcesRestricted: true,
		SkillSourcesAllowed:    []string{},
	}
	app.hubSecurityCache.mu.Unlock()

	if allowed := app.GetAllowedSkillSources(); allowed == nil || len(allowed) != 0 {
		t.Fatalf("GetAllowedSkillSources() = %#v, want non-nil empty block-all list", allowed)
	}
	if app.IsSkillSourceAllowed("skillhub") {
		t.Fatal("skillhub should be blocked by empty restricted source policy")
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "search", "query": "demo"}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("skill search allowed=%v reason=%q, want source rejection", ok, reason)
	}
}

func TestHubSecurityAppGuardCentralizedBlocksAllSkillSources(t *testing.T) {
	app := &App{policyEngine: NewPolicyEngineWithMode("standard")}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:    true,
			ImageOutboundEnabled:   true,
			NetworkLevel:           "full",
			SkillSourcesRestricted: true,
			SkillSourcesAllowed:    []string{},
		},
	}
	app.hubSecurityCache.mu.Unlock()

	if allowed := app.GetAllowedSkillSources(); allowed == nil || len(allowed) != 0 {
		t.Fatalf("GetAllowedSkillSources() = %#v, want non-nil empty block-all list", allowed)
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "zip", "zip_base64": "e30="}); ok || !strings.Contains(reason, "skill source") {
		t.Fatalf("skill install allowed=%v reason=%q, want source rejection", ok, reason)
	}
}

func TestHubSecurityAppGuardDeveloperModeAllowsIndependentSkillSource(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), policyEngine: NewPolicyEngineWithMode("developer")}
	if err := app.SaveConfig(corelib.AppConfig{SecurityPolicyMode: "developer", SkillSourcesAllowed: []string{"skillhub"}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		SkillSourcesAllowed: []string{"skillhub"},
	}
	app.hubSecurityCache.mu.Unlock()

	if allowed := app.GetAllowedSkillSources(); allowed != nil {
		t.Fatalf("developer allowed sources = %#v, want nil/all", allowed)
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "github", "install_ref": "https://github.com/acme/skill"}); !ok {
		t.Fatalf("developer github install blocked reason=%q", reason)
	}
}

func TestSkillSearchPolicySourceUsesAllowedSource(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		SkillSourcesAllowed: []string{"github"},
	}
	app.hubSecurityCache.mu.Unlock()

	if got := app.skillSearchPolicySource(); got != "github" {
		t.Fatalf("skillSearchPolicySource() = %q, want github", got)
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("search_and_install_skill", map[string]interface{}{"query": "deploy", "source": app.skillSearchPolicySource()}); !ok {
		t.Fatalf("github-only search should be allowed with selected source, reason=%q", reason)
	}
}

func TestSkillSearchPolicyArgsCarriesHubURLForAllowlist(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "https://hub.example.com"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			NetworkLevel:         "allowlist",
			NetworkAllowlist:     []string{"hub.example.com"},
			SkillSourcesAllowed:  []string{"skillhub"},
		},
	}
	app.hubSecurityCache.mu.Unlock()

	args := app.skillSearchPolicyArgs("deploy")
	if args["hub_url"] != "https://hub.example.com" {
		t.Fatalf("skillSearchPolicyArgs hub_url = %#v", args["hub_url"])
	}
	if ok, reason := app.enforceHubSecurityAppPolicy("search_and_install_skill", args); !ok {
		t.Fatalf("skillhub search should allow configured hub URL, reason=%q", reason)
	}
}

func TestSearchMixedSkillsHonorsHubNetworkPolicy(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.SearchMixedSkills("deploy"); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("SearchMixedSkills err=%v, want network rejection", err)
	}
}

func TestDeveloperSkillInstallFinalAuditActionRecordsRisk(t *testing.T) {
	app := &App{policyEngine: NewPolicyEngineWithMode("developer")}
	report := &cskill.ScanReport{FinalLevel: security.RiskCritical, Summary: "critical"}

	if got := app.skillInstallFinalAuditAction(report); got != security.PolicyAudit {
		t.Fatalf("developer critical final audit action = %s, want %s", got, security.PolicyAudit)
	}
	if got := app.skillInstallFinalAuditAction(&cskill.ScanReport{FinalLevel: security.RiskLow}); got != security.PolicyAllow {
		t.Fatalf("developer low final audit action = %s, want %s", got, security.PolicyAllow)
	}
}

func TestHubSecurityBlocksDirectMCPNetworkAndLocalCommand(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			SandboxMode:          "os",
			NetworkLevel:         "none",
		},
	}
	app.hubSecurityCache.mu.Unlock()
	app.mcpRegistry = NewMCPRegistry(app)

	if err := app.RegisterMCPServer(corelib.MCPServerEntry{Name: "remote", EndpointURL: "https://mcp.example/rpc"}); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("remote MCP register err=%v, want network rejection", err)
	}
	probe := app.TestMCPEndpoint("https://mcp.example/rpc", "none", "", nil)
	if !strings.Contains(probe.Message, "network") {
		t.Fatalf("probe message=%q, want network rejection", probe.Message)
	}
	if err := app.RegisterLocalMCPServer(corelib.LocalMCPServerEntry{Name: "local", Command: "node", Args: []string{"server.js"}}); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("local MCP register err=%v, want sandbox rejection", err)
	}
}
