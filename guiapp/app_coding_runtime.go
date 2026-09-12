package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// desktopCodingVerifiedSurfaceUnavailableDiagnostic is the user-facing
// reason returned by the pre-dispatch capability gate when an implementation
// or operational coding step reaches the local desktop runner without a
// verified task relation. It deliberately names the user action that fixes
// the condition instead of exposing internal surface terminology.
const desktopCodingVerifiedSurfaceUnavailableDiagnostic = "未获得本机编程执行授权：本次派发没有携带有效的桌面执行凭证，实现/运行类步骤无法启动。请回到编程工作台重新发送任务，或重新批准执行计划后重试。"

// isDesktopCodingOwner reports whether ownerID is a desktop-host session
// owner. Only such owners may hold Coding ingress capabilities; IM user IDs,
// request IDs, project paths and remote session IDs never qualify.
func isDesktopCodingOwner(ownerID string) bool {
	ownerID = strings.TrimSpace(ownerID)
	return ownerID == desktopUserID || strings.HasPrefix(ownerID, desktopUserID+":")
}

// desktopCodingTaskSession is an issuer-owned authenticated local-desktop
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

// desktopCodingCapabilityIssuer is the single owner of every desktop Coding
// capability: the task-relation service, per-owner task sessions, ingress
// tokens, workspace handles, generation fences and the continuation chain.
// Keeping all of this state and every rule that mutates it on one type makes
// the capability lifecycle auditable in one place; App only holds thin
// delegating wrappers, and no other type may mint, chain, fence, or revoke.
type desktopCodingCapabilityIssuer struct {
	relationsMu sync.Mutex
	relations   *codingTaskRelationService

	sessionsMu sync.Mutex
	sessions   map[string]desktopCodingTaskSession

	mu                 sync.Mutex
	ingress            map[string]desktopCodingIngress
	staticWorkspaces   map[string]desktopCodingStaticWorkspace
	generations        map[string]uint64
	continuationTokens map[string]string
	chainConsumed      map[string]map[string]struct{}
	continuationBudget map[string]int
}

// relationService is deliberately host-owned. No agent or model callback may
// create this service or issue identities from its own runtime values; the
// authenticated task/continuation ingress will use it in R1b after it has
// verified its subject and session independently.
func (i *desktopCodingCapabilityIssuer) relationService(dataDir string) (*codingTaskRelationService, error) {
	if i == nil {
		return nil, fmt.Errorf("coding task relation host is unavailable")
	}
	i.relationsMu.Lock()
	defer i.relationsMu.Unlock()
	if i.relations != nil {
		return i.relations, nil
	}
	service, err := newCodingTaskRelationService(filepath.Join(dataDir, "coding_task_relations.db"))
	if err != nil {
		return nil, err
	}
	i.relations = service
	return service, nil
}

func (i *desktopCodingCapabilityIssuer) closeRelationService() {
	if i == nil {
		return
	}
	i.relationsMu.Lock()
	service := i.relations
	i.relations = nil
	i.relationsMu.Unlock()
	if service != nil {
		_ = service.Close()
	}
}

func (i *desktopCodingCapabilityIssuer) beginIngress(ownerID string) string {
	return i.beginIngressWithWorkspace(ownerID, "")
}

// beginIngressWithWorkspace attaches a host-verified local workspace handle
// to one ingress token. workspaceDir is accepted only from a Wails/desktop
// host boundary after it resolved the active workbench directory; it is
// never copied into semantic identity or exposed to the model.
// Invalid/missing directories intentionally yield an ingress with no static
// workspace binding, which makes S1-A catalog planning fail closed.
func (i *desktopCodingCapabilityIssuer) beginIngressWithWorkspace(ownerID, workspaceDir string) string {
	ownerID = strings.TrimSpace(ownerID)
	if i == nil || !isDesktopCodingOwner(ownerID) {
		return ""
	}
	i.mu.Lock()
	if i.ingress == nil {
		i.ingress = make(map[string]desktopCodingIngress)
	}
	workspace := i.issueStaticWorkspaceLocked(ownerID, workspaceDir, time.Now().UTC())
	token := i.mintLocked(ownerID, workspace)
	i.mu.Unlock()
	return token
}

