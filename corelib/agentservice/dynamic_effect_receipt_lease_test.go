package agentservice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func neverReconciles(context.Context, DynamicEffectReceiptSource) error { return nil }

// Every deployment today registers no receipt source, so this is the loop's
// only job. If it does nothing here, an operation that was dispatched and
// never confirmed waits in awaiting_receipt forever and no exit can reach it.
func TestReceiptWorkerExpiresWaitsWithNoSourceRegistered(t *testing.T) {
	worker, err := NewDynamicEffectReceiptWorker(neverReconciles, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var expiries atomic.Int32
	worker.ExpireReceiptWaits = func(context.Context) (int, error) {
		expiries.Add(1)
		return 1, nil
	}
	if worker.SourceCount() != 0 {
		t.Fatalf("fixture registered %d sources", worker.SourceCount())
	}
	worker.ReconcileNow(context.Background())
	if expiries.Load() != 1 {
		t.Fatalf("expiries=%d on an empty loop", expiries.Load())
	}
}

// Expiry is the answer to "no receipt came", so it must run after the sources
// have had their chance. Reversing the order would convert a wait to unknown
// in the same cycle that a real receipt was available to settle it properly.
func TestReceiptWorkerAsksSourcesBeforeGivingUpOnThem(t *testing.T) {
	var order []string
	worker, err := NewDynamicEffectReceiptWorker(func(context.Context, DynamicEffectReceiptSource) error {
		order = append(order, "source")
		return nil
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	worker.ExpireReceiptWaits = func(context.Context) (int, error) {
		order = append(order, "expire")
		return 0, nil
	}
	if err := worker.RegisterSource(&workerReceiptSourceStub{bindingID: "binding"}); err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
	if len(order) != 2 || order[0] != "source" || order[1] != "expire" {
		t.Fatalf("sweep order=%v", order)
	}
}

// A failing expiry is a recovery problem, not a settlement problem: nothing
// has been decided, so the loop keeps running and tries again next cycle.
func TestReceiptWorkerSurvivesAFailingExpiry(t *testing.T) {
	worker, err := NewDynamicEffectReceiptWorker(neverReconciles, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var logged atomic.Int32
	worker.Logf = func(string, ...interface{}) { logged.Add(1) }
	worker.ExpireReceiptWaits = func(context.Context) (int, error) {
		return 0, errors.New("database is locked")
	}
	worker.ReconcileNow(context.Background())
	worker.ReconcileNow(context.Background())
	if logged.Load() != 2 {
		t.Fatalf("logged=%d", logged.Load())
	}
}

// A receipt wait is legitimate while the host is running, so unlike the other
// stale-state scans this one cannot be a startup-only pass: a server that
// stays up for weeks would never run it.
func TestReceiptWorkerKeepsExpiringOnItsInterval(t *testing.T) {
	worker, err := NewDynamicEffectReceiptWorker(neverReconciles, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var expiries atomic.Int32
	worker.ExpireReceiptWaits = func(context.Context) (int, error) {
		expiries.Add(1)
		return 0, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for expiries.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if expiries.Load() < 3 {
		t.Fatalf("expiries=%d; the loop is not sweeping on its interval", expiries.Load())
	}
}

// A worker without the hook is the state every host was in before this slice.
// It must stay harmless rather than panic.
func TestReceiptWorkerWithoutAnExpiryHookIsInert(t *testing.T) {
	worker, err := NewDynamicEffectReceiptWorker(neverReconciles, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	worker.ReconcileNow(context.Background())
}

// Cancellation stops the loop without converging anything: a shutdown is not
// evidence that a receipt will never arrive.
func TestReceiptWorkerDoesNotExpireOnACancelledSweep(t *testing.T) {
	worker, err := NewDynamicEffectReceiptWorker(neverReconciles, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var expiries atomic.Int32
	worker.ExpireReceiptWaits = func(context.Context) (int, error) {
		expiries.Add(1)
		return 0, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.ReconcileNow(ctx)
	if expiries.Load() != 0 {
		t.Fatalf("expiries=%d on a cancelled sweep", expiries.Load())
	}
}
