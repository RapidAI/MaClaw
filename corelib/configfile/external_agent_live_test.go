package configfile

import (
	"os"
	"testing"
)

func TestLiveScanExternalAgentsOnThisMachine(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_AGENT_SCAN") != "1" {
		t.Skip("set MACLAW_LIVE_AGENT_SCAN=1 to probe the real home directory")
	}
	cands := ScanExternalAgents()
	t.Logf("candidates=%d", len(cands))
	for _, c := range cands {
		t.Logf("source=%s name=%s url=%s model=%s protocol=%s wire=%s agent=%s auth=%s key_len=%d",
			c.Source, c.Name, c.URL, c.Model, c.Protocol, c.WireAPI, c.AgentType, c.AuthType, len(c.Key))
	}
}
