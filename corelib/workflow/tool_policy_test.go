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
		testToolDef("write_file"),
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
	for _, blocked := range []string{"task", "write_file", "edit_file", "unknown"} {
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
		testToolDef("bash"),
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

func TestPlanningPolicyAllowsInspectionButBlocksImplementationTools(t *testing.T) {
	for _, name := range []string{"bash", "read_file", "list_directory", "send_file", "web_search", "web_fetch"} {
		if !IsToolAllowedByPolicy(ToolFilterPlanning, name) {
			t.Fatalf("expected %s to be allowed by planning workflow policy", name)
		}
	}
	for _, name := range []string{"write_file", "edit_file", "edit_lines", "task", "delegate_task", "ssh", "async_wait", "browser"} {
		if IsToolAllowedByPolicy(ToolFilterPlanning, name) {
			t.Fatalf("expected %s to be blocked by planning workflow policy", name)
		}
		if err := ValidateToolCallByPolicy(ToolFilterPlanning, name, map[string]interface{}{"path": "src/main.go", "command": "true"}); err == nil {
			t.Fatalf("expected %s execution to be rejected by planning workflow policy", name)
		}
	}

	required := RequiredToolNamesForPolicy(ToolFilterPlanning)
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}
	for _, name := range []string{"bash", "read_file", "list_directory", "send_file"} {
		if !requiredSet[name] {
			t.Fatalf("expected %s to be a required planning workflow tool; got %#v", name, required)
		}
	}
	for _, name := range []string{"write_file", "edit_file", "edit_lines", "task", "delegate_task", "ssh"} {
		if requiredSet[name] {
			t.Fatalf("expected %s to be absent from required planning workflow tools; got %#v", name, required)
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
	for _, policy := range []ToolFilterPolicy{ToolFilterDocOnly, ToolFilterPlanning, ToolFilterOpsControlled, ToolFilterFull} {
		for _, name := range RequiredToolNamesForPolicy(policy) {
			if !IsToolAllowedByPolicy(policy, name) {
				t.Fatalf("required tool %s must be allowed by policy %s", name, policy)
			}
		}
	}
}

func TestRequiredToolNamesForFullPolicyKeepsLocalCodingTools(t *testing.T) {
	required := RequiredToolNamesForPolicy(ToolFilterFull)
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}
	for _, name := range []string{"bash", "read_file", "list_directory", "write_file", "edit_file"} {
		if !requiredSet[name] {
			t.Fatalf("expected %s to be pinned for full workflow execution; got %#v", name, required)
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

func TestArtifactMutationScopeAllowsArtifactsButBlocksProjectMutation(t *testing.T) {
	contract := PhaseContract{
		ToolPolicy:    ToolFilterFull,
		MutationScope: MutationScopeArtifact,
	}
	for _, name := range []string{"write_file", "send_file", "office", "generate_pdf", "read_file", "list_directory"} {
		if !IsToolAllowedByContract(contract, name) {
			t.Fatalf("expected %s to be exposed for artifact generation", name)
		}
	}
	for _, name := range []string{"edit_file", "edit_lines", "task", "delegate_task", "ssh"} {
		if IsToolAllowedByContract(contract, name) {
			t.Fatalf("expected %s to be hidden by artifact mutation scope", name)
		}
		if err := ValidateToolCallByContract(contract, name, map[string]interface{}{"path": "src/main.go"}); err == nil {
			t.Fatalf("expected %s call to be rejected by artifact mutation scope", name)
		}
	}
	for _, path := range []string{"business-plan.md", "deck.pptx", "report.pdf", "data.xlsx"} {
		if err := ValidateToolCallByContract(contract, "write_file", map[string]interface{}{"path": path, "content": "body"}); err != nil {
			t.Fatalf("expected artifact write %s to pass: %v", path, err)
		}
	}
	for _, path := range []string{"src/main.go", "CMakeLists.txt", "package.json", "app.tsx"} {
		if err := ValidateToolCallByContract(contract, "write_file", map[string]interface{}{"path": path, "content": "code"}); err == nil {
			t.Fatalf("expected project write %s to be rejected by artifact scope", path)
		}
	}
	if err := ValidateToolCallByContract(contract, "bash", map[string]interface{}{"command": "rg -n TODO"}); err != nil {
		t.Fatalf("expected read-only bash to pass under artifact scope: %v", err)
	}
	if err := ValidateToolCallByContract(contract, "bash", map[string]interface{}{"command": "mkdir -p src && touch src/main.go"}); err == nil {
		t.Fatal("expected mutating bash to be rejected by artifact scope")
	}
}

func TestWorkflowDocMutationScopeUsesSystemPersistenceOnly(t *testing.T) {
	contract := PhaseContractFromPolicy(ToolFilterPlanning, MutationScopeWorkflowDoc)
	if IsToolAllowedByContract(contract, "write_file") {
		t.Fatal("workflow_doc scope must not expose write_file")
	}
	if err := ValidateToolCallByContract(contract, "write_file", map[string]interface{}{"path": "task-plan.md"}); err == nil {
		t.Fatal("workflow_doc scope must reject direct write_file even for markdown")
	}
	if err := ValidateToolCallByContract(contract, "bash", map[string]interface{}{"command": "git status --short"}); err != nil {
		t.Fatalf("workflow_doc scope should allow read-only bash when policy allows it: %v", err)
	}
	if err := ValidateToolCallByContract(contract, "bash", map[string]interface{}{"command": "touch task-plan.md"}); err == nil {
		t.Fatal("workflow_doc scope must reject mutating bash")
	}
}

func TestRequiredToolNamesForArtifactContractDoesNotPinCodingTools(t *testing.T) {
	required := RequiredToolNamesForContract(PhaseContract{ToolPolicy: ToolFilterFull, MutationScope: MutationScopeArtifact})
	seen := make(map[string]bool, len(required))
	for _, name := range required {
		seen[name] = true
	}
	for _, name := range []string{"write_file", "send_file", "office", "generate_pdf"} {
		if !seen[name] {
			t.Fatalf("expected artifact required tool %s, got %#v", name, required)
		}
	}
	for _, name := range []string{"bash", "edit_file", "edit_lines"} {
		if seen[name] {
			t.Fatalf("artifact contract must not pin coding tool %s, got %#v", name, required)
		}
	}
}
