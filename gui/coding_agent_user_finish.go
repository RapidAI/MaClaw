package main

import (
	"fmt"
	"regexp"
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

var codingAgentPlanStepHeading = regexp.MustCompile(`(?m)^### T\d+`)

var codingAgentUserHiddenHeadings = []string{
	"## \u8d28\u91cf\u5ba1\u8ba1", "## \u9a8c\u8bc1\u72b6\u6001", "## \u63a2\u7d22\u72b6\u6001", "## Diff \u81ea\u68c0", "## \u6587\u4ef6\u53d8\u66f4",
	"## \u5b89\u5168\u8fb9\u754c", "## \u547d\u4ee4\u9a8c\u8bc1", "## \u52a8\u6001\u5de5\u5177", "## \u6b65\u9aa4\u6e05\u5355",
	"## \u8fdc\u7a0b Diff \u81ea\u68c0", "## \u786e\u8ba4\u72b6\u6001", "## \u547d\u4ee4\u72b6\u6001", "## \u65e0\u6539\u52a8\u8bc1\u636e",
	"## \u9a8c\u8bc1\u7ed3\u679c", "## \u6d89\u53ca\u6587\u4ef6", "## \u6458\u8981", "## \u9a8c\u8bc1\u547d\u4ee4",
	"**\u9a8c\u8bc1\u7ed3\u679c**", "**\u6d89\u53ca\u6587\u4ef6**", "**\u6458\u8981**",
	"## Execution report", "## \u6267\u884c\u62a5\u544a", "## \u57f7\u884c\u5831\u544a",
	"## Verification", "## Involved files", "## Files involved", "## Summary",
	"## Coding Task Execution Report", "## Coding Execution Report", "## Execution Stats",
	"### Failed Tasks",
	"### \u8ba1\u5212\u6267\u884c\u7ed3\u679c",
	"## \u6309\u5df2\u6279\u51c6\u8ba1\u5212\u6267\u884c",
	"\u6267\u884c\u6b65\u9aa4\uff1a",
}

func formatCodingAgentUserFinish(results []v2.TaskRunResult, cancelled bool) string {
	if len(results) == 0 {
		if cancelled {
			return codingExecText("Stopped before any coding step ran.", "Stopped before any coding step ran.", "Stopped before any coding step ran.")
		}
		return ""
	}
	parts := make([]string, 0, len(results)+2)
	if cancelled {
		parts = append(parts, codingExecText("Stopped before finishing.", "Stopped before finishing.", "Stopped before finishing."))
	}
	passed, failed := 0, 0
	for _, result := range results {
		switch result.Status {
		case v2.TaskPassed:
			passed++
		case v2.TaskFailed:
			failed++
		}
		if paragraph := formatCodingAgentResultParagraph(result); paragraph != "" {
			parts = append(parts, paragraph)
		}
	}
	if !cancelled && len(results) > 1 && failed > 0 && passed > 0 {
		parts = append(parts, fmt.Sprintf(codingExecText(
			"Finished %d of %d steps; %d still failed.",
			"Finished %d of %d steps; %d still failed.",
			"Finished %d of %d steps; %d still failed.",
		), passed, len(results), failed))
	}
	return strings.Join(parts, "\n\n")
}

func formatCodingAgentResultParagraph(result v2.TaskRunResult) string {
	summary := stripCodingAgentAuditSections(result.Summary)
	err := formatCodingAgentVisibleError(result.Error)
	title := strings.TrimSpace(compactSubAgentTaskTitle(result.Title))
	files := formatCodingAgentTouchedFiles(result.FilesCreated, result.FilesModified)
	switch result.Status {
	case v2.TaskFailed:
		if err != "" && codingAgentSummaryLooksSuccessful(summary) {
			if files != "" {
				return files + "\n\n" + err
			}
			return err
		}
		if summary != "" {
			if err != "" && !strings.Contains(summary, err) && !strings.Contains(summary, result.Error) {
				return summary + "\n\n" + err
			}
			return summary
		}
		if files != "" && err != "" {
			return files + "\n\n" + err
		}
		if title == "" {
			title = codingExecText("The step", "The step", "The step")
		}
		if err != "" {
			return fmt.Sprintf("%s did not finish.\n\n%s", title, err)
		}
		return title + " did not finish."
	case v2.TaskSkipped:
		if summary != "" {
			return summary
		}
		if title == "" {
			return codingExecText("Skipped this step.", "Skipped this step.", "Skipped this step.")
		}
		if err != "" {
			return fmt.Sprintf("Skipped %s: %s", title, err)
		}
		return fmt.Sprintf("Skipped %s.", title)
	default:
		if summary != "" {
			return summary
		}
		if files != "" {
			return files
		}
		if title != "" {
			return fmt.Sprintf("Finished %s.", title)
		}
		return codingExecText("Finished.", "Finished.", "Finished.")
	}
}

func formatCodingWorkbenchUserAnswer(kind codingRequestKind, results []v2.TaskRunResult, cancelled bool) string {
	finish := strings.TrimSpace(formatCodingAgentUserFinish(results, cancelled))
	if kind != codingRequestInquiry {
		return finish
	}
	note := codingExecText("Read-only check: no files were modified.", "Read-only check: no files were modified.", "Read-only check: no files were modified.")
	if finish == "" {
		return note
	}
	if strings.Contains(finish, note) {
		return finish
	}
	return finish + "\n\n" + note
}

func codingAgentFinishNeedsToken(finish string, _ *CodingSubAgentResult, _ []v2.TaskRunResult, _ bool) bool {
	// Mid-turn model tokens go to the collapsed thinking pane. The visible
	// answer is this finish text, even when it matches Summary.
	return strings.TrimSpace(finish) != ""
}

func emitCodingAgentFinishToken(onToken func(string), report string, results []v2.TaskRunResult, cancelled bool) {
	if onToken == nil || !codingAgentFinishNeedsToken(report, nil, results, cancelled) {
		return
	}
	onToken("\n\n" + report)
}

func formatCodingAgentVisibleError(err string) string {
	err = strings.TrimSpace(compactSubAgentErrorSummary(err))
	if err == "" {
		return ""
	}
	stripped := stripCodingAgentAuditErrorPrefix(err)
	if detail, ok := codingAgentFailedCommandDetail(stripped); ok {
		return formatCodingAgentFailedCommand(detail)
	}
	if stripped == "" {
		return codingExecText("Checks did not pass.", "Checks did not pass.", "Checks did not pass.")
	}
	return stripped
}

func stripCodingAgentAuditErrorPrefix(err string) string {
	lower := strings.ToLower(strings.TrimSpace(err))
	err = strings.TrimSpace(err)
	for _, prefix := range []string{
		"coding subagent quality audit failed: ",
		"coding subagent quality audit failed",
		"quality audit failed: ",
		"quality audit failed",
	} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(err[len(prefix):])
		}
	}
	return err
}

