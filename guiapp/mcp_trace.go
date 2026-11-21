package guiapp

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	codingruntime "github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	mcpStepWaiting         = "waiting_review"
	mcpStatusWaitingReview = "waiting_review"

	mcpStopUser        = "user_stop"
	mcpStopTimeout     = "timeout"
	mcpStopProgram     = "program_error"
	mcpStopDependency  = "dependency_failure"
	mcpStopReview      = "waiting_review"
	mcpStopUnconfirmed = "unconfirmed"
	// mcpStopNoProgress is a confirmed stall: the round produced no
	// host-visible activity for mcpRunStallWindow. It is not a user stop
	// and not an unconfirmed host-wait ending.
	mcpStopNoProgress = "no_progress"

	mcpRunArchiveLimit   = 8
	mcpChildPendingLimit = 64
)

// mcpChildView is one internal coding task on get_task.
// Status admitted is acceptance only. It is not execution completion.
type mcpChildView struct {
	ChildTaskID   string `json:"child_task_id"`
	ParentTaskID  string `json:"parent_task_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Status        string `json:"status"`
	Progress      string `json:"progress,omitempty"`
	Result        string `json:"result,omitempty"`
	ReviewPending bool   `json:"review_pending,omitempty"`
	Review        string `json:"review,omitempty"`
}

// mcpStopView is why a run stopped. Status alone is not this record:
// cancelled and "Stopped before finishing" do not say who stopped it.
type mcpStopView struct {
	Class    string `json:"class"`
	Source   string `json:"source"`
	Reason   string `json:"reason"`
	At       string `json:"at"`
	StepID   string `json:"step_id,omitempty"`
	Deadline string `json:"deadline,omitempty"`
}

func mcpNewRunID() string {
	return "run-" + randomHexID(8)
}

func mcpPresentSteps(steps []mcpStepView) []mcpStepView {
	if len(steps) == 0 {
		return nil
	}
	out := mcpCloneSteps(steps)
	for i := range out {
		out[i] = mcpPresentStep(out[i])
	}
	return out
}

// mcpPresentStep turns a child-review handoff into waiting. The stored
// runner status is often only skipped; that is not a failure and not an
// ordinary skip. A passed or failed row keeps that result. Leftover handoff
// text in its note is not a second review.
func mcpPresentStep(step mcpStepView) mcpStepView {
	status := strings.TrimSpace(step.Status)
	if mcpSkipIsApprovalWait(step.Error) && status != mcpStepPassed && status != mcpStepFailed && status != mcpStepWaiting {
		step.Status = mcpStatusNeedsConfirmation
		step.Retryable = false
		if strings.TrimSpace(step.Error) == "" {
			step.Error = mcpApprovalWaitErrorText
		}
		return step
	}
	handoff := status == mcpStepWaiting ||
		((status == codingStepSkipped || status == "") && mcpSkipIsReviewHandoff(step.Error))
	if !handoff {
		return step
	}
	step.Status = mcpStepWaiting
	step.Retryable = false
	if strings.TrimSpace(step.Error) == "" {
		step.Error = mcpReviewHandoffErrorText
	}
	return step
}

// mcpAnnotateBlocks names, for every step that did not run, who blocked it
// and the root cause. A review wait stays waiting.
func mcpAnnotateBlocks(steps []mcpStepView) []mcpStepView {
	if len(steps) == 0 {
		return steps
	}
	for i := range steps {
		// A runner cancellation is stored as an ordinary skip. The agent
		// step list classifies that same row as unfinished before this
		// gate runs. Neither status blocks as a user stop, so the plan
		// looks finished or like a program error.
		status := steps[i].Status
		if (status == codingStepSkipped || status == mcpStepUnfinished) && isCodingExecCancelError(steps[i].Error) {
			steps[i].Status = codingStepInterrupted
		}
	}
	steps = mcpPresentSteps(steps)
	rootID, rootReason, rootClass := mcpBlockRoot(steps)
	for i := range steps {
		step := &steps[i]
		if step.Status == mcpStepPassed || step.Status == mcpStepFailed {
			continue
		}
		switch step.Status {
		case mcpStepWaiting:
			step.BlockReason = firstNonEmpty(strings.TrimSpace(step.Error), mcpReviewHandoffErrorText)
			if id := strings.TrimSpace(step.ChildTaskID); id != "" {
				step.BlockedBy = []string{id}
			}
		default:
			if rootID != "" && !strings.EqualFold(rootID, step.ID) {
				step.BlockedBy = []string{rootID}
			}
			step.BlockReason = mcpUnexecutedReason(*step, rootReason, rootClass)
		}
	}
	return steps
}

func mcpBlockRoot(steps []mcpStepView) (id, reason, class string) {
	for _, step := range steps {
		if step.Status != mcpStepFailed {
			continue
		}
		return step.ID, strings.TrimSpace(step.Error), mcpStopProgram
	}
	for _, step := range steps {
		if step.Status != mcpStepWaiting && !mcpSkipIsReviewHandoff(step.Error) {
			continue
		}
		return step.ID, firstNonEmpty(strings.TrimSpace(step.Error), mcpReviewHandoffErrorText), mcpStopReview
	}
	return "", "", ""
}

func mcpUnexecutedReason(step mcpStepView, rootReason, rootClass string) string {
	if rootClass == mcpStopReview {
		return "等待子任务审阅"
	}
	if reason := strings.TrimSpace(step.Error); reason != "" && !mcpSkipWaitsOnPrior(reason) && !mcpSkipIsReviewHandoff(reason) {
		return reason
	}
	if strings.TrimSpace(rootReason) != "" {
		return rootReason
	}
	if mcpSkipWaitsOnPrior(step.Error) {
		return "依赖的步骤还没有通过"
	}
	return "这一步还没有执行"
}

// mcpStepsAwaitApprovalOnly reports a run parked on scope approval.
// Passed rows may sit beside it. A failure, a cancellation, or a child
// review is a different stop and must not be flattened into this pause.
func mcpStepsAwaitApprovalOnly(steps []mcpStepView) bool {
	saw := false
	for _, step := range steps {
		status := strings.TrimSpace(step.Status)
		switch status {
		case mcpStepPassed:
			continue
		case mcpStatusNeedsConfirmation:
			saw = true
		case mcpStepSkipped:
			if !mcpSkipIsApprovalWait(step.Error) {
				return false
			}
			saw = true
		case "":
			return false
		default:
			return false
		}
	}
	return saw
}

func mcpStepsAwaitReviewOnly(steps []mcpStepView) bool {
	saw := false
	for _, step := range steps {
		switch strings.TrimSpace(step.Status) {
		case mcpStepPassed:
			continue
		case mcpStepSkipped:
			continue
		case mcpStepWaiting:
			saw = true
		case "":
			return false
		default:
			return false
		}
	}
	return saw
}

// mcpApplyStepGate is the one completion rule. A necessary step that has not
// passed keeps the task unfinished. A review wait is waiting_review, not
// failed and not completed.
func mcpApplyStepGate(status, errText string, steps []mcpStepView) (string, string, []mcpStepView) {
	steps = mcpAnnotateBlocks(steps)
	switch status {
	case mcpStatusCancelled, mcpStatusNeedsConfirmation, mcpStatusRunning, mcpStatusIdle:
		return status, errText, steps
	}
	if mcpStepsAwaitApprovalOnly(steps) {
		return mcpStatusNeedsConfirmation, mcpApprovalWaitErrorText, steps
	}
	if mcpStepsAwaitReviewOnly(steps) {
		if status == mcpStatusFailed && strings.TrimSpace(errText) != "" && !mcpSkipIsReviewHandoff(errText) && errText != "有步骤失败或未完成" {
			return mcpStatusFailed, errText, steps
		}
		return mcpStatusWaitingReview, mcpReviewHandoffErrorText, steps
	}
	if status == mcpStatusCompleted && mcpStepsStoppedByCancel(steps) {
		return mcpStatusCancelled, "", steps
	}
	if status == mcpStatusCompleted && mcpStepsBlockCompletion(steps) {
		status = mcpStatusFailed
		if strings.TrimSpace(errText) == "" {
			errText = mcpStepFailureDetail(steps, "", nil)
		}
	}
	if status == mcpStatusWaitingReview && mcpStepsBlockCompletion(steps) && !mcpStepsAwaitReviewOnly(steps) {
		status = mcpStatusFailed
		if strings.TrimSpace(errText) == "" {
			errText = mcpStepFailureDetail(steps, "", nil)
		}
	}
	if status == mcpStatusFailed && strings.TrimSpace(errText) == "" {
		errText = mcpStepFailureDetail(steps, "", nil)
	}
	return status, errText, steps
}

func mcpStopFor(status, errText string, steps []mcpStepView, cause error, at time.Time) *mcpStopView {
	if status == mcpStatusCompleted || status == mcpStatusRunning || status == mcpStatusIdle || status == mcpStatusNeedsConfirmation || status == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	stop := &mcpStopView{At: mcpFormatTime(at), Reason: strings.TrimSpace(errText)}
	switch {
	case errors.Is(cause, errMCPRunStalled):
		stop.Class = mcpStopNoProgress
		stop.Source = "no_progress"
		stop.StepID = mcpOpenStepID(steps)
		stop.Reason = errMCPRunStalled.Error()
		stop.Deadline = mcpFormatTime(at)
	case errors.Is(cause, errMCPHostWaitEnded):
		stop.Class = mcpStopUnconfirmed
		stop.Source = "host_wait"
		stop.StepID = mcpOpenStepID(steps)
		stop.Reason = errMCPHostWaitEnded.Error()
	case errors.Is(cause, context.DeadlineExceeded):
		stop.Class = mcpStopTimeout
		stop.Source = "timeout"
		stop.Deadline = mcpFormatTime(at)
		stop.StepID = mcpOpenStepID(steps)
		if stop.Reason == "" {
			stop.Reason = "执行超时"
		}
	case status == mcpStatusCancelled || errors.Is(cause, context.Canceled):
		stop.Class = mcpStopUser
		stop.Source = "user"
		stop.StepID = mcpOpenStepID(steps)
		if stop.Reason == "" {
			stop.Reason = "用户停止"
		}
	case status == mcpStatusWaitingReview || mcpStepsAwaitReviewOnly(steps):
		stop.Class = mcpStopReview
		stop.Source = "review"
		stop.StepID = mcpFirstStepID(steps, mcpStepWaiting)
		if stop.Reason == "" {
			stop.Reason = mcpReviewHandoffErrorText
		}
	case mcpDependencyStop(steps):
		stop.Class = mcpStopDependency
		stop.Source = "dependency"
		stop.StepID = mcpBlockedStepID(steps)
		if stop.Reason == "" {
			stop.Reason = mcpReasonForStep(steps, stop.StepID)
		}
		if stop.Reason == "" {
			stop.Reason = "依赖的步骤还没有通过"
		}
	default:
		stop.Class = mcpStopProgram
		stop.Source = "program"
		stop.StepID = mcpFirstStepID(steps, mcpStepFailed)
		if stop.StepID == "" {
			stop.StepID = mcpOpenStepID(steps)
		}
		if stop.Reason == "" {
			stop.Reason = mcpStepFailureDetail(steps, "", nil)
		}
	}
	if stop.Class == mcpStopTimeout && strings.TrimSpace(stop.Deadline) == "" {
		stop.Class = mcpStopUnconfirmed
		stop.Source = "host_wait"
	}
	return stop
}

func mcpDependencyStop(steps []mcpStepView) bool {
	saw := false
	for _, step := range steps {
		switch step.Status {
		case mcpStepFailed, mcpStepWaiting:
			return false
		}
		if len(step.BlockedBy) > 0 || mcpSkipWaitsOnPrior(step.Error) {
			saw = true
		}
	}
	return saw
}

func mcpFirstStepID(steps []mcpStepView, status string) string {
	for _, step := range steps {
		if step.Status == status && strings.TrimSpace(step.ID) != "" {
			return step.ID
		}
	}
	return ""
}

func mcpOpenStepID(steps []mcpStepView) string {
	for _, step := range steps {
		if step.Status == mcpStepPassed || step.Status == mcpStepSkipped {
			continue
		}
		if id := strings.TrimSpace(step.ID); id != "" {
			return id
		}
	}
	return ""
}

func mcpBlockedStepID(steps []mcpStepView) string {
	for _, step := range steps {
		if len(step.BlockedBy) > 0 && strings.TrimSpace(step.ID) != "" {
			return step.ID
		}
	}
	return mcpOpenStepID(steps)
}

func mcpReasonForStep(steps []mcpStepView, id string) string {
	for _, step := range steps {
		if strings.EqualFold(step.ID, id) {
			return firstNonEmpty(strings.TrimSpace(step.BlockReason), strings.TrimSpace(step.Error))
		}
	}
	return ""
}

func mcpChildrenFromSteps(steps []mcpStepView) []mcpChildView {
	var out []mcpChildView
	seen := map[string]struct{}{}
	for _, step := range steps {
		for _, child := range step.Children {
			id := strings.TrimSpace(child.ChildTaskID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, child)
		}
		if id := strings.TrimSpace(step.ChildTaskID); id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, mcpChildView{
				ChildTaskID:   id,
				Status:        "admitted",
				Progress:      "admitted",
				ReviewPending: true,
				Review:        mcpReviewHandoffErrorText,
			})
		}
	}
	return out
}

func mcpMergeChildren(prev, next []mcpChildView) []mcpChildView {
	if len(next) == 0 {
		return mcpCloneChildren(prev)
	}
	byID := map[string]mcpChildView{}
	var order []string
	add := func(child mcpChildView) {
		id := strings.TrimSpace(child.ChildTaskID)
		if id == "" {
			return
		}
		child.ChildTaskID = id
		if old, ok := byID[id]; ok {
			byID[id] = mcpPreferChild(old, child)
			return
		}
		byID[id] = child
		order = append(order, id)
	}
	for _, child := range prev {
		add(child)
	}
	for _, child := range next {
		add(child)
	}
	out := make([]mcpChildView, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

func mcpPreferChild(prev, next mcpChildView) mcpChildView {
	// A step id re-publishes the admission row. That row is not a newer result.
	if mcpChildIsAdmission(next) && !mcpChildIsAdmission(prev) {
		if strings.TrimSpace(prev.Name) == "" {
			prev.Name = strings.TrimSpace(next.Name)
		}
		if strings.TrimSpace(prev.ParentTaskID) == "" {
			prev.ParentTaskID = strings.TrimSpace(next.ParentTaskID)
		}
		return prev
	}
	if strings.TrimSpace(next.Result) != "" {
		prev.Result = next.Result
	}
	if strings.TrimSpace(next.Review) != "" {
		prev.Review = next.Review
	}
	if strings.TrimSpace(next.Name) != "" {
		prev.Name = next.Name
	}
	if strings.TrimSpace(next.ParentTaskID) != "" {
		prev.ParentTaskID = next.ParentTaskID
	}
	if next.Status != "" && next.Status != "admitted" {
		prev.Status = next.Status
		prev.Progress = firstNonEmpty(next.Progress, next.Status)
	}
	prev.ReviewPending = next.ReviewPending || prev.ReviewPending
	if prev.Status == "completed" || prev.Status == "failed" || prev.Status == "cancelled" {
		prev.ReviewPending = true
	}
	return prev
}

func mcpChildIsAdmission(child mcpChildView) bool {
	switch strings.TrimSpace(child.Status) {
	case "", "admitted", string(codingruntime.TaskQueued):
		return strings.TrimSpace(child.Result) == ""
	default:
		return false
	}
}

func mcpCloneChildren(children []mcpChildView) []mcpChildView {
	if len(children) == 0 {
		return nil
	}
	return append([]mcpChildView(nil), children...)
}

func mcpPublishChildren(view mcpTaskView) mcpTaskView {
	view.Children = mcpMergeChildren(view.Children, mcpChildrenFromSteps(view.Steps))
	if len(view.Children) > 0 && strings.TrimSpace(view.ChildTaskID) == "" {
		view.ChildTaskID = view.Children[0].ChildTaskID
	}
	// The step keeps its own child row from admission. Publish projects the
	// same child the task list already has, so the two copies cannot disagree.
	if len(view.Steps) > 0 && len(view.Children) > 0 {
		view.Steps = mcpProjectChildrenOntoSteps(view.Steps, view.Children)
	}
	return view
}

func mcpProjectChildrenOntoSteps(steps []mcpStepView, children []mcpChildView) []mcpStepView {
	byID := make(map[string]mcpChildView, len(children))
	for _, child := range children {
		id := strings.TrimSpace(child.ChildTaskID)
		if id == "" {
			continue
		}
		byID[id] = child
	}
	for i := range steps {
		var matched []mcpChildView
		seen := map[string]struct{}{}
		consider := func(id string) {
			id = strings.TrimSpace(id)
			if id == "" {
				return
			}
			if _, ok := seen[id]; ok {
				return
			}
			seen[id] = struct{}{}
			if child, ok := byID[id]; ok {
				matched = append(matched, child)
			}
		}
		consider(steps[i].ChildTaskID)
		for _, child := range steps[i].Children {
			consider(child.ChildTaskID)
		}
		if len(matched) == 0 {
			continue
		}
		steps[i].Children = mcpMergeChildren(steps[i].Children, matched)
	}
	return steps
}

func mcpChildrenFromToolText(text string) []mcpChildView {
	var out []mcpChildView
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "child_task_id=") {
			continue
		}
		child := mcpChildView{
			ChildTaskID:   mcpToolField(line, "child_task_id"),
			ParentTaskID:  mcpToolField(line, "parent_task_id"),
			Name:          mcpToolField(line, "name"),
			Status:        mcpToolField(line, "status"),
			ReviewPending: true,
		}
		if child.ChildTaskID == "" {
			continue
		}
		if child.Status == "" || child.Status == string(codingruntime.TaskQueued) {
			child.Status = "admitted"
		}
		child.Progress = child.Status
		child.ReviewPending = child.Status != "failed" && child.Status != "cancelled"
		child.Review = mcpReviewHandoffErrorText
		out = append(out, child)
	}
	return out
}

func mcpToolField(line, key string) string {
	token := key + "="
	idx := strings.Index(line, token)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(token):]
	cut := len(rest)
	for _, next := range []string{" child_task_id=", " parent_task_id=", " name=", " status="} {
		if next == " "+token {
			continue
		}
		if i := strings.Index(rest, next); i >= 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(rest[:cut])
}

func mcpChildrenFromRun(children []v2.ChildTaskView) []mcpChildView {
	if len(children) == 0 {
		return nil
	}
	out := make([]mcpChildView, 0, len(children))
	for _, child := range children {
		if strings.TrimSpace(child.TaskID) == "" {
			continue
		}
		status := strings.TrimSpace(child.Status)
		if status == "" || status == string(codingruntime.TaskQueued) {
			status = "admitted"
		}
		out = append(out, mcpChildView{
			ChildTaskID:   child.TaskID,
			ParentTaskID:  child.ParentTaskID,
			Name:          child.Name,
			Status:        status,
			Progress:      firstNonEmpty(child.Progress, status),
			Result:        child.Result,
			ReviewPending: child.ReviewPending || (status != "failed" && status != "cancelled"),
			Review:        firstNonEmpty(child.Review, child.Result),
		})
	}
	return out
}

func codingChildPublicStatus(status codingruntime.TaskStatus) string {
	switch status {
	case "", codingruntime.TaskQueued:
		return "admitted"
	case codingruntime.TaskRunning:
		return "running"
	case codingruntime.TaskWaitingChild, codingruntime.TaskWaitingApproval:
		return mcpStatusWaitingReview
	case codingruntime.TaskCompleted:
		return mcpStatusCompleted
	case codingruntime.TaskFailed, codingruntime.TaskBlocked:
		return mcpStatusFailed
	case codingruntime.TaskCancelled:
		return mcpStatusCancelled
	case codingruntime.TaskInterrupted:
		return "interrupted"
	default:
		return string(status)
	}
}

func codingLedgerChildren(store codingruntime.Store, parentID string) []v2.ChildTaskView {
	parentID = strings.TrimSpace(parentID)
	if store == nil || parentID == "" {
		return nil
	}
	tasks, err := store.ListChildTasks(parentID)
	if err != nil || len(tasks) == 0 {
		return nil
	}
	results, _ := store.ListChildResults(parentID)
	byID := make(map[string]codingruntime.ChildTaskResult, len(results))
	for _, result := range results {
		byID[result.TaskID] = result
	}
	out := make([]v2.ChildTaskView, 0, len(tasks))
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.TaskID) == "" {
			continue
		}
		status := codingChildPublicStatus(task.Status)
		view := v2.ChildTaskView{
			TaskID:        task.TaskID,
			ParentTaskID:  task.ParentTaskID,
			Name:          strings.TrimSpace(task.RequestedWork),
			Status:        status,
			Progress:      status,
			ReviewPending: true,
		}
		if result, ok := byID[task.TaskID]; ok {
			view.Result = strings.TrimSpace(result.Summary)
			view.Review = view.Result
		}
		if view.Review == "" {
			view.Review = view.Name
		}
		out = append(out, view)
	}
	return out
}

func mcpStepsFromWorkbench(steps []codingWorkbenchStepStatus) []mcpStepView {
	if len(steps) == 0 {
		return nil
	}
	out := make([]mcpStepView, 0, len(steps))
	for _, step := range steps {
		status := strings.TrimSpace(step.Status)
		errText := strings.TrimSpace(step.Summary)
		if mcpSkipIsApprovalWait(errText) && status != mcpStepPassed && status != mcpStepFailed && status != mcpStepWaiting {
			status = mcpStatusNeedsConfirmation
		}
		if status == codingStepSkipped && mcpSkipIsReviewHandoff(errText) {
			status = mcpStepWaiting
		}
		if status == mcpStepWaiting && errText == "" {
			errText = mcpReviewHandoffErrorText
		}
		id := ""
		if step.Index > 0 {
			id = "T" + strconv.Itoa(step.Index)
		}
		out = append(out, mcpStepView{
			ID:          id,
			Title:       step.Title,
			Status:      status,
			Error:       errText,
			ChildTaskID: strings.TrimSpace(step.ChildTaskID),
			BlockedBy:   append([]string(nil), step.BlockedBy...),
			BlockReason: step.BlockReason,
		})
	}
	return out
}

func annotateWorkbenchBlocks(steps []codingWorkbenchStepStatus) []codingWorkbenchStepStatus {
	if len(steps) == 0 {
		return steps
	}
	views := mcpAnnotateBlocks(mcpStepsFromWorkbench(steps))
	if len(views) != len(steps) {
		return steps
	}
	out := append([]codingWorkbenchStepStatus(nil), steps...)
	for i := range out {
		out[i].BlockedBy = append([]string(nil), views[i].BlockedBy...)
		out[i].BlockReason = views[i].BlockReason
		if views[i].ChildTaskID != "" {
			out[i].ChildTaskID = views[i].ChildTaskID
		}
		if views[i].Status == mcpStepWaiting {
			out[i].Status = mcpStepWaiting
		}
		if views[i].Status == codingStepInterrupted && strings.TrimSpace(out[i].Status) == codingStepSkipped && isCodingExecCancelError(out[i].Summary) {
			out[i].Status = codingStepInterrupted
		}
	}
	return out
}

func mcpExecutionFromWorkbench(steps []codingWorkbenchStepStatus) (string, *mcpStopView) {
	if len(steps) == 0 {
		return "", nil
	}
	started := false
	running := false
	pendingLeft := false
	var startedSteps []codingWorkbenchStepStatus
	for _, step := range steps {
		switch strings.TrimSpace(step.Status) {
		case "", codingStepPending:
			pendingLeft = true
		case codingStepRunning:
			running = true
			started = true
			startedSteps = append(startedSteps, step)
		default:
			started = true
			startedSteps = append(startedSteps, step)
		}
	}
	if !started {
		return "", nil
	}
	if running {
		return mcpStatusRunning, nil
	}
	views := mcpStepsFromWorkbench(startedSteps)
	status, errText, views := mcpApplyStepGate(mcpStatusCompleted, "", views)
	if status == mcpStatusCompleted && pendingLeft {
		return mcpStatusRunning, nil
	}
	if status == mcpStatusFailed && pendingLeft && !mcpWorkbenchHasHardFailure(startedSteps) {
		return mcpStatusRunning, nil
	}
	return status, mcpStopFor(status, errText, views, nil, time.Now())
}

func mcpWorkbenchHasHardFailure(steps []codingWorkbenchStepStatus) bool {
	for _, step := range steps {
		switch strings.TrimSpace(step.Status) {
		case codingStepFailed, codingStepVerifyFail, mcpStepUnfinished:
			return true
		}
	}
	return false
}

func firstChildTaskID(result *CodingSubAgentResult) string {
	if result == nil || len(result.Children) == 0 {
		return ""
	}
	return strings.TrimSpace(result.Children[0].TaskID)
}

func cloneChildTaskViews(children []v2.ChildTaskView) []v2.ChildTaskView {
	if len(children) == 0 {
		return nil
	}
	return append([]v2.ChildTaskView(nil), children...)
}

func mcpCloneStop(stop *mcpStopView) *mcpStopView {
	if stop == nil {
		return nil
	}
	copy := *stop
	return &copy
}

func mcpCloneRun(rec *mcpRunRecord) *mcpRunRecord {
	if rec == nil {
		return nil
	}
	clone := *rec
	clone.cancel = nil
	clone.progressAt = rec.progressAt
	clone.Commands = append([]string(nil), rec.Commands...)
	clone.logs = append([]mcpLogEvent(nil), rec.logs...)
	clone.Steps = mcpCloneSteps(rec.Steps)
	clone.Checks = mcpCloneChecks(rec.Checks)
	clone.Children = mcpCloneChildren(rec.Children)
	clone.Stop = mcpCloneStop(rec.Stop)
	return &clone
}

func mcpTouchLocked(rec *mcpRunRecord) {
	if rec == nil {
		return
	}
	rec.StatusVersion++
}

// mcpKnownChildren is the child list a later round can report without
// starting another parent attempt. Older rounds are merged first. The live
// run then overlays the same child id. An admission row does not replace a
// bounded result already recorded, and a child admitted on this round does
// not hide a different child from the review that is still open.
func (a *App) mcpKnownChildren(path string) []v2.ChildTaskView {
	if a == nil {
		return nil
	}
	path = normalizeProjectSessionPath(path)
	if path == "" {
		return nil
	}
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	var merged []mcpChildView
	for _, rec := range a.mcpRunArchive[path] {
		if rec == nil || len(rec.Children) == 0 {
			continue
		}
		merged = mcpMergeChildren(merged, rec.Children)
	}
	if rec := a.mcpRuns[path]; rec != nil && len(rec.Children) > 0 {
		merged = mcpMergeChildren(merged, rec.Children)
	}
	return mcpChildrenToRun(merged)
}

func mcpChildrenToRun(children []mcpChildView) []v2.ChildTaskView {
	if len(children) == 0 {
		return nil
	}
	out := make([]v2.ChildTaskView, 0, len(children))
	for _, child := range children {
		id := strings.TrimSpace(child.ChildTaskID)
		if id == "" {
			continue
		}
		out = append(out, v2.ChildTaskView{
			TaskID:        id,
			ParentTaskID:  child.ParentTaskID,
			Name:          child.Name,
			Status:        child.Status,
			Progress:      child.Progress,
			Result:        child.Result,
			ReviewPending: child.ReviewPending,
			Review:        child.Review,
		})
	}
	return out
}

func (a *App) mcpArchiveRunLocked(prev *mcpRunRecord) {
	if a == nil || prev == nil || strings.TrimSpace(prev.RunID) == "" {
		return
	}
	if a.mcpRunArchive == nil {
		a.mcpRunArchive = map[string][]*mcpRunRecord{}
	}
	path := prev.ProjectPath
	clone := mcpCloneRun(prev)
	list := a.mcpRunArchive[path]
	replaced := false
	for i, old := range list {
		if old != nil && old.RunID == clone.RunID {
			list[i] = clone
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, clone)
	}
	if len(list) > mcpRunArchiveLimit {
		list = append([]*mcpRunRecord(nil), list[len(list)-mcpRunArchiveLimit:]...)
	}
	a.mcpRunArchive[path] = list
}

func (a *App) mcpViewForRun(path, runID string) (mcpTaskView, bool) {
	if a == nil || strings.TrimSpace(runID) == "" {
		return mcpTaskView{}, false
	}
	path = normalizeProjectSessionPath(path)
	a.mcpRunsMu.Lock()
	defer a.mcpRunsMu.Unlock()
	if rec := a.mcpRuns[path]; rec != nil && rec.RunID == runID {
		return rec.view(), true
	}
	for _, old := range a.mcpRunArchive[path] {
		if old != nil && old.RunID == runID {
			return old.view(), true
		}
	}
	return mcpTaskView{}, false
}

func (a *App) mcpRunForReadLocked(path, runID string) (*mcpRunRecord, error) {
	rec := a.mcpRuns[path]
	if runID == "" {
		if rec == nil {
			return nil, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, "")
		}
		return rec, nil
	}
	if rec != nil && rec.RunID == runID {
		return rec, nil
	}
	for _, old := range a.mcpRunArchive[path] {
		if old != nil && old.RunID == runID {
			return old, nil
		}
	}
	if rec != nil {
		for _, ev := range rec.logs {
			if ev.RunID == runID {
				return rec, nil
			}
		}
	}
	if rec == nil && len(a.mcpRunArchive[path]) == 0 {
		return nil, mcpErr(mcpCodeTaskNotFound, "没有找到这个任务", path, runID)
	}
	return nil, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", path, runID)
}

func mcpLogMatchesRun(ev mcpLogEvent, rec *mcpRunRecord, runID string) bool {
	if runID == "" {
		return true
	}
	if ev.RunID == runID {
		return true
	}
	return ev.RunID == "" && rec != nil && rec.RunID == runID
}

func (a *App) mcpNoteAdmittedChildren(path, requestID, text string) {
	children := mcpChildrenFromToolText(text)
	if a == nil || len(children) == 0 {
		return
	}
	path = normalizeProjectSessionPath(path)
	requestID = strings.TrimSpace(requestID)
	persist := false
	a.mcpRunsMu.Lock()
	defer func() {
		if persist {
			a.mcpPersistRun(path)
		}
	}()
	defer a.mcpRunsMu.Unlock()
	rec := a.mcpRuns[path]
	if rec == nil || rec.RequestID != requestID || rec.Status != mcpStatusRunning {
		return
	}
	next := a.mcpOverlayPendingChildrenLocked(mcpMergeChildren(rec.Children, children))
	if mcpChildViewsEqual(rec.Children, next) {
		return
	}
	rec.Children = next
	if strings.TrimSpace(rec.ChildTaskID) == "" && len(rec.Children) > 0 {
		rec.ChildTaskID = rec.Children[0].ChildTaskID
	}
	mcpTouchLocked(rec)
	persist = true
}

// mcpNoteChildOutcome writes one detached child's durable result onto every
// run that already lists that child. A result that arrives before the
// admission line is kept until admission or finish lists the child. The
// parent task status stays whatever the projection already published.
func (a *App) mcpNoteChildOutcome(child mcpChildView) {
	id := strings.TrimSpace(child.ChildTaskID)
	if a == nil || id == "" || mcpChildIsAdmission(child) {
		return
	}
	child.ChildTaskID = id
	a.mcpRunsMu.Lock()
	a.mcpRememberChildOutcomeLocked(child)
	applied := false
	dirty := map[string]struct{}{}
	for path, rec := range a.mcpRuns {
		if mcpApplyChildOutcomeLocked(rec, child) {
			applied = true
			dirty[path] = struct{}{}
		}
	}
	for path, list := range a.mcpRunArchive {
		for _, rec := range list {
			if mcpApplyChildOutcomeLocked(rec, child) {
				applied = true
				dirty[path] = struct{}{}
			}
		}
	}
	if applied {
		delete(a.mcpChildPending, child.ChildTaskID)
	}
	a.mcpRunsMu.Unlock()
	for path := range dirty {
		a.mcpPersistRun(path)
	}
}

func (a *App) mcpRememberChildOutcomeLocked(child mcpChildView) {
	if a.mcpChildPending == nil {
		a.mcpChildPending = map[string]mcpChildView{}
	}
	if _, exists := a.mcpChildPending[child.ChildTaskID]; !exists && len(a.mcpChildPending) >= mcpChildPendingLimit {
		for id := range a.mcpChildPending {
			delete(a.mcpChildPending, id)
			break
		}
	}
	a.mcpChildPending[child.ChildTaskID] = child
}

// mcpOverlayPendingChildrenLocked merges a result that arrived before this
// child was listed. The caller stores the returned slice. Listed ids leave
// the pending map.
func (a *App) mcpOverlayPendingChildrenLocked(children []mcpChildView) []mcpChildView {
	if a == nil || len(children) == 0 || len(a.mcpChildPending) == 0 {
		return children
	}
	extra := make([]mcpChildView, 0, 1)
	for _, child := range children {
		pending, ok := a.mcpChildPending[child.ChildTaskID]
		if !ok {
			continue
		}
		extra = append(extra, pending)
		delete(a.mcpChildPending, child.ChildTaskID)
	}
	if len(extra) == 0 {
		return children
	}
	return mcpMergeChildren(children, extra)
}

func mcpApplyChildOutcomeLocked(rec *mcpRunRecord, child mcpChildView) bool {
	if rec == nil || !mcpRecordHasChild(rec, child.ChildTaskID) {
		return false
	}
	next := mcpMergeChildren(rec.Children, []mcpChildView{child})
	if mcpChildViewsEqual(rec.Children, next) {
		return true
	}
	rec.Children = next
	if strings.TrimSpace(rec.ChildTaskID) == "" && len(next) > 0 {
		rec.ChildTaskID = next[0].ChildTaskID
	}
	mcpTouchLocked(rec)
	return true
}

func mcpRecordHasChild(rec *mcpRunRecord, id string) bool {
	for _, child := range rec.Children {
		if child.ChildTaskID == id {
			return true
		}
	}
	for _, step := range rec.Steps {
		if strings.TrimSpace(step.ChildTaskID) == id {
			return true
		}
		for _, child := range step.Children {
			if strings.TrimSpace(child.ChildTaskID) == id {
				return true
			}
		}
	}
	return false
}

func mcpChildViewsEqual(a, b []mcpChildView) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mcpChildViewFromRunOutcome reads the bounded ledger result for one child.
// An admission with no summary is empty, so the caller does not publish it.
func mcpChildViewFromRunOutcome(handle codingruntime.ChildTaskHandle, outcome codingruntime.ChildRunOutcome, store codingruntime.Store) mcpChildView {
	status := codingruntime.TaskStatus("")
	summary := ""
	if outcome.Attempt != nil && outcome.Attempt.Status != "" {
		status = outcome.Attempt.Status
		summary = strings.TrimSpace(outcome.Attempt.ErrorSummary)
	} else if outcome.Task != nil && outcome.Task.Status != "" {
		status = outcome.Task.Status
	}
	parentID := strings.TrimSpace(handle.ParentTaskID)
	if parentID == "" && outcome.Task != nil {
		parentID = strings.TrimSpace(outcome.Task.ParentTaskID)
	}
	childID := strings.TrimSpace(handle.TaskID)
	if store != nil && parentID != "" && childID != "" {
		if results, err := store.ListChildResults(parentID); err == nil {
			for _, result := range results {
				if result.TaskID != childID {
					continue
				}
				if text := strings.TrimSpace(result.Summary); text != "" {
					summary = text
				}
				if result.Status != "" {
					status = result.Status
				}
				break
			}
		}
	}
	if outcome.Err != nil && (status == "" || status == codingruntime.TaskQueued) {
		status = codingruntime.TaskFailed
	}
	if summary == "" && outcome.Err != nil {
		summary = compactSubAgentErrorSummary(outcome.Err.Error())
	}
	pub := codingChildPublicStatus(status)
	if pub == "admitted" && strings.TrimSpace(summary) == "" {
		return mcpChildView{}
	}
	review := summary
	// A failed or cancelled child is not the review handoff sentence.
	if review == "" && pub != mcpStatusFailed && pub != mcpStatusCancelled && pub != "interrupted" {
		review = mcpReviewHandoffErrorText
	}
	return mcpChildView{
		ChildTaskID:   childID,
		ParentTaskID:  parentID,
		Name:          strings.TrimSpace(handle.Name),
		Status:        pub,
		Progress:      pub,
		Result:        summary,
		ReviewPending: true,
		Review:        review,
	}
}

func publishMCPChildOutcome(handler *IMMessageHandler, handle codingruntime.ChildTaskHandle, outcome codingruntime.ChildRunOutcome, store codingruntime.Store) {
	if handler == nil || handler.app == nil {
		return
	}
	handler.app.mcpNoteChildOutcome(mcpChildViewFromRunOutcome(handle, outcome, store))
}
