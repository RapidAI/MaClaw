package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// desktopComputerReplaced is the tool error after a click or type was in
// progress when the computer was replaced. The model calls again. This
// process does not send that click a second time.
const desktopComputerReplaced = "电脑换过了，要再调用一次。"

// desktopToolVolumes is the volume set a replacement container keeps.
// PR-11 names them desktops, home, opt, and usr-local. A retry uses this
// same set. The run id does not change.
const desktopToolVolumes = "desktops,home,opt,usr-local"

// desktopToolCall is the only body a machine call sends to Hub.
// It has no conversation, no memory, and no send_to_user sentence.
type desktopToolCall struct {
	RunID      string
	ToolCallID string
	Tenant     string
	User       string
	Action     string
	Args       map[string]any
	Display    string
}

// desktopToolOutcome is what the computer returns to the worker.
type desktopToolOutcome struct {
	Text         string
	AttachmentID string
	Err          string
	ExecutorID   string
	Volumes      string
	Replaced     bool
}

// desktopToolExec is the container Hub assigned for this call.
type desktopToolExec struct {
	ID       string
	Volumes  string
	Replaced bool
}

type desktopToolSlot struct {
	mu      sync.Mutex
	have    bool
	reads   int
	outcome desktopToolOutcome
}

var (
	desktopToolSlots sync.Map
	desktopToolSeq   atomic.Uint64
	desktopExecutors sync.Map
)

func desktopToolReadOnly(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "screenshot", "file_list", "probe", "app_probe":
		return true
	default:
		return false
	}
}

func desktopToolNeedsDisplay(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "file_read", "file_write", "file_edit", "file_list", "bash", "install", "http", "skill_list", "deliver":
		return false
	default:
		return true
	}
}

// desktopToolArgs copies the tool arguments and drops anything that would
// carry a conversation, a memory excerpt, or a sentence for the person.
func desktopToolArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range args {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "messages", "memory", "send_to_user", "conversation", "history", "transcript":
			continue
		case "action":
			continue
		default:
			out[key] = value
		}
	}
	return out
}

// desktopToolJSON is the Hub request body. The keys are exactly run_id,
// tool_call_id, tenant, user, action, args, and display when a screen is needed.
func desktopToolJSON(call desktopToolCall) ([]byte, error) {
	payload := map[string]any{
		"run_id":       strings.TrimSpace(call.RunID),
		"tool_call_id": strings.TrimSpace(call.ToolCallID),
		"tenant":       strings.TrimSpace(call.Tenant),
		"user":         strings.TrimSpace(call.User),
		"action":       strings.TrimSpace(call.Action),
		"args":         desktopToolArgs(call.Args),
	}
	if display := strings.TrimSpace(call.Display); display != "" {
		payload["display"] = display
	}
	return json.Marshal(payload)
}

func desktopToolCallID(ctx context.Context, scope agentruntime.Scope, action string) string {
	if id := agentruntime.ToolCallID(ctx); id != "" {
		return id
	}
	return fmt.Sprintf("%s-%s-%d", strings.TrimSpace(scope.RunID), strings.TrimSpace(action), desktopToolSeq.Add(1))
}

func desktopToolSlotFor(runID, toolCallID string) *desktopToolSlot {
	key := strings.TrimSpace(runID) + "\x00" + strings.TrimSpace(toolCallID)
	value, _ := desktopToolSlots.LoadOrStore(key, &desktopToolSlot{})
	return value.(*desktopToolSlot)
}

func resetDesktopToolJournal() {
	desktopToolSlots.Range(func(key, _ any) bool {
		desktopToolSlots.Delete(key)
		return true
	})
}

func dropDesktopToolResults(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	prefix := runID + "\x00"
	desktopToolSlots.Range(func(key, _ any) bool {
		if text, ok := key.(string); ok && strings.HasPrefix(text, prefix) {
			desktopToolSlots.Delete(key)
		}
		return true
	})
}

func desktopCurrentExecutor(scope agentruntime.Scope) string {
	key := strings.TrimSpace(scope.TenantID) + "\x00" + strings.TrimSpace(scope.UserID)
	if value, ok := desktopExecutors.Load(key); ok {
		if id, _ := value.(string); strings.TrimSpace(id) != "" {
			return id
		}
	}
	return "assigned"
}

func noteDesktopExecutor(scope agentruntime.Scope, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	key := strings.TrimSpace(scope.TenantID) + "\x00" + strings.TrimSpace(scope.UserID)
	desktopExecutors.Store(key, id)
}

