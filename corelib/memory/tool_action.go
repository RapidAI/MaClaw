package memory

import "strings"

// MemoryToolAction is the normalized action set supported by the shared memory
// tool handler.
type MemoryToolAction string

const (
	MemoryToolActionUnknown        MemoryToolAction = ""
	MemoryToolActionRecall         MemoryToolAction = "recall"
	MemoryToolActionThemes         MemoryToolAction = "themes"
	MemoryToolActionScenes         MemoryToolAction = "scenes"
	MemoryToolActionTrace          MemoryToolAction = "trace"
	MemoryToolActionCandidates     MemoryToolAction = "candidates"
	MemoryToolActionDerived        MemoryToolAction = "derived"
	MemoryToolActionDerivedSurgery MemoryToolAction = "derived_surgery"
	MemoryToolActionSave           MemoryToolAction = "save"
	MemoryToolActionList           MemoryToolAction = "list"
	MemoryToolActionDelete         MemoryToolAction = "delete"
)

// NormalizeMemoryToolAction canonicalizes action aliases accepted by HandleTool.
func NormalizeMemoryToolAction(action string) MemoryToolAction {
	switch MemoryToolAction(strings.ToLower(strings.TrimSpace(action))) {
	case MemoryToolActionRecall:
		return MemoryToolActionRecall
	case MemoryToolActionThemes:
		return MemoryToolActionThemes
	case MemoryToolActionScenes, "scene_index":
		return MemoryToolActionScenes
	case MemoryToolActionTrace, "recall_trace":
		return MemoryToolActionTrace
	case MemoryToolActionCandidates, "memory_candidates", "candidate", "inspect_candidates":
		return MemoryToolActionCandidates
	case MemoryToolActionDerived, "derived_audit", "audit_derived":
		return MemoryToolActionDerived
	case MemoryToolActionDerivedSurgery, "supersede_derived", "derived_supersede", "memory_surgery":
		return MemoryToolActionDerivedSurgery
	case MemoryToolActionSave:
		return MemoryToolActionSave
	case MemoryToolActionList:
		return MemoryToolActionList
	case MemoryToolActionDelete:
		return MemoryToolActionDelete
	default:
		return MemoryToolActionUnknown
	}
}

// IsRecallOnlyAllowed reports whether the action is safe in side-query modes
// where memory is read-only but recall and inspection are allowed.
func (a MemoryToolAction) IsRecallOnlyAllowed() bool {
	return a == MemoryToolActionRecall || a == MemoryToolActionThemes || a == MemoryToolActionScenes || a == MemoryToolActionTrace || a == MemoryToolActionCandidates || a == MemoryToolActionDerived
}
