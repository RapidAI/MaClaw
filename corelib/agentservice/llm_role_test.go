package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func inheritFalse() *bool {
	f := false
	return &f
}

func testRoleProviders() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMCurrentProvider: "DeepSeek",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: OfficialHubLLMProviderName, URL: "https://hub.example/api/llm/v1", Key: "viewer", Model: "auto", IsHubService: true},
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-ds", Model: "deepseek-chat", Protocol: "openai"},
			{Name: "Claude", URL: "https://api.anthropic.com", Key: "sk-ant", Model: "claude-sonnet", Protocol: "anthropic"},
		},
	}
}

func TestResolveLLMRoleFallsBackToDefaultCurrent(t *testing.T) {
	cfg := testRoleProviders()
	got, err := ResolveLLMRole(cfg, LLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderName != "DeepSeek" || got.Model != "deepseek-chat" {
		t.Fatalf("default foreground = %+v", got)
	}
	bg, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || bg.ProviderName != "DeepSeek" {
		t.Fatalf("default background = %+v err=%v", bg, err)
	}
	cu, err := ResolveLLMRole(cfg, LLMRoleComputerUse)
	if err != nil || cu.ProviderName != "DeepSeek" {
		t.Fatalf("default computer_use = %+v err=%v", cu, err)
	}
	if _, err := ResolveLLMRole(cfg, LLMRoleGrounder); err != ErrGrounderUnused {
		t.Fatalf("default grounder err = %v, want unused", err)
	}
}

func TestResolveLLMRoleUsesHubAdminPin(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles = corelib.MaclawHubBotRoles{
		Foreground:  corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRoleInherit},
		Background:  corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"},
		ComputerUse: corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: OfficialHubLLMProviderName},
		Grounder:    corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"},
	}
	fg, err := ResolveLLMRole(cfg, LLMRoleForeground)
	if err != nil || fg.ProviderName != "DeepSeek" {
		t.Fatalf("inherit foreground = %+v err=%v", fg, err)
	}
	bg, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || bg.ProviderName != "Claude" || bg.Model != "claude-sonnet" || bg.Protocol != "anthropic" {
		t.Fatalf("pinned background = %+v err=%v", bg, err)
	}
	cu, err := ResolveLLMRole(cfg, LLMRoleComputerUse)
	if err != nil || cu.ProviderName != OfficialHubLLMProviderName || cu.Model != "auto" {
		t.Fatalf("official computer_use = %+v err=%v", cu, err)
	}
	gr, err := ResolveLLMRole(cfg, LLMRoleGrounder)
	if err != nil || gr.ProviderName != "Claude" {
		t.Fatalf("pinned grounder = %+v err=%v", gr, err)
	}
}

func TestResolveLLMRoleOfficialAliasesAndStalePinFallBack(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.ComputerUse = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "maclaw_official_group"}
	cu, err := ResolveLLMRole(cfg, LLMRoleComputerUse)
	if err != nil || cu.ProviderName != OfficialHubLLMProviderName {
		t.Fatalf("official alias = %+v err=%v", cu, err)
	}
	cfg.MaclawHubBotRoles.Background = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "gone"}
	bg, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || bg.ProviderName != "DeepSeek" {
		t.Fatalf("stale pin should fall back, got %+v err=%v", bg, err)
	}
}

func TestResolveLLMRoleProfilePinWinsOverAdmin(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.Foreground = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"}
	cfg.MaclawLLMProfiles = &corelib.MaclawLLMProfiles{
		Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: inheritFalse(), Model: "official-mid"},
	}
	fg, err := ResolveLLMRole(cfg, LLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.ProviderName != OfficialHubLLMProviderName || fg.Model != "official-mid" {
		t.Fatalf("profile pin should stay on official with the pinned model, got %+v", fg)
	}
}

