package guiapp

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// prepareCodingPlanDialogue intercepts a coding turn when the user wants to
// plan before development, or is already in that conversation.
//
// A non-nil stop is planning only: the caller returns it and must not dispatch
// writers. rewritten replaces userText when the user leaves planning to execute
// the original goal. allow is false for sessions that must not plan, such as
// remote maintenance.
func (h *IMMessageHandler) prepareCodingPlanDialogue(
	userID, userText, projectPath string,
	allow bool,
	onProgress func(string),
	onToken func(string),
) (string, *IMAgentResponse) {
	userText = strings.TrimSpace(userText)
	if h == nil || userID == "" || userText == "" || !allow {
		return userText, nil
	}
	if strings.HasPrefix(userText, codingPlanApproveExecuteMarker) {
		return userText, nil
	}
	if codingRequestLooksExplicitWorkspaceClear(userText) {
		return userText, nil
	}

	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanReviewRunning && !h.codingPlanReviewIsLive(userID) {
		h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
			m.PlanReviewRunning = false
		})
		mem.PlanReviewRunning = false
	}
	reviewLive := h.codingPlanReviewIsLive(userID)
	_, hasPending := h.loadStickyPendingCodingPlan(userID)
	inDialogue := mem.PlanDialogue
	// A host continuation is not the user confirming the plan. Letting it
	// through clears the pending plan and starts writing files.
	if isGoalSystemContinuation(userText) || isSessionRoundContinuation(userText) {
		if reviewLive || inDialogue || hasPending {
			log.Printf("[coding-plan] continuation held user=%s", userID)
			held := "规划还没确认，这一轮先不改文件。"
			if codingPlanDialogueMostlyASCII(mem.PlanDialogueBrief + mem.PendingPlanUserText) {
				held = "The plan is not confirmed yet, so this turn will not change files."
			}
			return userText, h.codingPlanDialogueResponse(onToken, held, nil)
		}
		return userText, nil
	}

	if inDialogue || hasPending {
		switch {
		case codingPlanDialogueCancels(userText):
			h.supersedeCodingPlanReview(userID)
			h.exitCodingPlanDialogue(userID, true)
			log.Printf("[coding-plan] dialogue cancelled user=%s", userID)
			return userText, h.codingPlanDialogueResponse(onToken, "已退出规划模式，没有改文件。要做的时候直接说任务即可。", nil)
		case codingPlanDialogueDirect(userText):
			goal := codingPlanJoinGoalAndNotes(h.codingPlanDialogueGoal(userID, userText), mem.PlanDialogueNotes)
			h.supersedeCodingPlanReview(userID)
			h.clearStickyPendingCodingPlan(userID)
			h.clearCodingPlanDialogue(userID)
			h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
				m.SkipNextPlan = true
				m.SkipPlanText = goal
			})
			log.Printf("[coding-plan] dialogue skipped user=%s", userID)
			if onProgress != nil {
				onProgress("已跳过规划，按原目标直接执行")
			}
			return goal, nil
		case codingPlanDialogueConfirms(userText) && hasPending:
			h.supersedeCodingPlanReview(userID)
			if _, ok := h.promotePendingToApprovedCodingPlan(userID); ok {
				log.Printf("[coding-plan] dialogue confirmed user=%s", userID)
				if onProgress != nil {
					onProgress("已确认规划，开始实施")
				}
				return userText, nil
			}
		}
		// A follow-up while a plan is waiting is a change to that plan.
		// Letting it through rewrites the request ("改成图形界面版本") and
		// replaces the steps with a fresh two-step draft.
		if reviewLive {
			return userText, h.codingPlanReviewWait(onToken)
		}
		if hasPending {
			return userText, h.startCodingPlanReview(userID, projectPath, onToken, func(seq int64, tok func(string)) *IMAgentResponse {
				progress := onProgress
				if seq != 0 {
					progress = nil
				}
				return h.reviseStickyPendingCodingPlan(userID, userText, projectPath, seq, progress, tok)
			})
		}
	}
	if reviewLive {
		return userText, h.codingPlanReviewWait(onToken)
	}

	// Skip replays one exact request through the runner. That sentence often
	// still says "先规划". Only that replay stays out of planning; a different
	// message is a new turn.
	if mem.SkipNextPlan {
		skipped := strings.TrimSpace(mem.SkipPlanText)
		if skipped != "" && skipped == userText {
			return userText, nil
		}
		if skipped != "" {
			h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
				m.SkipNextPlan = false
				m.SkipPlanText = ""
			})
		}
	}

	if !inDialogue && !codingUserWantsPlanFirst(userText) {
		return userText, nil
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		if strings.TrimSpace(m.PlanDialogueBrief) == "" {
			m.PlanDialogueBrief = userText
		}
	})
	return userText, h.startCodingPlanReview(userID, projectPath, onToken, func(seq int64, tok func(string)) *IMAgentResponse {
		progress := onProgress
		if seq != 0 {
			progress = nil
		}
		return h.runCodingPlanDialogueTurn(userID, userText, projectPath, seq, progress, tok)
	})
}

func (h *IMMessageHandler) codingPlanDialogueGoal(userID, fallback string) string {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if s := strings.TrimSpace(mem.PlanDialogueBrief); s != "" {
		return s
	}
	if s := strings.TrimSpace(mem.PendingPlanUserText); s != "" {
		return s
	}
	return strings.TrimSpace(fallback)
}

func (h *IMMessageHandler) clearCodingPlanDialogue(userID string) {
	if h == nil {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.PlanDialogue = false
		mem.PlanDialogueBrief = ""
		mem.PlanDialogueNotes = ""
	})
}

func (h *IMMessageHandler) exitCodingPlanDialogue(userID string, clearPlan bool) {
	// Reject and cancel both come through here. Bump the review seq before
	// clearing, or a review that is still writing puts the plan back.
	h.supersedeCodingPlanReview(userID)
	if clearPlan {
		h.clearStickyPendingCodingPlan(userID)
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
	}
	h.clearCodingPlanDialogue(userID)
	h.emitCodingWorkbenchStepsUpdate(userID)
}

func (h *IMMessageHandler) supersedeCodingPlanReview(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	h.planReviewLive.Delete(userID)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq++
		m.PlanReviewRunning = false
	})
}

// codingPlanAwaitingUser is true while a plan is still being written or
// still waiting for the user. Goal and session continuations must not start
// the next coding turn in that window.
func (h *IMMessageHandler) codingPlanAwaitingUser(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	if h.codingPlanReviewIsLive(userID) {
		return true
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanDialogue {
		return true
	}
	_, pending := h.loadStickyPendingCodingPlan(userID)
	return pending
}

func (h *IMMessageHandler) codingPlanReviewIsLive(userID string) bool {
	if h == nil {
		return false
	}
	_, ok := h.planReviewLive.Load(userID)
	return ok
}

func (h *IMMessageHandler) codingPlanReviewWait(onToken func(string)) *IMAgentResponse {
	return h.codingPlanDialogueResponse(onToken, "规划还在后台评审，这句先不改方案。写完后可以提出异议或补充，也可以说取消规划。确认前不会改文件。", nil)
}

func (h *IMMessageHandler) beginCodingPlanReview(userID string) int64 {
	var seq int64
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq++
		m.PlanReviewRunning = true
		seq = m.PlanReviewSeq
		// Store before the lock drops. A stale-flag heal that runs in the gap
		// used to see "running" with no live entry and clear this review.
		h.planReviewLive.Store(userID, seq)
	})
	return seq
}

const codingPlanReviewSupersededText = "这轮评审已被更新的请求取代。"

