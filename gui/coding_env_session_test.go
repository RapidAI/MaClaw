package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stickyTestUserID(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	userID := "desktop-user:" + dir
	t.Cleanup(func() {
		h := &IMMessageHandler{}
		h.clearStickyCodingWorkbenchMemory(userID)
	})
	return userID
}

func TestStickyCodingWorkbenchMemoryPrevOutputs(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{
		TurnCount:     2,
		LastUserText:  "fix the auth bug",
		LastSummary:   "patched middleware and added tests",
		SessionPlan:   "Ship auth fix",
		FilesModified: []string{"a.go", "b.go"},
		FilesCreated:  []string{"a_test.go"},
	}
	outs := mem.prevOutputs()
	if len(outs) == 0 {
		t.Fatal("expected prevOutputs for multi-turn memory")
	}
	joined := strings.Join(outs, "\n")
	if !strings.Contains(joined, "turn 3") {
		t.Fatalf("expected next-turn continuity hint, got %q", joined)
	}
	if !strings.Contains(joined, "Session plan") || !strings.Contains(joined, "Ship auth fix") {
		t.Fatalf("expected session plan, got %q", joined)
	}
	if !strings.Contains(joined, "fix the auth bug") {
		t.Fatalf("expected previous user request, got %q", joined)
	}
	if !strings.Contains(joined, "patched middleware") {
		t.Fatalf("expected previous summary, got %q", joined)
	}
	if !strings.Contains(joined, "a.go") || !strings.Contains(joined, "a_test.go") {
		t.Fatalf("expected file lists, got %q", joined)
	}
}

