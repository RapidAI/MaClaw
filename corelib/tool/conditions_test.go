package tool

import "testing"

func TestEvaluateToolConditions_NoConditions(t *testing.T) {
	// Skills with no conditions should always return true (backward compatible).
	cond := ToolConditions{}
	available := map[string]bool{"ssh": true, "bash": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true for skill with no conditions")
	}
}

func TestEvaluateToolConditions_RequiresTools_AllPresent(t *testing.T) {
	cond := ToolConditions{RequiresTools: []string{"ssh", "bash"}}
	available := map[string]bool{"ssh": true, "bash": true, "web_search": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when all required tools are available")
	}
}

func TestEvaluateToolConditions_RequiresTools_OneMissing(t *testing.T) {
	cond := ToolConditions{RequiresTools: []string{"ssh", "docker"}}
	available := map[string]bool{"ssh": true, "bash": true}
	if EvaluateToolConditions(cond, available) {
		t.Error("expected false when a required tool is missing")
	}
}

func TestEvaluateToolConditions_FallbackForTools_AllUnavailable(t *testing.T) {
	cond := ToolConditions{FallbackForTools: []string{"docker", "kubectl"}}
	available := map[string]bool{"ssh": true, "bash": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when all fallback_for tools are unavailable")
	}
}

func TestEvaluateToolConditions_FallbackForTools_OnePresent(t *testing.T) {
	cond := ToolConditions{FallbackForTools: []string{"docker", "kubectl"}}
	available := map[string]bool{"docker": true, "bash": true}
	if EvaluateToolConditions(cond, available) {
		t.Error("expected false when a fallback_for tool is available")
	}
}

func TestEvaluateToolConditions_RequiresToolsets_Available(t *testing.T) {
	// "ssh" toolset contains just ["ssh"].
	cond := ToolConditions{RequiresToolsets: []string{"ssh"}}
	available := map[string]bool{"ssh": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when required toolset is fully available")
	}
}

func TestEvaluateToolConditions_RequiresToolsets_PartiallyAvailable(t *testing.T) {
	// "coding" toolset requires multiple tools.
	cond := ToolConditions{RequiresToolsets: []string{"coding"}}
	// Only provide some of the coding tools.
	available := map[string]bool{"create_session": true, "send_and_observe": true}
	if EvaluateToolConditions(cond, available) {
		t.Error("expected false when required toolset is only partially available")
	}
}

func TestEvaluateToolConditions_RequiresToolsets_UnknownToolset(t *testing.T) {
	cond := ToolConditions{RequiresToolsets: []string{"nonexistent_toolset"}}
	available := map[string]bool{"ssh": true}
	if EvaluateToolConditions(cond, available) {
		t.Error("expected false for unknown required toolset")
	}
}

func TestEvaluateToolConditions_FallbackForToolsets_Unavailable(t *testing.T) {
	// "coding" toolset is not fully available → fallback activates.
	cond := ToolConditions{FallbackForToolsets: []string{"coding"}}
	available := map[string]bool{"ssh": true, "bash": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when fallback_for toolset is not fully available")
	}
}

func TestEvaluateToolConditions_FallbackForToolsets_FullyAvailable(t *testing.T) {
	// "ssh" toolset is fully available → fallback should NOT activate.
	cond := ToolConditions{FallbackForToolsets: []string{"ssh"}}
	available := map[string]bool{"ssh": true, "bash": true}
	if EvaluateToolConditions(cond, available) {
		t.Error("expected false when fallback_for toolset is fully available")
	}
}

func TestEvaluateToolConditions_FallbackForToolsets_UnknownToolset(t *testing.T) {
	// Unknown toolset → treated as unavailable → fallback condition satisfied.
	cond := ToolConditions{FallbackForToolsets: []string{"nonexistent_toolset"}}
	available := map[string]bool{"ssh": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true for unknown fallback_for toolset (treated as unavailable)")
	}
}

func TestEvaluateToolConditions_CombinedANDLogic(t *testing.T) {
	// Skill requires ssh AND is a fallback for docker.
	cond := ToolConditions{
		RequiresTools:    []string{"ssh"},
		FallbackForTools: []string{"docker"},
	}

	// ssh available, docker unavailable → both conditions met.
	available := map[string]bool{"ssh": true, "bash": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when both requires and fallback conditions are met")
	}

	// ssh available, docker also available → fallback condition fails.
	available2 := map[string]bool{"ssh": true, "docker": true}
	if EvaluateToolConditions(cond, available2) {
		t.Error("expected false when fallback_for tool is available")
	}

	// ssh unavailable, docker unavailable → requires condition fails.
	available3 := map[string]bool{"bash": true}
	if EvaluateToolConditions(cond, available3) {
		t.Error("expected false when required tool is missing")
	}
}

func TestEvaluateToolConditions_CombinedToolsAndToolsets(t *testing.T) {
	// Requires ssh tool AND is a fallback for the coding toolset.
	cond := ToolConditions{
		RequiresTools:       []string{"ssh"},
		FallbackForToolsets: []string{"coding"},
	}

	// ssh available, coding toolset not fully available → both conditions met.
	available := map[string]bool{"ssh": true, "create_session": true}
	if !EvaluateToolConditions(cond, available) {
		t.Error("expected true when requires_tools met and fallback_for_toolsets met")
	}

	// ssh available, coding toolset fully available → fallback condition fails.
	fullCoding := map[string]bool{"ssh": true}
	for _, t := range ToolsetGroups["coding"] {
		fullCoding[t] = true
	}
	if EvaluateToolConditions(cond, fullCoding) {
		t.Error("expected false when fallback_for_toolsets is fully available")
	}
}

func TestEvaluateToolConditionsForSkill(t *testing.T) {
	available := map[string]bool{"ssh": true}
	if !EvaluateToolConditionsForSkill([]string{"ssh"}, nil, nil, nil, available) {
		t.Error("expected true via convenience wrapper")
	}
	if EvaluateToolConditionsForSkill([]string{"docker"}, nil, nil, nil, available) {
		t.Error("expected false via convenience wrapper when tool missing")
	}
}
