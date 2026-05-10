package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const codingAgentEventPrefix = "Coding Agent Event:"

// CodingAgentEvent is the structured progress envelope for MaClaw's coding runtime.
// The text form remains line-oriented so existing progress pipes can carry it.
type CodingAgentEvent struct {
	Version    int      `json:"version"`
	Agent      string   `json:"agent"`
	Event      string   `json:"event"`
	Phase      string   `json:"phase"`
	TaskID     string   `json:"task_id,omitempty"`
	Title      string   `json:"title,omitempty"`
	RunID      string   `json:"run_id,omitempty"`
	TurnID     string   `json:"turn_id,omitempty"`
	Ts         string   `json:"ts,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	Count      int      `json:"count,omitempty"`
	Files      []string `json:"files,omitempty"`
}

func newCodingAgentTaskEvent(phase codingAgentEventPhaseKind, task *TaskItem, title string, runID string) CodingAgentEvent {
	if task != nil && strings.TrimSpace(title) == "" {
		title = compactSubAgentTaskTitle(task.Title)
	}
	taskID := ""
	if task != nil {
		taskID = fmt.Sprintf("T%d", task.Index)
	}
	return CodingAgentEvent{
		Version: 1,
		Agent:   "coding",
		Event:   codingAgentEventKindTaskStatus.String(),
		Phase:   phase.String(),
		TaskID:  taskID,
		Title:   strings.TrimSpace(title),
		RunID:   strings.TrimSpace(runID),
	}
}

func parseCodingAgentEventText(text string) (CodingAgentEvent, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, codingAgentEventPrefix) {
		return CodingAgentEvent{}, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, codingAgentEventPrefix))
	if payload == "" {
		return CodingAgentEvent{}, false
	}
	var event CodingAgentEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return CodingAgentEvent{}, false
	}
	if strings.TrimSpace(event.Agent) != "coding" {
		return CodingAgentEvent{}, false
	}
	return event, true
}

func formatCodingAgentEvent(event CodingAgentEvent) string {
	if event.Version == 0 {
		event.Version = 1
	}
	if strings.TrimSpace(event.Agent) == "" {
		event.Agent = "coding"
	}
	if strings.TrimSpace(event.Ts) == "" {
		event.Ts = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(codingAgentEventWire(event))
	if err != nil {
		return ""
	}
	return codingAgentEventPrefix + " " + string(payload)
}

type codingAgentEventPayload struct {
	Version    int      `json:"version"`
	Agent      string   `json:"agent"`
	Event      string   `json:"event"`
	Phase      string   `json:"phase"`
	TaskID     string   `json:"task_id,omitempty"`
	Title      string   `json:"title,omitempty"`
	RunID      string   `json:"run_id,omitempty"`
	TurnID     string   `json:"turn_id,omitempty"`
	Ts         string   `json:"ts,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	DurationMS *int64   `json:"duration_ms,omitempty"`
	Count      *int     `json:"count,omitempty"`
	Files      []string `json:"files,omitempty"`
}

func codingAgentEventWire(event CodingAgentEvent) codingAgentEventPayload {
	payload := codingAgentEventPayload{
		Version: event.Version,
		Agent:   event.Agent,
		Event:   event.Event,
		Phase:   event.Phase,
		TaskID:  event.TaskID,
		Title:   event.Title,
		RunID:   event.RunID,
		TurnID:  event.TurnID,
		Ts:      event.Ts,
		Detail:  event.Detail,
		Outcome: event.Outcome,
		Summary: event.Summary,
		Files:   event.Files,
	}
	if event.DurationMS > 0 || codingAgentEventCarriesDuration(event.Event) {
		durationMS := event.DurationMS
		payload.DurationMS = &durationMS
	}
	if event.Count > 0 || codingAgentEventCarriesCount(event.Event) {
		count := event.Count
		payload.Count = &count
	}
	return payload
}

func codingAgentEventCarriesDuration(event string) bool {
	return classifyCodingAgentEventKind(event).CarriesDuration()
}

func codingAgentEventCarriesCount(event string) bool {
	return classifyCodingAgentEventKind(event).CarriesCount()
}

func isCodingAgentEventText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), codingAgentEventPrefix)
}

func emitCodingAgentEvent(onProgress func(string), event CodingAgentEvent) {
	if onProgress == nil {
		return
	}
	message := formatCodingAgentEvent(event)
	if message == "" {
		return
	}
	onProgress(message)
}
