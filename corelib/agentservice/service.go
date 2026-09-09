package agentservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type Config struct {
	DataRoot         string
	TokenSecret      string
	TokenTTL         time.Duration
	CredentialPepper string
	// StoreBackend selects the durable control-plane repository when store is
	// nil. "file" (or empty) preserves the historical JSON backend; "sqlite"
	// gives message/run/outbox lifecycle operations one ACID database boundary.
	StoreBackend string
	// RuntimeModules is the single composition-root registry for transport-
	// neutral Agent modules shared by GUI, srv, and future hosts.
	RuntimeModules []agentruntime.Module
	// RuntimeMetricsTenantLimit bounds tenant-hash time series exposed by the
	// shared Runtime collector. Zero uses the collector's conservative default.
	RuntimeMetricsTenantLimit int
	// RuntimeRateLimitRate is the per-tenant Agent turn admission rate in
	// tokens/second. A non-positive value disables burst limiting (the default,
	// preserving historical behavior for embedders).
	RuntimeRateLimitRate float64
	// RuntimeRateLimitBurst is the maximum number of immediately admitted
	// turns per tenant. Zero derives a one-turn burst when rate limiting is
	// enabled.
	RuntimeRateLimitBurst int
	// RuntimeRateLimitTenantLimit bounds in-memory limiter buckets. Tenants
	// beyond the bound are accounted for but fail open; zero uses a default.
	RuntimeRateLimitTenantLimit int
	// RequireAtomicLifecycle rejects caller-supplied stores that cannot commit
	// run state and its durable outbox through RunLifecycleTransactionStore.
	// Headless production hosts should enable this; it remains opt-in for
	// embedders while legacy custom stores migrate. When false, a store that
	// lacks the contract still starts, but NewService logs that lifecycle
	// persistence is best_effort.
	RequireAtomicLifecycle bool
	// DistributedRateLimiter optionally supplies cross-process tenant admission
	// enforcement. The local token bucket remains the offline fallback; when
	// this port is configured it is authoritative for the request.
	DistributedRateLimiter DistributedRateLimiter
}

// DistributedRateLimiter is the narrow host port for deployments that need
// tenant fairness across multiple service instances. Implementations must be
// atomic for a single tenant key and return a retry duration when denied.
type DistributedRateLimiter interface {
	Allow(context.Context, string) (allowed bool, retryAfter time.Duration, err error)
}

type Service struct {
	store               Store
	records             RecordStore
	dynamicOperations   DynamicOperationLedger
	dynamicCapabilities DynamicCapabilityContractRegistry
	runEvents           RunEventStore
	dynamicSemanticMu   sync.Mutex
	dynamicSemantic     *DynamicSemanticRoutingResources
	executor            Executor
	runtime             agentruntime.Runtime
	runtimeModules      *agentruntime.ModuleRegistry
	runtimeHost         agentruntime.HostCapabilities
	metrics             *agentruntime.RuntimeMetrics
	rateLimiter         *agentruntime.TenantTokenBucket
	distributedLimiter  DistributedRateLimiter
	metricsMu           sync.Mutex
	tokens              *TokenManager
	dataRoot            string
	credentialPepper    string
	now                 func() time.Time

	// SkillSourceFilter returns the allowed skill sources for a given principal.
	// nil means all sources are allowed. Set by MaClawSrv at initialization
	// to integrate with the skill source control system.
	SkillSourceFilter func(tenantID, userID string) []string

	// AssistantMessageMetadataHook lets the hosting server attach runtime
	// capability metadata after an assistant response is produced.
	AssistantMessageMetadataHook func(ctx context.Context, p Principal, inst Instance, sess Session, run Run, msg Message, cfg corelib.AppConfig) map[string]string

	runMu            sync.Mutex
	runningRuns      map[string]context.CancelFunc
	activeRequests   sync.WaitGroup
	closed           bool
	idempotencyMu    sync.Mutex
	idempotencyLocks map[string]*idempotencyLock
	closeOnce        sync.Once
	closeErr         error
	// skillInstallTxnMu serializes local Skill directory transactions and
	// protects the service-owned cross-restart compensation journal.
	skillInstallTxnMu sync.Mutex
	// skillInstallRecoveryBlocked is fail-closed state set when AgentService
	// startup cannot prove that its durable directory compensation queue has
	// been recovered. Reads remain available, but imports/updates are refused
	// until a later admission check clears the condition.
	skillInstallRecoveryBlocked bool
	// skillDirectoryRename is the filesystem boundary used by Skill imports.
	// Production uses the retrying compensation primitive; tests may inject a
	// deterministic failure at a specific batch step to verify all-or-nothing
	// rollback semantics.
	skillDirectoryRename func(oldPath, newPath string) error
	// skillPostCommitCleanup is a test seam for validating the distinct
	// committed-but-cleanup-pending outcome. Production leaves it nil and uses
	// the normal idempotent backup cleanup in the importer.
	skillPostCommitCleanup func() error
}

