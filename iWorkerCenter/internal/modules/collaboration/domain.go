package collaboration

import "time"

// Task status values.
const (
	StatusPending    = "pending"
	StatusAccepted   = "accepted"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusRejected   = "rejected"
)

// TerminalStatuses are statuses that cannot transition further.
var TerminalStatuses = map[string]bool{
	StatusCompleted: true,
	StatusRejected:  true,
}

// Task represents a point-to-point delegation between two colleagues.
type Task struct {
	ID                     string
	Title                  string
	Description            string
	FromColleagueID        string
	ToColleagueID          string
	ToRoleCode             string
	Status                 string
	Priority               int
	Result                 string
	WorkflowStepInstanceID string // non-empty when created by workflow engine
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// IsTerminal returns true if the task cannot transition further.
func (t *Task) IsTerminal() bool { return TerminalStatuses[t.Status] }

// TaskEvent records a state change on a collaboration task.
type TaskEvent struct {
	ID        string
	TaskID    string
	Event     string
	ActorID   string
	Note      string
	CreatedAt time.Time
}