// updateIfCodingPlanReview applies fn only while reviewSeq is still the
// current review. reviewSeq 0 is the inline path and always applies.
// The check and the write share the sticky lock, so a cancel cannot land
// between them.
func (h *IMMessageHandler) updateIfCodingPlanReview(userID string, reviewSeq int64, fn func(*stickyCodingWorkbenchMemory)) bool {
	if h == nil || fn == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	applied := false
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		if reviewSeq != 0 && m.PlanReviewSeq != reviewSeq {
			return
		}
		fn(m)
		applied = true
	})
	return applied
}

// startCodingPlanReview runs draft, self-review, and fix on the existing AI
// background task when that session manager is available. The chat only hears
// that the review started. Tests and hosts without a manager run it inline.
func (h *IMMessageHandler) startCodingPlanReview(userID, projectPath string, onToken func(string), work func(seq int64, onToken func(string)) *IMAgentResponse) *IMAgentResponse {
	if h == nil || h.manager == nil || h.manager.app == nil || work == nil {
		if work == nil {
			return nil
		}
		return work(0, onToken)
	}
	seq := h.beginCodingPlanReview(userID)
	session := h.manager.CreateAIBackgroundSession("规划评审", projectPath, nil)
	if session == nil {
		h.supersedeCodingPlanReview(userID)
		return work(0, onToken)
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[coding-plan] background review panic: %v", rec)
				h.failCodingPlanReview(userID, session.ID, seq, "规划评审中断了，方案没有定下来。可以再说一次要规划的内容。确认前不会改文件。")
			}
		}()
		resp := work(seq, nil)
		h.publishCodingPlanReview(userID, session.ID, seq, resp)
	}()
	note := "规划已转到后台评审：先写方案，自动评审并修改，通过后再交给你确认。确认前不会改文件。"
	return h.codingPlanDialogueResponse(onToken, note, nil)
}

func (h *IMMessageHandler) publishCodingPlanReview(userID, sessionID string, seq int64, resp *IMAgentResponse) {
	if h == nil || h.manager == nil {
		return
	}
	// Drop the running flag before the live entry. Clearing live first lets the
	// next message treat a finished review as a dead one and start another.
	kept := h.updateIfCodingPlanReview(userID, seq, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewRunning = false
	})
	if cur, ok := h.planReviewLive.Load(userID); ok && cur == seq {
		h.planReviewLive.Delete(userID)
	}
	if !kept || h.getStickyCodingWorkbenchMemory(userID).PlanReviewSeq != seq {
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionExited
			s.Summary.Status = SessionExited.String()
			s.Summary.WaitingForUser = false
			s.Summary.ProgressSummary = "评审已取消"
		})
		return
	}
	text := ""
	if resp != nil {
		text = strings.TrimSpace(resp.Text)
	}
	if h.getStickyCodingWorkbenchMemory(userID).PlanReviewSeq != seq {
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionExited
			s.Summary.Status = SessionExited.String()
			s.Summary.WaitingForUser = false
			s.Summary.ProgressSummary = "评审已取消"
		})
		return
	}
	progress := "规划需要补充后再确认"
	if _, pending := h.loadStickyPendingCodingPlan(userID); pending {
		progress = "规划已自评，等待确认"
	}
	if text != "" {
		h.manager.AppendBackgroundAIOutput(sessionID, text)
		// The turn that started the review already returned the short ack.
		// This notice is the plan itself, in the same chat.
		emitCodingPlanReviewChat(h, userID, text)
	}
	h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
		s.Status = SessionWaitingInput
		s.Summary.Status = SessionWaitingInput.String()
		s.Summary.WaitingForUser = true
		s.Summary.ProgressSummary = progress
		s.Summary.LastResult = text
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

// failCodingPlanReview closes a review that did not finish. The chat hears
// that, and the background session does not stay busy.
func (h *IMMessageHandler) failCodingPlanReview(userID, sessionID string, seq int64, text string) {
	if h == nil {
		return
	}
	kept := h.updateIfCodingPlanReview(userID, seq, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewRunning = false
	})
	if cur, ok := h.planReviewLive.Load(userID); ok && cur == seq {
		h.planReviewLive.Delete(userID)
	}
	if !kept || (seq != 0 && h.getStickyCodingWorkbenchMemory(userID).PlanReviewSeq != seq) {
		if h.manager != nil && strings.TrimSpace(sessionID) != "" {
			h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
				s.Status = SessionExited
				s.Summary.Status = SessionExited.String()
				s.Summary.WaitingForUser = false
				s.Summary.ProgressSummary = "评审已取消"
			})
		}
		return
	}
	if h.manager != nil && strings.TrimSpace(sessionID) != "" {
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionExited
			s.Summary.Status = SessionExited.String()
			s.Summary.WaitingForUser = false
			s.Summary.ProgressSummary = "评审中断"
		})
	}
	emitCodingPlanReviewChat(h, userID, text)
}

// emitCodingPlanReviewChat delivers a finished review to the coding chat.
// The background session still keeps a copy; the chat is where the user confirms.
var emitCodingPlanReviewChat = func(h *IMMessageHandler, userID, text string) {
	if h == nil || h.app == nil {
		return
	}
	text = strings.TrimSpace(text)
	userID = strings.TrimSpace(userID)
	if text == "" || userID == "" {
		return
	}
	h.app.emitAIContinuationEvent(AIContinuationEvent{
		RequestID:  "coding-plan-review-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		SessionKey: userID,
		Kind:       "notice",
		Text:       text,
	})
}

const codingPlanSelfReviewSystemPrompt = `You review one coding plan before the user sees it. Return JSON only.

Schema: {"ok":true} when the plan is specific enough to build.
Or {"ok":false,"issues":[{"op":"insert|update|delete","target":"an existing step title","change":"what to add or how to change it"}]}

Judge the plan, not isolated words. Write change in the same language as the goal.
Delete only a step the plan itself makes redundant. Do not drop a step the goal still needs.
Do not claim files were changed. No markdown.`

func (h *IMMessageHandler) selfReviewCodingPlan(goal, projectPath string, tasks []*v2.TaskItem) ([]*v2.TaskItem, string) {
	raw := h.callCodingPlanSelfReview(goal, projectPath, tasks)
	okReview, issues, parsed := parseCodingPlanSelfReview(raw)
	if !parsed || okReview || len(issues) == 0 {
		return tasks, ""
	}
	next := cloneCodingWorkbenchTasks(tasks)
	rejected := map[string]bool{}
	for _, issue := range issues {
		updated, dropped, applied := applyCodingPlanReviewIssue(next, issue)
		if !applied || !codingPlanKeepsTitlesExcept(tasks, updated, rejected, dropped) {
			continue
		}
		if dropped != "" {
			rejected[codingPlanDialogueCompact(dropped)] = true
		}
		next = updated
	}
	if codingPlanTasksSame(tasks, next) {
		return tasks, ""
	}
	return next, "自动评审改过这版。"
}

func (h *IMMessageHandler) callCodingPlanSelfReview(goal, projectPath string, tasks []*v2.TaskItem) string {
	if h == nil || len(tasks) == 0 {
		return ""
	}
	cfg := h.getCodingLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getCodingLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Goal:\n")
	b.WriteString(truncateRunesV2(goal, 2000))
	b.WriteString("\n\nPlan:\n")
	for _, task := range tasks {
		if task == nil {
			continue
		}
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(task.Title))
		if body := codingPlanStepBody(task.Description); body != "" {
			b.WriteString(": ")
			b.WriteString(truncateRunesV2(body, 240))
		}
		b.WriteString("\n")
	}
	if p := strings.TrimSpace(projectPath); p != "" {
		b.WriteString("\nProject path: ")
		b.WriteString(p)
	}
	return h.callLightweightLLMOnce(cfg, codingPlanSelfReviewSystemPrompt, b.String(), 20)
}