func (s *Service) beginRequest() error {
	if s == nil {
		return ErrServiceClosed
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.closed {
		return ErrServiceClosed
	}
	s.activeRequests.Add(1)
	return nil
}

// idempotencyLock is a small keyed mutex used to make client session/message
// keys atomic without serializing unrelated principals or sessions.
type idempotencyLock struct {
	mu   sync.Mutex
	refs int
}

func (s *Service) lockIdempotency(key string) func() {
	key = strings.TrimSpace(key)
	if s == nil || key == "" {
		return func() {}
	}
	s.idempotencyMu.Lock()
	if s.idempotencyLocks == nil {
		s.idempotencyLocks = make(map[string]*idempotencyLock)
	}
	lock := s.idempotencyLocks[key]
	if lock == nil {
		lock = &idempotencyLock{}
		s.idempotencyLocks[key] = lock
	}
	lock.refs++
	s.idempotencyMu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.idempotencyMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.idempotencyLocks, key)
		}
		s.idempotencyMu.Unlock()
	}
}

func idempotencyKey(parts ...string) string {
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, "\x00")
}

// correlationMetadata copies the transport-neutral correlation id into
// durable run metadata. The id is already validated by agentruntime, so it is
// safe to expose to GUI/srv/TUI clients and to use when joining audit/events.
func correlationMetadata(ctx context.Context) map[string]string {
	metadata := make(map[string]string, 4)
	if id := agentruntime.CorrelationID(ctx); id != "" {
		metadata["request_id"] = id
	}
	if id := agentruntime.TraceID(ctx); id != "" {
		metadata["trace_id"] = id
	}
	if id := agentruntime.SpanID(ctx); id != "" {
		metadata["span_id"] = id
	}
	if id := agentruntime.RunSpanID(ctx); id != "" {
		metadata["run_span_id"] = id
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func mergeCorrelationMetadata(ctx context.Context, metadata map[string]string) map[string]string {
	id := agentruntime.CorrelationID(ctx)
	traceID := agentruntime.TraceID(ctx)
	spanID := agentruntime.SpanID(ctx)
	runSpanID := agentruntime.RunSpanID(ctx)
	if id == "" && traceID == "" && spanID == "" && runSpanID == "" {
		return metadata
	}
	out := make(map[string]string, len(metadata)+4)
	for key, value := range metadata {
		out[key] = value
	}
	if _, exists := out["request_id"]; !exists {
		if id != "" {
			out["request_id"] = id
		}
	}
	if _, exists := out["trace_id"]; !exists && traceID != "" {
		out["trace_id"] = traceID
	}
	if _, exists := out["span_id"]; !exists && spanID != "" {
		out["span_id"] = spanID
	}
	if _, exists := out["run_span_id"]; !exists && runSpanID != "" {
		out["run_span_id"] = runSpanID
	}
	return out
}

func NewService(cfg Config, store Store, executor Executor) (*Service, error) {
	if strings.TrimSpace(cfg.DataRoot) == "" {
		return nil, fmt.Errorf("data root is required")
	}
	useFileStores := store == nil
	ownedStore := false
	if store == nil {
		switch strings.ToLower(strings.TrimSpace(cfg.StoreBackend)) {
		case "", "file", "json":
			fileStore, err := NewFileStore(filepath.Join(cfg.DataRoot, "state", "store.json"))
			if err != nil {
				return nil, fmt.Errorf("create file store: %w", err)
			}
			store = fileStore
			ownedStore = true
		case "sqlite":
			sqliteStore, err := NewSQLiteStore(filepath.Join(cfg.DataRoot, "state", "service.db"))
			if err != nil {
				return nil, fmt.Errorf("create sqlite store: %w", err)
			}
			store = sqliteStore
			ownedStore = true
		default:
			return nil, fmt.Errorf("unsupported store backend %q (want file or sqlite)", cfg.StoreBackend)
		}
	}
	if _, ok := store.(RunLifecycleTransactionStore); !ok {
		if cfg.RequireAtomicLifecycle {
			if ownedStore {
				if closer, closeOK := store.(interface{ Close() error }); closeOK {
					_ = closer.Close()
				}
			}
			return nil, fmt.Errorf("atomic lifecycle store is required; %T does not implement RunLifecycleTransactionStore", store)
		}
		log.Printf("[agentservice] store %T does not implement RunLifecycleTransactionStore; run lifecycle persistence is best_effort", store)
	}
	closeOwnedStore := func() {
		if !ownedStore {
			return
		}
		if closer, ok := store.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	if executor == nil {
		executor = EchoExecutor{}
	}
	runtimeModules, err := agentruntime.RegisterBuiltinModules(cfg.RuntimeModules...)
	if err != nil {
		closeOwnedStore()
		return nil, fmt.Errorf("create runtime module registry: %w", err)
	}
	if err := secureMkdirAll(cfg.DataRoot); err != nil {
		closeOwnedStore()
		return nil, fmt.Errorf("create data root: %w", err)
	}
	var records RecordStore
	var dynamicOperations DynamicOperationLedger
	var dynamicCapabilities DynamicCapabilityContractRegistry
	var runEvents RunEventStore
	runEventsOwned := false
	// Every resource below is created by this constructor and must be released
	// if a later dependency fails. The supplied Store remains caller-owned;
	// only an internally-created backend is closed here. When a Store also owns
	// the event outbox (FileStore/SQLiteStore), close it once via runEvents and
	// skip the duplicate store close.
	cleanupInitialized := func() {
		if closer, ok := records.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if closer, ok := dynamicOperations.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if closer, ok := dynamicCapabilities.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if runEventsOwned {
			if closer, ok := runEvents.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		}
		if ownedStore {
			if !runEventsOwned {
				if closer, ok := store.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}
		}
	}
	if useFileStores {
		sqliteRecords, err := NewSQLiteRecordStore(filepath.Join(cfg.DataRoot, "records", "records.db"))
		if err != nil {
			closeOwnedStore()
			return nil, fmt.Errorf("create record store: %w", err)
		}
		records = sqliteRecords
		sqliteOperations, err := NewSQLiteDynamicOperationLedger(filepath.Join(cfg.DataRoot, "operations", "dynamic_operations.db"))
		if err != nil {
			cleanupInitialized()
			return nil, fmt.Errorf("create dynamic operation ledger: %w", err)
		}
		dynamicOperations = sqliteOperations
		sqliteCapabilities, err := NewSQLiteDynamicCapabilityRegistry(filepath.Join(cfg.DataRoot, "capabilities", "dynamic_contracts.db"))
		if err != nil {
			cleanupInitialized()
			return nil, fmt.Errorf("create dynamic capability registry: %w", err)
		}
		dynamicCapabilities = sqliteCapabilities
		// FileStore now persists the run outbox in the same atomic document as
		// messages and runs. Keep a SQLite fallback for custom file-backed
		// stores supplied by embedders that do not implement RunEventStore.
		if eventStore, ok := store.(RunEventStore); ok {
			runEvents = eventStore
			runEventsOwned = true
		} else {
			sqliteEvents, eventErr := NewSQLiteRunEventStore(filepath.Join(cfg.DataRoot, "events", "run_events.db"))
			if eventErr != nil {
				cleanupInitialized()
				return nil, fmt.Errorf("create run event store: %w", eventErr)
			}
			runEvents = sqliteEvents
			runEventsOwned = true
		}
	} else {
		records = NewMemoryRecordStore()
		dynamicOperations = NewMemoryDynamicOperationLedger()
		dynamicCapabilities = NewDynamicCapabilityRegistry()
		// A Store that also owns RunEventStore is the preferred composition:
		// message/run lifecycle and its outbox then share one repository lock
		// (and, for FileStore, one atomic file replacement). Custom stores that
		// have not migrated retain the standalone in-memory event store.
		if eventStore, ok := store.(RunEventStore); ok {
			runEvents = eventStore
			runEventsOwned = ownedStore
		} else {
			runEvents = NewMemoryRunEventStore()
			runEventsOwned = true
		}
	}
	// A previous process may have crashed after dispatching a dynamic provider
	// but before it persisted a terminal receipt. Never automatically replay
	// such work: make the stale lease explicitly unknown for reconciliation.
	if _, err := dynamicOperations.ReconcileStaleRunning(time.Now().UTC(), DynamicOperationRunningLease); err != nil {
		cleanupInitialized()
		return nil, fmt.Errorf("reconcile dynamic operation ledger: %w", err)
	}
	service := &Service{store: store, records: records, dynamicOperations: dynamicOperations, dynamicCapabilities: dynamicCapabilities, runEvents: runEvents, executor: executor, runtime: NewRuntimeExecutorWithModules(executor, runtimeModules), runtimeModules: runtimeModules, runtimeHost: agentruntime.HeadlessHostCapabilities{}, metrics: agentruntime.NewRuntimeMetrics(cfg.RuntimeMetricsTenantLimit), rateLimiter: agentruntime.NewTenantTokenBucket(cfg.RuntimeRateLimitRate, cfg.RuntimeRateLimitBurst, cfg.RuntimeRateLimitTenantLimit), distributedLimiter: cfg.DistributedRateLimiter, tokens: NewTokenManager(cfg.TokenSecret, cfg.TokenTTL), dataRoot: cfg.DataRoot, credentialPepper: cfg.CredentialPepper, now: time.Now}
	// Recover AgentService-owned directory transactions during startup, not
	// only immediately before a later import.  A pending/unreadable record is
	// retained as a fail-closed admission condition while read-only APIs remain
	// usable for diagnosis.
	if _, pending, recoverErr := service.recoverAgentSkillInstallCompensations(); recoverErr != nil || pending > 0 {
		service.skillInstallRecoveryBlocked = true
	}
	if setter, ok := executor.(interface{ SetDynamicOperationLedger(DynamicOperationLedger) }); ok {
		setter.SetDynamicOperationLedger(dynamicOperations)
	}
	return service, nil
}

// Close releases process-held resources (e.g. SQLite record store). Safe to call
// multiple times; hosts should call this when tearing down a Service instance.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		var closeErrs []error
		collectCloseErr := func(err error) {
			if err != nil {
				closeErrs = append(closeErrs, err)
			}
		}
		// Cancel live requests before closing durable stores so no executor
		// callback can race a store close during host shutdown.
		s.runMu.Lock()
		s.closed = true
		cancels := make([]context.CancelFunc, 0, len(s.runningRuns))
		for runID, cancel := range s.runningRuns {
			cancels = append(cancels, cancel)
			delete(s.runningRuns, runID)
		}
		s.runMu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		s.activeRequests.Wait()

		// The executor owns process-scoped runtime resources (memory stores,
		// schedulers, SSH sessions). Keep this optional for lightweight test
		// executors and backwards-compatible embedders.
		if s.runtime != nil {
			collectCloseErr(s.runtime.Close())
		} else if c, ok := s.executor.(interface{ Close() error }); ok {
			collectCloseErr(c.Close())
		}
		for _, resource := range []interface{}{
			s.records, s.dynamicOperations, s.dynamicCapabilities, s.runEvents, s.distributedLimiter,
		} {
			if c, ok := resource.(interface{ Close() error }); ok {
				collectCloseErr(c.Close())
			}
		}
		// Custom control-plane stores are not required to implement
		// RunEventStore, so they are not necessarily present in the resource
		// list above. Close them here to avoid leaking database handles in
		// embedders. When the store also owns RunEventStore, runEvents points to
		// the same object and remains the single closer for that resource.
		if _, ownsEvents := s.store.(RunEventStore); !ownsEvents {
			if c, ok := s.store.(interface{ Close() error }); ok {
				collectCloseErr(c.Close())
			}
		}
		s.dynamicSemanticMu.Lock()
		semantic := s.dynamicSemantic
		s.dynamicSemantic = nil
		s.dynamicSemanticMu.Unlock()
		if semantic != nil {
			collectCloseErr(semantic.Close())
		}
		switch len(closeErrs) {
		case 0:
			s.closeErr = nil
		case 1:
			// Preserve the historical identity of a single close error for
			// callers that compare a sentinel directly; aggregate only when
			// multiple resources fail.
			s.closeErr = closeErrs[0]
		default:
			s.closeErr = errors.Join(closeErrs...)
		}
	})
	return s.closeErr
}

