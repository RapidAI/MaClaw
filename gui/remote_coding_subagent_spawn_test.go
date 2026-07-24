package main

import (
	"strings"
	"testing"
)

func TestCanSpawnRemoteCodingAgentDepthAndRole(t *testing.T) {
	root := &RemoteCodingSubAgent{nestDepth: 0}
	if !root.canSpawnRemoteCodingAgent() {
		t.Fatal("root remote pure coding should spawn")
	}
	child := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker}
	if child.canSpawnRemoteCodingAgent() {
		t.Fatal("depth 1 must not spawn")
	}
	explorer := &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleExplorer}
	if explorer.canSpawnRemoteCodingAgent() {
		t.Fatal("explorer root should not spawn")
	}
}

func TestRemoteToolAllowedForRole(t *testing.T) {
	ex := &RemoteCodingSubAgent{role: codingRoleExplorer, nestDepth: 1}
	if !ex.remoteToolAllowedForRole("ssh_read_file") || !ex.remoteToolAllowedForRole("ssh_bash") {
		t.Fatal("explorer should read/bash explore")
	}
	if ex.remoteToolAllowedForRole("ssh_write_file") || ex.remoteToolAllowedForRole("ssh_edit_file") {
		t.Fatal("explorer must not write/edit")
	}
	if ex.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("explorer must not spawn")
	}

	rev := &RemoteCodingSubAgent{role: codingRoleReviewer, nestDepth: 1}
	if !rev.remoteToolAllowedForRole("ssh_bash") || !rev.remoteToolAllowedForRole("ssh_check_task") {
		t.Fatal("reviewer should bash/check_task")
	}
	if rev.remoteToolAllowedForRole("ssh_write_file") {
		t.Fatal("reviewer must not write")
	}

	worker := &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleWorker}
	if !worker.remoteToolAllowedForRole("ssh_write_file") || !worker.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("root worker should write and spawn")
	}
	nestedWorker := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker}
	if nestedWorker.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("nested worker must not spawn")
	}
}

func TestFilterRemoteCodingToolsForRole(t *testing.T) {
	tools := remoteCodingToolDefinitions()
	tools = append(tools, buildSpawnCodingAgentToolDefinition())
	tools = append(tools, buildCodingFullEnvExtraToolDefinitions()...)

	root := &RemoteCodingSubAgent{nestDepth: 0}
	filtered := filterRemoteCodingToolsForRole(tools, root)
	names := map[string]bool{}
	for _, d := range filtered {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[codingSubAgentSpawnToolName] || !names["ssh_write_file"] {
		t.Fatalf("root tools missing spawn/write: %#v", names)
	}

	ex := &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer}
	filtered = filterRemoteCodingToolsForRole(tools, ex)
	names = map[string]bool{}
	for _, d := range filtered {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if names["ssh_write_file"] || names["ssh_edit_file"] || names[codingSubAgentSpawnToolName] {
		t.Fatalf("explorer leaked write/spawn: %#v", names)
	}
	if !names["ssh_read_file"] || !names["ssh_bash"] {
		t.Fatalf("explorer missing read tools: %#v", names)
	}
}

func TestRemoteBuildToolsIncludesSpawnOnRoot(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 0, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "implement feature",
	}
	tools := cb.BuildTools("implement feature")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[codingSubAgentSpawnToolName] {
		t.Fatalf("remote root BuildTools missing %s; got %#v", codingSubAgentSpawnToolName, names)
	}
	if !names["ssh_bash"] || !names["web_search"] {
		t.Fatalf("expected baseline remote tools, got %#v", names)
	}
}

func TestRemoteInquiryToolSurfaceDoesNotExposeLocalExtensions(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{readOnlyInquiry: true, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "which file implements authentication?",
	}
	prompt := cb.BuildSystemPrompt(cb.task, true)
	if strings.Contains(prompt, "manage_skill") || strings.Contains(prompt, "call_mcp_tool") {
		t.Fatalf("repository inquiry must not advertise unavailable local extensions: %q", prompt)
	}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
		if name == "ssh_write_file" || name == "ssh_edit_file" || name == "manage_skill" || name == "call_mcp_tool" || name == "todo_write" {
			t.Fatalf("repository inquiry exposed a mutating or unavailable tool: %s", name)
		}
	}
}

func TestRemoteOperationalTaskUsesFocusedNonMutatingSurface(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{operationalRequest: true, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "run the app",
	}
	prompt := cb.BuildSystemPrompt(cb.task, true)
	if !strings.Contains(prompt, "Remote operational task") {
		t.Fatalf("expected operational prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Normal build output") {
		t.Fatalf("operational prompt must allow normal build output, got %q", prompt)
	}
	for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
		if name == "ssh_write_file" || name == "ssh_edit_file" || name == codingSubAgentSpawnToolName || name == "todo_write" {
			t.Fatalf("operational request exposed a mutating or planning tool: %s", name)
		}
	}
	if !isRemoteCodingOperationalTool("ssh_bash") || isRemoteCodingOperationalTool("ssh_write_file") {
		t.Fatal("unexpected remote operational tool policy")
	}
	if rejectCodingOperationalShellCommand("npm run build") != "" {
		t.Fatal("build should stay available for an operational request")
	}
	if rejectCodingOperationalShellCommand("npm install left-pad") == "" {
		t.Fatal("dependency installation must not be treated as a run/build request")
	}
	for _, command := range []string{
		"go generate ./...",
		"cargo fix",
		"cargo clippy --fix",
		"prettier --write src/app.ts",
		"protoc --go_out=. api.proto",
		"python manage.py makemigrations",
		"sh -c 'echo changed > source.go'",
		"python -c 'from pathlib import Path; Path(\"source.py\").write_text(\"changed\")'",
		"node -e 'require(\"fs\").writeFileSync(\"source.js\", \"changed\")'",
		"npm run build $(touch source.go)",
	} {
		if rejectCodingOperationalShellCommand(command) == "" {
			t.Fatalf("source-mutating operational command was allowed: %q", command)
		}
	}
}

