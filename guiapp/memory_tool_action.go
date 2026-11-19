package guiapp

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

// memoryToolActionReturnsRecalledData reports whether the action's output is
// recalled store content (which must carry the untrusted-data fence before it
// reaches the model) rather than a mutation confirmation or an error string.
// derived_surgery is excluded: it supersedes a store entry and returns a host
// write acknowledgement, not recalled data.
func memoryToolActionReturnsRecalledData(action memoryToolAction) bool {
	switch action {
	case memoryToolActionRecall, memoryToolActionThemes, memoryToolActionScenes,
		memoryToolActionTrace, memoryToolActionCandidates, memoryToolActionDerived,
		memoryToolActionList, memoryToolActionSummary:
		return true
	}
	return false
}
