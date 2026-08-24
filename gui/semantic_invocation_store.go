package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const semanticInvocationKeySize = 32

// semanticInvocationIssuer returns an issuer backed by the App-owned durable
// grant store. The signing key is deliberately host-local and never part of
// model context, provider arguments, plan text, or the SQLite state itself.
func (h *IMMessageHandler) semanticInvocationIssuer() (*tool.InvocationIssuer, error) {
	if h == nil || h.app == nil {
		// Standalone/test hosts explicitly have no durable lifecycle owner. They
		// retain the memory store rather than accidentally writing credentials to
		// a global user directory.
		return tool.NewRandomInvocationIssuer()
	}
	return h.app.semanticInvocationIssuer()
}

func (h *IMMessageHandler) semanticPlanExecutor(issuer *tool.InvocationIssuer) (*tool.PlanExecutor, error) {
	if h == nil || issuer == nil {
		return nil, fmt.Errorf("semantic invocation issuer is unavailable")
	}
	routes, err := h.semanticRouteStateStore()
	if err != nil {
		return nil, err
	}
	return h.semanticPlanExecutorWithRouteState(issuer, routes)
}

func (h *IMMessageHandler) semanticPlanExecutorWithRouteState(issuer *tool.InvocationIssuer, routes tool.RouteStateStore) (*tool.PlanExecutor, error) {
	if h == nil || issuer == nil || routes == nil {
		return nil, fmt.Errorf("semantic invocation issuer or route state is unavailable")
	}
	if h.app == nil {
		return tool.NewPlanExecutorWithRouteState(issuer, tool.NewMemoryPlanExecutionStore(), routes)
	}
	return h.app.semanticPlanExecutorWithRouteState(issuer, routes)
}

// semanticHostCallJournal supplies the durable host-protocol boundary for a
// semantic surface. Standalone/test handlers intentionally use a memory
// journal; production App hosts share a SQLite journal across reconnects.
func (h *IMMessageHandler) semanticHostCallJournal() (tool.HostCallJournal, error) {
	if h == nil || h.app == nil {
		return tool.NewMemoryHostCallJournal(), nil
	}
	return h.app.semanticHostCallJournalForApp()
}

// semanticExecutionCoordinator is optional only for standalone unit hosts.
// App-backed GUI turns always receive the shared transactional owner.
func (h *IMMessageHandler) semanticExecutionCoordinator() (*tool.SQLiteSemanticExecutionCoordinator, error) {
	if h == nil || h.app == nil {
		return nil, nil
	}
	return h.app.semanticExecutionCoordinatorForApp()
}

// semanticContinuityTenantID is host configuration, not request/model input.
// A desktop App owns one local data root; a connected installation uses the
// enrolled tenant. Standalone test hosts leave it empty so the coordinator's
// explicit single-tenant compatibility default remains the only fallback.
func (h *IMMessageHandler) semanticContinuityTenantID() string {
	if h == nil || h.app == nil {
		return ""
	}
	config, err := h.app.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(config.RemoteTenantID)
}

func (h *IMMessageHandler) semanticRouteStateStore() (tool.RouteStateStore, error) {
	if h == nil || h.app == nil {
		return tool.NewMemoryRouteStateStore(), nil
	}
	return h.app.semanticRouteStateStoreForApp()
}

func (h *IMMessageHandler) semanticArtifactStore() (tool.ArtifactStore, error) {
	if h == nil || h.app == nil {
		return tool.NewMemoryArtifactStore(), nil
	}
	return h.app.semanticArtifactStoreForApp()
}

// semanticDynamicCapabilityContracts returns the GUI-owned, durable
// control-plane registry for reviewed dynamic Skill/MCP contracts. Request
// execution receives this only through its resolver methods; no Agent path is
// given publication authority.
func (h *IMMessageHandler) semanticDynamicCapabilityContracts() (agentservice.DynamicCapabilityContractRegistry, error) {
	if h == nil || h.app == nil {
		// Standalone handlers intentionally have no implicit global control plane.
		// An absent registry is reported as incomplete by the dynamic inventory
		// publisher rather than treating discovered providers as approved.
		return nil, nil
	}
	return h.app.semanticDynamicCapabilityContractsForApp()
}

