package main

import corememory "github.com/RapidAI/CodeClaw/corelib/memory"

type memoryToolAction = corememory.MemoryToolAction

const (
	memoryToolActionUnknown    = corememory.MemoryToolActionUnknown
	memoryToolActionRecall     = corememory.MemoryToolActionRecall
	memoryToolActionThemes     = corememory.MemoryToolActionThemes
	memoryToolActionScenes     = corememory.MemoryToolActionScenes
	memoryToolActionTrace      = corememory.MemoryToolActionTrace
	memoryToolActionCandidates = corememory.MemoryToolActionCandidates
	memoryToolActionSave       = corememory.MemoryToolActionSave
	memoryToolActionList       = corememory.MemoryToolActionList
	memoryToolActionDelete     = corememory.MemoryToolActionDelete
)

func normalizeMemoryToolAction(action string) memoryToolAction {
	return corememory.NormalizeMemoryToolAction(action)
}
