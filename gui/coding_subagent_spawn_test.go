package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestParseCodingSubAgentRole(t *testing.T) {
	cases := []struct {
		in   string
		want codingSubAgentRole
	}{
		{"", codingRoleWorker},
		{"worker", codingRoleWorker},
		{"implement", codingRoleWorker},
		{"explorer", codingRoleExplorer},
		{"explore", codingRoleExplorer},
		{"reviewer", codingRoleReviewer},
		{"review", codingRoleReviewer},
	}
	for _, tc := range cases {
		got, err := parseCodingSubAgentRole(tc.in)
		if err != nil {
			t.Fatalf("role %q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("role %q = %q want %q", tc.in, got, tc.want)
		}
	}
	if _, err := parseCodingSubAgentRole("wizard"); err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestParseCodingSpawnSpecsSingleAndParallel(t *testing.T) {
	specs, err := parseCodingSpawnSpecs(map[string]interface{}{
		"role": "explorer",
		"task": "map auth package",
	})
	if err != nil || len(specs) != 1 || specs[0].Role != codingRoleExplorer {
		t.Fatalf("single = %#v err=%v", specs, err)
	}

	specs, err = parseCodingSpawnSpecs(map[string]interface{}{
		"agents": []interface{}{
			map[string]interface{}{"role": "explorer", "task": "A"},
			map[string]interface{}{"role": "worker", "task": "B", "context": "use A"},
		},
	})
	if err != nil || len(specs) != 2 {
		t.Fatalf("parallel = %#v err=%v", specs, err)
	}
	if specs[1].Context != "use A" || specs[1].Role != codingRoleWorker {
		t.Fatalf("second agent = %#v", specs[1])
	}
	// Coerce non-string task/role from loose tool arg decoding.
	specs, err = parseCodingSpawnSpecs(map[string]interface{}{
		"role": "worker",
		"task": float64(42),
	})
	if err != nil || len(specs) != 1 || specs[0].Task != "42" {
		t.Fatalf("numeric task coerce = %#v err=%v", specs, err)
	}

	_, err = parseCodingSpawnSpecs(map[string]interface{}{
		"agents": []interface{}{
			map[string]interface{}{"task": "1"},
			map[string]interface{}{"task": "2"},
			map[string]interface{}{"task": "3"},
			map[string]interface{}{"task": "4"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "max parallel") {
		t.Fatalf("expected max parallel error, got %v", err)
	}
}

func TestShouldParallelizeCodingSpawn(t *testing.T) {
	if shouldParallelizeCodingSpawn([]codingSpawnSpec{{Role: codingRoleExplorer, Task: "a"}}) {
		t.Fatal("single agent should not parallelize")
	}
	if !shouldParallelizeCodingSpawn([]codingSpawnSpec{
		{Role: codingRoleExplorer, Task: "a"},
		{Role: codingRoleExplorer, Task: "b"},
	}) {
		t.Fatal("multi explorer should parallelize")
	}
	if shouldParallelizeCodingSpawn([]codingSpawnSpec{
		{Role: codingRoleExplorer, Task: "a"},
		{Role: codingRoleWorker, Task: "b"},
	}) {
		t.Fatal("mixed with worker must be sequential")
	}
	if shouldParallelizeCodingSpawn([]codingSpawnSpec{
		{Role: codingRoleReviewer, Task: "a"},
		{Role: codingRoleReviewer, Task: "b"},
	}) {
		t.Fatal("reviewers must be sequential")
	}
}

func TestCanSpawnCodingAgentDepthAndRole(t *testing.T) {
	root := &CodingSubAgent{fullEnvironment: true, nestDepth: 0}
	if !root.canSpawnCodingAgent() {
		t.Fatal("root full env should spawn")
	}
	child := &CodingSubAgent{fullEnvironment: true, nestDepth: 1, role: codingRoleWorker}
	if child.canSpawnCodingAgent() {
		t.Fatal("depth 1 must not spawn")
	}
	explorer := &CodingSubAgent{fullEnvironment: true, nestDepth: 0, role: codingRoleExplorer}
	if explorer.canSpawnCodingAgent() {
		t.Fatal("explorer root should not spawn")
	}
	nonFull := &CodingSubAgent{fullEnvironment: false, nestDepth: 0}
	if nonFull.canSpawnCodingAgent() {
		t.Fatal("non-full env should not spawn")
	}
}

func TestToolAllowedForRole(t *testing.T) {
	ex := &CodingSubAgent{role: codingRoleExplorer, nestDepth: 1}
	if !ex.toolAllowedForRole("read_file") || !ex.toolAllowedForRole("Glob") {
		t.Fatal("explorer should read/search")
	}
	if ex.toolAllowedForRole("write_file") || ex.toolAllowedForRole("edit_file") || ex.toolAllowedForRole("bash") {
		t.Fatal("explorer must not write or bash")
	}
	if ex.toolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("explorer must not spawn")
	}

	rev := &CodingSubAgent{role: codingRoleReviewer, nestDepth: 1}
	if !rev.toolAllowedForRole("bash") || !rev.toolAllowedForRole("git_diff") {
		t.Fatal("reviewer should bash/git_diff")
	}
	if rev.toolAllowedForRole("write_file") || rev.toolAllowedForRole("edit_file") {
		t.Fatal("reviewer must not write")
	}

	worker := &CodingSubAgent{fullEnvironment: true, nestDepth: 0, role: codingRoleWorker}
	if !worker.toolAllowedForRole("write_file") || !worker.toolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("root worker should write and spawn")
	}
	nestedWorker := &CodingSubAgent{fullEnvironment: true, nestDepth: 1, role: codingRoleWorker}
	if nestedWorker.toolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("nested worker must not spawn")
	}
}

func TestBuildToolsIncludesSpawnForFullEnvRoot(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir(), fullEnvironment: true, nestDepth: 0},
		task:     &TaskItem{Index: 1, Title: "T", Description: "do work"},
	}
	tools := cb.BuildTools("implement feature")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[codingSubAgentSpawnToolName] {
		t.Fatalf("full-env root BuildTools missing %s; got %#v", codingSubAgentSpawnToolName, names)
	}
	if !names["web_search"] || !names["read_file"] {
		t.Fatalf("expected baseline tools, got %#v", names)
	}
}

