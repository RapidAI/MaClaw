package skill

// Evolution / skill lifecycle event names shared by EvolutionPipeline emitters
// and GUI/TUI listeners. Keep in sync with:
//
//	gui/events.go
//	gui/frontend/src/constants/events.ts
const (
	EventSkillUsageUpdated            = "skill:usage_updated"
	EventSkillRepaired                = "skill:repaired"
	EventSkillRepairDisabled          = "skill:repair_disabled"
	EventSkillOptimized               = "skill:optimized"
	EventSkillAutoDiscovered          = "skill:auto_discovered"
	EventSkillExecutionFailed         = "skill:execution_failed"
	EventSkillRepairDraftReady        = "skill:repair_draft_ready"
	EventSkillIndexRefreshed          = "skill:index_refreshed"
	EventSkillEvolutionQueueFull      = "skill:evolution_queue_full"
	EventSkillEvolutionCancelled      = "skill:evolution_cancelled"
	EventSkillEvolutionTimedOut       = "skill:evolution_timed_out"
	EventSkillEvolutionRolledBack     = "skill:evolution_rolled_back"
	EventSkillCompensationRecovered   = "skill:compensation_recovered"
	EventSkillCompensationNeedsReview = "skill:compensation_needs_review"
	// Manual compensation actions are explicit operator decisions. They are
	// intentionally separate from automatic recovery events so audit consumers
	// can distinguish a user-requested retry/clear from startup replay.
	EventSkillCompensationManualRetry = "skill:compensation_manual_retry"
	EventSkillCompensationManualClear = "skill:compensation_manual_clear"
)
