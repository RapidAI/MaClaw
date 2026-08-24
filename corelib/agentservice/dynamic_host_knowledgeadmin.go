package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostKnowledgeAdminProviderID     = "core-knowledge-admin"
	reviewedHostKnowledgeAdminImplementation = "local"
	reviewedHostKnowledgeAdminAdapterName    = "host_knowledge_admin_maintenance"
	reviewedHostKnowledgeAdminListLimit      = 20
)

type reviewedHostKnowledgeAdministrator interface {
	AdministerReviewedHostKnowledge(ctx context.Context, principal Principal, id, status string, refresh bool, hasRefresh bool) (string, error)
}

func reviewedHostKnowledgeAdminInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":      map[string]interface{}{"type": "string"},
			"status":  map[string]interface{}{"type": "string"},
			"refresh": map[string]interface{}{"type": "boolean"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostKnowledgeAdminContractDigest() string {
	return coretool.SchemaDigest([]byte("knowledge.admin.maintenance:v1:host-knowledge-admin"))
}

func reviewedHostKnowledgeAdminDispatch(id, status string, hasRefresh, refresh bool) (string, bool) {
	if id == "" {
		if status != "" || hasRefresh {
			return "", false
		}
		return "list", true
	}
	if hasRefresh {
		if status != "" || !refresh {
			return "", false
		}
		return "refresh", true
	}
	if status != "" {
		return "status", true
	}
	return "get", true
}

func reviewedHostKnowledgeAdminStatus(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "disabled", "disable":
		return "disabled", true
	case "enabled", "enable", "active":
		return "enabled", true
	case "deleted", "delete":
		return "deleted", true
	case "":
		return "", true
	default:
		return "", false
	}
}

// ProjectReviewedHostKnowledgeAdminProvider projects the host-owned
// knowledge-source administration surface. It is not a Skill/MCP discovery
// entry and must not import the GUI knowledge_maintain action catalog.
// Field presence decides list/get/enable/disable/delete/refresh. action,
// source_id, query, labels, snapshots, hub share, and quality-plan fields
// are rejected. This is not knowledge.read.local or knowledge.ingest.local.
// The host process observes the knowledge store, so the handler result is
// the local completion receipt.
func ProjectReviewedHostKnowledgeAdminProvider(admin reviewedHostKnowledgeAdministrator) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if admin == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host knowledge administrator is unavailable")
	}
	parameters := reviewedHostKnowledgeAdminInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host knowledge admin schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostKnowledgeAdminContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-knowledge-admin-id-status-or-refresh-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostKnowledgeAdminAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostKnowledgeAdminProviderID,
			ImplementationID: reviewedHostKnowledgeAdminImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityKnowledgeAdmin,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostKnowledgeAdmin(admin)}, nil
}

func AttachReviewedHostKnowledgeAdminProvider(catalog DynamicSemanticCatalog, admin reviewedHostKnowledgeAdministrator) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostKnowledgeAdminProvider(admin)
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

func executeReviewedHostKnowledgeAdmin(admin reviewedHostKnowledgeAdministrator) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if admin == nil {
			return "", fmt.Errorf("host_knowledge_admin_unavailable")
		}
		if len(args) > 3 {
			return "", fmt.Errorf("host_knowledge_admin_arguments_rejected")
		}
		id, status := "", ""
		refresh, hasRefresh := false, false
		for key, raw := range args {
			switch key {
			case "id", "status":
				value, ok := raw.(string)
				if !ok {
					return "", fmt.Errorf("host_knowledge_admin_arguments_rejected")
				}
				if key == "id" {
					id = strings.TrimSpace(value)
				} else {
					status = strings.TrimSpace(value)
				}
			case "refresh":
				value, ok := raw.(bool)
				if !ok {
					return "", fmt.Errorf("host_knowledge_admin_arguments_rejected")
				}
				refresh, hasRefresh = value, true
			default:
				return "", fmt.Errorf("host_knowledge_admin_arguments_rejected")
			}
		}
		if _, ok := reviewedHostKnowledgeAdminDispatch(id, status, hasRefresh, refresh); !ok {
			return "", fmt.Errorf("host_knowledge_admin_field_presence_rejected")
		}
		return admin.AdministerReviewedHostKnowledge(ctx, principal, id, status, refresh, hasRefresh)
	}
}

