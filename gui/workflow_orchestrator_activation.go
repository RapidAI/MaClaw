package main

import (
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func (h *IMMessageHandler) backfillExecutionOrchestratorActivation(engine *workflow.WorkflowEngine, userID string, resp *workflow.WorkflowResponse) {
	if engine == nil || resp == nil || resp.ActivateOrchestrator || !resp.RunAgentLoop {
		return
	}
	if h.taskOrchestratorRegistry == nil {
		return
	}
	if orch := h.taskOrchestratorRegistry.Get(userID); orch != nil && orch.IsActive() {
		return
	}

	ws, tmpl, ok := activeWorkflowExecutionPhase(engine, userID)
	if !ok {
		return
	}

	resp.ActivateOrchestrator = true
	if ws.PhaseIndex > 0 {
		prevPhaseID := tmpl.Phases[ws.PhaseIndex-1].ID
		resp.TaskBreakdownText = ws.PhaseOutputs[prevPhaseID]
	}
	var reqParts, designParts []string
	for i := 0; i < ws.PhaseIndex; i++ {
		output := ws.PhaseOutputs[tmpl.Phases[i].ID]
		if output == "" {
			continue
		}
		runes := []rune(output)
		if len(runes) > 500 {
			output = string(runes[:500])
		}
		if i == 0 {
			reqParts = append(reqParts, output)
		} else {
			designParts = append(designParts, output)
		}
	}
	resp.RequirementsContext = strings.Join(reqParts, "\n")
	resp.DesignContext = strings.Join(designParts, "\n")
	log.Printf("[WorkflowInterception] backfilled orchestrator activation for active execution phase: user=%s phase=%s", userID, ws.CurrentPhase)
}

func activeWorkflowExecutionPhase(engine *workflow.WorkflowEngine, userID string) (*workflow.V1WorkflowState, *workflow.V1WorkflowTemplate, bool) {
	if engine == nil {
		return nil, nil, false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return nil, nil, false
	}
	if !engine.IsActivePhaseExecutionOrchestrator(userID) {
		return nil, nil, false
	}
	registry := engine.GetRegistry()
	if registry == nil {
		return nil, nil, false
	}
	tmpl := registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return nil, nil, false
	}
	phase := tmpl.Phases[ws.PhaseIndex]
	if phase.ID != ws.CurrentPhase || !workflow.IsTemplatePhaseExecutionOrchestrator(tmpl, phase) {
		return nil, nil, false
	}
	return ws, tmpl, true
}

func (h *IMMessageHandler) activateWorkflowTaskOrchestrator(engine *workflow.WorkflowEngine, userID string, resp *workflow.WorkflowResponse) (bool, *IMAgentResponse) {
	if h == nil || engine == nil || resp == nil || !resp.ActivateOrchestrator {
		return false, nil
	}
	if !engine.IsActivePhaseExecutionOrchestrator(userID) {
		log.Printf("[WorkflowInterception] ignored orchestrator activation outside execution phase: user=%s", userID)
		return false, nil
	}
	taskOrch := h.getTaskOrchestrator(userID)
	if taskOrch == nil || taskOrch.IsActive() || strings.TrimSpace(resp.TaskBreakdownText) == "" {
		return false, nil
	}
	tasks := ParseTaskListFromText(resp.TaskBreakdownText)
	if !taskBreakdownValidForOrchestrator(engine, userID, resp.TaskBreakdownText, tasks) {
		if codingWorkflowActive(engine, userID) {
			log.Printf("[WorkflowInterception] execution phase entered but coding task breakdown is not executable; blocking normal agent loop fallback for user=%s", userID)
		} else {
			log.Printf("[WorkflowInterception] execution phase entered but preceding output is not a task list; using normal agent loop for user=%s", userID)
		}
		return false, nil
	}
	projectPath := h.workflowExecutionProjectPath(engine, userID)
	normalizedPath, err := h.resolveWorkflowProjectPath(projectPath)
	if err != nil {
		log.Printf("[WorkflowInterception] invalid orchestrator project path: user=%s project=%s err=%v", userID, projectPath, err)
		return false, &IMAgentResponse{Error: i18n.Tf(i18n.MsgWorkflowPrepareProjectError, h.getWorkflowLang(), err)}
	}
	projectPath = normalizedPath
	taskOrch.Activate(tasks, resp.RequirementsContext, resp.DesignContext, projectPath, "")
	log.Printf("[WorkflowInterception] orchestrator activated by engine: %d tasks for user=%s project=%s", len(tasks), userID, projectPath)
	return true, nil
}

