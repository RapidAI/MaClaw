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
	c.trayIconGrace = 0

	var events []string
	c.setTrayQuit(func() { events = append(events, "tray") })
	c.terminateProcess = func(int) { events = append(events, "terminate") }

	c.requestQuit(nil)

	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestRequestQuitForcesExitWhenShutdownStalls(t *testing.T) {
	c := newExitController()
	c.gracefulTimeout = 30 * time.Millisecond
	c.quitRuntime = func(context.Context) {} // simulates a wedged Wails shutdown

	exitCh := make(chan int, 1)
	c.terminateProcess = func(code int) { exitCh <- code }

	c.requestQuit(context.Background())

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not force the process to exit")
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
	c.trayIconGrace = 0

	var events []string
	c.setTrayQuit(func() { events = append(events, "tray") })
	c.terminateProcess = func(int) { events = append(events, "terminate") }

	c.finishDesktopExit()

	if len(events) != 2 || events[0] != "tray" || events[1] != "terminate" {
		t.Fatalf("events = %v, want [tray terminate]", events)
	}
}

func TestFinishDesktopExitSkipsGraceWithoutTray(t *testing.T) {
	c := newExitController()
	c.trayIconGrace = time.Hour // would hang the test if it were applied

	start := time.Now()
	c.terminateProcess = func(int) {}
	c.finishDesktopExit()

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("finishDesktopExit waited %s without a tray loop", elapsed)
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
