package guiapp

import (
	"encoding/json"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const codingPlanDialogueSystemPrompt = `You are the planning partner for a coding workbench. The user wants a plan before any file changes. Return JSON only.

Schema: {"action":"ask|propose","reply":"...","steps":[{"title":"...","description":"...","files":["relative/path"]}]}

ask: the goal is still too vague to plan. reply is 1-2 questions in the user's language. steps must be empty.
propose: name an ordered plan of 2-6 steps. reply is one short note about what you assumed, in the user's language. steps are concrete: explore, then implement, then verify. No code.
Do not claim that files were changed. Do not start implementation.`

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
	if isGoalSystemContinuation(userText) || isSessionRoundContinuation(userText) {
		return userText, nil
	}
	if strings.HasPrefix(userText, codingPlanApproveExecuteMarker) {
		return userText, nil
	}
	if codingRequestLooksExplicitWorkspaceClear(userText) {
		return userText, nil
	}

	mem := h.getStickyCodingWorkbenchMemory(userID)
	_, hasPending := h.loadStickyPendingCodingPlan(userID)
	inDialogue := mem.PlanDialogue

	if inDialogue || hasPending {
		switch {
		case codingPlanDialogueCancels(userText):
			h.exitCodingPlanDialogue(userID, true)
			log.Printf("[coding-plan] dialogue cancelled user=%s", userID)
			return userText, h.codingPlanDialogueResponse(onToken, "已退出规划模式，没有改文件。要做的时候直接说任务即可。", nil)
		case codingPlanDialogueDirect(userText):
			goal := h.codingPlanDialogueGoal(userID, userText)
			h.clearStickyPendingCodingPlan(userID)
			h.clearCodingPlanDialogue(userID)
			h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
				m.SkipNextPlan = true
			})
			log.Printf("[coding-plan] dialogue skipped user=%s", userID)
			if onProgress != nil {
				onProgress("已跳过规划，按原目标直接执行")
			}
			return goal, nil
		case codingPlanDialogueConfirms(userText) && hasPending:
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
		if hasPending {
			return userText, h.reviseStickyPendingCodingPlan(userID, userText, projectPath, onProgress, onToken)
		}
	}

	if !inDialogue && !codingUserWantsPlanFirst(userText) {
		return userText, nil
	}
	return userText, h.runCodingPlanDialogueTurn(userID, userText, projectPath, onProgress, onToken)
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
	})
}

func (h *IMMessageHandler) exitCodingPlanDialogue(userID string, clearPlan bool) {
	if clearPlan {
		h.clearStickyPendingCodingPlan(userID)
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
	}
	h.clearCodingPlanDialogue(userID)
	h.emitCodingWorkbenchStepsUpdate(userID)
}

