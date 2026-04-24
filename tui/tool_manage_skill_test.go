package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// TestManageSkillHandler_AllCanonicalActionsHandled verifies that the TUI
// dispatcher has a handler for every action in the canonical ManageSkillActions
// list. If a new action is added to the single source of truth but not to the
// TUI switch, this test fails.
func TestManageSkillHandler_AllCanonicalActionsHandled(t *testing.T) {
	app := &TUIApp{
		appConfig: corelib.AppConfig{},
	}
	handler := newManageSkillHandler(app)

	for _, action := range skill.ManageSkillActionNames() {
		got := handler(map[string]interface{}{"action": action})
		if strings.Contains(got, "未知 manage_skill action") {
			t.Errorf("TUI dispatcher has no handler for canonical action %q", action)
		}
	}
}
