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

// codingRuntimeDataDirResetWait bounds how long replacement teardown waits
// for a cancelled executor to leave the old ledger before the durable sweep
// marks that attempt interrupted anyway. It is a var so tests can shrink the
// quiescence window without weakening production cancellation semantics.
var codingRuntimeDataDirResetWait = 2 * time.Second

// codingAuthorizationDataDirForApp returns the data-dir epoch currently
// visible to coding authorization stores. The epoch intentionally lags
// GetDataDir while a switch is quiescing old runners and copying history, so
// no replacement-ledger write can appear before the migration commit point.
func (a *App) codingAuthorizationDataDirForApp() string {
	if a == nil {
		return ""
	}
	if dir, ok := a.codingAuthorizationDataDir.Load().(string); ok && strings.TrimSpace(dir) != "" {
		return filepath.Clean(dir)
	}
	dir := filepath.Clean(a.GetDataDir())
	a.codingAuthorizationDataDir.Store(dir)
	return dir
}

// openExistingCodingRuntimeStore returns the ledger when this process already
// has it open, or when a previous run left the database on disk. It does not
// create the file. The task list calls this on every refresh, and opening a
// missing database would invent a ledger for an app that never runs coding work.
func (a *App) openExistingCodingRuntimeStore() *codingruntime.SQLiteStore {
	if a == nil {
		return nil
	}
	a.codingRuntimeStoreMu.Lock()
	defer a.codingRuntimeStoreMu.Unlock()
	if a.codingRuntimeStoreClosed {
		return nil
	}
	dir := a.codingAuthorizationDataDirForApp()
	if store := a.codingRuntimeStore; store != nil {
		if sameCodingDataDirPath(a.codingRuntimeStorePath, dir) {
			return store
		}
		a.codingRuntimeStore = nil
		a.codingRuntimeStorePath = ""
		if err := store.Close(); err != nil {
			log.Printf("[coding-runtime] close replaced ledger failed: %v", err)
		}
	}
	// A missing final database does not mean there is no existing ledger while
	// a migration journal is present: the interrupted copy may simply not have
	// reached the install phase yet. Recovery is not ledger creation; it is the
	// only way this read-only probe can see the history that already belongs here.
	if codingAuthorizationMigrationPending(dir) {
		if err := resumeCodingAuthorizationMigrationIfPending(dir); err != nil {
			log.Printf("[coding-runtime] existing-ledger probe could not resume migration in %q: %v", dir, err)
			return nil
		}
	}
	dbPath := filepath.Join(dir, "coding_runtime.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	return a.openCodingRuntimeStoreLocked(dir)
}

// ensureCodingRuntimeStore opens the app-level execution ledger once. Keeping
// it application-scoped gives startup a chance to mark abandoned leases as
// interrupted instead of silently losing their attempt history.
func (a *App) ensureCodingRuntimeStore() *codingruntime.SQLiteStore {
	if a == nil {
		return nil
	}
	a.codingRuntimeStoreMu.Lock()
	defer a.codingRuntimeStoreMu.Unlock()
	if a.codingRuntimeStoreClosed {
		return nil
	}
	return a.openCodingRuntimeStoreLocked(a.codingAuthorizationDataDirForApp())
}

func (a *App) openCodingRuntimeStoreLocked(dir string) *codingruntime.SQLiteStore {
	if codingAuthorizationMigrationPending(dir) {
		if err := resumeCodingAuthorizationMigrationIfPending(dir); err != nil {
			log.Printf("[coding-runtime] interrupted data-dir migration in %q could not be resumed: %v", dir, err)
			return nil
		}
	}
	if codingAuthorizationMigrationPending(dir) {
		log.Printf("[coding-runtime] ledger open deferred: interrupted data-dir migration in %q", dir)
		return nil
	}
	if a.codingRuntimeStore != nil {
		if sameCodingDataDirPath(a.codingRuntimeStorePath, dir) {
			return a.codingRuntimeStore
		}
		old := a.codingRuntimeStore
		a.codingRuntimeStore = nil
		a.codingRuntimeStorePath = ""
		if err := old.Close(); err != nil {
			log.Printf("[coding-runtime] close replaced ledger failed: %v", err)
		}
	}
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
	// Process restart kills every executor and every dispatch queue. Rows
	// still inside their lease window and queued rows would otherwise keep
	// the task list at 进行中 until the lease clock runs out — or forever.
	if abandoned, err := store.InterruptAbandonedRuns(time.Now().UTC()); err != nil {
		log.Printf("[coding-runtime] reconcile abandoned runs failed: %v", err)
	} else if len(abandoned) > 0 {
		log.Printf("[coding-runtime] marked %d abandoned attempt(s) interrupted at store open; recovery requires read-only probe", len(abandoned))
	}
	a.codingRuntimeStore = store
	a.codingRuntimeStorePath = dir
	return store
}

func (a *App) closeCodingRuntimeStore() {
	if a == nil {
		return
	}
	a.codingRuntimeStoreMu.Lock()
	defer a.codingRuntimeStoreMu.Unlock()
	store := a.codingRuntimeStore
	a.codingRuntimeStore = nil
	a.codingRuntimeStorePath = ""
	a.codingRuntimeStoreClosed = true
	if store != nil {
		_ = store.Close()
	}
}

func (a *App) retireCodingRuntimeStoreForDataDirChange() {
	if a == nil {
		return
	}
	a.retireCodingRuntimeStoreForDataDirChangeTo(filepath.Clean(a.GetDataDir()))
}

func (a *App) retireCodingRuntimeStoreForDataDirChangeTo(replacementDir string) {
	if a == nil {
		return
	}
	replacementDir = filepath.Clean(strings.TrimSpace(replacementDir))
	a.codingRuntimeStoreMu.Lock()
	store := a.codingRuntimeStore
	storePath := a.codingRuntimeStorePath
	a.codingRuntimeStoreMu.Unlock()
	if store != nil && storePath != "" && !sameCodingDataDirPath(storePath, replacementDir) {
		store.RetireForReplacement()
	}
}

func (a *App) resetCodingRuntimeStoreForDataDirChange() {
	// Runtime and relation authorization state share one data-dir epoch.
	// Resetting only the ledger would leave relation capabilities and durable
	// migration behind, so even the legacy runtime-named entry point uses the
	// combined lifecycle reset.
	a.resetCodingAuthorizationStoresForDataDirChange("")
}

// resetCodingAuthorizationStoresForDataDirChange is the synchronized coding
// half of a data-dir switch. It quiesces old runners first, then holds both
// store lifecycle locks while the relation service and runtime ledger are
// retired and their durable files are copied. Holding both locks closes the
// race where a concurrent caller opens the replacement database between
// teardown and migration and thereby makes the migration decision mixed.
func (a *App) resetCodingAuthorizationStoresForDataDirChange(fallbackSourceDir string) {
	a.resetCodingAuthorizationStoresForDataDirChangeTo(fallbackSourceDir, filepath.Clean(a.GetDataDir()))
}

func (a *App) resetCodingAuthorizationStoresForDataDirChangeTo(fallbackSourceDir, replacementDir string) {
	if a == nil {
		return
	}
	replacementDir = filepath.Clean(strings.TrimSpace(replacementDir))
	if replacementDir == "" || replacementDir == "." {
		log.Printf("[coding-runtime] data-dir reset skipped: replacement data directory is empty")
		return
	}
	if committed, ok := a.codingAuthorizationDataDir.Load().(string); ok && strings.TrimSpace(committed) != "" && sameCodingDataDirPath(committed, replacementDir) {
		// Rapid config edits can be coalesced before the coding epoch ever moves
		// (for example A→B→A). In that case no coding-authorization transition
		// linearized, so the stores must not be half-cancelled as if one had.
		return
	}
	replacementLedgerPath := filepath.Join(replacementDir, "coding_runtime.db")
	a.retireCodingRuntimeStoreForDataDirChangeTo(replacementDir)
	codingruntime.CancelLiveCodingAttemptsOutsideSQLiteStorePath(replacementLedgerPath)
	if !codingruntime.WaitLiveCodingAttemptsOutsideSQLiteStorePath(replacementLedgerPath, codingRuntimeDataDirResetWait) {
		log.Printf("[coding-runtime] data-dir reset timed out waiting for cancelled attempts to stop")
	}

	// Lock order is fixed as runtime-store then relation-issuer. No other path
	// acquires both locks; relations handed to callers do not depend on the
	// app-level store mutex while executing their already-linearized critical
	// sections.
	a.codingRuntimeStoreMu.Lock()
	a.codingCaps.relationsMu.Lock()
	defer a.codingCaps.relationsMu.Unlock()
	defer a.codingRuntimeStoreMu.Unlock()
	defer codingruntime.CancelLiveCodingAttemptsOutsideSQLiteStorePath(replacementLedgerPath)
	// Commit the new coding epoch before releasing either lifecycle lock. This
	// is the linearization point after which callers may open the replacement
	// stores; before it they continue to see the retired directory and are
	// cancellation-swept as transition-era work.
	defer a.codingAuthorizationDataDir.Store(replacementDir)

	runtimeSourceDir := a.codingRuntimeStorePath
	relationSourceDir := a.codingCaps.relationsPath
	if !a.codingCaps.relationsClosed {
		a.codingCaps.closeRelationServiceLocked()
		a.codingCaps.invalidateRelationCapabilitiesForDataDirChange()
	}
	if !a.codingRuntimeStoreClosed {
		store := a.codingRuntimeStore
		storePath := a.codingRuntimeStorePath
		if store == nil || !sameCodingDataDirPath(storePath, replacementDir) {
			a.codingRuntimeStore = nil
			a.codingRuntimeStorePath = ""
			if store != nil {
				store.RetireForReplacement()
				if interrupted, err := store.InterruptRunsForReplacement(time.Now().UTC()); err != nil {
					log.Printf("[coding-runtime] interrupt data-dir attempts before close failed: %v", err)
				} else if len(interrupted) > 0 {
					log.Printf("[coding-runtime] marked %d data-dir attempt(s) interrupted before ledger close", len(interrupted))
				}
				if err := store.Close(); err != nil {
					log.Printf("[coding-runtime] close data-dir ledger failed: %v", err)
				}
			}
		}
	}
	codingruntime.CancelLiveCodingAttemptsOutsideSQLiteStorePath(replacementLedgerPath)

	sourceDir := ""
	contradictorySource := false
	mergeSource := func(dir string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" || sameCodingDataDirPath(dir, replacementDir) {
			return
		}
		if sourceDir == "" {
			sourceDir = dir
			return
		}
		if !sameCodingDataDirPath(sourceDir, dir) {
			contradictorySource = true
		}
	}
	// Paths recorded on actually-open stores and the not-yet-committed coding
	// epoch are stronger evidence than the pending config transition: they
	// describe the files this process had live or the directory it most recently
	// committed as authoritative. The epoch matters for rapid A→B→C switches
	// where B was committed but never opened; a stale pre-commit fallback would
	// otherwise skip B and copy A directly into C. The config value is only the
	// fallback for durable files never opened here.
	mergeSource(runtimeSourceDir)
	mergeSource(relationSourceDir)
	mergeSource(a.codingAuthorizationDataDirForApp())
	if sourceDir == "" {
		sourceDir = filepath.Clean(strings.TrimSpace(fallbackSourceDir))
		if sourceDir == "." {
			sourceDir = ""
		}
	}
	if contradictorySource {
		log.Printf("[coding-runtime] data-dir migration skipped: runtime, relation, or epoch source directories disagree (%q vs %q)", runtimeSourceDir, relationSourceDir)
		return
	}
	if sourceDir == "." || sourceDir == "" || sameCodingDataDirPath(sourceDir, replacementDir) {
		return
	}
	migrated, err := migrateCodingAuthorizationStoresSerialized(sourceDir, replacementDir)
	if err != nil {
		log.Printf("[coding-runtime] data-dir coding store migration failed: %v", err)
		return
	}
	if migrated {
		log.Printf("[coding-runtime] migrated coding runtime/relation history from %q to %q", sourceDir, replacementDir)
	}
}