func (h *IMMessageHandler) repairInvalidCodingTaskBreakdownExecution(engine *workflow.WorkflowEngine, userID string, resp *workflow.WorkflowResponse) (*IMAgentResponse, bool) {
	if !invalidCodingTaskBreakdownWouldBypassSubAgent(engine, userID, resp) {
		return nil, false
	}
	log.Printf("[WorkflowInterception] blocked coding execution because task breakdown cannot activate SubAgent: user=%s", userID)
	repairResp, err := engine.ReopenPhaseForRevision(userID, workflow.PhaseCodingTaskBreakdown, invalidCodingTaskBreakdownFeedbackText())
	if err != nil {
		log.Printf("[WorkflowInterception] failed to reopen invalid coding task breakdown: user=%s err=%v", userID, err)
		return &IMAgentResponse{Error: i18n.Tf(i18n.MsgWorkflowRepairError, h.getWorkflowLang(), err)}, true
	}
	if repairResp != nil && repairResp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(userID, repairResp.PhasePrompt)
		h.workflowAgentLoopMarker.Store(userID, true)
	}
	return nil, true
}

func invalidCodingTaskBreakdownWouldBypassSubAgent(engine *workflow.WorkflowEngine, userID string, resp *workflow.WorkflowResponse) bool {
	if engine == nil || resp == nil || !resp.ActivateOrchestrator {
		return false
	}
	if !engine.IsActivePhaseExecutionOrchestrator(userID) {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.Type != workflow.WorkflowCoding {
		return false
	}
	tasks := ParseTaskListFromText(resp.TaskBreakdownText)
	return !isExecutableCodingTaskBreakdown(resp.TaskBreakdownText, tasks)
}

func invalidCodingTaskBreakdownFeedbackText() string {
	return "Task breakdown is not an executable Markdown task list. Regenerate task breakdown with headings like `### T1: Task title` and `### T2: Task title`; each task must include description, files, dependencies, priority, and effort. Do not write code or output an implementation-complete report."
}

func (h *IMMessageHandler) shouldRegenerateInvalidCodingTaskBreakdown(engine *workflow.WorkflowEngine, userID string) bool {
	output, ok := currentCodingTaskBreakdownOutputBeforeExecution(engine, userID)
	if !ok {
		return false
	}
	return !isExecutableCodingTaskBreakdown(output, ParseTaskListFromText(output))
}

func (h *IMMessageHandler) reopenInvalidCodingTaskBreakdownForRepair(engine *workflow.WorkflowEngine, userID string) bool {
	if h == nil || engine == nil {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.Type != workflow.WorkflowCoding || ws.CurrentPhase != workflow.PhaseCodingTaskBreakdown {
		return false
	}
	if ws.PendingReviewRevisionRequested {
		return true
	}
	repairResp, err := engine.ReopenPhaseForRevision(userID, workflow.PhaseCodingTaskBreakdown, invalidCodingTaskBreakdownFeedbackText())
	if err != nil {
		log.Printf("[WorkflowInterception] failed to reopen invalid coding task breakdown for repair: user=%s err=%v", userID, err)
		return false
	}
	if repairResp != nil && repairResp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(userID, repairResp.PhasePrompt)
		h.workflowAgentLoopMarker.Store(userID, true)
	}
	return true
}

func isExecutableCodingTaskBreakdown(text string, tasks []*TaskItem) bool {
	if containsCodingTaskBreakdownBlockedExecutionLanguage(text) {
		return false
	}
	if len(tasks) == 0 || !hasSequentialTNumberedTaskHeadings(text, len(tasks)) {
		return false
	}
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.Title) == "" {
			return false
		}
	}
	return codingTaskBreakdownHasRequiredFields(text, len(tasks)) && codingTaskDependenciesAreValid(text, len(tasks))
}

