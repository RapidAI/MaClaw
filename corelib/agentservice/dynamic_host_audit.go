package agentservice

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostAuditProviderID     = "core-audit"
	reviewedHostAuditImplementation = "local"
	reviewedHostAuditAdapterName    = "host_security_audit_read"
	reviewedHostAuditResultLimit    = 20
	reviewedHostAuditSnippetRunes   = 160
)

func reviewedHostAuditInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostAuditContractDigest() string {
	return coretool.SchemaDigest([]byte("security.audit.read:v1:host-audit"))
}

// ProjectReviewedHostAuditProvider projects the host-owned principal-scoped
// audit read. It is not a Skill/MCP discovery entry and must not import GUI
// query_audit_log, session_search, or check_health. The closed schema accepts
// only an optional query; tenant/user identity comes from the principal.
// The host reader always returns audit events and conversation snippets
// together; it does not branch on health vs history vs audit keywords.
func ProjectReviewedHostAuditProvider(reader reviewedHostAuditReader) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if reader == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host audit reader is unavailable")
	}
	parameters := reviewedHostAuditInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host audit schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostAuditContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-audit-query-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostAuditAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostAuditProviderID,
			ImplementationID: reviewedHostAuditImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityAuditRead,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostAudit(reader)}, nil
}

func AttachReviewedHostAuditProvider(catalog DynamicSemanticCatalog, reader reviewedHostAuditReader) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostAuditProvider(reader)
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

func executeReviewedHostAudit(reader reviewedHostAuditReader) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if reader == nil {
			return "", fmt.Errorf("host_audit_unavailable")
		}
		query := ""
		switch len(args) {
		case 0:
		case 1:
			raw, ok := args["query"]
			if !ok {
				return "", fmt.Errorf("host_audit_arguments_rejected")
			}
			query, ok = raw.(string)
			if !ok {
				return "", fmt.Errorf("host_audit_arguments_rejected")
			}
		default:
			return "", fmt.Errorf("host_audit_arguments_rejected")
		}
		return reader.ReadReviewedHostAudit(ctx, principal, strings.TrimSpace(query))
	}
}

type serviceReviewedHostAuditReader struct {
	svc *Service
}

func (r serviceReviewedHostAuditReader) ReadReviewedHostAudit(ctx context.Context, principal Principal, query string) (string, error) {
	if r.svc == nil {
		return "", fmt.Errorf("host_audit_unavailable")
	}
	tenantID := strings.TrimSpace(principal.TenantID)
	userID := strings.TrimSpace(principal.UserID)
	if tenantID == "" || userID == "" {
		return "", fmt.Errorf("host_audit_principal_required")
	}
	events, err := r.svc.ListAuditEvents(ctx, ListAuditEventsInput{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	events = redactAuditEventsForExport(r.svc.dataRoot, events)
	query = strings.ToLower(strings.TrimSpace(query))
	if query != "" {
		filtered := make([]AuditEvent, 0, len(events))
		for _, event := range events {
			if reviewedHostAuditEventMatches(event, query) {
				filtered = append(filtered, event)
			}
		}
		events = filtered
	}
	if len(events) > reviewedHostAuditResultLimit {
		events = events[len(events)-reviewedHostAuditResultLimit:]
	}
	conversations, err := r.listReviewedHostConversations(principal, query)
	if err != nil {
		return "", err
	}
	if len(events) == 0 && len(conversations) == 0 {
		return "No matching audit events or conversations for the current principal.", nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("audit events (%d):\n", len(events)))
	for _, event := range events {
		b.WriteString("- ")
		b.WriteString(event.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString(" action=")
		b.WriteString(strings.TrimSpace(event.Action))
		if event.ResourceType != "" {
			b.WriteString(" resource=")
			b.WriteString(event.ResourceType)
		}
		if event.ResourceID != "" {
			b.WriteString(" id=")
			b.WriteString(event.ResourceID)
		}
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("\nconversations (%d):\n", len(conversations)))
	for _, hit := range conversations {
		b.WriteString("- ")
		b.WriteString(hit.CreatedAt.UTC().Format(time.RFC3339))
		b.WriteString(" role=")
		b.WriteString(string(hit.Role))
		if hit.SessionID != "" {
			b.WriteString(" session=")
			b.WriteString(hit.SessionID)
		}
		if hit.Snippet != "" {
			b.WriteByte(' ')
			b.WriteString(hit.Snippet)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String()), nil
}

type reviewedHostConversationHit struct {
	CreatedAt time.Time
	Role      MessageRole
	SessionID string
	Snippet   string
}

func (r serviceReviewedHostAuditReader) listReviewedHostConversations(principal Principal, query string) ([]reviewedHostConversationHit, error) {
	if r.svc == nil || r.svc.store == nil {
		return nil, nil
	}
	tenantID := strings.TrimSpace(principal.TenantID)
	userID := strings.TrimSpace(principal.UserID)
	instances, err := r.svc.store.ListInstances(tenantID, userID)
	if err != nil {
		return nil, err
	}
	hits := make([]reviewedHostConversationHit, 0)
	for _, inst := range instances {
		if strings.TrimSpace(inst.TenantID) != tenantID || strings.TrimSpace(inst.UserID) != userID {
			continue
		}
		sessions, err := r.svc.store.ListSessions(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			if strings.TrimSpace(sess.TenantID) != tenantID || strings.TrimSpace(sess.UserID) != userID {
				continue
			}
			messages, err := r.svc.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			for _, msg := range messages {
				if msg.Role != MessageRoleUser && msg.Role != MessageRoleAssistant {
					continue
				}
				content := strings.TrimSpace(msg.Content)
				if content == "" {
					continue
				}
				if query != "" && !strings.Contains(strings.ToLower(content), query) {
					continue
				}
				hits = append(hits, reviewedHostConversationHit{
					CreatedAt: msg.CreatedAt,
					Role:      msg.Role,
					SessionID: strings.TrimSpace(sess.ID),
					Snippet:   reviewedHostAuditSnippet(r.svc.dataRoot, content),
				})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].CreatedAt.Before(hits[j].CreatedAt) })
	if len(hits) > reviewedHostAuditResultLimit {
		hits = hits[len(hits)-reviewedHostAuditResultLimit:]
	}
	return hits, nil
}

func reviewedHostAuditSnippet(dataRoot, content string) string {
	content = redactAuditExportValue(dataRoot, strings.TrimSpace(content))
	runes := []rune(content)
	if len(runes) > reviewedHostAuditSnippetRunes {
		return string(runes[:reviewedHostAuditSnippetRunes]) + "..."
	}
	return content
}

func reviewedHostAuditEventMatches(event AuditEvent, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		event.Action, event.ResourceType, event.ResourceID, event.ActorType,
	}, " "))
	if strings.Contains(haystack, query) {
		return true
	}
	for key, value := range event.Metadata {
		if strings.Contains(strings.ToLower(key+" "+value), query) {
			return true
		}
	}
	return false
}
