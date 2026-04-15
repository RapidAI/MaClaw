package main

import "testing"

// TestDeferredToolNames_ContainsLegacySkillNames verifies that all 5 legacy
// skill tool names are present in DeferredToolNames so they remain discoverable
// via discover_tool but don't appear in the initial prompt.
//
// **Validates: Requirements 7.2, 7.3**
func TestDeferredToolNames_ContainsLegacySkillNames(t *testing.T) {
	legacyNames := []string{
		"list_skills",
		"search_skill_hub",
		"install_skill_hub",
		"run_skill",
		"get_skill_run",
	}

	deferredSet := make(map[string]bool, len(DeferredToolNames))
	for _, name := range DeferredToolNames {
		deferredSet[name] = true
	}

	for _, name := range legacyNames {
		if !deferredSet[name] {
			t.Errorf("DeferredToolNames should contain legacy skill name %q", name)
		}
	}
}