func TestRemoteOriginalRequestKindSurvivesExpandedPlanStep(t *testing.T) {
	// A plan-expanded prompt may be long and contain implementation words, but
	// it must not overrule the user's original direct run/build request.
	readOnly, operational := resolveRemoteCodingRequestFlags(codingRequestOperational, "[Plan step T1/3] implement and verify everything")
	if readOnly || !operational {
		t.Fatal("original operational kind must survive plan-step expansion")
	}
}

func TestRemoteSourcePreviewOnlyEnabledForImplementation(t *testing.T) {
	if !remoteCodingShouldEnableSourcePreview(codingRequestImplementation) {
		t.Fatal("implementation should enable remote source preview")
	}
	for _, kind := range []codingRequestKind{codingRequestInquiry, codingRequestOperational, ""} {
		if remoteCodingShouldEnableSourcePreview(kind) {
			t.Fatalf("request kind %q must not enable remote source preview", kind)
		}
	}
}

func TestRemoteOperationalQualityRequiresLaunchOrBuildEvidence(t *testing.T) {
	passed, summary, issues := summarizeRemoteOperationalQuality([]CodingSubAgentCommandResult{{
		Command: "npm run build", Succeeded: true,
	}}, 1)
	if passed != codingSubAgentQualityPassed || issues != 0 || !strings.Contains(summary, "launch/build command evidence") {
		t.Fatalf("build evidence=%q %q %d", passed, summary, issues)
	}
	passed, summary, issues = summarizeRemoteOperationalQuality([]CodingSubAgentCommandResult{{
		Command: "ls", Succeeded: true,
	}}, 1)
	if passed != codingSubAgentQualityFailed || issues != 1 || !strings.Contains(summary, "no launch/build command") {
		t.Fatalf("listing-only evidence=%q %q %d", passed, summary, issues)
	}
}

func TestRemoteBuildToolsExplorerFiltersWrites(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer, projectDir: "/tmp/app", workDir: "/tmp/app"},
		task:  "map code",
	}
	tools := cb.BuildTools("map auth")
	names := map[string]bool{}
	for _, d := range tools {
		fn, _ := d["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if names["ssh_write_file"] || names["ssh_edit_file"] || names[codingSubAgentSpawnToolName] {
		t.Fatalf("explorer tools leaked writes/spawn: %#v", names)
	}
	if !names["ssh_read_file"] {
		t.Fatalf("explorer missing read: %#v", names)
	}
}

func TestExecuteSpawnRemoteBlockedAtDepth(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleWorker},
	}
	res := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role": "explorer",
		"task": "should fail",
	})
	if !strings.Contains(res, "unavailable") {
		t.Fatalf("text=%s", res)
	}
}

func TestRemoteNestedSystemPromptNoRootSpawnSalesPitch(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			nestDepth:  1,
			role:       codingRoleWorker,
			projectDir: "/home/u/app",
			workDir:    "/home/u/app",
		},
		task:        "fix bug",
		taskContext: "parent ctx",
	}
	prompt := cb.BuildSystemPrompt("fix bug", true)
	if !strings.Contains(prompt, "Nested remote coding subagent") {
		t.Fatal("expected nested role header")
	}
	// Nested worker must not be told to spawn further.
	if strings.Contains(prompt, "用 spawn_coding_agent 派生子代理") {
		t.Fatal("nested prompt should not sell root spawn workflow")
	}
}

func TestRemoteExplorerSystemPromptIsInspectionOnly(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			nestDepth:  1,
			role:       codingRoleExplorer,
			projectDir: "/home/u/app",
			workDir:    "/home/u/app",
		},
		task:        "map auth",
		taskContext: "focus on jwt",
	}
	prompt := cb.BuildSystemPrompt("map auth", true)
	if !strings.Contains(prompt, "Remote Inspection SubAgent") {
		t.Fatal("expected inspection prompt")
	}
	if strings.Contains(prompt, "ssh_write_file") {
		t.Fatal("explorer prompt must not advertise write tools")
	}
	if !strings.Contains(prompt, "focus on jwt") {
		t.Fatal("expected task context")
	}
}

func TestApplyRemoteInspectionRoleOutcome(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 1, role: codingRoleExplorer},
	}
	// No inspection evidence → fail.
	got := cb.applyRemoteInspectionRoleOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "hello", ToolCalls: 2,
	}, nil, nil, nil, codingRoleExplorer)
	if got.Status != "failed" {
		t.Fatalf("expected fail without inspection, got %+v", got)
	}
	// With reads → pass.
	got = cb.applyRemoteInspectionRoleOutcome(&RemoteCodingSubAgentResult{
		Status: "success", Summary: "found auth", ToolCalls: 3,
	}, []string{"/home/u/app/a.go"}, nil, nil, codingRoleExplorer)
	if got.Status != "success" {
		t.Fatalf("expected success with read evidence, got %+v", got)
	}
	if !strings.Contains(got.Summary, "inspection-only") {
		t.Fatalf("expected inspection note, summary=%q", got.Summary)
	}
}