func parseCodingPlanSelfReview(raw string) (okReview bool, issues []codingPlanReviewIssue, parsed bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil, false
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var payload struct {
		OK     bool                    `json:"ok"`
		Issues []codingPlanReviewIssue `json:"issues"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, nil, false
	}
	return payload.OK, payload.Issues, true
}

type codingPlanReviewIssue struct {
	Op     string `json:"op"`
	Target string `json:"target"`
	Change string `json:"change"`
}

func applyCodingPlanReviewIssue(tasks []*v2.TaskItem, issue codingPlanReviewIssue) ([]*v2.TaskItem, string, bool) {
	op := strings.ToLower(strings.TrimSpace(issue.Op))
	switch op {
	case "delete":
		idx := codingPlanStepByTarget(tasks, issue.Target)
		if idx < 0 || tasks[idx] == nil {
			return nil, "", false
		}
		title := strings.TrimSpace(tasks[idx].Title)
		next, _, ok := applyCodingPlanOp(tasks, "delete", idx+1, nil)
		return next, title, ok
	case "update":
		idx := codingPlanStepByTarget(tasks, issue.Target)
		change := strings.TrimSpace(issue.Change)
		if idx < 0 || tasks[idx] == nil || change == "" {
			return nil, "", false
		}
		body := strings.TrimSpace(codingPlanStepBody(tasks[idx].Description) + "\n" + change)
		next, _, ok := applyCodingPlanOp(tasks, "update", idx+1, &v2.TaskItem{
			Title:       tasks[idx].Title,
			Description: body,
		})
		return next, "", ok
	case "insert":
		change := strings.TrimSpace(issue.Change)
		if change == "" {
			return nil, "", false
		}
		index := len(tasks) + 1
		if idx := codingPlanStepByTarget(tasks, issue.Target); idx >= 0 {
			index = idx + 1
		}
		next, _, ok := applyCodingPlanOp(tasks, "insert", index, &v2.TaskItem{
			Title:       truncateRunesV2(change, 24),
			Description: change,
		})
		return next, "", ok
	default:
		return nil, "", false
	}
}

func codingPlanStepByTarget(tasks []*v2.TaskItem, target string) int {
	want := codingPlanDialogueCompact(target)
	if want == "" {
		return -1
	}
	exact := -1
	best := -1
	bestLen := 0
	for i, task := range tasks {
		if task == nil {
			continue
		}
		title := codingPlanDialogueCompact(task.Title)
		if title == "" {
			continue
		}
		if title == want {
			exact = i
			break
		}
		if !strings.Contains(title, want) && !(utf8.RuneCountInString(title) >= 4 && strings.Contains(want, title)) {
			continue
		}
		n := utf8.RuneCountInString(title)
		if n > bestLen {
			best = i
			bestLen = n
		}
	}
	if exact >= 0 {
		return exact
	}
	return best
}

func codingPlanKeepsTitlesExcept(original, proposed []*v2.TaskItem, alreadyDropped map[string]bool, dropped string) bool {
	allowed := map[string]bool{}
	for title, ok := range alreadyDropped {
		if ok {
			allowed[title] = true
		}
	}
	if dropped != "" {
		allowed[codingPlanDialogueCompact(dropped)] = true
	}
	for _, task := range original {
		if task == nil {
			continue
		}
		title := strings.TrimSpace(task.Title)
		key := codingPlanDialogueCompact(title)
		if title == "" || allowed[key] {
			continue
		}
		if !codingPlanProposedKeepsTitle(proposed, title) {
			return false
		}
	}
	return len(proposed) >= codingWorkbenchPlanMinTasks || len(original) < codingWorkbenchPlanMinTasks
}

func (h *IMMessageHandler) runCodingPlanDialogueTurn(userID, userText, projectPath string, reviewSeq int64, onProgress func(string), onToken func(string)) *IMAgentResponse {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	brief := strings.TrimSpace(mem.PlanDialogueBrief)
	if brief == "" {
		brief = strings.TrimSpace(userText)
	}
	// A review cancelled while this turn was starting must not wipe the plan
	// that replaced it. Clear and reopen only while this seq is still current.
	entered := false
	if !h.updateIfCodingPlanReview(userID, reviewSeq, func(m *stickyCodingWorkbenchMemory) {
		if !m.PlanDialogue {
			entered = true
			m.PendingPlanJSON = ""
			m.PendingPlanMarkdown = ""
			m.PendingPlanUserText = ""
			m.ExecutionPlan = ""
			m.StepStatuses = nil
			m.PlanRunStarted = false
		}
		m.PlanDialogue = true
		if strings.TrimSpace(m.PlanDialogueBrief) == "" {
			m.PlanDialogueBrief = brief
		}
	}) {
		return h.codingPlanDialogueResponse(onToken, codingPlanReviewSupersededText, nil)
	}
	if entered {
		h.emitCodingWorkbenchStepsUpdate(userID)
	}
	if onProgress != nil {
		onProgress("规划模式：先和你确认方案，确认前不会改文件")
	}

	pending, _ := h.loadStickyPendingCodingPlan(userID)
	if len(pending.Tasks) >= codingWorkbenchPlanMinTasks && strings.TrimSpace(userText) != brief {
		return h.reviseStickyPendingCodingPlan(userID, userText, projectPath, reviewSeq, onProgress, onToken)
	}
	tasks, reply, asked := h.draftCodingPlanDialogue(userID, brief, userText, projectPath)
	if asked || len(tasks) < codingWorkbenchPlanMinTasks {
		note := strings.TrimSpace(reply)
		if note == "" {
			note = "想先规划再开发。请补充要做的功能、做到什么程度，以及有没有不能改的部分。"
			if codingPlanDialogueMostlyASCII(userText) {
				note = "This is a plan-first request. Say what to build, how far it should go, and what must not change."
			}
		}
		if !h.updateIfCodingPlanReview(userID, reviewSeq, func(m *stickyCodingWorkbenchMemory) {
			m.RequirementRestatement = truncateRunesForSubAgent(note, 400)
		}) {
			return h.codingPlanDialogueResponse(onToken, codingPlanReviewSupersededText, nil)
		}
		h.emitCodingWorkbenchStepsUpdate(userID)
		log.Printf("[coding-plan] dialogue asking user=%s", userID)
		return h.codingPlanDialogueResponse(onToken, note+"\n\n"+codingPlanDialogueFooter(userText), nil)
	}
	if strings.TrimSpace(reply) == "" {
		reply = "这是按当前目标起草的规划。可以直接补充修改，确认前不会改文件。"
	}
	goal := codingPlanDialogueSubstance(brief, userText)
	if goal != "" && goal != brief {
		if !h.updateIfCodingPlanReview(userID, reviewSeq, func(m *stickyCodingWorkbenchMemory) {
			m.PlanDialogueBrief = goal
		}) {
			return h.codingPlanDialogueResponse(onToken, codingPlanReviewSupersededText, nil)
		}
	}
	tasks, reviewNote := h.selfReviewCodingPlan(goal, projectPath, tasks)
	if reviewNote != "" {
		reply = strings.TrimSpace(reviewNote + "\n" + reply)
	}
	log.Printf("[coding-plan] dialogue proposed user=%s steps=%d", userID, len(tasks))
	return h.commitCodingPlanDraft(userID, goal, tasks, reply, reviewSeq, onToken)
}

// codingPlanDialogueSubstance is the goal worth planning. A bare "先规划"
// stays the brief until the user says what to build; that later sentence
// replaces it, so confirm and direct-execute do not run the filler.
func codingPlanDialogueSubstance(brief, latest string) string {
	brief = strings.TrimSpace(brief)
	latest = strings.TrimSpace(latest)
	switch {
	case latest == "" || latest == brief:
		return brief
	case brief == "" || codingPlanDialogueTooThin(brief):
		return latest
	case codingPlanDialogueTooThin(latest):
		return brief
	case strings.Contains(latest, brief):
		return latest
	default:
		return brief + "\n" + latest
	}
}

func codingPlanTasksSame(a, b []*v2.TaskItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		at, bt := a[i], b[i]
		if at == nil || bt == nil {
			if at != bt {
				return false
			}
			continue
		}
		if strings.TrimSpace(at.Title) != strings.TrimSpace(bt.Title) {
			return false
		}
		if codingPlanStepBody(at.Description) != codingPlanStepBody(bt.Description) {
			return false
		}
	}
	return true
}

func (h *IMMessageHandler) rememberCodingPlanRevision(userID, feedback string, reviewSeq int64) {
	feedback = strings.TrimSpace(feedback)
	if h == nil || userID == "" || feedback == "" {
		return
	}
	feedback = truncateRunesV2(feedback, 300)
	key := codingPlanDialogueCompact(feedback)
	h.updateIfCodingPlanReview(userID, reviewSeq, func(m *stickyCodingWorkbenchMemory) {
		var kept []string
		for _, line := range strings.Split(m.PlanDialogueNotes, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if codingPlanDialogueCompact(line) == key {
				return
			}
			kept = append(kept, line)
		}
		kept = append(kept, feedback)
		if len(kept) > 8 {
			kept = kept[len(kept)-8:]
		}
		m.PlanDialogueNotes = strings.Join(kept, "\n")
	})
}

func codingPlanJoinGoalAndNotes(goal, notes string) string {
	goal = strings.TrimSpace(goal)
	goalKey := codingPlanDialogueCompact(goal)
	var lines []string
	seen := map[string]bool{}
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		key := codingPlanDialogueCompact(line)
		if key == "" || seen[key] {
			continue
		}
		if goalKey != "" && strings.Contains(goalKey, key) {
			continue
		}
		seen[key] = true
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return goal
	}
	if goal == "" {
		return strings.Join(lines, "\n")
	}
	return goal + "\n\n补充要求：\n" + strings.Join(lines, "\n")
}

const codingPlanRevisionSystemPrompt = `You edit one existing coding plan. The user is changing that plan before any file is written. Return JSON only.

Prefer a patch: {"action":"patch","op":"insert|update|delete","index":1,"title":"...","description":"...","reply":"..."}
index is 1-based. Use 0 when the host should pick the position.
insert adds one step. update rewrites that step. delete removes that step.
Do not renumber the other steps and do not drop a step the user did not reject.

A full replacement is allowed only when every current step title remains: {"action":"propose","reply":"...","steps":[{"title":"...","description":"...","files":["relative/path"]}]}
reply names what changed, in the user's language. Do not claim files were changed.`

// reviseStickyPendingCodingPlan folds a follow-up into the plan that is
// already waiting for confirmation. The original goal stays the goal.
func (h *IMMessageHandler) reviseStickyPendingCodingPlan(userID, userText, projectPath string, reviewSeq int64, onProgress func(string), onToken func(string)) *IMAgentResponse {
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) < codingWorkbenchPlanMinTasks {
		return h.runCodingPlanDialogueTurn(userID, userText, projectPath, reviewSeq, onProgress, onToken)
	}
	brief := strings.TrimSpace(pending.UserText)
	if brief == "" {
		brief = h.codingPlanDialogueGoal(userID, userText)
	}
	if !h.updateIfCodingPlanReview(userID, reviewSeq, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		if strings.TrimSpace(m.PlanDialogueBrief) == "" {
			m.PlanDialogueBrief = brief
		}
	}) {
		return h.codingPlanDialogueResponse(onToken, codingPlanReviewSupersededText, nil)
	}
	if onProgress != nil {
		onProgress("规划模式：正在把你的补充写进当前方案，确认前不会改文件")
	}

	tasks, reply := h.draftCodingPlanRevision(brief, userText, projectPath, pending)
	if len(tasks) < codingWorkbenchPlanMinTasks {
		tasks, reply = patchCodingPlan(pending.Tasks, userText)
	}
	tasks, reviewNote := h.selfReviewCodingPlan(brief, projectPath, tasks)
	if reviewNote != "" {
		reply = strings.TrimSpace(reviewNote + "\n" + reply)
	}
	if !codingPlanTasksSame(pending.Tasks, tasks) {
		h.rememberCodingPlanRevision(userID, userText, reviewSeq)
	}
	log.Printf("[coding-plan] dialogue revised user=%s steps=%d", userID, len(tasks))
	return h.commitCodingPlanDraft(userID, brief, tasks, reply, reviewSeq, onToken)
}

