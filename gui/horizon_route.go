package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func (h *IMMessageHandler) handleHorizonIMRoute(msg IMUserMessage, trimmed string) (*IMAgentResponse, bool) {
	if h == nil {
		return nil, false
	}
	userID := strings.TrimSpace(msg.UserID)
	if userID == "" {
		return nil, false
	}
	resp := func(text string) *IMAgentResponse {
		return finalizeIMEntryHostResponse(&IMAgentResponse{Text: text}, imRequestID(msg), userID)
	}
	cmdKind := classifyImmediateIMCommand(trimmed)

	sess := h.loadHorizonSession(userID)
	if sess != nil && sess.isCancelled() {
		h.dropHorizonSession(userID)
		sess = nil
	}
	if sess != nil {
		sess.mu.Lock()
		resumeAsk := sess.resumeAsk
		sess.mu.Unlock()
		if resumeAsk {
			if longhorizon.IsAbandonText(trimmed) {
				sess.markCancelNotified()
				horizonLog(sess, "abandon", "")
				h.cancelHorizonSessionWithReason(userID, "abandon")
				return resp(horizonText(msg.Lang, "\u5df2\u653e\u5f03\u672a\u5b8c\u6210\u7684\u957f\u7a0b\u4efb\u52a1\u3002", "Abandoned the unfinished long-horizon task.")), true
			}
			if longhorizon.IsResumeText(trimmed) {
				sess.mu.Lock()
				sess.resumeAsk = false
				sess.mu.Unlock()
				horizonLog(sess, "resume", "")
				h.launchHorizonSupervisor(sess)
				return resp(horizonText(msg.Lang, "\u5df2\u6062\u590d\u957f\u7a0b\u4efb\u52a1\u3002", "Resumed the long-horizon task.")), true
			}
		}
		if cmdKind == imCommandCancel || longhorizon.IsCancelText(trimmed) {
			sess.markCancelNotified()
			h.cancelHorizonSessionWithReason(userID, "text")
			return resp(horizonText(msg.Lang, "\u5df2\u53d6\u6d88\u957f\u7a0b\u4efb\u52a1\u3002", "Long-horizon task cancelled.")), true
		}
		if cmdKind != imCommandUnknown {
			if cmdKind == imCommandReset || cmdKind == imCommandExit {
				sess.markCancelNotified()
				h.cancelHorizonSessionWithReason(userID, "command")
			}
			return nil, false
		}
		if resumeAsk {
			if body, admit := longhorizon.ParseAdmitCommand(trimmed); admit {
				if strings.TrimSpace(body) == "" {
					return resp(horizonText(msg.Lang, "\u8bf7\u5148\u8bf4\u660e\u8981\u505a\u4ec0\u4e48\u3002\u7528\u6cd5\uff1a@horizon \u4efb\u52a1\u63cf\u8ff0", "Please say what to do. Usage: @horizon <task>")), true
				}
				h.cancelHorizonSessionWithReason(userID, "replaced")
			} else {
				sess.enqueue(trimmed)
				return resp(horizonText(msg.Lang, "\u53d1\u73b0\u672a\u5b8c\u6210\u7684\u957f\u7a0b\u4efb\u52a1\u3002\u56de\u590d\u300c\u6062\u590d\u300d\u7ee7\u7eed\uff0c\u6216\u300c\u653e\u5f03\u300d\u53d6\u6d88\u3002", "Unfinished long-horizon task found. Reply resume to continue, or abandon to cancel.")), true
			}
		} else {
			if _, admit := longhorizon.ParseAdmitCommand(trimmed); admit {
				horizonLog(sess, "admit_rejected", "reason=already_running")
				return resp(horizonText(msg.Lang, "\u5df2\u6709\u957f\u7a0b\u4efb\u52a1\u5728\u8dd1\uff0c\u8bf7\u5148\u505c\u6b62\u6216\u7b49\u5f85\u7ed3\u675f\u3002", "A long-horizon task is already running. Stop it or wait.")), true
			}
			sess.enqueue(trimmed)
			return resp(horizonText(msg.Lang, "\u5df2\u8bb0\u5f55\uff0c\u5c06\u5728\u4e0b\u4e00\u8f6e\u8c03\u5ea6\u65f6\u8003\u8651\u3002", "Recorded. It will be considered in the next manager round.")), true
		}
	}

	body, admit := longhorizon.ParseAdmitCommand(trimmed)
	if !admit {
		if cmdKind != imCommandUnknown {
			return nil, false
		}
		if recovered := h.maybeOfferHorizonResume(msg); recovered != nil {
			return recovered, true
		}
		return nil, false
	}
	if h.horizonSupervisorRunning(userID) {
		horizonLogOwner(userID, "admit_rejected", "reason=still_stopping")
		return resp(horizonText(msg.Lang, "\u4e0a\u4e00\u4e2a\u957f\u7a0b\u4efb\u52a1\u6b63\u5728\u505c\u6b62\uff0c\u8bf7\u7a0d\u540e\u518d\u8bd5\u3002", "The previous long-horizon task is still stopping. Try again in a moment.")), true
	}
	if ShouldUseSubAgent(h.getTaskOrchestratorReadOnly(userID)) {
		horizonLogOwner(userID, "admit_rejected", "reason=workflow_subagent")
		return resp(horizonText(msg.Lang, "\u5f53\u524d\u6709 Workflow SubAgent \u5728\u8dd1\uff0c\u4e0d\u80fd\u540c\u65f6\u542f\u52a8\u957f\u7a0b\u4efb\u52a1\u3002", "A Workflow SubAgent is active; cannot start a long-horizon task.")), true
	}
	if strings.TrimSpace(body) == "" {
		horizonLogOwner(userID, "admit_rejected", "reason=empty_goal")
		return resp(horizonText(msg.Lang, "\u8bf7\u5148\u8bf4\u660e\u8981\u505a\u4ec0\u4e48\u3002\u7528\u6cd5\uff1a@horizon \u4efb\u52a1\u63cf\u8ff0", "Please say what to do. Usage: @horizon <task>")), true
	}
	body = longhorizon.Clip(body, longhorizon.GoalCap)
	projectPath := h.horizonProjectPath(userID, msg)
	if projectPath == "" {
		horizonLogOwner(userID, "admit_rejected", "reason=no_project")
		return resp(horizonText(msg.Lang, "\u8bf7\u5148\u6253\u5f00\u9879\u76ee\u5de5\u4f5c\u533a\u3002", "Open a project workspace first.")), true
	}

	taskID := fmt.Sprintf("hz-%d", timeNowUnixNano())
	state := &longhorizon.TaskState{
		TaskID:    taskID,
		UserGoal:  body,
		Status:    longhorizon.StatusIdle,
		MaxRounds: longhorizon.ClampMaxRounds(longhorizon.DefaultMaxRounds),
		Policy: longhorizon.PolicySnapshot{
			OwnerID:       userID,
			ProjectRoot:   projectPath,
			HorizonTaskID: taskID,
			EventScopeID:  h.horizonEventScopeID(userID),
		},
	}
	sess = &horizonSession{
		ownerID:      userID,
		requestID:    imRequestID(msg),
		lang:         msg.Lang,
		state:        state,
		notify:       make(chan struct{}, 1),
		status:       longhorizon.StatusIdle,
		storeRoot:    h.horizonStoreRoot(),
		handler:      h,
		eventScopeID: state.Policy.EventScopeID,
	}
	if !h.storeHorizonSessionIfAbsent(sess) {
		horizonLog(sess, "admit_rejected", "reason=already_running")
		return resp(horizonText(msg.Lang, "\u5df2\u6709\u957f\u7a0b\u4efb\u52a1\u5728\u8dd1\uff0c\u8bf7\u5148\u505c\u6b62\u6216\u7b49\u5f85\u7ed3\u675f\u3002", "A long-horizon task is already running. Stop it or wait.")), true
	}
	sess.persist()
	horizonLog(sess, "admit", horizonLogKV(horizonLogField("project", projectPath), horizonLogField("goal", body), horizonLogField("scope", sess.eventScopeID)))
	h.launchHorizonSupervisor(sess)
	return resp(horizonText(msg.Lang, "\u957f\u7a0b\u4efb\u52a1\u5df2\u5f00\u59cb\u3002\u4e4b\u540e\u7684\u6d88\u606f\u4f1a\u8bb0\u5165\u4e0b\u4e00\u8f6e\u8c03\u5ea6\uff0c\u4e0d\u4f1a\u65b0\u5f00\u5bf9\u8bdd\u3002", "Long-horizon task started. Later messages are queued for the next manager round.")), true
}

