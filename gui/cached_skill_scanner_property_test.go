package main

import (
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"pgregory.net/rapid"
)

// Feature: gui-startup-response-optimization, Property 9: Graceful degradation during scan
// For any caller requesting the skill list while a background scan is in progress
// and no prior cache exists, the scanner SHALL return an empty skill list without blocking.
// **Validates: Requirements 2.3, 2.10**
func TestProperty9_GracefulDegradationDuringScan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random skill configurations that the scanner would eventually return.
		numSkills := rapid.IntRange(0, 20).Draw(t, "numSkills")
		skills := make([]corelib.NLSkillEntry, numSkills)
		for i := range skills {
			skills[i] = corelib.NLSkillEntry{
				Name:        rapid.StringMatching(`[a-z][a-z0-9\-]{2,20}`).Draw(t, "skillName"),
				Description: rapid.StringMatching(`[a-zA-Z ]{5,50}`).Draw(t, "skillDesc"),
				Status:      "active",
			}
		}

		// Create a CachedSkillScanner with a controlled scan function.
		// We simulate a scan that is in progress by setting the scanning flag.
		scanner := &CachedSkillScanner{}

		// Override the roots to a non-existent path — we'll control the scan via
		// a custom approach. Instead, we directly test the behavior by:
		// 1. Starting Init (which triggers background scan)
		// 2. Immediately calling Get() before scan completes
		// 3. Verifying Get() returns nil (no cache available)

		// To properly test this, we need to ensure the scan takes time.
		// We'll use a temp directory that exists but is empty, and inject
		// a blocking mechanism by replacing the scan behavior.

		// Approach: Use the scanner's atomic state directly.
		// Init() sets scanning=true and spawns a goroutine that calls scan().
		// We can test by creating a scanner where roots point to a directory
		// that will take time to scan (simulated via a wrapper).

		// Simpler approach: directly test the contract.
		// 1. Set scanning flag to true (simulating scan in progress)
		// 2. Ensure cache is nil (no prior cache)
		// 3. Call Get() — should return nil without blocking

		// Set up: scanner with no cache and scanning flag set
		scanner.scanning.Store(true)
		// cache is zero-value (nil atomic pointer) — no prior cache

		// Act: Get() should return nil immediately (graceful degradation)
		done := make(chan []corelib.NLSkillEntry, 1)
		go func() {
			done <- scanner.Get()
		}()

		select {
		case result := <-done:
			// Property: Get() returns nil when no cache exists and scan is in progress
			if result != nil {
				t.Fatalf("expected nil (graceful degradation) during scan with no cache, got %d skills", len(result))
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Get() blocked — should return immediately during scan with no cache")
		}

		// Now simulate scan completion: store results in cache
		scanner.scanning.Store(false)
		entry := &skillCacheEntry{
			skills:    skills,
			createdAt: time.Now(),
			stale:     false,
		}
		scanner.cache.Store(entry)

		// After scan completes, Get() should return actual results
		afterResult := scanner.Get()
		if afterResult == nil {
			t.Fatal("expected non-nil result after scan completes")
		}
		if len(afterResult) != numSkills {
			t.Fatalf("expected %d skills after scan, got %d", numSkills, len(afterResult))
		}
	})
}

// TestProperty9_GracefulDegradation_ConcurrentCallers verifies that multiple
// concurrent callers all receive nil (empty list) when scan is in progress
// and no prior cache exists, without any caller blocking.
func TestProperty9_GracefulDegradation_ConcurrentCallers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numCallers := rapid.IntRange(2, 10).Draw(t, "numCallers")

		scanner := &CachedSkillScanner{}
		scanner.scanning.Store(true)
		// No cache — zero-value atomic pointer

		var wg sync.WaitGroup
		results := make([][]corelib.NLSkillEntry, numCallers)

		wg.Add(numCallers)
		for i := 0; i < numCallers; i++ {
			go func(idx int) {
				defer wg.Done()
				results[idx] = scanner.Get()
			}(i)
		}

		// All callers should complete within a short time (non-blocking)
		doneCh := make(chan struct{})
		go func() {
			wg.Wait()
			close(doneCh)
		}()

		select {
		case <-doneCh:
			// All callers completed — verify all got nil
			for i, r := range results {
				if r != nil {
					t.Fatalf("caller %d got non-nil result during scan with no cache: %d skills", i, len(r))
				}
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("concurrent callers blocked — Get() should return immediately")
		}
	})
}
