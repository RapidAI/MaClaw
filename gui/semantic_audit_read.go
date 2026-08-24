package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/session"
)

const (
	semanticTrustedAuditReadAdapter        = "semantic_read_trusted_audit"
	semanticTrustedAuditReadImplementation = "trusted-audit-read-v1"
	semanticTrustedAuditResultLimit        = 20
	semanticTrustedAuditSnippetRunes       = 160
)

func semanticUnpublishedLegacyAuditProvider(registered RegisteredTool) bool {
	return registered.Name == "session_search" || registered.Name == "check_health"
}

func semanticTrustedAuditReadDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedAuditReadAdapter,
			"description": "Read the current principal's security audit events and conversation snippets together. Identity is host-bound; only an optional query is accepted.",
			"parameters":  semanticTrustedAuditInvocationSchema(),
		},
	}
}

func semanticTrustedAuditInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedAuditArgsAllowed(args map[string]interface{}) (string, error) {
	switch len(args) {
	case 0:
		return "", nil
	case 1:
		raw, ok := args["query"]
		if !ok {
			return "", fmt.Errorf("trusted_audit_arguments_rejected")
		}
		query, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_audit_arguments_rejected")
		}
		return strings.TrimSpace(query), nil
	default:
		return "", fmt.Errorf("trusted_audit_arguments_rejected")
	}
}

func (h *IMMessageHandler) readTrustedAudit(principalID, query string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_audit_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_audit_principal_required")
	}
	if h.semanticTrustedAuditRead != nil {
		return h.semanticTrustedAuditRead(principalID, strings.TrimSpace(query))
	}
	events, eventErr := h.listTrustedAuditEvents(principalID, query)
	conversations, convErr := h.listTrustedAuditConversations(principalID, query)
	if errors.Is(eventErr, errTrustedAuditStoreAbsent) && errors.Is(convErr, errTrustedAuditStoreAbsent) {
		// Neither half exists on this instance, so there is no partial answer
		// worth composing. Refusing states the same thing an empty report
		// would have implied, without implying it falsely.
		return "", fmt.Errorf("trusted_audit_unavailable")
	}
	if eventErr != nil && convErr != nil {
		return "", eventErr
	}
	if eventErr != nil {
		events = nil
	}
	if convErr != nil {
		conversations = nil
	}
	if eventErr == nil && convErr == nil && len(events) == 0 && len(conversations) == 0 {
		return "No matching audit events or conversations for the current principal.", nil
	}
	var b strings.Builder
	if eventErr != nil {
		b.WriteString("audit events (unavailable):\n")
	} else {
		b.WriteString(fmt.Sprintf("audit events (%d):\n", len(events)))
	}
	for _, event := range events {
		b.WriteString("- ")
		b.WriteString(event.Timestamp.UTC().Format(time.RFC3339))
		if event.ToolName != "" {
			b.WriteString(" tool=")
			b.WriteString(event.ToolName)
		}
		if event.Action != "" {
			b.WriteString(" action=")
			b.WriteString(string(event.Action))
		}
		if event.RiskLevel != "" {
			b.WriteString(" risk=")
			b.WriteString(string(event.RiskLevel))
		}
		if event.PolicyAction != "" {
			b.WriteString(" decision=")
			b.WriteString(string(event.PolicyAction))
		}
		if event.Result != "" {
			b.WriteString(" result=")
			b.WriteString(trustedAuditSnippet(event.Result))
		}
		b.WriteByte('\n')
	}
	if convErr != nil {
		b.WriteString("\nconversations (unavailable):\n")
	} else {
		b.WriteString(fmt.Sprintf("\nconversations (%d):\n", len(conversations)))
	}
	for _, hit := range conversations {
		b.WriteString("- ")
		b.WriteString(hit.timestamp)
		if hit.platform != "" {
			b.WriteString(" platform=")
			b.WriteString(hit.platform)
		}
		if hit.sessionID != "" {
			b.WriteString(" session=")
			b.WriteString(hit.sessionID)
		}
		if hit.topic != "" {
			b.WriteString(" topic=")
			b.WriteString(hit.topic)
		}
		if hit.snippet != "" {
			b.WriteByte(' ')
			b.WriteString(hit.snippet)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String()), nil
}

// errTrustedAuditStoreAbsent separates "this instance has no such store" from
// "the store was read and matched nothing".
//
// Both used to arrive as an empty slice and a nil error, and readTrustedAudit
// answered them identically: "No matching audit events or conversations for the
// current principal." That is a finding, and the host was in no position to
// report one — nothing had been searched. A model handed a negative result
// reports it to the user as a negative result, so an instance with no audit
// store did not degrade quietly, it asserted that nothing had happened.
//
// A missing store is an instance-level fact, not a per-turn outcome. Naming it
// lets the answer say the capability is unavailable here, and lets the turn fail
// when neither half of it can be served.
var errTrustedAuditStoreAbsent = errors.New("trusted_audit_store_absent")

