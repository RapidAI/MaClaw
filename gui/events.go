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
	// EventProjectIndexChanged is emitted when the ProjectIndex is updated
	// (new project record or activity timestamp change). Frontend uses this
	// to refresh the "最近任务" sidebar.
	// Frontend listener: App.tsx useEffect → refreshRecentProjects()
	EventProjectIndexChanged = "project-index:changed"

	// EventTasksChanged is a companion event to EventProjectIndexChanged,
	// emitted together for backward compatibility with components that only
	// listen to one of the two.
	// Frontend listener: App.tsx useEffect → refreshRecentProjects()
	EventTasksChanged = "tasks:changed"
)
