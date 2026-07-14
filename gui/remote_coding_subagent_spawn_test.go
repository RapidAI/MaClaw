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
