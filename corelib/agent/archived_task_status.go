package agent

import "strings"

// ArchivedTaskStatus is the lifecycle reason stored with an archived task.
// It is a string alias so persisted JSON remains backward compatible.
type ArchivedTaskStatus string

const (
	ArchivedTaskStatusUnknown     ArchivedTaskStatus = ""
	ArchivedTaskStatusCompleted   ArchivedTaskStatus = "completed"
	ArchivedTaskStatusAbandoned   ArchivedTaskStatus = "abandoned"
	ArchivedTaskStatusInterrupted ArchivedTaskStatus = "interrupted"
	ArchivedTaskStatusSwitched    ArchivedTaskStatus = "switched"
)

func NormalizeArchivedTaskStatus(status string) ArchivedTaskStatus {
	switch ArchivedTaskStatus(strings.TrimSpace(status)) {
	case ArchivedTaskStatusCompleted:
		return ArchivedTaskStatusCompleted
	case ArchivedTaskStatusAbandoned:
		return ArchivedTaskStatusAbandoned
	case ArchivedTaskStatusInterrupted:
		return ArchivedTaskStatusInterrupted
	case ArchivedTaskStatusSwitched:
		return ArchivedTaskStatusSwitched
	default:
		return ArchivedTaskStatusUnknown
	}
}

func (status ArchivedTaskStatus) String() string {
	return string(status)
}