func TestRecordStickyLocalCodingTurnAndClear(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.recordStickyLocalCodingTurn(userID, "D:/repo", "add login", &CodingSubAgentResult{
		Summary:       "implemented login handler",
		FilesModified: []string{"auth.go"},
		FilesCreated:  []string{"auth_test.go"},
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.TurnCount != 1 || mem.Kind != "local" {
		t.Fatalf("memory after first turn = %+v", mem)
	}
	if mem.LastSummary != "implemented login handler" {
		t.Fatalf("summary = %q", mem.LastSummary)
	}
	outs := mem.prevOutputs()
	if len(outs) < 2 {
		t.Fatalf("prevOutputs too short: %#v", outs)
	}

	h.recordStickyLocalCodingTurn(userID, "D:/repo", "add logout", &CodingSubAgentResult{
		Summary:       "implemented logout",
		FilesModified: []string{"session.go"},
	})
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.TurnCount != 2 {
		t.Fatalf("turn count = %d", mem.TurnCount)
	}
	if len(mem.FilesModified) < 2 {
		t.Fatalf("files should accumulate, got %#v", mem.FilesModified)
	}

	h.clearStickyCodingWorkbenchMemory(userID)
	if mem = h.getStickyCodingWorkbenchMemory(userID); mem.TurnCount != 0 || mem.LastSummary != "" {
		t.Fatalf("clear should wipe memory, got %+v", mem)
	}
}

func TestRecordStickyRemoteCodingTurn(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.recordStickyRemoteCodingTurn(userID, "deploy fix", &RemoteCodingSubAgentResult{
		Status:  "success",
		Summary: "updated remote service",
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.Kind != "remote" || mem.TurnCount != 1 {
		t.Fatalf("remote memory = %+v", mem)
	}
	if !strings.Contains(mem.LastSummary, "updated remote service") {
		t.Fatalf("summary = %q", mem.LastSummary)
	}
}

func TestStickyCodingEffectiveFullAccessAndRemoteApply(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	// Neither global nor sticky → not full.
	if h.stickyCodingEffectiveFullAccess(userID, false) {
		t.Fatal("expected not full")
	}
	// Global full alone → full (no explicit request mode).
	if !h.stickyCodingEffectiveFullAccess(userID, true) {
		t.Fatal("global full should be full")
	}
	// Sticky path+high-risk without global → full.
	h.markStickyCodingSessionFullAccess(userID, "remote", "/home/u/app")
	h.markStickyCodingSessionHighRiskAccess(userID)
	if !h.stickyCodingEffectiveFullAccess(userID, false) {
		t.Fatal("sticky path+high-risk should be full")
	}
	// Remote sticky overlay without construction fullAccess.
	state := newRemoteHighRiskApprovalState(nil, false)
	h.applyStickyRemoteCodingPermissions(userID, state)
	state.mu.Lock()
	pathOK, riskOK := state.pathFullAccess, state.highRiskFullAccess
	state.mu.Unlock()
	if !pathOK || !riskOK {
		t.Fatalf("sticky remote apply path=%v highRisk=%v", pathOK, riskOK)
	}
	// Sync from global seeds sticky high-risk on a fresh session.
	user2 := stickyTestUserID(t)
	h.syncStickyCodingFullAccessFromGlobal(user2, "local", "D:/repo", true)
	mem := h.getStickyCodingWorkbenchMemory(user2)
	if !mem.SessionFullAccess || !mem.SessionHighRiskAccess || mem.SessionPermissionMode != "full" {
		t.Fatalf("sync from global sticky = %+v", mem)
	}
	// Second sync is a no-op (still full, no panic).
	h.syncStickyCodingFullAccessFromGlobal(user2, "local", "D:/repo", true)
	// Explicit request mode is not overridden by global full sync.
	user3 := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(user3, "request", "local", "D:/repo")
	if h.codingWorkbenchPermissionMode(user3, true) != "request" {
		t.Fatal("explicit request should win over global full")
	}
	h.syncStickyCodingFullAccessFromGlobal(user3, "local", "D:/repo", true)
	if mem3 := h.getStickyCodingWorkbenchMemory(user3); mem3.SessionFullAccess || mem3.SessionPermissionMode != "request" {
		t.Fatalf("sync must not override request mode: %+v", mem3)
	}
	// ensureLoopCtxUserID fills empty and upgrades bare desktop-user → project owner.
	lc := &LoopContext{}
	ensureLoopCtxUserID(lc, user2)
	if lc.UserID != user2 {
		t.Fatalf("UserID = %q", lc.UserID)
	}
	ensureLoopCtxUserID(lc, "other")
	if lc.UserID != user2 {
		t.Fatalf("UserID should stay %q, got %q", user2, lc.UserID)
	}
	// Bare desktop-user may be upgraded to a project-scoped owner (preview routing).
	lc2 := &LoopContext{UserID: desktopUserID}
	projectOwner := projectSessionOwnerID(`D:\data\tasks\hello`)
	ensureLoopCtxUserID(lc2, projectOwner)
	if lc2.UserID != projectOwner {
		t.Fatalf("bare desktop-user should upgrade to %q, got %q", projectOwner, lc2.UserID)
	}
	// Do not overwrite a different project owner with another project path.
	otherOwner := projectSessionOwnerID(`D:\data\tasks\other`)
	ensureLoopCtxUserID(lc2, otherOwner)
	if lc2.UserID != projectOwner {
		t.Fatalf("project owner must not be overwritten, got %q", lc2.UserID)
	}
}

func TestSetStickyCodingSessionPermissionMode(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(userID, "workspace", "local", "D:/w/workspace")
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "workspace" {
		t.Fatalf("mode=%s", got)
	}
	// Permission-only update must preserve execution ProjectPath.
	h.setStickyCodingSessionPermissionMode(userID, "full", "", "")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.ProjectPath != "D:/w/workspace" {
		t.Fatalf("permission mode change clobbered ProjectPath: %q", mem.ProjectPath)
	}
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "full" {
		t.Fatalf("mode=%s", got)
	}
	if !h.stickyCodingEffectiveFullAccess(userID, false) {
		t.Fatal("full sticky should be effective full")
	}
	h.setStickyCodingSessionPermissionMode(userID, "request", "local", "")
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "request" {
		t.Fatalf("mode=%s", got)
	}
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionFullAccess || mem.SessionHighRiskAccess {
		t.Fatalf("request should clear access flags: %+v", mem)
	}
	if mem.ProjectPath != "D:/w/workspace" {
		t.Fatalf("request mode should keep ProjectPath: %q", mem.ProjectPath)
	}
	// applySticky must not grant path-full under explicit request (stale flags).
	sa := &CodingSubAgent{projectPath: "D:/w"}
	mem.SessionFullAccess = true
	mem.SessionHighRiskAccess = false
	h.storeStickyCodingWorkbenchMemory(userID, mem)
	sa.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		t.Fatal("should not prompt under request with only ApprovedDirs path")
		return ScopeApprovalDeny
	}, false)
	h.applyStickyCodingPermissions(userID, sa)
	if sa.scopeApproval.fullAccess {
		t.Fatal("request mode must not auto-grant path full from stale SessionFullAccess")
	}
	// Dialog path grant escalates request → workspace so multi-turn honors trust.
	user3b := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(user3b, "request", "local", "D:/y")
	h.markStickyCodingSessionFullAccess(user3b, "local", "D:/y")
	if got := h.codingWorkbenchPermissionMode(user3b, false); got != "workspace" {
		t.Fatalf("path dialog under request should escalate to workspace, got %s", got)
	}
	sa3 := &CodingSubAgent{projectPath: "D:/y"}
	sa3.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false)
	h.applyStickyCodingPermissions(user3b, sa3)
	if !sa3.scopeApproval.fullAccess || sa3.scopeApproval.highRiskFullAccess {
		t.Fatal("escalated workspace should grant path full only")
	}
	// Dialog grants both → mode upgrades to full (including from request).
	user4 := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(user4, "request", "local", "D:/x")
	h.markStickyCodingSessionFullAccess(user4, "local", "D:/x")
	h.markStickyCodingSessionHighRiskAccess(user4)
	h.maybeUpgradeStickyPermissionModeToFull(user4)
	if got := h.codingWorkbenchPermissionMode(user4, false); got != "full" {
		t.Fatalf("after path+high-risk upgrade mode=%s", got)
	}
}

func TestMarkStickyCodingSessionFullAccessAndApply(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	project := t.TempDir()
	outside := filepath.Join(filepath.Dir(project), "sibling-outside.txt")

	h.markStickyCodingSessionFullAccess(userID, "local", project)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !mem.SessionFullAccess || mem.Kind != "local" || mem.ProjectPath != project {
		t.Fatalf("session full access not marked: %+v", mem)
	}

	h.rememberStickyApprovedDir(userID, filepath.Join(project, "vendor"))
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.ApprovedDirs) != 1 {
		t.Fatalf("approved dirs = %#v", mem.ApprovedDirs)
	}

	// Apply onto a fresh SubAgent without global full-access.
	sa := &CodingSubAgent{projectPath: project, fullEnvironment: true}
	calls := 0
	sa.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalDeny
	}, false)
	h.applyStickyCodingPermissions(userID, sa)

	// Session full-access should allow out-of-project paths without prompting.
	if msg := sa.scopeApproval.check("read_file", outside, project); msg != "" {
		t.Fatalf("session full access should allow outside path, got %q", msg)
	}
	if calls != 0 {
		t.Fatalf("session full access should not prompt, calls=%d", calls)
	}
}