// desktopCodingVerifiedSurfaceUnavailableDiagnostic is the user-facing
// reason returned by the pre-dispatch capability gate when an implementation
// or operational coding step reaches the local desktop runner without a
// verified task relation. It deliberately names the user action that fixes
// the condition instead of exposing internal surface terminology.
const desktopCodingVerifiedSurfaceUnavailableDiagnostic = "未获得本机编程执行授权：本次派发没有携带有效的桌面执行凭证，实现/运行类步骤无法启动。请回到编程工作台重新发送任务，或重新批准执行计划后重试。"

// desktopCodingWritableSurfaceBindFailedDiagnostic is returned when a desktop
// implementation turn did receive a relation handle, but that handle did not
// become a correlated writable surface (semantic anchor or workspace binding
// incomplete). The model must not be started on the read-only compatibility
// surface for a task that can only finish by writing files or running commands.
const desktopCodingWritableSurfaceBindFailedDiagnostic = "编程执行凭证没有绑定到可写执行面（语义锚点或工作区句柄不完整），实现步骤已停止，没有进入只读检索循环。请重新发送这一次任务。"

// desktopCodingRequestLease is the lifetime of every in-process desktop
// Coding credential held by a live turn: the initial ingress token, each
// continuation, and the workspace handle. It matches the lease
// CreateCodingTask and VerifyCodingContinuation grant the semantic relation.
// A continuation is minted when a step binds and consumed when the next step
// starts, so a shorter fuse measured from bind — or restarted when the step
// returns — expires during the step or the following verify gap. The owner's
// turn window sweeps these credentials when it closes; this lease is only the
// backstop for a window that never reaches its teardown.
const desktopCodingRequestLease = 24 * time.Hour

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
	relationsMu     sync.Mutex
	relations       *codingTaskRelationService
	relationsPath   string
	relationsClosed bool

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
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("coding task relation data directory is required")
	}
	dataDir = filepath.Clean(dataDir)
	i.relationsMu.Lock()
	defer i.relationsMu.Unlock()
	if i.relationsClosed {
		return nil, fmt.Errorf("coding task relation host is closed")
	}
	if i.relations != nil {
		if sameCodingDataDirPath(i.relationsPath, dataDir) {
			return i.relations, nil
		}
		i.closeRelationServiceLocked()
		i.invalidateRelationCapabilitiesForDataDirChange()
	}
	if codingAuthorizationMigrationPending(dataDir) {
		if err := resumeCodingAuthorizationMigrationIfPending(dataDir); err != nil {
			return nil, fmt.Errorf("resume coding task relation migration in %q: %w", dataDir, err)
		}
	}
	if codingAuthorizationMigrationPending(dataDir) {
		return nil, fmt.Errorf("coding task relation migration is interrupted in %q", dataDir)
	}
	service, err := newCodingTaskRelationService(filepath.Join(dataDir, "coding_task_relations.db"))
	if err != nil {
		return nil, err
	}
	i.relations = service
	i.relationsPath = dataDir
	return service, nil
}

