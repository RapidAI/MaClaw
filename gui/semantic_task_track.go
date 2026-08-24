package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedTaskAdapter        = "semantic_administer_trusted_task"
	semanticTrustedTaskImplementation = "trusted-task-track-v1"
	semanticTrustedTaskTitleMaxRunes  = 500
	semanticTrustedTaskNoteMaxRunes   = 5000
	semanticTrustedTaskTimeout        = 10 * time.Second
)

func semanticUnpublishedLegacyTaskProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityTaskTrackLocal {
			return true
		}
	}
	return false
}

func semanticTrustedTaskDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedTaskAdapter,
			"description": "Read or update the host-local todo list. Field presence decides create, update, delete, or list.",
			"parameters":  semanticTrustedTaskInvocationSchema(),
		},
	}
}

func semanticTrustedTaskInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":       map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"id":          map[string]interface{}{"type": "string"},
			"status":      map[string]interface{}{"type": "string"},
			"note":        map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedTaskArgsAllowed(args map[string]interface{}) (title, description, id, status, note string, err error) {
	if len(args) > 5 {
		return "", "", "", "", "", fmt.Errorf("trusted_task_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", "", "", "", fmt.Errorf("trusted_task_arguments_rejected")
		}
		switch key {
		case "title":
			title = strings.TrimSpace(value)
		case "description":
			description = strings.TrimSpace(value)
		case "id":
			id = strings.TrimSpace(value)
		case "status":
			status = strings.TrimSpace(value)
		case "note":
			note = strings.TrimSpace(value)
		default:
			return "", "", "", "", "", fmt.Errorf("trusted_task_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedTaskDispatch(title, description, id, status, note); !ok {
		return "", "", "", "", "", fmt.Errorf("trusted_task_field_presence_rejected")
	}
	return title, description, id, status, note, nil
}

func semanticTrustedTaskDispatch(title, description, id, status, note string) (string, bool) {
	hasTitle := title != ""
	hasDescription := description != ""
	hasID := id != ""
	hasStatus := status != ""
	hasNote := note != ""
	if hasTitle {
		if hasID || hasStatus || hasNote {
			return "", false
		}
		return "create", true
	}
	if hasDescription {
		return "", false
	}
	if hasID && (hasStatus || hasNote) {
		return "update", true
	}
	if hasID {
		return "delete", true
	}
	if hasStatus || hasNote {
		return "", false
	}
	return "list", true
}

func semanticTrustedTaskStatus(raw string) (task.Status, bool) {
	switch task.Status(strings.ToLower(strings.TrimSpace(raw))) {
	case task.StatusPending, task.StatusInProgress, task.StatusCompleted, task.StatusFailed, task.StatusBlocked:
		return task.Status(strings.ToLower(strings.TrimSpace(raw))), true
	case "":
		return "", true
	default:
		return "", false
	}
}

func (h *IMMessageHandler) administerTrustedTask(principalID, title, description, id, status, note string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_task_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_task_principal_required")
	}
	if h.semanticTrustedTask != nil {
		return h.semanticTrustedTask(principalID, title, description, id, status, note)
	}
	if h.taskStore == nil {
		h.taskStore = task.NewStore()
	}
	title, description, id, status, note = strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(id), strings.TrimSpace(status), strings.TrimSpace(note)
	op, ok := semanticTrustedTaskDispatch(title, description, id, status, note)
	if !ok {
		return "", fmt.Errorf("trusted_task_field_presence_rejected")
	}
	if title != "" && utf8.RuneCountInString(title) > semanticTrustedTaskTitleMaxRunes {
		return "", fmt.Errorf("trusted_task_title_too_large")
	}
	if description != "" && utf8.RuneCountInString(description) > semanticTrustedTaskNoteMaxRunes {
		return "", fmt.Errorf("trusted_task_description_too_large")
	}
	if note != "" && utf8.RuneCountInString(note) > semanticTrustedTaskNoteMaxRunes {
		return "", fmt.Errorf("trusted_task_note_too_large")
	}
	parsedStatus, statusOK := semanticTrustedTaskStatus(status)
	if !statusOK {
		return "", fmt.Errorf("trusted_task_status_rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedTaskTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	// Every operation is scoped to the calling principal. The handler serves
	// many IM users from one process, so an unscoped store hands whoever asks
	// the todo list of everyone who asked before them.
	owner := strings.TrimSpace(principalID)
	switch op {
	case "create":
		taskID := h.taskStore.CreateOwned(owner, title, description, nil)
		item, found := h.taskStore.GetOwned(owner, taskID)
		if !found {
			return "", fmt.Errorf("trusted_task_create_failed")
		}
		return fmt.Sprintf("任务已创建: %s [%s] %s", item.ID, item.Status, item.Title), nil
	case "update":
		if err := h.taskStore.UpdateOwned(owner, id, parsedStatus, note); err != nil {
			return "", err
		}
		item, found := h.taskStore.GetOwned(owner, id)
		if !found {
			return "", fmt.Errorf("trusted_task_update_failed")
		}
		result := fmt.Sprintf("任务已更新: %s [%s] %s", item.ID, item.Status, item.Title)
		if note != "" {
			result += "\n备注: " + note
		}
		return result, nil
	case "delete":
		if err := h.taskStore.DeleteOwned(owner, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("任务已删除: %s", id), nil
	default:
		return agent.RenderTaskList(h.taskStore.ListOwned(owner)), nil
	}
}

func semanticTrustedTaskResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_task_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_task_empty")
	}
	return text, nil
}
