package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedKnowledgeAdminAdapter        = "semantic_administer_trusted_knowledge"
	semanticTrustedKnowledgeAdminImplementation = "trusted-knowledge-admin-v1"
	semanticTrustedKnowledgeAdminListLimit      = 20
	semanticTrustedKnowledgeAdminReadTimeout    = 10 * time.Second
	semanticTrustedKnowledgeAdminRefreshTimeout = 2 * time.Minute
)

func semanticUnpublishedLegacyKnowledgeAdminProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityKnowledgeAdminMaintenance {
			return true
		}
	}
	return false
}

func semanticTrustedKnowledgeAdminDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedKnowledgeAdminAdapter,
			"description": "Administer the current principal's knowledge sources. Field presence decides list, get, enable, disable, delete, or refresh.",
			"parameters":  semanticTrustedKnowledgeAdminInvocationSchema(),
		},
	}
}

func semanticTrustedKnowledgeAdminInvocationSchema() map[string]interface{} {
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

func semanticTrustedKnowledgeAdminArgsAllowed(args map[string]interface{}) (id, status string, refresh, hasRefresh bool, err error) {
	if len(args) > 3 {
		return "", "", false, false, fmt.Errorf("trusted_knowledge_admin_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "id", "status":
			value, ok := raw.(string)
			if !ok {
				return "", "", false, false, fmt.Errorf("trusted_knowledge_admin_arguments_rejected")
			}
			if key == "id" {
				id = strings.TrimSpace(value)
			} else {
				status = strings.TrimSpace(value)
			}
		case "refresh":
			value, ok := raw.(bool)
			if !ok {
				return "", "", false, false, fmt.Errorf("trusted_knowledge_admin_arguments_rejected")
			}
			refresh, hasRefresh = value, true
		default:
			return "", "", false, false, fmt.Errorf("trusted_knowledge_admin_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedKnowledgeAdminDispatch(id, status, hasRefresh, refresh); !ok {
		return "", "", false, false, fmt.Errorf("trusted_knowledge_admin_field_presence_rejected")
	}
	return id, status, refresh, hasRefresh, nil
}

func semanticTrustedKnowledgeAdminDispatch(id, status string, hasRefresh, refresh bool) (string, bool) {
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

func semanticTrustedKnowledgeAdminStatus(raw string) (string, bool) {
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

func (h *IMMessageHandler) administerTrustedKnowledge(principalID, id, status string, refresh, hasRefresh bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_knowledge_admin_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_knowledge_admin_principal_required")
	}
	if h.semanticTrustedKnowledgeAdmin != nil {
		return h.semanticTrustedKnowledgeAdmin(principalID, id, status, refresh, hasRefresh)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_knowledge_admin_unavailable")
	}
	op, ok := semanticTrustedKnowledgeAdminDispatch(id, status, hasRefresh, refresh)
	if !ok {
		return "", fmt.Errorf("trusted_knowledge_admin_field_presence_rejected")
	}
	parsedStatus, statusOK := semanticTrustedKnowledgeAdminStatus(status)
	if !statusOK {
		return "", fmt.Errorf("trusted_knowledge_admin_status_rejected")
	}
	store, err := h.app.openKnowledgeStore()
	if err != nil {
		return "", fmt.Errorf("trusted_knowledge_admin_unavailable")
	}
	defer store.Close()
	ctx, cancel := trustedKnowledgeAdminContext(h.app.knowledgeContext(), op)
	defer cancel()
	switch op {
	case "get":
		source, err := trustedKnowledgeSourceForWrite(ctx, store, principalID, id)
		if err != nil {
			return "", err
		}
		return semanticTrustedKnowledgeAdminProjection("current", source), nil
	case "status":
		if _, err := trustedKnowledgeSourceForWrite(ctx, store, principalID, id); err != nil {
			return "", err
		}
		switch parsedStatus {
		case "disabled":
			source, err := store.DisableSource(ctx, id)
			if err != nil {
				return "", err
			}
			return semanticTrustedKnowledgeAdminProjection("updated", source), nil
		case "enabled":
			source, err := store.EnableSource(ctx, id)
			if err != nil {
				return "", err
			}
			return semanticTrustedKnowledgeAdminProjection("updated", source), nil
		default:
			if err := store.DeleteSource(ctx, id); err != nil {
				return "", err
			}
			return "知识来源已删除。", nil
		}
	case "refresh":
		if _, err := trustedKnowledgeSourceForWrite(ctx, store, principalID, id); err != nil {
			return "", err
		}
		source, err := store.RefreshSource(ctx, id)
		if err != nil {
			return "", err
		}
		return semanticTrustedKnowledgeAdminProjection("refreshed", source), nil
	default:
		sources, err := listTrustedKnowledgeSources(ctx, store, principalID)
		if err != nil {
			return "", err
		}
		if len(sources) == 0 {
			return "当前没有知识来源。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个知识来源:\n", len(sources))
		for _, item := range sources {
			fmt.Fprintf(&b, "- %s\n", semanticTrustedKnowledgeAdminLine(item))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func trustedKnowledgeAdminContext(parent context.Context, op string) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return parent, func() {}
	}
	timeout := semanticTrustedKnowledgeAdminReadTimeout
	if op == "refresh" {
		timeout = semanticTrustedKnowledgeAdminRefreshTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func listTrustedKnowledgeSources(ctx context.Context, store *knowledge.SQLiteStore, principalID string) ([]knowledge.Source, error) {
	opts := knowledge.ListSourcesOptions{
		OwnerID:           principalID,
		IncludeEmptyOwner: trustedKnowledgePrincipalMaySeeHostLocal(principalID),
		IncludeDisabled:   true,
		Limit:             semanticTrustedKnowledgeAdminListLimit,
	}
	sources, err := store.ListSources(ctx, opts)
	if err != nil {
		return nil, err
	}
	owned := make([]knowledge.Source, 0, len(sources))
	for _, source := range sources {
		if !trustedKnowledgeSourceOwned(source, principalID) {
			continue
		}
		owned = append(owned, source)
	}
	return owned, nil
}

func trustedKnowledgePrincipalMaySeeHostLocal(principalID string) bool {
	return trustedDesktopPrincipal(principalID)
}

func trustedKnowledgeSourceOwned(source knowledge.Source, principalID string) bool {
	owner := strings.TrimSpace(source.OwnerID)
	principalID = strings.TrimSpace(principalID)
	if owner == principalID {
		return true
	}
	return owner == "" && trustedKnowledgePrincipalMaySeeHostLocal(principalID)
}

func trustedKnowledgeSourceForWrite(ctx context.Context, store *knowledge.SQLiteStore, principalID, id string) (knowledge.Source, error) {
	source, err := store.GetSource(ctx, id)
	if err != nil {
		return knowledge.Source{}, fmt.Errorf("trusted_knowledge_admin_not_found")
	}
	if !trustedKnowledgeSourceOwned(source, principalID) {
		return knowledge.Source{}, fmt.Errorf("trusted_knowledge_admin_not_found")
	}
	return source, nil
}

func semanticTrustedKnowledgeAdminProjection(kind string, source knowledge.Source) string {
	line := semanticTrustedKnowledgeAdminLine(source)
	switch kind {
	case "updated":
		return "知识来源已更新: " + line
	case "refreshed":
		return "知识来源已刷新: " + line
	default:
		return line
	}
}

func semanticTrustedKnowledgeAdminLine(source knowledge.Source) string {
	title := strings.TrimSpace(source.Title)
	if title == "" {
		title = strings.TrimSpace(source.Kind)
	}
	if title == "" {
		title = source.ID
	}
	return fmt.Sprintf("%s [%s] ID=%s", title, source.Status, source.ID)
}

func semanticTrustedKnowledgeAdminResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_knowledge_admin_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_knowledge_admin_empty")
	}
	return text, nil
}
