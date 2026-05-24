package lifecycle

import (
	"context"
	"strconv"
	"strings"
)

type Scope struct {
	Boundary Boundary    `json:"boundary,omitempty"`
	Types    []EntryType `json:"types,omitempty"`
	Limit    int         `json:"limit,omitempty"`
}

type Query struct {
	Text     string      `json:"text"`
	Types    []EntryType `json:"types,omitempty"`
	Boundary Boundary    `json:"boundary,omitempty"`
	Limit    int         `json:"limit,omitempty"`
}

type Candidate struct {
	Entry         Entry   `json:"entry"`
	Relevance     float64 `json:"relevance,omitempty"`
	PriorityScore float64 `json:"priority_score,omitempty"`
	BoundaryScore float64 `json:"boundary_score,omitempty"`
	TokenCost     int     `json:"token_cost,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

type UtilityUpdate struct {
	EntryID      string `json:"entry_id"`
	TraceID      string `json:"trace_id,omitempty"`
	Helpful      bool   `json:"helpful,omitempty"`
	Harmful      bool   `json:"harmful,omitempty"`
	Success      bool   `json:"success,omitempty"`
	StepDelta    int    `json:"step_delta,omitempty"`
	TokenDelta   int    `json:"token_delta,omitempty"`
	Reason       string `json:"reason,omitempty"`
	EvidenceType string `json:"evidence_type,omitempty"`
}

type Provider interface {
	ListExperience(ctx context.Context, scope Scope) ([]Entry, error)
	SearchExperience(ctx context.Context, query Query) ([]Candidate, error)
	UpdateUtility(ctx context.Context, update UtilityUpdate) error
}

type RetrievalMode string

const (
	RetrievalModeAuto           RetrievalMode = "auto"
	RetrievalModeAgentRequested RetrievalMode = "agent_requested"
	RetrievalModeRecovery       RetrievalMode = "recovery"
	RetrievalModeWorkflowPhase  RetrievalMode = "workflow_phase"
)

type RetrievalBudget struct {
	MaxEntries int               `json:"max_entries,omitempty"`
	MaxTokens  int               `json:"max_tokens,omitempty"`
	Quotas     map[EntryType]int `json:"quotas,omitempty"`
}

type RetrievalPolicyInput struct {
	TraceID        string   `json:"trace_id,omitempty"`
	TaskID         string   `json:"task_id,omitempty"`
	CurrentGoal    string   `json:"current_goal,omitempty"`
	RecentTrace    []Event  `json:"recent_trace,omitempty"`
	KnownContext   string   `json:"known_context,omitempty"`
	MissingSignals []string `json:"missing_signals,omitempty"`
	TokenBudget    int      `json:"token_budget,omitempty"`
	Boundary       Boundary `json:"boundary,omitempty"`
}

type RetrievalDecision struct {
	ShouldRetrieve bool            `json:"should_retrieve"`
	Query          string          `json:"query,omitempty"`
	Types          []EntryType     `json:"types,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Budget         RetrievalBudget `json:"budget,omitempty"`
	Boundary       Boundary        `json:"boundary,omitempty"`
	Mode           RetrievalMode   `json:"mode,omitempty"`
}

type RetrievalPolicy interface {
	Decide(ctx context.Context, input RetrievalPolicyInput) RetrievalDecision
}

type RetrievalPolicyFunc func(context.Context, RetrievalPolicyInput) RetrievalDecision

func (f RetrievalPolicyFunc) Decide(ctx context.Context, input RetrievalPolicyInput) RetrievalDecision {
	if f == nil {
		return DefaultRetrievalPolicy{}.Decide(ctx, input)
	}
	return f(ctx, input)
}

type DefaultRetrievalPolicy struct{}

func (DefaultRetrievalPolicy) Decide(_ context.Context, input RetrievalPolicyInput) RetrievalDecision {
	query := strings.TrimSpace(input.CurrentGoal)
	if query == "" {
		return RetrievalDecision{ShouldRetrieve: false, Reason: "empty_goal", Mode: RetrievalModeAuto}
	}
	maxEntries := input.BoundaryDefaultLimit()
	return RetrievalDecision{
		ShouldRetrieve: true,
		Query:          query,
		Types:          DefaultRetrievalTypes(),
		Reason:         "auto_context_gap",
		Budget:         RetrievalBudget{MaxEntries: maxEntries, MaxTokens: input.TokenBudget, Quotas: DefaultRetrievalQuotas()},
		Boundary:       input.Boundary,
		Mode:           RetrievalModeAuto,
	}
}

func (input RetrievalPolicyInput) BoundaryDefaultLimit() int {
	for _, signal := range input.MissingSignals {
		if strings.HasPrefix(strings.TrimSpace(signal), "max_entries:") {
			value := strings.TrimPrefix(strings.TrimSpace(signal), "max_entries:")
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 12
}

func DefaultRetrievalTypes() []EntryType {
	return []EntryType{EntryTypeFactual, EntryTypeEpisodic, EntryTypeSuccessSkill, EntryTypeFailureSkill, EntryTypeComparativeSkill}
}

func DefaultRetrievalQuotas() map[EntryType]int {
	return map[EntryType]int{
		EntryTypeFactual:          1,
		EntryTypeEpisodic:         1,
		EntryTypeSuccessSkill:     1,
		EntryTypeFailureSkill:     1,
		EntryTypeComparativeSkill: 1,
	}
}
