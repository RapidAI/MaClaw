package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestCodingTaskRelationCreatesDistinctSemanticRootAndTurn(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateCodingTask(subject, time.Unix(1_700_000_000, 0), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !created.complete() || created.principalID == created.sessionID {
		t.Fatalf("new task handle is incomplete or collapsed: %#v", created)
	}
	second, err := service.CreateCodingTask(subject, time.Unix(1_700_000_001, 0), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if second.rootTaskID == created.rootTaskID || second.turnID == created.turnID {
		t.Fatalf("new coding tasks shared semantic identifiers: first=%#v second=%#v", created, second)
	}
}

func TestCodingTaskRelationContinuationConsumesHandleAndPreservesRoot(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	first, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.VerifyCodingContinuation(subject, first, now.Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if next.rootTaskID != first.rootTaskID || next.turnID == first.turnID || next.handleID == first.handleID {
		t.Fatalf("invalid continuation lineage: first=%#v next=%#v", first, next)
	}
	if _, err := service.VerifyCodingContinuation(subject, first, now.Add(2*time.Minute), time.Hour); !errors.Is(err, errCodingTaskHandleConsumed) {
		t.Fatalf("replayed continuation error=%v, want consumed", err)
	}
}

func TestCodingTaskRelationRejectsCrossSessionForgeryAndExpiry(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	handle, err := service.CreateCodingTask(subject, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyCodingContinuation(foreign, handle, now.Add(time.Second), time.Hour); !errors.Is(err, errCodingTaskHandleForbidden) {
		t.Fatalf("cross-session continuation error=%v, want forbidden", err)
	}
	if _, err := service.VerifyCodingContinuation(subject, handle, now.Add(2*time.Minute), time.Hour); !errors.Is(err, errCodingTaskHandleExpired) {
		t.Fatalf("expired continuation error=%v, want expired", err)
	}
}

func TestCodingTaskRelationPersistsAndRevocationSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relations.db")
	service, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	handle, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCodingTaskHandle(subject, handle, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.VerifyCodingContinuation(subject, handle, now.Add(2*time.Minute), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("revoked persisted handle continuation error=%v, want revoked", err)
	}
}

func TestVerifiedCodingSubjectRejectsCollapsedPrincipalAndSession(t *testing.T) {
	if _, err := newVerifiedCodingSubject("tenant-a", "same", "same"); !errors.Is(err, errVerifiedCodingSubjectInvalid) {
		t.Fatalf("collapsed principal/session error=%v, want invalid", err)
	}
}

func TestCodingTaskRelationBindsOnlyVerifiedHandleToOneRuntimeAttempt(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	handle, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	request := codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}
	identity, err := service.BindCodingAttempt(subject, handle, store, request, now)
	if err != nil {
		t.Fatal(err)
	}
	if identity.RootTaskID != handle.rootTaskID || identity.TurnID != handle.turnID {
		t.Fatalf("bound identity %#v does not match verified handle %#v", identity, handle)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, request, now.Add(time.Second)); err != nil {
		t.Fatalf("same attempt binding must be idempotent: %v", err)
	}
	otherTask, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "other"})
	if err != nil {
		t.Fatal(err)
	}
	otherAttempt, err := store.StartAttempt(otherTask.TaskID, "owner", time.Minute, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, codingruntime.ExecutionRequest{Task: *otherTask, Attempt: *otherAttempt}, now); !errors.Is(err, errCodingTaskHandleConsumed) {
		t.Fatalf("second attempt binding error=%v, want consumed", err)
	}
}

func TestBindVerifiedCodingTaskHandleOnlyResolvesDurableAnchor(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handle, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	request := codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}
	identity, ok := bindVerifiedCodingTaskHandle(service, subject, &handle, store, request)
	if !ok || identity == nil || identity.RootTaskID != handle.rootTaskID || identity.TurnID != handle.turnID {
		t.Fatalf("bound identity=%#v ok=%v", identity, ok)
	}
	resolved, ok := resolveTrustedCodingInvocationIdentity(store, request)
	if !ok || resolved == nil || *resolved != *identity {
		t.Fatalf("durable resolved identity=%#v ok=%v", resolved, ok)
	}
}

