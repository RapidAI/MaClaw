package remote

type SSHBackgroundTaskStatus string

const (
	SSHBackgroundTaskStatusPending   SSHBackgroundTaskStatus = "pending"
	SSHBackgroundTaskStatusRunning   SSHBackgroundTaskStatus = "running"
	SSHBackgroundTaskStatusCompleted SSHBackgroundTaskStatus = "completed"
	SSHBackgroundTaskStatusFailed    SSHBackgroundTaskStatus = "failed"
	SSHBackgroundTaskStatusKilled    SSHBackgroundTaskStatus = "killed"
	SSHBackgroundTaskStatusUnknown   SSHBackgroundTaskStatus = "unknown"
)

func (s SSHBackgroundTaskStatus) String() string {
	if s == "" {
		return string(SSHBackgroundTaskStatusUnknown)
	}
	return string(s)
}

func (s SSHBackgroundTaskStatus) IsCompleted() bool {
	return s == SSHBackgroundTaskStatusCompleted
}

func (s SSHBackgroundTaskStatus) IsFailed() bool {
	return s == SSHBackgroundTaskStatusFailed
}

func (s SSHBackgroundTaskStatus) IsKilled() bool {
	return s == SSHBackgroundTaskStatusKilled
}

func (s SSHBackgroundTaskStatus) IsActive() bool {
	return s == SSHBackgroundTaskStatusPending || s == SSHBackgroundTaskStatusRunning
}

func (s SSHBackgroundTaskStatus) IsUnknown() bool {
	return s == "" || s == SSHBackgroundTaskStatusUnknown
}
