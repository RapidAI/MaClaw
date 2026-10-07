package guiapp

// clearPerUserSessionState resets all per-user ephemeral state that
// accumulates during a conversation. This is the single source of truth
// for session cleanup — every code path that destroys a conversation
// (/new, /exit, StartNewTask, delete task, recall) MUST call this
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
//
// A pure coding workbench is not conversation residue. SSH session, work
// directory, and the armed coding turn are the project execution
// environment. Explicit destruction (this method) tears that down.
// A conversation boundary that is not destruction — the first message has
// no transcript yet — must use resetConversationResidue instead.
func (h *IMMessageHandler) clearPerUserSessionState(userID string) {
	h.clearPerUserSessionStateOpts(userID, false)
}

// resetConversationResidue ends chat-scoped state when a turn is classified
// as a new conversation (empty history, cancelled previous turn, topic
// switch). The workbench stays when the visible row is a coding task, or
// when that row is not visible yet and a template or sticky kind is already
// armed for the project owner. A visible ordinary row does not keep a
// leftover arm.
func (h *IMMessageHandler) resetConversationResidue(userID string) {
	if h == nil {
		return
	}
	h.clearPerUserSessionStateOpts(userID, h.codingWorkbenchSurvivesConversationBoundary(userID))
}

func (h *IMMessageHandler) clearPerUserSessionStateOpts(userID string, keepCodingWorkbench bool) {
	h.clearTaskIdentityAnchor(userID)

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
	h.pendingRecordAudio.Delete(userID)
	h.clearPendingPostRecording(userID)
	if _, preservePendingReply := h.suppressPendingUserReplyUpdate.Load(userID); !preservePendingReply {
		h.pendingUserReply.Delete(userID)
	}
	h.pendingCapabilityGap.Delete(userID)
	h.forgetAgentGuidedSkill(userID)
	h.pendingSlotUserText.Delete(userID)
	h.pendingInjection.Delete(userID)
	h.pendingPreLoopGuide.Delete(userID)
	h.clearGuideLaunchAcceptancesForUser(userID)
	h.cancelAndClearInFlightTurn(userID)
	h.setPendingForegroundText(userID, "")
	h.cancelledTaskBoundary.Delete(userID)

	// Compaction tracking state.
	h.compactionCount.Delete(userID)

	// Drift detection state.
	h.sessionDriftReplanCount.Delete(userID)
	h.sessionDriftTool.Delete(userID)

	// Workflow prompts and choice UI are chat residue (the consumer may
	// never have run, e.g. /new before the next message). The coding arm
	// — marker, template pending, sticky SSH binding — is the workbench
	// and survives a conversation boundary. Explicit destruction still
	// drops it.
	h.workflowReviewExperienceContext.Delete(userID)
	h.stashedPhasePrompt.Delete(userID)
	h.workflowOriginalRequest.Delete(userID)
	h.pendingCancelExecuteRequest.Delete(userID)
	h.pendingWorkflowChoice.Delete(userID)
	if !keepCodingWorkbench {
		h.workflowAgentLoopMarker.Delete(userID)
		h.pendingV2SubAgentExecution.Delete(userID)
		h.pendingTemplateCodingProjectPath.Delete(userID)
		h.pendingTemplateRemoteCoding.Delete(userID)
		h.clearStickyCodingWorkbenchMemory(userID)
	}
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
		h.toolRouter.ResetSessionForSession(userID)
	}

	// Steering file context (per-user partitioned).
	h.clearSteeringContextFiles(userID)

	// Memory snapshot cache.
	h.RefreshMemorySnapshot(userID)
	h.clearSessionFacts(userID)

	h.clearSessionGovernedTasksForUser(userID)
	h.clearSemanticSessionResidue(userID)
	// The parent carry is the same pin as the residue. Reset clears the
	// transcript and the obligation; leaving the carry stored made the next
	// short reply, and the next process, restore ssh for a task that ended.
	h.clearParentExecutionCarry(userID)
	h.clearActiveLocalDocumentsForUser(userID)
}
