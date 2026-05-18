package main

import "strings"

type memoryToolAction string

const (
	memoryToolActionUnknown memoryToolAction = ""
	memoryToolActionRecall  memoryToolAction = "recall"
	memoryToolActionThemes  memoryToolAction = "themes"
	memoryToolActionScenes  memoryToolAction = "scenes"
	memoryToolActionTrace   memoryToolAction = "trace"
	memoryToolActionSave    memoryToolAction = "save"
	memoryToolActionList    memoryToolAction = "list"
	memoryToolActionDelete  memoryToolAction = "delete"
)

func normalizeMemoryToolAction(action string) memoryToolAction {
	switch memoryToolAction(strings.ToLower(strings.TrimSpace(action))) {
	case memoryToolActionRecall:
		return memoryToolActionRecall
	case memoryToolActionThemes:
		return memoryToolActionThemes
	case memoryToolActionScenes, "scene_index":
		return memoryToolActionScenes
	case memoryToolActionTrace, "recall_trace":
		return memoryToolActionTrace
	case memoryToolActionSave:
		return memoryToolActionSave
	case memoryToolActionList:
		return memoryToolActionList
	case memoryToolActionDelete:
		return memoryToolActionDelete
	default:
		return memoryToolActionUnknown
	}
}

func (a memoryToolAction) IsRecallOnlyAllowed() bool {
	return a == memoryToolActionRecall || a == memoryToolActionThemes || a == memoryToolActionScenes || a == memoryToolActionTrace
}