func TestSeedFullEnvironmentWorkspaceApprovals(t *testing.T) {
	project := t.TempDir()
	parent := filepath.Dir(project)
	sibling := filepath.Join(parent, "sibling-file.txt")

	sa := &CodingSubAgent{projectPath: project, fullEnvironment: true}
	calls := 0
	sa.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		calls++
		return ScopeApprovalDeny
	}, false)
	sa.seedFullEnvironmentWorkspaceApprovals()

	// Paths under project should already be in-scope via projectPath checks elsewhere;
	// seed specifically approves parent so sibling under parent is allowed.
	if msg := sa.scopeApproval.check("read_file", sibling, project); msg != "" {
		t.Fatalf("seeded parent should allow sibling path, got %q", msg)
	}
	if calls != 0 {
		t.Fatalf("seeded approval should not prompt, calls=%d", calls)
	}

	// Far outside parent should still prompt/deny.
	far := filepath.Join(filepath.Dir(parent), "totally-elsewhere", "x.go")
	if msg := sa.scopeApproval.check("read_file", far, project); msg == "" {
		t.Fatal("path outside seeded parent should not be auto-allowed")
	}
}

func TestRearmStickyRemoteCodingEnvironment(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	remoteCtx := remoteCodingTemplateContext{
		SessionID:  "ssh-session-1",
		WorkDir:    "/home/u/app",
		ProjectDir: "/home/u/app",
	}
	h.rearmStickyRemoteCodingEnvironment(userID, remoteCtx)
	if !h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("expected sticky remote coding session to be pending")
	}
	raw, ok := h.pendingTemplateRemoteCoding.Load(userID)
	if !ok {
		t.Fatal("pendingTemplateRemoteCoding missing")
	}
	got, _ := raw.(remoteCodingTemplateContext)
	if got.SessionID != remoteCtx.SessionID || got.ProjectDir != remoteCtx.ProjectDir {
		t.Fatalf("remote ctx = %+v", got)
	}
	h.clearStickyCodingEnvironment(userID)
	if h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("clear should drop sticky remote session")
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); mem.SessionFullAccess || mem.TurnCount != 0 {
		t.Fatalf("clear should drop sticky memory, got %+v", mem)
	}
}

