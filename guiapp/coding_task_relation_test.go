package guiapp

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// Test-only consumption helpers. Production coding dispatch must use the
// root-step chain paths; these helpers model a bare single-token consumption
// without making that shortcut available to production code.
func (a *App) nextDesktopCodingTaskRelation(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, bool) {
	service, subject, handle, _, ok := a.nextDesktopCodingTaskRelationWithWorkspace(token, ownerID)
	return service, subject, handle, ok
}

func (a *App) nextDesktopCodingTaskRelationWithWorkspace(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	if a == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	service, subject, handle, workspace, _, ok := a.codingCaps.nextRelationWithWorkspaceAndIngress(a.GetDataDir(), token, ownerID)
	return service, subject, handle, workspace, ok
}

// beginDesktopCodingTaskIngressWithWorkspaceForTest builds a workspace-bound
// ingress with the same authority ordering production requires: mint a bare
// request token first, then bind only an already materialized execution surface.
func beginDesktopCodingTaskIngressWithWorkspaceForTest(app *App, ownerID, workspaceDir string) string {
	token := app.beginDesktopCodingTaskIngress(ownerID)
	if token != "" && strings.TrimSpace(workspaceDir) != "" {
		app.codingCaps.bindExecutionWorkspace(ownerID, token, workspaceDir)
	}
	return token
}

// bindExecutionWorkspace is a test-only convenience over the issuer's locked
// binder. Production dispatch must use nextRootStepRelationForExecution so the
// bind and single-use consumption remain one issuer-critical section.
func (i *desktopCodingCapabilityIssuer) bindExecutionWorkspace(ownerID, presentedToken, workspaceDir string) bool {
	ownerID = strings.TrimSpace(ownerID)
	presentedToken = strings.TrimSpace(presentedToken)
	if i == nil || !isDesktopCodingOwner(ownerID) || presentedToken == "" {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	_, ok := i.bindExecutionWorkspaceLocked(ownerID, presentedToken, workspaceDir, time.Now().UTC())
	return ok
}

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

func TestCodingTaskRelationCloseRetiresActiveHandlesDurable(t *testing.T) {
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
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.VerifyCodingContinuation(subject, handle, now.Add(time.Minute), time.Hour); !errors.Is(err, errCodingTaskHandleRevoked) {
		t.Fatalf("retired handle continuation error=%v, want revoked", err)
	}
}

func TestVerifiedCodingSubjectRejectsCollapsedPrincipalAndSession(t *testing.T) {
	if _, err := newVerifiedCodingSubject("tenant-a", "same", "same"); !errors.Is(err, errVerifiedCodingSubjectInvalid) {
		t.Fatalf("collapsed principal/session error=%v, want invalid", err)
	}
}

func TestCodingTaskRelationServiceUsesOneSQLiteConnection(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if got := service.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	var foreignKeys int
	if err := service.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestCodingTaskRelationServiceUsesImmediateTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relations.db")
	service, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	tx, err := service.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(50)&_txlock=immediate")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	defer other.Close()
	other.SetMaxOpenConns(1)
	conflicting, err := other.Begin()
	if err == nil {
		_ = conflicting.Rollback()
		_ = tx.Rollback()
		t.Fatal("a second immediate transaction started while the relation transaction was open; relation transactions are not immediate")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	conflicting, err = other.Begin()
	if err != nil {
		t.Fatalf("immediate transaction after relation rollback: %v", err)
	}
	if err := conflicting.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestCodingTaskRelationConcurrentContinuationAcrossServiceInstancesAllowsOneConsumer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relations.db")
	firstService, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer firstService.Close()
	secondService, err := newCodingTaskRelationService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer secondService.Close()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	handle, err := firstService.CreateCodingTask(subject, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		err error
	}
	results := make(chan result, 2)
	for _, service := range []*codingTaskRelationService{firstService, secondService} {
		go func(service *codingTaskRelationService) {
			_, err := service.VerifyCodingContinuation(subject, handle, now.Add(time.Minute), time.Hour)
			results <- result{err: err}
		}(service)
	}
	first, second := <-results, <-results
	successes, consumed := 0, 0
	for _, result := range []result{first, second} {
		switch {
		case result.err == nil:
			successes++
		case errors.Is(result.err, errCodingTaskHandleConsumed):
			consumed++
		default:
			t.Fatalf("unexpected concurrent continuation error: %v", result.err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("concurrent continuations: successes=%d consumed=%d, want exactly one each", successes, consumed)
	}
}

func TestRelationServiceDataDirResetFencesCapabilitiesAndReopens(t *testing.T) {
	issuer := &desktopCodingCapabilityIssuer{}
	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")
	ownerID := desktopUserID
	token := issuer.beginIngress(ownerID)
	if token == "" {
		t.Fatal("ingress token unavailable")
	}
	first, err := issuer.relationService(firstDir)
	if err != nil {
		t.Fatal(err)
	}
	issuer.resetRelationServiceForDataDirChange()
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateCodingTask(subject, time.Now().UTC(), time.Hour); err == nil {
		t.Fatal("retired relation service still accepts task creation")
	}
	if service, _, _, _, _, ok := issuer.nextRelationWithWorkspaceAndIngress(secondDir, token, ownerID); ok || service != nil {
		t.Fatalf("pre-reset ingress survived data-dir reset: service=%#v ok=%v", service, ok)
	}
	second, err := issuer.relationService(secondDir)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second == first {
		t.Fatalf("relation service was not replaced after data-dir reset: first=%#v second=%#v", first, second)
	}
	defer second.Close()
}

func TestRelationServiceDataDirResetFencesConsumedInFlightOwner(t *testing.T) {
	issuer := &desktopCodingCapabilityIssuer{}
	ownerID := desktopUserID
	token := issuer.beginIngress(ownerID)
	if token == "" {
		t.Fatal("ingress token unavailable")
	}
	issuer.mu.Lock()
	delete(issuer.ingress, token)
	issuer.mu.Unlock()
	issuer.resetRelationServiceForDataDirChange()
	issuer.mu.Lock()
	generation := issuer.generations[ownerID]
	issuer.mu.Unlock()
	if generation == 0 {
		t.Fatal("data-dir reset did not advance the generation of a consumed in-flight owner")
	}
}

func TestRelationServiceDataDirMismatchRetiresPreviousService(t *testing.T) {
	issuer := &desktopCodingCapabilityIssuer{}
	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")
	ownerID := desktopUserID
	token := issuer.beginIngress(ownerID)
	first, err := issuer.relationService(firstDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := issuer.relationService(secondDir)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second == first {
		t.Fatalf("relation service did not switch data directories: first=%#v second=%#v", first, second)
	}
	defer second.Close()
	if service, _, _, _, _, ok := issuer.nextRelationWithWorkspaceAndIngress(secondDir, token, ownerID); ok || service != nil {
		t.Fatalf("pre-mismatch ingress survived lazy data-dir replacement: service=%#v ok=%v", service, ok)
	}
	subject, err := newVerifiedCodingSubject("tenant-a", "principal-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateCodingTask(subject, time.Now().UTC(), time.Hour); err == nil {
		t.Fatal("mismatched relation service still accepts task creation")
	}
}

func TestCloseRelationServiceFencesInFlightCriticalSectionAndLateUse(t *testing.T) {
	issuer := &desktopCodingCapabilityIssuer{}
	dataDir := t.TempDir()
	service, err := issuer.relationService(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the service lifecycle mutex as BindCodingAttempt does while it is
	// crossing the relation/runtime stores. Close must wait for that critical
	// section, and a caller must not be able to open a replacement service for
	// the same database while the old one is still inside it.
	service.mu.Lock()
	closeDone := make(chan struct{})
	go func() {
		issuer.closeRelationService()
		close(closeDone)
	}()
	// Once closeRelationService is inside Close it must still own relationsMu.
	// If it drops relationsMu before waiting on service.mu (the old ordering),
	// this TryLock succeeds and a replacement service could open underneath the
	// in-flight cross-store critical section.
	deadline := time.Now().Add(5 * time.Second)
	for issuer.relationsMu.TryLock() {
		issuer.relationsMu.Unlock()
		if time.Now().After(deadline) {
			service.mu.Unlock()
			t.Fatal("close did not retain the issuer relation lock while waiting for the service critical section")
		}
		time.Sleep(time.Millisecond)
	}
	service.mu.Unlock()
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not finish after the in-flight critical section released")
	}
	if replacement, err := issuer.relationService(dataDir); err == nil || replacement != nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("relation service after terminal close replacement=%v err=%v, want closed failure", replacement != nil, err)
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
	var bridgeRows int
	if err := service.db.QueryRow(`SELECT COUNT(1) FROM coding_task_attempt_bindings WHERE handle_id=?`, handle.handleID).Scan(&bridgeRows); err != nil {
		t.Fatal(err)
	}
	if bridgeRows != 0 {
		t.Fatalf("bind wrote %d legacy bridge rows, want runtime anchor as sole authorization commit", bridgeRows)
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

func TestBindCodingAttemptLegacyBridgeRowStillFencesDifferentAttempt(t *testing.T) {
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
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`INSERT INTO coding_task_attempt_bindings(handle_id,runtime_task_id,runtime_attempt_id,bound_at) VALUES(?,?,?,?)`, handle.handleID, "legacy-task", "legacy-attempt", now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}, now); !errors.Is(err, errCodingTaskHandleConsumed) {
		t.Fatalf("legacy bridge mismatch error=%v, want consumed", err)
	}
}

func TestBindCodingAttemptConcurrentAttemptsAreSerialized(t *testing.T) {
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
	policy := codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}
	requests := make([]codingruntime.ExecutionRequest, 0, 2)
	for _, work := range []string{"first", "second"} {
		task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: work})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, policy, now)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt})
	}
	start := make(chan struct{})
	results := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Add(1)
		go func(request codingruntime.ExecutionRequest) {
			defer wg.Done()
			<-start
			_, err := service.BindCodingAttempt(subject, handle, store, request, now)
			results <- err
		}(request)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, consumed := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errCodingTaskHandleConsumed):
			consumed++
		default:
			t.Fatalf("concurrent bind returned unexpected error: %v", err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("concurrent bind outcomes successes=%d consumed=%d, want exactly one winner and one consumed loser", successes, consumed)
	}
}

