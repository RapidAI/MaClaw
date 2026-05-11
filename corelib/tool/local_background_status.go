package tool

type LocalBackgroundTaskStatus string

const (
	LocalBackgroundTaskStatusRunning   LocalBackgroundTaskStatus = "running"
	LocalBackgroundTaskStatusCompleted LocalBackgroundTaskStatus = "completed"
	LocalBackgroundTaskStatusFailed    LocalBackgroundTaskStatus = "failed"
	LocalBackgroundTaskStatusKilled    LocalBackgroundTaskStatus = "killed"
)

func (s LocalBackgroundTaskStatus) String() string {
	return string(s)
}

func (s LocalBackgroundTaskStatus) IsRunning() bool {
	return s == LocalBackgroundTaskStatusRunning
}
