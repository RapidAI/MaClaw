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

func TestDocOnlyPolicyBlocksExecutionAndMutationTools(t *testing.T) {
	for _, name := range []string{"read_file", "list_directory", "send_file"} {
		if !IsToolAllowedByPolicy(ToolFilterDocOnly, name) {
			t.Fatalf("expected %s to be allowed by doc-only workflow policy", name)
		}
	}
	for _, name := range []string{"bash", "ssh", "write_file", "edit_file", "edit_lines", "async_wait", "task", "delegate_task", "browser"} {
		if IsToolAllowedByPolicy(ToolFilterDocOnly, name) {
			t.Fatalf("expected %s to be blocked by doc-only workflow policy", name)
		}
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, name, map[string]interface{}{"path": "out.md", "command": "true"}); err == nil {
			t.Fatalf("expected %s execution to be rejected by doc-only workflow policy", name)
		}
	}

	required := RequiredToolNamesForPolicy(ToolFilterDocOnly)
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}
	for _, name := range []string{"read_file", "list_directory", "send_file"} {
		if !requiredSet[name] {
			t.Fatalf("expected %s to be a required doc-only workflow tool; got %#v", name, required)
		}
	}
	for _, name := range []string{"write_file", "edit_file", "edit_lines", "bash", "ssh", "async_wait"} {
		if requiredSet[name] {
			t.Fatalf("expected %s to be absent from required doc-only workflow tools; got %#v", name, required)
		}
	}
}

func TestRequiredToolNamesForPolicyReturnsCopy(t *testing.T) {
	first := RequiredToolNamesForPolicy(ToolFilterDocOnly)
	if len(first) == 0 {
		t.Fatal("expected doc-only policy to declare required tools")
	}
	first[0] = "mutated"
	second := RequiredToolNamesForPolicy(ToolFilterDocOnly)
	if second[0] == "mutated" {
		t.Fatal("RequiredToolNamesForPolicy must return a copy")
	}
}

func TestRequiredToolNamesForPolicyAreAllowed(t *testing.T) {
	for _, policy := range []ToolFilterPolicy{ToolFilterDocOnly, ToolFilterOpsControlled} {
		for _, name := range RequiredToolNamesForPolicy(policy) {
			if !IsToolAllowedByPolicy(policy, name) {
				t.Fatalf("required tool %s must be allowed by policy %s", name, policy)
			}
		}
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