func TestBindCodingAttemptRejectsTerminalAttemptEvenWhenAnchorAlreadyExists(t *testing.T) {
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
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterSemanticTaskAnchor(codingruntime.SemanticTaskAnchor{
		RuntimeTaskID: task.TaskID, RuntimeAttemptID: attempt.AttemptID,
		TenantID: handle.tenantID, PrincipalID: handle.principalID, SessionID: handle.sessionID,
		RootTaskID: handle.rootTaskID, TurnID: handle.turnID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "owner", codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}, now); !errors.Is(err, codingruntime.ErrInvalidTransition) {
		t.Fatalf("terminal attempt bind error = %v, want ErrInvalidTransition", err)
	}
	var bindings int
	if err := service.db.QueryRow(`SELECT COUNT(1) FROM coding_task_attempt_bindings WHERE handle_id=?`, handle.handleID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("terminal attempt committed %d bridge rows, want 0", bindings)
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
	tokenA := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, ownerA, workspaceA)
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
	tokenB := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, ownerB, workspaceB)
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
	token := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, filepath.Join(t.TempDir(), "missing"))
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

func TestDesktopCodingNewTaskFenceDropsPriorRelationAndPendingTokens(t *testing.T) {
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
	app.revokeDesktopCodingTaskRelation(owner)
	newToken := app.beginDesktopCodingTaskIngress(owner)
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
	app.codingCaps.mu.Lock()
	oldIngress, ok := app.codingCaps.ingress[oldToken]
	delete(app.codingCaps.ingress, oldToken) // model the token's consume phase
	app.codingCaps.mu.Unlock()
	if !ok {
		t.Fatal("old ingress token was not recorded")
	}
	// The host starts a new task while the old request is between consume and
	// runtime binding. Reinsert only as a test model of the already-copied
	// request capability: its old generation must still be rejected.
	app.revokeDesktopCodingTaskRelation(owner)
	newToken := app.beginDesktopCodingTaskIngress(owner)
	app.codingCaps.mu.Lock()
	app.codingCaps.ingress[oldToken] = oldIngress
	app.codingCaps.mu.Unlock()
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

func TestDesktopCodingRootStepRelationChainsContinuationWithinRequest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if requestToken == "" {
		t.Fatal("desktop ingress token was not issued")
	}
	// Step T1 binds with the request token.
	_, _, first, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !first.complete() {
		t.Fatalf("first root step did not bind: handle=%#v ok=%v", first, ok)
	}
	// Step T2 of the same multi-step request presents the same (now consumed)
	// loop-context token: the chained continuation capability must carry it
	// onto the same semantic root with a fresh turn.
	_, _, second, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || second.rootTaskID != first.rootTaskID || second.turnID == first.turnID {
		t.Fatalf("second root step first=%#v second=%#v ok=%v", first, second, ok)
	}
	// Step T3 keeps presenting the original request token from its loop
	// context: any token already consumed on this chain proves the dispatch
	// descends from the verified request and admits exactly one more turn.
	_, _, third, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || third.rootTaskID != first.rootTaskID || third.turnID == second.turnID {
		t.Fatalf("third root step second=%#v third=%#v ok=%v", second, third, ok)
	}
	// Every successful bind re-arms exactly one continuation, so a plan with N
	// steps gets N turns: a fourth step of this same request still binds.
	_, _, fourth, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || fourth.rootTaskID != first.rootTaskID || fourth.turnID == third.turnID {
		t.Fatalf("fourth root step third=%#v fourth=%#v ok=%v", third, fourth, ok)
	}
	// The owner string alone never suffices: a dispatch presenting no token
	// (or a token foreign to this chain) cannot pop the continuation slot
	// even while the chain is armed.
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation("", owner); ok {
		t.Fatal("root step bound on an empty token with an armed chain")
	}
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation("coding-ingress-foreign", owner); ok {
		t.Fatal("root step bound on a foreign token with an armed chain")
	}
	// Without a chain armed by a verified bind, no relation can be created:
	// a different owner whose request token was consumed outside the root-step
	// entry (so no continuation was ever armed) fails closed.
	otherOwner := projectSessionOwnerID("C:/workspace/b")
	otherToken := app.beginDesktopCodingTaskIngress(otherOwner)
	if _, _, _, ok := app.nextDesktopCodingTaskRelation(otherToken, otherOwner); !ok {
		t.Fatal("raw relation bind for the other owner did not succeed")
	}
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(otherToken, otherOwner); ok {
		t.Fatal("root step bound without any armed capability")
	}
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation("", otherOwner); ok {
		t.Fatal("root step bound on an empty token without an armed chain")
	}
	// Once the owning request ends, the armed chain dies with it: even the
	// first owner's pending continuation is gone.
	app.endDesktopCodingTaskIngressForOwner(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); ok {
		t.Fatal("chained continuation survived the end of its request")
	}
}