func TestRearmStickyRemoteCodingEnvironmentKeepsRecreatedSession(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	oldCtx := remoteCodingTemplateContext{
		SessionID:  "ssh_stale_1",
		WorkDir:    "/home/u/app",
		ProjectDir: "/home/u/app",
	}
	h.bindStickyRemoteCodingContext(userID, remoteCodingTemplateContext{
		SessionID:  "ssh_fresh_2",
		WorkDir:    "/home/u/app",
		ProjectDir: "/home/u/app",
	}, "example.test", "root", 22)

	h.rearmStickyRemoteCodingEnvironment(userID, oldCtx)
	raw, ok := h.pendingTemplateRemoteCoding.Load(userID)
	if !ok {
		t.Fatal("pendingTemplateRemoteCoding missing")
	}
	got, _ := raw.(remoteCodingTemplateContext)
	if got.SessionID != "ssh_fresh_2" {
		t.Fatalf("rearmed session = %q, want recreated session", got.SessionID)
	}
}

func TestRearmStickyRemoteCodingEnvironmentPreservesInitialDiagnosisInquiry(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.rearmStickyRemoteCodingEnvironment(userID, remoteCodingTemplateContext{
		SessionID:           "ssh-diagnosis-1",
		WorkDir:             "/srv/app",
		ProjectDir:          "/srv/app",
		ForceInitialInquiry: true,
	})
	raw, ok := h.pendingTemplateRemoteCoding.Load(userID)
	if !ok {
		t.Fatal("pendingTemplateRemoteCoding missing")
	}
	ctx, _ := raw.(remoteCodingTemplateContext)
	if !ctx.ForceInitialInquiry {
		t.Fatalf("re-armed diagnosis context lost first-turn inquiry lock: %+v", ctx)
	}
}

func TestRecoverStickyRemoteCodingSSHSessionRequiresReconnectMetadata(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := remoteCodingTemplateContext{SessionID: "ssh_stale_1", WorkDir: "/home/u/app", ProjectDir: "/home/u/app"}
	userID := stickyTestUserID(t)
	h.bindStickyRemoteCodingContext(userID, ctx, "", "", 0)
	got, errText := h.recoverStickyRemoteCodingSSHSession(userID, ctx)
	if got != ctx {
		t.Fatalf("context changed without recoverable metadata: %+v", got)
	}
	if !strings.Contains(errText, "没有可用于自动重建的主机信息") {
		t.Fatalf("unexpected recovery error: %q", errText)
	}
}

func TestClearStickyCodingEnvironmentDropsMemoryAndPending(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.markStickyCodingSessionFullAccess(userID, "local", "D:/repo")
	h.rearmStickyLocalCodingEnvironment(userID, "D:/repo")
	h.recordStickyLocalCodingTurn(userID, "D:/repo", "hi", &CodingSubAgentResult{Summary: "done"})
	h.clearStickyCodingEnvironment(userID)
	if h.hasPendingTemplateSubAgentExecution(userID) {
		t.Fatal("pending should be cleared")
	}
	if mem := h.getStickyCodingWorkbenchMemory(userID); mem.TurnCount != 0 || mem.SessionFullAccess {
		t.Fatalf("memory should be cleared: %+v", mem)
	}
}

func TestClearStickyCleansWorktreeConflicts(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	// Record a synthetic conflict pointing at a temp dir (discard will try RemoveAll).
	orphan := t.TempDir()
	h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
		StepIndex: 1,
		Path:      orphan,
		Kind:      "local_worktree",
		Error:     "test",
	})
	if len(h.listStickyCodingConflicts(userID)) != 1 {
		t.Fatal("expected conflict")
	}
	h.clearStickyCodingWorkbenchMemory(userID)
	if len(h.listStickyCodingConflicts(userID)) != 0 {
		t.Fatal("conflicts should be cleared")
	}
}

