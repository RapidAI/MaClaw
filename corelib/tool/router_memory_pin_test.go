package tool

import (
	"fmt"
	"testing"
)

func TestMatchConditionalTools_MemoryBrowserNotPinned(t *testing.T) {
	memoryText := "Previous server check mentioned a Chrome browser process using CPU."
	matched := MatchConditionalTools(memoryText)

	if matched["browser"] {
		t.Fatalf("MatchConditionalTools(%q) should not include browser", memoryText)
	}
}

func TestMatchConditionalTools_MemorySSHNotPinned(t *testing.T) {
	memoryText := "Previous task connected to api.rapidai.tech and checked Docker containers."
	matched := MatchConditionalTools(memoryText)

	if matched["ssh"] {
		t.Fatalf("MatchConditionalTools(%q) should not include ssh", memoryText)
	}
}

func TestMatchConditionalTools_BrowserLikeMemoryNotPinned(t *testing.T) {
	memoryText := "User wanted to build a web game whose page opens directly."
	matched := MatchConditionalTools(memoryText)

	if matched["browser"] {
		t.Fatalf("MatchConditionalTools(%q) should not include browser", memoryText)
	}
}

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

func TestMatchConditionalTools_DesktopGUINotPinned(t *testing.T) {
	tests := []struct {
		name       string
		memoryText string
	}{
		{name: "window memory", memoryText: "Previous task observed a desktop window and typed text."},
		{name: "desktop memory", memoryText: "User asked to inspect desktop app elements."},
		{name: "gui tool memory", memoryText: "Tool usage mentioned gui_observe output."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := MatchConditionalTools(tt.memoryText)
			if matched["gui_observe"] || matched["gui_verify"] {
				t.Fatalf("MatchConditionalTools(%q) should not include GUI observe/verify tools", tt.memoryText)
			}
		})
	}
}

func TestMatchConditionalTools_NoEagerPinToolsFiltered(t *testing.T) {
	matched := MatchConditionalTools("browser automation, gui recording, and remote server notes")
	for name := range matched {
		if noEagerPinTools[name] {
			t.Fatalf("MatchConditionalTools returned %q, which is in noEagerPinTools", name)
		}
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

	for _, name := range []string{"browser", "gui_record_start"} {
		if !noEagerPinTools[name] {
			t.Fatalf("expected %q to be in noEagerPinTools", name)
		}
	}
	for _, name := range []string{"gui_observe", "gui_verify", "ssh"} {
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

	matched := MatchConditionalTools("GUI agent, web agent, browser agent, computer use")
	if matched["gui_observe"] || matched["gui_verify"] {
		t.Fatalf("MatchConditionalTools should not return gui_observe/gui_verify")
	}
}
