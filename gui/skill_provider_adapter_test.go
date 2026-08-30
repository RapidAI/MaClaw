package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillExecutorProviderListActiveSkillsExcludesNonExecutableEntries(t *testing.T) {
	app := &App{}
	app.publishNLSkillsLocked([]corelib.NLSkillEntry{
		{Name: "paper-translator", Status: "active", Type: "instruction", Description: "MaClaw App container"},
		{Name: "knowledge-guide", Status: "active", Type: "knowledge", Description: "Reference material"},
		{Name: "translate-document", Status: "active", Description: "Translate a document", Triggers: []string{"translate"}},
		{Name: "empty-skill", Status: "active"},
		{Name: "auto-discovered-staged", Status: "staged", Source: "auto_discovered", Description: "Pending approval"},
	})

	provider := &skillExecutorProvider{executor: &SkillExecutor{app: app}}
	active := provider.ListActiveSkills()
	names := make(map[string]bool, len(active))
	for _, entry := range active {
		names[entry.Name] = true
	}
	if !names["translate-document"] {
		t.Fatalf("runnable skill missing from ListActiveSkills(): %#v", active)
	}
	for _, name := range []string{"paper-translator", "knowledge-guide", "auto-discovered-staged", "empty-skill"} {
		if names[name] {
			t.Fatalf("non-executable entry %q leaked into ListActiveSkills(): %#v", name, active)
		}
	}
}

func TestSkillExecutorProviderExcludesAgentGuidedWorkflow(t *testing.T) {
	app := &App{}
	app.publishNLSkillsLocked([]corelib.NLSkillEntry{
		{Name: "Book-PDF", Source: "clawhub", Status: "active", Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "阶段1 调研，启动多个background agent并行调研；与用户确认大纲；多Agent并行写作；从 templates/ 复制项目骨架；维护 version.json。"},
		}}},
	})
	provider := &skillExecutorProvider{executor: &SkillExecutor{app: app}}
	for _, entry := range provider.ListActiveSkills() {
		if entry.Name == "Book-PDF" {
			t.Fatalf("agent-guided workflow leaked into active routing: %#v", entry)
		}
	}
}
