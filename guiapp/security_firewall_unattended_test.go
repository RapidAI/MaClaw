package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestUnattendedConfirmWaivesAskWithoutASessionGrant(t *testing.T) {
	engine := NewPolicyEngineWithMode("standard")
	writeFirewallRules(t, engine, []security.PolicyRule{
		{Name: "ask-tool", Priority: 1, ToolPattern: "ask_tool", Action: security.PolicyAsk},
	})
	fw := NewSecurityFirewall(NewSecurityRiskAnalyzer(), engine, nil)
	asked := 0
	fw.SetOnAsk(func(string, security.RiskAssessment) (bool, error) {
		asked++
		return false, nil
	})
	ctx := &SecurityCallContext{SessionID: "sess-unattended", UnattendedConfirm: true}
	allowed, reason := fw.Check("ask_tool", map[string]interface{}{}, ctx)
	if !allowed || asked != 0 {
		t.Fatalf("ask waived allowed=%v asked=%d reason=%s", allowed, asked, reason)
	}
	allowed, reason = fw.Check("ask_tool", map[string]interface{}{}, &SecurityCallContext{SessionID: "sess-unattended"})
	if allowed || asked != 1 {
		t.Fatalf("session grant leaked allowed=%v asked=%d reason=%s", allowed, asked, reason)
	}

	writeFirewallRules(t, engine, []security.PolicyRule{
		{Name: "deny-tool", Priority: 1, ToolPattern: "ask_tool", Action: security.PolicyDeny},
	})
	allowed, reason = fw.Check("ask_tool", map[string]interface{}{}, ctx)
	if allowed {
		t.Fatalf("deny waived: %s", reason)
	}
}

func writeFirewallRules(t *testing.T, engine *PolicyEngine, rules []security.PolicyRule) {
	t.Helper()
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := engine.LoadRules(path); err != nil {
		t.Fatal(err)
	}
}
