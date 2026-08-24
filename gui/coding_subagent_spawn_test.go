package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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

func TestLocalRuntimeSpawnRejectsClosedParentAttempt(t *testing.T) {
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "local-spawn-closed", ProjectRef: t.TempDir(), Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: task.ProjectRef, Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelTask(task.TaskID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{fullEnvironment: true, runtimeStore: store, runtimeAttempt: attempt}}
	result := cb.executeSpawnCodingAgent(map[string]interface{}{"role": "explorer", "task": "inspect"})
	if result.Outcome != codingToolOutcomeFailed || !strings.Contains(result.Text, "runtime parent attempt is no longer running") {
		t.Fatalf("closed Runtime attempt must reject spawn, got %#v", result)
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
			map[string]interface{}{"role": "reviewer", "task": "B", "context": "use A"},
		},
	})
	if err != nil || len(specs) != 2 {
		t.Fatalf("parallel = %#v err=%v", specs, err)
	}
	if specs[1].Context != "use A" || specs[1].Role != codingRoleReviewer {
		t.Fatalf("second agent = %#v", specs[1])
	}
	// Coerce non-string task/role from loose tool arg decoding.
	specs, err = parseCodingSpawnSpecs(map[string]interface{}{
		"role": "explorer",
		"task": float64(42),
	})
	if err != nil || len(specs) != 1 || specs[0].Task != "42" {
		t.Fatalf("numeric task coerce = %#v err=%v", specs, err)
	}
	if _, err := parseCodingSpawnSpecs(map[string]interface{}{"role": "worker", "task": "write"}); err == nil {
		t.Fatal("worker without files write-set must be rejected")
	}
	specs, err = parseCodingSpawnSpecs(map[string]interface{}{
		"role":  "worker",
		"task":  "write helper",
		"files": []interface{}{"src/helper.go"},
	})
	if err != nil || len(specs) != 1 || specs[0].Role != codingRoleWorker || len(specs[0].Files) != 1 {
		t.Fatalf("worker with files = %#v err=%v", specs, err)
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
		{Role: codingRoleWorker, Task: "b", Files: []string{"a.go"}},
	}) {
		t.Fatal("mixed with worker must be sequential")
	}
	if shouldParallelizeCodingSpawn([]codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"a.go"}},
		{Role: codingRoleWorker, Task: "b", Files: []string{"b.go"}},
	}) {
		t.Fatal("inspection parallelizer must never treat workers as read-only children")
	}
	if !shouldParallelizeCodingSpawn([]codingSpawnSpec{
		{Role: codingRoleReviewer, Task: "a"},
		{Role: codingRoleReviewer, Task: "b"},
	}) {
		t.Fatal("read-only reviewers may parallelize")
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
	if !rev.toolAllowedForRole("git_diff") {
		t.Fatal("reviewer should inspect git diff")
	}
	if rev.toolAllowedForRole("write_file") || rev.toolAllowedForRole("edit_file") || rev.toolAllowedForRole("bash") {
		t.Fatal("reviewer must be strictly read-only")
	}
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if ok, _ := rev.toolCallAllowedForRole("web_fetch", map[string]interface{}{key: "report.pdf"}); ok {
			t.Fatalf("reviewer web_fetch %s must not write", key)
		}
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

func TestNestedCodingSpawnUsesRoleIterationBudget(t *testing.T) {
	loopCtx := NewLoopContext("spawn-budget", 180, nil)
	root := &codingSubAgentCallbacks{subagent: &CodingSubAgent{fullEnvironment: true, nestDepth: 0, loopCtx: loopCtx}}
	if got := root.GetMaxIterations(); got != 180 {
		t.Fatalf("root budget = %d, want parent loopCtx 180", got)
	}

	cases := []struct {
		role codingSubAgentRole
		want int
	}{
		{codingRoleExplorer, codingSpawnRoleMaxIterations(codingRoleExplorer)},
		{codingRoleReviewer, codingSpawnRoleMaxIterations(codingRoleReviewer)},
		{codingRoleWorker, codingSpawnRoleMaxIterations(codingRoleWorker)},
		{"", codingSpawnRoleMaxIterations(codingRoleWorker)},
	}
	for _, tc := range cases {
		cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
			fullEnvironment: true, nestDepth: 1, role: tc.role, loopCtx: loopCtx,
		}}
		got := cb.GetMaxIterations()
		want := config.EffectiveMaxIterations(tc.want)
		if got != want {
			t.Fatalf("nested role %q budget = %d, want role cap %d (not parent 180)", tc.role, got, want)
		}
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

func TestCodingSpawnLocalPathsEqual(t *testing.T) {
	dir := t.TempDir()
	if !codingSpawnLocalPathsEqual(dir, dir) {
		t.Fatal("same path should compare equal")
	}
	if codingSpawnLocalPathsEqual(dir, "") || codingSpawnLocalPathsEqual("", dir) {
		t.Fatal("empty path must not compare equal")
	}
}

func TestIsolatedWorkerShouldKeepWorktree(t *testing.T) {
	if isolatedWorkerShouldKeepWorktree(nil, &CodingSubAgentResult{FilesModified: []string{"a.go"}}) != true {
		t.Fatal("audit files should keep the worktree")
	}
	if isolatedWorkerShouldKeepWorktree(&codingWorkbenchWorktree{}, &CodingSubAgentResult{}) {
		t.Fatal("empty unused worktree should not be kept")
	}
	if !isolatedWorkerShouldKeepWorktree(&codingWorkbenchWorktree{created: true}, &CodingSubAgentResult{}) {
		t.Fatal("created worktree with no probeable path must be kept")
	}
	if !isolatedWorkerShouldKeepWorktree(&codingWorkbenchWorktree{created: true, Path: t.TempDir()}, &CodingSubAgentResult{}) {
		t.Fatal("git status failure must keep the worktree")
	}
}

func TestCanParallelizeIsolatedWorkers(t *testing.T) {
	project := t.TempDir()
	if !canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"src/a.go"}},
		{Role: codingRoleWorker, Task: "b", Files: []string{"src/b.go"}},
	}) {
		t.Fatal("two isolated workers with disjoint files should parallelize")
	}
	if canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"src/shared.go"}},
		{Role: codingRoleWorker, Task: "b", Files: []string{"src/shared.go"}},
	}) {
		t.Fatal("overlapping write-sets must not parallelize")
	}
	if canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"src/a.go"}},
		{Role: codingRoleWorker, Task: "b", Files: []string{"src/"}},
	}) {
		t.Fatal("file vs containing directory must not parallelize")
	}
	if canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"a.go"}},
		{Role: codingRoleExplorer, Task: "inspect"},
	}) {
		t.Fatal("mixed explorer+worker must stay sequential")
	}
	if canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"a.go"}},
		{Role: codingRoleWorker, Task: "b", Files: []string{"b.go"}},
		{Role: codingRoleWorker, Task: "c", Files: []string{"c.go"}},
	}) {
		t.Fatal("more than two isolated workers must stay sequential")
	}
	if canParallelizeIsolatedWorkers(project, []codingSpawnSpec{
		{Role: codingRoleWorker, Task: "a", Files: []string{"a.go"}},
	}) {
		t.Fatal("a single worker has nothing to parallelize")
	}
}

