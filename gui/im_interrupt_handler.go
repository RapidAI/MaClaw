package main

import (
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/progress"
)

// imInterruptHandler implements progress.InterruptHandler for the GUI layer.
// It bridges the IM gateway's interrupt signal to the running agent loop's
// cancel mechanism.
type imInterruptHandler struct {
	handler *IMMessageHandler

	// milestoneTrackers stores the active AgentProgressTracker per user.
	// Set by runAgentLoop when the tracker is created, cleared when it stops.
	milestoneTrackers sync.Map // map[userID]*progress.AgentProgressTracker
}

func newIMInterruptHandler(h *IMMessageHandler) *imInterruptHandler {
	return &imInterruptHandler{handler: h}
}

// SetTracker registers the active milestone tracker for a user.
func (ih *imInterruptHandler) SetTracker(userID string, tracker *progress.AgentProgressTracker) {
	if tracker != nil {
		ih.milestoneTrackers.Store(userID, tracker)
	}
}

// ClearTracker removes the milestone tracker for a user.
func (ih *imInterruptHandler) ClearTracker(userID string) {
	ih.milestoneTrackers.Delete(userID)
}

// TryInterrupt implements progress.InterruptHandler.
func (ih *imInterruptHandler) TryInterrupt(userID string, messageText string) progress.InterruptResult {
	messageText = strings.TrimSpace(messageText)
	if messageText == "" {
		return progress.InterruptResult{}
	}

	// Get the active milestone tracker for this user.
	var tracker *progress.AgentProgressTracker
	if v, ok := ih.milestoneTrackers.Load(userID); ok {
		tracker = v.(*progress.AgentProgressTracker)
	}

	// Compute scheduling signals.
	structure := progress.AnalyzeStructure(messageText)

	// Relevance: use embedding cosine if both vectors are available.
	var relevance float64 = -1
	if tracker != nil {
		taskEmbed := tracker.Buffer().TaskEmbed()
		if taskEmbed != nil {
			// TODO: compute message embedding via embedder when wired.
			// For now, relevance stays at -1 (unavailable).
			_ = taskEmbed
		}
	}

	// Domain match: use L1 keyword classification on both the current task
	// and the new message. This is the same classifyTaskIntent used by
	// CodingToolGate — zero latency, no LLM call.
	domainMatch := false
	if tracker != nil {
		taskIntent := tracker.Buffer().TaskIntent()
		if taskIntent != "" {
			// Classify the new message's intent using L1 keywords.
			newMsgResult := classifyTaskIntent(messageText)
			newMsgIntent := string(newMsgResult.Intent)
			// Same intent label = same domain.
			domainMatch = newMsgIntent == taskIntent
			// Also match if both are in the "coding" family.
			if isCodingFamily(taskIntent) && isCodingFamily(newMsgIntent) {
				domainMatch = true
			}
		}
	}

	decision := progress.Schedule(progress.ScheduleInput{
		Relevance:   relevance,
		DomainMatch: domainMatch,
		Structure:   structure,
	})

	log.Printf("[interrupt] user=%s msg=%q action=%s conf=%.2f domain=%v reason=%s",
		userID, truncateForLog(messageText, 30), decision.Action, decision.Confidence, domainMatch, decision.Reason)

	switch decision.Action {
	case progress.ActionReplace:
		taskText, err := ih.handler.CancelCurrentSession()
		if err != nil {
			log.Printf("[interrupt] CancelCurrentSession error: %v", err)
			return progress.InterruptResult{}
		}
		cancelReply := "⏹️ 已停止当前任务。"
		if taskText != "" {
			cancelReply = "⏹️ 已停止任务「" + truncateForLog(taskText, 20) + "」。"
		}
		if tracker != nil {
			if summary := tracker.Buffer().CompletedOutputSummary(); summary != "" {
				cancelReply += "\n已完成: " + summary
			}
		}
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionReplace,
			Reply:   cancelReply,
		}

	case progress.ActionStatusQuery:
		reply := "正在处理中..."
		if tracker != nil {
			reply = tracker.Buffer().ProgressSummary()
		}
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionStatusQuery,
			Reply:   reply,
		}

	case progress.ActionMerge:
		ih.handler.pendingInjection.Store(userID, messageText)
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionMerge,
			Reply:   "收到，已纳入当前任务。",
		}

	default:
		// Insert, Enqueue — let the gateway queue normally.
		return progress.InterruptResult{}
	}
}

// isCodingFamily returns true if the intent is in the coding domain family.
func isCodingFamily(intent string) bool {
	switch intent {
	case "coding", "bug_fix", "maintenance":
		return true
	default:
		return false
	}
}
