package agentservice

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultDynamicEffectReceiptReconcileInterval bounds how long a trusted
// receipt that arrives between observation cycles stays unprojected. It is a
// recovery ceiling, not a dispatch path: a source may always observe earlier.
const DefaultDynamicEffectReceiptReconcileInterval = 30 * time.Second

// DynamicEffectReceiptReconcileFunc is the host-owned settlement boundary the
// worker drives. Production hosts pass a function that re-derives the durable
// routing resources and invokes
// DynamicSemanticRouting.ReconcileDynamicEffectReceiptSource; the worker
// itself never touches a grant, an adapter, or a dispatch closure.
type DynamicEffectReceiptReconcileFunc func(ctx context.Context, source DynamicEffectReceiptSource) error

// DynamicEffectReceiptWorker is the generic reconciliation loop for dynamic
// external/sensitive effects. It owns a set of binding-specific receipt
// sources and, on startup and on every interval, asks each source for its
// trusted observations. Settlement validation (operation-to-plan rebinding,
// selection digest comparison, receipt digest persistence) stays inside the
// reconciler, so a failed or malformed observation leaves the operation
// awaiting_receipt/unknown and is never promoted to success.
//
// The worker holds no model, request, or provider state: registration is
// keyed only by the immutable binding ID, and one failing source never blocks
// the reconciliation of another binding. Context cancellation stops the loop
// without marking any operation.
type DynamicEffectReceiptWorker struct {
	reconcile DynamicEffectReceiptReconcileFunc
	interval  time.Duration
	// Logf optionally receives per-source reconciliation failures. It is
	// diagnostic only; a logged failure has already failed closed.
	Logf func(format string, args ...interface{})
	// ExpireReceiptWaits converges operations whose receipt lease has run out
	// from awaiting_receipt to unknown. It belongs on this loop because it is
	// the same job seen from the other side: the sources say what arrived,
	// and this says what never will.
	//
	// It is also the part that runs when no source is registered at all,
	// which today is every deployment. Without it the loop has nothing to do
	// and an unconfirmed operation waits forever.
	ExpireReceiptWaits func(ctx context.Context) (int, error)

	mu      sync.Mutex
	sources map[string]DynamicEffectReceiptSource
	cancel  context.CancelFunc
	done    chan struct{}
	// sweepMu serializes the startup/interval sweep with an explicit
	// ReconcileNow so one source is never reconciled by two overlapping sweeps.
	sweepMu sync.Mutex
}

// NewDynamicEffectReceiptWorker creates a stopped worker. A non-positive
// interval selects DefaultDynamicEffectReceiptReconcileInterval.
func NewDynamicEffectReceiptWorker(reconcile DynamicEffectReceiptReconcileFunc, interval time.Duration) (*DynamicEffectReceiptWorker, error) {
	if reconcile == nil {
		return nil, fmt.Errorf("dynamic effect receipt reconciler is required")
	}
	if interval <= 0 {
		interval = DefaultDynamicEffectReceiptReconcileInterval
	}
	return &DynamicEffectReceiptWorker{reconcile: reconcile, interval: interval, sources: make(map[string]DynamicEffectReceiptSource)}, nil
}

// RegisterSource attaches one binding-specific receipt source. Registration
// is deduplicated by BindingID: re-registering the same binding (for example
// after a provider reconnect) replaces the previous source instead of running
// two observation loops for one binding.
func (w *DynamicEffectReceiptWorker) RegisterSource(source DynamicEffectReceiptSource) error {
	if w == nil {
		return fmt.Errorf("dynamic effect receipt worker is unavailable")
	}
	if source == nil {
		return fmt.Errorf("dynamic effect receipt source is required")
	}
	bindingID := strings.TrimSpace(source.BindingID())
	if bindingID == "" {
		return fmt.Errorf("dynamic effect receipt source binding is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sources[bindingID] = source
	return nil
}

// UnregisterSource detaches the source for one binding, if present.
func (w *DynamicEffectReceiptWorker) UnregisterSource(bindingID string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.sources, strings.TrimSpace(bindingID))
}

// SourceCount reports how many distinct bindings currently have a source.
func (w *DynamicEffectReceiptWorker) SourceCount() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.sources)
}

// Start launches the reconciliation loop. The first sweep runs immediately so
// receipts observed while the host was down settle without waiting one full
// interval. Start is rejected while the worker is already running.
func (w *DynamicEffectReceiptWorker) Start(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("dynamic effect receipt worker is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return fmt.Errorf("dynamic effect receipt worker is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(runCtx, w.done)
	return nil
}

// Stop cancels the loop and waits for any in-flight sweep to finish. It is
// idempotent and never settles an operation by itself.
func (w *DynamicEffectReceiptWorker) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// ReconcileNow runs one reconciliation sweep synchronously. Hosts call it
// after registering a source when the next tick would be too late; tests use
// it to keep settlement deterministic.
func (w *DynamicEffectReceiptWorker) ReconcileNow(ctx context.Context) {
	if w == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.sweep(ctx)
}

func (w *DynamicEffectReceiptWorker) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	w.sweep(ctx)
	timer := time.NewTimer(w.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.sweep(ctx)
			timer.Reset(w.interval)
		}
	}
}

func (w *DynamicEffectReceiptWorker) sweep(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	w.sweepMu.Lock()
	defer w.sweepMu.Unlock()
	// Sources are asked first: a receipt that is actually available should
	// settle the operation properly rather than have its wait expire in the
	// same cycle.
	for _, source := range w.snapshotSources() {
		if ctx.Err() != nil {
			return
		}
		if err := w.reconcile(ctx, source); err != nil {
			if ctx.Err() != nil {
				return
			}
			// A failing source fails closed: its operations stay awaiting or
			// unknown, and the remaining bindings are still reconciled.
			if w.Logf != nil {
				w.Logf("dynamic effect receipt source %q reconciliation failed: %v", source.BindingID(), err)
			}
		}
	}
	w.expireWaits(ctx)
}

func (w *DynamicEffectReceiptWorker) expireWaits(ctx context.Context) {
	if w.ExpireReceiptWaits == nil || ctx.Err() != nil {
		return
	}
	changed, err := w.ExpireReceiptWaits(ctx)
	if err != nil {
		if w.Logf != nil && ctx.Err() == nil {
			w.Logf("dynamic effect receipt wait expiry failed: %v", err)
		}
		return
	}
	// Converging an operation to unknown is not routine. It says an effect was
	// dispatched and its outcome will never be established automatically, and
	// somebody now has to go and find out.
	if changed > 0 && w.Logf != nil {
		w.Logf("dynamic effect receipt waits expired to unknown: %d", changed)
	}
}

func (w *DynamicEffectReceiptWorker) snapshotSources() []DynamicEffectReceiptSource {
	w.mu.Lock()
	defer w.mu.Unlock()
	bindingIDs := make([]string, 0, len(w.sources))
	for bindingID := range w.sources {
		bindingIDs = append(bindingIDs, bindingID)
	}
	// A stable order keeps sweeps deterministic when several bindings settle in
	// the same cycle.
	sort.Strings(bindingIDs)
	sources := make([]DynamicEffectReceiptSource, 0, len(bindingIDs))
	for _, bindingID := range bindingIDs {
		sources = append(sources, w.sources[bindingID])
	}
	return sources
}