func (s *Service) DataRoot() string { return s.dataRoot }

// RuntimeMetrics exposes the shared, transport-neutral runtime counters. The
// returned collector is safe for concurrent updates and snapshots; callers
// must not mutate its internal state. A non-nil collector is always returned
// for a live Service, while a nil receiver yields nil for compatibility.
func (s *Service) RuntimeMetrics() *agentruntime.RuntimeMetrics {
	if s == nil {
		return nil
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if s.metrics == nil {
		// Keep custom/legacy Service literals safe. NewService initializes this
		// eagerly, but embedded tests may construct Service directly.
		s.metrics = agentruntime.NewRuntimeMetrics(0)
	}
	return s.metrics
}

// RuntimeRateLimiter exposes the shared per-tenant admission limiter for
// host diagnostics and tests. NewService always initializes it; a nil
// receiver or legacy Service literal gets a disabled limiter lazily.
func (s *Service) RuntimeRateLimiter() *agentruntime.TenantTokenBucket {
	if s == nil {
		return nil
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if s.rateLimiter == nil {
		s.rateLimiter = agentruntime.NewTenantTokenBucket(0, 0, 0)
	}
	return s.rateLimiter
}

// Runtime exposes the transport-neutral Agent contract. Hosts should use this
// facade for new integrations; existing service APIs remain source-compatible
// while their executor implementation is migrated behind RuntimeExecutor.
func (s *Service) Runtime() agentruntime.Runtime {
	if s == nil {
		return nil
	}
	if s.runtime != nil {
		return s.runtime
	}
	return NewRuntimeExecutorWithModules(s.executor, s.runtimeModules)
}

// SetRuntimeHostCapabilities injects the GUI or headless host surface used by
// new Runtime integrations. It is request-independent metadata; authorization
// remains the responsibility of the concrete host adapter.
func (s *Service) SetRuntimeHostCapabilities(host agentruntime.HostCapabilities) {
	if s == nil {
		return
	}
	s.runMu.Lock()
	s.runtimeHost = host
	s.runMu.Unlock()
}

func (s *Service) runtimeHostCapabilities() agentruntime.HostCapabilities {
	if s == nil {
		return nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.runtimeHost
}

// UserDataRoot returns the isolated persistent data directory for one
// principal. Host-side integrations use it for per-user delivery cursors and
// must not place that state in the service-wide root.
func (s *Service) UserDataRoot(p Principal) string {
	if s == nil {
		return ""
	}
	return s.userDataRoot(p.TenantID, p.UserID)
}

func (s *Service) userRoot(tenantID, userID string) string {
	return filepath.Join(s.dataRoot, "tenants", slugID(tenantID), "users", slugID(userID))
}
func (s *Service) userDataRoot(tenantID, userID string) string {
	return filepath.Join(s.userRoot(tenantID, userID), "data")
}
func (s *Service) userConfigPath(tenantID, userID string) string {
	return filepath.Join(s.userRoot(tenantID, userID), "config", "app_config.json")
}

var (
	auditExportSecretKeyPattern    = auditExportSecretKeyRegexpPattern()
	safePathPart                   = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	auditExportInlineSecretPattern = regexp.MustCompile(`(?i)(` + auditExportSecretKeyPattern + `)(\s*[:=]\s*)([^\s,;]+)`)
	auditExportBearerPattern       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	auditExportJSONSecretPattern   = regexp.MustCompile(`(?i)("?(?:` + auditExportSecretKeyPattern + `)"?\s*:\s*)"[^"]*"`)
)

func slugID(v string) string { return safePathPart.ReplaceAllString(v, "_") }
func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func laterTime(current *time.Time, candidate time.Time) *time.Time {
	if candidate.IsZero() {
		return current
	}
	if current == nil || candidate.After(*current) {
		next := candidate
		return &next
	}
	return current
}
