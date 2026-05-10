package guiautomation

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// GUIActivityUpdater abstracts the AgentActivityStore for decoupling corelib from gui.
type GUIActivityUpdater interface {
	UpdateReplay(flowName string, currentStep, totalSteps int, status string)
	ClearReplay()
}

// GUILoopManager abstracts BackgroundLoopManager for decoupling corelib from gui.
type GUILoopManager interface {
	Complete(loopID string)
	Stop(loopID string)
}

// RunGUIReplayInBackground executes a GUI replay as a background task.
// It updates the activity store with progress and sends completion notifications.
// Designed to run in a goroutine spawned by BackgroundLoopManager.
func RunGUIReplayInBackground(
	loopCtx *agent.LoopContext,
	flow *GUIRecordedFlow,
	overrides map[string]string,
	replayer *GUIReplayer,
	activityStore GUIActivityUpdater,
	statusC chan agent.StatusEvent,
	loopMgr GUILoopManager,
	logger func(string),
) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger(fmt.Sprintf("[gui-replay-bg] panic recovered: %v", r))
			}
		}
		if activityStore != nil {
			activityStore.ClearReplay()
		}
		loopMgr.Complete(loopCtx.ID)
	}()

	startTime := time.Now()
	flowName := flow.Name
	totalSteps := len(flow.Steps)

	if logger != nil {
		logger(fmt.Sprintf("[gui-replay-bg] starting replay: %s (%d steps)", flowName, totalSteps))
	}
	if activityStore != nil {
		activityStore.UpdateReplay(flowName, 0, totalSteps, "running")
	}

	// Monitor cancel signal
	replayDone := make(chan struct{})
	go func() {
		select {
		case <-loopCtx.CancelC:
			replayer.CancelAll()
			if logger != nil {
				logger(fmt.Sprintf("[gui-replay-bg] cancel signal received for %s", flowName))
			}
		case <-replayDone:
		}
	}()

	state, err := replayer.Replay(flow, overrides)
	close(replayDone)
	elapsed := time.Since(startTime)

	if err != nil {
		if logger != nil {
			logger(fmt.Sprintf("[gui-replay-bg] replay failed: %s — %v", flowName, err))
		}
		failStep := 0
		if state != nil {
			failStep = state.CurrentStep
		}
		notifyGUIReplayComplete(statusC, loopCtx.ID, flowName, elapsed, false, failStep, totalSteps, err.Error(), state)
		return
	}

	if logger != nil {
		logger(fmt.Sprintf("[gui-replay-bg] replay completed: %s in %v", flowName, elapsed))
	}
	notifyGUIReplayComplete(statusC, loopCtx.ID, flowName, elapsed, true, totalSteps, totalSteps, "", state)
}

func notifyGUIReplayComplete(
	statusC chan agent.StatusEvent,
	loopID, flowName string,
	elapsed time.Duration,
	success bool,
	currentStep, totalSteps int,
	errMsg string,
	state *GUITaskState,
) {
	if statusC == nil {
		return
	}

	evType := agent.StatusEventSessionCompleted
	msg := fmt.Sprintf("GUI 回放 [%s] 完成，耗时 %v", flowName, elapsed.Round(time.Second))
	if !success {
		evType = agent.StatusEventSessionFailed
		msg = fmt.Sprintf("GUI 回放 [%s] 失败（步骤 %d/%d）: %s", flowName, currentStep, totalSteps, errMsg)
	}

	ev := agent.StatusEvent{
		Type:    evType,
		LoopID:  loopID,
		Message: msg,
	}

	if state != nil && len(state.Checkpoints) > 0 {
		last := state.Checkpoints[len(state.Checkpoints)-1]
		if last.ScreenshotB64 != "" {
			ev.Extra = map[string]string{"screenshot": last.ScreenshotB64}
		}
	}

	select {
	case statusC <- ev:
	default:
	}
}

// bgGUILoopManagerAdapter wraps *agent.BackgroundLoopManager to satisfy GUILoopManager.
type bgGUILoopManagerAdapter struct {
	mgr *agent.BackgroundLoopManager
}

func (a *bgGUILoopManagerAdapter) Complete(loopID string) { a.mgr.Complete(loopID) }
func (a *bgGUILoopManagerAdapter) Stop(loopID string)     { a.mgr.Stop(loopID) }
