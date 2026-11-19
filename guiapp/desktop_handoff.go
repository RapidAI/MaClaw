package guiapp

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// desktopHandoffPaused is the set of sessions this handoff itself paused.
// A session the person already paused is left alone, and ending the handoff
// resumes only the sessions in this set.
var (
	desktopHandoffPauseMu sync.Mutex
	desktopHandoffPaused  map[*computeruse.Session]struct{}
)

// installDesktopHandoff makes a pay click or a password field wait until the
// person finishes on the real screen. Tests leave it off so they return at once.
func (a *App) installDesktopHandoff() {
	if a == nil || testing.Testing() {
		return
	}
	computeruse.SetDesktopHandoffNotifier(noteDesktopHandoff)
	logComputerUseControl("pay and secret steps wait for the person on screen")
}

// ComputerUseHandoffFinish ends the waiting pay or secret step.
// done true means the person finished it. False means they cancelled it.
// The click or keystrokes are not sent either way.
func (a *App) ComputerUseHandoffFinish(done bool) error {
	err := computeruse.ResolveDesktopHandoff(done)
	if errors.Is(err, computeruse.ErrDesktopHandoffNone) {
		return nil
	}
	return err
}

func noteDesktopHandoff(n computeruse.DesktopHandoffNotice) {
	if !n.Active {
		if n.Resume {
			resumeDesktopHandoffPause()
			emitComputerUseEvent(EventComputerUseControl, map[string]interface{}{
				"at":     time.Now().Format(time.RFC3339),
				"action": "resume",
				"paused": false,
			})
			return
		}
		clearDesktopHandoffPause()
		return
	}
	pauseDesktopHandoff(cuSession())
	emitComputerUseEvent(EventComputerUseControl, map[string]interface{}{
		"at":      time.Now().Format(time.RFC3339),
		"action":  "handoff",
		"reason":  n.Reason,
		"title":   n.Title,
		"paused":  true,
		"stopped": false,
	})
}

func pauseDesktopHandoff(sess *computeruse.Session) {
	if sess == nil {
		return
	}
	paused, stopped := sess.ControlState()
	if paused || stopped {
		return
	}
	sess.Pause()
	desktopHandoffPauseMu.Lock()
	if desktopHandoffPaused == nil {
		desktopHandoffPaused = map[*computeruse.Session]struct{}{}
	}
	desktopHandoffPaused[sess] = struct{}{}
	desktopHandoffPauseMu.Unlock()
}

func resumeDesktopHandoffPause() {
	desktopHandoffPauseMu.Lock()
	paused := desktopHandoffPaused
	desktopHandoffPaused = nil
	desktopHandoffPauseMu.Unlock()
	for sess := range paused {
		_ = sess.Resume()
	}
}

func clearDesktopHandoffPause() {
	desktopHandoffPauseMu.Lock()
	desktopHandoffPaused = nil
	desktopHandoffPauseMu.Unlock()
}
