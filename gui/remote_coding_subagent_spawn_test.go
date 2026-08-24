package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
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
	if !ex.remoteToolAllowedForRole("ssh_read_file") {
		t.Fatal("explorer should read")
	}
	if ex.remoteToolAllowedForRole("ssh_write_file") || ex.remoteToolAllowedForRole("ssh_edit_file") || ex.remoteToolAllowedForRole("ssh_bash") {
		t.Fatal("explorer must be strictly read-only")
	}
	if ex.remoteToolAllowedForRole(codingSubAgentSpawnToolName) {
		t.Fatal("explorer must not spawn")
	}

	rev := &RemoteCodingSubAgent{role: codingRoleReviewer, nestDepth: 1}
	if !rev.remoteToolAllowedForRole("ssh_check_task") {
		t.Fatal("reviewer should check task")
	}
	if rev.remoteToolAllowedForRole("ssh_write_file") || rev.remoteToolAllowedForRole("ssh_bash") {
		t.Fatal("reviewer must be strictly read-only")
	}
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if ok, _ := rev.remoteToolCallAllowedForRole("web_fetch", map[string]interface{}{key: "report.pdf"}); ok {
			t.Fatalf("remote reviewer web_fetch %s must not write through host", key)
		}
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
	if !names["ssh_read_file"] {
		t.Fatalf("explorer missing read tools: %#v", names)
	}
	if names["ssh_bash"] {
		t.Fatalf("explorer leaked ssh_bash: %#v", names)
	}
}

func TestRemoteBuildToolsIncludesSpawnOnRoot(t *testing.T) {
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	cb := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{nestDepth: 0, projectDir: "/tmp/app", workDir: "/tmp/app", generalKB: store},
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
	if !names["knowledge_image_search"] {
		t.Fatalf("remote root BuildTools missing knowledge_image_search; got %#v", names)
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

func TestRemoteFocusedToolSurfacesIncludeConfiguredKnowledgeSearch(t *testing.T) {
	codingStore, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = codingStore.Close() })
	generalStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = generalStore.Close() })

	for _, mode := range []struct {
		name  string
		agent *RemoteCodingSubAgent
	}{
		{
			name:  "inquiry",
			agent: &RemoteCodingSubAgent{readOnlyInquiry: true, projectDir: "/tmp/app", workDir: "/tmp/app", codingKB: codingStore, generalKB: generalStore},
		},
		{
			name:  "operational",
			agent: &RemoteCodingSubAgent{operationalRequest: true, projectDir: "/tmp/app", workDir: "/tmp/app", codingKB: codingStore, generalKB: generalStore},
		},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cb := &remoteCodingCallbacks{agent: mode.agent, task: "inspect the configured project"}
			names := map[string]bool{}
			for _, name := range codingSubAgentToolDefinitionNamesForTest(cb.BuildTools(cb.task)) {
				names[name] = true
			}
			for _, want := range []string{"coding_knowledge_search", "knowledge_search", "knowledge_image_search"} {
				if !names[want] {
					t.Fatalf("configured knowledge tool %q missing from %s surface: %#v", want, mode.name, names)
				}
			}
		})
	}
}

func TestRemoteRuntimeSpawnRejectsClosedParentAttempt(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "remote-spawn-closed", ProjectRef: "/srv/repo", Mode: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "/srv/repo", Mode: "remote"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelTask(task.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{runtimeStore: store, runtimeAttempt: attempt}}
	result := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{"role": "explorer", "task": "inspect"})
	if !strings.Contains(result, "runtime parent attempt is no longer running") {
		t.Fatalf("closed Runtime attempt must reject spawn, got %q", result)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, result) != "failed" {
		t.Fatalf("closed Runtime attempt must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, result), result)
	}
}

func TestExecuteRemoteWorkerSpawnRejected(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{nestDepth: 0, role: codingRoleWorker}}
	got := cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "implement helper",
		"files": []interface{}{"src/helper.go"},
	})
	if !strings.Contains(got, "remote worker requires an active SSH session and project directory") {
		t.Fatalf("remote worker without session = %q", got)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got) != "failed" {
		t.Fatalf("remote worker without session must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got), got)
	}
	got = cb.executeSpawnRemoteCodingAgent(map[string]interface{}{
		"role": "worker",
		"task": "implement helper",
	})
	if !strings.Contains(got, "worker requires files") {
		t.Fatalf("remote worker without files = %q", got)
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got) != "failed" {
		t.Fatalf("remote worker without files must be a failed tool outcome, got %q from %q", remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, got), got)
	}
}

func TestRemoteSpawnAdmissionFailuresAreFailedToolOutcomes(t *testing.T) {
	closed := "spawn_coding_agent: runtime parent attempt is no longer running"
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, codingSpawnRemoteFailure(closed)) != "failed" {
		t.Fatal("closed parent attempt must classify as a failed remote tool")
	}
	success := "spawn_coding_agent(remote) completed: 1 agent(s) mode=sequential\n\n### agent[0] role=worker\nsummary:\nfixed the error: off-by-one\npassed=1 failed=0\n"
	if remoteCodingToolOutcome(success) != "failed" {
		t.Fatal("sanity: SSH-log heuristic still trips on error: in a child summary")
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, success) != "success" {
		t.Fatal("successful remote spawn report must stay a successful tool outcome even if a child summary mentions error:")
	}
	if remoteCodingExecutionOutcome(codingSubAgentSpawnToolName, "spawn_coding_agent admitted read-only child task(s); parent attempt released its lease:") != "success" {
		t.Fatal("ledger admission report must stay a successful tool outcome")
	}
}

