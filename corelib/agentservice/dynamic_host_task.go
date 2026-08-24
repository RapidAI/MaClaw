package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/task"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostTaskProviderID     = "core-task"
	reviewedHostTaskImplementation = "local"
	reviewedHostTaskAdapterName    = "host_task_track_local"
	reviewedHostTaskTitleMaxRunes  = 500
	reviewedHostTaskNoteMaxRunes   = 5000
)

type reviewedHostTaskTracker interface {
	TrackReviewedHostTask(ctx context.Context, principal Principal, title, description, id, status, note string) (string, error)
}

func reviewedHostTaskInvocationSchema() map[string]interface{} {
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

func reviewedHostTaskContractDigest() string {
	return coretool.SchemaDigest([]byte("task.track.local:v1:host-task-track"))
}

func reviewedHostTaskDispatch(title, description, id, status, note string) (string, bool) {
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

func reviewedHostTaskStatus(raw string) (task.Status, bool) {
	switch task.Status(strings.ToLower(strings.TrimSpace(raw))) {
	case task.StatusPending, task.StatusInProgress, task.StatusCompleted, task.StatusFailed, task.StatusBlocked:
		return task.Status(strings.ToLower(strings.TrimSpace(raw))), true
	case "":
		return "", true
	default:
		return "", false
	}
}

// ProjectReviewedHostTaskProvider projects the host-owned local todo list.
// It is not a Skill/MCP discovery entry and must not import the GUI task
// action catalog. Field presence decides create/update/delete/list; there
// is no action, delegate_to, depends_on, or task_id. This is not
// goal.manage.long_running, schedule.administer.local, or
// agent.delegate.subtask. The host process observes the session task
// store, so the handler result is the local completion receipt.
func ProjectReviewedHostTaskProvider(tracker reviewedHostTaskTracker) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if tracker == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host task tracker is unavailable")
	}
	parameters := reviewedHostTaskInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host task schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostTaskContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-task-title-or-id-status-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostTaskAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostTaskProviderID,
			ImplementationID: reviewedHostTaskImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityTaskTrack,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostTask(tracker)}, nil
}

func AttachReviewedHostTaskProvider(catalog DynamicSemanticCatalog, tracker reviewedHostTaskTracker) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostTaskProvider(tracker)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostTask(tracker reviewedHostTaskTracker) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if tracker == nil {
			return "", fmt.Errorf("host_task_unavailable")
		}
		if len(args) > 5 {
			return "", fmt.Errorf("host_task_arguments_rejected")
		}
		title, description, id, status, note := "", "", "", "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_task_arguments_rejected")
			}
			switch key {
			case "title":
				title = value
			case "description":
				description = value
			case "id":
				id = value
			case "status":
				status = value
			case "note":
				note = value
			default:
				return "", fmt.Errorf("host_task_arguments_rejected")
			}
		}
		title, description, id, status, note = strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(id), strings.TrimSpace(status), strings.TrimSpace(note)
		if _, ok := reviewedHostTaskDispatch(title, description, id, status, note); !ok {
			return "", fmt.Errorf("host_task_field_presence_rejected")
		}
		return tracker.TrackReviewedHostTask(ctx, principal, title, description, id, status, note)
	}
}

func (c *coreAgentCallbacks) TrackReviewedHostTask(ctx context.Context, principal Principal, title, description, id, status, note string) (string, error) {
	if c == nil || c.tasks == nil {
		return "", fmt.Errorf("host_task_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_task_principal_mismatch")
	}
	title, description, id, status, note = strings.TrimSpace(title), strings.TrimSpace(description), strings.TrimSpace(id), strings.TrimSpace(status), strings.TrimSpace(note)
	op, ok := reviewedHostTaskDispatch(title, description, id, status, note)
	if !ok {
		return "", fmt.Errorf("host_task_field_presence_rejected")
	}
	if title != "" && utf8.RuneCountInString(title) > reviewedHostTaskTitleMaxRunes {
		return "", fmt.Errorf("host_task_title_too_large")
	}
	if description != "" && utf8.RuneCountInString(description) > reviewedHostTaskNoteMaxRunes {
		return "", fmt.Errorf("host_task_description_too_large")
	}
	if note != "" && utf8.RuneCountInString(note) > reviewedHostTaskNoteMaxRunes {
		return "", fmt.Errorf("host_task_note_too_large")
	}
	parsedStatus, statusOK := reviewedHostTaskStatus(status)
	if !statusOK {
		return "", fmt.Errorf("host_task_status_rejected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	// The store is keyed per session, which is not the same as per principal:
	// nothing binds a session ID to one caller, so the owner has to come from
	// the principal itself. This is the same owner identity the memory, goal
	// and knowledge services already scope by.
	owner := memoryOwnerIDForPrincipal(principal)
	switch op {
	case "create":
		taskID := c.tasks.CreateOwned(owner, title, description, nil)
		item, found := c.tasks.GetOwned(owner, taskID)
		if !found {
			return "", fmt.Errorf("host_task_create_failed")
		}
		return fmt.Sprintf("任务已创建: %s [%s] %s", item.ID, item.Status, item.Title), nil
	case "update":
		if err := c.tasks.UpdateOwned(owner, id, parsedStatus, note); err != nil {
			return "", err
		}
		item, found := c.tasks.GetOwned(owner, id)
		if !found {
			return "", fmt.Errorf("host_task_update_failed")
		}
		result := fmt.Sprintf("任务已更新: %s [%s] %s", item.ID, item.Status, item.Title)
		if note != "" {
			result += "\n备注: " + note
		}
		return result, nil
	case "delete":
		if err := c.tasks.DeleteOwned(owner, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("任务已删除: %s", id), nil
	default:
		return agent.RenderTaskList(c.tasks.ListOwned(owner)), nil
	}
}