func (i *desktopCodingCapabilityIssuer) closeRelationServiceLocked() {
	service := i.relations
	i.relations = nil
	i.relationsPath = ""
	if service != nil {
		if err := service.Close(); err != nil {
			log.Printf("[coding-runtime] close task relation service failed: %v", err)
		}
	}
}

func (i *desktopCodingCapabilityIssuer) invalidateRelationCapabilitiesForDataDirChange() {
	owners := make(map[string]struct{})
	i.sessionsMu.Lock()
	defer i.sessionsMu.Unlock()
	i.mu.Lock()
	defer i.mu.Unlock()
	for ownerID := range i.sessions {
		owners[ownerID] = struct{}{}
	}
	i.sessions = nil
	if i.generations == nil {
		i.generations = make(map[string]uint64)
	}
	for ownerID := range i.generations {
		owners[ownerID] = struct{}{}
	}
	for _, ingress := range i.ingress {
		owners[ingress.ownerID] = struct{}{}
	}
	for _, workspace := range i.staticWorkspaces {
		owners[workspace.ownerID] = struct{}{}
	}
	for ownerID := range i.continuationTokens {
		owners[ownerID] = struct{}{}
	}
	for ownerID := range i.chainConsumed {
		owners[ownerID] = struct{}{}
	}
	for ownerID := range i.continuationBudget {
		owners[ownerID] = struct{}{}
	}
	for ownerID := range owners {
		i.generations[ownerID]++
	}
	i.ingress = nil
	i.staticWorkspaces = nil
	i.continuationTokens = nil
	i.chainConsumed = nil
	i.continuationBudget = nil
}

