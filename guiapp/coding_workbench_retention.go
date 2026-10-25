package guiapp

import "strings"

func codingWorkbenchHasUnfinishedWork(mem stickyCodingWorkbenchMemory) bool {
	if strings.TrimSpace(mem.PendingPlanJSON) != "" || strings.TrimSpace(mem.ApprovedPlanJSON) != "" || len(mem.WorktreeConflicts) > 0 {
		return true
	}
	for _, step := range mem.StepStatuses {
		switch strings.ToLower(strings.TrimSpace(step.Status)) {
		case "", "passed", "skipped":
			continue
		default:
			return true
		}
	}
	return false
}
