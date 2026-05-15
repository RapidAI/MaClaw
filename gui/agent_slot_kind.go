package main

// SlotKind categorizes background loops for concurrency control.
type SlotKind int

const (
	SlotKindCoding    SlotKind = iota // coding task, max 1
	SlotKindScheduled                 // scheduled task, max 1
	SlotKindAuto                      // auto task, max 1
	SlotKindSSH                       // SSH remote session, max 10
	SlotKindBrowser                   // browser task, max 2
	SlotKindGUI                       // GUI desktop automation task, max 1
)

// SlotKindString returns a human-readable label for the slot kind.
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
	case SlotKindGUI:
		return "gui"
	default:
		return "unknown"
	}
}

func normalizeSlotKind(value string) SlotKind {
	switch value {
	case SlotKindCoding.String():
		return SlotKindCoding
	case SlotKindScheduled.String(), "":
		return SlotKindScheduled
	case SlotKindAuto.String():
		return SlotKindAuto
	case SlotKindSSH.String():
		return SlotKindSSH
	case SlotKindBrowser.String():
		return SlotKindBrowser
	case SlotKindGUI.String():
		return SlotKindGUI
	default:
		return SlotKindScheduled
	}
}
