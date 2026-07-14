package main

// clearPerUserSessionState resets all per-user ephemeral state that
// accumulates during a conversation. This is the single source of truth
// for session cleanup — every code path that resets a conversation
// (/new, /exit, StartNewTask, topic switch, auto-clear) MUST call this
// method instead of manually deleting individual sync.Map entries.
//
// This prevents the "forgot to add .Delete for the new field" class of
// bugs: when a new sync.Map field is added to IMMessageHandler, it only
// needs to be cleaned up here.
//
// NOTE: This method only clears ephemeral per-message/session state. It does NOT:
//   - Clear conversation memory (caller decides: memory.clear vs clearConversationAndDismissSlot)
//   - Decide whether reset is allowed (caller decides command/task boundary)
//   - Flush evidence (only /new and /exit do this)
//   - Reset workflow adapter state (only /exit does this)
//
// Those are caller-specific side effects that vary by reset path.
func (h *IMMessageHandler) clearPerUserSessionState(userID string) {
	// Cancel any active workflow and understanding session. Without this,
	// a stale workflow survives dismiss/clear and hijacks subsequent messages
	// via QuickFilter.HasActiveWorkflow → FilterActiveWorkflow.
	h.cancelWorkflowForUser(userID)

	// Clear page index for cross-page recall (Requirement 7).
	if h.memoryStore != nil {
		if pi := h.memoryStore.PageIdx(); pi != nil {
			pi.Clear(userID)
		}
	}

	// Pending interaction state.
	h.pendingAskUser.Delete(userID)
	if _, preservePendingReply := h.suppressPendingUserReplyUpdate.Load(userID); !preservePendingReply {
		h.pendingUserReply.Delete(userID)
	}
	h.pendingCapabilityGap.Delete(userID)
	h.pendingSlotUserText.Delete(userID)
	h.pendingInjection.Delete(userID)
	h.pendingPreLoopGuide.Delete(userID)
	h.cancelledTaskBoundary.Delete(userID)

	// Compaction tracking state.
	h.compactionCount.Delete(userID)

	// Drift detection state.
	h.sessionDriftReplanCount.Delete(userID)
	h.sessionDriftTool.Delete(userID)

	// Workflow ephemeral markers (LoadAndDelete-consumed, but clean up
	// in case the consumer never ran — e.g. user /new before next message).
	h.workflowAgentLoopMarker.Delete(userID)
	h.workflowReviewExperienceContext.Delete(userID)
	h.stashedPhasePrompt.Delete(userID)
	h.workflowOriginalRequest.Delete(userID)
	h.pendingCancelExecuteRequest.Delete(userID)
	h.pendingV2SubAgentExecution.Delete(userID)
	h.pendingWorkflowChoice.Delete(userID)
	h.pendingTemplateCodingProjectPath.Delete(userID)
	h.pendingTemplateRemoteCoding.Delete(userID)
	h.clearStickyCodingWorkbenchMemory(userID)
	if h.confirmationStore != nil {
		h.confirmationStore.clear(userID)
	}

	// Task execution orchestrator: deactivate to prevent stale orchestrator
	// state from routing the next message to SubAgent after a session reset.
	if h.taskOrchestratorRegistry != nil {
		if o := h.taskOrchestratorRegistry.Get(userID); o != nil && o.IsActive() {
			o.Deactivate()
		}
	}

	// Tool router session state: clear session-pinned conditional tools
	// (e.g. ssh, browser) so the new conversation starts with a clean tool
	// list determined solely by the user's first message.
	if h.toolRouter != nil {
		h.toolRouter.ResetSession()
	}

	// Steering file context (per-user partitioned).
	h.clearSteeringContextFiles(userID)

	// Memory snapshot cache.
	h.RefreshMemorySnapshot(userID)
}