// commitCodingPlanDraft stores a plan and shows it for confirmation. Step
// descriptions stay the step itself: the execution planner's "overall request"
// footer would copy the whole prompt into every step and make the next edit
// look like a new task.
func (h *IMMessageHandler) commitCodingPlanDraft(userID, brief string, tasks []*v2.TaskItem, note string, reviewSeq int64, onToken func(string)) *IMAgentResponse {
	tasks = finalizeCodingWorkbenchTasks(tasks, brief)
	for _, task := range tasks {
		if task != nil {
			task.Description = codingPlanStepBody(task.Description)
		}
	}
	if len(tasks) > codingWorkbenchPlanMaxTasks {
		tasks = tasks[:codingWorkbenchPlanMaxTasks]
	}
	if len(tasks) == 0 {
		return h.codingPlanDialogueResponse(onToken, "这轮评审没有形成可确认的步骤。", nil)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		note = "规划已更新。确认前不会改文件。"
	}
	markdown := formatCodingWorkbenchPlanMarkdown(brief, tasks)
	steps := codingWorkbenchStepsFromTasks(tasks, codingStepPending)
	if len(steps) > 0 {
		projected := make([]codingWorkbenchStepStatus, len(steps))
		copy(projected, steps)
		for i := range projected {
			projected[i].Summary = codingStepChecklistNote(projected[i].Summary)
		}
		steps = projected
	}
	pending := codingWorkbenchPendingPlan{
		UserText:  strings.TrimSpace(brief),
		Markdown:  strings.TrimSpace(markdown),
		Tasks:     cloneCodingWorkbenchTasks(tasks),
		CreatedAt: time.Now().Unix(),
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return h.codingPlanDialogueResponse(onToken, "这轮评审没有写成可确认的方案。", nil)
	}
	// The seq check and the write share one update. A cancel that lands between
	// them used to clear the plan and then let this commit put it back.
	stored := false
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		if reviewSeq != 0 && m.PlanReviewSeq != reviewSeq {
			return
		}
		m.PendingPlanJSON = string(raw)
		m.PendingPlanMarkdown = pending.Markdown
		m.PendingPlanUserText = pending.UserText
		if pending.Markdown != "" {
			m.ExecutionPlan = truncateRunesForSubAgent(pending.Markdown, codingWorkbenchExecutionPlanPersistRunes)
		}
		m.StepStatuses = steps
		m.RequirementRestatement = truncateRunesForSubAgent(note, 400)
		stored = true
	})
	if !stored {
		return h.codingPlanDialogueResponse(onToken, codingPlanReviewSupersededText, nil)
	}
	h.emitCodingWorkbenchStepsUpdate(userID)
	body := note + "\n\n" + formatPendingPlanApprovalText(markdown, len(tasks)) + "\n\n" + codingPlanDialogueFooter(brief)
	return h.codingPlanDialogueResponse(onToken, body, codingPlanApproveActions())
}

