package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/skill"
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

func TestCommitSkillHubConfigBatchIsAtomicAndAudited(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(dataDir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatal(err)
	}
	entries := []corelib.NLSkillEntry{{Name: "batch-a", Source: "github"}, {Name: "batch-b", Source: "github"}}
	if err := commitSkillHubConfigBatch(store, entries, "github_install", "skill:cli_skillhub_github_installed"); err != nil {
		t.Fatalf("batch commit failed: %v", err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NLSkills) != len(entries) {
		t.Fatalf("config entries = %d, want %d", len(cfg.NLSkills), len(entries))
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil || len(summaries) != 0 {
		t.Fatalf("compensation queue = %#v, err=%v", summaries, err)
	}
	if _, err := os.Stat(skill.DefaultEvolutionAuditPath()); err != nil {
		t.Fatalf("strict final audit missing: %v", err)
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