func containsCodingTaskBreakdownBlockedExecutionLanguage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	blockedMarkers := []string{
		"\u6267\u884c\u53d7\u963b", "\u53d7\u963b\u8bf4\u660e", "\u5de5\u5177\u4e0d\u53ef\u7528", "\u6ca1\u6709\u5de5\u5177", "\u6ca1\u5de5\u5177",
		"\u65e0\u6cd5\u521b\u5efa\u76ee\u5f55", "\u4e0d\u80fd\u521b\u5efa\u76ee\u5f55", "\u65e0\u6cd5\u5199\u6587\u4ef6", "\u4e0d\u80fd\u5199\u6587\u4ef6",
		"\u9700\u8981\u5f00\u542f\u5de5\u5177", "\u542f\u7528\u5de5\u5177", "\u624b\u52a8\u521b\u5efa",
		"tool unavailable", "tools unavailable", "no tools", "missing tool", "missing tools", "cannot create directory", "cannot write file",
		"write_file/edit_file", "write_file unavailable", "edit_file unavailable", "bash unavailable", "enable tools",
	}
	mentionsToolBoundary := strings.Contains(lower, "write_file") ||
		strings.Contains(lower, "edit_file") ||
		strings.Contains(lower, "bash") ||
		strings.Contains(lower, "\u5de5\u5177")
	if !mentionsToolBoundary {
		return false
	}
	return containsAnyWorkflowReviewMarker(lower, blockedMarkers)
}

func taskBreakdownValidForOrchestrator(engine *workflow.WorkflowEngine, userID, text string, tasks []*TaskItem) bool {
	if len(tasks) == 0 {
		return false
	}
	if !codingWorkflowActive(engine, userID) {
		return true
	}
	return isExecutableCodingTaskBreakdown(text, tasks)
}

func codingWorkflowActive(engine *workflow.WorkflowEngine, userID string) bool {
	if engine == nil {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	return ws != nil && ws.Type == workflow.WorkflowCoding
}

func codingTaskBreakdownHasRequiredFields(text string, expectedTasks int) bool {
	sections := tNumberedTaskSections(text)
	if len(sections) != expectedTasks || len(sections) == 0 {
		return false
	}
	for _, section := range sections {
		if !codingTaskSectionHasRequiredFields(section) {
			return false
		}
	}
	return true
}

func tNumberedTaskSections(text string) []string {
	var sections []string
	var current []string
	for _, line := range strings.Split(text, "\n") {
		if isMarkdownTNumberedTaskHeading(line) {
			if len(current) > 0 {
				sections = append(sections, strings.Join(current, "\n"))
			}
			current = []string{line}
			continue
		}
		if len(current) > 0 {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n"))
	}
	return sections
}

func codingTaskSectionHasRequiredFields(section string) bool {
	return containsAnyTaskField(section, "描述", "description") &&
		containsAnyTaskField(section, "涉及文件", "文件", "files", "file") &&
		containsAnyTaskField(section, "依赖", "depends", "dependency", "dependencies") &&
		containsAnyTaskField(section, "优先级", "priority") &&
		containsAnyTaskField(section, "工作量", "effort", "estimate")
}

func codingTaskDependenciesAreValid(text string, expectedTasks int) bool {
	sections := tNumberedTaskSections(text)
	if len(sections) != expectedTasks || expectedTasks <= 0 {
		return false
	}
	for sectionIndex, section := range sections {
		for _, rawLine := range strings.Split(section, "\n") {
			line := strings.TrimSpace(strings.TrimLeft(rawLine, "-* "))
			if !lineContainsTaskDependencyField(line) {
				continue
			}
			labels, found := extractTDependencyLabels(line)
			if !found {
				if dependencyLineDeclaresNone(line) {
					continue
				}
				return false
			}
			for _, label := range labels {
				if label <= 0 || label > expectedTasks || label >= sectionIndex+1 {
					return false
				}
			}
		}
	}
	return true
}

func lineContainsTaskDependencyField(line string) bool {
	lower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line), "*", ""))
	if lower == "" {
		return false
	}
	colon := strings.IndexAny(lower, ":：")
	for _, marker := range []string{"依赖", "depends", "dependency", "dependencies"} {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		if colon >= 0 {
			return idx <= colon
		}
		return idx <= 16
	}
	return false
}

func dependencyLineDeclaresNone(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "none") || strings.Contains(lower, "n/a") || strings.Contains(line, "无") || strings.Contains(line, "無")
}

