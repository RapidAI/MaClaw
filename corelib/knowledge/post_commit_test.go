package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestFinalizeCommittedSource_CancelledHydrationStillReturnsSuccess(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	persisted, err := store.SaveText(context.Background(), TextSaveRequest{
		Title: "Committed before cancellation",
		Text:  "Post-commit hydration must never convert durable data into a retryable failure.",
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	returned := store.finalizeCommittedSource(ctx, persisted, false)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context was not cancelled: %v", ctx.Err())
	}
	if returned.ID != persisted.ID || returned.SaveStatus != SaveStatusCreated {
		t.Fatalf("committed source was not returned as successful: %#v", returned)
	}
	stored, err := store.GetSource(context.Background(), persisted.ID)
	if err != nil {
		t.Fatalf("GetSource(%q): %v", persisted.ID, err)
	}
	if stored.ID != returned.ID || stored.Title != returned.Title {
		t.Fatalf("persisted source mismatch: returned=%#v stored=%#v", returned, stored)
	}
}
