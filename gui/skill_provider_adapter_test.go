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
	for _, name := range []string{"paper-translator", "knowledge-guide"} {
		if names[name] {
			t.Fatalf("non-executable entry %q leaked into ListActiveSkills(): %#v", name, active)
		}
	}
}
