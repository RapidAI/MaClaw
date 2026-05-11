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
	task := taskOrch.CurrentTask()
	if task == nil {
		return text
	}

	taskOrch.SetCurrentSessionID(sessionID)
	if task.Status != TaskExecPending {
		return text
	}

	taskPrompt := taskOrch.BuildTaskPrompt()
	if taskPrompt != "" {
		text = mergeTaskPromptWithSendObserveText(taskPrompt, text)
		log.Printf("[task-orchestrator] enriched send_and_observe for task %d: %s", task.Index+1, task.Title)
	}
	taskOrch.MarkCurrentStatus(TaskExecInProgress, "")
	return text
}

func (h *IMMessageHandler) activeTaskOrchestratorForSendObserve() *TaskExecutionOrchestrator {
	if h == nil || h.taskOrchestratorRegistry == nil || h.lastUserID == "" {
		return nil
	}
	return h.taskOrchestratorRegistry.Get(h.lastUserID)
}

func mergeTaskPromptWithSendObserveText(taskPrompt, text string) string {
	if strings.TrimSpace(text) == "" {
		return taskPrompt
	}
	return taskPrompt + "\n\n---\n补充说明：\n" + text
}
