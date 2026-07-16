package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func TestComputerUseEnabledFromConfig(t *testing.T) {
	if !computerUseEnabledFromConfig(nil) {
		t.Fatal("nil cfg default true")
	}
	f := false
	if computerUseEnabledFromConfig(&corelib.AppConfig{ComputerUseEnabled: &f}) {
		t.Fatal("want false")
	}
	tr := true
	if !computerUseEnabledFromConfig(&corelib.AppConfig{ComputerUseEnabled: &tr}) {
		t.Fatal("want true")
	}
}

func TestEnsureComputerUseTools(t *testing.T) {
	all := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "computer_observe"}},
		{"type": "function", "function": map[string]interface{}{"name": "computer_click"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_click"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_type"}},
	}
	routed := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "gui_click"}},
	}
	out := ensureComputerUseTools(routed, all, true)
	names := map[string]bool{}
	for _, tdef := range out {
		names[extractToolName(tdef)] = true
	}
	if !names["computer_observe"] || !names["computer_click"] {
		t.Fatalf("missing CU tools: %v", names)
	}
	if names["gui_click"] || names["gui_type"] {
		t.Fatalf("legacy GUI tools should be demoted: %v", names)
	}
	if names["bash"] != true {
		t.Fatal("bash should remain")
	}
	// inactive: no change
	out2 := ensureComputerUseTools(routed, all, false)
	if len(out2) != len(routed) {
		t.Fatalf("inactive should pass through, got %d", len(out2))
	}
}

func TestComputerUsePlaybookSection(t *testing.T) {
	if computerUsePlaybookSection(false) != "" {
		t.Fatal("inactive empty")
	}
	s := computerUsePlaybookSection(true)
	if s == "" || !strings.Contains(s, "Computer Use") || !strings.Contains(s, "computer_observe") {
		t.Fatalf("playbook section incomplete: %q", s)
	}
	_ = computeruse.Playbook()
}
