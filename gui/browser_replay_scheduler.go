package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// browserReplaySchedulerBridge wraps the scheduled task executor to handle
// browser_replay type actions.
type browserReplaySchedulerBridge struct {
	loopMgr       *agent.BackgroundLoopManager
	recorder      *browser.BrowserRecorder
	replayer      *browser.FlowReplayer
	activityStore browser.ActivityUpdater
	statusC       chan agent.StatusEvent
}

// handleScheduledReplay rejects legacy browser replay schedules. Stable browser
// automation uses browser(action="session_start") plus task_run in one session;
// replay remains disabled because old flows can carry stale selectors and
// coordinate-style actions.
// Returns (result, err, handled). If handled is false, the caller should use the default executor.
func (b *browserReplaySchedulerBridge) handleScheduledReplay(task *scheduler.ScheduledTask) (string, error, bool) {
	if task == nil {
		return "", nil, false
	}

	var action browser.ScheduledReplayAction
	if err := json.Unmarshal([]byte(task.Action), &action); err != nil {
		return "", nil, false
	}
	if !normalizeScheduledActionTypeKind(action.Type).IsBrowserReplay() || action.FlowName == "" {
		return "", nil, false
	}
	return "", fmt.Errorf("browser_replay schedules are disabled; use browser(action=\"session_start\") plus browser(action=\"task_run\", session_id=...)"), true
}

// wrapExecutorWithReplay wraps an existing TaskExecutor to intercept browser_replay actions.
func wrapExecutorWithReplay(original scheduler.TaskExecutor, bridge *browserReplaySchedulerBridge) scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		if bridge != nil {
			result, err, handled := bridge.handleScheduledReplay(task)
			if handled {
				return result, err
			}
		}
		if original != nil {
			return original(ctx, task)
		}
		return "", fmt.Errorf("no executor configured")
	}
}

// bgLoopMgrAdapter wraps *agent.BackgroundLoopManager to satisfy browser.LoopManager.
// Note: a similar adapter exists in corelib/browser/task_tools.go for in-package use.
type bgLoopMgrAdapter struct {
	mgr *agent.BackgroundLoopManager
}

func (a *bgLoopMgrAdapter) Complete(loopID string) { a.mgr.Complete(loopID) }
func (a *bgLoopMgrAdapter) Stop(loopID string)     { a.mgr.Stop(loopID) }
