package agentnet

import "strings"

type TaskStateStatus string

const (
	TaskStateStatusUnknown   TaskStateStatus = ""
	TaskStateStatusOpen      TaskStateStatus = "open"
	TaskStateStatusCreated   TaskStateStatus = "created"
	TaskStateStatusAccepted  TaskStateStatus = "accepted"
	TaskStateStatusRejected  TaskStateStatus = "rejected"
	TaskStateStatusCancelled TaskStateStatus = "cancelled"
	TaskStateStatusSettled   TaskStateStatus = "settled"
)

func NormalizeTaskStateStatus(status TaskStateStatus) TaskStateStatus {
	switch TaskStateStatus(strings.ToLower(strings.TrimSpace(status.String()))) {
	case TaskStateStatusOpen:
		return TaskStateStatusOpen
	case TaskStateStatusCreated:
		return TaskStateStatusCreated
	case TaskStateStatusAccepted:
		return TaskStateStatusAccepted
	case TaskStateStatusRejected:
		return TaskStateStatusRejected
	case TaskStateStatusCancelled:
		return TaskStateStatusCancelled
	case TaskStateStatusSettled:
		return TaskStateStatusSettled
	default:
		return TaskStateStatusUnknown
	}
}

func (status TaskStateStatus) String() string {
	return string(status)
}
