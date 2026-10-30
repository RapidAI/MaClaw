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
	// shutdown cannot leave the process running after the user asked to quit.
	// Conversation state is flushed at the start of shutdown; the remaining
	// steps (serial port, cloud lease, gateways) are best-effort inside this
	// window. The tray icon stays up until this deadline: it is removed in
	// the same step that terminates the process.
	defaultGracefulQuitTimeout = 1 * time.Second

	// defaultExitFlushBudget is how long a stuck session flush may block
	// quit. The graceful deadline starts only after the flush returns, so
	// without this cap a flush that never returns leaves the process up.
	defaultExitFlushBudget = 2 * time.Second
)

// exitController owns process termination. Exactly one graceful-quit request
// and exactly one termination run per process; both are guarded so concurrent
// quit sources (tray menu, pet menu, updater) cannot race.
type exitController struct {
	quitOnce sync.Once
	exitOnce sync.Once

	gracefulTimeout time.Duration
	flushBudget     time.Duration

	trayMu   sync.Mutex
	trayQuit func()

	prepareMu   sync.Mutex
	prepareExit func()

	// Seams so the quit path is testable without ending the test process.
	quitRuntime      func(context.Context)
	terminateProcess func(int)
}

func newExitController() *exitController {
	return &exitController{
		gracefulTimeout:  defaultGracefulQuitTimeout,
		flushBudget:      defaultExitFlushBudget,
		quitRuntime:      runtime.Quit,
		terminateProcess: forceTerminateProcess,
	}
}

// appExit is the process-wide controller used by all quit entry points.
var appExit = newExitController()

// setPrepareExit registers the session flush that must finish before the
// quit deadline starts. Called once from startup.
func (c *exitController) setPrepareExit(fn func()) {
	c.prepareMu.Lock()
	c.prepareExit = fn
	c.prepareMu.Unlock()
}

func (c *exitController) runPrepareExit() {
	c.prepareMu.Lock()
	fn := c.prepareExit
	c.prepareMu.Unlock()
	if fn != nil {
		fn()
	}
}

// setTrayQuit registers the platform tray loop terminator. Called once when
// the tray starts; platforms without a Go-owned tray loop leave it unset.
func (c *exitController) setTrayQuit(fn func()) {
	c.trayMu.Lock()
	c.trayQuit = fn
	c.trayMu.Unlock()
}

// requestTrayQuit removes the notification icon. The platform function is
// synchronous (Shell_NotifyIcon returns after the shell accepts NIM_DELETE).
// There is no follow-up wait: the process is terminated by the caller.
func (c *exitController) requestTrayQuit() {
	c.trayMu.Lock()
	fn := c.trayQuit
	c.trayMu.Unlock()
	if fn != nil {
		fn()
	}
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
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("[exit-lifecycle] session flush panicked: %v", recovered)
					c.exitNow()
				}
			}()
			// Flush first. The graceful deadline must not start until this
			// returns, or a busy UI thread is killed before the resume slot
			// is on disk. The flush timer is the cap when the flush itself
			// never returns.
			flushTimer := time.AfterFunc(c.flushBudget, func() {
				log.Printf("[exit-lifecycle] session flush exceeded %s; forcing exit", c.flushBudget)
				c.exitNow()
			})
			c.runPrepareExit()
			flushTimer.Stop()
			time.AfterFunc(c.gracefulTimeout, func() {
				log.Printf("[exit-lifecycle] graceful shutdown exceeded %s; forcing exit", c.gracefulTimeout)
				// Icon removal and process termination are one step.
				c.exitNow()
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

// exitNow removes the notification icon and terminates the process.
// Both happen here, back to back, on a normal goroutine. The icon is not
// taken down earlier, and the tray window procedure does not own termination.
func (c *exitController) exitNow() {
	c.requestTrayQuit()
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