func (c *coreAgentCallbacks) AdministerReviewedHostKnowledge(ctx context.Context, principal Principal, id, status string, refresh bool, hasRefresh bool) (string, error) {
	if c == nil || c.knowledgeStore == nil {
		return "", fmt.Errorf("host_knowledge_admin_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_knowledge_admin_principal_mismatch")
	}
	id, status = strings.TrimSpace(id), strings.TrimSpace(status)
	op, ok := reviewedHostKnowledgeAdminDispatch(id, status, hasRefresh, refresh)
	if !ok {
		return "", fmt.Errorf("host_knowledge_admin_field_presence_rejected")
	}
	parsedStatus, statusOK := reviewedHostKnowledgeAdminStatus(status)
	if !statusOK {
		return "", fmt.Errorf("host_knowledge_admin_status_rejected")
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
		timeout := 10 * time.Second
		if op == "refresh" {
			timeout = 2 * time.Minute
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	switch op {
	case "get":
		source, err := c.knowledgeSourceForWrite(id)
		if err != nil {
			return "", fmt.Errorf("host_knowledge_admin_not_found")
		}
		return reviewedHostKnowledgeAdminProjection("current", source), nil
	case "status":
		if _, err := c.knowledgeSourceForWrite(id); err != nil {
			return "", fmt.Errorf("host_knowledge_admin_not_found")
		}
		switch parsedStatus {
		case "disabled":
			source, err := c.knowledgeStore.DisableSource(ctx, id)
			if err != nil {
				return "", err
			}
			return reviewedHostKnowledgeAdminProjection("updated", source), nil
		case "enabled":
			source, err := c.knowledgeStore.EnableSource(ctx, id)
			if err != nil {
				return "", err
			}
			return reviewedHostKnowledgeAdminProjection("updated", source), nil
		default:
			if err := c.knowledgeStore.DeleteSource(ctx, id); err != nil {
				return "", err
			}
			return "知识来源已删除。", nil
		}
	case "refresh":
		if _, err := c.knowledgeSourceForWrite(id); err != nil {
			return "", fmt.Errorf("host_knowledge_admin_not_found")
		}
		source, err := c.knowledgeStore.RefreshSource(ctx, id)
		if err != nil {
			return "", err
		}
		return reviewedHostKnowledgeAdminProjection("refreshed", source), nil
	default:
		sources, err := c.knowledgeStore.ListSources(ctx, knowledge.ListSourcesOptions{
			TenantID:        principal.TenantID,
			OwnerID:         principal.UserID,
			IncludeDisabled: true,
			Limit:           reviewedHostKnowledgeAdminListLimit,
		})
		if err != nil {
			return "", err
		}
		if len(sources) == 0 {
			return "当前没有知识来源。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个知识来源:\n", len(sources))
		for _, item := range sources {
			fmt.Fprintf(&b, "- %s\n", reviewedHostKnowledgeAdminLine(item))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func reviewedHostKnowledgeAdminProjection(kind string, source knowledge.Source) string {
	line := reviewedHostKnowledgeAdminLine(source)
	switch kind {
	case "updated":
		return "知识来源已更新: " + line
	case "refreshed":
		return "知识来源已刷新: " + line
	default:
		return line
	}
}

func reviewedHostKnowledgeAdminLine(source knowledge.Source) string {
	title := strings.TrimSpace(source.Title)
	if title == "" {
		title = strings.TrimSpace(source.Kind)
	}
	if title == "" {
		title = source.ID
	}
	return fmt.Sprintf("%s [%s] ID=%s", title, source.Status, source.ID)
}