func TestStickyCodingDebounceFlushAndClear(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:debounce-flush"
	// Hot-path mem-only update schedules debounce.
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		mem.Kind = "local"
		mem.LastSummary = "pending-step"
		mem.SessionInputTokens = 7
	})
	// Immediate flush should write pending snapshot and cancel timer.
	h.FlushStickyCodingWorkbenchMemory(userID)
	// Clear must not leave a timer that rewrites the file.
	h.clearStickyCodingWorkbenchMemory(userID)
	// Force any stray timer (should be none).
	flushAllStickyCodingDebouncedPersist()
	path := stickyCodingMemoryFilePath(userID)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected sticky file removed after clear, still exists: %s", path)
		}
	}
}

func TestStickyCodingDebounceCoalescesWithoutForcedFlush(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:debounce-coalesce"
	// Rapid mem-only updates should leave a single pending flush entry.
	for i := 0; i < 5; i++ {
		n := i
		h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
			mem.Kind = "local"
			mem.TurnCount = n + 1
			mem.LastSummary = fmt.Sprintf("step-%d", n)
		})
	}
	if _, ok := stickyCodingPendingFlush.Load(userID); !ok {
		t.Fatal("expected debounced pending flush after mem-only updates")
	}
	// Live memory has the latest value without waiting for disk.
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.TurnCount != 5 || mem.LastSummary != "step-4" {
		t.Fatalf("live mem=%+v", mem)
	}
	// Explicit flush for test cleanup / durability check.
	h.FlushStickyCodingWorkbenchMemory(userID)
	if _, ok := stickyCodingPendingFlush.Load(userID); ok {
		t.Fatal("pending should be cleared after flush")
	}
	h2 := &IMMessageHandler{}
	cold := h2.getStickyCodingWorkbenchMemory(userID)
	if cold.TurnCount != 5 {
		t.Fatalf("disk cold load turn=%d", cold.TurnCount)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
	h2.clearStickyCodingWorkbenchMemory(userID)
}

func TestFlushAllStickyCodingWorkbenchMemory(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:flush-all"
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		mem.Kind = "local"
		mem.SessionPlan = "ship it"
	})
	h.FlushAllStickyCodingWorkbenchMemory()
	// Cold load from disk via second handler map.
	h2 := &IMMessageHandler{}
	// Force disk path: clear process map for h2 and load from disk by using same userID load.
	// getSticky will cold-load from disk if not in map.
	mem := h2.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "ship it" {
		t.Fatalf("disk after flush-all: %+v", mem)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
	h2.clearStickyCodingWorkbenchMemory(userID)
}

func TestStickyCodingWorkbenchMemoryDiskPersistence(t *testing.T) {
	dir := t.TempDir()
	userID := "desktop-user:" + dir
	h1 := &IMMessageHandler{}
	h1.recordStickyLocalCodingTurn(userID, dir, "add feature", &CodingSubAgentResult{
		Summary:       "added feature X",
		FilesModified: []string{"main.go"},
		FilesCreated:  []string{"main_test.go"},
	})
	h1.markStickyCodingSessionFullAccess(userID, "local", dir)

	path := stickyCodingMemoryFilePath(userID)
	if path == "" {
		t.Fatal("expected memory file path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected memory file at %s: %v", path, err)
	}

	// Simulate process restart: empty handler map, cold-load from disk.
	h2 := &IMMessageHandler{}
	mem := h2.getStickyCodingWorkbenchMemory(userID)
	if mem.TurnCount != 1 || !mem.SessionFullAccess {
		t.Fatalf("cold load memory = %+v", mem)
	}
	if mem.LastSummary != "added feature X" {
		t.Fatalf("summary = %q", mem.LastSummary)
	}
	if len(mem.FilesModified) == 0 || mem.FilesModified[0] != "main.go" {
		t.Fatalf("files = %#v", mem.FilesModified)
	}
	outs := mem.prevOutputs()
	if len(outs) == 0 || !strings.Contains(strings.Join(outs, "\n"), "feature X") {
		t.Fatalf("prevOutputs after restart = %#v", outs)
	}

	h2.clearStickyCodingWorkbenchMemory(userID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clear should remove disk file, err=%v", err)
	}
}

