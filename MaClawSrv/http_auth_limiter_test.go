package main

import (
	"fmt"
	"testing"
	"time"
)

func TestAuthLimiterPruneRemovesExpiredStateWithoutClearingActiveBlock(t *testing.T) {
	limiter := newAuthLimiter(2, time.Minute)
	limiter.baseBlockDuration = 5 * time.Minute
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !limiter.Allow("expired-bucket", now) {
		t.Fatal("initial request was rejected")
	}
	limiter.RegisterFailure("expired-failure", now)
	for i := 0; i < limiter.failureThreshold; i++ {
		limiter.RegisterFailure("blocked", now)
	}
	later := now.Add(2 * time.Minute)
	if !limiter.Allow("fresh-bucket", later) {
		t.Fatal("fresh request was rejected")
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.pruneLocked(later)
	if _, ok := limiter.buckets["expired-bucket"]; ok {
		t.Fatal("expired bucket was retained")
	}
	if _, ok := limiter.failures["expired-failure"]; ok {
		t.Fatal("expired failure was retained")
	}
	if _, ok := limiter.buckets["fresh-bucket"]; !ok {
		t.Fatal("fresh bucket was removed")
	}
	if state, ok := limiter.failures["blocked"]; !ok || !state.BlockedUntil.After(later) {
		t.Fatalf("active block was removed: %#v, %v", state, ok)
	}
}

func TestAuthLimiterCapacityRejectsOnlyNewKeysAndPruneFreesCapacity(t *testing.T) {
	limiter := newAuthLimiter(2, time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < authLimiterMaxKeys; i++ {
		if !limiter.Allow(fmt.Sprintf("key-%d", i), now) {
			t.Fatalf("initial key %d was rejected", i)
		}
	}
	if allowed, retry := limiter.AllowWithRetry("new-key", now); allowed || retry != time.Minute {
		t.Fatalf("new key at capacity = %v, %v; want rejected with window retry", allowed, retry)
	}
	if !limiter.Allow("key-0", now) {
		t.Fatal("existing key was evicted or rejected at capacity")
	}
	if block := limiter.RegisterFailure("another-new-key", now); block != limiter.maxBlockDuration {
		t.Fatalf("new failure at capacity block = %v; want %v", block, limiter.maxBlockDuration)
	}
	if _, ok := limiter.failures["another-new-key"]; ok {
		t.Fatal("over-capacity failure was retained")
	}

	limiter.mu.Lock()
	limiter.pruneLocked(now.Add(2 * time.Minute))
	limiter.mu.Unlock()
	if !limiter.Allow("new-key", now.Add(2*time.Minute)) {
		t.Fatal("new key was rejected after expired entries were pruned")
	}
}