func (i *desktopCodingCapabilityIssuer) closeRelationService() {
	if i == nil {
		return
	}
	// Close is terminal for this issuer. Keep the issuer lock while Close waits
	// for an in-flight bind or lifecycle transition: a late caller must not open
	// a second service for the same database either during the old cross-store
	// critical section or after process shutdown has begun.
	i.relationsMu.Lock()
	defer i.relationsMu.Unlock()
	i.relationsClosed = true
	i.closeRelationServiceLocked()
}

func (i *desktopCodingCapabilityIssuer) resetRelationServiceForDataDirChange() {
	if i == nil {
		return
	}
	i.relationsMu.Lock()
	defer i.relationsMu.Unlock()
	if i.relationsClosed {
		return
	}
	i.closeRelationServiceLocked()
	i.invalidateRelationCapabilitiesForDataDirChange()
}

// beginIngress mints one bare request credential. It deliberately does not
// decide semantic task boundaries or workspace authority: explicit cancel,
// clear, and new-task paths fence separately, while the local dispatch path
// binds the materialized execution surface atomically at consumption time.
func (i *desktopCodingCapabilityIssuer) beginIngress(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if i == nil || !isDesktopCodingOwner(ownerID) {
		return ""
	}
	i.mu.Lock()
	if i.ingress == nil {
		i.ingress = make(map[string]desktopCodingIngress)
	}
	token := i.mintLocked(ownerID, codingStaticWorkspaceBinding{}, desktopCodingRequestLease)
	i.mu.Unlock()
	return token
}

// selectRootStepIngressLocked chooses the exact live ingress that may be
// consumed for this root step: the presented request token itself, or the
// pending continuation when the presented token is already consumed chain
// proof. Callers must hold i.mu.
func (i *desktopCodingCapabilityIssuer) selectRootStepIngressLocked(ownerID, presentedToken string, now time.Time) (string, desktopCodingIngress, bool) {
	if ingress, ok := i.ingress[presentedToken]; ok {
		if !i.ingressUsableLocked(ownerID, ingress, now) {
			return "", desktopCodingIngress{}, false
		}
		return presentedToken, ingress, true
	}
	if _, ok := i.chainConsumed[ownerID][presentedToken]; !ok {
		return "", desktopCodingIngress{}, false
	}
	continuation := strings.TrimSpace(i.continuationTokens[ownerID])
	if continuation == "" {
		return "", desktopCodingIngress{}, false
	}
	ingress, ok := i.ingress[continuation]
	if !ok || !i.ingressUsableLocked(ownerID, ingress, now) {
		return "", desktopCodingIngress{}, false
	}
	return continuation, ingress, true
}

