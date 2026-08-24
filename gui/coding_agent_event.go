package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const codingAgentEventPrefix = "Coding Agent Event:"

// CodingAgentEvent is the structured progress envelope for MaClaw's coding runtime.
// The text form remains line-oriented so existing progress pipes can carry it.
type CodingAgentEvent struct {
	Version     int                     `json:"version"`
	Agent       string                  `json:"agent"`
	Event       string                  `json:"event"`
	Phase       string                  `json:"phase"`
	TaskID      string                  `json:"task_id,omitempty"`
	Title       string                  `json:"title,omitempty"`
	RunID       string                  `json:"run_id,omitempty"`
	TurnID      string                  `json:"turn_id,omitempty"`
	Ts          string                  `json:"ts,omitempty"`
	Detail      string                  `json:"detail,omitempty"`
	Command     string                  `json:"command,omitempty"`
	Outcome     string                  `json:"outcome,omitempty"`
	Severity    string                  `json:"severity,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	DurationMS  int64                   `json:"duration_ms,omitempty"`
	Count       int                     `json:"count,omitempty"`
	Files       []string                `json:"files,omitempty"`
	Added       int                     `json:"added,omitempty"`
	Removed     int                     `json:"removed,omitempty"`
	FileChanges []CodingAgentFileChange `json:"file_changes,omitempty"`
}

// CodingAgentFileChange is one row in the OpenCode-style changed-file table.
type CodingAgentFileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
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
		Agent:   codingAgentNameCoding.String(),
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
	if normalizeCodingAgentNameKind(event.Agent) != codingAgentNameCoding {
		return CodingAgentEvent{}, false
	}
	return event, true
}

func formatCodingAgentEvent(event CodingAgentEvent) string {
	if event.Version == 0 {
		event.Version = 1
	}
	if strings.TrimSpace(event.Agent) == "" {
		event.Agent = codingAgentNameCoding.String()
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
	Version     int                     `json:"version"`
	Agent       string                  `json:"agent"`
	Event       string                  `json:"event"`
	Phase       string                  `json:"phase"`
	TaskID      string                  `json:"task_id,omitempty"`
	Title       string                  `json:"title,omitempty"`
	RunID       string                  `json:"run_id,omitempty"`
	TurnID      string                  `json:"turn_id,omitempty"`
	Ts          string                  `json:"ts,omitempty"`
	Detail      string                  `json:"detail,omitempty"`
	Command     string                  `json:"command,omitempty"`
	Outcome     string                  `json:"outcome,omitempty"`
	Severity    string                  `json:"severity,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	DurationMS  *int64                  `json:"duration_ms,omitempty"`
	Count       *int                    `json:"count,omitempty"`
	Files       []string                `json:"files,omitempty"`
	Added       *int                    `json:"added,omitempty"`
	Removed     *int                    `json:"removed,omitempty"`
	FileChanges []CodingAgentFileChange `json:"file_changes,omitempty"`
}

func codingAgentEventWire(event CodingAgentEvent) codingAgentEventPayload {
	payload := codingAgentEventPayload{
		Version:     event.Version,
		Agent:       event.Agent,
		Event:       event.Event,
		Phase:       event.Phase,
		TaskID:      event.TaskID,
		Title:       event.Title,
		RunID:       event.RunID,
		TurnID:      event.TurnID,
		Ts:          event.Ts,
		Detail:      event.Detail,
		Command:     event.Command,
		Outcome:     event.Outcome,
		Severity:    event.Severity,
		Summary:     event.Summary,
		Files:       event.Files,
		FileChanges: event.FileChanges,
	}
	if event.Added > 0 || event.Removed > 0 || len(event.FileChanges) > 0 {
		added := event.Added
		removed := event.Removed
		payload.Added = &added
		payload.Removed = &removed
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

func isCodingAgentUserProgressText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if isCodingAgentEventText(text) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(text), "coding agent:")
}

func emitCodingAgentUserProgress(onProgress func(string), message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if isCodingAgentUserProgressText(message) {
		if onProgress != nil {
			onProgress(message)
		}
		return
	}
	log.Printf("[coding-progress] %s", truncateRunesV2(message, 240))
}

func wrapCodingAgentUserProgress(onProgress func(string)) func(string) {
	return func(message string) {
		emitCodingAgentUserProgress(onProgress, message)
	}
}

// suppressCodingWorkbenchStatusProgress drops generic [Status] milestones on
// local/remote coding workbench turns. Codex shows thinking + Read/Edit/$ —
// not "Task received" / "Preparing the execution path".
func suppressCodingWorkbenchStatusProgress(onProgress func(string), codingWorkbench bool) func(string) {
	if onProgress == nil {
		return nil
	}
	if !codingWorkbench {
		return onProgress
	}
	return func(text string) {
		if strings.HasPrefix(strings.TrimSpace(text), "[Status]") {
			return
		}
		onProgress(text)
	}
}

// wrapCodingAgentReasoningToken sends live model prose to the collapsed
// thinking pane. Finish text must use the raw onToken callback.
func wrapCodingAgentReasoningToken(onToken func(string)) func(string) {
	if onToken == nil {
		return nil
	}
	return func(delta string) {
		if delta == "" {
			return
		}
		if strings.HasPrefix(delta, "\x01") {
			onToken(delta)
			return
		}
		if strings.HasPrefix(delta, "Browser:") || strings.HasPrefix(delta, "Browser：") {
			delta = strings.TrimPrefix(strings.TrimPrefix(delta, "Browser:"), "Browser：")
			delta = strings.TrimLeft(delta, " ")
			if delta == "" {
				return
			}
		}
		onToken("\x01" + delta)
	}
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
