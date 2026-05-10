package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/task"
)

type taskToolAction string

const (
	taskToolActionCreate   taskToolAction = "create"
	taskToolActionUpdate   taskToolAction = "update"
	taskToolActionComplete taskToolAction = "complete"
	taskToolActionFail     taskToolAction = "fail"
	taskToolActionList     taskToolAction = "list"
	taskToolActionDelegate taskToolAction = "delegate"
	taskToolActionDelete   taskToolAction = "delete"
)

func parseTaskToolAction(raw string) taskToolAction {
	return taskToolAction(strings.ToLower(strings.TrimSpace(raw)))
}

func (a taskToolAction) completionStatus() (task.Status, bool) {
	switch a {
	case taskToolActionComplete:
		return task.StatusCompleted, true
	case taskToolActionFail:
		return task.StatusFailed, true
	default:
		return "", false
	}
}

func parseTaskToolStatus(raw string) (task.Status, bool) {
	switch task.Status(strings.ToLower(strings.TrimSpace(raw))) {
	case task.StatusPending:
		return task.StatusPending, true
	case task.StatusInProgress:
		return task.StatusInProgress, true
	case task.StatusCompleted:
		return task.StatusCompleted, true
	case task.StatusFailed:
		return task.StatusFailed, true
	case task.StatusBlocked:
		return task.StatusBlocked, true
	case "":
		return "", true
	default:
		return "", false
	}
}
