package tool

import (
	"testing"
	"time"
)

// TestCoreToolNames_ExcludesDynamicManageSkillGateway verifies that a merged
// Skill transport cannot be reintroduced as a legacy routing candidate.
func TestCoreToolNames_ExcludesDynamicManageSkillGateway(t *testing.T) {
	if CoreToolNames["manage_skill"] {
		t.Fatal("CoreToolNames must not contain dynamic manage_skill gateway")
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

// TestBuiltinToolNames_ContainsManageSkill preserves host registry
// classification. Model visibility is controlled separately by
// IsLegacyModelDynamicGateway.
func TestBuiltinToolNames_ContainsManageSkill(t *testing.T) {
	if !BuiltinToolNames["manage_skill"] {
		t.Fatal("BuiltinToolNames should contain manage_skill")
	}
}

func TestManageSkillIsNotLegacyModelCapability(t *testing.T) {
	if !IsLegacyModelDynamicGateway("manage_skill") {
		t.Fatal("manage_skill must require a managed dynamic binding")
	}
	if _, ok := LegacyAdapterProvisionForTool("manage_skill", time.Now()); ok {
		t.Fatal("manage_skill must not have a static legacy adapter provision")
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
