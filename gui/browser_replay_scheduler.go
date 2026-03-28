package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// browserReplaySchedulerBridge wraps the scheduled task executor to handle
// browser_replay type actions by spawning background replay tasks.
type browserReplaySchedulerBridge struct {
	loopMgr       *agent.BackgroundLoopManager
	recorder      *browser.BrowserRecorder
	replayer      *browser.FlowReplayer
	activityStore browser.ActivityUpdater
	statusC       chan agent.StatusEvent
}

// handleScheduledReplay checks if a scheduled task is a browser replay and executes it.
// Returns (result, err, handled). If handled is false, the caller should use the default executor.
func (b *browserReplaySchedulerBridge) handleScheduledReplay(task *scheduler.ScheduledTask) (string, error, bool) {
	if task == nil || b.loopMgr == nil {
		return "", nil, false
	}

	// Try to parse as ScheduledReplayAction
	var action browser.ScheduledReplayAction
	if err := json.Unmarshal([]byte(task.Action), &action); err != nil {
		return "", nil, false // not a structured action, let default executor handle
	}
	if action.Type != "browser_replay" || action.FlowName == "" {
		return "", nil, false
	}

	// Load the flow
	flow, err := b.recorder.LoadFlow(action.FlowName)
	if err != nil {
		return "", fmt.Errorf("加载流程 %s 失败: %w", action.FlowName, err), true
	}

	desc := fmt.Sprintf("定时回放: %s", flow.Name)
	loopCtx, waitCh := b.loopMgr.SpawnOrQueue(agent.SlotKindBrowser, "", desc, 1)

	logger := func(msg string) {
		log.Printf("[scheduled-replay] %s", msg)
	}
	loopMgrAdapter := &bgLoopMgrAdapter{mgr: b.loopMgr}

	if loopCtx != nil {
		go browser.RunReplayInBackground(loopCtx, flow, action.Overrides, b.replayer, b.activityStore, b.statusC, loopMgrAdapter, logger)
		return fmt.Sprintf("定时回放 [%s] 已提交后台执行 (task_id=%s)", flow.Name, loopCtx.ID), nil, true
	}

	// Queued
	go func() {
		ctx := <-waitCh
		browser.RunReplayInBackground(ctx, flow, action.Overrides, b.replayer, b.activityStore, b.statusC, loopMgrAdapter, logger)
	}()
	queuePos := b.loopMgr.QueueLength(agent.SlotKindBrowser)
	return fmt.Sprintf("定时回放 [%s] 已排队（位置 %d）", flow.Name, queuePos), nil, true
}

// wrapExecutorWithReplay wraps an existing TaskExecutor to intercept browser_replay actions.
func wrapExecutorWithReplay(original scheduler.TaskExecutor, bridge *browserReplaySchedulerBridge) scheduler.TaskExecutor {
	return func(task *scheduler.ScheduledTask) (string, error) {
		result, err, handled := bridge.handleScheduledReplay(task)
		if handled {
			return result, err
		}
		if original != nil {
			return original(task)
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