func TestDesktopCodingRootStepRelationContinuationCarriesWorkspaceBinding(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceDir := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	_, _, _, firstWorkspace, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !firstWorkspace.complete() {
		t.Fatalf("first root step workspace=%#v ok=%v", firstWorkspace, ok)
	}
	_, _, _, secondWorkspace, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || secondWorkspace != firstWorkspace {
		t.Fatalf("continuation workspace first=%#v second=%#v ok=%v", firstWorkspace, secondWorkspace, ok)
	}
}

func TestDesktopCodingRootStepRelationFenceAndRequestEndDropContinuation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")

	// A new-task/cancel fence must invalidate an unused chained continuation.
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("first root step did not bind")
	}
	app.fenceDesktopCodingTaskRelation(owner, true)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); ok {
		t.Fatal("chained continuation survived the new-task fence")
	}

	// Ending the owning request must invalidate an unused chained continuation.
	secondToken := app.beginDesktopCodingTaskIngress(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(secondToken, owner); !ok {
		t.Fatal("root step after fence did not bind")
	}
	app.endDesktopCodingTaskIngressForOwner(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(secondToken, owner); ok {
		t.Fatal("chained continuation survived the end of its request")
	}
}

func TestDesktopCodingRootStepRelationContinuationBudgetCapsChain(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	// One plan step arms 2*1+2 = 4 continuation mints: the request token bind
	// plus four chained binds succeed, the sixth dispatch must fail closed.
	app.armDesktopCodingContinuationBudget(owner, 1)
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	bound := 0
	for i := 0; i < 10; i++ {
		if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
			break
		}
		bound++
	}
	if bound != 5 {
		t.Fatalf("budgeted chain bound %d turns, want 5 (request token + 4 chained)", bound)
	}
}

