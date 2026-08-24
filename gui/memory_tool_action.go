package main

import corememory "github.com/RapidAI/CodeClaw/corelib/memory"

type memoryToolAction = corememory.MemoryToolAction

const (
	memoryToolActionUnknown        = corememory.MemoryToolActionUnknown
	memoryToolActionRecall         = corememory.MemoryToolActionRecall
	memoryToolActionThemes         = corememory.MemoryToolActionThemes
	memoryToolActionScenes         = corememory.MemoryToolActionScenes
	memoryToolActionTrace          = corememory.MemoryToolActionTrace
	memoryToolActionCandidates     = corememory.MemoryToolActionCandidates
	memoryToolActionDerived        = corememory.MemoryToolActionDerived
	memoryToolActionDerivedSurgery = corememory.MemoryToolActionDerivedSurgery
	memoryToolActionSave           = corememory.MemoryToolActionSave
	memoryToolActionList           = corememory.MemoryToolActionList
	memoryToolActionDelete         = corememory.MemoryToolActionDelete
	memoryToolActionSummary        = corememory.MemoryToolActionSummary
)

func normalizeMemoryToolAction(action string) memoryToolAction {
	return corememory.NormalizeMemoryToolAction(action)
}
