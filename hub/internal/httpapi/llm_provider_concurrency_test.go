package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestProviderConcurrencyControllerAcquireAndRelease(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	snap := controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 || snap.MaxConcurrency != 1 || snap.MaxQueueWaiters != 2 {
		t.Fatalf("unexpected snapshot after acquire: %+v", snap)
	}
	release()
	snap = controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 0 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after release: %+v", snap)
	}
}

func TestProviderConcurrencyControllerQueuesUntilReleased(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
		if err != nil {
			return
		}
		release2()
		close(acquired)
	}()

	time.Sleep(50 * time.Millisecond)
	snap := controller.snapshot("provider-a", 1, 2, 0)
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
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := controller.acquire(ctx, "provider-a", 1, 2, 0); err == nil {
		t.Fatal("expected queued acquire to fail on timeout")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueCanceled {
		t.Fatalf("unexpected error kind: %v", err)
	}
	snap := controller.snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after canceled wait: %+v", snap)
	}
}

func TestProviderConcurrencyControllerRejectsWhenQueueFull(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 1, 0)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	queued := make(chan struct{})
	go func() {
		_, _ = controller.acquire(context.Background(), "provider-a", 1, 1, 0)
		close(queued)
	}()
	select {
	case <-queued:
		t.Fatal("expected second acquire to remain queued")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := controller.acquire(context.Background(), "provider-a", 1, 1, 0); err == nil {
		t.Fatal("expected queue full error")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueFull {
		t.Fatalf("unexpected error kind: %v", err)
	}
}

func TestProviderConcurrencyControllerQueueTimeout(t *testing.T) {
	controller := newProviderConcurrencyController()
	release, err := controller.acquire(context.Background(), "provider-a", 1, 2, 30)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	if _, err := controller.acquire(context.Background(), "provider-a", 1, 2, 30); err == nil {
		t.Fatal("expected queue timeout error")
	} else if queueErr, ok := err.(*providerConcurrencyError); !ok || queueErr.Kind != providerConcurrencyQueueTimeout {
		t.Fatalf("unexpected error kind: %v", err)
	}
}