func TestDesktopCodingContinuationLeaseMatchesRelation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceDir := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	now := time.Now().UTC()
	app.codingCaps.mu.Lock()
	requestExpiry := app.codingCaps.ingress[requestToken].expiresAt
	app.codingCaps.mu.Unlock()
	// Planning runs before the first bind, inside the same request. The
	// request token has to survive that work; teardown, not a short fuse,
	// is what drops it.
	if requestExpiry.Before(now.Add(desktopCodingRequestLease - time.Minute)) {
		t.Fatalf("request token expiry = %s, want the request lease", requestExpiry)
	}
	_, _, first, binding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !first.complete() || !binding.complete() {
		t.Fatalf("first bind handle=%#v workspace=%#v ok=%v", first, binding, ok)
	}
	app.codingCaps.mu.Lock()
	armed := app.codingCaps.continuationTokens[owner]
	continuationExpiry := app.codingCaps.ingress[armed].expiresAt
	workspaceExpiry := app.codingCaps.staticWorkspaces[binding.WorkspaceHandle].expiresAt
	app.codingCaps.mu.Unlock()
	if armed == "" || armed == requestToken {
		t.Fatalf("in-flight continuation was not armed: %q", armed)
	}
	// Minted at bind, consumed at the next step. A fuse shorter than the
	// relation lease expires during the step (T3 ran 34 minutes and T4–T6
	// never started).
	if continuationExpiry.Before(now.Add(desktopCodingRequestLease-time.Minute)) || workspaceExpiry.Before(now.Add(desktopCodingRequestLease-time.Minute)) {
		t.Fatalf("continuation %s workspace %s, want the request lease", continuationExpiry, workspaceExpiry)
	}

	app.codingCaps.mu.Lock()
	staleWorkspace := app.codingCaps.staticWorkspaces[binding.WorkspaceHandle]
	staleWorkspace.expiresAt = now.Add(-time.Minute)
	app.codingCaps.staticWorkspaces[binding.WorkspaceHandle] = staleWorkspace
	app.codingCaps.mu.Unlock()
	if _, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); ok {
		t.Fatal("expired workspace handle still resolved")
	}
	_, _, second, secondBinding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || second.rootTaskID != first.rootTaskID || second.turnID == first.turnID || secondBinding != binding {
		t.Fatalf("next step first=%#v second=%#v workspace=%#v ok=%v", first, second, secondBinding, ok)
	}
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); !ok || resolved != workspaceDir {
		t.Fatalf("bind did not slide the workspace lease: resolved=%q ok=%v", resolved, ok)
	}

	app.codingCaps.mu.Lock()
	nextArmed := app.codingCaps.continuationTokens[owner]
	expired := app.codingCaps.ingress[nextArmed]
	expired.expiresAt = time.Now().UTC().Add(-time.Minute)
	app.codingCaps.ingress[nextArmed] = expired
	app.codingCaps.mu.Unlock()
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); ok {
		t.Fatal("expired continuation bound")
	}
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); !ok || resolved != workspaceDir {
		t.Fatalf("workspace resolved=%q ok=%v before request end", resolved, ok)
	}
	app.endDesktopCodingTaskIngressForOwner(owner)
	if _, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); ok {
		t.Fatal("workspace handle survived the end of its request")
	}
}

func TestDesktopCodingWorkspaceLeaseSlidesWhenContinuationBudgetIsExhausted(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceDir := t.TempDir()
	app.armDesktopCodingContinuationBudget(owner, 1)
	app.codingCaps.mu.Lock()
	app.codingCaps.continuationBudget[owner] = 0
	app.codingCaps.mu.Unlock()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	app.codingCaps.mu.Lock()
	var binding codingStaticWorkspaceBinding
	for handle, workspace := range app.codingCaps.staticWorkspaces {
		if workspace.ownerID != owner {
			continue
		}
		workspace.expiresAt = time.Now().UTC().Add(-time.Minute)
		app.codingCaps.staticWorkspaces[handle] = workspace
		binding = codingStaticWorkspaceBinding{WorkspaceHandle: handle, HostKind: "local"}
	}
	app.codingCaps.mu.Unlock()
	if !binding.complete() {
		t.Fatal("request did not issue a workspace handle")
	}
	if _, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); ok {
		t.Fatal("stale workspace resolved before the bind")
	}
	if _, _, _, got, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok || got != binding {
		t.Fatalf("bind with exhausted budget binding=%#v ok=%v", got, ok)
	}
	app.codingCaps.mu.Lock()
	if armed := app.codingCaps.continuationTokens[owner]; armed != "" {
		t.Fatalf("exhausted budget minted continuation %q", armed)
	}
	app.codingCaps.mu.Unlock()
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); !ok || resolved != workspaceDir {
		t.Fatalf("bind did not slide the workspace lease: resolved=%q ok=%v", resolved, ok)
	}
}

func TestDesktopCodingExpiredContinuationSlotIsClearedOnMint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	other := projectSessionOwnerID("C:/workspace/b")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("first root step did not bind")
	}
	app.codingCaps.mu.Lock()
	armed := app.codingCaps.continuationTokens[owner]
	expired := app.codingCaps.ingress[armed]
	expired.expiresAt = time.Now().UTC().Add(-time.Minute)
	app.codingCaps.ingress[armed] = expired
	app.codingCaps.mu.Unlock()
	if armed == "" {
		t.Fatal("continuation was not armed")
	}
	// Another request's mint sweeps expired ingress. The armed slot has to
	// drop with it; leaving the id in place hands the next step a dead token.
	if otherToken := app.beginDesktopCodingTaskIngress(other); otherToken == "" {
		t.Fatal("other owner ingress was not issued")
	}
	app.codingCaps.mu.Lock()
	_, ingressLeft := app.codingCaps.ingress[armed]
	slot := app.codingCaps.continuationTokens[owner]
	app.codingCaps.mu.Unlock()
	if ingressLeft {
		t.Fatal("expired continuation ingress survived a later mint")
	}
	if slot != "" {
		t.Fatalf("continuation slot still points at swept token %q", slot)
	}
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); ok {
		t.Fatal("swept continuation still bound")
	}
}

