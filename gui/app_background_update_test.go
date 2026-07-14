package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppUpdateNotifyVersion(t *testing.T) {
	cases := []struct {
		name        string
		result      UpdateResult
		last        string
		wantOK      bool
		wantVersion string
	}{
		{
			name:   "no update",
			result: UpdateResult{HasUpdate: false, LatestVersion: "V1.2.0"},
			wantOK: false,
		},
		{
			name:        "new version",
			result:      UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			wantOK:      true,
			wantVersion: "V1.2.0",
		},
		{
			name:   "same version already notified",
			result: UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			last:   "V1.2.0",
			wantOK: false,
		},
		{
			name:        "newer version after prior notice",
			result:      UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0"},
			last:        "V1.2.0",
			wantOK:      true,
			wantVersion: "V1.3.0",
		},
		{
			name:   "has update but empty version",
			result: UpdateResult{HasUpdate: true, LatestVersion: "  "},
			wantOK: false,
		},
		{
			name:        "whitespace normalized for notify and dedupe",
			result:      UpdateResult{HasUpdate: true, LatestVersion: " V2.0.0 "},
			wantOK:      true,
			wantVersion: "V2.0.0",
		},
		{
			name:   "whitespace-normalized version dedupes against stored",
			result: UpdateResult{HasUpdate: true, LatestVersion: " V1.2.0 "},
			last:   "V1.2.0",
			wantOK: false,
		},
		{
			name:   "whitespace-normalized last also dedupes",
			result: UpdateResult{HasUpdate: true, LatestVersion: "V1.2.0"},
			last:   " V1.2.0 ",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotOK := appUpdateNotifyVersion(tc.result, tc.last)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotVersion != tc.wantVersion {
				t.Fatalf("version = %q, want %q", gotVersion, tc.wantVersion)
			}
		})
	}
}

func TestShouldFetchAppUpdateNow(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		next time.Time
		want bool
	}{
		{name: "never scheduled", next: time.Time{}, want: true},
		{name: "before next allowed", next: now.Add(10 * time.Minute), want: false},
		{name: "exactly at next allowed", next: now, want: true},
		{name: "after next allowed", next: now.Add(-time.Minute), want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFetchAppUpdateNow(tc.next, now)
			if got != tc.want {
				t.Fatalf("shouldFetchAppUpdateNow() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextAppUpdateFetchAt(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	successInterval := 30 * time.Minute
	failureBackoff := 5 * time.Minute

	got := nextAppUpdateFetchAt(now, true, successInterval, failureBackoff)
	if want := now.Add(successInterval); !got.Equal(want) {
		t.Fatalf("success next = %v, want %v", got, want)
	}

	got = nextAppUpdateFetchAt(now, false, successInterval, failureBackoff)
	if want := now.Add(failureBackoff); !got.Equal(want) {
		t.Fatalf("failure next = %v, want %v", got, want)
	}

	// Non-positive intervals mean "allow immediately again".
	if got := nextAppUpdateFetchAt(now, true, 0, failureBackoff); !got.Equal(now) {
		t.Fatalf("zero success interval next = %v, want %v", got, now)
	}
	if got := nextAppUpdateFetchAt(now, false, successInterval, 0); !got.Equal(now) {
		t.Fatalf("zero failure backoff next = %v, want %v", got, now)
	}
}

func TestRunBackgroundUpdateCheckLoop_RunsFirstThenPeriodicAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n atomic.Int32
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 5*time.Millisecond, 10*time.Millisecond, func() {
			if n.Add(1) >= 3 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background update loop did not stop in time")
	}

	if got := n.Load(); got < 2 {
		t.Fatalf("expected at least first + one periodic tick, got %d", got)
	}
}

func TestRunBackgroundUpdateCheckLoop_TickPanicDoesNotKillLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n atomic.Int32
	done := make(chan struct{})
	go func() {
		// Mirror production: recover inside the tick wrapper so panics do not
		// terminate the loop for the rest of the process lifetime.
		runBackgroundUpdateCheckLoop(ctx, 0, 5*time.Millisecond, func() {
			defer func() { _ = recover() }()
			i := n.Add(1)
			if i == 1 {
				panic("simulated check failure")
			}
			if i >= 3 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not recover from tick panic")
	}
	if got := n.Load(); got < 3 {
		t.Fatalf("expected loop to continue after panic, ticks=%d", got)
	}
}

func TestRunBackgroundUpdateCheckLoop_ZeroInitialDelayStillRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Bool
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 0, 50*time.Millisecond, func() {
			called.Store(true)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit with zero initial delay and cancelled ctx")
	}
	if called.Load() {
		t.Fatal("tick must not run when context is already cancelled")
	}
}

func TestRunBackgroundUpdateCheckLoop_CancelDuringInitialDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Bool
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 200*time.Millisecond, 50*time.Millisecond, func() {
			called.Store(true)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after ctx cancel during initial delay")
	}
	if called.Load() {
		t.Fatal("tick must not run when context is already cancelled")
	}
}

func TestRunBackgroundUpdateCheckLoop_NilContextNoPanic(t *testing.T) {
	runBackgroundUpdateCheckLoop(nil, 0, time.Millisecond, func() {
		t.Fatal("tick should not run with nil context")
	})
}

func TestRunBackgroundUpdateCheckLoop_ConcurrentCancelSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	ticks := 0
	done := make(chan struct{})
	go func() {
		runBackgroundUpdateCheckLoop(ctx, 0, 5*time.Millisecond, func() {
			mu.Lock()
			ticks++
			n := ticks
			mu.Unlock()
			if n == 2 {
				cancel()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("loop did not stop")
	}
}

func TestStartBackgroundUpdateChecks_RequiresContextAndIsIdempotent(t *testing.T) {
	app := &App{}
	app.startBackgroundUpdateChecks()
	app.startBackgroundUpdateChecks()

	ctx, cancel := context.WithCancel(context.Background())
	app2 := &App{ctx: ctx}
	app2.startBackgroundUpdateChecks()
	app2.startBackgroundUpdateChecks()
	cancel()
	// Allow the armed loop to observe cancel during the initial delay and exit.
	time.Sleep(20 * time.Millisecond)
}

func TestBackgroundUpdateBackoff_FailureShorterThanSuccess(t *testing.T) {
	now := time.Now()
	successNext := nextAppUpdateFetchAt(now, true, backgroundUpdateCheckInterval, backgroundUpdateCheckFailureBackoff)
	failureNext := nextAppUpdateFetchAt(now, false, backgroundUpdateCheckInterval, backgroundUpdateCheckFailureBackoff)
	if !failureNext.Before(successNext) {
		t.Fatalf("failure backoff %v should be sooner than success interval %v", failureNext.Sub(now), successNext.Sub(now))
	}
	if failureNext.Sub(now) != backgroundUpdateCheckFailureBackoff {
		t.Fatalf("failure backoff = %v, want %v", failureNext.Sub(now), backgroundUpdateCheckFailureBackoff)
	}
}