// semanticDynamicEffectCoordinator returns the execution-plane coordinator
// for receipt-bound dynamic effects. Request execution receives only the
// coordinator interface; neither the Agent nor a dynamic provider gets a
// ledger, receipt store, or settlement authority.
func (h *IMMessageHandler) semanticDynamicEffectCoordinator() (agentservice.DynamicExternalEffectCoordinator, error) {
	if h == nil || h.app == nil {
		return nil, fmt.Errorf("dynamic effect coordinator is unavailable")
	}
	return h.app.semanticDynamicEffectCoordinatorForApp()
}

func (a *App) semanticInvocationIssuer() (*tool.InvocationIssuer, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if len(a.semanticInvocationKey) == 0 {
		key, err := loadOrCreateSemanticInvocationKey(filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "invocation-signing-key"))
		if err != nil {
			return nil, err
		}
		a.semanticInvocationKey = key
	}
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return tool.NewInvocationIssuerWithStore(a.semanticInvocationKey, a.semanticInvocationStore)
}

// ensureSemanticExecutionCoordinatorLocked gives all semantic runtime
// projections one SQLite transaction domain. It is deliberately invoked under
// semanticInvocationMu; the component stores are aliases into the coordinator
// and must never be independently opened or closed by the GUI host.
func (a *App) ensureSemanticExecutionCoordinatorLocked() error {
	if a == nil {
		return fmt.Errorf("semantic invocation host is unavailable")
	}
	if a.semanticExecutionCoordinator != nil {
		return nil
	}
	if len(a.semanticInvocationKey) == 0 {
		key, err := loadOrCreateSemanticInvocationKey(filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "invocation-signing-key"))
		if err != nil {
			return err
		}
		a.semanticInvocationKey = key
	}
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "semantic-execution.db"),
		tool.WithCoordinatorArtifactEncryptionKey(a.semanticInvocationKey),
		tool.WithCoordinatorArtifactQuotaBytes(tool.DefaultArtifactQuotaBytes),
		tool.WithCoordinatorArtifactRetention(tool.DefaultArtifactRetention),
	)
	if err != nil {
		return err
	}
	if _, err := coordinator.ReconcileStaleExternalEffects(time.Now().UTC(), tool.PlanExecutionRunningLease); err != nil {
		_ = coordinator.Close()
		return fmt.Errorf("reconcile semantic external effects: %w", err)
	}
	if _, err := coordinator.Executions.ReconcileStaleRunning(time.Now().UTC(), tool.PlanExecutionRunningLease); err != nil {
		_ = coordinator.Close()
		return fmt.Errorf("reconcile semantic plan executions: %w", err)
	}
	if _, err := coordinator.HostCalls.ReconcileStale(time.Now().UTC(), tool.HostCallRunningLease); err != nil {
		_ = coordinator.Close()
		return fmt.Errorf("reconcile semantic host calls: %w", err)
	}
	if _, err := coordinator.ReconcileStaleDeliveryDispatches(time.Now().UTC(), tool.DeliveryDispatchLease); err != nil {
		_ = coordinator.Close()
		return fmt.Errorf("reconcile semantic delivery dispatches: %w", err)
	}
	if _, err := coordinator.Artifacts.SweepExpiredArtifacts(time.Now().UTC()); err != nil {
		_ = coordinator.Close()
		return fmt.Errorf("sweep expired semantic artifacts: %w", err)
	}
	a.semanticExecutionCoordinator = coordinator
	a.semanticInvocationStore = coordinator.Grants
	a.semanticPlanExecutionStore = coordinator.Executions
	a.semanticRouteStateStore = coordinator.Routes
	a.semanticHostCallJournal = coordinator.HostCalls
	a.semanticArtifactStore = coordinator.Artifacts
	return nil
}

func (a *App) semanticExecutionCoordinatorForApp() (*tool.SQLiteSemanticExecutionCoordinator, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return a.semanticExecutionCoordinator, nil
}

func (a *App) semanticPlanExecutor(issuer *tool.InvocationIssuer) (*tool.PlanExecutor, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	routes, err := a.semanticRouteStateStoreForApp()
	if err != nil {
		return nil, err
	}
	return a.semanticPlanExecutorWithRouteState(issuer, routes)
}

func (a *App) semanticPlanExecutorWithRouteState(issuer *tool.InvocationIssuer, routes tool.RouteStateStore) (*tool.PlanExecutor, error) {
	if a == nil || issuer == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return tool.NewPlanExecutorWithRouteState(issuer, a.semanticPlanExecutionStore, routes)
}

func (a *App) semanticHostCallJournalForApp() (tool.HostCallJournal, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return a.semanticHostCallJournal, nil
}