func TestDesktopCodingLiveIngressKeepsExpiredWorkspaceForNextSlide(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	other := projectSessionOwnerID("C:/workspace/b")
	workspaceDir := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	app.codingCaps.mu.Lock()
	var binding codingStaticWorkspaceBinding
	for handle, workspace := range app.codingCaps.staticWorkspaces {
		if workspace.ownerID != owner {
			continue
		}
		workspace.expiresAt = time.Now().UTC().Add(-time.Minute)
		app.codingCaps.staticWorkspaces[handle] = workspace
		binding = codingStaticWorkspaceBinding{WorkspaceHandle: handle, HostKind: "local"}
	}
	app.codingCaps.mu.Unlock()
	if !binding.complete() {
		t.Fatal("request did not issue a workspace handle")
	}
	// Another request's mint sweeps expired workspaces. This handle is still
	// named by a live ingress, so the sweep must leave it for the next bind
	// to slide.
	if otherToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, other, t.TempDir()); otherToken == "" {
		t.Fatal("other owner ingress was not issued")
	}
	app.codingCaps.mu.Lock()
	_, kept := app.codingCaps.staticWorkspaces[binding.WorkspaceHandle]
	app.codingCaps.mu.Unlock()
	if !kept {
		t.Fatal("live ingress lost its workspace handle to another request's sweep")
	}
	if _, _, _, got, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok || got != binding {
		t.Fatalf("bind after sweep binding=%#v ok=%v", got, ok)
	}
	if resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding); !ok || resolved != workspaceDir {
		t.Fatalf("bind did not slide the kept workspace: resolved=%q ok=%v", resolved, ok)
	}
}

func TestDesktopCodingContinuationRearmCannotCrossFence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	token := app.beginDesktopCodingTaskIngress(owner)
	if token == "" {
		t.Fatal("request ingress was not issued")
	}
	// Model the consume-to-rearm interleaving: the token has been consumed and
	// the durable relation issued, then a cancel/clear/new-task fence lands
	// before the chain is rearmed. The old generation must not recreate a
	// continuation capability after that fence.
	app.codingCaps.mu.Lock()
	ingress, live := app.codingCaps.ingress[token]
	delete(app.codingCaps.ingress, token)
	app.codingCaps.mu.Unlock()
	if !live {
		t.Fatal("consumed ingress fixture was not live")
	}
	_, _, handle, _, ok := app.codingCaps.issueRelationForConsumedIngress(app.GetDataDir(), owner, ingress)
	if !ok || !handle.complete() {
		t.Fatalf("consumed ingress did not issue a relation handle=%#v ok=%v", handle, ok)
	}
	app.revokeDesktopCodingTaskRelation(owner)
	app.codingCaps.rearmContinuationToken(owner, token, ingress.workspace, ingress.generation)
	app.codingCaps.mu.Lock()
	continuation := app.codingCaps.continuationTokens[owner]
	_, chainProof := app.codingCaps.chainConsumed[owner][token]
	ingressCount := 0
	for _, candidate := range app.codingCaps.ingress {
		if candidate.ownerID == owner {
			ingressCount++
		}
	}
	app.codingCaps.mu.Unlock()
	if continuation != "" || chainProof || ingressCount != 0 {
		t.Fatalf("fence-crossing rearm recreated capability continuation=%q proof=%v ingress=%d", continuation, chainProof, ingressCount)
	}
}

func TestDesktopCodingContinuationRearmCannotCrossTurnEnd(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	token := app.beginDesktopCodingTaskIngress(owner)
	if token == "" {
		t.Fatal("request ingress was not issued")
	}
	app.codingCaps.mu.Lock()
	ingress, live := app.codingCaps.ingress[token]
	delete(app.codingCaps.ingress, token) // model the consumption phase
	app.codingCaps.mu.Unlock()
	if !live {
		t.Fatal("consumed ingress fixture was not live")
	}
	app.endDesktopCodingTaskIngressForOwner(owner)
	app.codingCaps.rearmContinuationToken(owner, token, ingress.workspace, ingress.generation)
	app.codingCaps.mu.Lock()
	continuation := app.codingCaps.continuationTokens[owner]
	_, chainProof := app.codingCaps.chainConsumed[owner][token]
	ingressCount := 0
	for _, candidate := range app.codingCaps.ingress {
		if candidate.ownerID == owner {
			ingressCount++
		}
	}
	app.codingCaps.mu.Unlock()
	if continuation != "" || chainProof || ingressCount != 0 {
		t.Fatalf("turn-end-crossing rearm recreated capability continuation=%q proof=%v ingress=%d", continuation, chainProof, ingressCount)
	}
}

func TestDesktopCodingContinuationRearmStillArmsInsideLiveTurn(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	token := app.beginDesktopCodingTaskIngress(owner)
	if token == "" {
		t.Fatal("request ingress was not issued")
	}
	app.codingCaps.mu.Lock()
	ingress, live := app.codingCaps.ingress[token]
	delete(app.codingCaps.ingress, token) // model the consumption phase
	app.codingCaps.mu.Unlock()
	if !live {
		t.Fatal("consumed ingress fixture was not live")
	}
	app.codingCaps.rearmContinuationToken(owner, token, ingress.workspace, ingress.generation)
	app.codingCaps.mu.Lock()
	continuation := app.codingCaps.continuationTokens[owner]
	_, chainProof := app.codingCaps.chainConsumed[owner][token]
	_, continuationLive := app.codingCaps.ingress[continuation]
	app.codingCaps.mu.Unlock()
	if continuation == "" || !chainProof || !continuationLive {
		t.Fatalf("live turn did not rearm exactly one continuation continuation=%q proof=%v live=%v", continuation, chainProof, continuationLive)
	}
}

func TestDesktopCodingRootStepRelationRestoresPoppedContinuationAfterFailedBind(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("first root step did not bind")
	}
	// Pop the chained continuation as the next step would, then model a bind
	// that never happened (the token stays a live ingress): the slot must be
	// restorable so a later retry of the same link can still bind.
	popped := app.codingCaps.takeContinuationToken(owner, requestToken)
	if popped == "" {
		t.Fatal("chained continuation was not armed after the first bind")
	}
	app.codingCaps.restoreContinuationToken(owner, popped)
	if _, _, next, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("restored continuation did not bind on retry")
	} else if next.turnID == "" {
		t.Fatal("restored continuation bound an incomplete handle")
	}
	// A consumed token must never be restored: the slot only ever hands out
	// live ingress. Model the failed-after-consume path by restoring the
	// original request token (long consumed) — the chain's real next link
	// must stay in place instead of being overwritten.
	app.codingCaps.restoreContinuationToken(owner, requestToken)
	fresh := app.codingCaps.takeContinuationToken(owner, requestToken)
	if fresh == "" || fresh == requestToken {
		t.Fatalf("restored a consumed token into the continuation slot: %q", fresh)
	}
}

