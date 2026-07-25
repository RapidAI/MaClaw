package main

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// ComputerUsePause soft-blocks further click/type/key/scroll/focus.
// Observe remains allowed so the operator/agent can still inspect the screen.
func (a *App) ComputerUsePause() error {
	sess := cuSession()
	if sess == nil {
		return fmt.Errorf("computer use session not ready")
	}
	sess.Pause()
	a.emitComputerUseControl("pause")
	return nil
}

// ComputerUseResume clears soft pause (fails if session was hard-stopped).
func (a *App) ComputerUseResume() error {
	sess := cuSession()
	if sess == nil {
		return fmt.Errorf("computer use session not ready")
	}
	if err := sess.Resume(); err != nil {
		a.emitComputerUseControl("resume_failed")
		return err
	}
	a.emitComputerUseControl("resume")
	return nil
}

// ComputerUseStop hard-stops Computer Use and best-effort cancels the active
// desktop AI assistant turn so the agent stops issuing more actions.
func (a *App) ComputerUseStop() error {
	// Always drop sticky injection first so the next chat cannot re-enter CU
	// tools even if session teardown fails.
	clearComputerUseSessionActive()
	sess := cuSession()
	if sess == nil {
		a.emitComputerUseControl("stop")
		return fmt.Errorf("computer use session not ready")
	}
	sess.Stop()
	a.emitComputerUseControl("stop")
	// Best-effort: cancel current assistant generation so the loop exits.
	if a != nil {
		if _, err := a.CancelAIAssistantSession(); err != nil {
			// Non-fatal: tools already blocked by Stop().
			logComputerUseControl("cancel assistant after stop: %v", err)
		}
	}
	return nil
}

// ComputerUseReset clears stop/pause so a new desktop task can run.
func (a *App) ComputerUseReset() error {
	clearComputerUseSessionActive()
	sess := cuSession()
	if sess == nil {
		// Recreate empty session.
		globalComputerUse.mu.Lock()
		globalComputerUse.session = computeruse.NewSession(computeruse.DefaultConfig())
		globalComputerUse.mu.Unlock()
		a.emitComputerUseControl("reset")
		return nil
	}
	sess.ResetControl()
	a.emitComputerUseControl("reset")
	return nil
}

func (a *App) emitComputerUseControl(action string) {
	sess := cuSession()
	paused, stopped := false, false
	steps := 0
	if sess != nil {
		paused, stopped = sess.ControlState()
		if p := sess.Policy(); p != nil {
			steps = p.StepCount()
		}
	}
	payload := map[string]interface{}{
		"at":      time.Now().Format(time.RFC3339),
		"action":  action,
		"paused":  paused || stopped, // UI treats stopped as not interactive
		"stopped": stopped,
		"steps":   steps,
	}
	if a != nil {
		a.emitEvent(EventComputerUseControl, payload)
	} else {
		emitComputerUseEvent(EventComputerUseControl, payload)
	}
	// Keep system tray submenu in sync (Windows).
	if UpdateComputerUseTray != nil {
		UpdateComputerUseTray()
	}
}

func logComputerUseControl(format string, args ...interface{}) {
	if globalComputerUse.logger != nil {
		globalComputerUse.logger(fmt.Sprintf(format, args...))
	}
}