func codingAgentFailedCommandDetail(err string) (string, bool) {
	lower := strings.ToLower(err)
	const marker = "command(s) failed:"
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return "", false
	}
	detail := strings.TrimSpace(err[idx+len(marker):])
	if detail == "" {
		return "", false
	}
	return detail, true
}

func formatCodingAgentFailedCommand(detail string) string {
	cmd, diag, found := strings.Cut(detail, " -> ")
	cmd = strings.TrimSpace(cmd)
	diag = strings.TrimSpace(diag)
	if cmd == "" {
		return codingExecText("A command failed.", "A command failed.", "A command failed.")
	}
	if found && diag != "" {
		return fmt.Sprintf("`%s` failed: %s", cmd, diag)
	}
	return fmt.Sprintf("`%s` failed.", cmd)
}

func codingAgentTextIsPlanApproval(text string) bool {
	return strings.Contains(text, "## \u9700\u8981\u786e\u8ba4\u6267\u884c\u8ba1\u5212") ||
		strings.Contains(text, "/plan approve")
}

func stripCodingAgentAuditSections(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if codingAgentTextIsPlanApproval(text) {
		return text
	}
	for _, heading := range codingAgentUserHiddenHeadings {
		if idx := strings.Index(text, heading); idx >= 0 {
			text = strings.TrimSpace(text[:idx])
		}
	}
	if loc := codingAgentPlanStepHeading.FindStringIndex(text); loc != nil {
		text = strings.TrimSpace(text[:loc[0]])
	}
	return text
}

func formatCodingSubAgentUserAnswer(result *CodingSubAgentResult) string {
	if result == nil {
		return ""
	}
	return formatCodingAgentResultParagraph(v2.TaskRunResult{
		Status:        taskExecStatusToRunResult(result.Status),
		Summary:       result.Summary,
		Error:         result.Error,
		FilesCreated:  result.FilesCreated,
		FilesModified: result.FilesModified,
	})
}

func codingAgentSummaryLooksSuccessful(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	lower := strings.ToLower(summary)
	if strings.Contains(lower, "fail") ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "incomplete") ||
		strings.Contains(summary, "\u5931\u8d25") ||
		strings.Contains(summary, "\u672a\u5b8c\u6210") ||
		strings.Contains(summary, "\u9519\u8bef") ||
		strings.Contains(summary, "\u65e0\u6cd5") {
		return false
	}
	return strings.Contains(lower, "success") ||
		strings.Contains(lower, "passed") ||
		strings.Contains(lower, "compiled") ||
		strings.Contains(lower, "complete") ||
		strings.Contains(summary, "\u6210\u529f") ||
		strings.Contains(summary, "\u5df2\u5b8c\u6210") ||
		strings.Contains(summary, "\u7f16\u8bd1\u901a\u8fc7") ||
		strings.Contains(summary, "\u7f16\u8bd1\u8fd0\u884c")
}

func formatCodingAgentTouchedFiles(created, modified []string) string {
	created = uniqueSortedSubAgentStrings(created)
	modified = uniqueSortedSubAgentStrings(modified)
	if len(created) == 0 && len(modified) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if len(created) > 0 {
		parts = append(parts, fmt.Sprintf("Created %s.", compactSubAgentFileList(created, codingSubAgentFileChangeSummaryMax)))
	}
	if existing := codingAgentExistingModifiedFiles(modified, created); len(existing) > 0 {
		parts = append(parts, fmt.Sprintf("Updated %s.", compactSubAgentFileList(existing, codingSubAgentFileChangeSummaryMax)))
	}
	return strings.Join(parts, " ")
}

