package browser

import (
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ActivityUpdater abstracts the AgentActivityStore for decoupling corelib from gui.
type ActivityUpdater interface {
	UpdateReplay(flowName string, currentStep, totalSteps int, status string)
	ClearReplay()
}

// LoopManager abstracts BackgroundLoopManager for decoupling corelib from gui.
type LoopManager interface {
	Complete(loopID string)
	Stop(loopID string)
}

// RunReplayInBackground executes a browser replay as a background task.
// It updates the activity store with progress and sends completion notifications.
// Designed to run in a goroutine spawned by BackgroundLoopManager.
func RunReplayInBackground(
	loopCtx *agent.LoopContext,
	flow *RecordedFlow,
	overrides map[string]string,
	replayer *FlowReplayer,
	activityStore ActivityUpdater,
	statusC chan agent.StatusEvent,
	loopMgr LoopManager,
	logger func(string),
) {
	defer func() {
		if r := recover(); r != nil {
			if logger != nil {
				logger(fmt.Sprintf("[replay-bg] panic recovered: %v", r))
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
		logger(fmt.Sprintf("[replay-bg] starting replay: %s (%d steps)", flowName, totalSteps))
	}
	if activityStore != nil {
		activityStore.UpdateReplay(flowName, 0, totalSteps, "running")
	}

	// Monitor loopCtx cancel signal — propagate to supervisor
	replayDone := make(chan struct{})
	go func() {
		select {
		case <-loopCtx.CancelC:
			// Cancel the replay task via supervisor's public Cancel method.
			// We iterate GetState to find running tasks — but since replay
			// creates exactly one task, we cancel all running/paused ones.
			sup := replayer.supervisor
			sup.mu.RLock()
			var ids []string
			for id := range sup.tasks {
				ids = append(ids, id)
			}
			sup.mu.RUnlock()
			for _, id := range ids {
				_ = sup.Cancel(id) // Cancel checks status internally
			}
			if logger != nil {
				logger(fmt.Sprintf("[replay-bg] cancel signal received for %s", flowName))
			}
		case <-replayDone:
		}
	}()

	state, err := replayer.Replay(flow, overrides)
	close(replayDone) // stop cancel monitor
	elapsed := time.Since(startTime)

	if err != nil {
		if logger != nil {
			logger(fmt.Sprintf("[replay-bg] replay failed: %s — %v", flowName, err))
		}
		failStep := 0
		if state != nil {
			failStep = state.CurrentStep
		}
		notifyReplayComplete(statusC, loopCtx.ID, flowName, elapsed, false, failStep, totalSteps, err.Error(), state)
		return
	}

	if logger != nil {
		logger(fmt.Sprintf("[replay-bg] replay completed: %s in %v", flowName, elapsed))
	}
	notifyReplayComplete(statusC, loopCtx.ID, flowName, elapsed, true, totalSteps, totalSteps, "", state)
}

// notifyReplayComplete sends a status event for replay completion.
func notifyReplayComplete(
	statusC chan agent.StatusEvent,
	loopID, flowName string,
	elapsed time.Duration,
	success bool,
	currentStep, totalSteps int,
	errMsg string,
	state *TaskState,
) {
	if statusC == nil {
		return
	}

	evType := agent.StatusEventSessionCompleted
	msg := fmt.Sprintf("浏览器回放 [%s] 完成，耗时 %v", flowName, elapsed.Round(time.Second))
	if !success {
		evType = agent.StatusEventSessionFailed
		msg = fmt.Sprintf("浏览器回放 [%s] 失败（步骤 %d/%d）: %s", flowName, currentStep, totalSteps, errMsg)
	}

	ev := agent.StatusEvent{
		Type:    evType,
		LoopID:  loopID,
		Message: msg,
	}

	// Attach last screenshot if available
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

// ScheduledReplayAction is the structured action payload for scheduled browser replay tasks.
// It is serialized as JSON in ScheduledTask.Action when task_type is "process".
type ScheduledReplayAction struct {
	Type      string            `json:"type"`      // fixed: "browser_replay"
	FlowName  string            `json:"flow_name"` // name of the recorded flow
	Overrides map[string]string `json:"overrides,omitempty"`
}
