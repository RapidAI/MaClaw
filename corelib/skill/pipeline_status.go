package skill

type PipelineStatus string

const (
	PipelineStatusCompleted           PipelineStatus = "completed"
	PipelineStatusFailed              PipelineStatus = "failed"
	PipelineStatusStoppedAtCheckpoint PipelineStatus = "stopped_at_checkpoint"
	PipelineStatusCancelled           PipelineStatus = "cancelled"
)

func (s PipelineStatus) IsCompleted() bool {
	return s == PipelineStatusCompleted
}

func (s PipelineStatus) IsFailed() bool {
	return s == PipelineStatusFailed
}

func (s PipelineStatus) IsCancelled() bool {
	return s == PipelineStatusCancelled
}

type PipelineStepStatus string

const (
	PipelineStepStatusCompleted PipelineStepStatus = "completed"
	PipelineStepStatusFailed    PipelineStepStatus = "failed"
	PipelineStepStatusSkipped   PipelineStepStatus = "skipped"
	PipelineStepStatusCancelled PipelineStepStatus = "cancelled"
)

func (s PipelineStepStatus) IsFailed() bool {
	return s == PipelineStepStatusFailed
}

func (s PipelineStepStatus) IsSkipped() bool {
	return s == PipelineStepStatusSkipped
}