func (h *IMMessageHandler) listTrustedAuditEvents(principalID, query string) ([]security.AuditEntry, error) {
	log := h.getAuditLog()
	if log == nil {
		return nil, errTrustedAuditStoreAbsent
	}
	query = strings.ToLower(strings.TrimSpace(query))
	return log.QueryNewestMatching(func(entry security.AuditEntry) bool {
		if !trustedAuditEntryVisibleToPrincipal(entry, principalID) {
			return false
		}
		return query == "" || trustedAuditEventMatches(entry, query)
	}, semanticTrustedAuditResultLimit)
}

func (h *IMMessageHandler) listTrustedAuditConversations(principalID, query string) ([]trustedAuditConversationHit, error) {
	store := h.getSessionStore()
	if store == nil {
		return nil, errTrustedAuditStoreAbsent
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return listTrustedAuditRecentConversations(store, principalID)
	}
	results, err := store.SearchOwned(trustedAuditFTSQuery(query), principalID, semanticTrustedAuditResultLimit)
	if err != nil {
		return nil, err
	}
	hits := make([]trustedAuditConversationHit, 0, semanticTrustedAuditResultLimit)
	for _, result := range results {
		if result.SessionID == "" && result.Snippet == "no results found" {
			continue
		}
		if !trustedAuditSessionOwnedByPrincipal(result.SessionID, principalID) {
			continue
		}
		hits = append(hits, trustedAuditConversationHit{
			timestamp: result.Timestamp,
			platform:  result.Platform,
			sessionID: result.SessionID,
			topic:     result.Topic,
			snippet:   trustedAuditConversationSnippet(result.Snippet),
		})
	}
	return hits, nil
}

func listTrustedAuditRecentConversations(store *session.Store, principalID string) ([]trustedAuditConversationHit, error) {
	if store == nil {
		return nil, nil
	}
	summaries, err := store.ListRecentOwned(principalID, semanticTrustedAuditResultLimit)
	if err != nil {
		return nil, err
	}
	hits := make([]trustedAuditConversationHit, 0, len(summaries))
	for _, summary := range summaries {
		if !trustedAuditSessionOwnedByPrincipal(summary.SessionID, principalID) {
			continue
		}
		hits = append(hits, trustedAuditConversationHit{
			timestamp: summary.Timestamp,
			platform:  summary.Platform,
			sessionID: summary.SessionID,
			topic:     summary.Topic,
			snippet:   trustedAuditConversationSnippet(summary.Snippet),
		})
	}
	return hits, nil
}

func trustedAuditFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	query = strings.ReplaceAll(query, `"`, `""`)
	return `"` + query + `"`
}

type trustedAuditConversationHit struct {
	timestamp string
	platform  string
	sessionID string
	topic     string
	snippet   string
}

func trustedAuditEntryVisibleToPrincipal(entry security.AuditEntry, principalID string) bool {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return false
	}
	userID := strings.TrimSpace(entry.UserID)
	if userID == principalID {
		return true
	}
	if userID != "" {
		return false
	}
	if trustedAuditSessionOwnedByPrincipal(entry.SessionID, principalID) {
		return true
	}
	return trustedDesktopPrincipal(principalID)
}

func trustedAuditSessionOwnedByPrincipal(sessionID, principalID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	principalID = strings.TrimSpace(principalID)
	if sessionID == "" || principalID == "" {
		return false
	}
	if sessionID == principalID {
		return true
	}
	prefix := principalID + "_"
	if !strings.HasPrefix(sessionID, prefix) {
		return false
	}
	suffix := sessionID[len(prefix):]
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func trustedAuditEventMatches(entry security.AuditEntry, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		entry.ToolName,
		string(entry.Action),
		string(entry.RiskLevel),
		string(entry.PolicyAction),
		entry.Result,
		entry.SessionID,
		entry.Source,
	}, " "))
	return strings.Contains(haystack, query)
}

func trustedAuditConversationSnippet(content string) string {
	content = strings.ReplaceAll(content, "<b>", "")
	content = strings.ReplaceAll(content, "</b>", "")
	return trustedAuditSnippet(content)
}

func trustedAuditSnippet(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) > semanticTrustedAuditSnippetRunes {
		return string(runes[:semanticTrustedAuditSnippetRunes]) + "..."
	}
	return content
}

func semanticTrustedAuditResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_audit_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_audit_empty")
	}
	return text, nil
}
