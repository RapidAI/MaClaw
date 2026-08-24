package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostMemoryProviderID           = "core-memory"
	reviewedHostMemoryImplementation       = "local"
	reviewedHostMemoryAdapterName          = "host_memory_manage_agent"
	reviewedHostMemoryRecallAdapterName    = "host_memory_recall_agent"
	reviewedHostMemoryRecallImplementation = "local-recall"
	reviewedHostMemoryMaxRunes             = 20000
)

type reviewedHostMemoryManager interface {
	ManageReviewedHostMemory(ctx context.Context, principal Principal, content, query, id string) (string, error)
}

func reviewedHostMemoryInvocationSchema() map[string]interface{} {
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

func reviewedHostMemoryContractDigest() string {
	return coretool.SchemaDigest([]byte("memory.manage.agent:v1:host-memory-manage"))
}

func reviewedHostMemoryFieldPresenceOK(content, query, id string) bool {
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

// ProjectReviewedHostMemoryProvider projects the host-owned agent memory
// store. It is not a Skill/MCP discovery entry and must not import the GUI
// memory action catalog. The closed schema accepts content XOR query XOR id,
// or empty for list; field presence decides, not user keywords. Channel,
// destination, group_name, path, file_path, action, owner, surgery, themes,
// and apply are rejected. This is not knowledge.read.local or
// knowledge.ingest.local. The host process observes HandleTool, so the
// handler result is the local completion receipt.
func ProjectReviewedHostMemoryProvider(manager reviewedHostMemoryManager) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if manager == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host memory manager is unavailable")
	}
	parameters := reviewedHostMemoryInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host memory schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostMemoryContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-memory-content-xor-query-xor-id-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostMemoryAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostMemoryProviderID,
			ImplementationID: reviewedHostMemoryImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityMemoryManage,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostMemory(manager)}, nil
}

func reviewedHostMemoryRecallInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func reviewedHostMemoryRecallContractDigest() string {
	return coretool.SchemaDigest([]byte("memory.recall.agent:v1:host-memory-recall"))
}

// ProjectReviewedHostMemoryRecallProvider projects the read-only half of the
// host-owned agent memory store. Field presence is query only; save and
// delete stay on the manage adapter.
func ProjectReviewedHostMemoryRecallProvider(manager reviewedHostMemoryManager) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if manager == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host memory manager is unavailable")
	}
	parameters := reviewedHostMemoryRecallInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host memory recall schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostMemoryRecallContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-memory-query-only-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostMemoryRecallAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostMemoryProviderID,
			ImplementationID: reviewedHostMemoryRecallImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityMemoryRecall,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostMemoryRecall(manager)}, nil
}

func AttachReviewedHostMemoryRecallProvider(catalog DynamicSemanticCatalog, manager reviewedHostMemoryManager) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostMemoryRecallProvider(manager)
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

func executeReviewedHostMemoryRecall(manager reviewedHostMemoryManager) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if manager == nil {
			return "", fmt.Errorf("host_memory_unavailable")
		}
		if len(args) > 1 {
			return "", fmt.Errorf("host_memory_recall_arguments_rejected")
		}
		query := ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_memory_recall_arguments_rejected")
			}
			if key != "query" {
				return "", fmt.Errorf("host_memory_recall_arguments_rejected")
			}
			query = value
		}
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("host_memory_recall_query_required")
		}
		return manager.ManageReviewedHostMemory(ctx, principal, "", query, "")
	}
}

func AttachReviewedHostMemoryProvider(catalog DynamicSemanticCatalog, manager reviewedHostMemoryManager) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostMemoryProvider(manager)
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

func executeReviewedHostMemory(manager reviewedHostMemoryManager) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if manager == nil {
			return "", fmt.Errorf("host_memory_unavailable")
		}
		if len(args) > 3 {
			return "", fmt.Errorf("host_memory_arguments_rejected")
		}
		content, query, id := "", "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_memory_arguments_rejected")
			}
			switch key {
			case "content":
				content = value
			case "query":
				query = value
			case "id":
				id = value
			default:
				return "", fmt.Errorf("host_memory_arguments_rejected")
			}
		}
		content, query, id = strings.TrimSpace(content), strings.TrimSpace(query), strings.TrimSpace(id)
		if !reviewedHostMemoryFieldPresenceOK(content, query, id) {
			return "", fmt.Errorf("host_memory_content_xor_query_xor_id_or_empty_required")
		}
		return manager.ManageReviewedHostMemory(ctx, principal, content, query, id)
	}
}

func (c *coreAgentCallbacks) ManageReviewedHostMemory(ctx context.Context, principal Principal, content, query, id string) (string, error) {
	if c == nil || c.memory == nil {
		return "", fmt.Errorf("host_memory_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_memory_principal_mismatch")
	}
	content, query, id = strings.TrimSpace(content), strings.TrimSpace(query), strings.TrimSpace(id)
	if !reviewedHostMemoryFieldPresenceOK(content, query, id) {
		return "", fmt.Errorf("host_memory_content_xor_query_xor_id_or_empty_required")
	}
	if content != "" && utf8.RuneCountInString(content) > reviewedHostMemoryMaxRunes {
		return "", fmt.Errorf("host_memory_content_too_large")
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
	args := map[string]interface{}{}
	switch {
	case content != "":
		args["action"] = string(memory.MemoryToolActionSave)
		args["content"] = content
	case query != "":
		args["action"] = string(memory.MemoryToolActionRecall)
		args["query"] = query
	case id != "":
		args["action"] = string(memory.MemoryToolActionDelete)
		args["id"] = id
	default:
		args["action"] = string(memory.MemoryToolActionList)
	}
	out := memory.HandleTool(c.memory, args, memory.ToolOptions{
		ContextHint: c.userText,
		OwnerID:     memoryOwnerIDForPrincipal(principal),
		StrictOwner: true,
		LoopID:      c.loopID,
	})
	if reviewedHostMemoryResultFailed(out) {
		return "", fmt.Errorf("%s", out)
	}
	return out, nil
}

func reviewedHostMemoryResultFailed(result string) bool {
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
