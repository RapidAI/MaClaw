package tool

import (
	"fmt"
	"testing"
)

// Recalled memory text is routed like any other wording; see
// TestRouterLocalWordingNeverActivatesConditionalTools for those cases.

func TestRoute_SemanticIntentEnhancement_NoBrowserActivation(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("browser", "browser automation tool"))
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("Check remote server resource usage.", tools)
	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if resultNames["browser"] {
		t.Fatalf("Route should not include browser without semantic browser intent")
	}
}

func TestNoEagerPinTools_DerivedFromRules(t *testing.T) {
	expected := make(map[string]bool)
	for _, rule := range conditionalKeepRules {
		if rule.noMemoryPin {
			for _, name := range rule.keepTools {
				expected[name] = true
			}
		}
	}

	if len(noEagerPinTools) != len(expected) {
		t.Fatalf("noEagerPinTools has %d entries, want %d", len(noEagerPinTools), len(expected))
	}
	for name := range expected {
		if !noEagerPinTools[name] {
			t.Fatalf("%q is in a noMemoryPin rule but not in noEagerPinTools", name)
		}
	}
	for name := range noEagerPinTools {
		if !expected[name] {
			t.Fatalf("%q is in noEagerPinTools but not in a noMemoryPin rule", name)
		}
	}

	for _, name := range []string{"browser"} {
		if !noEagerPinTools[name] {
			t.Fatalf("expected %q to be in noEagerPinTools", name)
		}
	}
	for _, name := range []string{"gui_record_start", "gui_observe", "gui_verify", "ssh"} {
		if noEagerPinTools[name] {
			t.Fatalf("%q should not be in noEagerPinTools", name)
		}
	}
}

func TestDesktopGUITools_NotConditional(t *testing.T) {
	for _, name := range []string{"gui_observe", "gui_verify"} {
		if allConditionalKeepTools[name] {
			t.Fatalf("%q should not be in allConditionalKeepTools", name)
		}
		if noEagerPinTools[name] {
			t.Fatalf("%q should not be in noEagerPinTools", name)
		}
	}
}
