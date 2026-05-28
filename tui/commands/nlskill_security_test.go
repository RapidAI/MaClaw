package commands

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNLSkillBashStepHonorsHubSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		SandboxMode:            "os",
		NetworkLevel:           "full",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err := tui_executeBashStep("echo blocked", map[string]interface{}{}, "")
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("tui_executeBashStep err=%v, want sandbox rejection", err)
	}
}

func TestNLSkillBashStepBlocksNetworkCommand(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	if err := NewFileConfigStore(dataDir).SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		SandboxMode:            "none",
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err := tui_executeBashStep("curl https://example.com", map[string]interface{}{}, "")
	if err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("tui_executeBashStep err=%v, want network rejection", err)
	}
}
