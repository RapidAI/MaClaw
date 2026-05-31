package main

import (
	"log"
	"strings"
)

func (h *IMMessageHandler) enrichSendAndObserveTextForTask(sessionID, text string) string {
	taskOrch := h.activeTaskOrchestratorForSendObserve()
	if taskOrch == nil || !taskOrch.IsActive() {
		return text
	}
	handles := taskOrch.ReadyTaskHandles(1)
	if len(handles) == 0 {
		return text
	}
	handle := handles[0]
	task := handle.Task

	taskOrch.SetTaskSessionIDForRun(task, handle.RunID, sessionID)
	if task.Status != TaskExecPending {
		return text
	}

	taskPrompt := taskOrch.BuildTaskPromptForTaskRun(task, handle.RunID)
	if taskPrompt != "" {
		text = mergeTaskPromptWithSendObserveText(taskPrompt, text)
		log.Printf("[task-orchestrator] enriched send_and_observe for task %d: %s", task.Index+1, task.Title)
	}
	taskOrch.MarkTaskStatusForRun(task, handle.RunID, TaskExecInProgress, "")
	return text
}

func (h *IMMessageHandler) activeTaskOrchestratorForSendObserve() *TaskExecutionOrchestrator {
	if h == nil || h.taskOrchestratorRegistry == nil {
		return nil
	}
	ownerID := h.currentRuntimePolicyOwnerID()
	if ownerID == "" {
		return nil
	}
	if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(ownerID); !allowed {
		log.Printf("[task-orchestrator] skipped send_and_observe enrichment by workflow policy user=%s reason=%s", ownerID, reason)
		h.deactivateTaskOrchestratorForWorkflowPolicyBlock(ownerID, reason)
		return nil
	}
	return h.taskOrchestratorRegistry.Get(ownerID)
}

func mergeTaskPromptWithSendObserveText(taskPrompt, text string) string {
	if strings.TrimSpace(text) == "" {
		return taskPrompt
	}
	return taskPrompt + "\n\n---\n补充说明：\n" + text
}
