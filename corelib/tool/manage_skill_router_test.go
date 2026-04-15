package tool

import "testing"

// TestCoreToolNames_ContainsManageSkill verifies that CoreToolNames includes
// "manage_skill" as a recognized core tool (always available in LLM context).
//
// **Validates: Requirements 5.5**
func TestCoreToolNames_ContainsManageSkill(t *testing.T) {
	if !CoreToolNames["manage_skill"] {
		t.Fatal("CoreToolNames should contain manage_skill")
	}
}

// TestCoreToolNames_DoesNotContainLegacySkillNames verifies that the legacy
// tool names "list_skills" and "run_skill" have been removed from CoreToolNames.
//
// **Validates: Requirements 5.5**
func TestCoreToolNames_DoesNotContainLegacySkillNames(t *testing.T) {
	if CoreToolNames["list_skills"] {
		t.Fatal("CoreToolNames should NOT contain list_skills (replaced by manage_skill)")
	}
	if CoreToolNames["run_skill"] {
		t.Fatal("CoreToolNames should NOT contain run_skill (replaced by manage_skill)")
	}
}

// TestBuiltinToolNames_ContainsManageSkill verifies that BuiltinToolNames
// includes "manage_skill".
//
// **Validates: Requirements 4.6, 5.4**
func TestBuiltinToolNames_ContainsManageSkill(t *testing.T) {
	if !BuiltinToolNames["manage_skill"] {
		t.Fatal("BuiltinToolNames should contain manage_skill")
	}
}

// TestBuiltinToolNames_ContainsAllLegacySkillNames verifies that all 5 legacy
// skill tool names remain in BuiltinToolNames for backward compatibility.
//
// **Validates: Requirements 4.6**
func TestBuiltinToolNames_ContainsAllLegacySkillNames(t *testing.T) {
	legacyNames := []string{
		"list_skills",
		"search_skill_hub",
		"install_skill_hub",
		"run_skill",
		"get_skill_run",
	}
	for _, name := range legacyNames {
		if !BuiltinToolNames[name] {
			t.Errorf("BuiltinToolNames should contain legacy name %q", name)
		}
	}
}