func TestExecuteLedgerReadOnlyRemoteSpawnRejectsWorker(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{
		runtimeStore:   codingruntime.NewMemoryStore(),
		runtimeAttempt: &codingruntime.Attempt{AttemptID: "att-1"},
	}}
	got := cb.executeLedgerReadOnlyRemoteSpawn([]codingSpawnSpec{{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/helper.go"},
	}})
	if !strings.Contains(got, "inspection ledger admission cannot run worker children") {
		t.Fatalf("ledger remote spawn = %q", got)
	}
}

func TestIsolatedRemoteWorkerShouldKeepIsolate(t *testing.T) {
	if !isolatedRemoteWorkerShouldKeepIsolate(nil, nil, &RemoteCodingSubAgentResult{FilesModified: []string{"a.go"}}) {
		t.Fatal("audit files should keep the isolate")
	}
	if !isolatedRemoteWorkerShouldKeepIsolate(&remoteCodingIsolate{created: true, IsolateDir: "/tmp/maclaw-wt-1"}, nil, nil) {
		t.Fatal("unprobed isolate must be kept")
	}
}

func TestRemapRemoteIsolatePaths(t *testing.T) {
	got := remapRemoteIsolatePaths(
		[]string{"/tmp/maclaw-wt-1/src/a.go", "/tmp/maclaw-wt-1", "/home/u/app/keep.go"},
		"/tmp/maclaw-wt-1",
		"/home/u/app",
	)
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "/home/u/app/src/a.go") || !strings.Contains(joined, "/home/u/app") {
		t.Fatalf("remap = %#v", got)
	}
	if strings.Contains(joined, "/tmp/maclaw-wt-1/src") {
		t.Fatalf("isolate prefix leaked: %#v", got)
	}
	escaped := remapRemoteIsolatePaths(
		[]string{"/tmp/maclaw-wt-1/../etc/passwd"},
		"/tmp/maclaw-wt-1",
		"/home/u/app",
	)
	for _, p := range escaped {
		if strings.Contains(p, "/home/u/app") {
			t.Fatalf("traversal must not remap onto the source tree: %#v", escaped)
		}
	}
}

func TestValidateRemoteIsolatedWorkerSpecs(t *testing.T) {
	if err := validateRemoteIsolatedWorkerSpecs("/home/u/app", []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "w", Files: []string{"./src/a.go"},
	}}); err == nil {
		t.Fatal("dot-relative remote write-set must fail at admission")
	}
	if err := validateRemoteIsolatedWorkerSpecs("/home/u/app", []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "w", Files: []string{"src/a.go"},
	}}); err != nil {
		t.Fatalf("plain relative path should pass: %v", err)
	}
}

func TestCodingSpawnBatchHeaderMarksFailure(t *testing.T) {
	ok := codingSpawnBatchHeader("spawn_coding_agent", 2, 2, 0, "parallel")
	if !strings.Contains(ok, "completed") || strings.Contains(ok, "错误") {
		t.Fatalf("success header=%q", ok)
	}
	bad := codingSpawnBatchHeader("spawn_coding_agent", 2, 1, 1, "sequential")
	if !strings.Contains(bad, "错误") || strings.Contains(bad, "completed") {
		t.Fatalf("failure header=%q", bad)
	}
}

func TestRemoteIsolatedWorkerUsesIsolateProjectDir(t *testing.T) {
	parent := &RemoteCodingSubAgent{projectDir: "/home/u/app", workDir: "/home/u/work"}
	workDir, projectDir, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"}, projectPath: "/tmp/maclaw-wt-1",
	})
	if err != nil || projectDir != "/tmp/maclaw-wt-1" || workDir != "/tmp/maclaw-wt-1" {
		t.Fatalf("worker must bind isolate dirs, got project=%s work=%s err=%v", projectDir, workDir, err)
	}
	if _, _, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"},
	}); err == nil {
		t.Fatal("worker without isolate path must fail closed")
	}
	if _, _, err := codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/a.go"}, projectPath: "/home/u/app",
	}); err == nil {
		t.Fatal("worker isolate equal to primary must fail closed")
	}
	workDir, projectDir, err = codingSpawnRemoteChildDirs(parent, codingSpawnSpec{
		Role: codingRoleExplorer, Task: "inspect", projectPath: "/tmp/maclaw-wt-1",
	})
	if err != nil || projectDir != "/home/u/app" || workDir != "/home/u/work" {
		t.Fatalf("explorer must stay on primary, got project=%s work=%s err=%v", projectDir, workDir, err)
	}
}

func TestRemoteReadOnlyChildDoesNotInheritPreviewLifecycle(t *testing.T) {
	parent := &RemoteCodingSubAgent{sourcePreviewEnabled: true, sourcePreviewSessionID: "root-preview"}
	child := parent.newReadOnlyNestedRemoteCodingAgent(codingSpawnSpec{Role: codingRoleExplorer, Task: "inspect"}, nil)
	if child == nil || child.sourcePreviewEnabled || child.sourcePreviewSessionID != "" {
		t.Fatalf("read-only child must not inherit root preview lifecycle: %#v", child)
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

func TestResolveRemoteCodingRequestFlagsWorkspaceClearIsImplementation(t *testing.T) {
	readOnly, operational := resolveRemoteCodingRequestFlags(codingRequestOperational, "清空当前目录")
	if readOnly || operational {
		t.Fatalf("workspace clear must not stay operational remotely, readOnly=%v operational=%v", readOnly, operational)
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
