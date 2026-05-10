package workflow

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func testToolDef(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": name,
		},
	}
}

func TestFilterToolDefinitionsOpsControlledIsDenyByDefault(t *testing.T) {
	tools := []map[string]interface{}{
		testToolDef("ssh"),
		testToolDef("bash"),
		testToolDef("read_file"),
		testToolDef("task"),
		testToolDef("create_session"),
		testToolDef("edit_file"),
		testToolDef("unknown"),
	}

	filtered := FilterToolDefinitions(ToolFilterOpsControlled, tools)
	names := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		names[tooldef.Name(tool)] = true
	}

	for _, allowed := range []string{"ssh", "bash", "read_file"} {
		if !names[allowed] {
			t.Fatalf("expected %s to remain allowed; got %#v", allowed, names)
		}
	}
	for _, blocked := range []string{"task", "create_session", "edit_file", "unknown"} {
		if names[blocked] {
			t.Fatalf("expected %s to be blocked; got %#v", blocked, names)
		}
		if IsToolAllowedByPolicy(ToolFilterOpsControlled, blocked) {
			t.Fatalf("expected %s to be denied by execution policy", blocked)
		}
	}
}

func TestFilterToolDefinitionsDocOnlyCanReturnEmpty(t *testing.T) {
	filtered := FilterToolDefinitions(ToolFilterDocOnly, []map[string]interface{}{
		testToolDef("task"),
		testToolDef("create_session"),
	})
	if len(filtered) != 0 {
		t.Fatalf("expected doc-only policy to return empty allowed set, got %#v", filtered)
	}
}

func TestIsToolAllowedByPolicyTrimsToolName(t *testing.T) {
	if !IsToolAllowedByPolicy(ToolFilterOpsControlled, " bash ") {
		t.Fatal("expected tool policy to trim tool names before checking allowlist")
	}
	if IsToolAllowedByPolicy(ToolFilterOpsControlled, " task ") {
		t.Fatal("expected trimmed blocked tool to remain blocked")
	}
}
