package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func assertManagedModelName(t *testing.T, name string, definition map[string]interface{}, selection tool.PlannedSelection, want string, siblings ...string) {
	t.Helper()
	if name != want || definition["name"] != want {
		t.Fatalf("managed name=%q, want %q", name, want)
	}
	if selection.AdapterName == want {
		t.Fatalf("adapter leaked prompt name %q", selection.AdapterName)
	}
	for _, leaked := range siblings {
		if name == leaked || definition["name"] == leaked || selection.AdapterName == leaked {
			t.Fatalf("managed surface leaked %q name=%q selection=%+v", leaked, name, selection)
		}
	}
}