func TestCodingTaskRelationChildTurnSharesRootButNeverParentTurn(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	parent, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	childA, err := service.IssueChildCodingTurn(subject, parent, now.Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	childB, err := service.IssueChildCodingTurn(subject, parent, now.Add(2*time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if childA.rootTaskID != parent.rootTaskID || childA.turnID == parent.turnID || childA.handleID == parent.handleID {
		t.Fatalf("child A lineage invalid: parent=%#v child=%#v", parent, childA)
	}
	if childB.turnID == childA.turnID || childB.handleID == childA.handleID || childB.rootTaskID != parent.rootTaskID {
		t.Fatalf("children do not have distinct turns: A=%#v B=%#v", childA, childB)
	}
	// Child issuance must not consume the parent: it may later be explicitly
	// continued after all admitted children have returned.
	if _, err := service.VerifyCodingContinuation(subject, parent, now.Add(3*time.Minute), time.Hour); err != nil {
		t.Fatalf("parent continuation after child issuance: %v", err)
	}
}

func TestCodingTaskRelationRevocationFencesActiveChildLineage(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	parent, err := service.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.IssueChildCodingTurn(subject, parent, now.Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeCodingTaskHandle(subject, parent, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyCodingContinuation(subject, child, now.Add(3*time.Minute), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("child continuation after parent cancellation error=%v, want revoked", err)
	}
}

func TestDesktopCodingTaskIngressIsOwnerBoundAndContinuesSameRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	firstToken := app.beginDesktopCodingTaskIngress(owner)
	if firstToken == "" {
		t.Fatal("desktop ingress token was not issued")
	}
	service, subject, first, ok := app.nextDesktopCodingTaskRelation(firstToken, owner)
	if !ok || service == nil || !first.complete() {
		t.Fatalf("first desktop relation service=%v subject=%#v handle=%#v ok=%v", service, subject, first, ok)
	}
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(firstToken, desktopUserID+":C:/workspace/b"); ok {
		t.Fatal("ingress token crossed desktop owners")
	}
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(firstToken, owner); ok {
		t.Fatal("desktop ingress token was reusable after issuing a relation")
	}
	secondToken := app.beginDesktopCodingTaskIngress(owner)
	_, secondSubject, second, ok := app.nextDesktopCodingTaskRelation(secondToken, owner)
	if !ok || second.rootTaskID != first.rootTaskID || second.turnID == first.turnID || secondSubject.sessionID != subject.sessionID {
		t.Fatalf("desktop continuation first=%#v second=%#v first_subject=%#v second_subject=%#v ok=%v", first, second, subject, secondSubject, ok)
	}
	app.endDesktopCodingTaskIngress(firstToken)
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(firstToken, owner); ok {
		t.Fatal("ended desktop ingress token remained usable")
	}
}

func TestDesktopCodingTaskIngressCarriesOnlyHostIssuedLocalWorkspaceBinding(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	ownerA := projectSessionOwnerID("C:/workspace/a")
	ownerB := projectSessionOwnerID("C:/workspace/b")
	tokenA := app.beginDesktopCodingTaskIngressWithWorkspace(ownerA, workspaceA)
	if tokenA == "" {
		t.Fatal("workspace-bound ingress token was not issued")
	}
	_, _, _, bindingA, ok := app.nextDesktopCodingTaskRelationWithWorkspace(tokenA, ownerA)
	if !ok || !bindingA.complete() {
		t.Fatalf("workspace-bound relation binding=%#v ok=%v", bindingA, ok)
	}
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(ownerA, bindingA); !ok || resolved != workspaceA {
		t.Fatalf("owner A resolved workspace=%q ok=%v, want %q", resolved, ok, workspaceA)
	}
	if _, ok := app.resolveDesktopCodingStaticWorkspace(ownerB, bindingA); ok {
		t.Fatal("workspace binding crossed desktop owners")
	}
	tokenB := app.beginDesktopCodingTaskIngressWithWorkspace(ownerB, workspaceB)
	_, _, _, bindingB, ok := app.nextDesktopCodingTaskRelationWithWorkspace(tokenB, ownerB)
	if !ok || !bindingB.complete() || bindingB.WorkspaceHandle == bindingA.WorkspaceHandle {
		t.Fatalf("workspace B binding=%#v A=%#v ok=%v", bindingB, bindingA, ok)
	}
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(ownerB, bindingB); !ok || resolved != workspaceB {
		t.Fatalf("owner B resolved workspace=%q ok=%v, want %q", resolved, ok, workspaceB)
	}
}

func TestDesktopCodingTaskIngressRejectsUnverifiedWorkspaceDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	token := app.beginDesktopCodingTaskIngressWithWorkspace(owner, filepath.Join(t.TempDir(), "missing"))
	_, _, _, binding, ok := app.nextDesktopCodingTaskRelationWithWorkspace(token, owner)
	if !ok {
		t.Fatal("identity relation should remain available without a static workspace")
	}
	if binding.complete() {
		t.Fatalf("unverified directory produced a workspace binding: %#v", binding)
	}
}

func TestDesktopCodingTaskRelationRevocationDropsFutureIngress(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := desktopUserID + ":C:/workspace/a"
	token := app.beginDesktopCodingTaskIngress(owner)
	_, _, first, ok := app.nextDesktopCodingTaskRelation(token, owner)
	if !ok {
		t.Fatal("first desktop relation was not issued")
	}
	app.revokeDesktopCodingTaskRelation(owner)
	service, subject, next, ok := app.nextDesktopCodingTaskRelation(app.beginDesktopCodingTaskIngress(owner), owner)
	if !ok || service == nil || next.rootTaskID == first.rootTaskID || subject.sessionID == "" {
		t.Fatalf("revoked desktop relation did not start independent task: first=%#v next=%#v subject=%#v ok=%v", first, next, subject, ok)
	}
}

func TestDesktopCodingTaskIngressForNewTaskFencesPriorRelationAndPendingTokens(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	service, subject, prior, ok := app.nextDesktopCodingTaskRelation(app.beginDesktopCodingTaskIngress(owner), owner)
	if !ok {
		t.Fatal("prior desktop relation was not issued")
	}
	lateOldToken := app.beginDesktopCodingTaskIngress(owner)
	newToken := app.beginDesktopCodingTaskIngressForRequest(owner, true)
	if newToken == "" {
		t.Fatal("new-task ingress token was not issued")
	}
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(lateOldToken, owner); ok {
		t.Fatal("pre-fence ingress token minted a relation after explicit new task")
	}
	_, nextSubject, next, ok := app.nextDesktopCodingTaskRelation(newToken, owner)
	if !ok || next.rootTaskID == prior.rootTaskID || nextSubject.sessionID == subject.sessionID {
		t.Fatalf("new task did not establish an independent relation: prior=%#v next=%#v subjects=%#v/%#v ok=%v", prior, next, subject, nextSubject, ok)
	}
	if _, err := service.VerifyCodingContinuation(subject, prior, time.Now().UTC(), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("prior relation survived explicit new task: %v", err)
	}
}

func TestDesktopCodingTaskIngressGenerationRejectsConsumedPreFenceToken(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	oldToken := app.beginDesktopCodingTaskIngress(owner)
	app.desktopCodingIngressMu.Lock()
	oldIngress, ok := app.desktopCodingIngress[oldToken]
	delete(app.desktopCodingIngress, oldToken) // model the token's consume phase
	app.desktopCodingIngressMu.Unlock()
	if !ok {
		t.Fatal("old ingress token was not recorded")
	}
	// The host starts a new task while the old request is between consume and
	// runtime binding. Reinsert only as a test model of the already-copied
	// request capability: its old generation must still be rejected.
	newToken := app.beginDesktopCodingTaskIngressForRequest(owner, true)
	app.desktopCodingIngressMu.Lock()
	app.desktopCodingIngress[oldToken] = oldIngress
	app.desktopCodingIngressMu.Unlock()
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(oldToken, owner); ok {
		t.Fatal("consumed pre-fence token bound a relation after new-task fence")
	}
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(newToken, owner); !ok {
		t.Fatal("post-fence token did not bind the new task relation")
	}
}

func TestProjectTaskCancellationRevokesDesktopCodingRelationBeforeLoopLookup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	projectPath := "C:/workspace/a"
	owner := projectSessionOwnerID(projectPath)
	token := app.beginDesktopCodingTaskIngress(owner)
	service, subject, handle, ok := app.nextDesktopCodingTaskRelation(token, owner)
	if !ok {
		t.Fatal("desktop relation was not issued")
	}
	// No IM handler is intentionally installed: lifecycle fencing must not
	// depend on finding an active loop before it revokes the durable handle.
	app.cancelProjectTaskLoop(projectPath)
	if _, err := service.VerifyCodingContinuation(subject, handle, time.Now().UTC(), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("project cancellation did not revoke relation: %v", err)
	}
}

func TestCancelAIAssistantSessionForOwnerRevokesResolvedDesktopRelation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	service, subject, handle, ok := app.nextDesktopCodingTaskRelation(app.beginDesktopCodingTaskIngress(owner), owner)
	if !ok {
		t.Fatal("desktop relation was not issued")
	}
	loop := NewLoopContext("project", 1, nil)
	handler := &IMMessageHandler{lastUserID: owner, currentLoopCtx: loop}
	handler.setSessionLoopCtx(owner, loop)
	go func() {
		<-loop.CancelC
		loop.Done()
	}()
	if _, err := app.cancelAIAssistantSessionForOwner(handler, owner); err != nil {
		t.Fatalf("cancel desktop session: %v", err)
	}
	if _, err := service.VerifyCodingContinuation(subject, handle, time.Now().UTC(), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("desktop cancel did not revoke relation: %v", err)
	}
}

func TestClearAIAssistantHistoryFencesRelationWithoutIMHandler(t *testing.T) {
	// ClearAIAssistantHistoryForSession is intentionally allowed to construct
	// the IM runtime after it fences the relation. Reuse the app test helper so
	// its SQLite-backed memory store and scheduled workers are stopped before
	// TempDir cleanup on Windows.
	app := newProjectSearchTestApp(t)
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	service, subject, handle, ok := app.nextDesktopCodingTaskRelation(app.beginDesktopCodingTaskIngress(owner), owner)
	if !ok {
		t.Fatal("desktop relation was not issued")
	}
	if err := app.ClearAIAssistantHistoryForSession(owner); err != nil {
		t.Fatalf("clear desktop session: %v", err)
	}
	if _, err := service.VerifyCodingContinuation(subject, handle, time.Now().UTC(), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("clear without IM handler did not revoke relation: %v", err)
	}
}