func (a *App) semanticRouteStateStoreForApp() (tool.RouteStateStore, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return a.semanticRouteStateStore, nil
}

func (a *App) semanticArtifactStoreForApp() (tool.ArtifactStore, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return nil, err
	}
	return a.semanticArtifactStore, nil
}

func (a *App) semanticDynamicCapabilityContractsForApp() (agentservice.DynamicCapabilityContractRegistry, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if a.semanticDynamicContracts == nil {
		registry, err := agentservice.NewSQLiteDynamicCapabilityRegistry(filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "dynamic-capability-contracts.db"))
		if err != nil {
			return nil, err
		}
		a.semanticDynamicContracts = registry
	}
	return a.semanticDynamicContracts, nil
}

// semanticDynamicEffectCoordinatorForApp creates the durable GUI boundary for
// dynamic external/sensitive effects. A stale running operation is always
// recovered as unknown, never redispatched. Only a trusted host integration
// holding the concrete coordinator may later settle it with receipt evidence.
func (a *App) semanticDynamicEffectCoordinatorForApp() (agentservice.LedgerDynamicExternalEffectCoordinator, error) {
	if a == nil {
		return agentservice.LedgerDynamicExternalEffectCoordinator{}, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if err := a.ensureSemanticExecutionCoordinatorLocked(); err != nil {
		return agentservice.LedgerDynamicExternalEffectCoordinator{}, err
	}
	return agentservice.LedgerDynamicExternalEffectCoordinator{
		SemanticCoordinator: a.semanticExecutionCoordinator,
	}, nil
}

// reconcileSemanticDynamicEffectReceiptSource is the GUI host's trusted
// recovery entry point for one binding-specific provider/channel receipt
// integration. It does not take model-visible tool names, grants, selection
// IDs, scopes, or a dispatch closure: the core reconciler derives all of that
// from the durable operation admission record before allowing a receipt to
// settle PlanExecution and RouteState.
//
// Sources are owned and invoked by the GUI lifecycle/channel integration.
// They may observe receipts, but they must never dispatch/retry a provider;
// an unavailable or untrusted source therefore leaves the effect awaiting or
// unknown rather than creating a new outward operation.
// expireSemanticDynamicEffectReceiptWaits converges operations whose receipt
// lease has run out. Startup already reconciles the other stale states; this
// one has to run on a timer instead, because a receipt wait is legitimate
// during normal operation and a desktop host may stay up for weeks.
func (a *App) expireSemanticDynamicEffectReceiptWaits(context.Context) (int, error) {
	if a == nil {
		return 0, fmt.Errorf("semantic invocation host is unavailable")
	}
	coordinator, err := a.semanticExecutionCoordinatorForApp()
	if err != nil {
		return 0, err
	}
	return coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC(), tool.ExternalEffectReceiptLease)
}

// semanticDynamicRoutingForApp assembles the durable routing view used by the
// host-side effect paths. Both the receipt reconciler and the manual exit go
// through here so they cannot drift into disagreeing about which stores back
// an operation.
func (a *App) semanticDynamicRoutingForApp() (agentservice.DynamicSemanticRouting, error) {
	if a == nil {
		return agentservice.DynamicSemanticRouting{}, fmt.Errorf("semantic invocation host is unavailable")
	}
	coordinator, err := a.semanticDynamicEffectCoordinatorForApp()
	if err != nil {
		return agentservice.DynamicSemanticRouting{}, err
	}
	issuer, err := a.semanticInvocationIssuer()
	if err != nil {
		return agentservice.DynamicSemanticRouting{}, err
	}
	routes, err := a.semanticRouteStateStoreForApp()
	if err != nil {
		return agentservice.DynamicSemanticRouting{}, err
	}
	if _, err := a.semanticPlanExecutorWithRouteState(issuer, routes); err != nil {
		return agentservice.DynamicSemanticRouting{}, err
	}
	a.semanticInvocationMu.Lock()
	executionStore := a.semanticPlanExecutionStore
	a.semanticInvocationMu.Unlock()
	if executionStore == nil {
		return agentservice.DynamicSemanticRouting{}, fmt.Errorf("semantic plan execution store is unavailable")
	}
	return agentservice.DynamicSemanticRouting{
		ExecutionStore: executionStore, RouteState: routes, EffectCoordinator: coordinator,
	}, nil
}

