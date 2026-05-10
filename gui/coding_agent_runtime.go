package main

import (
	"fmt"
	"strings"
	"time"
)

// CodingTurnContext carries the stable identity for one delegated coding turn.
// All task, tool, diff, and result events in that turn should share these IDs.
type CodingTurnContext struct {
	TurnID      string
	RunID       string
	TaskID      string
	TaskTitle   string
	ProjectPath string
	StartedAt   time.Time
}

func newCodingTurnContext(runID string, task *TaskItem, projectPath string) CodingTurnContext {
	runID = strings.TrimSpace(runID)
	taskID := ""
	taskTitle := ""
	taskIndex := -1
	if task != nil {
		taskIndex = task.Index
		taskID = fmt.Sprintf("T%d", task.Index)
		taskTitle = compactSubAgentTaskTitle(task.Title)
	}
	return CodingTurnContext{
		TurnID:      makeCodingTurnID(runID, taskIndex),
		RunID:       runID,
		TaskID:      taskID,
		TaskTitle:   taskTitle,
		ProjectPath: strings.TrimSpace(projectPath),
		StartedAt:   time.Now(),
	}
}

func makeCodingTurnID(runID string, taskIndex int) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = "local"
	}
	if taskIndex >= 0 {
		return fmt.Sprintf("coding-turn-%s-T%d", sanitizeCodingTurnIDPart(runID), taskIndex)
	}
	return fmt.Sprintf("coding-turn-%s", sanitizeCodingTurnIDPart(runID))
}

func sanitizeCodingTurnIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "local"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-_")
	if result == "" {
		return "local"
	}
	return result
}

func (c CodingTurnContext) ApplyToEvent(event CodingAgentEvent) CodingAgentEvent {
	if strings.TrimSpace(event.RunID) == "" {
		event.RunID = c.RunID
	}
	if strings.TrimSpace(event.TurnID) == "" {
		event.TurnID = c.TurnID
	}
	if strings.TrimSpace(event.TaskID) == "" {
		event.TaskID = c.TaskID
	}
	if strings.TrimSpace(event.Title) == "" {
		event.Title = c.TaskTitle
	}
	return event
}

func (c CodingTurnContext) TaskEvent(phase string, task *TaskItem, title string) CodingAgentEvent {
	return c.ApplyToEvent(newCodingAgentTaskEvent(normalizeCodingAgentEventPhaseKind(phase), task, title, c.RunID))
}

func (c CodingTurnContext) Emit(onProgress func(string), event CodingAgentEvent) {
	emitCodingAgentEvent(onProgress, c.ApplyToEvent(event))
}

func (c CodingTurnContext) WrapProgress(onProgress func(string)) func(string) {
	if onProgress == nil {
		return nil
	}
	return func(text string) {
		if event, ok := parseCodingAgentEventText(text); ok {
			emitCodingAgentEvent(onProgress, c.ApplyToEvent(event))
			return
		}
		onProgress(text)
	}
}
