package guiapp

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// defaultGracefulQuitTimeout bounds how long the Wails shutdown sequence
	// may run. Every quit entry point (tray menu, desktop pet menu, macOS
	// status item, updater restart) goes through requestQuit, so a wedged
	// shutdown or a third-party DLL hook injected into the process cannot
	// leave it running after the user asked to quit.
	defaultGracefulQuitTimeout = 15 * time.Second

	// defaultTrayIconGrace gives the tray message loop time to remove the
	// notification icon before the process terminates.
	defaultTrayIconGrace = 500 * time.Millisecond
)

// exitController owns process termination. Exactly one graceful-quit request
// and exactly one termination run per process; both are guarded so concurrent
// quit sources (tray menu, pet menu, updater) cannot race.
type exitController struct {
	quitOnce sync.Once
	exitOnce sync.Once

	gracefulTimeout time.Duration
	trayIconGrace   time.Duration

	trayMu   sync.Mutex
	trayQuit func()

	// Seams so the quit path is testable without ending the test process.
	quitRuntime      func(context.Context)
	terminateProcess func(int)
}

func newExitController() *exitController {
	return &exitController{
		gracefulTimeout:  defaultGracefulQuitTimeout,
		trayIconGrace:    defaultTrayIconGrace,
		quitRuntime:      runtime.Quit,
		terminateProcess: forceTerminateProcess,
	}
}

// appExit is the process-wide controller used by all quit entry points.
var appExit = newExitController()

// setTrayQuit registers the platform tray loop terminator. Called once when
// the tray starts; platforms without a Go-owned tray loop leave it unset.
func (c *exitController) setTrayQuit(fn func()) {
	c.trayMu.Lock()
	c.trayQuit = fn
	c.trayMu.Unlock()
}

// requestTrayQuit asks the tray loop to stop and reports whether one was
// registered, i.e. whether shutdown must wait for the notification icon.
func (c *exitController) requestTrayQuit() bool {
	c.trayMu.Lock()
	fn := c.trayQuit
	c.trayMu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

// requestQuit starts the graceful shutdown sequence and arms the hard-exit
// watchdog. Safe to call repeatedly and from a UI callback goroutine.
func (c *exitController) requestQuit(ctx context.Context) {
	if ctx == nil {
		log.Printf("[exit-lifecycle] quit requested before startup completed; forcing exit")
		c.exitNow()
		return
	}
	c.quitOnce.Do(func() {
		go func() {
			time.AfterFunc(c.gracefulTimeout, func() {
				log.Printf("[exit-lifecycle] graceful shutdown exceeded %s; forcing exit", c.gracefulTimeout)
				// Drop the notification icon first; the tray onExit callback
				// may win the race and terminate the process itself.
				c.requestTrayQuit()
				c.hardExit(0)
			})
			log.Printf("[exit-lifecycle] quit requested; starting Wails shutdown")
			c.quitRuntime(ctx)
		}()
	})
}

// finishDesktopExit runs after wails.Run returns, i.e. after the Wails
// shutdown callback completed. Removing the tray icon is all that is left,
// and the process must end here: returning from main relies on ExitProcess,
// which can block forever in DLL detach (graphics/audio hooks injected into
// the process) and previously left a window-less process behind.
func (c *exitController) finishDesktopExit() {
	log.Printf("[exit-lifecycle] desktop shutdown complete; terminating process")
	c.exitNow()
}

// exitNow removes the notification icon (when a tray loop owns one) and
// terminates the process.
func (c *exitController) exitNow() {
	if c.requestTrayQuit() && c.trayIconGrace > 0 {
		time.Sleep(c.trayIconGrace)
	}
	c.hardExit(0)
}

// hardExit terminates the process exactly once. In production
// terminateProcess never returns, so concurrent callers park inside sync.Once
// instead of falling back into the Go runtime exit path, which would race the
// loader's process-detach sequence.
func (c *exitController) hardExit(code int) {
	c.exitOnce.Do(func() {
		log.Printf("[exit-lifecycle] terminating process code=%d", code)
		c.terminateProcess(code)
	})
}

// Package-level entry points used by the rest of the app.

func requestAppQuit(ctx context.Context) { appExit.requestQuit(ctx) }

func finishDesktopExit() { appExit.finishDesktopExit() }

func hardExitProcess(code int) { appExit.hardExit(code) }