func TestValidateIsolatedWorkerSpecs(t *testing.T) {
	project := t.TempDir()
	if err := validateIsolatedWorkerSpecs(project, []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/helper.go"},
	}}); err != nil {
		t.Fatalf("declared worker files should validate, got %v", err)
	}
	if err := validateIsolatedWorkerSpecs(project, []codingSpawnSpec{{
		Role: codingRoleWorker, Task: "write", Files: []string{"src/*.go"},
	}}); err == nil {
		t.Fatal("wildcard write-set must be rejected")
	}
	if err := validateIsolatedWorkerSpecs(project, []codingSpawnSpec{{
		Role: codingRoleExplorer, Task: "inspect",
	}}); err != nil {
		t.Fatalf("inspection spec should skip write-set, got %v", err)
	}
}

func TestRunNestedCodingAgentWorkerRequiresIsolate(t *testing.T) {
	parent := &CodingSubAgent{fullEnvironment: true, projectPath: t.TempDir()}
	res := parent.runNestedCodingAgent(codingSpawnSpec{
		Role: codingRoleWorker, Task: "write helper", Files: []string{"src/a.go"},
	}, nil, nil)
	if res == nil || res.Status != TaskExecFailed || !strings.Contains(res.Error, "isolated workspace") {
		t.Fatalf("worker without isolate must fail closed, got %#v", res)
	}
	res = parent.runNestedCodingAgent(codingSpawnSpec{
		Role: codingRoleWorker, Task: "write helper", Files: []string{"src/a.go"}, projectPath: parent.projectPath,
	}, nil, nil)
	if res == nil || res.Status != TaskExecFailed || !strings.Contains(res.Error, "primary project") {
		t.Fatalf("worker isolate equal to primary must fail closed, got %#v", res)
	}
}