func (h *IMMessageHandler) runCodingPlanDialogueTurn(userID, userText, projectPath string, onProgress func(string), onToken func(string)) *IMAgentResponse {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	entering := !mem.PlanDialogue
	brief := strings.TrimSpace(mem.PlanDialogueBrief)
	if brief == "" {
		brief = strings.TrimSpace(userText)
	}
	if entering {
		h.clearStickyPendingCodingPlan(userID)
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		if strings.TrimSpace(m.PlanDialogueBrief) == "" {
			m.PlanDialogueBrief = brief
		}
	})
	if onProgress != nil {
		onProgress("规划模式：先和你确认方案，确认前不会改文件")
	}

	pending, _ := h.loadStickyPendingCodingPlan(userID)
	if len(pending.Tasks) >= codingWorkbenchPlanMinTasks && strings.TrimSpace(userText) != brief {
		return h.reviseStickyPendingCodingPlan(userID, userText, projectPath, onProgress, onToken)
	}
	tasks, reply, asked := h.draftCodingPlanDialogue(userID, brief, userText, projectPath, pending.Markdown)
	if asked || len(tasks) < codingWorkbenchPlanMinTasks {
		note := strings.TrimSpace(reply)
		if note == "" {
			note = "想先规划再开发。请补充要做的功能、做到什么程度，以及有没有不能改的部分。"
		}
		h.setStickyCodingRequirementRestatement(userID, truncateRunesV2(note, codingRequestUnderstandingMaxRunes))
		h.emitCodingWorkbenchStepsUpdate(userID)
		log.Printf("[coding-plan] dialogue asking user=%s", userID)
		return h.codingPlanDialogueResponse(onToken, note+"\n\n"+codingPlanDialogueFooter(userText), nil)
	}

	tasks = finalizeCodingWorkbenchTasks(tasks, brief)
	if len(tasks) > codingWorkbenchPlanMaxTasks {
		tasks = tasks[:codingWorkbenchPlanMaxTasks]
	}
	if len(tasks) < codingWorkbenchPlanMinTasks {
		h.emitCodingWorkbenchStepsUpdate(userID)
		return h.codingPlanDialogueResponse(onToken, "方案还不够具体，请补充要改的功能和完成标准。\n\n"+codingPlanDialogueFooter(userText), nil)
	}
	markdown := formatCodingWorkbenchPlanMarkdown(brief, tasks)
	h.storeStickyPendingCodingPlan(userID, brief, markdown, tasks)
	h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	if note := strings.TrimSpace(reply); note != "" {
		h.setStickyCodingRequirementRestatement(userID, truncateRunesV2(note, codingRequestUnderstandingMaxRunes))
	}
	h.emitCodingWorkbenchStepsUpdate(userID)
	log.Printf("[coding-plan] dialogue proposed user=%s steps=%d", userID, len(tasks))

	body := formatPendingPlanApprovalText(markdown, len(tasks))
	if note := strings.TrimSpace(reply); note != "" && !strings.Contains(body, note) {
		body = note + "\n\n" + body
	}
	body += "\n\n" + codingPlanDialogueFooter(userText)
	return h.codingPlanDialogueResponse(onToken, body, codingPlanApproveActions())
}

const codingPlanRevisionSystemPrompt = `You revise an existing coding-workbench plan. The user is changing that plan before any file is written. Return JSON only.

Schema: {"action":"propose","reply":"...","steps":[{"title":"...","description":"...","files":["relative/path"]}]}

Rules:
- Return the FULL updated plan, 2-6 steps.
- Keep every current step the latest message does not contradict.
- Apply the latest message to the scope, a step, or the files. Do not collapse the plan into a generic two-step rewrite.
- reply is one short note, in the user's language, naming what changed.
- Do not claim files were changed. Do not start implementation.`

// reviseStickyPendingCodingPlan folds a follow-up into the plan that is
// already waiting for confirmation. The original goal stays the goal.
func (h *IMMessageHandler) reviseStickyPendingCodingPlan(userID, userText, projectPath string, onProgress func(string), onToken func(string)) *IMAgentResponse {
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) < codingWorkbenchPlanMinTasks {
		return h.runCodingPlanDialogueTurn(userID, userText, projectPath, onProgress, onToken)
	}
	brief := strings.TrimSpace(pending.UserText)
	if brief == "" {
		brief = h.codingPlanDialogueGoal(userID, userText)
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		if strings.TrimSpace(m.PlanDialogueBrief) == "" {
			m.PlanDialogueBrief = brief
		}
	})
	if onProgress != nil {
		onProgress("规划模式：正在把你的补充写进当前方案，确认前不会改文件")
	}

	tasks, reply := h.draftCodingPlanRevision(brief, userText, projectPath, pending)
	tasks = finalizeCodingWorkbenchTasks(tasks, brief)
	if len(tasks) > codingWorkbenchPlanMaxTasks {
		tasks = tasks[:codingWorkbenchPlanMaxTasks]
	}
	if len(tasks) < codingWorkbenchPlanMinTasks {
		tasks = applyCodingPlanFeedback(pending.Tasks, userText)
		tasks = finalizeCodingWorkbenchTasks(tasks, brief)
	}
	markdown := formatCodingWorkbenchPlanMarkdown(brief, tasks)
	h.storeStickyPendingCodingPlan(userID, brief, markdown, tasks)
	h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	note := strings.TrimSpace(reply)
	if note == "" {
		note = "已把「" + truncateRunesV2(userText, 80) + "」写进当前规划。"
	}
	h.setStickyCodingRequirementRestatement(userID, truncateRunesV2(note, codingRequestUnderstandingMaxRunes))
	h.emitCodingWorkbenchStepsUpdate(userID)
	log.Printf("[coding-plan] dialogue revised user=%s steps=%d", userID, len(tasks))

	body := note + "\n\n" + formatPendingPlanApprovalText(markdown, len(tasks)) + "\n\n" + codingPlanDialogueFooter(userText)
	return h.codingPlanDialogueResponse(onToken, body, codingPlanApproveActions())
}

