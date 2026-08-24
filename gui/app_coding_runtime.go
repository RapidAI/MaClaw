package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/google/uuid"
)

// ensureCodingRuntimeStore opens the app-level execution ledger once. Keeping
// it application-scoped gives startup a chance to mark abandoned leases as
// interrupted instead of silently losing their attempt history.
func (a *App) ensureCodingRuntimeStore() *codingruntime.SQLiteStore {
	if a == nil {
		return nil
	}
	a.codingRuntimeStoreMu.Lock()
	defer a.codingRuntimeStoreMu.Unlock()
	if a.codingRuntimeStore != nil {
		return a.codingRuntimeStore
	}
	dir := a.GetDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[coding-runtime] create data directory failed: %v", err)
		return nil
	}
	store, err := codingruntime.NewSQLiteStore(filepath.Join(dir, "coding_runtime.db"))
	if err != nil {
		log.Printf("[coding-runtime] open ledger failed: %v", err)
		return nil
	}
	if expired, err := store.ExpireLeases(time.Now().UTC()); err != nil {
		log.Printf("[coding-runtime] expire stale leases failed: %v", err)
	} else if len(expired) > 0 {
		log.Printf("[coding-runtime] marked %d stale attempt(s) interrupted; recovery requires read-only probe", len(expired))
	}
	if interrupted, err := store.InterruptUnstartedChildren(time.Now().UTC()); err != nil {
		log.Printf("[coding-runtime] reconcile unstarted child tasks failed: %v", err)
	} else if len(interrupted) > 0 {
		log.Printf("[coding-runtime] marked %d parent attempt(s) interrupted because child dispatch cannot survive restart", len(interrupted))
	}
	a.codingRuntimeStore = store
	return store
}

func (a *App) closeCodingRuntimeStore() {
	if a == nil {
		return
	}
	a.codingRuntimeStoreMu.Lock()
	store := a.codingRuntimeStore
	a.codingRuntimeStore = nil
	a.codingRuntimeStoreMu.Unlock()
	if store != nil {
		_ = store.Close()
	}
}

// codingTaskRelationServiceForApp is deliberately host-owned. No agent or
// model callback may create this service or issue identities from its own
// runtime values; the authenticated task/continuation ingress will use it in
// R1b after it has verified its subject and session independently.
func (a *App) codingTaskRelationServiceForApp() (*codingTaskRelationService, error) {
	if a == nil {
		return nil, fmt.Errorf("coding task relation host is unavailable")
	}
	a.codingTaskRelationsMu.Lock()
	defer a.codingTaskRelationsMu.Unlock()
	if a.codingTaskRelations != nil {
		return a.codingTaskRelations, nil
	}
	service, err := newCodingTaskRelationService(filepath.Join(a.GetDataDir(), "coding_task_relations.db"))
	if err != nil {
		return nil, err
	}
	a.codingTaskRelations = service
	return service, nil
}

func (a *App) closeCodingTaskRelationService() {
	if a == nil {
		return
	}
	a.codingTaskRelationsMu.Lock()
	service := a.codingTaskRelations
	a.codingTaskRelations = nil
	a.codingTaskRelationsMu.Unlock()
	if service != nil {
		_ = service.Close()
	}
}

// desktopCodingTaskSession is an App-owned authenticated local-desktop
// relation. The session ID is randomly issued by the desktop host and stays
// distinct from both the host principal and UI owner/cache key.
type desktopCodingTaskSession struct {
	subject verifiedCodingSubject
	handle  verifiedCodingTaskHandle
}

// desktopCodingIngress is an in-process, request-scoped authorization from
// the Wails host. Unlike an identity, it has no tenant/principal/session/root
// fields and cannot be serialized; it simply proves the current coding call
// descended from an authenticated desktop request for this exact UI owner.
type desktopCodingIngress struct {
	ownerID    string
	generation uint64
	expiresAt  time.Time
	workspace  codingStaticWorkspaceBinding
}