func extractTDependencyLabels(line string) ([]int, bool) {
	lower := strings.ToLower(line)
	var labels []int
	found := false
	for i := 0; i < len(lower); i++ {
		if lower[i] != 't' || i+1 >= len(lower) || lower[i+1] < '0' || lower[i+1] > '9' {
			continue
		}
		j := i + 1
		label := 0
		for j < len(lower) && lower[j] >= '0' && lower[j] <= '9' {
			label = label*10 + int(lower[j]-'0')
			j++
		}
		found = true
		labels = append(labels, label)
		i = j - 1
	}
	return labels, found
}

func containsAnyTaskField(text string, markers ...string) bool {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimLeft(rawLine, "-* "))
		line = strings.ToLower(strings.ReplaceAll(line, "*", ""))
		if line == "" {
			continue
		}
		colon := strings.IndexAny(line, ":：")
		for _, marker := range markers {
			idx := strings.Index(line, strings.ToLower(marker))
			if idx < 0 {
				continue
			}
			if colon >= 0 {
				if idx <= colon {
					return true
				}
				continue
			}
			if idx <= 16 {
				return true
			}
		}
	}
	return false
}

func hasSequentialTNumberedTaskHeadings(text string, expectedTasks int) bool {
	if expectedTasks <= 0 {
		return false
	}
	labels := tNumberedTaskHeadingLabels(text)
	if len(labels) != expectedTasks {
		return false
	}
	for i, label := range labels {
		if label != i+1 {
			return false
		}
	}
	return true
}

func tNumberedTaskHeadingLabels(text string) []int {
	var labels []int
	for _, line := range strings.Split(text, "\n") {
		if label, ok := tNumberedTaskHeadingLabel(line); ok {
			labels = append(labels, label)
		}
	}
	return labels
}

func isMarkdownTNumberedTaskHeading(line string) bool {
	_, ok := tNumberedTaskHeadingLabel(line)
	return ok
}

func tNumberedTaskHeadingLabel(line string) (int, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "### ") {
		return 0, false
	}
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "###"))
	if trimmed == "" {
		return 0, false
	}
	lower := strings.ToLower(trimmed)
	r, width := utf8DecodeRuneInString(lower)
	if r != 't' {
		return 0, false
	}
	rest := strings.TrimSpace(lower[width:])
	if rest == "" {
		return 0, false
	}
	label := 0
	pos := 0
	for pos < len(rest) {
		c := rest[pos]
		if c < '0' || c > '9' {
			break
		}
		label = label*10 + int(c-'0')
		pos++
	}
	if label == 0 || pos == 0 || pos >= len(rest) {
		return 0, false
	}
	runeAfter, _ := utf8DecodeRuneInString(rest[pos:])
	if !isTaskHeaderDelimiter(runeAfter) {
		return 0, false
	}
	return label, true
}

func currentCodingTaskBreakdownFeedsExecution(engine *workflow.WorkflowEngine, userID string) bool {
	if engine == nil {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.Type != workflow.WorkflowCoding || ws.CurrentPhase != workflow.PhaseCodingTaskBreakdown {
		return false
	}
	registry := engine.GetRegistry()
	if registry == nil {
		return false
	}
	tmpl := registry.Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex+1 >= len(tmpl.Phases) {
		return false
	}
	return tmpl.Phases[ws.PhaseIndex].ID == ws.CurrentPhase && workflow.IsTemplatePhaseExecutionOrchestrator(tmpl, tmpl.Phases[ws.PhaseIndex+1])
}

func currentCodingTaskBreakdownOutputBeforeExecution(engine *workflow.WorkflowEngine, userID string) (string, bool) {
	if engine == nil {
		return "", false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.PendingReviewPhaseID != ws.CurrentPhase || !currentCodingTaskBreakdownFeedsExecution(engine, userID) {
		return "", false
	}
	output := strings.TrimSpace(ws.PhaseOutputs[ws.CurrentPhase])
	if output == "" {
		return "", false
	}
	return output, true
}

func (h *IMMessageHandler) workflowExecutionProjectPath(engine *workflow.WorkflowEngine, userID string) string {
	if engine != nil {
		if ws := engine.GetActiveWorkflow(userID); ws != nil {
			if projectPath := strings.TrimSpace(ws.ProjectPath); projectPath != "" {
				return projectPath
			}
		}
	}
	if h != nil {
		if projectPath := strings.TrimSpace(h.workflowStartProjectPathForOwner(userID)); projectPath != "" {
			return projectPath
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return strings.TrimSpace(home)
	}
	return "."
}
