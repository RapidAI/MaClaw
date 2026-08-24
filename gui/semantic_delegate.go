package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedDelegateAdapter        = "semantic_delegate_trusted_subtask"
	semanticTrustedDelegateImplementation = "trusted-delegate-subtask-v1"
	semanticTrustedDelegateDefaultTimeout = 2 * time.Minute
)

func semanticUnpublishedLegacyDelegateProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityAgentDelegateSubtask {
			return true
		}
	}
	return false
}

func semanticTrustedDelegatePublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedDelegate != nil || trustedDelegateHostAvailable(h))
}

func trustedDelegateHostAvailable(h *IMMessageHandler) bool {
	return h != nil && (h.app != nil || h.standaloneConfig != nil)
}

func semanticTrustedDelegateDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedDelegateAdapter,
			"description": "Run one bound child subtask. Only the task description is accepted. Started is not completed.",
			"parameters":  semanticTrustedDelegateInvocationSchema(),
		},
	}
}

func semanticTrustedDelegateInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	}
}

func semanticTrustedDelegateArgsAllowed(args map[string]interface{}) (task string, err error) {
	if len(args) > 1 {
		return "", fmt.Errorf("trusted_delegate_arguments_rejected")
	}
	hasTask := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_delegate_arguments_rejected")
		}
		switch key {
		case "task":
			task, hasTask = value, true
		default:
			return "", fmt.Errorf("trusted_delegate_arguments_rejected")
		}
	}
	task = strings.TrimSpace(task)
	if !hasTask || task == "" {
		return "", fmt.Errorf("trusted_delegate_task_required")
	}
	return task, nil
}

func (h *IMMessageHandler) runTrustedDelegate(principalID, task string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_delegate_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_delegate_principal_required")
	}
	if h.semanticTrustedDelegate != nil {
		return h.semanticTrustedDelegate(principalID, task)
	}
	if !trustedDelegateHostAvailable(h) {
		return "", fmt.Errorf("trusted_delegate_runner_unavailable")
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("trusted_delegate_workspace_unavailable")
	}
	httpClient := &http.Client{Timeout: time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second}
	item := &TaskItem{
		Index:         0,
		DisplayNumber: 1,
		Title:         "Delegated coding task",
		Description:   task,
		AcceptanceCriteria: []string{
			"Requested files are created or modified in the target project path.",
			"The implementation is checked before reporting completion.",
		},
	}
	runner := runTaskWithSubAgent
	if runner == nil {
		runner = RunTaskWithSubAgent
	}
	done := make(chan *CodingSubAgentResult, 1)
	go func() {
		done <- runner(
			h,
			h.getCodingLLMConfig(),
			httpClient,
			item,
			workspace,
			task,
			"Directly delegated coding task; user already requested implementation.",
			nil,
			&LoopContext{UserID: principalID},
			nil,
			nil,
		)
	}()
	timer := time.NewTimer(semanticTrustedDelegateDefaultTimeout)
	defer timer.Stop()
	var result *CodingSubAgentResult
	select {
	case result = <-done:
	case <-timer.C:
		return "", fmt.Errorf("trusted_delegate_timeout")
	}
	if result == nil {
		return "", fmt.Errorf("trusted_delegate_runner_unavailable")
	}
	if result.RuntimeHandoff {
		return "", fmt.Errorf("trusted_delegate_started_is_not_complete")
	}
	summary := formatCodingSubAgentUserAnswer(result)
	if summary == "" {
		summary = strings.TrimSpace(result.Error)
	}
	if result.Status == TaskExecPassed {
		if summary == "" {
			summary = "child finished"
		}
		return "child completed: " + summary, nil
	}
	if summary == "" {
		return "", fmt.Errorf("trusted_delegate_empty")
	}
	return "", fmt.Errorf("%s", summary)
}

func semanticTrustedDelegateResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_delegate_delivery_token")
	}
	if strings.Contains(text, "delegate_to") || strings.Contains(text, "delegate_task") {
		return "", fmt.Errorf("trusted_delegate_legacy_name")
	}
	if strings.Contains(strings.ToLower(text), "started") && !strings.Contains(strings.ToLower(text), "completed") {
		return "", fmt.Errorf("trusted_delegate_started_is_not_complete")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_delegate_empty")
	}
	return text, nil
}
