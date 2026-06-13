package main

import (
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

// imInterruptHandler implements progress.InterruptHandler for the GUI layer.
// It bridges the IM gateway's interrupt signal to the running agent loop's
// cancel mechanism.
//
// Three-signal scheduling: relevance (embedding cosine) + semantic domain
// match + structure (negation/length). All four ScheduleActions have
// execution paths: Replace, Merge, StatusQuery, Queue.
type imInterruptHandler struct {
	handler *IMMessageHandler

	// milestoneTrackers stores the active AgentProgressTracker per user.
	// Set by runAgentLoop when the tracker is created, cleared when it stops.
	milestoneTrackers sync.Map // map[userID]*progress.AgentProgressTracker

	// embedder computes message embeddings for relevance scoring.
	// Set via SetEmbedder after the embedding model is loaded.
	// May be nil or NoopEmbedder — relevance degrades to -1 (unavailable).
	embedder embedding.Embedder
}

func newIMInterruptHandler(h *IMMessageHandler) *imInterruptHandler {
	return &imInterruptHandler{handler: h}
}

// SetEmbedder configures the embedder for semantic relevance computation.
// Called from app.go / activateEmbedderAsync after the embedding model loads.
func (ih *imInterruptHandler) SetEmbedder(emb embedding.Embedder) {
	ih.embedder = emb
}

// EmbedderForSubAgent returns the configured embedder for use by SubAgent
// skill selection. Returns nil if no embedder is loaded.
func (ih *imInterruptHandler) EmbedderForSubAgent() embedding.Embedder {
	return ih.embedder
}

// EmbedText computes the embedding vector for the given text.
// Returns nil if no embedder is available or embedding fails.
// Used by runAgentLoop to pre-compute taskEmbed for relevance scoring.
func (ih *imInterruptHandler) EmbedText(text string) []float32 {
	if ih.embedder == nil || embedding.IsNoop(ih.embedder) {
		return nil
	}
	vec, err := ih.embedder.Embed(text)
	if err != nil {
		return nil
	}
	return vec
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
	if ih.handler == nil || ih.handler.hasCancelledTaskBoundary(userID) || !ih.handler.hasActiveLoopForUser(userID) {
		return progress.InterruptResult{}
	}

	// Get the active milestone tracker for this user.
	var tracker *progress.AgentProgressTracker
	if v, ok := ih.milestoneTrackers.Load(userID); ok {
		tracker = v.(*progress.AgentProgressTracker)
	}

	// Compute scheduling signals.
	structure := progress.AnalyzeStructure(messageText)

	// Relevance: compute embedding cosine similarity between the new message
	// and the current task description. When embedder is unavailable or task
	// has no embedding, relevance stays at -1 and Schedule() degrades to
	// domain match + structure only (two-signal mode).
	var relevance float64 = -1
	if tracker != nil {
		taskEmbed := tracker.Buffer().TaskEmbed()
		if taskEmbed != nil && ih.embedder != nil && !embedding.IsNoop(ih.embedder) {
			if msgEmbed, err := ih.embedder.Embed(messageText); err == nil && len(msgEmbed) > 0 {
				relevance = progress.CosineSimilarity(taskEmbed, msgEmbed)
			}
		}
	}

	// Domain match: use classifyTaskIntent (delegates to UIC when available)
	// on both the current task and the new message.
	domainMatch := false
	if tracker != nil {
		taskIntent := tracker.Buffer().TaskIntent()
		if taskIntent != "" {
			// Classify the new message's intent through the semantic intent path.
			newMsgResult := classifyTaskIntent(messageText)
			newMsgIntent := string(newMsgResult.Intent)
			// Same intent label = same domain.
			domainMatch = newMsgIntent == taskIntent
			// Also match if both are in the "coding" family.
			if sameInterruptIntentFamily(taskIntent, newMsgIntent) {
				domainMatch = true
			}
		}
	}

	decision := progress.Schedule(progress.ScheduleInput{
		Relevance:   relevance,
		DomainMatch: domainMatch,
		Structure:   structure,
	})

	log.Printf("[interrupt] user=%s msg_len=%d action=%s conf=%.2f domain=%v reason=%s",
		userID, len([]rune(messageText)), decision.Action, decision.Confidence, domainMatch, decision.Reason)

	switch decision.Action {
	case progress.ActionReplace:
		// Low-confidence Replace (e.g. negation detected but could be a
		// modification like "帮我把那个订单取消了") — don't execute immediately.
		// Return PendingConfirm so the gateway holds the message and asks
		// the user to pick an action. If the user doesn't respond before
		// TTL, the gateway re-dispatches as a normal queued message.
		if decision.Confidence < 0.70 {
			return progress.InterruptResult{
				PendingConfirm: true,
				Action:         progress.ActionReplace,
				Reply:          "⚠️ 检测到可能要停止当前任务，确认吗？",
				Corrections: []progress.CorrectionOption{
					progress.NewCorrectionOption("确认打断", progress.ActionReplace),
					progress.NewCorrectionOption("补充当前任务", progress.ActionMerge),
					progress.NewCorrectionOption("排队等候", progress.ActionQueue),
				},
			}
		}
		taskText, err := ih.handler.CancelSessionForUser(userID)
		if err != nil {
			log.Printf("[interrupt] CancelSessionForUser error: %v", err)
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
			// StatusQuery is read-only — no corrections needed.
		}

	case progress.ActionMerge:
		// Grade the injection by signal strength so LLM treats it with
		// appropriate priority (directive > supplement > note).
		injection := classifyMergeInjection(messageText, decision, progress.ScheduleInput{
			Relevance:   relevance,
			DomainMatch: domainMatch,
			Structure:   structure,
		})
		ih.handler.accumulateInjection(userID, injection)
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionMerge,
			Reply:   "👌 收到，已纳入当前任务。",
			Corrections: []progress.CorrectionOption{
				progress.NewCorrectionOption("改为打断", progress.ActionReplace),
				progress.NewCorrectionOption("改为排队", progress.ActionQueue),
			},
		}

	case progress.ActionQueue:
		// Don't consume the message — let it queue normally in the gateway.
		// The gateway will process it after the current loop releases the lock.
		// Queued=true tells the gateway to send Reply as instant feedback.
		//
		// No corrections: the message is already in the gateway's lock queue
		// and will be processed when the current task finishes. Offering
		// corrections here would risk double-processing (once via correction,
		// once via the queue).
		return progress.InterruptResult{
			Handled: false,
			Queued:  true,
			Action:  progress.ActionQueue,
			Reply:   "📋 收到，当前任务完成后处理。",
		}

	default:
		return progress.InterruptResult{}
	}
}