func (h *IMMessageHandler) draftCodingPlanRevision(brief, latest, projectPath string, pending codingWorkbenchPendingPlan) ([]*v2.TaskItem, string) {
	if tasks := extractUserProvidedCodingPlan(latest); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks, "已换成你写出的步骤。"
	}
	if !codingPlanFeedbackIsEdit(pending.Tasks, latest) {
		return cloneCodingWorkbenchTasks(pending.Tasks), "没有改方案。要调整的话直接说加上、改成或去掉哪一步。"
	}
	// A short add, refine, or delete is a local patch. Waiting on a model here
	// is what turned "需要有图形界面" into a replacement plan.
	if codingPlanHostPatchConfident(pending.Tasks, latest) {
		return patchCodingPlan(pending.Tasks, latest)
	}
	if raw := h.callCodingPlanRevisionModel(brief, latest, projectPath, pending.Markdown); raw != "" {
		if op, index, patch, reply, ok := parseCodingPlanPatch(raw); ok && codingPlanModelOpAgrees(pending.Tasks, op, index, patch, latest) {
			index = codingPlanResolveOpIndex(pending.Tasks, op, index, latest)
			if tasks, summary, applied := applyCodingPlanOp(pending.Tasks, op, index, patch); applied {
				if strings.TrimSpace(reply) != "" {
					summary = reply
				}
				return tasks, summary
			}
		}
		if tasks, reply, asked, ok := parseCodingPlanDialogue(raw); ok && !asked && len(tasks) >= codingWorkbenchPlanMinTasks {
			if !codingPlanRevisionDropsSteps(pending.Tasks, tasks, latest) {
				if strings.TrimSpace(reply) == "" {
					reply = "已按你的补充更新规划。"
				}
				return tasks, reply
			}
			log.Printf("[coding-plan] rejected revision that dropped current steps")
		}
	}
	return patchCodingPlan(pending.Tasks, latest)
}

// codingPlanHostPatchConfident reports an edit the local patch can apply
// without a model: a named step to change or remove, or one short new step.
func codingPlanHostPatchConfident(tasks []*v2.TaskItem, feedback string) bool {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" || utf8.RuneCountInString(feedback) > 48 {
		return false
	}
	return codingPlanFeedbackIsEdit(tasks, feedback)
}

// codingPlanFeedbackIsEdit is a change to the current steps. A question about
// the plan is not a new step, and neither is a short remark that names nothing.
func codingPlanFeedbackIsEdit(tasks []*v2.TaskItem, feedback string) bool {
	if len(extractUserProvidedCodingPlan(feedback)) >= codingWorkbenchPlanMinTasks {
		return true
	}
	if codingPlanMatchingStep(tasks, feedback) >= 0 || codingPlanFeedbackHasChangeCue(feedback) {
		return true
	}
	if codingPlanFeedbackIsQuestion(feedback) {
		return false
	}
	return utf8.RuneCountInString(codingPlanFeedbackFocus(feedback)) >= 4
}

// codingPlanModelOpAgrees rejects a model patch that does not match the
// user's sentence. A delete is accepted only for a removal of the named step.
func codingPlanModelOpAgrees(tasks []*v2.TaskItem, op string, index int, patch *v2.TaskItem, feedback string) bool {
	focus := codingPlanFeedbackFocus(feedback)
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "delete":
		if !codingPlanFeedbackRemoves(feedback) {
			return false
		}
		idx := codingPlanResolveOpIndex(tasks, op, index, feedback) - 1
		return idx >= 0 && idx < len(tasks) && codingPlanStepMatchesFocus(tasks[idx], focus)
	case "update":
		if patch == nil {
			return false
		}
		blob := codingPlanDialogueCompact(patch.Title + patch.Description)
		return focus != "" && (strings.Contains(blob, focus) || strings.Contains(codingPlanDialogueCompact(feedback), codingPlanDialogueCompact(patch.Title)))
	case "insert":
		if codingPlanFeedbackRemoves(feedback) || patch == nil || codingPlanMatchingStep(tasks, feedback) >= 0 {
			return false
		}
		title := codingPlanDialogueCompact(patch.Title)
		blob := codingPlanDialogueCompact(patch.Title + patch.Description)
		if focus == "" {
			return title != ""
		}
		return strings.Contains(blob, focus) || (title != "" && strings.Contains(focus, title))
	default:
		return false
	}
}

func codingPlanResolveOpIndex(tasks []*v2.TaskItem, op string, index int, feedback string) int {
	if index > 0 || (op != "delete" && op != "update") {
		return index
	}
	if idx := codingPlanMatchingStep(tasks, feedback); idx >= 0 {
		return idx + 1
	}
	return index
}

// codingPlanRevisionDropsSteps reports a replacement that lost a current step
// the user did not ask to remove. Mentioning the new request is not enough.
func codingPlanRevisionDropsSteps(current, proposed []*v2.TaskItem, feedback string) bool {
	if len(proposed) == 0 {
		return true
	}
	removeFocus := ""
	if codingPlanFeedbackRemoves(feedback) {
		removeFocus = codingPlanFeedbackFocus(feedback)
	}
	for _, task := range current {
		if task == nil {
			continue
		}
		title := strings.TrimSpace(task.Title)
		if title == "" {
			continue
		}
		if removeFocus != "" && strings.Contains(codingPlanDialogueCompact(title), removeFocus) {
			continue
		}
		if !codingPlanProposedKeepsTitle(proposed, title) {
			return true
		}
	}
	return false
}

func codingPlanProposedKeepsTitle(proposed []*v2.TaskItem, title string) bool {
	want := codingPlanDialogueCompact(title)
	if want == "" {
		return true
	}
	for _, task := range proposed {
		if task == nil {
			continue
		}
		blob := codingPlanDialogueCompact(task.Title)
		if strings.Contains(blob, want) {
			return true
		}
	}
	return false
}

// patchCodingPlan applies one user message as an edit of the current steps.
// A removal deletes the named step. A refinement rewrites the step it names.
// Anything else is inserted once, ahead of the closing verify step.
func patchCodingPlan(tasks []*v2.TaskItem, feedback string) ([]*v2.TaskItem, string) {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" || len(tasks) == 0 {
		return cloneCodingWorkbenchTasks(tasks), "规划没有变化。"
	}
	if codingPlanFeedbackRemoves(feedback) {
		if idx := codingPlanMatchingStep(tasks, feedback); idx >= 0 {
			title := strings.TrimSpace(tasks[idx].Title)
			if next, summary, ok := applyCodingPlanOp(tasks, "delete", idx+1, nil); ok {
				return next, summary
			}
			next, _, ok := applyCodingPlanOp(tasks, "update", idx+1, &v2.TaskItem{
				Title:       title,
				Description: codingPlanStepBody(tasks[idx].Description) + "\n本步取消：" + feedback,
			})
			if !ok {
				return cloneCodingWorkbenchTasks(tasks), "步骤不能再少，也没能改掉「" + title + "」。"
			}
			return next, "步骤不能再少，已在「" + title + "」标成取消。"
		}
		if codingPlanFeedbackHardRemove(feedback) {
			name := codingPlanFeedbackFocus(feedback)
			if name == "" {
				name = feedback
			}
			return cloneCodingWorkbenchTasks(tasks), "当前步骤里没有和「" + name + "」对应的一步，没有删步骤。"
		}
	}
	if idx := codingPlanMatchingStep(tasks, feedback); idx >= 0 {
		title := strings.TrimSpace(tasks[idx].Title)
		body := codingPlanStepBody(tasks[idx].Description)
		if strings.Contains(body, feedback) {
			return cloneCodingWorkbenchTasks(tasks), "「" + title + "」里已经有这项。"
		}
		next, summary, ok := applyCodingPlanOp(tasks, "update", idx+1, &v2.TaskItem{
			Title:       title,
			Description: strings.TrimSpace(body + "\n" + feedback),
		})
		if ok {
			return next, summary
		}
	}
	step := &v2.TaskItem{
		Title:       codingPlanChangeTitle(feedback),
		Description: feedback,
	}
	if next, summary, ok := applyCodingPlanOp(tasks, "insert", 0, step); ok {
		return next, summary
	}
	return codingPlanFoldFeedback(tasks, feedback)
}

