package guiapp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestQuitWithoutContextTerminates(t *testing.T) {
	c := newExitController()

	var codes []int
	c.terminateProcess = func(code int) { codes = append(codes, code) }

	c.requestQuit(nil)

	if len(codes) != 1 || codes[0] != 0 {
		t.Fatalf("terminate calls = %v, want exactly [0]", codes)
	}
}

func TestRequestQuitWithoutContextRemovesTrayIconFirst(t *testing.T) {
	c := newExitController()

	var events []string
	c.setTrayQuit(func() { events = append(events, "tray") })
	c.terminateProcess = func(int) { events = append(events, "terminate") }

	c.requestQuit(nil)

	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestRequestQuitKeepsTrayUntilProcessExits(t *testing.T) {
	c := newExitController()
	c.gracefulTimeout = time.Hour

	var mu sync.Mutex
	var events []string
	quitStarted := make(chan struct{})
	release := make(chan struct{})
	c.setTrayQuit(func() {
		mu.Lock()
		events = append(events, "tray")
		mu.Unlock()
	})
	c.quitRuntime = func(context.Context) {
		close(quitStarted)
		<-release
	}
	c.terminateProcess = func(int) {
		mu.Lock()
		events = append(events, "terminate")
		mu.Unlock()
	}

	c.requestQuit(context.Background())
	select {
	case <-quitStarted:
	case <-time.After(time.Second):
		t.Fatal("quitRuntime was not called")
	}
	mu.Lock()
	if len(events) != 0 {
		got := append([]string(nil), events...)
		mu.Unlock()
		t.Fatalf("events while shutting down = %v, tray must stay up", got)
	}
	mu.Unlock()

	// Shutdown finished: icon removal and termination are one step.
	c.exitNow()
	close(release)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestRequestQuitDoesNotKillDuringPrepare(t *testing.T) {
	c := newExitController()
	c.gracefulTimeout = 30 * time.Millisecond

	started := make(chan struct{})
	release := make(chan struct{})
	var killed atomic.Bool
	c.setPrepareExit(func() {
		close(started)
		<-release
	})
	c.quitRuntime = func(context.Context) {}
	c.terminateProcess = func(int) { killed.Store(true) }

	c.requestQuit(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prepare was not called")
	}
	time.Sleep(80 * time.Millisecond)
	if killed.Load() {
		t.Fatal("watchdog fired while session state was still flushing")
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for !killed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !killed.Load() {
		t.Fatal("watchdog did not fire after the flush returned")
	}
}

func TestRequestQuitBackstopKillsStuckFlush(t *testing.T) {
	c := newExitController()
	c.gracefulTimeout = time.Hour
	c.flushBudget = 40 * time.Millisecond

	c.setPrepareExit(func() { <-make(chan struct{}) })
	c.quitRuntime = func(context.Context) {
		t.Error("Wails shutdown started while the session flush was stuck")
	}
	exitCh := make(chan int, 1)
	c.terminateProcess = func(code int) { exitCh <- code }

	c.requestQuit(context.Background())

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(time.Second):
		t.Fatal("stuck session flush was not forced to exit")
	}
}

func TestRequestQuitForcesExitWhenShutdownStalls(t *testing.T) {
	c := newExitController()
	c.gracefulTimeout = 30 * time.Millisecond
	c.quitRuntime = func(context.Context) {} // simulates a wedged Wails shutdown

	var mu sync.Mutex
	var events []string
	exitCh := make(chan int, 1)
	c.setTrayQuit(func() {
		mu.Lock()
		events = append(events, "tray")
		mu.Unlock()
	})
	c.terminateProcess = func(code int) {
		mu.Lock()
		events = append(events, "terminate")
		mu.Unlock()
		exitCh <- code
	}

	c.requestQuit(context.Background())

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not force the process to exit")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestRequestQuitIsIdempotent(t *testing.T) {
	c := newExitController()

	quitCh := make(chan struct{}, 8)
	c.quitRuntime = func(context.Context) { quitCh <- struct{}{} }
	c.gracefulTimeout = time.Hour // keep the watchdog out of this test

	for i := 0; i < 5; i++ {
		c.requestQuit(context.Background())
	}
	// The quit call runs on its own goroutine.
	select {
	case <-quitCh:
	case <-time.After(time.Second):
		t.Fatal("quitRuntime was never called")
	}
	time.Sleep(20 * time.Millisecond) // window for a duplicate call to land
	select {
	case <-quitCh:
		t.Fatal("quitRuntime called more than once")
	default:
	}
}

func TestFinishDesktopExitRemovesTrayIconBeforeTerminating(t *testing.T) {
	c := newExitController()

	var events []string
	c.setTrayQuit(func() { events = append(events, "tray") })
	c.terminateProcess = func(int) { events = append(events, "terminate") }

	c.finishDesktopExit()

	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestFinishDesktopExitTerminatesWithoutTray(t *testing.T) {
	c := newExitController()

	var codes []int
	c.terminateProcess = func(code int) { codes = append(codes, code) }
	start := time.Now()
	c.finishDesktopExit()

	if len(codes) != 1 || codes[0] != 0 {
		t.Fatalf("terminate calls = %v, want [0]", codes)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("finishDesktopExit took %s without a tray", elapsed)
	}
}

func TestHardExitTerminatesExactlyOnce(t *testing.T) {
	c := newExitController()

	var calls atomic.Int32
	c.terminateProcess = func(int) { calls.Add(1) }

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.hardExit(0)
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("terminate calls = %d, want 1", got)
	}
}