func TestBuildToolsExplorerFiltersWrites(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			projectPath:     t.TempDir(),
			fullEnvironment: false,
			nestDepth:       1,
			role:            codingRoleExplorer,
		},
		task: &TaskItem{Index: 1, Title: "Explore", Description: "map code"},
	}
	tools := cb.BuildTools("map auth")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if names["write_file"] || names["edit_file"] || names["edit_lines"] || names["bash"] {
		t.Fatalf("explorer tools leaked writes/bash: %#v", names)
	}
	if !names["read_file"] || !names["Glob"] {
		t.Fatalf("explorer missing read tools: %#v", names)
	}
	if names[codingSubAgentSpawnToolName] {
		t.Fatal("explorer must not expose spawn")
	}
}

func TestExecuteSpawnBlockedAtDepth(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{fullEnvironment: true, nestDepth: 1, role: codingRoleWorker},
	}
	res := cb.executeSpawnCodingAgent(map[string]interface{}{
		"role": "explorer",
		"task": "should fail",
	})
	if res.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("outcome=%v text=%s", res.Outcome, res.Text)
	}
	if !strings.Contains(res.Text, "unavailable") {
		t.Fatalf("text=%s", res.Text)
	}
}

func TestBuildFullEnvPreambleMentionsSpawn(t *testing.T) {
	preamble := buildFullCodingEnvironmentPromptPreamble()
	if !strings.Contains(preamble, codingSubAgentSpawnToolName) {
		t.Fatal("preamble should document spawn_coding_agent")
	}
	if !strings.Contains(preamble, "explorer") || !strings.Contains(preamble, "reviewer") {
		t.Fatal("preamble should list roles")
	}
	nested := buildNestedFullCodingEnvironmentPromptPreamble()
	if strings.Contains(nested, codingSubAgentSpawnToolName) {
		t.Fatal("nested worker preamble must not advertise spawn")
	}
}

func TestLocalInspectionRoleSystemPrompt(t *testing.T) {
	p := buildLocalInspectionRoleSystemPrompt("D:/repo", codingRoleExplorer, "map auth")
	if !strings.Contains(p, "Local Inspection SubAgent") {
		t.Fatal("expected inspection header")
	}
	if strings.Contains(p, "write_file") || strings.Contains(p, "edit_file") {
		t.Fatal("inspection prompt must not advertise write tools")
	}
	if !strings.Contains(p, "map auth") {
		t.Fatal("expected req context")
	}
}

func TestFinishInspectionRoleTaskRequiresEvidence(t *testing.T) {
	sa := &CodingSubAgent{nestDepth: 1, role: codingRoleExplorer, projectPath: t.TempDir()}
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Index: 1, Title: "explore"}}
	// No tool activity → fail.
	got := sa.finishInspectionRoleTask(cb, cb.task, "explore", agent.LoopResult{
		Text: "looked around", Iterations: 1, ToolCalls: 0,
	})
	if got.Status != TaskExecFailed {
		t.Fatalf("expected fail without tools, got %+v", got)
	}
	// With a read audit → pass.
	cb.filesRead = map[string]bool{"a.go": true}
	// Ensure tool call count is positive via LoopResult.
	got = sa.finishInspectionRoleTask(cb, cb.task, "explore", agent.LoopResult{
		Text: "found a.go", Iterations: 2, ToolCalls: 2,
	})
	if got.Status != TaskExecPassed {
		t.Fatalf("expected pass with read evidence, got status=%s err=%s", got.Status, got.Error)
	}
	if !strings.Contains(got.Summary, "inspection-only") {
		t.Fatalf("expected inspection note, summary=%q", got.Summary)
	}
}
