package agent

// LoopKind distinguishes front-end chat loops from background loops.
type LoopKind int

const (
	LoopKindChat       LoopKind = iota // interactive user chat
	LoopKindBackground                 // background task (coding, scheduled, auto)
)

// SlotKind categorizes background loops for concurrency control.
type SlotKind int

const (
	SlotKindCoding    SlotKind = iota // 编程任务 — max 1
	SlotKindScheduled                 // 定时任务 — max 1
	SlotKindAuto                      // ClawNet 自动任务 — max 1
	SlotKindSSH                       // SSH 远程会话 — max 10
	SlotKindBrowser                   // 浏览器任务 — max 2
)

// String returns a human-readable label for the slot kind.
func (s SlotKind) String() string {
	switch s {
	case SlotKindCoding:
		return "coding"
	case SlotKindScheduled:
		return "scheduled"
	case SlotKindAuto:
		return "auto"
	case SlotKindSSH:
		return "ssh"
	case SlotKindBrowser:
		return "browser"
	default:
		return "unknown"
	}
}

// StatusEventType enumerates the kinds of events a background loop can emit.
type StatusEventType int

const (
	StatusEventSessionCompleted StatusEventType = iota
	StatusEventSessionFailed
	StatusEventApproachingLimit
	StatusEventStopped
	StatusEventProgress
)

// StatusEvent is pushed from a background loop (or SessionMonitor) to the
// chat loop to inform it about state changes.
type StatusEvent struct {
	Type      StatusEventType
	LoopID    string // which background loop
	SessionID string // related coding session (if any)
	Message   string // human-readable description
	Remaining int    // remaining iterations (for ApproachingLimit)
}

// ProgressCallback is a function that receives progress updates.
type ProgressCallback func(msg string)