func TestDesktopCodingContinuationRestoreRejectsStaleGeneration(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("first root step did not bind")
	}
	popped := app.codingCaps.takeContinuationToken(owner, requestToken)
	if popped == "" {
		t.Fatal("chained continuation was not armed after the first bind")
	}
	// A fence normally deletes the ingress as it increments the generation.
	// Keep the ingress artificially live here so the restore path itself must
	// enforce the same generation boundary instead of relying only on deletion.
	app.codingCaps.mu.Lock()
	app.codingCaps.generations[owner]++
	app.codingCaps.mu.Unlock()
	app.codingCaps.restoreContinuationToken(owner, popped)
	app.codingCaps.mu.Lock()
	restored := app.codingCaps.continuationTokens[owner]
	app.codingCaps.mu.Unlock()
	if restored != "" {
		t.Fatalf("stale-generation continuation was restored into the slot: %q", restored)
	}
}

func TestDesktopCodingExecutionWorkspaceBindsAfterSurfaceMaterializes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	// The runner materializes detached/exclusive execution directories after
	// the turn window mints the ingress token. The credential must bind at
	// that later host dispatch point, not require the directory to exist early.
	surface := filepath.Join(t.TempDir(), "exclusive", "task", "qmstampregv26")
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, surface)
	if requestToken == "" {
		t.Fatal("request ingress was not issued")
	}
	if app.codingCaps.bindExecutionWorkspace(owner, requestToken, surface) {
		t.Fatal("missing execution surface was accepted before materialization")
	}
	if err := os.MkdirAll(surface, 0o755); err != nil {
		t.Fatal(err)
	}
	if !app.codingCaps.bindExecutionWorkspace(owner, requestToken, surface) {
		t.Fatal("materialized execution surface did not bind")
	}
	_, _, _, binding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !binding.complete() {
		t.Fatalf("root step did not carry the materialized workspace binding=%#v ok=%v", binding, ok)
	}
	resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding)
	if !ok || !sameNormalizedPath(resolved, surface) {
		t.Fatalf("resolved workspace=%q ok=%v, want %q", resolved, ok, surface)
	}
}

func TestDesktopCodingRunnerBindsExactMaterializedSurfaceBeforeConsumption(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		CloseAutoRecallStore()
		closeCodingKnowledgeStore(t, app)
		app.closeCodingTaskRelationService()
	})
	handler := &IMMessageHandler{app: app}
	owner := projectSessionOwnerID("C:/workspace/a")
	// Model the contended-work-root case from the incident: turn ingress is
	// minted while the detached execution directory does not exist yet, and
	// the workflow materializes that directory only when the step is dispatched.
	surface := filepath.Join(t.TempDir(), "parallel-task", "QmStampRegV26")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if requestToken == "" {
		t.Fatal("request ingress was not issued")
	}
	if err := os.MkdirAll(surface, 0o755); err != nil {
		t.Fatal(err)
	}
	loopCtx := NewLoopContext("coding-workbench", 1, nil)
	loopCtx.UserID = owner
	loopCtx.CodingTaskIngressToken = requestToken
	// Cancel before the model loop so this integration test stops immediately
	// after the host dispatch boundary; the assertion is about the capability
	// state the runner established before consumption, not model behavior.
	loopCtx.Cancel()
	task := &TaskItem{Index: 1, Title: "implement neon optimization", Description: "write code", RequestKind: codingRequestImplementation}
	result := runTaskWithSubAgentRuntimeOptions(handler, corelib.MaclawLLMConfig{}, nil, task, surface, "", "", nil, loopCtx, nil, nil, nil)
	if result == nil {
		t.Fatal("runner returned no result")
	}

	app.codingCaps.mu.Lock()
	continuation := app.codingCaps.continuationTokens[owner]
	ingress, ingressOK := app.codingCaps.ingress[continuation]
	binding := ingress.workspace
	app.codingCaps.mu.Unlock()
	if continuation == "" || !ingressOK || !binding.complete() {
		t.Fatalf("runner did not bind and chain the materialized workspace continuation=%q ingress=%v binding=%#v", continuation, ingressOK, binding)
	}
	resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding)
	if !ok || !sameNormalizedPath(resolved, surface) {
		t.Fatalf("runner-bound workspace=%q ok=%v, want exact dispatch surface %q", resolved, ok, surface)
	}
}

func TestDesktopCodingExecutionWorkspaceReplacesInitialSurfaceWithDispatchSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	turnStartSurface := t.TempDir()
	dispatchSurface := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, turnStartSurface)
	if !app.codingCaps.bindExecutionWorkspace(owner, requestToken, dispatchSurface) {
		t.Fatal("dispatch surface did not rebind a live request token")
	}
	_, _, _, binding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !binding.complete() {
		t.Fatalf("root step did not carry rebound workspace binding=%#v ok=%v", binding, ok)
	}
	resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, binding)
	if !ok || !sameNormalizedPath(resolved, dispatchSurface) {
		t.Fatalf("workspace resolved to %q ok=%v, want dispatch surface %q rather than turn-start surface %q", resolved, ok, dispatchSurface, turnStartSurface)
	}
}

func TestDesktopCodingExecutionWorkspaceDoesNotFallBackToPriorSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	priorSurface := t.TempDir()
	missingSurface := filepath.Join(t.TempDir(), "missing-worktree")
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, priorSurface)
	if app.codingCaps.bindExecutionWorkspace(owner, requestToken, missingSurface) {
		t.Fatal("missing execution surface was accepted")
	}
	_, _, _, binding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok {
		t.Fatal("semantic relation should still bind without a workspace")
	}
	if binding.complete() {
		t.Fatalf("failed exact-surface bind fell back to prior workspace authority: %#v", binding)
	}
}