// bindExecutionWorkspaceLocked retargets the request or pending continuation
// ingress to the exact host-selected execution surface and returns the exact
// ingress token that was bound. The directory may have been materialized only
// after the turn token was minted. Callers must hold i.mu. A failed bind
// clears stale workspace authority from that ingress before returning, so a
// missing surface can never fall back to the previous step's directory.
func (i *desktopCodingCapabilityIssuer) bindExecutionWorkspaceLocked(ownerID, presentedToken, workspaceDir string, now time.Time) (string, bool) {
	workspaceDir = strings.TrimSpace(workspaceDir)
	surfaceValid := workspaceDir != ""
	abs := ""
	if surfaceValid {
		var err error
		abs, err = filepath.Abs(workspaceDir)
		if err != nil {
			surfaceValid = false
		} else {
			abs = normalizeProjectSessionPath(abs)
		}
	}

	target, ingress, ok := i.selectRootStepIngressLocked(ownerID, presentedToken, now)
	if !ok {
		return "", false
	}

	if surfaceValid && i.refreshMatchingWorkspaceLocked(ownerID, ingress.workspace, abs, now) {
		return target, true
	}
	ingress.workspace = codingStaticWorkspaceBinding{}
	i.ingress[target] = ingress
	if !surfaceValid {
		return "", false
	}
	workspace := i.issueStaticWorkspaceLocked(ownerID, abs, now)
	if !workspace.complete() {
		return "", false
	}
	ingress.workspace = workspace
	i.ingress[target] = ingress
	return target, true
}

// ingressUsableLocked reports whether ingress is still the current generation
// capability of ownerID. Callers must hold i.mu.
func (i *desktopCodingCapabilityIssuer) ingressUsableLocked(ownerID string, ingress desktopCodingIngress, now time.Time) bool {
	return ingress.ownerID == ownerID && ingress.generation == i.generations[ownerID] && now.Before(ingress.expiresAt)
}

// refreshMatchingWorkspaceLocked keeps one handle when the next step uses the
// same directory. Reusing it preserves the continuation chain's stable
// workspace identity; a different execution surface receives a fresh handle.
// Callers must hold i.mu.
func (i *desktopCodingCapabilityIssuer) refreshMatchingWorkspaceLocked(ownerID string, binding codingStaticWorkspaceBinding, workspaceDir string, now time.Time) bool {
	handle := strings.TrimSpace(binding.WorkspaceHandle)
	if !binding.complete() || handle == "" {
		return false
	}
	workspace, ok := i.staticWorkspaces[handle]
	if !ok || workspace.ownerID != ownerID || !sameNormalizedPath(workspace.directory, workspaceDir) || !now.Before(workspace.expiresAt) {
		return false
	}
	info, err := os.Stat(workspace.directory)
	if err != nil || !info.IsDir() {
		return false
	}
	deadline := now.Add(desktopCodingRequestLease)
	if workspace.expiresAt.Before(deadline) {
		workspace.expiresAt = deadline
		i.staticWorkspaces[handle] = workspace
	}
	return true
}

// mintLocked records a fresh single-use ingress token for ownerID under the
// current generation. Callers must hold i.mu. ttl is the request lease;
// a non-positive ttl falls back to that lease rather than a shorter fuse.
func (i *desktopCodingCapabilityIssuer) mintLocked(ownerID string, workspace codingStaticWorkspaceBinding, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = desktopCodingRequestLease
	}
	if i.generations == nil {
		i.generations = make(map[string]uint64)
	}
	if _, ok := i.generations[ownerID]; !ok {
		i.generations[ownerID] = 0
	}
	now := time.Now().UTC()
	var expired []string
	for id, ingress := range i.ingress {
		if !now.Before(ingress.expiresAt) {
			delete(i.ingress, id)
			expired = append(expired, id)
		}
	}
	if len(expired) > 0 && i.continuationTokens != nil {
		// The slot must not keep pointing at an ingress entry this sweep
		// just removed. A later step would pop a dead id and fail with no
		// expiry trace.
		gone := make(map[string]struct{}, len(expired))
		for _, id := range expired {
			gone[id] = struct{}{}
		}
		for owner, token := range i.continuationTokens {
			if _, ok := gone[token]; ok {
				delete(i.continuationTokens, owner)
				log.Printf("[coding-ingress] dropped expired continuation user=%s", owner)
			}
		}
	}
	token := "coding-ingress-" + uuid.NewString()
	i.ingress[token] = desktopCodingIngress{
		ownerID: ownerID, generation: i.generations[ownerID], expiresAt: now.Add(ttl),
		workspace: workspace,
	}
	return token
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
	// Turn end is an ingress-generation fence too. It does not revoke the
	// owner's durable semantic relation — the next turn may continue it — but
	// no capability minted inside this turn may recreate the chain after the
	// sweep, including a consumption whose rearm was still in flight.
	if i.generations == nil {
		i.generations = make(map[string]uint64)
	}
	i.generations[ownerID]++
	for token, ingress := range i.ingress {
		if ingress.ownerID == ownerID {
			delete(i.ingress, token)
		}
	}
	delete(i.continuationTokens, ownerID)
	delete(i.chainConsumed, ownerID)
	delete(i.continuationBudget, ownerID)
	// The workspace handle is request-scoped authority too. endIngress is the
	// teardown that makes a request-lease TTL safe: without this delete, a
	// handle would stay resolvable until the lease backstop.
	for handle, workspace := range i.staticWorkspaces {
		if workspace.ownerID == ownerID {
			delete(i.staticWorkspaces, handle)
		}
	}
	i.mu.Unlock()
}

