package workflow

import "time"

// Definition status values.
const (
	DefStatusDraft     = "draft"
	DefStatusPublished = "published"
	DefStatusDisabled  = "disabled"
)

// Instance status values.
const (
	InstStatusRunning   = "running"
	InstStatusCompleted = "completed"
	InstStatusRejected  = "rejected"
)

// Step instance status values.
const (
	StepPending    = "pending"
	StepInProgress = "in_progress"
	StepCompleted  = "completed"
	StepRejected   = "rejected"
)

// StepTerminal returns true if the step cannot transition further.
func StepTerminal(status string) bool {
	return status == StepCompleted || status == StepRejected
}

// Definition is a reusable workflow template.
type Definition struct {
	ID          string
	Name        string
	Description string
	TriggerType string // manual, scheduled, event
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StepDefinition is an ordered step within a workflow template.
type StepDefinition struct {
	ID                  string
	WorkflowID          string
	StepCode            string
	StepName            string
	StepType            string // processing, review, notification, archive
	AssigneeMode        string // by_role, fixed_colleague
	AssigneeRoleCode    string
	AssigneeColleagueID string
	TimeoutMinutes      int
	RejectRule          string // end_process, return_initiator
	SortOrder           int
}

// Instance is a running instance of a workflow.
type Instance struct {
	ID            string
	DefinitionID  string
	Title         string
	InitiatorID   string
	CurrentStepID string
	Status        string
	InputData     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StepInstance is an individual step execution record.
type StepInstance struct {
	ID                  string
	InstanceID          string
	StepDefinitionID    string
	AssigneeColleagueID string
	CollaborationTaskID string
	Status              string
	Result              string
	SortOrder           int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// InstanceEvent records a state change on a workflow instance.
type InstanceEvent struct {
	ID         string
	InstanceID string
	StepID     string
	Event      string
	ActorID    string
	Note       string
	CreatedAt  time.Time
}
