package commands

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
)

func TestSkillHubInstallHonorsHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := skillhubInstall([]string{"demo"})
	if err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("skillhubInstall err=%v, want network rejection", err)
	}
}

func TestSkillHubInstallGitHubHonorsSourcePolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		NetworkLevel:         "full",
		FileOutboundEnabled:  true,
		ImageOutboundEnabled: true,
		SkillSourcesAllowed:  []string{"skillhub"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := skillhubInstallGitHub([]string{"https://github.com/acme/demo-skill"})
	if err == nil || !strings.Contains(err.Error(), "skill source") {
		t.Fatalf("skillhubInstallGitHub err=%v, want source rejection", err)
	}
}

func TestSkillHubUpdateHonorsHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := skillhubUpdate([]string{"demo"})
	if err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("skillhubUpdate err=%v, want network rejection", err)
	}
}

func TestSearchSkillAPIsHonorHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := SearchSkillHub("deploy"); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("SearchSkillHub err=%v, want network rejection", err)
	}

	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{SkillSourcesAllowed: []string{"github"}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := SearchSkillMarket("https://hub.example", "deploy", 5); err == nil || !strings.Contains(err.Error(), "skill source") {
		t.Fatalf("SearchSkillMarket err=%v, want source rejection", err)
	}
}

func TestSkillSearchAPIArgsCarriesHubURLForAllowlist(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"hub.example"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"skillhub"}}

	args := skillSearchAPIArgs(cfg, "deploy", "skillhub", "https://hub.example")
	if args["hub_url"] != "https://hub.example" {
		t.Fatalf("skillSearchAPIArgs hub_url = %#v", args["hub_url"])
	}
	if ok, reason := clientsecurity.EnforceConfig(cfg, "search_and_install_skill", args); !ok {
		t.Fatalf("skillhub API search should carry allowlisted hub URL, reason=%q", reason)
	}
}

func TestSkillSearchAPIAllowedSourcesSkipsBlockedSources(t *testing.T) {
	cfg := corelib.AppConfig{HubSecurityCentralized: true, NetworkLevel: "allowlist", NetworkAllowlist: []string{"github.com"}, FileOutboundEnabled: true, ImageOutboundEnabled: true, SkillSourcesAllowed: []string{"hubcenter", "clawhub", "git_hub"}}

	sources, err := skillSearchAPIAllowedSourcesForPolicy(cfg, "deploy", "https://hub.example")
	if err != nil {
		t.Fatalf("skillSearchAPIAllowedSourcesForPolicy() error = %v", err)
	}
	if len(sources) != 1 || sources[0] != "github" {
		t.Fatalf("allowed sources = %#v, want github only", sources)
	}
}

func TestSkillHubDeveloperModeDoesNotBlockSkillFetch(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "developer",
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		SkillSourcesAllowed:    []string{"skillhub"},
	}
	if err := enforceSkillHubFetchSecurity(cfg, "https://github.com/acme/demo-skill"); err != nil {
		t.Fatalf("developer mode should audit-only allow skill fetch, got %v", err)
	}
	if err := enforceSkillHubSourceAction(cfg, "github", "install", map[string]interface{}{"install_ref": "https://github.com/acme/demo-skill"}); err != nil {
		t.Fatalf("developer mode should allow any skill source, got %v", err)
	}
}