func (i *desktopCodingCapabilityIssuer) ingressOverdue(token string) (time.Duration, bool) {
	if i == nil {
		return 0, false
	}
	token = strings.TrimSpace(token)
	i.mu.Lock()
	defer i.mu.Unlock()
	ingress, ok := i.ingress[token]
	if !ok {
		return 0, false
	}
	now := time.Now().UTC()
	if now.Before(ingress.expiresAt) {
		return 0, false
	}
	return now.Sub(ingress.expiresAt), true
}

// nextRootStepRelation resolves the verified relation for one root coding
// step of an authenticated desktop request. The request token authorizes the
// first root step; every successful bind re-arms exactly one single-use
// continuation token (same owner, current generation) so a multi-step plan —
// which dispatches each root step as its own R1 entry within one request —
// gets one verified turn per step on the same semantic root. The workspace
// binding is carried only as lease continuity until the host dispatch
// boundary rebinds that pending continuation to the exact surface selected
// for the next step. Each token is still consumed exactly once; a
// replayed or stale token fails closed; popping the continuation slot
// requires presenting a token already consumed on this chain, so the owner
// string alone never suffices; concurrent dispatches race for the single
// slot so at most one of them binds, exactly as before.
func (i *desktopCodingCapabilityIssuer) nextRootStepRelation(dataDir, token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	token = strings.TrimSpace(token)
	service, subject, handle, workspace, consumedIngress, ok := i.nextRelationWithWorkspaceAndIngress(dataDir, token, ownerID)
	if !ok {
		continuation := i.takeContinuationToken(ownerID, token)
		if continuation == "" {
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		service, subject, handle, workspace, consumedIngress, ok = i.nextRelationWithWorkspaceAndIngress(dataDir, continuation, ownerID)
		if !ok {
			// The bind failed after the slot was popped (for example a fence
			// landed in between, the continuation's request lease elapsed, or
			// the relation service errored). Return the token to the slot so a
			// later step may retry the same link; the proof requirement in
			// takeContinuationToken still keeps the slot closed to anyone who
			// never held this chain. An expired continuation is not retried
			// into a new lease here: extending it after the deadline would
			// hide a request that actually ended.
			if overdue, expired := i.ingressOverdue(continuation); expired {
				log.Printf("[coding-ingress] continuation expired %s before the next root step consumed it user=%s", overdue.Round(time.Second), ownerID)
			}
			i.restoreContinuationToken(ownerID, continuation)
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		token = continuation
	}
	i.rearmContinuationToken(ownerID, token, workspace, consumedIngress.generation)
	return service, subject, handle, workspace, true
}

// nextRootStepRelationForExecution is the local writable-dispatch consumption
// path. Binding the selected surface and consuming the single-use request or
// continuation token happen in the same issuer-critical section. That closes
// the race where two dispatched steps could otherwise retarget the pending
// ingress between a separate bind and consume, leaving one step with another
// step's workspace authority. A bind failure happens before the continuation
// slot is popped, so the chain remains retryable; a relation failure after
// consumption follows the existing single-use semantics and does not retry a
// consumed token. Read-only steps pass writableSurface=false: they consume the
// semantic relation but strip any stale workspace authority first.
func (i *desktopCodingCapabilityIssuer) nextRootStepRelationForExecution(dataDir, token, ownerID, workspaceDir string, writableSurface bool) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	ownerID = strings.TrimSpace(ownerID)
	token = strings.TrimSpace(token)
	if i == nil || !isDesktopCodingOwner(ownerID) || token == "" {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	i.mu.Lock()
	now := time.Now().UTC()
	target := ""
	ingress := desktopCodingIngress{}
	ok := false
	if writableSurface {
		target, ok = i.bindExecutionWorkspaceLocked(ownerID, token, workspaceDir, now)
		if ok {
			ingress, ok = i.ingress[target]
		}
	} else {
		// Inquiry and plan-phase steps are read-only by contract. They still
		// consume the semantic relation, but stale workspace authority carried
		// for continuation lease continuity is stripped before consumption.
		target, ingress, ok = i.selectRootStepIngressLocked(ownerID, token, now)
		if ok {
			ingress.workspace = codingStaticWorkspaceBinding{}
			i.ingress[target] = ingress
		}
	}
	if !ok {
		i.mu.Unlock()
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	// The consumed continuation must leave its slot even when the caller
	// presented that continuation token directly rather than presenting the
	// previous consumed token as chain proof. A slot may only ever name a
	// live, unconsumed continuation.
	if i.continuationTokens[ownerID] == target {
		delete(i.continuationTokens, ownerID)
	}
	delete(i.ingress, target)
	workspace := ingress.workspace
	i.mu.Unlock()

	service, subject, handle, _, ok := i.issueRelationForConsumedIngress(dataDir, ownerID, ingress)
	if !ok {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	i.rearmContinuationToken(ownerID, target, workspace, ingress.generation)
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
	ingress, live := i.ingress[token]
	if !live || !i.ingressUsableLocked(ownerID, ingress, time.Now().UTC()) {
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
// verified request after a successful bind, provided the consumed ingress
// generation is still current, and records the just-consumed
// token as chain proof for the next step. The workspace binding is carried
// over from the just-consumed ingress only so its lease continuity never
// depends on a path lookup; before the next step consumes the continuation,
// the host dispatch boundary must rebind it to that step's exact execution
// surface. It is never reconstructed from project paths, runtime tasks, or
// task text inside the subagent. The workspace lease slides on
// this bind even when the continuation budget refuses another turn: the step
// that just bound still has to resolve its directory. The new token, when
// minted, keeps the request lease until the next step consumes it or the
// request ends. When the plan runner armed a continuation budget for this
// owner, minting stops once the budget is exhausted so a dispatch bug cannot
// mint unbounded turns from one request.
func (i *desktopCodingCapabilityIssuer) rearmContinuationToken(ownerID, consumedToken string, workspace codingStaticWorkspaceBinding, consumedGeneration uint64) {
	ownerID = strings.TrimSpace(ownerID)
	consumedToken = strings.TrimSpace(consumedToken)
	if i == nil || !isDesktopCodingOwner(ownerID) || consumedToken == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	// The relation was issued before this rearm. A cancel/clear/new-task fence
	// or turn-end sweep may have landed in that gap; because the generation
	// check and mint share this critical section, the old request cannot
	// recreate a continuation chain after the fence.
	if i.generations[ownerID] != consumedGeneration {
		return
	}
	// This bind already extended the semantic relation. Refresh the directory
	// handle before the budget gate: a step that is allowed to run still needs
	// the workspace when the chain is not allowed to mint another turn.
	now := time.Now().UTC()
	i.slideStaticWorkspaceLeaseLocked(ownerID, workspace, now)
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
				log.Printf("[coding-ingress] continuation budget exhausted; next root step will not be armed user=%s", ownerID)
				return
			}
			i.continuationBudget[ownerID] = remaining - 1
		}
	}
	token := i.mintLocked(ownerID, workspace, desktopCodingRequestLease)
	if i.continuationTokens == nil {
		i.continuationTokens = make(map[string]string)
	}
	i.continuationTokens[ownerID] = token
}

// slideStaticWorkspaceLeaseLocked extends a host-issued workspace handle to
// the request lease. Callers must hold i.mu. A missing or foreign handle is
// left untouched: this never recreates a directory binding from a path.
func (i *desktopCodingCapabilityIssuer) slideStaticWorkspaceLeaseLocked(ownerID string, binding codingStaticWorkspaceBinding, now time.Time) {
	handle := strings.TrimSpace(binding.WorkspaceHandle)
	ownerID = strings.TrimSpace(ownerID)
	if i == nil || handle == "" || ownerID == "" || i.staticWorkspaces == nil {
		return
	}
	workspace, ok := i.staticWorkspaces[handle]
	if !ok || workspace.ownerID != ownerID {
		return
	}
	deadline := now.Add(desktopCodingRequestLease)
	if workspace.expiresAt.Before(deadline) {
		workspace.expiresAt = deadline
		i.staticWorkspaces[handle] = workspace
	}
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

// nextRelationWithWorkspaceAndIngress consumes one ingress and returns the
// consumed ingress facts needed by the root-step chain. The generation must
// travel with the consumption result: a fence can land after relation issuance
// but before the continuation is rearmed, and using the then-current generation
// would mistakenly let the old request recreate a chain after that fence.
func (i *desktopCodingCapabilityIssuer) nextRelationWithWorkspaceAndIngress(dataDir, token, ownerID string) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, desktopCodingIngress, bool) {
	ownerID = strings.TrimSpace(ownerID)
	if i == nil || !isDesktopCodingOwner(ownerID) {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, desktopCodingIngress{}, false
	}
	i.mu.Lock()
	token = strings.TrimSpace(token)
	ingress, permitted := i.ingress[token]
	currentGeneration := i.generations[ownerID]
	if !permitted || ingress.ownerID != ownerID || ingress.generation != currentGeneration || !time.Now().UTC().Before(ingress.expiresAt) {
		i.mu.Unlock()
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, desktopCodingIngress{}, false
	}
	// Consume before issuing a relation. A request-scoped token is a
	// capability to enter R1 exactly once, not a lookup key: allowing two
	// local/remote dispatch paths to read it would mint consecutive turns for
	// one request and invalidate whichever handle binds second.
	delete(i.ingress, token)
	i.mu.Unlock()
	service, subject, handle, workspace, ok := i.issueRelationForConsumedIngress(dataDir, ownerID, ingress)
	return service, subject, handle, workspace, ingress, ok
}

// issueRelationForConsumedIngress creates or continues the owner's durable
// semantic relation for an ingress token already consumed by the caller. The
// ingress generation is checked again while holding the relation lock, so a
// new-task/cancel fence racing after consumption cannot let this old request
// resurrect a relation.
func (i *desktopCodingCapabilityIssuer) issueRelationForConsumedIngress(dataDir, ownerID string, ingress desktopCodingIngress) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
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
		handle, createErr := service.CreateCodingTask(subject, now, desktopCodingRequestLease)
		if createErr != nil {
			return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
		}
		current = desktopCodingTaskSession{subject: subject, handle: handle}
	} else {
		next, continuationErr := service.VerifyCodingContinuation(current.subject, current.handle, now, desktopCodingRequestLease)
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
		if now.Before(workspace.expiresAt) {
			continue
		}
		// A live ingress still names this handle. Deleting it would let the
		// next bind succeed and then fail directory resolution; that bind
		// slides the lease instead.
		if i.workspaceReferencedByLiveIngressLocked(handle, now) {
			continue
		}
		delete(i.staticWorkspaces, handle)
	}
	handle := "coding-workspace-" + uuid.NewString()
	i.staticWorkspaces[handle] = desktopCodingStaticWorkspace{ownerID: ownerID, directory: abs, expiresAt: now.Add(desktopCodingRequestLease)}
	return codingStaticWorkspaceBinding{WorkspaceHandle: handle, HostKind: "local"}
}

func (i *desktopCodingCapabilityIssuer) workspaceReferencedByLiveIngressLocked(handle string, now time.Time) bool {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return false
	}
	for _, ingress := range i.ingress {
		if !now.Before(ingress.expiresAt) {
			continue
		}
		if strings.TrimSpace(ingress.workspace.WorkspaceHandle) == handle {
			return true
		}
	}
	return false
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
	return a.codingCaps.relationService(a.codingAuthorizationDataDirForApp())
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

// attachDesktopCodingIngressForTurn mints the turn-scoped desktop Coding
// credential for one foreground turn of a desktop coding workbench owner.
//
// The credential belongs to the TURN, not to whichever component happened to
// dispatch it. Minting it at each dispatch site let host-authored turns that
// nobody remembered to instrument (session round waves, degraded-turn retries)
// reach the workbench with nothing to present, where the pre-dispatch gate
// refuses every implementation step. Minting it at the request boundary was
// no better: that request can wait on the owner's turn lock for up to a
// minute, and the turn still running sweeps every token of the owner when it
// ends. So the owner's serialized turn window is the only place that mints,
// and it sweeps the whole chain when that window closes.
//
// It returns "" and does nothing when the turn is background work (never
// serialized as a turn, must not hold write authority) or when the owner is
// not a desktop coding workbench session.
//
// Callers must hold the owner's turn lock: the matching
// endDesktopCodingTaskIngressForOwner sweep removes every ingress of the owner,
// so it may only run while no other turn of that owner can be in flight.
func (a *App) attachDesktopCodingIngressForTurn(msg *IMUserMessage) string {
	if a == nil || msg == nil || msg.IsBackground {
		return ""
	}
	if strings.TrimSpace(msg.UserID) == "" || !isDesktopCodingOwner(msg.UserID) {
		return ""
	}
	projectPath := projectPathFromSessionOwnerID(msg.UserID)
	if !a.isDesktopCodingWorkbenchSession(msg.UserID, projectPath) {
		return ""
	}
	// StartNewTask is fenced again at the ordered turn boundary. The transport
	// fence stops the old chain immediately, but this request may queue behind
	// earlier turns of the same owner; one of those turns could otherwise
	// recreate a relation in the gap. At this point every earlier turn has
	// completed, this window is held, and no later turn can mint until this
	// one ends. This explicit destruction is not skipped by repo maintenance:
	// the maintenance guard only decides whether the new turn may receive a
	// fresh credential.
	if msg.StartNewTask {
		a.revokeDesktopCodingTaskRelation(msg.UserID)
	}
	if _, active, blocked := a.repoMaintGuardForOwner(msg.UserID); active && blocked {
		return ""
	}
	// The workspace is deliberately not bound here. At turn-start the host
	// has not yet chosen or materialized the step surface (primary checkout,
	// exclusive copy, or isolated worktree); the local runner consumes the
	// relation through an atomic bind of that exact surface. An ordinary
	// in-process turn continues the owner's current relation.
	return a.beginDesktopCodingTaskIngress(msg.UserID)
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
	return a.codingCaps.nextRootStepRelation(a.codingAuthorizationDataDirForApp(), token, ownerID)
}

// nextDesktopCodingRootStepRelationForExecution atomically binds the exact
// materialized local execution surface and consumes one root-step relation.
func (a *App) nextDesktopCodingRootStepRelationForExecution(token, ownerID, workspaceDir string, writableSurface bool) (*codingTaskRelationService, verifiedCodingSubject, verifiedCodingTaskHandle, codingStaticWorkspaceBinding, bool) {
	if a == nil {
		return nil, verifiedCodingSubject{}, verifiedCodingTaskHandle{}, codingStaticWorkspaceBinding{}, false
	}
	return a.codingCaps.nextRootStepRelationForExecution(a.codingAuthorizationDataDirForApp(), token, ownerID, workspaceDir, writableSurface)
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
	a.codingCaps.revokeRelation(a.codingAuthorizationDataDirForApp(), ownerID)
}

func (a *App) fenceDesktopCodingTaskRelation(ownerID string, revokeRelation bool) {
	if a == nil {
		return
	}
	a.codingCaps.fence(a.codingAuthorizationDataDirForApp(), ownerID, revokeRelation)
}