func (h *IMMessageHandler) maybeOfferHorizonResume(msg IMUserMessage) *IMAgentResponse {
	if h == nil {
		return nil
	}
	userID := strings.TrimSpace(msg.UserID)
	root := h.horizonStoreRoot()
	if userID == "" || root == "" {
		return nil
	}
	if h.horizonSupervisorRunning(userID) || ShouldUseSubAgent(h.getTaskOrchestratorReadOnly(userID)) {
		return nil
	}
	state, err := longhorizon.FindIncompleteTask(root, userID)
	if err != nil || state == nil {
		return nil
	}
	if trigger := strings.TrimSpace(msg.Text); trigger != "" {
		state.Carryover = append(state.Carryover, trigger)
		state.Carryover = longhorizon.ClipCarryover(state.Carryover)
	}
	sess := &horizonSession{
		ownerID:          userID,
		requestID:        imRequestID(msg),
		lang:             msg.Lang,
		state:            state,
		notify:           make(chan struct{}, 1),
		status:           state.Status,
		resumeAsk:        true,
		experienceWrites: state.ExperienceWrites,
		storeRoot:        root,
		handler:          h,
		eventScopeID:     state.Policy.EventScopeID,
	}
	if !h.storeHorizonSessionIfAbsent(sess) {
		existing := h.loadHorizonSession(userID)
		if existing == nil {
			return nil
		}
		existing.enqueue(strings.TrimSpace(msg.Text))
		existing.mu.Lock()
		resumeAsk := existing.resumeAsk
		existing.mu.Unlock()
		if resumeAsk {
			return finalizeIMEntryHostResponse(&IMAgentResponse{Text: horizonText(msg.Lang, "\u53d1\u73b0\u672a\u5b8c\u6210\u7684\u957f\u7a0b\u4efb\u52a1\u3002\u56de\u590d\u300c\u6062\u590d\u300d\u7ee7\u7eed\uff0c\u6216\u300c\u653e\u5f03\u300d\u53d6\u6d88\u3002", "Unfinished long-horizon task found. Reply resume to continue, or abandon to cancel.")}, imRequestID(msg), userID)
		}
		return finalizeIMEntryHostResponse(&IMAgentResponse{Text: horizonText(msg.Lang, "\u5df2\u8bb0\u5f55\uff0c\u5c06\u5728\u4e0b\u4e00\u8f6e\u8c03\u5ea6\u65f6\u8003\u8651\u3002", "Recorded. It will be considered in the next manager round.")}, imRequestID(msg), userID)
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		horizonLog(sess, "persist_fail", horizonLogField("err", err.Error()))
	}
	horizonLog(sess, "resume_offer", horizonLogKV("status="+state.Status, fmt.Sprintf("round=%d", state.RoundIndex)))
	return finalizeIMEntryHostResponse(&IMAgentResponse{Text: horizonText(msg.Lang, "\u53d1\u73b0\u672a\u5b8c\u6210\u7684\u957f\u7a0b\u4efb\u52a1\u3002\u56de\u590d\u300c\u6062\u590d\u300d\u7ee7\u7eed\uff0c\u6216\u300c\u653e\u5f03\u300d\u53d6\u6d88\u3002", "Unfinished long-horizon task found. Reply resume to continue, or abandon to cancel.")}, imRequestID(msg), userID)
}

func (h *IMMessageHandler) launchHorizonSupervisor(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	sess.mu.Lock()
	if sess.started || sess.cancelled {
		sess.mu.Unlock()
		return
	}
	sess.started = true
	sess.mu.Unlock()
	horizonLog(sess, "supervisor_launch", "")
	if h.horizonStartSupervisor != nil {
		h.horizonStartSupervisor(sess)
		return
	}
	h.markHorizonRunning(sess)
	go func() {
		defer h.clearHorizonRunning(sess)
		h.runHorizonSupervisor(sess)
	}()
}

func (h *IMMessageHandler) horizonEventScopeID(userID string) string {
	if h != nil && h.app != nil {
		return h.app.getEventScopeID(userID)
	}
	return ""
}

func horizonText(lang, zh, en string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		return en
	}
	return zh
}

func timeNowUnixNano() int64 {
	return time.Now().UnixNano()
}
