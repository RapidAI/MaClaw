package agent

type UnfinishedTaskSlotStatus string

const (
	UnfinishedTaskSlotStatusUnknown   UnfinishedTaskSlotStatus = ""
	UnfinishedTaskSlotStatusResumed   UnfinishedTaskSlotStatus = "resumed"
	UnfinishedTaskSlotStatusCompleted UnfinishedTaskSlotStatus = "completed"
)

func (s UnfinishedTaskSlotStatus) String() string {
	return string(s)
}