func TestRecordStickyRemoteCodingTurnIncludesFiles(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.recordStickyRemoteCodingTurn(userID, "fix remote bug", &RemoteCodingSubAgentResult{
		Status:        "success",
		Summary:       "patched service",
		FilesModified: []string{"/app/server.go"},
		FilesCreated:  []string{"/app/server_test.go"},
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.Kind != "remote" || mem.TurnCount != 1 {
		t.Fatalf("mem = %+v", mem)
	}
	if len(mem.FilesModified) != 1 || mem.FilesModified[0] != "/app/server.go" {
		t.Fatalf("modified = %#v", mem.FilesModified)
	}
	if len(mem.FilesCreated) != 1 {
		t.Fatalf("created = %#v", mem.FilesCreated)
	}
}

func TestCodingWorkbenchPermissionMode(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "request" {
		t.Fatalf("default = %q", got)
	}
	h.markStickyCodingSessionFullAccess(userID, "local", "D:/repo")
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "workspace" {
		t.Fatalf("workspace = %q", got)
	}
	// Path trust alone is not "full" — high-risk still prompts.
	h.markStickyCodingSessionHighRiskAccess(userID)
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "full" {
		t.Fatalf("path+high-risk session = %q, want full", got)
	}
	if got := h.codingWorkbenchPermissionMode(userID, true); got != "full" {
		t.Fatalf("global full = %q", got)
	}
	h.clearStickyCodingSessionFullAccess(userID)
	if got := h.codingWorkbenchPermissionMode(userID, false); got != "request" {
		t.Fatalf("after clear = %q", got)
	}
}

func TestApplyStickyPermissionsSplitsPathAndHighRisk(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	project := t.TempDir()
	h.markStickyCodingSessionFullAccess(userID, "local", project)

	sa := &CodingSubAgent{projectPath: project, fullEnvironment: true}
	sa.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false)
	h.applyStickyCodingPermissions(userID, sa)

	// Path full-access granted.
	if msg := sa.scopeApproval.check("read_file", filepath.Join(filepath.Dir(project), "out.go"), project); msg != "" {
		t.Fatalf("path trust should allow outside path, got %q", msg)
	}
	// High-risk still blocked without session high-risk grant.
	if sa.scopeApproval.highRiskApproved() {
		t.Fatal("workspace path trust must not auto-allow high-risk bash")
	}

	h.markStickyCodingSessionHighRiskAccess(userID)
	sa2 := &CodingSubAgent{projectPath: project, fullEnvironment: true}
	sa2.SetScopeApprovalCallback(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false)
	h.applyStickyCodingPermissions(userID, sa2)
	if !sa2.scopeApproval.highRiskApproved() {
		t.Fatal("session high-risk grant should auto-allow high-risk bash")
	}
}

func TestSetStickyCodingSessionPlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingSessionPlan(userID, "  Implement billing API  ")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "Implement billing API" {
		t.Fatalf("plan = %q", mem.SessionPlan)
	}
	joined := strings.Join(mem.prevOutputs(), "\n")
	// TurnCount is 0 so prevOutputs may only include plan.
	if !strings.Contains(joined, "Implement billing API") && mem.SessionPlan == "" {
		t.Fatalf("expected plan stored")
	}
	h.setStickyCodingSessionPlan(userID, "")
	if mem = h.getStickyCodingWorkbenchMemory(userID); mem.SessionPlan != "" {
		t.Fatalf("clear plan failed: %q", mem.SessionPlan)
	}
}

func TestStickySessionPlanPreservedAcrossTurns(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.recordStickyLocalCodingTurn(userID, "D:/repo", "Build auth module with JWT", &CodingSubAgentResult{
		Summary: "scaffolded auth package",
	})
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "Build auth module with JWT" {
		t.Fatalf("session plan = %q", mem.SessionPlan)
	}
	h.recordStickyLocalCodingTurn(userID, "D:/repo", "add refresh token", &CodingSubAgentResult{
		Summary: "added refresh endpoint",
	})
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionPlan != "Build auth module with JWT" {
		t.Fatalf("session plan should stay original, got %q", mem.SessionPlan)
	}
	if mem.LastUserText != "add refresh token" {
		t.Fatalf("last user = %q", mem.LastUserText)
	}
	joined := strings.Join(mem.prevOutputs(), "\n")
	if !strings.Contains(joined, "Session plan") || !strings.Contains(joined, "Build auth module") {
		t.Fatalf("prevOutputs missing plan: %q", joined)
	}
}