// desktopCodingStaticWorkspace is host-private resolution data for a local
// Coding workspace handle.  S1-A only uses the handle in a shadow catalog;
// S1-B will be the first consumer permitted to resolve Directory for an
// adapter, with a fresh scope check immediately before filesystem access.
type desktopCodingStaticWorkspace struct {
	ownerID   string
	directory string
	expiresAt time.Time
}

func (a *App) beginDesktopCodingTaskIngress(ownerID string) string {
	return a.beginDesktopCodingTaskIngressWithWorkspace(ownerID, "")
}

// beginDesktopCodingTaskIngressWithWorkspace attaches a host-verified local
// workspace handle to one ingress token.  workspaceDir is accepted only from
// a Wails/desktop host boundary after it resolved the active workbench
// directory; it is never copied into semantic identity or exposed to the
// model. Invalid/missing directories intentionally yield an ingress with no
// static workspace binding, which makes S1-A catalog planning fail closed.
func (a *App) beginDesktopCodingTaskIngressWithWorkspace(ownerID, workspaceDir string) string {
	ownerID = strings.TrimSpace(ownerID)
	if a == nil || (ownerID != desktopUserID && !strings.HasPrefix(ownerID, desktopUserID+":")) {
		return ""
	}
	token := "coding-ingress-" + uuid.NewString()
	a.desktopCodingIngressMu.Lock()
	if a.desktopCodingIngress == nil {
		a.desktopCodingIngress = make(map[string]desktopCodingIngress)
	}
	if a.desktopCodingIngressGenerations == nil {
		a.desktopCodingIngressGenerations = make(map[string]uint64)
	}
	now := time.Now().UTC()
	for id, ingress := range a.desktopCodingIngress {
		if !now.Before(ingress.expiresAt) {
			delete(a.desktopCodingIngress, id)
		}
	}
	workspace := a.issueDesktopCodingStaticWorkspaceLocked(ownerID, workspaceDir, now)
	a.desktopCodingIngress[token] = desktopCodingIngress{
		ownerID: ownerID, generation: a.desktopCodingIngressGenerations[ownerID], expiresAt: now.Add(30 * time.Minute),
		workspace: workspace,
	}
	a.desktopCodingIngressMu.Unlock()
	return token
}

// beginDesktopCodingTaskIngressForRequest is the only desktop request boundary
// that may mint a Coding ingress token. An explicit "start new task" is a
// semantic-root boundary, not merely a transcript preference: it first fences
// the prior relation and every unconsumed ingress token for that owner, then
// creates a token for the new request. Text similarity, a project path, or a
// new RequestID must never be used as an implicit substitute for this action.
func (a *App) beginDesktopCodingTaskIngressForRequest(ownerID string, startNewTask bool) string {
	return a.beginDesktopCodingTaskIngressForRequestWithWorkspace(ownerID, "", startNewTask)
}

// beginDesktopCodingTaskIngressForRequestWithWorkspace is the local Coding
// variant of the authenticated desktop request boundary.  It keeps the
// workspace handle on the short-lived ingress token instead of deriving it
// later from a LoopContext, runtime task, task text, or project path.
func (a *App) beginDesktopCodingTaskIngressForRequestWithWorkspace(ownerID, workspaceDir string, startNewTask bool) string {
	if startNewTask {
		a.fenceDesktopCodingTaskRelation(ownerID, true)
	}
	return a.beginDesktopCodingTaskIngressWithWorkspace(ownerID, workspaceDir)
}

func (a *App) endDesktopCodingTaskIngress(token string) {
	if a == nil {
		return
	}
	a.desktopCodingIngressMu.Lock()
	delete(a.desktopCodingIngress, strings.TrimSpace(token))
	a.desktopCodingIngressMu.Unlock()
}

// nextDesktopCodingTaskRelation is the first production R1 ingress. It is
// intentionally restricted to desktop session owners created by the Wails
// host. IM user IDs, LoopContext IDs, request IDs, project paths and remote
// SSH session IDs never reach this function as semantic identity values.
//
// Desktop is a locally authenticated installation: tenant/principal are
// host constants, while the session ID is generated once per UI owner. The
// UI owner only selects which host session record to load; it is not copied
// into TenantID, PrincipalID, SessionID or RootTaskID.
func (a *App) nextDesktopCodingTaskRelation(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, bool) {
	service, subject, handle, _, ok := a.nextDesktopCodingTaskRelationWithWorkspace(token, ownerID)
	return service, subject, handle, ok
}

