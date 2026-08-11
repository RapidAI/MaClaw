package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestTUIReadOnlyChildToolBoundaryFailsClosed(t *testing.T) {
	defs := []map[string]interface{}{
		agent.ToolDef("read_file", "read", nil, nil),
		agent.ToolDef("list_directory", "list", nil, nil),
		agent.ToolDef("web_search", "search", nil, nil),
		agent.ToolDef("web_fetch", "fetch", nil, nil),
		agent.ToolDef("bash", "bash", nil, nil),
		agent.ToolDef("ssh", "ssh", nil, nil),
		agent.ToolDef("write_file", "write", nil, nil),
		agent.ToolDef("edit_file", "edit", nil, nil),
		agent.ToolDef("manage_skill", "skill", nil, nil),
		agent.ToolDef("task", "task", nil, nil),
	}
	seen := map[string]bool{}
	for _, def := range tuiFilterReadOnlyChildToolDefinitions(defs) {
		seen[tooldef.Name(def)] = true
	}
	for _, allowed := range []string{"read_file", "list_directory", "web_search", "web_fetch"} {
		if !seen[allowed] || !tuiReadOnlyChildToolAllowed(allowed) {
			t.Fatalf("read-only child must expose %q: %#v", allowed, seen)
		}
	}
	for _, denied := range []string{"bash", "ssh", "write_file", "edit_file", "manage_skill", "memory", "task", "spawn_coding_agent"} {
		if seen[denied] || tuiReadOnlyChildToolAllowed(denied) {
			t.Fatalf("read-only child must deny %q: %#v", denied, seen)
		}
	}
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if ok, _ := tuiReadOnlyChildToolCallAllowed("web_fetch", map[string]interface{}{"url": "https://example.com/a.txt", key: "a.txt"}); ok {
			t.Fatalf("read-only child must deny web_fetch %s when it would write to disk", key)
		}
	}
}

func TestParseTUIReadOnlyChildSpawnRejectsWorkersAndBoundsFanout(t *testing.T) {
	if _, err := parseTUIReadOnlyChildSpawn(`{"role":"worker","task":"write code"}`); err == nil {
		t.Fatal("worker child must be rejected")
	}
	if _, err := parseTUIReadOnlyChildSpawn(`{"agents":[{"task":"a"},{"task":"b"},{"task":"c"},{"task":"d"}]}`); err == nil {
		t.Fatal("fanout above maximum must be rejected")
	}
	specs, err := parseTUIReadOnlyChildSpawn(`{"agents":[{"role":"explorer","task":"map"},{"role":"reviewer","task":"review"}]}`)
	if err != nil || len(specs) != 2 || specs[0].Role != "explorer" || specs[1].Role != "reviewer" {
		t.Fatalf("valid child specs = %#v err=%v", specs, err)
	}
	if got := tuiReadOnlyChildSystemPrompt("inspect"); !strings.Contains(got, "do not edit files") {
		t.Fatalf("read-only prompt lacks mutation boundary: %q", got)
	}
}
