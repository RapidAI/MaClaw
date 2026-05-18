package needledata

import "time"

const (
	EventToolRouting       = "tool_routing"
	EventIntentGate        = "intent_gate"
	EventWorkflowReview    = "workflow_review"
	EventPendingReply      = "pending_reply"
	EventMemoryExtractGate = "memory_extract_gate"
	EventSmartApproval     = "smart_approval"
)

// Event is the local training signal captured from MacLaw micro-decisions.
// It intentionally stores compact decision context instead of full transcripts.
type Event struct {
	EventID          string         `json:"event_id"`
	Type             string         `json:"type"`
	Timestamp        time.Time      `json:"timestamp"`
	Input            EventInput     `json:"input"`
	NeedlePrediction *Decision      `json:"needle_prediction,omitempty"`
	LLMPrediction    *Decision      `json:"llm_prediction,omitempty"`
	RulePrediction   *Decision      `json:"rule_prediction,omitempty"`
	FinalDecision    Decision       `json:"final_decision"`
	Outcome          EventOutcome   `json:"outcome"`
	Privacy          PrivacyInfo    `json:"privacy"`
	Meta             map[string]any `json:"meta,omitempty"`
}

type EventInput struct {
	UserText       string        `json:"user_text"`
	ShortContext   string        `json:"short_context,omitempty"`
	AvailableTools []ToolSummary `json:"available_tools,omitempty"`
	Choices        []string      `json:"choices,omitempty"`
}

type ToolSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Required    []string `json:"required,omitempty"`
}

type Decision struct {
	Name       string         `json:"name"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Source     string         `json:"source,omitempty"`
}

type EventOutcome struct {
	Success       bool   `json:"success"`
	UserCorrected bool   `json:"user_corrected,omitempty"`
	ToolError     string `json:"tool_error,omitempty"`
}

type PrivacyInfo struct {
	Redacted    bool   `json:"redacted"`
	ProjectHash string `json:"project_hash,omitempty"`
}

// TrainingRecord is a compact function-calling sample for Needle fine-tuning.
type TrainingRecord struct {
	ID       string        `json:"id"`
	Task     string        `json:"task"`
	Messages []ChatMessage `json:"messages"`
	Tools    []ToolSpec    `json:"tools,omitempty"`
	Expected Decision      `json:"expected"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}
