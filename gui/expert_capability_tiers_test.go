package main

import (
	"encoding/json"
	"testing"
)

func TestResolveExpertCapabilityTier_Rules(t *testing.T) {
	tools := []string{
		"memory", "ask_user", "read_file", "write_file", "web_search",
		"ssh", "bash", "screenshot", "send_file", "task", "tts", "knowledge_search",
	}
	skills := []string{"pptx-gen", "pdf-word", "sheet-analysis", "contract-review", "craft_task_abc", "Catfee Ssh"}

	advisorTools, advisorSkills := resolveExpertCapabilityTier(expertTierAdvisor, tools, skills)
	if !containsStr(advisorTools, "memory") || !containsStr(advisorTools, "tts") {
		t.Fatalf("advisor tools = %v", advisorTools)
	}
	if containsStr(advisorTools, "ssh") || containsStr(advisorTools, "write_file") || containsStr(advisorTools, "screenshot") {
		t.Fatalf("advisor should exclude elevated/dangerous: %v", advisorTools)
	}
	if len(advisorSkills) != 0 {
		t.Fatalf("advisor skills want empty, got %v", advisorSkills)
	}

	docsTools, docsSkills := resolveExpertCapabilityTier(expertTierDocs, tools, skills)
	if !containsStr(docsTools, "write_file") || !containsStr(docsTools, "knowledge_search") {
		t.Fatalf("docs tools = %v", docsTools)
	}
	if containsStr(docsTools, "ssh") || containsStr(docsTools, "task") {
		t.Fatalf("docs should exclude system/automation: %v", docsTools)
	}
	if !containsStr(docsSkills, "pdf-word") || containsStr(docsSkills, "pptx-gen") || containsStr(docsSkills, "Catfee Ssh") {
		t.Fatalf("docs skills = %v", docsSkills)
	}

	officeTools, officeSkills := resolveExpertCapabilityTier(expertTierOffice, tools, skills)
	if !containsStr(officeTools, "screenshot") || !containsStr(officeTools, "task") {
		t.Fatalf("office tools = %v", officeTools)
	}
	if containsStr(officeTools, "bash") {
		t.Fatalf("office must not include bash: %v", officeTools)
	}
	if !containsStr(officeSkills, "pptx-gen") || containsStr(officeSkills, "Catfee Ssh") {
		t.Fatalf("office skills = %v", officeSkills)
	}

	fullTools, fullSkills := resolveExpertCapabilityTier(expertTierFull, tools, skills)
	if len(fullTools) != 0 || len(fullSkills) != 0 {
		t.Fatalf("full must be empty allow-lists, got tools=%v skills=%v", fullTools, fullSkills)
	}
}

func TestInferExpertCapabilityTier(t *testing.T) {
	tools := []string{"memory", "ask_user", "read_file", "write_file", "web_search", "ssh", "tts"}
	skills := []string{"pdf-word", "pptx-gen"}
	if got := inferExpertCapabilityTier(nil, nil, tools, skills); got != expertTierFull {
		t.Fatalf("empty → full, got %s", got)
	}
	docsTools, docsSkills := resolveExpertCapabilityTier(expertTierDocs, tools, skills)
	if got := inferExpertCapabilityTier(docsTools, docsSkills, tools, skills); got != expertTierDocs {
		t.Fatalf("docs set → docs, got %s", got)
	}
	if got := inferExpertCapabilityTier([]string{"ssh"}, nil, tools, skills); got != expertTierCustom {
		t.Fatalf("ssh only → custom, got %s", got)
	}
}

func TestLookupExpertSkillMeta(t *testing.T) {
	cat, risk := lookupExpertSkillMeta("pptx-gen")
	if cat != "office" || risk != "elevated" {
		t.Fatalf("pptx-gen → %s/%s", cat, risk)
	}
	cat, risk = lookupExpertSkillMeta("Catfee Ssh")
	if cat != "security" || risk != "dangerous" {
		t.Fatalf("ssh skill → %s/%s", cat, risk)
	}
}

func TestResolveExpertCapabilityTier_JSONShape(t *testing.T) {
	// Pure marshal shape used by the Wails binding.
	tools, skills := resolveExpertCapabilityTier(expertTierAdvisor, []string{"memory", "ssh"}, nil)
	raw, err := json.Marshal(expertCapabilityTierResult{Tier: expertTierAdvisor, Tools: tools, Skills: skills})
	if err != nil {
		t.Fatal(err)
	}
	var got expertCapabilityTierResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Tier != expertTierAdvisor || !containsStr(got.Tools, "memory") || containsStr(got.Tools, "ssh") {
		t.Fatalf("payload = %#v", got)
	}
	if got.Skills == nil {
		// Marshal should keep skills as [] not null when we set empty slice.
		t.Fatalf("skills should be non-nil empty slice in result construction")
	}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
