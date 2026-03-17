package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// DefaultPolicyRules
// ---------------------------------------------------------------------------

func TestDefaultPolicyRules_SortedByPriority(t *testing.T) {
	rules := DefaultPolicyRules()
	if len(rules) == 0 {
		t.Fatal("DefaultPolicyRules returned empty slice")
	}
	for i := 1; i < len(rules); i++ {
		if rules[i].Priority < rules[i-1].Priority {
			t.Errorf("rules not sorted: rule[%d].Priority=%d < rule[%d].Priority=%d",
				i, rules[i].Priority, i-1, rules[i-1].Priority)
		}
	}
}

// ---------------------------------------------------------------------------
// Evaluate — basic risk-level routing
// ---------------------------------------------------------------------------

func TestEvaluate_LowRisk_Allow(t *testing.T) {
	pe := NewPolicyEngine()
	action := pe.Evaluate("ReadFile", nil, RiskLow)
	if action != PolicyAllow {
		t.Errorf("expected allow for low risk, got %s", action)
	}
}

func TestEvaluate_MediumRisk_Audit(t *testing.T) {
	pe := NewPolicyEngine()
	action := pe.Evaluate("WriteFile", nil, RiskMedium)
	if action != PolicyAudit {
		t.Errorf("expected audit for medium risk, got %s", action)
	}
}

func TestEvaluate_HighRisk_Ask(t *testing.T) {
	pe := NewPolicyEngine()
	action := pe.Evaluate("Bash", nil, RiskHigh)
	if action != PolicyAsk {
		t.Errorf("expected ask for high risk, got %s", action)
	}
}

func TestEvaluate_CriticalRisk_WithDangerousArgs_Deny(t *testing.T) {
	pe := NewPolicyEngine()
	args := map[string]interface{}{"command": "sudo rm -rf /"}
	action := pe.Evaluate("Bash", args, RiskCritical)
	if action != PolicyDeny {
		t.Errorf("expected deny for critical risk with dangerous args, got %s", action)
	}
}

func TestEvaluate_CriticalRisk_WithoutDangerousArgs_Ask(t *testing.T) {
	pe := NewPolicyEngine()
	args := map[string]interface{}{"path": "/etc/passwd"}
	action := pe.Evaluate("WriteFile", args, RiskCritical)
	if action != PolicyAsk {
		t.Errorf("expected ask for critical risk without dangerous args, got %s", action)
	}
}

// ---------------------------------------------------------------------------
// Evaluate — glob tool pattern matching
// ---------------------------------------------------------------------------

func TestEvaluate_ToolPatternGlob(t *testing.T) {
	pe := &PolicyEngine{
		rules: []PolicyRule{
			{
				Name:        "deny-bash-tools",
				Priority:    1,
				ToolPattern: "Bash*",
				ArgsPattern: "",
				RiskLevels:  nil, // matches any risk level
				Action:      PolicyDeny,
			},
		},
	}

	if action := pe.Evaluate("BashExec", nil, RiskLow); action != PolicyDeny {
		t.Errorf("expected deny for BashExec matching Bash*, got %s", action)
	}
	// Non-matching tool falls through to default (ask).
	if action := pe.Evaluate("ReadFile", nil, RiskLow); action != PolicyAsk {
		t.Errorf("expected ask for ReadFile (no match), got %s", action)
	}
}

// ---------------------------------------------------------------------------
// Evaluate — regex args pattern matching
// ---------------------------------------------------------------------------

func TestEvaluate_ArgsPatternRegex(t *testing.T) {
	pe := &PolicyEngine{
		rules: []PolicyRule{
			{
				Name:        "deny-drop-table",
				Priority:    1,
				ToolPattern: "*",
				ArgsPattern: "(?i)DROP\\s+TABLE",
				RiskLevels:  nil,
				Action:      PolicyDeny,
			},
		},
	}

	args := map[string]interface{}{"sql": "DROP TABLE users"}
	if action := pe.Evaluate("SQLExec", args, RiskHigh); action != PolicyDeny {
		t.Errorf("expected deny for DROP TABLE args, got %s", action)
	}

	safeArgs := map[string]interface{}{"sql": "SELECT * FROM users"}
	if action := pe.Evaluate("SQLExec", safeArgs, RiskHigh); action != PolicyAsk {
		t.Errorf("expected ask for safe args (no match), got %s", action)
	}
}

// ---------------------------------------------------------------------------
// Evaluate — priority ordering (first match wins)
// ---------------------------------------------------------------------------

func TestEvaluate_PriorityOrdering(t *testing.T) {
	pe := &PolicyEngine{
		rules: []PolicyRule{
			{
				Name:        "low-priority-allow",
				Priority:    100,
				ToolPattern: "*",
				RiskLevels:  []RiskLevel{RiskHigh},
				Action:      PolicyAllow,
			},
			{
				Name:        "high-priority-deny",
				Priority:    1,
				ToolPattern: "*",
				RiskLevels:  []RiskLevel{RiskHigh},
				Action:      PolicyDeny,
			},
		},
	}
	// Rules should be evaluated in priority order after sorting.
	// But we intentionally inserted them out of order — Evaluate walks the
	// slice as-is, so the caller (or LoadRules/DefaultPolicyRules) must sort.
	// Let's sort and re-check.
	sortRulesByPriority(pe.rules)

	action := pe.Evaluate("Bash", nil, RiskHigh)
	if action != PolicyDeny {
		t.Errorf("expected deny (higher priority), got %s", action)
	}
}

// ---------------------------------------------------------------------------
// LoadRules
// ---------------------------------------------------------------------------

func TestLoadRules_FromFile(t *testing.T) {
	rules := []PolicyRule{
		{Name: "custom-allow", Priority: 5, ToolPattern: "Read*", Action: PolicyAllow, RiskLevels: []RiskLevel{RiskLow}},
		{Name: "custom-deny", Priority: 10, ToolPattern: "*", Action: PolicyDeny, RiskLevels: []RiskLevel{RiskCritical}},
	}
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	pe := NewPolicyEngine()
	if err := pe.LoadRules(path); err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}

	loaded := pe.Rules()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(loaded))
	}
	if loaded[0].Name != "custom-allow" {
		t.Errorf("expected first rule 'custom-allow', got %q", loaded[0].Name)
	}
}

func TestLoadRules_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	pe := NewPolicyEngine()
	if err := pe.LoadRules(path); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLoadRules_FileNotFound(t *testing.T) {
	pe := NewPolicyEngine()
	if err := pe.LoadRules("/nonexistent/path/rules.json"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestEvaluate_ConcurrentAccess(t *testing.T) {
	pe := NewPolicyEngine()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pe.Evaluate("Bash", map[string]interface{}{"cmd": "ls"}, RiskMedium)
		}()
	}

	// Concurrent LoadRules while Evaluate is running.
	rules := []PolicyRule{
		{Name: "concurrent-rule", Priority: 1, ToolPattern: "*", Action: PolicyAllow},
	}
	data, _ := json.Marshal(rules)
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.json")
	_ = os.WriteFile(path, data, 0644)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = pe.LoadRules(path)
	}()

	wg.Wait()
}