// dispatchDesktopTool sends one machine call. A tool_call_id that already
// has a result is not sent again. A click or type that meets a replaced
// computer is not replayed. A screenshot, directory list, or probe may try
// once more on the new container, with the same tool_call_id, the same run
// id, and the same volumes.
func dispatchDesktopTool(call desktopToolCall, exec desktopToolExec, send func(desktopToolCall, desktopToolExec) (desktopToolOutcome, error)) (desktopToolOutcome, error) {
	if strings.TrimSpace(exec.Volumes) == "" {
		exec.Volumes = desktopToolVolumes
	}
	// An empty run is not a round. Remembering it would let a later call
	// with the same tool id skip the computer, or share one slot across users.
	if strings.TrimSpace(call.RunID) == "" || strings.TrimSpace(call.ToolCallID) == "" {
		outcome, err := send(call, exec)
		if outcome.Volumes == "" {
			outcome.Volumes = exec.Volumes
		}
		if err != nil && strings.TrimSpace(outcome.Err) == "" {
			outcome.Err = err.Error()
		}
		if outcome.ExecutorID == "" {
			outcome.ExecutorID = exec.ID
		}
		return desktopToolReturn(outcome)
	}
	slot := desktopToolSlotFor(call.RunID, call.ToolCallID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.have {
		return desktopToolReturn(slot.outcome)
	}
	if exec.Replaced && !desktopToolReadOnly(call.Action) {
		slot.outcome = desktopToolOutcome{Err: desktopComputerReplaced, ExecutorID: exec.ID, Volumes: exec.Volumes}
		slot.have = true
		return desktopToolReturn(slot.outcome)
	}
	outcome, err := send(call, exec)
	if outcome.Volumes == "" {
		outcome.Volumes = exec.Volumes
	}
	if outcome.Replaced && !desktopToolReadOnly(call.Action) {
		slot.outcome = desktopToolOutcome{Err: desktopComputerReplaced, ExecutorID: firstDesktopText(outcome.ExecutorID, exec.ID), Volumes: outcome.Volumes}
		slot.have = true
		return desktopToolReturn(slot.outcome)
	}
	if outcome.Replaced && desktopToolReadOnly(call.Action) {
		if slot.reads >= 1 {
			slot.outcome = desktopToolOutcome{Err: desktopComputerReplaced, ExecutorID: firstDesktopText(outcome.ExecutorID, exec.ID), Volumes: outcome.Volumes}
			slot.have = true
			return desktopToolReturn(slot.outcome)
		}
		slot.reads = 1
		next := exec
		next.Replaced = false
		if id := strings.TrimSpace(outcome.ExecutorID); id != "" {
			next.ID = id
		}
		if outcome.Volumes != "" {
			next.Volumes = outcome.Volumes
		}
		outcome, err = send(call, next)
		if outcome.Volumes == "" {
			outcome.Volumes = next.Volumes
		}
		outcome.Replaced = false
	}
	if err != nil && strings.TrimSpace(outcome.Err) == "" {
		outcome.Err = err.Error()
	}
	if outcome.ExecutorID == "" {
		outcome.ExecutorID = exec.ID
	}
	slot.outcome = outcome
	slot.have = true
	return desktopToolReturn(outcome)
}

func desktopToolReturn(outcome desktopToolOutcome) (desktopToolOutcome, error) {
	if strings.TrimSpace(outcome.Err) != "" {
		return outcome, fmt.Errorf("%s", outcome.Err)
	}
	return outcome, nil
}

func firstDesktopText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func desktopRoundDisplay(scope agentruntime.Scope) string {
	runID := strings.TrimSpace(scope.RunID)
	if runID == "" {
		return ""
	}
	value, ok := desktopRoundWakes.Load(runID)
	if !ok {
		return ""
	}
	wake, _ := value.(*desktopRoundWake)
	if wake == nil {
		return ""
	}
	wake.mu.Lock()
	defer wake.mu.Unlock()
	return strings.TrimSpace(wake.endpoint.Display)
}

func desktopHubExecutesTool() bool {
	if desktopHubClientFromEnv() == nil {
		return false
	}
	if desktopRemoteSession != nil || desktopRemoteFile != nil || desktopRemoteBash != nil || desktopRemoteScreenshot != nil || desktopRemoteApp != nil || desktopRemoteOpen != nil || desktopRemoteInstall != nil {
		return false
	}
	return true
}

// runDesktopHubTool sends one machine call. The container runs the tool.
// This process does not put a model loop in that request.
func runDesktopHubTool(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	client := desktopHubClientFromEnv()
	if client == nil {
		return "", fmt.Errorf("desktop service is unreachable")
	}
	call := desktopToolCall{
		RunID:      strings.TrimSpace(scope.RunID),
		ToolCallID: desktopToolCallID(ctx, scope, action),
		Tenant:     strings.TrimSpace(scope.TenantID),
		User:       strings.TrimSpace(scope.UserID),
		Action:     strings.TrimSpace(action),
		Args:       desktopToolArgs(args),
	}
	if desktopToolNeedsDisplay(action) {
		call.Display = desktopRoundDisplay(scope)
	}
	exec := desktopToolExec{ID: desktopCurrentExecutor(scope), Volumes: desktopToolVolumes}
	outcome, err := dispatchDesktopTool(call, exec, func(call desktopToolCall, _ desktopToolExec) (desktopToolOutcome, error) {
		return client.postDesktopTool(ctx, call)
	})
	if id := strings.TrimSpace(outcome.ExecutorID); id != "" {
		noteDesktopExecutor(scope, id)
	}
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(outcome.Text)
	if id := strings.TrimSpace(outcome.AttachmentID); id != "" {
		if text == "" {
			return id, nil
		}
		return text + "\nattachment_id: " + id, nil
	}
	return text, nil
}