func (h *IMMessageHandler) draftCodingPlanRevision(brief, latest, projectPath string, pending codingWorkbenchPendingPlan) ([]*v2.TaskItem, string) {
	if tasks := extractUserProvidedCodingPlan(latest); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks, "已换成你写出的步骤。"
	}
	if raw := h.callCodingPlanRevisionModel(brief, latest, projectPath, pending.Markdown); raw != "" {
		if tasks, reply, asked, ok := parseCodingPlanDialogue(raw); ok && !asked && len(tasks) >= codingWorkbenchPlanMinTasks {
			if !codingPlanRevisionCollapsed(pending.Tasks, tasks, latest) {
				if strings.TrimSpace(reply) == "" {
					reply = "已按你的补充更新规划。"
				}
				return tasks, reply
			}
		}
	}
	return applyCodingPlanFeedback(pending.Tasks, latest), "已把「" + truncateRunesV2(latest, 80) + "」写进当前规划。"
}

// codingPlanRevisionCollapsed reports a model plan that threw away current
// steps. Shrinking is allowed only when the user asked to remove something
// and the new plan still reflects that change. "需要有图形界面" must not
// replace a five-step plan with a two-step rewrite that merely mentions a GUI.
func codingPlanRevisionCollapsed(current, proposed []*v2.TaskItem, feedback string) bool {
	if len(current) < 2 || len(proposed) == 0 || len(proposed) >= len(current) {
		return false
	}
	return !(codingPlanFeedbackRemoves(feedback) && codingPlanTasksMention(proposed, feedback))
}

func codingPlanTasksMention(tasks []*v2.TaskItem, feedback string) bool {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return false
	}
	focus := codingPlanFeedbackFocus(feedback)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		blob := task.Title + "\n" + task.Description
		if strings.Contains(blob, feedback) {
			return true
		}
		if focus != "" && strings.Contains(codingPlanDialogueCompact(blob), focus) {
			return true
		}
	}
	return false
}

// applyCodingPlanFeedback keeps the current steps and records the follow-up.
// A new requirement is inserted before the closing verify step. A removal
// annotates the step it names instead of appending a contradictory step.
func applyCodingPlanFeedback(tasks []*v2.TaskItem, feedback string) []*v2.TaskItem {
	feedback = strings.TrimSpace(feedback)
	cloned := cloneCodingWorkbenchTasks(tasks)
	if feedback == "" || len(cloned) == 0 {
		return cloned
	}
	for _, task := range cloned {
		if task == nil {
			continue
		}
		task.Description = codingPlanStepBody(task.Description)
		task.DependsOn = nil
	}
	if codingPlanTasksMention(cloned, feedback) {
		return cloned
	}
	note := "按你的补充修改：" + feedback
	if codingPlanFeedbackRemoves(feedback) {
		target := codingPlanFeedbackTarget(cloned, feedback)
		if target >= 0 && cloned[target] != nil {
			cloned[target].Description = strings.TrimSpace(cloned[target].Description + "\n" + note)
		}
		return cloned
	}
	if len(cloned) >= codingWorkbenchPlanMaxTasks {
		target := len(cloned) - 1
		for i := len(cloned) - 1; i >= 0; i-- {
			if cloned[i] != nil && !codingPlanStepLooksLikeVerify(cloned[i]) {
				target = i
				break
			}
		}
		if cloned[target] != nil {
			cloned[target].Description = strings.TrimSpace(cloned[target].Description + "\n" + note)
		}
		return cloned
	}
	step := &v2.TaskItem{
		Title:       truncateRunesV2(feedback, 24),
		Description: note + "。确认前不写文件。",
	}
	insertAt := len(cloned)
	if insertAt > 0 && codingPlanStepLooksLikeVerify(cloned[insertAt-1]) {
		insertAt--
	}
	cloned = append(cloned, nil)
	copy(cloned[insertAt+1:], cloned[insertAt:])
	cloned[insertAt] = step
	for i, task := range cloned {
		if task != nil {
			task.Index = i + 1
		}
	}
	return cloned
}