// mintLocked records a fresh single-use ingress token for ownerID under the
// current generation. Callers must hold i.mu.
func (i *desktopCodingCapabilityIssuer) mintLocked(ownerID string, workspace codingStaticWorkspaceBinding) string {
	if i.generations == nil {
		i.generations = make(map[string]uint64)
	}
	now := time.Now().UTC()
	for id, ingress := range i.ingress {
		if !now.Before(ingress.expiresAt) {
			delete(i.ingress, id)
		}
	}
	token := "coding-ingress-" + uuid.NewString()
	i.ingress[token] = desktopCodingIngress{
		ownerID: ownerID, generation: i.generations[ownerID], expiresAt: now.Add(30 * time.Minute),
		workspace: workspace,
	}
	return token
}

// beginIngressForRequest is the only desktop request boundary that may mint
// a Coding ingress token. An explicit "start new task" is a semantic-root
// boundary, not merely a transcript preference: it first fences the prior
// relation and every unconsumed ingress token for that owner, then creates a
// token for the new request. Text similarity, a project path, or a new
// RequestID must never be used as an implicit substitute for this action.
func (i *desktopCodingCapabilityIssuer) beginIngressForRequest(dataDir, ownerID string, startNewTask bool) string {
	return i.beginIngressForRequestWithWorkspace(dataDir, ownerID, "", startNewTask)
}

// beginIngressForRequestWithWorkspace is the local Coding variant of the
// authenticated desktop request boundary. It keeps the workspace handle on
// the short-lived ingress token instead of deriving it later from a
// LoopContext, runtime task, task text, or project path.
func (i *desktopCodingCapabilityIssuer) beginIngressForRequestWithWorkspace(dataDir, ownerID, workspaceDir string, startNewTask bool) string {
	if startNewTask {
		i.fence(dataDir, ownerID, true)
	}
	return i.beginIngressWithWorkspace(ownerID, workspaceDir)
}

func (i *desktopCodingCapabilityIssuer) endIngress(token string) {
	if i == nil {
		return
	}
	i.mu.Lock()
	delete(i.ingress, strings.TrimSpace(token))
	i.mu.Unlock()
}

// endIngressForOwner closes every outstanding ingress capability of one
// desktop owner at the end of its request: the request token (usually
// already consumed), any unused chained continuation token, and the
// continuation slot itself. Without this, a continuation token minted for a
// plan step that never ran would stay bindable until expiry, outliving the
// request it descended from.
func (i *desktopCodingCapabilityIssuer) endIngressForOwner(ownerID string) {
	if i == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	i.mu.Lock()
	for token, ingress := range i.ingress {
		if ingress.ownerID == ownerID {
			delete(i.ingress, token)
		}
	}
	delete(i.continuationTokens, ownerID)
	delete(i.chainConsumed, ownerID)
	delete(i.continuationBudget, ownerID)
	i.mu.Unlock()
}

