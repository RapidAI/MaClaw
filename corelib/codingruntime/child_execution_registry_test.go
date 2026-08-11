package codingruntime

import (
	"context"
	"testing"
	"time"
)

func TestChildExecutionRegistryCancelsOnlyRequestedParent(t *testing.T) {
	var registry ChildExecutionRegistry
	first, releaseFirst := registry.Begin("parent-one", "child-one")
	defer releaseFirst()
	second, releaseSecond := registry.Begin("parent-two", "child-two")
	defer releaseSecond()

	registry.CancelParent("parent-one")
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("first child context was not cancelled")
	}
	if err := second.Err(); err != nil {
		t.Fatalf("unrelated child was cancelled: %v", err)
	}
}

func TestChildExecutionRegistryReleaseRemovesLiveHandle(t *testing.T) {
	var registry ChildExecutionRegistry
	ctx, release := registry.Begin("parent", "child")
	release()
	registry.CancelParent("parent")
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("released child context should be cancelled locally, got %v", err)
	}

	// A completed child is no longer in the registry, so a later parent
	// cancellation cannot affect a new independent context.
	fresh, releaseFresh := registry.Begin("other-parent", "other-child")
	defer releaseFresh()
	registry.CancelParent("parent")
	if err := fresh.Err(); err != nil {
		t.Fatalf("released parent retained a cancellation handle: %v", err)
	}
}