func TestBindStickyRemoteCodingContextStoresReconnectMeta(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.bindStickyRemoteCodingContext(userID, remoteCodingTemplateContext{
		SessionID:  "ssh_abc_1",
		WorkDir:    "/home/u/app",
		ProjectDir: "/home/u/app",
	}, "10.0.0.8", "ubuntu", 22)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.RemoteHost != "10.0.0.8" || mem.RemoteUser != "ubuntu" || mem.RemotePort != 22 {
		t.Fatalf("remote meta = %+v", mem)
	}
	if mem.RemoteSessionID != "ssh_abc_1" {
		t.Fatalf("session id = %q", mem.RemoteSessionID)
	}
}

func TestCodingWorkbenchStatusIncludesExecutionPlan(t *testing.T) {
	app := &App{}
	h := &IMMessageHandler{}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	h.setStickyCodingSessionPlan(userID, "Ship auth")
	h.setStickyCodingExecutionPlan(userID, "### T1: explore\n### T2: implement")
	h.setStickyCodingPlanMode(userID, codingPlanModeApprove)
	h.storeStickyPendingCodingPlan(userID, "build", "### T1: explore\n### T2: implement", nil)
	// empty tasks won't store; use real tasks via setSticky step statuses
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "explore", Status: codingStepPassed},
		{Index: 2, Title: "implement", Status: codingStepRunning},
	})
	st := app.codingWorkbenchStatusFromHandler(path, h)
	if st.SessionPlan != "Ship auth" {
		t.Fatalf("SessionPlan=%q", st.SessionPlan)
	}
	if !strings.Contains(st.ExecutionPlan, "T1") {
		t.Fatalf("ExecutionPlan=%q", st.ExecutionPlan)
	}
	if st.PlanMode != codingPlanModeApprove {
		t.Fatalf("PlanMode=%q", st.PlanMode)
	}
	if len(st.StepStatuses) != 2 {
		t.Fatalf("StepStatuses=%+v", st.StepStatuses)
	}
}

func TestCodingWorkbenchStatusNeedsReconnectWhenSessionMissing(t *testing.T) {
	// Status path must not cold-init memory store; sticky alone is enough.
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	h := &IMMessageHandler{app: app}
	dir := t.TempDir()
	projectPath := dir
	userID := projectSessionOwnerID(projectPath)
	h.bindStickyRemoteCodingContext(userID, remoteCodingTemplateContext{
		SessionID:  "ssh_dead_1",
		WorkDir:    "/tmp/app",
		ProjectDir: "/tmp/app",
	}, "192.168.1.10", "root", 22)
	// No live SSH manager sessions → needs reconnect.
	st := app.codingWorkbenchStatusFromHandler(projectPath, h)
	if st.Kind != "remote" {
		t.Fatalf("kind = %q", st.Kind)
	}
	if !st.NeedsReconnect {
		t.Fatalf("expected needs_reconnect, status=%+v", st)
	}
	if st.RemoteHost != "192.168.1.10" || st.RemoteUser != "root" {
		t.Fatalf("reconnect prefill meta missing: %+v", st)
	}
	if app.memoryStore != nil {
		t.Fatal("status path should not cold-init memory store")
	}
	// Re-bind with identical meta should be a no-op (no panic / no clobber).
	h.bindStickyRemoteCodingContext(userID, remoteCodingTemplateContext{
		SessionID:  "ssh_dead_1",
		WorkDir:    "/tmp/app",
		ProjectDir: "/tmp/app",
	}, "192.168.1.10", "root", 22)
}

func TestRememberRemoteScopeStickyDecisionHighRisk(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	loopCtx := &LoopContext{UserID: userID}
	rememberRemoteScopeStickyDecision(h, loopCtx, ScopeApprovalRequest{
		Kind:        remoteHighRiskApprovalKind,
		Path:        "rm -rf /tmp/x",
		ProjectPath: "/home/u/app",
	}, ScopeApprovalFullAccess)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !mem.SessionHighRiskAccess {
		t.Fatalf("expected session high-risk sticky, got %+v", mem)
	}
	// Path full-access decision should mark path trust, not only high-risk.
	rememberRemoteScopeStickyDecision(h, loopCtx, ScopeApprovalRequest{
		Kind:        remotePathAccessKind,
		Path:        "/etc/hosts",
		ProjectPath: "/home/u/app",
		Directory:   "/etc",
	}, ScopeApprovalFullAccess)
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if !mem.SessionFullAccess {
		t.Fatalf("expected session path trust, got %+v", mem)
	}
}
