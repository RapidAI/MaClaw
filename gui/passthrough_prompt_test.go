package main

import (
	"strings"
	"testing"
)

func TestPassthroughSystemPromptMentionsManagementTool(t *testing.T) {
	h := &IMMessageHandler{}
	prompt := h.buildSystemPromptBase(false)
	for _, want := range []string{"passthrough_task", "action=list/status/show/export/preview/save/delete/set_enabled/audit", "params_json", "/runctl save --params-json", "/runctl preview <name>", "/runctl status"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}