// nextDesktopCodingTaskRelationWithWorkspace is the one atomic consumption
// path for an authenticated desktop Coding ingress.  The returned workspace
// is an opaque, host-issued binding from that same request, not a lookup made
// using the just-created semantic relation or runtime attempt.
func (a *App) nextDesktopCodingTaskRelationWithWorkspace(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if a == nil || (ownerID != desktopUserID && !strings.HasPrefix(ownerID, desktopUserID+":")) {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	a.desktopCodingIngressMu.Lock()
	token = strings.TrimSpace(token)
	ingress, permitted := a.desktopCodingIngress[token]
	currentGeneration := a.desktopCodingIngressGenerations[ownerID]
	if !permitted || ingress.ownerID != ownerID || ingress.generation != currentGeneration || !time.Now().UTC().Before(ingress.expiresAt) {
		a.desktopCodingIngressMu.Unlock()
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	// Consume before issuing a relation. A request-scoped token is a capability
	// to enter R1 exactly once, not a lookup key: allowing two local/remote
	// dispatch paths to read it would mint consecutive turns for one request and
	// invalidate whichever handle binds second. Its generation is checked again
	// below while holding the relation lock, so a new-task/cancel fence racing
	// after consumption cannot let this old request resurrect a relation.
	delete(a.desktopCodingIngress, token)
	a.desktopCodingIngressMu.Unlock()
	service, err := a.codingTaskRelationServiceForApp()
	if err != nil || service == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	now := time.Now().UTC()
	a.desktopCodingTaskSessionsMu.Lock()
	defer a.desktopCodingTaskSessionsMu.Unlock()
	a.desktopCodingIngressMu.Lock()
	stillCurrent := a.desktopCodingIngressGenerations[ownerID] == ingress.generation
	a.desktopCodingIngressMu.Unlock()
	if !stillCurrent {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	if a.desktopCodingTaskSessions == nil {
		a.desktopCodingTaskSessions = make(map[string]desktopCodingTaskSession)
	}
	current, exists := a.desktopCodingTaskSessions[ownerID]
	if !exists {
		subject, subjectErr := newVerifiedCodingSubject("desktop-local", "desktop-host-principal", newCodingTaskRelationSessionID())
		if subjectErr != nil {
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		handle, createErr := service.CreateCodingTask(subject, now, 24*time.Hour)
		if createErr != nil {
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		current = desktopCodingTaskSession{subject: subject, handle: handle}
	} else {
		next, continuationErr := service.VerifyCodingContinuation(current.subject, current.handle, now, 24*time.Hour)
		if continuationErr != nil {
			// A cancelled/expired/replayed handle starts no dynamic-capable task.
			// Callers retain a static Coding surface instead of silently changing
			// the semantic root behind the user's active desktop session.
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		current.handle = next
	}
	a.desktopCodingTaskSessions[ownerID] = current
	return service, current.subject, current.handle, ingress.workspace, true
}

func (a *App) issueDesktopCodingStaticWorkspaceLocked(ownerID, workspaceDir string, now time.Time) codingStaticWorkspaceBinding {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if a == nil || workspaceDir == "" {
		return codingStaticWorkspaceBinding{}
	}
	abs, err := filepath.Abs(workspaceDir)
	if err != nil {
		return codingStaticWorkspaceBinding{}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return codingStaticWorkspaceBinding{}
	}
	if a.desktopCodingStaticWorkspaces == nil {
		a.desktopCodingStaticWorkspaces = make(map[string]desktopCodingStaticWorkspace)
	}
	for handle, workspace := range a.desktopCodingStaticWorkspaces {
		if !now.Before(workspace.expiresAt) {
			delete(a.desktopCodingStaticWorkspaces, handle)
		}
	}
	handle := "coding-workspace-" + uuid.NewString()
	a.desktopCodingStaticWorkspaces[handle] = desktopCodingStaticWorkspace{ownerID: ownerID, directory: abs, expiresAt: now.Add(30 * time.Minute)}
	return codingStaticWorkspaceBinding{WorkspaceHandle: handle, HostKind: "local"}
}

// resolveDesktopCodingStaticWorkspace is intentionally unused by S1-A. It is
// the future S1-B adapter seam and proves that a catalog binding can only be
// resolved through host state for its original desktop owner.
func (a *App) resolveDesktopCodingStaticWorkspace(ownerID string, binding codingStaticWorkspaceBinding) (string, bool) {
	if a == nil || !binding.complete() {
		return "", false
	}
	a.desktopCodingIngressMu.Lock()
	defer a.desktopCodingIngressMu.Unlock()
	workspace, ok := a.desktopCodingStaticWorkspaces[strings.TrimSpace(binding.WorkspaceHandle)]
	if !ok || workspace.ownerID != strings.TrimSpace(ownerID) || !time.Now().UTC().Before(workspace.expiresAt) {
		return "", false
	}
	// The handle is not a permanent claim to a directory. Re-check its host
	// resolution on every use so a deleted/replaced workspace fails closed
	// before any future S1-B adapter executes a selection.
	info, err := os.Stat(workspace.directory)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return workspace.directory, true
}

func newCodingTaskRelationSessionID() string {
	return "desktop-session-" + uuid.NewString()
}

// revokeDesktopCodingTaskRelation is the desktop cancel/clear fence. It
// revokes active descendant child turns through the relation service before
// dropping the UI owner mapping, so a late child cannot bind a new attempt.
func (a *App) revokeDesktopCodingTaskRelation(ownerID string) {
	a.fenceDesktopCodingTaskRelation(ownerID, true)
}

// fenceDesktopCodingTaskRelation invalidates the current ingress generation
// before touching the durable handle. The generation makes the fence survive
// a token's consume-to-bind interval; removing only map entries cannot close
// that race. The relation lock is then taken before the generation recheck in
// nextDesktopCodingTaskRelation, so no attempt can bind across this boundary.
func (a *App) fenceDesktopCodingTaskRelation(ownerID string, revokeRelation bool) {
	if a == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	// A token only proves descent from one host request. It does not embed a
	// relation generation, so leaving an already-issued token live after a
	// cancel/clear/new-task fence would let a late old request mint another
	// relation after the owner mapping was dropped. Remove every outstanding
	// token for this owner before revoking the durable handle.
	a.desktopCodingIngressMu.Lock()
	if a.desktopCodingIngressGenerations == nil {
		a.desktopCodingIngressGenerations = make(map[string]uint64)
	}
	a.desktopCodingIngressGenerations[ownerID]++
	for token, ingress := range a.desktopCodingIngress {
		if ingress.ownerID == ownerID {
			delete(a.desktopCodingIngress, token)
		}
	}
	// Workspace handles are request-scoped capability context too. Leaving one
	// resolvable after a new-task/cancel fence would let a stale future S1-B
	// adapter execute against the old project even though its semantic lineage
	// had already been revoked.
	for handle, workspace := range a.desktopCodingStaticWorkspaces {
		if workspace.ownerID == ownerID {
			delete(a.desktopCodingStaticWorkspaces, handle)
		}
	}
	a.desktopCodingIngressMu.Unlock()
	if !revokeRelation {
		return
	}
	a.desktopCodingTaskSessionsMu.Lock()
	current, ok := a.desktopCodingTaskSessions[ownerID]
	delete(a.desktopCodingTaskSessions, ownerID)
	a.desktopCodingTaskSessionsMu.Unlock()
	if !ok {
		return
	}
	service, err := a.codingTaskRelationServiceForApp()
	if err == nil && service != nil {
		_ = service.RevokeCodingTaskHandle(current.subject, current.handle, time.Now().UTC())
	}
}
