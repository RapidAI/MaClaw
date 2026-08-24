package main

import (
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

func TestLiveExternalAgentAuthOnThisMachine(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_AGENT_SCAN") != "1" {
		t.Skip("set MACLAW_LIVE_AGENT_SCAN=1 to probe real local agent configs")
	}
	app := &App{}
	for _, candidate := range configfile.ScanExternalAgents() {
		provider, skip, _, err := app.importOneExternalAgent(candidate)
		if err != nil {
			t.Errorf("%s import error: %v", candidate.Name, err)
			continue
		}
		if skip.Reason != "" {
			t.Errorf("%s skipped: %s", candidate.Name, skip.Reason)
			continue
		}
		t.Logf("%s auth ok ua=%s wire=%s model=%s vision=%v", provider.Name, provider.AgentType, provider.WireAPI, provider.Model, provider.SupportsVision)
	}
}
