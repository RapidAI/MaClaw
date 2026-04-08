package main

import "time"

type TraceJobKind string

type TraceRunStatus string

const (
	TraceJobKindRemoteSession  TraceJobKind = "remote_session"
	TraceJobKindAIAssistant    TraceJobKind = "ai_assistant"
	TraceJobKindBrowserSession TraceJobKind = "browser_session"
)

const (
	TraceRunStatusQueued       TraceRunStatus = "queued"
	TraceRunStatusStarting     TraceRunStatus = "starting"
	TraceRunStatusRunning      TraceRunStatus = "running"
	TraceRunStatusBusy         TraceRunStatus = "busy"
	TraceRunStatusWaitingInput TraceRunStatus = "waiting_input"
	TraceRunStatusCompleted    TraceRunStatus = "completed"
	TraceRunStatusFailed       TraceRunStatus = "failed"
	TraceRunStatusCancelled    TraceRunStatus = "cancelled"
	TraceRunStatusExited       TraceRunStatus = "exited"
	TraceRunStatusPaused       TraceRunStatus = "paused"
	TraceRunStatusStopped      TraceRunStatus = "stopped"
	TraceRunStatusTimeout      TraceRunStatus = "timeout"
)

type TraceJob struct {
	JobID       string         `json:"job_id"`
	Kind        TraceJobKind   `json:"kind"`
	Title       string         `json:"title"`
	Source      string         `json:"source,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	ProjectPath string         `json:"project_path,omitempty"`
	Status      TraceRunStatus `json:"status"`
	LatestRunID string         `json:"latest_run_id,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
}

type TraceRun struct {
	RunID         string         `json:"run_id"`
	JobID         string         `json:"job_id"`
	Kind          TraceJobKind   `json:"kind"`
	Title         string         `json:"title"`
	Source        string         `json:"source,omitempty"`
	UserID        string         `json:"user_id,omitempty"`
	ProjectPath   string         `json:"project_path,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	LoopID        string         `json:"loop_id,omitempty"`
	LinkedRunIDs  []string       `json:"linked_run_ids,omitempty"`
	Status        TraceRunStatus `json:"status"`
	Summary       string         `json:"summary,omitempty"`
	Error         string         `json:"error,omitempty"`
	StartedAt     int64          `json:"started_at"`
	UpdatedAt     int64          `json:"updated_at"`
	EndedAt       int64          `json:"ended_at,omitempty"`
	EventCount    int            `json:"event_count"`
	EvidenceCount int            `json:"evidence_count"`
}

type TraceEvent struct {
	EventID     string `json:"event_id"`
	JobID       string `json:"job_id"`
	RunID       string `json:"run_id"`
	ProjectPath string `json:"project_path,omitempty"`
	Kind        string `json:"kind"`
	Severity    string `json:"severity,omitempty"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	RelatedFile string `json:"related_file,omitempty"`
	Command     string `json:"command,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type EvidenceRecord struct {
	EvidenceID     string `json:"evidence_id"`
	JobID          string `json:"job_id"`
	RunID          string `json:"run_id"`
	ProjectPath    string `json:"project_path,omitempty"`
	SourceKind     string `json:"source_kind"`
	Category       string `json:"category,omitempty"`
	Summary        string `json:"summary"`
	ContentSnippet string `json:"content_snippet,omitempty"`
	RelatedFile    string `json:"related_file,omitempty"`
	Command        string `json:"command,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

type TrialReflectSummary struct {
	AttemptCount      int      `json:"attempt_count,omitempty"`
	AttemptedTools    []string `json:"attempted_tools,omitempty"`
	FailureCount      int      `json:"failure_count,omitempty"`
	FailureCategories []string `json:"failure_categories,omitempty"`
	Recovered         bool     `json:"recovered,omitempty"`
	FinalOutcome      string   `json:"final_outcome,omitempty"`
	StrategyNote      string   `json:"strategy_note,omitempty"`
}

type AIAssistantTraceView struct {
	JobID               string               `json:"job_id"`
	RunID               string               `json:"run_id"`
	Kind                TraceJobKind         `json:"kind"`
	Title               string               `json:"title"`
	Source              string               `json:"source,omitempty"`
	UserID              string               `json:"user_id,omitempty"`
	ProjectPath         string               `json:"project_path,omitempty"`
	SessionID           string               `json:"session_id,omitempty"`
	LoopID              string               `json:"loop_id,omitempty"`
	LinkedRunIDs        []string             `json:"linked_run_ids,omitempty"`
	Status              TraceRunStatus       `json:"status"`
	Summary             string               `json:"summary,omitempty"`
	Error               string               `json:"error,omitempty"`
	StartedAt           int64                `json:"started_at"`
	UpdatedAt           int64                `json:"updated_at"`
	EndedAt             int64                `json:"ended_at,omitempty"`
	EventCount          int                  `json:"event_count"`
	EvidenceCount       int                  `json:"evidence_count"`
	TrialReflectSummary *TrialReflectSummary `json:"trial_reflect_summary,omitempty"`
	Events              []TraceEvent         `json:"events"`
	Evidence            []EvidenceRecord     `json:"evidence"`
}

func traceStatusFromSessionStatus(status SessionStatus) TraceRunStatus {
	switch status {
	case SessionStarting:
		return TraceRunStatusStarting
	case SessionRunning:
		return TraceRunStatusRunning
	case SessionBusy:
		return TraceRunStatusBusy
	case SessionWaitingInput:
		return TraceRunStatusWaitingInput
	case SessionError:
		return TraceRunStatusFailed
	case SessionExited:
		return TraceRunStatusExited
	default:
		return TraceRunStatusRunning
	}
}

func traceStatusFromLoopState(state string) TraceRunStatus {
	switch state {
	case "queued":
		return TraceRunStatusQueued
	case "starting":
		return TraceRunStatusStarting
	case "running":
		return TraceRunStatusRunning
	case "paused":
		return TraceRunStatusPaused
	case "completed":
		return TraceRunStatusCompleted
	case "failed":
		return TraceRunStatusFailed
	case "stopped":
		return TraceRunStatusStopped
	case "timeout":
		return TraceRunStatusTimeout
	default:
		return TraceRunStatusRunning
	}
}

func isTraceTerminalStatus(status TraceRunStatus) bool {
	switch status {
	case TraceRunStatusCompleted, TraceRunStatusFailed, TraceRunStatusCancelled, TraceRunStatusExited, TraceRunStatusStopped, TraceRunStatusTimeout:
		return true
	default:
		return false
	}
}

func traceNowMillis() int64 {
	return time.Now().UnixMilli()
}
