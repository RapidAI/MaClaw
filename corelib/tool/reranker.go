package tool

import (
	"fmt"
	"sort"
	"strings"
)

// CandidateSummary describes a candidate tool's summary information for reranker input.
type CandidateSummary struct {
	Name        string
	Description string
	BodySummary string
}

// Reranker defines the LLM listwise reranking interface.
// Implementations receive a user message and a list of candidate tool summaries,
// and return an ordered list of tool names ranked by relevance.
type Reranker interface {
	// Rerank reorders candidates by relevance to userMessage.
	// candidates contains at most 20 entries; returns at most topK tool names.
	Rerank(userMessage string, candidates []CandidateSummary, topK int) ([]string, error)
}

// BuildMCPToolBody constructs a readable body text from an MCP tool's inputSchema.
// Format: "Parameters:\n- name (type): description\n..."
// Returns empty string for nil or empty schema.
func BuildMCPToolBody(schema map[string]interface{}) string {
	if len(schema) == 0 {
		return ""
	}
	propsRaw, ok := schema["properties"]
	if !ok {
		return ""
	}
	props, ok := propsRaw.(map[string]interface{})
	if !ok || len(props) == 0 {
		return ""
	}

	// Collect and sort property names for deterministic output.
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Parameters:")
	for _, name := range names {
		propRaw := props[name]
		propMap, ok := propRaw.(map[string]interface{})
		if !ok {
			b.WriteString(fmt.Sprintf("\n- %s (any)", name))
			continue
		}

		typ := "any"
		if t, ok := propMap["type"].(string); ok && t != "" {
			typ = t
		}

		desc, hasDesc := propMap["description"].(string)
		if hasDesc && desc != "" {
			b.WriteString(fmt.Sprintf("\n- %s (%s): %s", name, typ, desc))
		} else {
			b.WriteString(fmt.Sprintf("\n- %s (%s)", name, typ))
		}
	}
	return b.String()
}
