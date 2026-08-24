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
	semanticTrustedKnowledgeReadAdapter        = "semantic_read_trusted_knowledge"
	semanticTrustedKnowledgeReadImplementation = "trusted-knowledge-read-v1"
	semanticTrustedKnowledgeReadLimit          = 8
	semanticTrustedKnowledgeReadTimeout        = 10 * time.Second
)

func semanticUnpublishedLegacyKnowledgeReadProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityKnowledgeReadLocal {
			return true
		}
	}
	return false
}

func semanticTrustedKnowledgeReadDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedKnowledgeReadAdapter,
			"description": "Search the current principal's local knowledge store. Only a query is accepted.",
			"parameters":  semanticTrustedKnowledgeReadInvocationSchema(),
		},
	}
}

func semanticTrustedKnowledgeReadInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func semanticTrustedKnowledgeReadArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("trusted_knowledge_read_arguments_rejected")
	}
	raw, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("trusted_knowledge_read_arguments_rejected")
	}
	query, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("trusted_knowledge_read_arguments_rejected")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("trusted_knowledge_read_query_required")
	}
	return query, nil
}

func (h *IMMessageHandler) readTrustedKnowledge(principalID, query string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_knowledge_read_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_knowledge_read_principal_required")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("trusted_knowledge_read_query_required")
	}
	if h.semanticTrustedKnowledgeRead != nil {
		return h.semanticTrustedKnowledgeRead(principalID, query)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_knowledge_read_unavailable")
	}
	store, err := h.app.openKnowledgeStore()
	if err != nil {
		return "", fmt.Errorf("trusted_knowledge_read_unavailable")
	}
	defer store.Close()
	ctx, cancel := trustedKnowledgeReadContext(h.app.knowledgeContext())
	defer cancel()
	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query:   query,
		OwnerID: principalID,
		Limit:   semanticTrustedKnowledgeReadLimit,
	})
	if err != nil {
		return "", err
	}
	owned := make([]knowledge.SearchResult, 0, len(results))
	for _, item := range results {
		if !trustedKnowledgeSourceOwned(item.Source, principalID) {
			continue
		}
		owned = append(owned, item)
	}
	return semanticTrustedKnowledgeReadProjection(owned), nil
}

func trustedKnowledgeReadContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return parent, func() {}
	}
	return context.WithTimeout(parent, semanticTrustedKnowledgeReadTimeout)
}

func semanticTrustedKnowledgeReadProjection(results []knowledge.SearchResult) string {
	results = knowledge.ProjectImageSearchResultsForTool(results)
	if len(results) == 0 {
		return "No matching knowledge found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d knowledge hits:\n", len(results))
	for i, item := range results {
		title := strings.TrimSpace(item.CardTitle)
		if title == "" {
			title = strings.TrimSpace(item.Source.Title)
		}
		if title == "" {
			title = strings.TrimSpace(item.Source.ID)
		}
		if title == "" {
			title = "knowledge hit"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, title)
		if body := strings.TrimSpace(knowledge.BestContentText(item)); body != "" && body != title {
			fmt.Fprintf(&b, "   %s\n", body)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func semanticTrustedKnowledgeReadResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_knowledge_read_delivery_token")
	}
	if strings.Contains(text, "knowledge_search") || strings.Contains(text, "knowledge_context_pack") || strings.Contains(text, "knowledge_image_search") {
		return "", fmt.Errorf("trusted_knowledge_read_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_knowledge_read_empty")
	}
	return text, nil
}
