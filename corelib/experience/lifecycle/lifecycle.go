package lifecycle

import (
	"strings"
	"time"
)

// EntryType identifies the role an experience entry plays during retrieval.
// The types mirror the split between evidence and behavior.
type EntryType string

const (
	EntryTypeFactual          EntryType = "factual"
	EntryTypeEpisodic         EntryType = "episodic"
	EntryTypeSuccessSkill     EntryType = "success_skill"
	EntryTypeFailureSkill     EntryType = "failure_skill"
	EntryTypeComparativeSkill EntryType = "comparative_skill"
)

func (t EntryType) Valid() bool {
	switch t {
	case EntryTypeFactual, EntryTypeEpisodic, EntryTypeSuccessSkill, EntryTypeFailureSkill, EntryTypeComparativeSkill:
		return true
	default:
		return false
	}
}

func NormalizeEntryType(value string) EntryType {
	value = strings.TrimSpace(value)
	switch EntryType(value) {
	case EntryTypeFactual, EntryTypeEpisodic, EntryTypeSuccessSkill, EntryTypeFailureSkill, EntryTypeComparativeSkill:
		return EntryType(value)
	default:
		return ""
	}
}

type Boundary struct {
	OwnerID     string   `json:"owner_id,omitempty"`
	ProjectPath string   `json:"project_path,omitempty"`
	TaskType    string   `json:"task_type,omitempty"`
	Workflow    string   `json:"workflow,omitempty"`
	Toolchain   []string `json:"toolchain,omitempty"`
	TimeWindow  string   `json:"time_window,omitempty"`
	SourceScope string   `json:"source_scope,omitempty"`
}

type UtilityStats struct {
	RetrievedCount int       `json:"retrieved_count,omitempty"`
	InjectedCount  int       `json:"injected_count,omitempty"`
	HelpfulCount   int       `json:"helpful_count,omitempty"`
	HarmfulCount   int       `json:"harmful_count,omitempty"`
	SuccessCount   int       `json:"success_count,omitempty"`
	LastUsedAt     time.Time `json:"last_used_at,omitempty"`
	AvgTokenCost   float64   `json:"avg_token_cost,omitempty"`
	AvgStepDelta   float64   `json:"avg_step_delta,omitempty"`
}

type GovernanceState string

const (
	GovernanceEvidenceOnly GovernanceState = "evidence_only"
	GovernanceDraft        GovernanceState = "draft"
	GovernanceReviewed     GovernanceState = "reviewed"
	GovernanceRejected     GovernanceState = "rejected"
	GovernanceBlocked      GovernanceState = "blocked"
)

type Entry struct {
	ID          string          `json:"id"`
	EntryType   EntryType       `json:"entry_type"`
	WhenToUse   string          `json:"when_to_use,omitempty"`
	Content     string          `json:"content,omitempty"`
	SourceType  string          `json:"source_type,omitempty"`
	SourceURL   string          `json:"source_url,omitempty"`
	EvidenceIDs []string        `json:"evidence_ids,omitempty"`
	Boundary    Boundary        `json:"boundary,omitempty"`
	Priority    float64         `json:"priority,omitempty"`
	Utility     UtilityStats    `json:"utility,omitempty"`
	Governance  GovernanceState `json:"governance,omitempty"`
}

type EventType string

const (
	EventToolCallStarted        EventType = "tool_call_started"
	EventToolCallFinished       EventType = "tool_call_finished"
	EventRetrievalDecided       EventType = "retrieval_decided"
	EventExperienceRetrieved    EventType = "experience_retrieved"
	EventExperienceInjected     EventType = "experience_injected"
	EventWorkflowPhaseCompleted EventType = "workflow_phase_completed"
	EventTaskSucceeded          EventType = "task_succeeded"
	EventTaskFailed             EventType = "task_failed"
	EventUserFeedbackReceived   EventType = "user_feedback_received"
	EventRepairAttempted        EventType = "repair_attempted"
	EventRepairApplied          EventType = "repair_applied"
)

type Event struct {
	TraceID    string    `json:"trace_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	EventType  EventType `json:"event_type"`
	StepIndex  int       `json:"step_index,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	EntryIDs   []string  `json:"entry_ids,omitempty"`
	Query      string    `json:"query,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	TokenCost  int       `json:"token_cost,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
	ErrorClass string    `json:"error_class,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

type EventContext struct {
	TraceID string `json:"trace_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
}

func (c EventContext) Apply(event Event) Event {
	if event.TraceID == "" {
		event.TraceID = strings.TrimSpace(c.TraceID)
	}
	if event.TaskID == "" {
		event.TaskID = strings.TrimSpace(c.TaskID)
	}
	return event
}

func (e Event) WithDefaults(now func() time.Time) Event {
	if now == nil {
		now = time.Now
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now()
	}
	e.EntryIDs = normalizeStringList(e.EntryIDs, 64)
	return e
}

func normalizeStringList(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = len(values)
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if len([]rune(trimmed)) > 240 {
			trimmed = string([]rune(trimmed)[:237]) + "..."
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= limit {
			break
		}
	}
	return out
}