// codingPlanFoldFeedback keeps a change when the plan is already at the step
// cap, by writing it into the last implementation step.
func codingPlanFoldFeedback(tasks []*v2.TaskItem, feedback string) ([]*v2.TaskItem, string) {
	idx := codingPlanInsertAt(tasks) - 1
	if idx < 0 {
		idx = len(tasks) - 1
	}
	if idx < 0 || idx >= len(tasks) || tasks[idx] == nil {
		return cloneCodingWorkbenchTasks(tasks), "规划已有 " + strconv.Itoa(len(tasks)) + " 步，这条补充没有写进去。"
	}
	title := strings.TrimSpace(tasks[idx].Title)
	body := codingPlanStepBody(tasks[idx].Description)
	if strings.Contains(body, feedback) {
		return cloneCodingWorkbenchTasks(tasks), "「" + title + "」里已经有这项。"
	}
	next, _, ok := applyCodingPlanOp(tasks, "update", idx+1, &v2.TaskItem{
		Title:       title,
		Description: strings.TrimSpace(body + "\n" + feedback),
	})
	if !ok {
		return cloneCodingWorkbenchTasks(tasks), "规划已有 " + strconv.Itoa(len(tasks)) + " 步，这条补充没有写进去。"
	}
	return next, "规划已有 " + strconv.Itoa(len(tasks)) + " 步，已把补充写进「" + title + "」。"
}

// applyCodingPlanOp is the only way a plan grows, shrinks, or changes a step.
// index is 1-based. 0 lets insert choose the slot ahead of trailing verify steps.
func applyCodingPlanOp(tasks []*v2.TaskItem, op string, index int, patch *v2.TaskItem) ([]*v2.TaskItem, string, bool) {
	cloned := cloneCodingWorkbenchTasks(tasks)
	for _, task := range cloned {
		if task == nil {
			continue
		}
		task.Description = codingPlanStepBody(task.Description)
		task.DependsOn = nil
	}
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "delete":
		idx := index - 1
		if idx < 0 || idx >= len(cloned) || cloned[idx] == nil {
			return nil, "", false
		}
		if len(cloned) <= codingWorkbenchPlanMinTasks {
			return nil, "", false
		}
		title := strings.TrimSpace(cloned[idx].Title)
		cloned = append(cloned[:idx], cloned[idx+1:]...)
		reindexCodingPlan(cloned)
		return cloned, "已去掉「" + title + "」。", true
	case "update":
		idx := index - 1
		if idx < 0 || idx >= len(cloned) || cloned[idx] == nil || patch == nil {
			return nil, "", false
		}
		if title := strings.TrimSpace(patch.Title); title != "" && codingPlanTitleKeepsIdentity(cloned[idx].Title, title) {
			cloned[idx].Title = title
		}
		if desc := strings.TrimSpace(patch.Description); desc != "" {
			cloned[idx].Description = desc
		}
		if len(patch.Files) > 0 {
			cloned[idx].Files = append([]string(nil), patch.Files...)
		}
		reindexCodingPlan(cloned)
		return cloned, "已更新「" + strings.TrimSpace(cloned[idx].Title) + "」。", true
	case "insert":
		if patch == nil || (strings.TrimSpace(patch.Title) == "" && strings.TrimSpace(patch.Description) == "") {
			return nil, "", false
		}
		if len(cloned) >= codingWorkbenchPlanMaxTasks {
			return nil, "", false
		}
		if strings.TrimSpace(patch.Title) == "" {
			patch.Title = truncateRunesV2(patch.Description, 24)
		}
		idx := index - 1
		if idx < 0 || idx > len(cloned) {
			idx = codingPlanInsertAt(cloned)
		}
		cloned = append(cloned, nil)
		copy(cloned[idx+1:], cloned[idx:])
		cloned[idx] = &v2.TaskItem{
			Title:       strings.TrimSpace(patch.Title),
			Description: strings.TrimSpace(patch.Description),
			Files:       append([]string(nil), patch.Files...),
		}
		reindexCodingPlan(cloned)
		return cloned, "已加入「" + strings.TrimSpace(patch.Title) + "」。", true
	default:
		return nil, "", false
	}
}

func reindexCodingPlan(tasks []*v2.TaskItem) {
	for i, task := range tasks {
		if task != nil {
			task.Index = i + 1
			task.DependsOn = nil
		}
	}
}

func codingPlanInsertAt(tasks []*v2.TaskItem) int {
	i := len(tasks)
	for i > 0 && codingPlanStepLooksLikeVerify(tasks[i-1]) {
		i--
	}
	if i == 0 {
		return len(tasks)
	}
	return i
}

func codingPlanMatchingStep(tasks []*v2.TaskItem, feedback string) int {
	focus := codingPlanFeedbackFocus(feedback)
	if utf8.RuneCountInString(focus) < 2 {
		return -1
	}
	best := -1
	bestLen := 0
	for i, task := range tasks {
		if task == nil || !codingPlanStepMatchesFocus(task, focus) {
			continue
		}
		n := utf8.RuneCountInString(codingPlanDialogueCompact(task.Title))
		if n > bestLen {
			best = i
			bestLen = n
		}
	}
	if best >= 0 {
		return best
	}
	return codingPlanSegmentStep(tasks, focus)
}

// codingPlanSegmentStep matches "验证要覆盖表单提交失败" to a step titled
// "构建并验证": the sentence starts with one piece of that title. The piece
// has to be unique, so "结束时保存分数" does not attach to "自撞结束".
func codingPlanSegmentStep(tasks []*v2.TaskItem, focus string) int {
	if utf8.RuneCountInString(focus) < 4 {
		return -1
	}
	best := -1
	bestLen := 0
	tied := false
	for i, task := range tasks {
		if task == nil {
			continue
		}
		for _, seg := range codingPlanTitleSegments(task.Title) {
			// "build" is a prefix of "build the installer" once spaces are
			// removed. A short Latin fragment must not capture that sentence.
			if codingPlanSegmentIsLatin(seg) && utf8.RuneCountInString(seg) < 6 {
				continue
			}
			if !strings.HasPrefix(focus, seg) {
				continue
			}
			n := utf8.RuneCountInString(seg)
			switch {
			case n > bestLen:
				best = i
				bestLen = n
				tied = false
			case n == bestLen && i != best:
				tied = true
			}
		}
	}
	if tied {
		return -1
	}
	return best
}