func TestDesktopCodingExecutionWorkspaceFailedAtomicBindDoesNotConsumeContinuation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	missingSurface := filepath.Join(t.TempDir(), "missing-worktree")
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	_, _, _, firstBinding, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, workspaceA, true)
	if !ok || !firstBinding.complete() {
		t.Fatalf("first execution surface workspace=%#v ok=%v", firstBinding, ok)
	}
	// The production bind/consume pair is atomic: when the next materialized
	// surface is invalid, the pending continuation stays in its slot instead of
	// being consumed into a dead step or inheriting workspace A.
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, missingSurface, true); ok {
		t.Fatal("missing execution surface consumed the pending continuation")
	}
	_, _, _, secondBinding, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, workspaceB, true)
	if !ok || !secondBinding.complete() || secondBinding == firstBinding {
		t.Fatalf("retried continuation first=%#v second=%#v ok=%v", firstBinding, secondBinding, ok)
	}
	resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, secondBinding)
	if !ok || !sameNormalizedPath(resolved, workspaceB) {
		t.Fatalf("retried continuation resolved workspace=%q ok=%v, want %q", resolved, ok, workspaceB)
	}
}

func TestDesktopCodingExecutionWorkspaceRebindsPendingContinuationToStepSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceA)
	_, _, _, firstBinding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !firstBinding.complete() {
		t.Fatalf("first root step workspace=%#v ok=%v", firstBinding, ok)
	}
	// The original token is only chain proof after the first bind. Rebinding
	// through it must retarget the pending continuation token, not resurrect
	// the consumed request token or reuse the previous step's worktree.
	if !app.codingCaps.bindExecutionWorkspace(owner, requestToken, workspaceB) {
		t.Fatal("pending continuation did not bind the next execution surface")
	}
	_, _, _, secondBinding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !secondBinding.complete() || secondBinding == firstBinding {
		t.Fatalf("second root step first=%#v second=%#v ok=%v", firstBinding, secondBinding, ok)
	}
	resolved, ok := app.resolveDesktopCodingStaticWorkspace(owner, secondBinding)
	if !ok || !sameNormalizedPath(resolved, workspaceB) {
		t.Fatalf("continuation resolved workspace=%q ok=%v, want %q", resolved, ok, workspaceB)
	}
}

func TestDesktopCodingExecutionWorkspaceReadOnlyStepDropsCarriedWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceDir := t.TempDir()
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	_, _, _, firstBinding, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, workspaceDir, true)
	if !ok || !firstBinding.complete() {
		t.Fatalf("first writable step workspace=%#v ok=%v", firstBinding, ok)
	}
	// A read-only root step still consumes the semantic chain, but it must not
	// inherit the previous writer's workspace merely because the continuation
	// carried that handle for lease continuity.
	_, _, _, readOnlyBinding, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, "", false)
	if !ok {
		t.Fatal("read-only root step did not consume the continuation")
	}
	if readOnlyBinding.complete() {
		t.Fatalf("read-only root step carried writable workspace authority: %#v", readOnlyBinding)
	}
	_, _, _, nextWritableBinding, ok := app.nextDesktopCodingRootStepRelationForExecution(requestToken, owner, workspaceDir, true)
	if !ok || !nextWritableBinding.complete() {
		t.Fatalf("next writable step workspace=%#v ok=%v", nextWritableBinding, ok)
	}
}

func TestDesktopCodingRunnerConsumesInquiryWithoutWritableWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		CloseAutoRecallStore()
		closeCodingKnowledgeStore(t, app)
		app.closeCodingTaskRelationService()
	})
	handler := &IMMessageHandler{app: app}
	owner := projectSessionOwnerID("C:/workspace/a")
	surface := t.TempDir()
	requestToken := app.beginDesktopCodingTaskIngress(owner)
	if requestToken == "" {
		t.Fatal("request ingress was not issued")
	}
	loopCtx := NewLoopContext("coding-workbench", 1, nil)
	loopCtx.UserID = owner
	loopCtx.CodingTaskIngressToken = requestToken
	loopCtx.Cancel()
	task := &TaskItem{Index: 1, Title: "explain current implementation", Description: "answer read-only", RequestKind: codingRequestInquiry}
	result := runTaskWithSubAgentRuntimeOptions(handler, corelib.MaclawLLMConfig{}, nil, task, surface, "", "", nil, loopCtx, nil, nil, nil)
	if result == nil {
		t.Fatal("runner returned no result")
	}
	app.codingCaps.mu.Lock()
	continuation := app.codingCaps.continuationTokens[owner]
	ingress, ingressOK := app.codingCaps.ingress[continuation]
	binding := ingress.workspace
	app.codingCaps.mu.Unlock()
	if continuation == "" || !ingressOK {
		t.Fatalf("inquiry step did not continue the semantic chain continuation=%q ingress=%v", continuation, ingressOK)
	}
	if binding.complete() {
		t.Fatalf("inquiry step carried writable workspace authority into the chain: %#v", binding)
	}
}

func TestDesktopCodingExecutionWorkspaceReusesMatchingSurfaceHandle(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	workspaceDir := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	_, _, _, firstBinding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || !firstBinding.complete() {
		t.Fatalf("first root step workspace=%#v ok=%v", firstBinding, ok)
	}
	if !app.codingCaps.bindExecutionWorkspace(owner, requestToken, workspaceDir) {
		t.Fatal("matching continuation surface did not bind")
	}
	_, _, _, secondBinding, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner)
	if !ok || secondBinding != firstBinding {
		t.Fatalf("matching surface must keep the stable workspace handle first=%#v second=%#v ok=%v", firstBinding, secondBinding, ok)
	}
}