func TestNestedCodingAgentDoesNotInheritCallbackControlPlaneState(t *testing.T) {
	parent := &CodingSubAgent{fullEnvironment: true, projectPath: t.TempDir()}
	parentCB := &codingSubAgentCallbacks{subagent: parent, task: &TaskItem{Title: "fix bug"}}
	_ = parentCB.BuildToolsForModelRequest("fix bug", 0)
	parentCB.localization.setForRevision(CodingSubAgentLocalizationEvidence{RootCauseFile: "root.go"}, parentCB.currentControlPlaneRevision())
	parentCB.todos.bindControlPlaneRevision(parentCB.currentControlPlaneRevision())
	_, _ = parentCB.todos.applyTodoWriteCAS([]codingAgentTodoItem{{ID: "1", Content: "parent todo", Status: codingAgentTodoInProgress}}, false, 1, 0, true)

	child := NewCodingSubAgent(parent.handler, parent.cfg, parent.httpClient, parent.projectPath, parent.loopCtx)
	childCB := newCodingSubAgentCallbacks(child, &TaskItem{Title: "child inspect"}, "", "", nil)
	if childCB.currentControlPlaneRevision() != 0 || childCB.localizationForCurrentControlPlaneRevision() != nil {
		t.Fatalf("child must begin with independent control-plane state: revision=%d evidence=%#v", childCB.currentControlPlaneRevision(), childCB.localizationForCurrentControlPlaneRevision())
	}
	if got := childCB.todos.controlPlaneSnapshot(); got.Revision != 0 || got.Version != 0 || len(got.Items) != 0 {
		t.Fatalf("child inherited parent todo state: %+v", got)
	}
}

func TestNestedCodingAgentsUseRestrictedChildLoopContexts(t *testing.T) {
	parentLoop := NewLoopContext("parent-loop", 91, nil)
	parentLoop.UserID = "parent-user"
	parentLoop.Runtime.RequestID = "parent-request"
	parentLoop.Runtime.PolicyOwnerID = "parent-owner"
	parentLoop.CodingTaskIngressToken = "parent-ingress"
	parentLoop.CodingAttachments = []agent.MessageAttachment{{}}
	parentLoop.WorkflowID = "parent-workflow"
	parentLoop.codeSessionID = "parent-preview"

	localParent := &CodingSubAgent{
		fullEnvironment: true,
		projectPath:     t.TempDir(),
		loopCtx:         parentLoop,
		scopeApproval:   newScopeApprovalState(nil, true),
	}
	localChild := localParent.newReadOnlyNestedCodingAgent(codingSpawnSpec{Role: codingRoleExplorer, Task: "inspect"}, nil)
	if localChild.loopCtx == nil || localChild.loopCtx == parentLoop {
		t.Fatal("local detached child must receive a fresh loop context")
	}
	if localChild.scopeApproval != nil {
		t.Fatal("local child must not inherit parent scope approval")
	}
	assertRestrictedCodingChildLoopContext(t, localChild.loopCtx, parentLoop)

	remoteParent := &RemoteCodingSubAgent{
		loopCtx:              parentLoop,
		highRiskApproval:     newRemoteHighRiskApprovalState(nil, true),
		sourcePreviewEnabled: true,
	}
	remoteChild := remoteParent.newReadOnlyNestedRemoteCodingAgent(codingSpawnSpec{Role: codingRoleReviewer, Task: "review"}, nil)
	if remoteChild.loopCtx == nil || remoteChild.loopCtx == parentLoop {
		t.Fatal("remote detached child must receive a fresh loop context")
	}
	if remoteChild.highRiskApproval != nil {
		t.Fatal("remote child must not inherit parent high-risk approval")
	}
	assertRestrictedCodingChildLoopContext(t, remoteChild.loopCtx, parentLoop)
}

func TestCodingChildLoopCancellationBoundary(t *testing.T) {
	parent := NewLoopContext("parent-loop", 0, nil)
	synchronous := newCodingChildExecutionContext(parent, nil, false)
	detached := newCodingChildExecutionContext(parent, nil, true)
	defer synchronous.release()
	defer detached.release()

	parent.Cancel()
	deadline := time.After(time.Second)
	for !synchronous.loopCtx.IsCancelled() {
		select {
		case <-deadline:
			t.Fatal("parent cancellation must reach synchronous child bridge")
		case <-time.After(time.Millisecond):
		}
	}
	if detached.loopCtx.IsCancelled() {
		t.Fatal("parent cancellation must not cancel detached child loop context")
	}
}