// nextRootStepRelation resolves the verified relation for one root coding
// step of an authenticated desktop request. The request token authorizes the
// first root step; every successful bind re-arms exactly one single-use
// continuation token (same owner, same host-issued workspace binding,
// current generation) so a multi-step plan — which dispatches each root step
// as its own R1 entry within one request — gets one verified turn per step
// on the same semantic root. Each token is still consumed exactly once; a
// replayed or stale token fails closed; popping the continuation slot
// requires presenting a token already consumed on this chain, so the owner
// string alone never suffices; concurrent dispatches race for the single
// slot so at most one of them binds, exactly as before.
func (i *desktopCodingCapabilityIssuer) nextRootStepRelation(dataDir, token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	token = strings.TrimSpace(token)
	service, subject, handle, workspace, ok := i.nextRelationWithWorkspace(dataDir, token, ownerID)
	if !ok {
		continuation := i.takeContinuationToken(ownerID, token)
		if continuation == "" {
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		service, subject, handle, workspace, ok = i.nextRelationWithWorkspace(dataDir, continuation, ownerID)
		if !ok {
			// The bind failed after the slot was popped (for example a fence
			// landed in between, or the relation service errored). Return the
			// token to the slot so a later step may retry the same link; the
			// proof requirement in takeContinuationToken still keeps the slot
			// closed to anyone who never held this chain.
			i.restoreContinuationToken(ownerID, continuation)
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		token = continuation
	}
	i.rearmContinuationToken(ownerID, token, workspace)
	return service, subject, handle, workspace, true
}

// restoreContinuationToken returns a previously popped continuation token to
// its owner's slot when the bind that consumed the slot did not complete.
// It never overwrites a newer chained token, and it only restores a token
// that is still a live ingress (a bind that failed after consuming the token
// leaves nothing restorable).
func (i *desktopCodingCapabilityIssuer) restoreContinuationToken(ownerID, token string) {
	ownerID = strings.TrimSpace(ownerID)
	token = strings.TrimSpace(token)
	if i == nil || ownerID == "" || token == "" {
		return
	}
	i.mu.Lock()
	if _, live := i.ingress[token]; !live {
		i.mu.Unlock()
		return
	}
	if i.continuationTokens == nil {
		i.continuationTokens = make(map[string]string)
	}
	if i.continuationTokens[ownerID] == "" {
		i.continuationTokens[ownerID] = token
	}
	i.mu.Unlock()
}

// rearmContinuationToken mints the next single-use ingress for the current
// verified request after a successful bind and records the just-consumed
// token as chain proof for the next step. The workspace binding is carried
// over from the just-consumed ingress; it is never reconstructed from
// project paths, runtime tasks, or task text. When the plan runner armed a
// continuation budget for this owner, minting stops once the budget is
// exhausted so a dispatch bug cannot mint unbounded turns from one request.
func (i *desktopCodingCapabilityIssuer) rearmContinuationToken(ownerID, consumedToken string, workspace codingStaticWorkspaceBinding) {
	ownerID = strings.TrimSpace(ownerID)
	consumedToken = strings.TrimSpace(consumedToken)
	if i == nil || !isDesktopCodingOwner(ownerID) || consumedToken == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ingress == nil {
		return
	}
	if i.chainConsumed == nil {
		i.chainConsumed = make(map[string]map[string]struct{})
	}
	if i.chainConsumed[ownerID] == nil {
		i.chainConsumed[ownerID] = make(map[string]struct{})
	}
	i.chainConsumed[ownerID][consumedToken] = struct{}{}
	if i.continuationBudget != nil {
		if remaining, armed := i.continuationBudget[ownerID]; armed {
			if remaining <= 0 {
				return
			}
			i.continuationBudget[ownerID] = remaining - 1
		}
	}
	token := i.mintLocked(ownerID, workspace)
	if i.continuationTokens == nil {
		i.continuationTokens = make(map[string]string)
	}
	i.continuationTokens[ownerID] = token
}

// takeContinuationToken atomically pops the pending continuation token of
// one owner. The caller must present a token that was already consumed on
// this request's chain (the loop context retains the original request token,
// which qualifies for every later step). Without that proof the slot stays
// closed even though the owner string is known.
func (i *desktopCodingCapabilityIssuer) takeContinuationToken(ownerID, presentedToken string) string {
	if i == nil {
		return ""
	}
	ownerID = strings.TrimSpace(ownerID)
	presentedToken = strings.TrimSpace(presentedToken)
	i.mu.Lock()
	defer i.mu.Unlock()
	if presentedToken == "" {
		return ""
	}
	if _, ok := i.chainConsumed[ownerID][presentedToken]; !ok {
		return ""
	}
	token := i.continuationTokens[ownerID]
	delete(i.continuationTokens, ownerID)
	return token
}

// armContinuationBudget caps how many continuation tokens the current
// request of this owner may still mint. Each plan step may legitimately bind
// one writer plus one reviewer, with slack for a retry dispatch; the cap
// turns a runaway dispatch loop into a visible pre-dispatch gate failure
// instead of an unbounded turn chain.
func (i *desktopCodingCapabilityIssuer) armContinuationBudget(ownerID string, steps int) {
	if i == nil || !isDesktopCodingOwner(ownerID) {
		return
	}
	if steps < 1 {
		steps = 1
	}
	i.mu.Lock()
	if i.continuationBudget == nil {
		i.continuationBudget = make(map[string]int)
	}
	i.continuationBudget[strings.TrimSpace(ownerID)] = 2*steps + 2
	i.mu.Unlock()
}

// nextRelation is the first production R1 ingress. It is intentionally
// restricted to desktop session owners created by the Wails host. IM user
// IDs, LoopContext IDs, request IDs, project paths and remote SSH session
// IDs never reach this function as semantic identity values.
//
// Desktop is a locally authenticated installation: tenant/principal are host
// constants, while the session ID is generated once per UI owner. The UI
// owner only selects which host session record to load; it is not copied
// into TenantID, PrincipalID, SessionID or RootTaskID.
func (i *desktopCodingCapabilityIssuer) nextRelation(dataDir, token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, bool) {
	service, subject, handle, _, ok := i.nextRelationWithWorkspace(dataDir, token, ownerID)
	return service, subject, handle, ok
}

// nextRelationWithWorkspace is the one atomic consumption path for an
// authenticated desktop Coding ingress. The returned workspace is an opaque,
// host-issued binding from that same request, not a lookup made using the
// just-created semantic relation or runtime attempt.
func (i *desktopCodingCapabilityIssuer) nextRelationWithWorkspace(dataDir, token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if i == nil || !isDesktopCodingOwner(ownerID) {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	i.mu.Lock()
	token = strings.TrimSpace(token)
	ingress, permitted := i.ingress[token]
	currentGeneration := i.generations[ownerID]
	if !permitted || ingress.ownerID != ownerID || ingress.generation != currentGeneration || !time.Now().UTC().Before(ingress.expiresAt) {
		i.mu.Unlock()
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	// Consume before issuing a relation. A request-scoped token is a
	// capability to enter R1 exactly once, not a lookup key: allowing two
	// local/remote dispatch paths to read it would mint consecutive turns for
	// one request and invalidate whichever handle binds second. Its
	// generation is checked again below while holding the relation lock, so a
	// new-task/cancel fence racing after consumption cannot let this old
	// request resurrect a relation.
	delete(i.ingress, token)
	i.mu.Unlock()
	service, err := i.relationService(dataDir)
	if err != nil || service == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	now := time.Now().UTC()
	i.sessionsMu.Lock()
	defer i.sessionsMu.Unlock()
	i.mu.Lock()
	stillCurrent := i.generations[ownerID] == ingress.generation
	i.mu.Unlock()
	if !stillCurrent {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	if i.sessions == nil {
		i.sessions = make(map[string]desktopCodingTaskSession)
	}
	current, exists := i.sessions[ownerID]
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
			// A cancelled/expired/replayed handle starts no dynamic-capable
			// task. Callers retain a static Coding surface instead of
			// silently changing the semantic root behind the user's active
			// desktop session.
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		current.handle = next
	}
	i.sessions[ownerID] = current
	return service, current.subject, current.handle, ingress.workspace, true
}

func (i *desktopCodingCapabilityIssuer) issueStaticWorkspaceLocked(ownerID, workspaceDir string, now time.Time) codingStaticWorkspaceBinding {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if i == nil || workspaceDir == "" {
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
	if i.staticWorkspaces == nil {
		i.staticWorkspaces = make(map[string]desktopCodingStaticWorkspace)
	}
	for handle, workspace := range i.staticWorkspaces {
		if !now.Before(workspace.expiresAt) {
			delete(i.staticWorkspaces, handle)
		}
	}
	handle := "coding-workspace-" + uuid.NewString()
	i.staticWorkspaces[handle] = desktopCodingStaticWorkspace{ownerID: ownerID, directory: abs, expiresAt: now.Add(30 * time.Minute)}
	return codingStaticWorkspaceBinding{WorkspaceHandle: handle, HostKind: "local"}
}

// resolveStaticWorkspace is intentionally unused by S1-A. It is the future
// S1-B adapter seam and proves that a catalog binding can only be resolved
// through host state for its original desktop owner.
func (i *desktopCodingCapabilityIssuer) resolveStaticWorkspace(ownerID string, binding codingStaticWorkspaceBinding) (string, bool) {
	if i == nil || !binding.complete() {
		return "", false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	workspace, ok := i.staticWorkspaces[strings.TrimSpace(binding.WorkspaceHandle)]
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

// revokeRelation is the desktop cancel/clear fence. It revokes active
// descendant child turns through the relation service before dropping the UI
// owner mapping, so a late child cannot bind a new attempt.
func (i *desktopCodingCapabilityIssuer) revokeRelation(dataDir, ownerID string) {
	i.fence(dataDir, ownerID, true)
}

// fence invalidates the current ingress generation before touching the
// durable handle. The generation makes the fence survive a token's
// consume-to-bind interval; removing only map entries cannot close that
// race. The relation lock is then taken before the generation recheck in
// nextRelationWithWorkspace, so no attempt can bind across this boundary.
func (i *desktopCodingCapabilityIssuer) fence(dataDir, ownerID string, revokeRelation bool) {
	if i == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	// A token only proves descent from one host request. It does not embed a
	// relation generation, so leaving an already-issued token live after a
	// cancel/clear/new-task fence would let a late old request mint another
	// relation after the owner mapping was dropped. Remove every outstanding
	// token for this owner before revoking the durable handle.
	i.mu.Lock()
	if i.generations == nil {
		i.generations = make(map[string]uint64)
	}
	i.generations[ownerID]++
	for token, ingress := range i.ingress {
		if ingress.ownerID == ownerID {
			delete(i.ingress, token)
		}
	}
	// Workspace handles are request-scoped capability context too. Leaving
	// one resolvable after a new-task/cancel fence would let a stale future
	// S1-B adapter execute against the old project even though its semantic
	// lineage had already been revoked.
	for handle, workspace := range i.staticWorkspaces {
		if workspace.ownerID == ownerID {
			delete(i.staticWorkspaces, handle)
		}
	}
	// A chained continuation token is the same kind of outstanding
	// capability: it must not survive a cancel/clear/new-task fence either.
	delete(i.continuationTokens, ownerID)
	delete(i.chainConsumed, ownerID)
	delete(i.continuationBudget, ownerID)
	i.mu.Unlock()
	if !revokeRelation {
		return
	}
	i.sessionsMu.Lock()
	current, ok := i.sessions[ownerID]
	delete(i.sessions, ownerID)
	i.sessionsMu.Unlock()
	if !ok {
		return
	}
	service, err := i.relationService(dataDir)
	if err == nil && service != nil {
		_ = service.RevokeCodingTaskHandle(current.subject, current.handle, time.Now().UTC())
	}
}

// App wrappers below keep the historical App-scoped call sites stable while
// delegating every capability decision to the issuer.

func (a *App) codingTaskRelationServiceForApp() (*codingTaskRelationService, error) {
	if a == nil {
		return nil, fmt.Errorf("coding task relation host is unavailable")
	}
	return a.codingCaps.relationService(a.GetDataDir())
}

func (a *App) closeCodingTaskRelationService() {
	if a == nil {
		return
	}
	a.codingCaps.closeRelationService()
}

func (a *App) beginDesktopCodingTaskIngress(ownerID string) string {
	if a == nil {
		return ""
	}
	return a.codingCaps.beginIngress(ownerID)
}

func (a *App) beginDesktopCodingTaskIngressWithWorkspace(ownerID, workspaceDir string) string {
	if a == nil {
		return ""
	}
	return a.codingCaps.beginIngressWithWorkspace(ownerID, workspaceDir)
}

func (a *App) beginDesktopCodingTaskIngressForRequest(ownerID string, startNewTask bool) string {
	if a == nil {
		return ""
	}
	return a.codingCaps.beginIngressForRequest(a.GetDataDir(), ownerID, startNewTask)
}

func (a *App) beginDesktopCodingTaskIngressForRequestWithWorkspace(ownerID, workspaceDir string, startNewTask bool) string {
	if a == nil {
		return ""
	}
	return a.codingCaps.beginIngressForRequestWithWorkspace(a.GetDataDir(), ownerID, workspaceDir, startNewTask)
}

func (a *App) endDesktopCodingTaskIngress(token string) {
	if a == nil {
		return
	}
	a.codingCaps.endIngress(token)
}

func (a *App) endDesktopCodingTaskIngressForOwner(ownerID string) {
	if a == nil {
		return
	}
	a.codingCaps.endIngressForOwner(ownerID)
}

func (a *App) nextDesktopCodingRootStepRelation(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	if a == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	return a.codingCaps.nextRootStepRelation(a.GetDataDir(), token, ownerID)
}

func (a *App) nextDesktopCodingTaskRelation(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, bool) {
	if a == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, false
	}
	return a.codingCaps.nextRelation(a.GetDataDir(), token, ownerID)
}

func (a *App) nextDesktopCodingTaskRelationWithWorkspace(token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	if a == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	return a.codingCaps.nextRelationWithWorkspace(a.GetDataDir(), token, ownerID)
}

func (a *App) armDesktopCodingContinuationBudget(ownerID string, steps int) {
	if a == nil {
		return
	}
	a.codingCaps.armContinuationBudget(ownerID, steps)
}

func (a *App) resolveDesktopCodingStaticWorkspace(ownerID string, binding codingStaticWorkspaceBinding) (string, bool) {
	if a == nil {
		return "", false
	}
	return a.codingCaps.resolveStaticWorkspace(ownerID, binding)
}

func (a *App) revokeDesktopCodingTaskRelation(ownerID string) {
	if a == nil {
		return
	}
	a.codingCaps.revokeRelation(a.GetDataDir(), ownerID)
}

func (a *App) fenceDesktopCodingTaskRelation(ownerID string, revokeRelation bool) {
	if a == nil {
		return
	}
	a.codingCaps.fence(a.GetDataDir(), ownerID, revokeRelation)
}