func codingPlanStepBody(desc string) string {
	if i := strings.Index(strings.ToLower(desc), "\n\n## overall request"); i >= 0 {
		return strings.TrimSpace(desc[:i])
	}
	return strings.TrimSpace(desc)
}

func codingPlanStepLooksLikeVerify(task *v2.TaskItem) bool {
	if task == nil {
		return false
	}
	blob := strings.ToLower(task.Title + " " + codingPlanStepBody(task.Description))
	for _, marker := range []string{"测试", "验证", "构建", "编译", "readme", "说明", "test", "build", "verify"} {
		if strings.Contains(blob, marker) {
			return true
		}
	}
	return false
}

func codingPlanFeedbackRemoves(feedback string) bool {
	compact := codingPlanDialogueCompact(feedback)
	for _, marker := range []string{"不要", "去掉", "删除", "取消", "不用", "remove", "drop", "without"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func codingPlanFeedbackFocus(feedback string) string {
	compact := codingPlanDialogueCompact(feedback)
	for _, marker := range []string{"不要", "去掉", "删除", "取消", "不用", "需要有", "需要", "加上", "增加", "改成", "改为"} {
		compact = strings.ReplaceAll(compact, marker, "")
	}
	return compact
}

func codingPlanFeedbackTarget(tasks []*v2.TaskItem, feedback string) int {
	focus := codingPlanFeedbackFocus(feedback)
	if focus == "" {
		return len(tasks) - 1
	}
	for i, task := range tasks {
		if task == nil {
			continue
		}
		blob := codingPlanDialogueCompact(task.Title + task.Description)
		if strings.Contains(blob, focus) {
			return i
		}
	}
	return len(tasks) - 1
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

func (h *IMMessageHandler) draftCodingPlanDialogue(userID, brief, latest, projectPath, currentMarkdown string) ([]*v2.TaskItem, string, bool) {
	if tasks := extractUserProvidedCodingPlan(latest); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks, "", false
	}
	if raw := h.callCodingPlanDialogueModel(brief, latest, projectPath, currentMarkdown); raw != "" {
		if tasks, reply, asked, ok := parseCodingPlanDialogue(raw); ok {
			if asked {
				return nil, reply, true
			}
			if len(tasks) >= codingWorkbenchPlanMinTasks {
				return tasks, reply, false
			}
		}
	}
	// A concrete first request still gets a host draft when the planner model
	// is absent. A bare "先规划" stays a question so we do not invent a product.
	// Revisions of an existing plan never land here: they keep those steps.
	if !codingPlanDialogueTooThin(latest) || codingRequestLooksModeratelyComplex(latest) || codingRequestLooksModeratelyComplex(brief) {
		goal := strings.TrimSpace(brief)
		if latest != "" && latest != brief {
			goal = strings.TrimSpace(goal + "\n" + latest)
		}
		return defaultModerateCodingPlan(goal, ""), "", false
	}
	return nil, "", true
}

func (h *IMMessageHandler) callCodingPlanDialogueModel(brief, latest, projectPath, currentMarkdown string) string {
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
	b.WriteString("\n\nLatest user message:\n")
	b.WriteString(truncateRunesV2(latest, 2000))
	if p := strings.TrimSpace(projectPath); p != "" {
		b.WriteString("\n\nProject path: ")
		b.WriteString(p)
	}
	if s := strings.TrimSpace(currentMarkdown); s != "" {
		b.WriteString("\n\nCurrent draft plan:\n")
		b.WriteString(truncateRunesV2(s, 2000))
	}
	return h.callLightweightLLM(cfg, codingPlanDialogueSystemPrompt, b.String(), 45)
}

type codingPlanDialogueJSON struct {
	Action string                        `json:"action"`
	Reply  string                        `json:"reply"`
	Steps  []codingWorkbenchPlanStepJSON `json:"steps"`
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
		return "Planning mode: nothing is written until you confirm. Reply to revise the plan. Say \"start\" to implement it, or \"just do it\" to skip planning."
	}
	return "规划模式：确认前不会改文件。直接回复可以改方案。说「开始」按方案开工，说「直接做」跳过规划。"
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