func TestCodingChildTraceAndIngressAreIndependentFromParent(t *testing.T) {
	parent := NewLoopContext("parent-loop", 0, nil)
	parent.UserID = "parent-user"
	parent.Runtime.RequestID = "parent-request"
	parent.Runtime.PolicyOwnerID = "parent-owner"
	parent.CodingTaskIngressToken = "parent-ingress"
	childExecution := newCodingChildExecutionContext(parent, nil, true)
	defer childExecution.release()

	ctx, finish, err := codingLoopLLMRequestContext(nil, childExecution.loopCtx, "coding-child-test", 1)
	if err != nil {
		t.Fatalf("child request context: %v", err)
	}
	defer finish(nil)
	trace, ok := llm.RequestTraceFromContext(ctx)
	if !ok {
		t.Fatal("child request context must carry a trace")
	}
	if trace.LoopID != childExecution.loopCtx.ID || trace.LoopID == parent.ID {
		t.Fatalf("child trace loop=%q parent=%q child=%q", trace.LoopID, parent.ID, childExecution.loopCtx.ID)
	}
	if trace.OwnerID != "" || trace.RequestID != "" {
		t.Fatalf("child trace inherited parent request identity: %+v", trace)
	}
	if (&RemoteCodingSubAgent{nestDepth: 1}).mayReadDesktopCodingIngress() {
		t.Fatal("nested remote child must never read desktop root ingress")
	}
	if !(&RemoteCodingSubAgent{}).mayReadDesktopCodingIngress() {
		t.Fatal("root remote agent remains eligible for authenticated ingress handling")
	}
}

func TestNestedWorkersReceiveChildScopedApprovals(t *testing.T) {
	parentLoop := NewLoopContext("parent-loop", 0, nil)
	localParent := &CodingSubAgent{
		loopCtx:       parentLoop,
		projectPath:   t.TempDir(),
		scopeApproval: newScopeApprovalState(nil, true),
	}
	localIsolate := t.TempDir()
	child := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, localIsolate, NewLoopContext("child", 0, nil))
	child.nestDepth, child.role = 1, codingRoleWorker
	child.setNestedWorkerScopeApproval(nil)
	if child.scopeApproval == nil || child.scopeApproval == localParent.scopeApproval || child.scopeApproval.fullAccess {
		t.Fatal("local worker requires a fresh, non-full-access approval state")
	}
	if !child.scopeApproval.isApproved(localIsolate) {
		t.Fatal("local worker isolate root must be the only pre-approved scope")
	}

	remoteParent := &RemoteCodingSubAgent{highRiskApproval: newRemoteHighRiskApprovalState(nil, true)}
	remoteChild := &RemoteCodingSubAgent{projectDir: "/isolated", nestDepth: 1, role: codingRoleWorker}
	remoteChild.setNestedRemoteWorkerApproval(nil)
	if remoteChild.highRiskApproval == nil || remoteChild.highRiskApproval == remoteParent.highRiskApproval || remoteChild.highRiskApproval.highRiskFullAccess || remoteChild.highRiskApproval.pathFullAccess {
		t.Fatal("remote worker requires a fresh, non-full-access approval state")
	}
	remoteChild.highRiskApproval.mu.Lock()
	remoteRootApproved := remoteChild.highRiskApproval.isApprovedLocked("/isolated")
	remoteChild.highRiskApproval.mu.Unlock()
	if !remoteRootApproved {
		t.Fatal("remote worker isolate root must be the only pre-approved scope")
	}
}

func TestReadOnlyChildAdmissionDoesNotRetainParentSemanticIdentity(t *testing.T) {
	parentIdentity := &trustedCodingInvocationIdentity{
		RootTaskID: "parent-root", TenantID: "tenant", PrincipalID: "principal", SessionID: "session",
	}
	parent := &CodingSubAgent{
		role:                       codingRoleExplorer,
		dynamicInvocationIdentity:  parentIdentity,
		verifiedInvocationIdentity: parentIdentity,
		staticShadowPlan:           &codingStaticPlanPreparation{},
	}
	child := *parent
	child.prepareAdmittedReadOnlyChildSemanticState(codingruntime.ExecutionRequest{})
	if child.dynamicInvocationIdentity != nil || child.verifiedInvocationIdentity != nil || child.staticShadowPlan != nil || child.staticWorkspaceBinding.complete() || child.verifiedTaskHandle != nil || child.verifiedTaskRelationService != nil {
		t.Fatalf("local child retained parent semantic state: %#v", child)
	}

	remoteParent := &RemoteCodingSubAgent{
		role:                       codingRoleExplorer,
		dynamicInvocationIdentity:  parentIdentity,
		verifiedInvocationIdentity: parentIdentity,
	}
	remoteChild := *remoteParent
	remoteChild.prepareAdmittedReadOnlyChildSemanticState(codingruntime.ExecutionRequest{})
	if remoteChild.dynamicInvocationIdentity != nil || remoteChild.verifiedInvocationIdentity != nil || remoteChild.verifiedTaskHandle != nil || remoteChild.verifiedTaskRelationService != nil {
		t.Fatalf("remote child retained parent semantic state: %#v", remoteChild)
	}
}

