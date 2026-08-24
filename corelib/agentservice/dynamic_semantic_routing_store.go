package agentservice

import (
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const dynamicSemanticRoutingKeySize = 32

// Default durable-artifact governance for the Service-owned semantic stores.
// The quota is per principal and applies to decoded payload bytes; the
// retention bounds how long payloads stay on disk before the sweeper removes
// them (audit facts live in the never-swept delivery records).
const (
	defaultArtifactQuotaBytes = coretool.DefaultArtifactQuotaBytes
	defaultArtifactRetention  = coretool.DefaultArtifactRetention
)

// DynamicSemanticRoutingResources owns the durable state required by a
// restartable Core Agent semantic route: grant admission, execution facts,
// immutable materialization state, host-call replay and the local signing key.
// All files are rooted below the Service data root; callers never pass paths
// from a model, a provider, or a request.
type DynamicSemanticRoutingResources struct {
	mu             sync.Mutex
	key            []byte
	coordinator    *coretool.SQLiteSemanticExecutionCoordinator
	grantStore     *coretool.SQLiteInvocationGrantStore
	executionStore *coretool.SQLitePlanExecutionStore
	routeState     *coretool.SQLiteRouteStateStore
	hostCalls      *coretool.SQLiteHostCallJournal
	// effectCoordinator is the coordinator last bound through Routing. A
	// trusted receipt-reconciliation worker must settle through exactly the
	// same durable owner that prepared the operation.
	effectCoordinator DynamicExternalEffectCoordinator
	// sessionGoverned is process-local continuation state. It is not a
	// durable external-effect ledger; Routing reuses the same store so a
	// reconfigure does not drop an in-flight granted task.
	sessionGoverned *SessionGovernedTaskStore
}

func OpenDynamicSemanticRoutingResources(dataRoot string) (*DynamicSemanticRoutingResources, error) {
	root := strings.TrimSpace(dataRoot)
	if root == "" {
		return nil, fmt.Errorf("dynamic semantic routing data root is required")
	}
	dir := filepath.Join(root, "semantic-routing")
	if err := secureMkdirAll(dir); err != nil {
		return nil, fmt.Errorf("create dynamic semantic routing directory: %w", err)
	}
	key, err := loadOrCreateDynamicSemanticRoutingKey(filepath.Join(dir, "invocation-signing-key"))
	if err != nil {
		return nil, err
	}
	// Artifact payloads are encrypted at rest with a key derived (HKDF,
	// domain-separated) from the host-local signing key, and bounded by a
	// per-principal quota and retention period.
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"),
		coretool.WithCoordinatorContinuityTenant(""),
		coretool.WithCoordinatorArtifactEncryptionKey(key),
		coretool.WithCoordinatorArtifactQuotaBytes(defaultArtifactQuotaBytes),
		coretool.WithCoordinatorArtifactRetention(defaultArtifactRetention),
	)
	if err != nil {
		return nil, err
	}
	grants, executions, routeState, hostCalls := coordinator.Grants, coordinator.Executions, coordinator.Routes, coordinator.HostCalls
	if _, err := coordinator.ReconcileStaleExternalEffects(time.Now().UTC(), coretool.PlanExecutionRunningLease); err != nil {
		_ = coordinator.Close()
		return nil, fmt.Errorf("reconcile dynamic semantic external effects: %w", err)
	}
	if _, err := executions.ReconcileStaleRunning(time.Now().UTC(), coretool.PlanExecutionRunningLease); err != nil {
		_ = coordinator.Close()
		return nil, fmt.Errorf("reconcile dynamic semantic plan executions: %w", err)
	}
	if _, err := hostCalls.ReconcileStale(time.Now().UTC(), coretool.HostCallRunningLease); err != nil {
		_ = coordinator.Close()
		return nil, fmt.Errorf("reconcile dynamic semantic host calls: %w", err)
	}
	if _, err := coordinator.ReconcileStaleDeliveryDispatches(time.Now().UTC(), coretool.DeliveryDispatchLease); err != nil {
		_ = coordinator.Close()
		return nil, fmt.Errorf("reconcile dynamic semantic delivery dispatches: %w", err)
	}
	// Startup sweep of expired artifact payloads; in-flight deliveries keep
	// their referenced payloads (see SweepExpiredArtifacts).
	if _, err := coordinator.Artifacts.SweepExpiredArtifacts(time.Now().UTC()); err != nil {
		_ = coordinator.Close()
		return nil, fmt.Errorf("sweep expired dynamic semantic artifacts: %w", err)
	}
	return &DynamicSemanticRoutingResources{key: key, coordinator: coordinator, grantStore: grants, executionStore: executions, routeState: routeState, hostCalls: hostCalls, sessionGoverned: NewSessionGovernedTaskStore()}, nil
}

