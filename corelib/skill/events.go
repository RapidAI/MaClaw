package skill

// Evolution / skill lifecycle event names shared by EvolutionPipeline emitters
// and GUI/TUI listeners. Keep in sync with:
//
//	gui/events.go
//	gui/frontend/src/constants/events.ts
const (
	EventSkillUsageUpdated       = "skill:usage_updated"
	EventSkillRepaired           = "skill:repaired"
	EventSkillOptimized          = "skill:optimized"
	EventSkillAutoDiscovered     = "skill:auto_discovered"
	EventSkillExecutionFailed    = "skill:execution_failed"
	EventSkillRepairDraftReady   = "skill:repair_draft_ready"
	EventSkillIndexRefreshed     = "skill:index_refreshed"
	EventSkillEvolutionQueueFull = "skill:evolution_queue_full"
)
