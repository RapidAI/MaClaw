package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedMemoryAdapter              = "semantic_administer_trusted_memory"
	semanticTrustedMemoryImplementation       = "trusted-memory-manage-v1"
	semanticTrustedMemoryRecallAdapter        = "semantic_recall_trusted_memory"
	semanticTrustedMemoryRecallImplementation = "trusted-memory-recall-v1"
	semanticTrustedMemoryMaxRunes             = 20000
	semanticTrustedMemoryTimeout              = 10 * time.Second
)

func semanticUnpublishedLegacyMemoryProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMemoryManageAgent {
			return true
		}
	}
	return false
}

func semanticTrustedMemoryRecallDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMemoryRecallAdapter,
			"description": "Recall entries from the current principal's agent memory. Read-only; cannot save or delete.",
			"parameters":  semanticTrustedMemoryRecallInvocationSchema(),
		},
	}
}

func semanticTrustedMemoryRecallInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func (h *IMMessageHandler) recallTrustedMemory(principalID, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("trusted_memory_recall_query_required")
	}
	return h.administerTrustedMemory(principalID, "", query, "")
}

func semanticTrustedMemoryDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMemoryAdapter,
			"description": "Read or update the current principal's agent memory. Field presence decides save, recall, delete, or list.",
			"parameters":  semanticTrustedMemoryInvocationSchema(),
		},
	}
}

func semanticTrustedMemoryInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{"type": "string"},
			"query":   map[string]interface{}{"type": "string"},
			"id":      map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedMemoryArgsAllowed(args map[string]interface{}) (content, query, id string, err error) {
	if len(args) > 3 {
		return "", "", "", fmt.Errorf("trusted_memory_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", "", fmt.Errorf("trusted_memory_arguments_rejected")
		}
		switch key {
		case "content":
			content = strings.TrimSpace(value)
		case "query":
			query = strings.TrimSpace(value)
		case "id":
			id = strings.TrimSpace(value)
		default:
			return "", "", "", fmt.Errorf("trusted_memory_arguments_rejected")
		}
	}
	if !semanticTrustedMemoryFieldPresenceOK(content, query, id) {
		return "", "", "", fmt.Errorf("trusted_memory_content_xor_query_xor_id_or_empty_required")
	}
	return content, query, id, nil
}

func semanticTrustedMemoryFieldPresenceOK(content, query, id string) bool {
	n := 0
	if content != "" {
		n++
	}
	if query != "" {
		n++
	}
	if id != "" {
		n++
	}
	return n <= 1
}

func (h *IMMessageHandler) administerTrustedMemory(principalID, content, query, id string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_memory_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_memory_principal_required")
	}
	if h.semanticTrustedMemory != nil {
		return h.semanticTrustedMemory(principalID, content, query, id)
	}
	if h.memoryStore == nil {
		return "", fmt.Errorf("trusted_memory_unavailable")
	}
	content, query, id = strings.TrimSpace(content), strings.TrimSpace(query), strings.TrimSpace(id)
	if !semanticTrustedMemoryFieldPresenceOK(content, query, id) {
		return "", fmt.Errorf("trusted_memory_content_xor_query_xor_id_or_empty_required")
	}
	if content != "" && utf8.RuneCountInString(content) > semanticTrustedMemoryMaxRunes {
		return "", fmt.Errorf("trusted_memory_content_too_large")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedMemoryTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	args := map[string]interface{}{}
	switch {
	case content != "":
		args["action"] = string(corememory.MemoryToolActionSave)
		args["content"] = content
	case query != "":
		args["action"] = string(corememory.MemoryToolActionRecall)
		args["query"] = query
	case id != "":
		args["action"] = string(corememory.MemoryToolActionDelete)
		args["id"] = id
	default:
		args["action"] = string(corememory.MemoryToolActionList)
	}
	out := corememory.HandleTool(h.memoryStore, args, corememory.ToolOptions{
		ContextHint: h.buildMemoryContextHintForUser(principalID),
		OwnerID:     principalID,
		StrictOwner: true,
		LoopID:      h.currentLoopIDForUser(principalID),
		AfterWrite: func() {
			if h.app != nil {
				h.app.triggerMemoryPipelineSoon(45 * time.Second)
			}
			h.RefreshMemorySnapshot(principalID)
		},
	})
	if semanticTrustedMemoryResultFailed(out) {
		return "", fmt.Errorf("%s", out)
	}
	return out, nil
}

func semanticTrustedMemoryResultFailed(result string) bool {
	result = strings.TrimSpace(result)
	if result == "" {
		return true
	}
	for _, prefix := range []string{
		"long-term memory is not initialized",
		"missing ",
		"save memory failed:",
		"delete memory failed:",
		"memory candidate rejected:",
		"unknown memory action:",
		"cannot combine ",
		"memory pagination",
		"memory not found",
		"archived experience is read-only",
		"memory themes are unavailable",
	} {
		if strings.HasPrefix(strings.ToLower(result), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func semanticTrustedMemoryResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_memory_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_memory_empty")
	}
	return text, nil
}
