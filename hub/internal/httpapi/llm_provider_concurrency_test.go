package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestProviderConcurrencyControllerAcquireAndRelease(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	snap := controller.snapshot("provider-a", 1)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 || snap.MaxConcurrency != 1 {
		t.Fatalf("unexpected snapshot after acquire: %+v", snap)
	}
	release()
	snap = controller.snapshot("provider-a", 1)
	if snap.InFlight != 0 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after release: %+v", snap)
	}
}

func TestProviderConcurrencyControllerQueuesUntilReleased(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := controller.acquire(context.Background(), "provider-a", 1)
		if err != nil {
			return
		}
		release2()
		close(acquired)
	}()

	time.Sleep(50 * time.Millisecond)
	snap := controller.snapshot("provider-a", 1)
	if snap.InFlight != 1 || snap.QueueWaiters != 1 {
		t.Fatalf("unexpected snapshot while queued: %+v", snap)
	}
	release()
	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second acquire did not complete after release")
	}
}

func TestProviderConcurrencyControllerAcquireCanceledWhileQueued(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := controller.acquire(ctx, "provider-a", 1); err == nil {
		t.Fatal("expected queued acquire to fail on timeout")
	}
	snap := controller.snapshot("provider-a", 1)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after canceled wait: %+v", snap)
	}
}