// Routing binds this Service-owned durable state to the governed semantic
// control plane. Registry/Resolver/PolicyAdapter remain explicit so opening a
// database never invents a capability family or a request semantic mapping.
func (r *DynamicSemanticRoutingResources) Routing(registry *coretool.CapabilityRegistry, resolver DynamicCapabilityNeedResolver, policy DynamicCapabilityPolicyAdapter, ttl time.Duration, coordinators ...DynamicExternalEffectCoordinator) (DynamicSemanticRouting, error) {
	if r == nil || r.grantStore == nil || r.executionStore == nil || r.routeState == nil || r.hostCalls == nil {
		return DynamicSemanticRouting{}, fmt.Errorf("dynamic semantic routing resources are unavailable")
	}
	issuer, err := coretool.NewInvocationIssuerWithStore(r.key, r.grantStore)
	if err != nil {
		return DynamicSemanticRouting{}, err
	}
	if r.sessionGoverned == nil {
		r.sessionGoverned = NewSessionGovernedTaskStore()
	}
	// The tenant comes from the authenticated Principal at read time. An empty
	// binding selects that trusted value, never a shared process-wide fallback.
	r.sessionGoverned.BindCoordinator(r.coordinator, "")
	routing := DynamicSemanticRouting{
		Registry: registry, Resolver: resolver, Issuer: issuer, ExecutionStore: r.executionStore,
		RouteState: r.routeState, HostCalls: r.hostCalls, Coordinator: r.coordinator, GrantTTL: ttl, PolicyAdapter: policy,
		SessionGoverned: r.sessionGoverned,
	}
	bindSessionGovernedStore(&routing)
	if len(coordinators) > 1 {
		return DynamicSemanticRouting{}, fmt.Errorf("dynamic semantic routing accepts at most one effect coordinator")
	}
	if len(coordinators) == 1 {
		routing.EffectCoordinator = coordinators[0]
	}
	if err := routing.validate(); err != nil {
		return DynamicSemanticRouting{}, err
	}
	r.mu.Lock()
	r.effectCoordinator = routing.EffectCoordinator
	r.mu.Unlock()
	return routing, nil
}

// EffectCoordinator returns the effect coordinator bound through Routing, or
// nil when no receipt-bound capability family has been configured.
func (r *DynamicSemanticRoutingResources) EffectCoordinator() DynamicExternalEffectCoordinator {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.effectCoordinator
}

func (r *DynamicSemanticRoutingResources) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	coordinator := r.coordinator
	r.coordinator, r.grantStore, r.executionStore, r.routeState, r.hostCalls, r.key = nil, nil, nil, nil, nil, nil
	r.mu.Unlock()
	var firstErr error
	for _, store := range []interface{ Close() error }{coordinator} {
		if store != nil {
			if err := store.Close(); err != nil {
				// Release every durable handle even when an earlier close reports a
				// failure; leaving a later SQLite handle open can otherwise block a
				// controlled service restart. The first failure remains the caller's
				// diagnostic cause.
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func loadOrCreateDynamicSemanticRoutingKey(path string) ([]byte, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("create dynamic semantic signing-key directory: %w", err)
	}
	if key, err := readDynamicSemanticRoutingKey(path); err == nil {
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, dynamicSemanticRoutingKeySize)
	if _, err := io.ReadFull(cryptorand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate dynamic semantic signing key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, err := file.Write(key); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("write dynamic semantic signing key: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close dynamic semantic signing key: %w", err)
		}
		return key, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("create dynamic semantic signing key: %w", err)
	}
	return readDynamicSemanticRoutingKey(path)
}

func readDynamicSemanticRoutingKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != dynamicSemanticRoutingKeySize {
		return nil, fmt.Errorf("dynamic semantic signing key has invalid length")
	}
	return append([]byte(nil), key...), nil
}