// classifyMergeInjection determines the injection prefix based on signal
// strength. Three tiers ensure LLM treats the injected message with
// appropriate priority:
//
//   - Directive: negation detected → user wants to CHANGE the current approach
//   - Supplement: high confidence merge → user adds a clear requirement
//   - Note: medium confidence → informational, LLM may consider
//
// This is not a keyword hack — the tiers are determined by computable signals
// (negation structure + scheduler confidence), and the prefix wording exploits
// LLM instruction-following characteristics (stronger wording = higher compliance).
func classifyMergeInjection(text string, decision progress.ScheduleDecision, input progress.ScheduleInput) string {
	if input.Structure.HasNegation {
		// "不要 Python 改 C++"、"别用那个库"
		return "[用户要求修改——必须立即执行] " + text
	}
	if decision.Confidence >= 0.80 {
		// High-confidence merge = clear supplementary requirement.
		return "[用户补充需求——请在当前任务中纳入] " + text
	}
	// Medium confidence = possibly relevant information.
	return "[用户补充] " + text
}

// HandleCorrection executes a user-initiated correction of a previous
// scheduling decision. This is called when the user clicks a correction
// button (e.g. "改为打断" after a Merge decision).
//
// Parameters:
//   - userID: the user who clicked the correction
//   - messageText: the original message that was scheduled
//   - originalAction: the action that was originally taken
//   - correctionAction: the action the user wants instead
//
// Returns an InterruptResult describing what was done.
func (ih *imInterruptHandler) HandleCorrection(
	userID string,
	messageText string,
	originalAction progress.ScheduleAction,
	correctionAction progress.ScheduleAction,
) progress.InterruptResult {
	log.Printf("[correction] user=%s original=%s correction=%s msg_len=%d",
		userID, originalAction, correctionAction, len([]rune(messageText)))

	switch correctionAction {
	case progress.ActionReplace:
		// User wants to interrupt — cancel the current task.
		// If the original was Merge, try to retract the pending injection.
		// If already consumed, the message was processed — cancellation still
		// proceeds (user explicitly asked to interrupt).
		if originalAction == progress.ActionMerge {
			ih.handler.pendingInjection.LoadAndDelete(userID) // best-effort retract
		}
		taskText, err := ih.handler.CancelSessionForUser(userID)
		if err != nil {
			log.Printf("[correction] CancelSessionForUser error: %v", err)
			return progress.InterruptResult{Reply: "⚠️ 打断失败: " + err.Error()}
		}
		reply := "⏹️ 已改为打断"
		if taskText != "" {
			reply += "，已停止任务「" + truncateForLog(taskText, 20) + "」。"
		} else {
			reply += "。"
		}
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionReplace,
			Reply:   reply,
		}

	case progress.ActionQueue:
		// User wants to queue instead of merge — remove the pending injection
		// and let the message go through normal queuing.
		if originalAction == progress.ActionMerge {
			// Try to delete the injection. If it was already consumed by the
			// agent loop, the message has already been processed — don't
			// re-dispatch (that would cause double processing).
			if _, loaded := ih.handler.pendingInjection.LoadAndDelete(userID); !loaded {
				return progress.InterruptResult{
					Handled: true,
					Action:  progress.ActionQueue,
					Reply:   "⚠️ 消息已被当前任务处理，无法改为排队。",
				}
			}
		}
		return progress.InterruptResult{
			Handled: false,
			Queued:  true,
			Action:  progress.ActionQueue,
			Reply:   "📋 已改为排队，当前任务完成后处理。",
		}

	case progress.ActionMerge:
		// User wants to inject into current task instead of queuing.
		// Prefix included here — consumption side does NOT add another.
		injection := "[用户补充] " + messageText
		ih.handler.accumulateInjection(userID, injection)
		return progress.InterruptResult{
			Handled: true,
			Action:  progress.ActionMerge,
			Reply:   "👌 已改为注入当前任务。",
		}

	default:
		return progress.InterruptResult{
			Reply: "⚠️ 不支持的纠正操作。",
		}
	}
}