func codingPlanTitleSegments(title string) []string {
	parts := strings.FieldsFunc(title, func(r rune) bool {
		switch r {
		case '、', '，', ',', '。', '和', '与', '并', '及', '/', '|':
			return true
		default:
			return unicode.IsSpace(r)
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = codingPlanDialogueCompact(part)
		if utf8.RuneCountInString(part) >= 2 {
			out = append(out, part)
		}
	}
	return out
}

func codingPlanSegmentIsLatin(seg string) bool {
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r > 127 {
			return false
		}
	}
	return true
}

// codingPlanStepMatchesFocus matches the step title only. Descriptions repeat
// the original goal, and a goal that mentions "测试" would capture every later
// edit about tests.
func codingPlanStepMatchesFocus(task *v2.TaskItem, focus string) bool {
	if task == nil || focus == "" {
		return false
	}
	title := codingPlanDialogueCompact(task.Title)
	if title == "" {
		return false
	}
	if strings.Contains(title, focus) {
		return true
	}
	// "图形界面用 SDL" and "试一下图形界面" both refine a step titled 图形界面.
	// A two-character title such as "实现" must not capture a longer new request.
	if utf8.RuneCountInString(title) < 4 || !strings.Contains(focus, title) {
		return false
	}
	extra := utf8.RuneCountInString(focus) - utf8.RuneCountInString(title)
	return extra >= 0 && extra <= 8
}

func codingPlanTitleKeepsIdentity(current, next string) bool {
	oldTitle := codingPlanDialogueCompact(current)
	newTitle := codingPlanDialogueCompact(next)
	if oldTitle == "" || newTitle == "" {
		return true
	}
	return strings.Contains(newTitle, oldTitle) || strings.Contains(oldTitle, newTitle)
}

func codingPlanChangeTitle(feedback string) string {
	text := strings.TrimSpace(codingPlanStripEnglishCues(feedback))
	for _, marker := range []string{"不要", "去掉", "删除", "取消", "不用", "需要有", "需要", "加上", "增加", "改成", "改为", "添加", "帮我", "一下"} {
		text = strings.ReplaceAll(text, marker, "")
	}
	text = strings.TrimSpace(text)
	for {
		trimmed := strings.TrimSpace(text)
		for _, marker := range []string{"把", "将", "请"} {
			trimmed = strings.TrimPrefix(trimmed, marker)
		}
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == text {
			break
		}
		text = trimmed
	}
	if utf8.RuneCountInString(text) >= 2 {
		return truncateRunesV2(text, 24)
	}
	return truncateRunesV2(strings.TrimSpace(feedback), 24)
}

func codingPlanStepBody(desc string) string {
	if i := strings.Index(strings.ToLower(desc), "\n\n## overall request"); i >= 0 {
		desc = desc[:i]
	}
	desc = strings.TrimSpace(desc)
	for _, tail := range []string{"确认前不写文件。", "确认前不写文件"} {
		if strings.HasSuffix(desc, tail) {
			desc = strings.TrimSpace(strings.TrimSuffix(desc, tail))
			desc = strings.TrimRight(desc, "。.")
		}
	}
	return strings.TrimSpace(desc)
}

func codingPlanStepLooksLikeVerify(task *v2.TaskItem) bool {
	if task == nil {
		return false
	}
	title := strings.TrimSpace(task.Title)
	for _, marker := range []string{"测试", "验证", "构建", "编译", "说明", "文档"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	return codingPlanLatinTitleIsClosing(title)
}

// codingPlanLatinTitleIsClosing is a trailing check such as "Build and verify"
// or "Run tests". "Build the installer" names a feature, and "contest" only
// happens to contain the letters "test".
func codingPlanLatinTitleIsClosing(title string) bool {
	closing := map[string]bool{
		"readme": true, "test": true, "tests": true, "build": true, "verify": true, "verification": true,
	}
	skip := map[string]bool{
		"and": true, "the": true, "a": true, "an": true, "to": true, "run": true, "running": true,
	}
	sawClosing := false
	for _, field := range strings.Fields(strings.ToLower(title)) {
		field = strings.Trim(field, ".,!?;:\"'")
		if field == "" || skip[field] {
			continue
		}
		if closing[field] {
			sawClosing = true
			continue
		}
		return false
	}
	return sawClosing
}

func codingPlanFeedbackRemoves(feedback string) bool {
	if !codingPlanFeedbackHasMarker(feedback, []string{"不要", "去掉", "删除", "取消", "不用", "remove", "drop", "without"}) {
		return false
	}
	// "需要图形界面，不用只做控制台" is a new scope, not a request to delete a step.
	return !codingPlanFeedbackHasMarker(feedback, []string{"需要", "加上", "增加", "改成", "改为", "添加"})
}

func codingPlanFeedbackHardRemove(feedback string) bool {
	return codingPlanFeedbackRemoves(feedback) && codingPlanFeedbackHasMarker(feedback, []string{"不要", "去掉", "删除", "取消", "remove", "drop"})
}

func codingPlanFeedbackHasChangeCue(feedback string) bool {
	return codingPlanFeedbackHasMarker(feedback, []string{
		"不要", "去掉", "删除", "取消", "不用", "需要", "加上", "增加", "改成", "改为", "添加",
		"remove", "drop", "without", "add",
	})
}

func codingPlanFeedbackIsQuestion(feedback string) bool {
	if strings.ContainsAny(feedback, "?？") {
		return true
	}
	compact := codingPlanDialogueCompact(feedback)
	for _, marker := range []string{
		"为什么", "為何", "怎么", "怎麼", "如何", "怎么样", "怎麼樣",
		"是什么", "是什麼", "什么意思", "什麼意思", "有几", "有幾",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	if codingPlanFeedbackHasMarker(feedback, []string{"why", "how", "what"}) {
		return true
	}
	return strings.HasSuffix(compact, "吗") || strings.HasSuffix(compact, "嗎") || strings.HasSuffix(compact, "么") || strings.HasSuffix(compact, "呢")
}

func codingPlanFeedbackHasMarker(feedback string, markers []string) bool {
	compact := codingPlanDialogueCompact(feedback)
	for _, marker := range markers {
		if marker == "" {
			continue
		}
		if marker[0] < 128 {
			if codingPlanHasLatinWord(feedback, marker) {
				return true
			}
			continue
		}
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func codingPlanHasLatinWord(feedback, word string) bool {
	for _, field := range strings.Fields(strings.ToLower(feedback)) {
		field = strings.Trim(field, ".,!?;:\"'")
		if field == word {
			return true
		}
	}
	return false
}

func codingPlanFeedbackFocus(feedback string) string {
	compact := codingPlanDialogueCompact(codingPlanStripEnglishCues(feedback))
	for _, marker := range []string{"不要", "去掉", "删除", "取消", "不用", "需要有", "需要", "加上", "增加", "改成", "改为", "添加", "帮我", "一下"} {
		compact = strings.ReplaceAll(compact, marker, "")
	}
	for {
		trimmed := compact
		for _, marker := range []string{"把", "将", "请"} {
			trimmed = strings.TrimPrefix(trimmed, marker)
		}
		if trimmed == compact {
			break
		}
		compact = trimmed
	}
	return compact
}

func codingPlanStripEnglishCues(feedback string) string {
	drop := map[string]bool{
		"add": true, "need": true, "needs": true, "please": true,
		"remove": true, "drop": true, "without": true, "change": true,
		"a": true, "an": true, "the": true,
	}
	var kept []string
	for _, field := range strings.Fields(feedback) {
		word := strings.Trim(strings.ToLower(field), ".,!?;:\"'")
		if drop[word] {
			continue
		}
		kept = append(kept, field)
	}
	if len(kept) == 0 {
		return feedback
	}
	return strings.Join(kept, " ")
}

func (h *IMMessageHandler) callCodingPlanRevisionModel(brief, latest, projectPath, currentMarkdown string) string {
	if h == nil {
		return ""
	}
	cfg := h.getCodingLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getCodingLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Original request:\n")
	b.WriteString(truncateRunesV2(brief, 2000))
	b.WriteString("\n\nCurrent plan:\n")
	b.WriteString(truncateRunesV2(currentMarkdown, 2500))
	b.WriteString("\n\nChange requested:\n")
	b.WriteString(truncateRunesV2(latest, 2000))
	if p := strings.TrimSpace(projectPath); p != "" {
		b.WriteString("\n\nProject path: ")
		b.WriteString(p)
	}
	return h.callLightweightLLM(cfg, codingPlanRevisionSystemPrompt, b.String(), 45)
}

func (h *IMMessageHandler) draftCodingPlanDialogue(userID, brief, latest, projectPath string) ([]*v2.TaskItem, string, bool) {
	if tasks := extractUserProvidedCodingPlan(latest); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks, "已按你列出的步骤起草。", false
	}
	goal := codingPlanDialogueSubstance(brief, latest)
	// A bare "先规划" has nothing to break into steps. Ask before inventing a product.
	if codingPlanDialogueTooThin(goal) && !codingRequestLooksModeratelyComplex(goal) {
		return nil, "", true
	}
	// Same planner the workbench uses for a multi-step execution plan. A second
	// dialogue model here only produced a rival draft.
	if _, tasks := h.planCodingWorkbenchTasks(userID, goal, projectPath, h.getStickyCodingWorkbenchMemory(userID)); len(tasks) >= codingWorkbenchPlanMinTasks {
		if codingPlanDialogueMostlyASCII(goal) {
			return tasks, "The goal is split into steps you can confirm.", false
		}
		return tasks, "已按目标拆成可确认的步骤。", false
	}
	if codingPlanDialogueMostlyASCII(goal) {
		return hostCodingPlanScaffold(goal), "The model did not return steps, so this draft follows the goal. You can edit it directly.", false
	}
	return hostCodingPlanScaffold(goal), "模型没有返回步骤，已按目标起草一版，你可以直接改。", false
}

func hostCodingPlanScaffold(goal string) []*v2.TaskItem {
	goal = strings.TrimSpace(goal)
	short := truncateRunesV2(goal, 80)
	if codingPlanDialogueMostlyASCII(goal) {
		return []*v2.TaskItem{
			{Index: 1, Title: "Confirm the scope", Description: "Read the related code and name the files to change and the behavior to keep. Goal: " + short},
			{Index: 2, Title: "Implement the goal", Description: "Implement this goal: " + short + ". Do not add features that were not asked for."},
			{Index: 3, Title: "Build and verify", Description: "Build and run, and check that the requested behavior works."},
		}
	}
	return []*v2.TaskItem{
		{Index: 1, Title: "确认改动范围", Description: "阅读相关代码，确定要改的文件和必须保留的行为。目标：" + short},
		{Index: 2, Title: "按目标实现", Description: "实现这个目标：" + short + "。不扩大到没提到的功能。"},
		{Index: 3, Title: "构建并验证", Description: "构建并运行，确认目标行为成立。"},
	}
}

type codingPlanDialogueJSON struct {
	Action string                        `json:"action"`
	Reply  string                        `json:"reply"`
	Steps  []codingWorkbenchPlanStepJSON `json:"steps"`
}

func parseCodingPlanPatch(raw string) (op string, index int, patch *v2.TaskItem, reply string, ok bool) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var payload struct {
		Action      string   `json:"action"`
		Op          string   `json:"op"`
		Index       int      `json:"index"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Reply       string   `json:"reply"`
		Files       []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", 0, nil, "", false
	}
	op = strings.ToLower(strings.TrimSpace(payload.Op))
	action := strings.ToLower(strings.TrimSpace(payload.Action))
	if action != "" && action != "patch" {
		return "", 0, nil, "", false
	}
	switch op {
	case "insert", "update", "delete":
	default:
		return "", 0, nil, "", false
	}
	return op, payload.Index, &v2.TaskItem{
		Title:       strings.TrimSpace(payload.Title),
		Description: strings.TrimSpace(payload.Description),
		Files:       payload.Files,
	}, strings.TrimSpace(payload.Reply), true
}

func parseCodingPlanDialogue(raw string) ([]*v2.TaskItem, string, bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", false, false
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var payload codingPlanDialogueJSON
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, "", false, false
	}
	reply := strings.TrimSpace(payload.Reply)
	switch strings.ToLower(strings.TrimSpace(payload.Action)) {
	case "ask":
		return nil, reply, true, true
	case "propose":
		tasks := stepsJSONToTasks(payload.Steps)
		return tasks, reply, false, true
	default:
		return nil, "", false, false
	}
}

func (h *IMMessageHandler) codingPlanDialogueResponse(onToken func(string), text string, actions []IMResponseAction) *IMAgentResponse {
	text = strings.TrimSpace(text)
	if onToken != nil && text != "" {
		onToken("\n\n" + text)
	}
	return &IMAgentResponse{Text: text, Actions: actions}
}

func codingPlanDialogueFooter(userText string) string {
	if codingPlanDialogueMostlyASCII(userText) {
		return "The plan was self-reviewed before it reached you. Object or add detail and it will be replanned. Nothing is written until you confirm."
	}
	return "这版已先自动评审。可以提出异议或补充，随后会重新规划。确认前不会改文件。"
}

func codingPlanDialogueMostlyASCII(text string) bool {
	letters := 0
	nonASCII := 0
	for _, r := range text {
		if r > 127 && unicode.IsLetter(r) {
			nonASCII++
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters > 0 && nonASCII == 0
}

// codingUserWantsPlanFirst reports an explicit "plan, then build" intent.
// Complexity alone does not qualify: that path already has its own gate.
func codingUserWantsPlanFirst(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || codingPlanDialogueIsModeQuestion(text) {
		return false
	}
	compact := codingPlanDialogueCompact(text)
	for _, marker := range []string{
		"先规划", "先規劃", "规划模式", "規劃模式", "规划一下", "規劃一下",
		"先设计", "先設計", "先出方案", "先出计划", "先出計劃",
		"先出开发文档", "先写开发文档", "先寫開發文檔", "先写设计文档", "先寫設計文檔",
		"确认后再开发", "确认后再写", "确认后再做", "确认后再开工", "确认后再实现",
		"確認後再開發", "確認後再寫", "確認後再做", "確認後再開工",
		"先别写代码", "先不要写代码", "先别改代码", "先不要改代码",
		"先別寫程式", "先不要寫程式",
		"先定方案", "先讨论方案", "先討論方案",
		"planfirst", "designfirst", "planbefore", "beforecoding", "beforeyoucode",
		"dontcodeyet", "donotcodeyet", "writeaplan", "draftaplan",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	lower := strings.ToLower(text)
	for _, phrase := range []string{
		"plan first", "design first", "plan before", "before coding", "before you code",
		"don't code yet", "do not code yet", "write a plan", "draft a plan",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func codingPlanDialogueIsModeQuestion(text string) bool {
	if utf8.RuneCountInString(strings.TrimSpace(text)) > 18 {
		return false
	}
	compact := codingPlanDialogueCompact(text)
	if !strings.Contains(compact, "规划") && !strings.Contains(compact, "規劃") && !strings.Contains(compact, "plan") {
		return false
	}
	for _, marker := range []string{"是什么", "是什麼", "怎么用", "怎麼用", "如何", "whatis", "howdoes", "howdo"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func codingPlanDialogueTooThin(text string) bool {
	compact := codingPlanDialogueCompact(text)
	for _, marker := range []string{
		"先规划", "先規劃", "规划模式", "規劃模式", "规划一下", "規劃一下",
		"先设计", "先設計", "planfirst", "designfirst",
	} {
		compact = strings.ReplaceAll(compact, marker, "")
	}
	return utf8.RuneCountInString(strings.TrimSpace(compact)) < 8
}

func codingPlanDialogueConfirms(text string) bool {
	switch codingPlanDialogueCompact(text) {
	case "确认", "確認", "确认计划", "確認計畫", "确认方案", "確認方案",
		"就按这个", "就按這個", "按这个做", "按這個做", "按这个方案", "按這個方案",
		"开始", "開始", "开始实施", "開始實施", "开工", "開工", "批准",
		"approve", "start", "lgtm", "goahead":
		return true
	default:
		return false
	}
}

func codingPlanDialogueDirect(text string) bool {
	compact := codingPlanDialogueCompact(text)
	if compact == "" || utf8.RuneCountInString(compact) > 24 {
		return false
	}
	for _, marker := range []string{
		"直接做", "直接执行", "直接執行", "不用规划", "不用規劃",
		"跳过规划", "跳過規劃", "justdoit", "skipplan", "skiptheplan",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func codingPlanDialogueCancels(text string) bool {
	compact := codingPlanDialogueCompact(text)
	if compact == "" || utf8.RuneCountInString(compact) > 16 {
		return false
	}
	for _, marker := range []string{"取消规划", "取消規劃", "退出规划", "退出規劃", "取消方案", "cancelplan"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func codingPlanDialogueCompact(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
