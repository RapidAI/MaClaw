package guiautomation

import "github.com/RapidAI/CodeClaw/corelib/taskengine"

type GUITaskStatus = taskengine.TaskStatus

const (
	GUITaskStatusRunning   GUITaskStatus = taskengine.StatusRunning
	GUITaskStatusPaused    GUITaskStatus = taskengine.StatusPaused
	GUITaskStatusCompleted GUITaskStatus = taskengine.StatusCompleted
	GUITaskStatusFailed    GUITaskStatus = taskengine.StatusFailed
	GUITaskStatusCancelled GUITaskStatus = taskengine.StatusCancelled
)