func codingAgentExistingModifiedFiles(modified, created []string) []string {
	createdSet := make(map[string]bool, len(created))
	for _, path := range created {
		createdSet[path] = true
	}
	var existing []string
	for _, path := range modified {
		if !createdSet[path] {
			existing = append(existing, path)
		}
	}
	return existing
}

func formatCodingAgentRunnerFinish(o *TaskExecutionOrchestrator, notes []string) string {
	if o == nil {
		return strings.Join(codingAgentRunnerNotes(notes), "\n\n")
	}
	finish := strings.TrimSpace(o.FinalReport())
	extra := codingAgentRunnerNotes(notes)
	if finish == "" {
		return strings.Join(extra, "\n\n")
	}
	var unseen []string
	for _, note := range extra {
		if !strings.Contains(finish, note) {
			unseen = append(unseen, note)
		}
	}
	if len(unseen) == 0 {
		return finish
	}
	return finish + "\n\n" + strings.Join(unseen, "\n\n")
}

func codingAgentRunnerNotes(notes []string) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" || !codingAgentRunnerNoteVisible(note) {
			continue
		}
		out = append(out, note)
	}
	return out
}

func codingAgentRunnerNoteVisible(note string) bool {
	lower := strings.ToLower(note)
	return strings.Contains(lower, "stopped after") ||
		strings.Contains(lower, "cancelled") ||
		// Transient provider pause ("paused to avoid retry storms") is a
		// user-actionable stop reason and must survive into the final report.
		strings.Contains(lower, "paused") ||
		strings.Contains(lower, "deadlock") ||
		strings.Contains(lower, "no runnable") ||
		strings.Contains(lower, "blocked by")
}

func taskItemsToCodingAgentResults(tasks []*TaskItem) []v2.TaskRunResult {
	results := make([]v2.TaskRunResult, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		results = append(results, v2.TaskRunResult{
			TaskIndex:     taskDisplayNumber(task),
			Title:         compactSubAgentTaskTitle(task.Title),
			Status:        taskExecStatusToRunResult(task.Status),
			Summary:       task.ResultSummary,
			Error:         task.ErrorSummary,
			FilesCreated:  append([]string{}, task.ActualCreatedFiles...),
			FilesModified: append([]string{}, task.ActualFiles...),
		})
	}
	return results
}

func remoteCodingStepToRunResult(step *v2.TaskItem, result *RemoteCodingSubAgentResult) v2.TaskRunResult {
	rr := v2.TaskRunResult{}
	if step != nil {
		rr.TaskIndex = step.Index
		rr.Title = compactSubAgentTaskTitle(step.Title)
	}
	if result == nil {
		rr.Status = v2.TaskFailed
		rr.Error = "nil result"
		return rr
	}
	switch result.Status {
	case "success":
		rr.Status = v2.TaskPassed
	case "cancelled":
		rr.Status = v2.TaskSkipped
		rr.Error = strings.TrimSpace(result.Error)
		if rr.Error == "" {
			rr.Error = "cancelled"
		}
	default:
		rr.Status = v2.TaskFailed
		rr.Error = result.Error
	}
	rr.Summary = result.Summary
	rr.FilesCreated = append([]string{}, result.FilesCreated...)
	rr.FilesModified = append([]string{}, result.FilesModified...)
	rr.RuntimeTaskID = result.RuntimeTaskID
	rr.RuntimeHandoff = result.RuntimeHandoff
	return rr
}

func appendCodingWorkbenchSkippedResults(results []v2.TaskRunResult, tasks []*v2.TaskItem, steps []codingWorkbenchStepStatus) []v2.TaskRunResult {
	seen := make(map[int]bool, len(results))
	for _, result := range results {
		seen[result.TaskIndex] = true
	}
	statusByIndex := make(map[int]codingWorkbenchStepStatus, len(steps))
	for _, step := range steps {
		statusByIndex[step.Index] = step
	}
	for _, task := range tasks {
		if task == nil || seen[task.Index] {
			continue
		}
		st := statusByIndex[task.Index]
		if st.Status != "" && st.Status != codingStepSkipped && st.Status != codingStepPending && st.Status != codingStepRunning {
			continue
		}
		summary := strings.TrimSpace(st.Summary)
		err := summary
		if err == "" {
			err = "skipped: prior step failed"
		}
		results = append(results, v2.TaskRunResult{
			TaskIndex: task.Index,
			Title:     compactSubAgentTaskTitle(task.Title),
			Status:    v2.TaskSkipped,
			Summary:   summary,
			Error:     err,
		})
	}
	return results
}

func taskExecStatusToRunResult(status TaskExecStatus) v2.TaskRunStatus {
	switch status {
	case TaskExecFailed:
		return v2.TaskFailed
	case TaskExecSkipped:
		return v2.TaskSkipped
	case TaskExecPassed:
		return v2.TaskPassed
	default:
		return v2.TaskSkipped
	}
}
