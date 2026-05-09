package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestOpenKnowledgeStoreWithRetryRetriesLockedErrors(t *testing.T) {
	attempts := 0
	var sleeps []time.Duration
	store, err := openKnowledgeStoreWithRetry(context.Background(), func() (*knowledge.SQLiteStore, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New(`knowledge sqlite pragma "PRAGMA foreign_keys=ON": database is locked (261)`)
		}
		return nil, nil
	}, func(_ context.Context, delay time.Duration) bool {
		sleeps = append(sleeps, delay)
		return true
	})
	if err != nil {
		t.Fatalf("openKnowledgeStoreWithRetry: %v", err)
	}
	if store != nil {
		t.Fatalf("expected nil test store, got %#v", store)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(sleeps) != 2 || sleeps[0] != 50*time.Millisecond || sleeps[1] != 100*time.Millisecond {
		t.Fatalf("retry sleeps = %#v, want 50ms then 100ms", sleeps)
	}
}

func TestOpenKnowledgeStoreWithRetryDoesNotRetryPermanentErrors(t *testing.T) {
	attempts := 0
	_, err := openKnowledgeStoreWithRetry(context.Background(), func() (*knowledge.SQLiteStore, error) {
		attempts++
		return nil, errors.New("knowledge sqlite open: permission denied")
	}, func(context.Context, time.Duration) bool {
		t.Fatal("should not sleep for a non-lock error")
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestOpenKnowledgeStoreWithRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := openKnowledgeStoreWithRetry(ctx, func() (*knowledge.SQLiteStore, error) {
		attempts++
		return nil, errors.New("database is locked")
	}, func(context.Context, time.Duration) bool {
		t.Fatal("should not sleep after context cancellation")
		return false
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