func TestResolveLLMRoleProfilePinHonorsProviderID(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawLLMProviders[2].ID = "prov-claude" // Claude
	cfg.MaclawLLMProfiles = &corelib.MaclawLLMProfiles{
		Background: corelib.MaclawLLMRoleAssignment{InheritHub: inheritFalse(), ProviderID: "prov-claude", Model: "claude-haiku"},
	}
	bg, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if bg.ProviderName != "Claude" || bg.Model != "claude-haiku" || bg.Key != "sk-ant" {
		t.Fatalf("provider-pinned background = %+v", bg)
	}

	// A GUI pin may reference a provider that has no durable ID yet: it is
	// stored as the legacy name-derived ID.
	cfg.MaclawLLMProviders[2].ID = ""
	cfg.MaclawLLMProfiles.Background.ProviderID = corelib.MaclawLLMLegacyProviderID("Claude")
	bg, err = ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || bg.ProviderName != "Claude" {
		t.Fatalf("legacy-id pin = %+v err=%v", bg, err)
	}

	// A stale provider id fails the role instead of silently rerouting.
	cfg.MaclawLLMProfiles.Background.ProviderID = "gone"
	if _, err := ResolveLLMRole(cfg, LLMRoleBackground); err == nil {
		t.Fatal("expected a pin referencing a missing provider to fail")
	}
}

func TestResolveLLMRoleGrounderInheritFollowsDefault(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.Grounder = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRoleInherit}
	got, err := ResolveLLMRole(cfg, LLMRoleGrounder)
	if err != nil || got.ProviderName != "DeepSeek" {
		t.Fatalf("inherit grounder should follow the default, got %+v err=%v", got, err)
	}
	cfg.MaclawHubBotRoles.Grounder = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRoleUnused}
	if _, err := ResolveLLMRole(cfg, LLMRoleGrounder); err != ErrGrounderUnused {
		t.Fatalf("unused grounder = %v, want unused", err)
	}
}

func TestResolveTurnLLMRoleUsesBackgroundForLongJobWatch(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.Background = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"}
	fg, err := ResolveTurnLLMRole(cfg, nil, nil)
	if err != nil || fg.ProviderName != "DeepSeek" {
		t.Fatalf("ordinary turn = %+v err=%v", fg, err)
	}
	bg, err := ResolveTurnLLMRole(cfg, map[string]string{"long_job_watch": "1"}, nil)
	if err != nil || bg.ProviderName != "Claude" {
		t.Fatalf("watch turn = %+v err=%v", bg, err)
	}
}

func TestResolveLLMRoleModeSelectsOneSource(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.Background = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"}
	cfg.MaclawLLMProfiles = &corelib.MaclawLLMProfiles{
		Background: corelib.MaclawLLMRoleAssignment{InheritHub: inheritFalse(), ProviderID: corelib.MaclawLLMLegacyProviderID("DeepSeek"), Model: "deepseek-reasoner"},
	}

	// Empty mode is a config Hub has not rewritten: the profile pin still wins.
	legacy, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || legacy.Model != "deepseek-reasoner" || legacy.ProviderName != "DeepSeek" {
		t.Fatalf("legacy pin = %+v err=%v", legacy, err)
	}

	// Default ignores the pin and uses the service (here, the administrator pin).
	cfg.MaclawBotLLMMode = corelib.MaclawBotLLMModeDefault
	service, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || service.ProviderName != "Claude" || service.Model != "claude-sonnet" {
		t.Fatalf("default mode = %+v err=%v", service, err)
	}
	if _, err := ResolveLLMRole(cfg, LLMRoleGrounder); err != ErrGrounderUnused {
		t.Fatalf("default grounder = %v", err)
	}

	// Custom ignores the administrator pin and reads only the profile pin.
	cfg.MaclawBotLLMMode = corelib.MaclawBotLLMModeCustom
	custom, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil || custom.Model != "deepseek-reasoner" || custom.Key != "sk-ds" {
		t.Fatalf("custom mode = %+v err=%v", custom, err)
	}
	fg, err := ResolveLLMRole(cfg, LLMRoleForeground)
	if err != nil || fg.ProviderName != "DeepSeek" || fg.Model != "deepseek-chat" {
		t.Fatalf("unset custom foreground should follow the service, got %+v err=%v", fg, err)
	}
	if _, err := ResolveLLMRole(cfg, LLMRoleGrounder); err != ErrGrounderUnused {
		t.Fatalf("custom grounder without a pin = %v", err)
	}
}

func TestResolveLLMRoleDoesNotExposeOtherProviderKeys(t *testing.T) {
	cfg := testRoleProviders()
	cfg.MaclawHubBotRoles.Background = corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Claude"}
	got, err := ResolveLLMRole(cfg, LLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "sk-ant" {
		t.Fatalf("expected the pinned provider key, got %q", got.Key)
	}
	if strings.Contains(got.URL, "deepseek") {
		t.Fatalf("pinned Claude still pointed at DeepSeek: %+v", got)
	}
}
