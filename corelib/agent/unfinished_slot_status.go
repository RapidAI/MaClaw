package agent

import "strings"

type UnfinishedTaskSlotStatus string

const (
	UnfinishedTaskSlotStatusUnknown          UnfinishedTaskSlotStatus = ""
	UnfinishedTaskSlotStatusPendingResume    UnfinishedTaskSlotStatus = "pending_resume"
	UnfinishedTaskSlotStatusInterrupted      UnfinishedTaskSlotStatus = "interrupted"
	UnfinishedTaskSlotStatusMaxRoundsReached UnfinishedTaskSlotStatus = "max_rounds_reached"
	UnfinishedTaskSlotStatusResumed          UnfinishedTaskSlotStatus = "resumed"
	UnfinishedTaskSlotStatusCompleted        UnfinishedTaskSlotStatus = "completed"
)

func NormalizeUnfinishedTaskSlotStatus(status string) UnfinishedTaskSlotStatus {
	switch UnfinishedTaskSlotStatus(strings.TrimSpace(status)) {
	case UnfinishedTaskSlotStatusPendingResume:
		return UnfinishedTaskSlotStatusPendingResume
	case UnfinishedTaskSlotStatusInterrupted:
		return UnfinishedTaskSlotStatusInterrupted
	case UnfinishedTaskSlotStatusMaxRoundsReached:
		return UnfinishedTaskSlotStatusMaxRoundsReached
	case UnfinishedTaskSlotStatusResumed:
		return UnfinishedTaskSlotStatusResumed
	case UnfinishedTaskSlotStatusCompleted:
		return UnfinishedTaskSlotStatusCompleted
	default:
		return UnfinishedTaskSlotStatusUnknown
	}
}

func (s UnfinishedTaskSlotStatus) String() string {
	return string(s)
}