func TestAdmittedLocalChildShadowPlanCannotReuseParentWorkspaceBinding(t *testing.T) {
	identity := &trustedCodingInvocationIdentity{
		RootTaskID: "root", TurnID: "child-turn", TenantID: "tenant", PrincipalID: "principal", SessionID: "session",
	}
	child := &CodingSubAgent{
		dynamicInvocationIdentity: identity,
		staticWorkspaceBinding:    codingStaticWorkspaceBinding{WorkspaceHandle: "parent-workspace", HostKind: "local"},
	}
	request := codingruntime.ExecutionRequest{Attempt: codingruntime.Attempt{Policy: codingruntime.PolicySnapshot{ReadOnly: true}}}
	child.staticWorkspaceBinding = codingStaticWorkspaceBinding{}
	prepared := prepareAdmittedLocalChildStaticShadowPlan(child, request)
	if prepared == nil {
		t.Fatal("child without workspace binding must still expose catalog-incomplete shadow plan")
	}
	if len(prepared.Plan.Selections) != 0 || len(prepared.Plan.Unmet) == 0 {
		t.Fatalf("child shadow plan must not reuse parent workspace: %#v", prepared.Plan)
	}
}

func assertRestrictedCodingChildLoopContext(t *testing.T, child, parent *LoopContext) {
	t.Helper()
	if child.ID == "" || child.ID == parent.ID {
		t.Fatalf("child diagnostic loop ID must be fresh: child=%q parent=%q", child.ID, parent.ID)
	}
	if child.UserID != "" || child.Runtime.RequestID != "" || child.Runtime.PolicyOwnerID != "" || child.CodingTaskIngressToken != "" {
		t.Fatalf("child inherited parent identity/ingress state: %+v", child)
	}
	if len(child.CodingAttachments) != 0 || child.WorkflowID != "" || child.codeSessionID != "" {
		t.Fatalf("child inherited parent attachment/workflow/preview state: %+v", child)
	}
}

func TestExecuteIsolatedWorkerSpawnRequiresGitRepo(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{fullEnvironment: true, projectPath: t.TempDir()},
	}
	res := cb.executeSpawnCodingAgent(map[string]interface{}{
		"role":  "worker",
		"task":  "add helper",
		"files": []interface{}{"src/helper.go"},
	})
	if res.Outcome != codingToolOutcomeFailed || !strings.Contains(res.Text, "git repository") {
		t.Fatalf("non-git worker spawn = %#v", res)
	}
	res = cb.executeSpawnCodingAgent(map[string]interface{}{
		"agents": []interface{}{
			map[string]interface{}{"role": "worker", "task": "a", "files": []interface{}{"src/a.go"}},
			map[string]interface{}{"role": "worker", "task": "b", "files": []interface{}{"src/b.go"}},
		},
	})
	if res.Outcome != codingToolOutcomeFailed || !strings.Contains(res.Text, "git repository") {
		t.Fatalf("non-git parallel worker spawn = %#v", res)
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

func TestFormatAdmittedReadOnlyChildHandlesReturnsHandoffOnly(t *testing.T) {
	text := formatAdmittedReadOnlyChildHandles([]codingruntime.ChildTaskHandle{{
		TaskID: "child-1", Name: "explorer", Status: codingruntime.TaskQueued,
	}})
	if !strings.Contains(text, "child-1") || !strings.Contains(text, "fresh parent attempt") {
		t.Fatalf("handoff text=%q", text)
	}
	if strings.Contains(text, "completed") {
		t.Fatalf("admission response must not claim child completion: %q", text)
	}
}

func TestBuildFullEnvPreambleMentionsSpawn(t *testing.T) {
	preamble := buildFullCodingEnvironmentPromptPreamble()
	if !strings.Contains(preamble, codingSubAgentSpawnToolName) {
		t.Fatal("preamble should document spawn_coding_agent")
	}
	if !strings.Contains(preamble, "explorer") || !strings.Contains(preamble, "reviewer") || !strings.Contains(preamble, "files") {
		t.Fatal("preamble should list roles and worker files contract")
	}
	if !strings.Contains(preamble, "不重叠") || !strings.Contains(preamble, "远程 worker 一律顺序") {
		t.Fatal("preamble should document local parallel workers and sequential remote workers")
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
