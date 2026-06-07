package knowledge

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Shared formatting for knowledge tool execution results.
// Used by GUI, TUI, and agentservice (maclawsrv) to present knowledge_search
// and knowledge_context_pack results to the LLM in a consistent manner.
// Modify here → all platforms pick up changes.
// ---------------------------------------------------------------------------

// SearchResultsHeader is the guidance text prepended to formatted search results.
// It instructs the LLM to use results as evidence and search again if incomplete.
const SearchResultsHeader = "Use these results as evidence. Cite Source/Citation when answering. If the results do not fully cover the user's question, search again with refined terms or use knowledge_context_pack for comprehensive coverage.\n\n"

// EmptySearchResultMessage is returned when knowledge_search finds no matches.
const EmptySearchResultMessage = "No results found for this query. Try different search terms, broader keywords, or use knowledge_context_pack for topic-based retrieval."

// EmptyContextPackMessage is returned when knowledge_context_pack finds no matches.
const EmptyContextPackMessage = "No relevant knowledge found for this context pack query. Try different search terms or broader topic hints."

// FormatSearchResultsForLLM formats search results as structured text for LLM consumption.
// This is the shared implementation used by agentservice and TUI.
// GUI uses JSON serialization instead (for frontend rendering), but the guidance
// header principle is the same.
func FormatSearchResultsForLLM(results []SearchResult) string {
	if len(results) == 0 {
		return EmptySearchResultMessage
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	b.WriteString(SearchResultsHeader)
	for i, r := range results {
		b.WriteString(fmt.Sprintf("### Result %d (score: %.2f, type: %s)\n", i+1, r.Score, r.ResultType))
		if r.CardTitle != "" {
			b.WriteString(fmt.Sprintf("**Title**: %s\n", r.CardTitle))
		}
		if r.Claim != "" {
			b.WriteString(fmt.Sprintf("**Claim**: %s\n", r.Claim))
		}
		if r.Summary != "" {
			b.WriteString(fmt.Sprintf("**Summary**: %s\n", r.Summary))
		}
		if r.Snippet != "" && r.Snippet != r.Claim && r.Snippet != r.Summary {
			b.WriteString(fmt.Sprintf("**Snippet**: %s\n", r.Snippet))
		}
		if r.Subject != "" {
			b.WriteString(fmt.Sprintf("**Fact**: %s %s %s\n", r.Subject, r.Predicate, r.Object))
		}
		b.WriteString(fmt.Sprintf("**Source**: %s\n", FormatSourceLabel(r)))
		b.WriteString(fmt.Sprintf("**Citation**: %s\n", FormatCitationLabel(r)))
		b.WriteString("\n")
	}
	return b.String()
}

// FormatContextPackForLLM formats context pack results as structured text for LLM consumption.
func FormatContextPackForLLM(result ContextPackResult) string {
	if len(result.Items) == 0 {
		return EmptyContextPackMessage
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Knowledge context pack (%d items, %d chars):\n\n", result.Count, result.CharacterCount))
	for i, item := range result.Items {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, item.ResultType, item.Text))
		if item.Citation != "" {
			b.WriteString(fmt.Sprintf("   Citation: %s\n", item.Citation))
		}
	}
	return b.String()
}

// KnowledgeSearchToolDescription is the shared tool description for knowledge_search.
// Used by agentservice and can be referenced by GUI/TUI tool registrations.
const KnowledgeSearchToolDescription = "Search the local knowledge base (documents, URLs, saved text). Returns ranked knowledge cards, facts, and source citations without calling an LLM. Use when the user asks about saved knowledge, imported documents, or previously stored information. If results are incomplete for the user's question, search again with different keywords or use knowledge_context_pack for comprehensive multi-source coverage."

// KnowledgeContextPackToolDescription is the shared tool description for knowledge_context_pack.
const KnowledgeContextPackToolDescription = "Build a compact, citation-backed knowledge context pack from the local knowledge base. Use before answering from stored knowledge when you need a prompt-ready bundle of ranked cards and facts under a character budget. Especially useful for count/list questions (e.g., 'how many books/patents/projects') where multiple evidence items must be aggregated."

// FormatSourceLabel returns the best human-readable label for a search result's source.
func FormatSourceLabel(r SearchResult) string {
	for _, value := range []string{r.Source.Title, r.Source.RelativePath, r.Source.URI, r.Source.ID} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown source"
}

// FormatCitationLabel returns the best human-readable citation for a search result.
func FormatCitationLabel(r SearchResult) string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(r.Citation) != "" {
		parts = append(parts, strings.TrimSpace(r.Citation))
	}
	if r.Page > 0 {
		parts = append(parts, fmt.Sprintf("page %d", r.Page))
	}
	for _, value := range []string{r.SheetName, r.RowRange, r.ColRange, r.NodeTitle} {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	if len(parts) == 0 {
		return "source item"
	}
	return strings.Join(parts, ", ")
}