func (a *App) reconcileSemanticDynamicEffectReceiptSource(ctx context.Context, source agentservice.DynamicEffectReceiptSource) error {
	routing, err := a.semanticDynamicRoutingForApp()
	if err != nil {
		return err
	}
	return routing.ReconcileDynamicEffectReceiptSource(ctx, source)
}

// startSemanticEffectReceiptWorker launches the generic receipt
// reconciliation loop for dynamic external/sensitive effects. The GUI
// currently registers no binding-specific sources, so the loop runs empty;
// durable resources stay lazily initialized until a trusted source is
// registered by a future channel/provider integration.
func (a *App) startSemanticEffectReceiptWorker(ctx context.Context) {
	if a == nil {
		return
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if a.semanticEffectReceiptWorker != nil {
		return
	}
	worker, err := agentservice.NewDynamicEffectReceiptWorker(a.reconcileSemanticDynamicEffectReceiptSource, 0)
	if err != nil {
		log.Printf("[startup] semantic effect receipt worker create failed: %v", err)
		return
	}
	worker.Logf = func(format string, args ...interface{}) {
		log.Printf("[semantic-effect-receipts] "+format, args...)
	}
	// No binding-specific source is registered here either, so this is the
	// only work the loop does: it stops an operation nobody will ever confirm
	// from sitting in awaiting_receipt, which nothing can leave.
	worker.ExpireReceiptWaits = a.expireSemanticDynamicEffectReceiptWaits
	if err := worker.Start(ctx); err != nil {
		log.Printf("[startup] semantic effect receipt worker start failed: %v", err)
		return
	}
	a.semanticEffectReceiptWorker = worker
}

// stopSemanticEffectReceiptWorker halts the reconciliation loop before the
// durable semantic stores are closed. Stopping never settles an operation: a
// still-awaiting effect remains awaiting_receipt for the next launch.
func (a *App) stopSemanticEffectReceiptWorker() {
	if a == nil {
		return
	}
	a.semanticInvocationMu.Lock()
	worker := a.semanticEffectReceiptWorker
	a.semanticEffectReceiptWorker = nil
	a.semanticInvocationMu.Unlock()
	if worker != nil {
		worker.Stop()
	}
}

func (a *App) closeSemanticInvocationStore() {
	if a == nil {
		return
	}
	a.semanticInvocationMu.Lock()
	coordinator := a.semanticExecutionCoordinator
	dynamicContracts := a.semanticDynamicContracts
	a.semanticExecutionCoordinator = nil
	a.semanticInvocationStore = nil
	a.semanticPlanExecutionStore = nil
	a.semanticRouteStateStore = nil
	a.semanticHostCallJournal = nil
	a.semanticArtifactStore = nil
	a.semanticDynamicContracts = nil
	a.semanticInvocationKey = nil
	a.semanticInvocationMu.Unlock()
	if coordinator != nil {
		if err := coordinator.Close(); err != nil {
			log.Printf("[shutdown] semantic execution coordinator close failed: %v", err)
		}
	}
	if closable, ok := dynamicContracts.(interface{ Close() error }); ok {
		if err := closable.Close(); err != nil {
			log.Printf("[shutdown] semantic dynamic capability contracts close failed: %v", err)
		}
	}
}

func loadOrCreateSemanticInvocationKey(keyPath string) ([]byte, error) {
	keyPath = strings.TrimSpace(keyPath)
	if keyPath == "" {
		return nil, fmt.Errorf("semantic invocation key path is required")
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create semantic invocation key directory: %w", err)
	}
	if key, err := readSemanticInvocationKey(keyPath); err == nil {
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	generated := make([]byte, semanticInvocationKeySize)
	if _, err := io.ReadFull(rand.Reader, generated); err != nil {
		return nil, fmt.Errorf("generate semantic invocation signing key: %w", err)
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := file.Write(generated); writeErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("write semantic invocation signing key: %w", writeErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return nil, fmt.Errorf("close semantic invocation signing key: %w", closeErr)
		}
		return generated, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("create semantic invocation signing key: %w", err)
	}
	// Another local host instance won initialization. Read its complete key;
	// do not generate a competing key that would invalidate its active grants.
	return readSemanticInvocationKey(keyPath)
}

func readSemanticInvocationKey(keyPath string) ([]byte, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	if len(key) != semanticInvocationKeySize {
		return nil, fmt.Errorf("semantic invocation signing key has invalid length")
	}
	return append([]byte(nil), key...), nil
}
