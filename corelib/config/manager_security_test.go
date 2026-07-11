package config

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type memoryConfigStore struct {
	cfg corelib.AppConfig
}

func (s *memoryConfigStore) LoadConfig() (corelib.AppConfig, error) {
	return s.cfg, nil
}

func (s *memoryConfigStore) SaveConfig(cfg corelib.AppConfig) error {
	s.cfg = cfg
	return nil
}

func TestImportConfigSkipsHubManagedSecurityKeys(t *testing.T) {
	store := &memoryConfigStore{cfg: corelib.AppConfig{
		Language:               "zh",
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "strict",
		SandboxMode:            "os",
		NetworkLevel:           "none",
		NetworkAllowlist:       []string{"api.example.com"},
		YoloModeAllowed:        false,
		SmartRouteEnabled:      false,
		GossipEnabled:          false,
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   false,
		SkillSourcesAllowed:    []string{"skillhub"},
	}}
	mgr := NewManager(store)

	report, err := mgr.ImportConfig(`{
		"language":"en",
		"hub_security_centralized":false,
		"security_policy_mode":"developer",
		"sandbox_mode":"none",
		"network_level":"full",
		"network_allowlist":["evil.example"],
		"yolo_mode_allowed":true,
		"smart_route_enabled":true,
		"gossip_enabled":true,
		"file_outbound_enabled":true,
		"image_outbound_enabled":true,
		"skill_sources_allowed":["github"]
	}`)
	if err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	if report.Applied != 1 || report.Skipped < 10 {
		t.Fatalf("report = %#v, want language applied and security keys skipped", report)
	}
	warnings := strings.Join(report.Warnings, "\n")
	if !strings.Contains(warnings, "Hub-managed security key") {
		t.Fatalf("warnings = %q, want Hub-managed skip warning", warnings)
	}
	saved := store.cfg
	if saved.Language != "en" {
		t.Fatalf("Language = %q, want en", saved.Language)
	}
	if !saved.HubSecurityCentralized || saved.SecurityPolicyMode != "strict" || saved.SandboxMode != "os" || saved.NetworkLevel != "none" {
		t.Fatalf("managed scalar security changed: %#v", saved)
	}
	if len(saved.NetworkAllowlist) != 1 || saved.NetworkAllowlist[0] != "api.example.com" || len(saved.SkillSourcesAllowed) != 1 || saved.SkillSourcesAllowed[0] != "skillhub" {
		t.Fatalf("managed security slices changed: allow=%v sources=%v", saved.NetworkAllowlist, saved.SkillSourcesAllowed)
	}
	if saved.YoloModeAllowed || saved.SmartRouteEnabled || saved.GossipEnabled || saved.FileOutboundEnabled || saved.ImageOutboundEnabled {
		t.Fatalf("managed bool security changed: %#v", saved)
	}
}

func TestImportConfigAllowsSecurityKeysWhenNotHubManaged(t *testing.T) {
	store := &memoryConfigStore{cfg: corelib.AppConfig{Language: "zh", HubSecurityCentralized: false, SecurityPolicyMode: "standard", SandboxMode: "none", NetworkLevel: "full", FileOutboundEnabled: true, ImageOutboundEnabled: true}}
	mgr := NewManager(store)

	if _, err := mgr.ImportConfig(`{"security_policy_mode":"strict","sandbox_mode":"os","network_level":"none","file_outbound_enabled":false}`); err != nil {
		t.Fatalf("ImportConfig() error = %v", err)
	}
	saved := store.cfg
	if saved.SecurityPolicyMode != "strict" || saved.SandboxMode != "os" || saved.NetworkLevel != "none" || saved.FileOutboundEnabled {
		t.Fatalf("security keys should import when Hub management is off: %#v", saved)
	}
}

func TestMaclawRoleUsesSharedDefaultDescription(t *testing.T) {
	mgr := NewManager(&memoryConfigStore{})

	formatted, err := mgr.GetConfig("maclaw_role")
	if err != nil {
		t.Fatalf("GetConfig(maclaw_role) error = %v", err)
	}
	if !strings.Contains(formatted, "maclaw_role_description: "+corelib.DefaultMaclawRoleDescription) {
		t.Fatalf("formatted role config = %q, want shared default description", formatted)
	}

	for _, section := range mgr.GetSchema() {
		if section.Name != "maclaw_role" {
			continue
		}
		for _, key := range section.Keys {
			if key.Key == "maclaw_role_description" && key.Default == corelib.DefaultMaclawRoleDescription {
				return
			}
		}
	}
	t.Fatalf("maclaw_role_description schema default = missing, want %q", corelib.DefaultMaclawRoleDescription)
}
