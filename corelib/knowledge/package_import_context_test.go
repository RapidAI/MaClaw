package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestDetachedTimeoutContext_PreservesValuesWithoutParentCancellation(t *testing.T) {
	type contextKey string
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), contextKey("trace"), "trace-123"))
	cancelParent()

	ctx, cancel := detachedTimeoutContext(parent, time.Second)
	defer cancel()
	if ctx.Err() != nil {
		t.Fatalf("detached context inherited parent cancellation: %v", ctx.Err())
	}
	if got := ctx.Value(contextKey("trace")); got != "trace-123" {
		t.Fatalf("detached context lost parent value: got %#v", got)
	}
}

func TestImportPackageSources_PreCancelledContextDoesNotCreateBatch(t *testing.T) {
	store := &mockBatchCreatorStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := ImportPackageSources(ctx, store, []PackageSource{{ID: "pending", Content: "content"}}, PackageImportOptions{})
	if len(store.batches) != 0 {
		t.Fatalf("pre-cancelled import created batch records: %#v", store.batches)
	}
	if result.Imported != 0 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("pre-cancelled import classified work: %#v", result)
	}
	if result.Status != "partial" {
		t.Fatalf("status=%q, want partial", result.Status)
	}
	if len(result.RetrySourceIDs) != 1 || result.RetrySourceIDs[0] != "pending" {
		t.Fatalf("RetrySourceIDs=%#v, want pending", result.RetrySourceIDs)
	}
}