func TestDesktopCodingExecutionWorkspaceRebindFailsClosed(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.closeCodingTaskRelationService()
	})
	owner := projectSessionOwnerID("C:/workspace/a")
	other := projectSessionOwnerID("C:/workspace/b")
	workspaceDir := t.TempDir()
	requestToken := beginDesktopCodingTaskIngressWithWorkspaceForTest(app, owner, workspaceDir)
	if app.codingCaps.bindExecutionWorkspace(other, requestToken, workspaceDir) {
		t.Fatal("foreign owner rebound a desktop coding workspace")
	}
	if app.codingCaps.bindExecutionWorkspace(owner, "coding-ingress-forged", workspaceDir) {
		t.Fatal("forged chain proof rebound a desktop coding workspace")
	}
	expiredToken := app.beginDesktopCodingTaskIngress(owner)
	if expiredToken == "" {
		t.Fatal("expiry fixture ingress was not issued")
	}
	app.codingCaps.mu.Lock()
	expiredIngress := app.codingCaps.ingress[expiredToken]
	expiredIngress.expiresAt = time.Now().UTC().Add(-time.Minute)
	app.codingCaps.ingress[expiredToken] = expiredIngress
	app.codingCaps.mu.Unlock()
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelationForExecution(expiredToken, owner, workspaceDir, true); ok {
		t.Fatal("expired ingress bound an execution workspace")
	}
	if !app.codingCaps.bindExecutionWorkspace(owner, requestToken, workspaceDir) {
		t.Fatal("owner request surface did not bind")
	}
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(requestToken, owner); !ok {
		t.Fatal("first root step did not bind")
	}
	app.endDesktopCodingTaskIngressForOwner(owner)
	if app.codingCaps.bindExecutionWorkspace(owner, requestToken, workspaceDir) {
		t.Fatal("turn-end sweep left a continuation surface rebindable")
	}
}

type failFirstSemanticAnchorStore struct {
	codingruntime.Store
	failed bool
}

func (s *failFirstSemanticAnchorStore) RegisterSemanticTaskAnchor(anchor codingruntime.SemanticTaskAnchor) (*codingruntime.SemanticTaskAnchor, error) {
	if !s.failed {
		s.failed = true
		return nil, errors.New("injected semantic anchor failure")
	}
	return s.Store.(codingruntime.SemanticTaskAnchorStore).RegisterSemanticTaskAnchor(anchor)
}

func (s *failFirstSemanticAnchorStore) ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID string) (*codingruntime.SemanticTaskAnchor, error) {
	return s.Store.(codingruntime.SemanticTaskAnchorStore).ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID)
}

// Regression: a transient anchor failure must not consume the verified handle.
// The runtime anchor is the only durable authorization commit, so retrying the
// same attempt can complete that single commit after the transient failure.
func TestBindCodingAttemptAnchorFailureDoesNotWriteAuthorization(t *testing.T) {
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
	store := &failFirstSemanticAnchorStore{Store: codingruntime.NewMemoryStore()}
	task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	request := codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}
	if _, err := service.BindCodingAttempt(subject, handle, store, request, now); err == nil {
		t.Fatal("injected anchor failure did not fail the bind")
	}
	var bindings int
	if err := service.db.QueryRow(`SELECT COUNT(1) FROM coding_task_attempt_bindings WHERE handle_id=?`, handle.handleID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("anchor failure committed %d attempt fence rows; want a retryable unbound handle", bindings)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, request, now.Add(time.Second)); err != nil {
		t.Fatalf("same attempt bind did not heal after the transient anchor failure: %v", err)
	}
	if err := service.db.QueryRow(`SELECT COUNT(1) FROM coding_task_attempt_bindings WHERE handle_id=?`, handle.handleID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("healed bind wrote %d legacy bridge rows, want the runtime anchor to remain the sole authorization commit", bindings)
	}
}

type closeRelationsAfterAnchorStore struct {
	codingruntime.Store
	relationDB *sql.DB
	closed     bool
}

func (s *closeRelationsAfterAnchorStore) RegisterSemanticTaskAnchor(anchor codingruntime.SemanticTaskAnchor) (*codingruntime.SemanticTaskAnchor, error) {
	registered, err := s.Store.(codingruntime.SemanticTaskAnchorStore).RegisterSemanticTaskAnchor(anchor)
	if err == nil && !s.closed {
		s.closed = true
		_ = s.relationDB.Close()
	}
	return registered, err
}

func (s *closeRelationsAfterAnchorStore) ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID string) (*codingruntime.SemanticTaskAnchor, error) {
	return s.Store.(codingruntime.SemanticTaskAnchorStore).ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID)
}

// Regression for the inverse failure: once the runtime anchor has been
// committed, a relation-store validation failure must fail the bind closed but
// remain retryable for the same attempt. The caller must not treat the
// unvalidated anchor as authorization.
func TestBindCodingAttemptPostvalidationFailureHealsOnRetry(t *testing.T) {
	relationPath := filepath.Join(t.TempDir(), "relations.db")
	service, err := newCodingTaskRelationService(relationPath)
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
	closingStore := &closeRelationsAfterAnchorStore{Store: store, relationDB: service.db}
	if _, err := service.BindCodingAttempt(subject, handle, closingStore, request, now); err == nil {
		t.Fatal("relation postvalidation failure did not fail the bind")
	}

	reopened, err := newCodingTaskRelationService(relationPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.BindCodingAttempt(subject, handle, store, request, now.Add(time.Second)); err != nil {
		t.Fatalf("same attempt did not heal after relation postvalidation failure: %v", err)
	}
	var bindings int
	if err := reopened.db.QueryRow(`SELECT COUNT(1) FROM coding_task_attempt_bindings WHERE handle_id=?`, handle.handleID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 {
		t.Fatalf("healed bind wrote %d legacy bridge rows, want the runtime anchor to remain the sole authorization commit", bindings)
	}
}

func TestBindCodingAttemptAfterRelationServiceCloseFailsClosed(t *testing.T) {
	service, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
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
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BindCodingAttempt(subject, handle, store, codingruntime.ExecutionRequest{Task: *task, Attempt: *attempt}, now); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("bind after close error = %v, want service unavailable", err)
	}
}
