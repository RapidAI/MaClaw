package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/task"
)

type taskToolAction string

const (
	taskToolActionUnknown  taskToolAction = ""
	taskToolActionCreate   taskToolAction = "create"
	taskToolActionUpdate   taskToolAction = "update"
	taskToolActionComplete taskToolAction = "complete"
	taskToolActionFail     taskToolAction = "fail"
	taskToolActionList     taskToolAction = "list"
	taskToolActionDelegate taskToolAction = "delegate"
	taskToolActionDelete   taskToolAction = "delete"
)

func normalizeTaskToolAction(action string) taskToolAction {
	switch taskToolAction(strings.ToLower(strings.TrimSpace(action))) {
	case taskToolActionCreate:
		return taskToolActionCreate
	case taskToolActionUpdate:
		return taskToolActionUpdate
	case taskToolActionComplete:
		return taskToolActionComplete
	case taskToolActionFail:
		return taskToolActionFail
	case taskToolActionList:
		return taskToolActionList
	case taskToolActionDelegate:
		return taskToolActionDelegate
	case taskToolActionDelete:
		return taskToolActionDelete
	default:
		return taskToolAction(strings.TrimSpace(action))
	}
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

func normalizeTaskToolStatus(status string) (task.Status, bool) {
	switch task.Status(strings.ToLower(strings.TrimSpace(status))) {
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
