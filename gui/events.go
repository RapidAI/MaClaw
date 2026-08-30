package main

// Wails event name constants.
//
// These constants define the event names used for communication between
// the Go backend and the TypeScript frontend via Wails EventsEmit/EventsOn.
//
// IMPORTANT: When adding or renaming events, update the corresponding
// frontend constants in:
//   gui/frontend/src/constants/events.ts
//
// This file exists to prevent event name mismatches (the root cause of #98:
// backend emitted "task-list-changed" but frontend listened for
// "project-index:changed"). Using constants makes mismatches a compile-time
// error (unused constant) rather than a silent runtime bug.

const (
	// EventProjectIndexChanged is emitted when the ProjectIndex is updated.
	// Frontend listener: App.tsx useEffect -> refreshTasks().
	EventProjectIndexChanged = "project-index:changed"

	// EventTasksChanged is a companion event to EventProjectIndexChanged,
	// emitted together for backward compatibility with components that only
	// listen to one of the two.
	// Frontend listener: App.tsx useEffect -> refreshTasks().
	EventTasksChanged = "tasks:changed"

	// EventProjectTaskClosed is emitted when a task is removed or archived and
	// any matching isolated project tab should be closed.
	EventProjectTaskClosed = "project-task:closed"

	// EventProjectTaskDeleted is emitted before EventProjectTaskClosed when a
	// task is permanently removed. Consumers use it to discard local caches
	// without sending a CloseProjectTabSession write back to the backend.
	EventProjectTaskDeleted = "project-task:deleted"

	// EventProjectTaskRenamed is emitted after a task display name changes.
	// Payload contains project_path and name so open project tabs can update
	// without waiting for a full task-index reload.
	EventProjectTaskRenamed = "project-task:renamed"

	// EventExpertTaskDeleted is emitted with the expert id when a durable
	// expert task is permanently removed. Expert conversation state is keyed
	// by expert id (not the task workspace path), so the frontend needs this
	// signal to drop orphaned tab/history caches.
	EventExpertTaskDeleted = "expert-task:deleted"

	// EventAppUpdateAvailable is emitted when the background update checker
	// (startup delay + periodic re-check) finds a newer application release.
	EventAppUpdateAvailable = "app-update-available"

	// Skill evolution / self-repair events (EvolutionPipeline + SkillRunner).
	// Values must match corelib/skill event constants and frontend events.ts.
	EventSkillUsageUpdated        = "skill:usage_updated"
	EventSkillRepaired            = "skill:repaired"
	EventSkillOptimized           = "skill:optimized"
	EventSkillAutoDiscovered      = "skill:auto_discovered"
	EventSkillExecutionFailed     = "skill:execution_failed"
	EventSkillRepairDraftReady    = "skill:repair_draft_ready"
	EventSkillIndexRefreshed      = "skill:index_refreshed"
	EventSkillEvolutionCancelled  = "skill:evolution_cancelled"
	EventSkillEvolutionTimedOut   = "skill:evolution_timed_out"
	EventSkillEvolutionRolledBack = "skill:evolution_rolled_back"

	// Computer Use operator preview (local OmniParser loop; not multimodal screenshots).
	EventComputerUseObserve = "computer-use:observe"
	EventComputerUseAction  = "computer-use:action"
	// Operator control (pause/resume/stop/reset) status broadcast.
	EventComputerUseControl = "computer-use:control"
	// Background warmup / self-check finished.
	EventComputerUseWarmup = "computer-use:warmup"
	// Observe/action pipeline error with operator guidance (permissions, display, …).
	EventComputerUseError = "computer-use:error"
	// Log lifecycle (prune / delete / batch-delete) for settings + operator refresh.
	EventComputerUseLogs = "computer-use:logs"

	// EventSurveyUpdated is emitted after an IM survey response is submitted
	// (or other survey mutations that should refresh the Utilities results UI).
	// Payload: map with survey_id (string) and optional event (string).
	EventSurveyUpdated = "survey-updated"
)
