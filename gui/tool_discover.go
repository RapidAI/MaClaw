package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// toolDiscoverTool handles the "discover_tool" tool call.
// It searches the full registry for tools matching the user's described need,
// activates matched tools in the current session, and returns their descriptions
// so the LLM can use them in subsequent turns.
func (h *IMMessageHandler) toolDiscoverTool(args map[string]interface{}) string {
	need, _ := args["need"].(string)
	if need == "" {
		return "Missing 'need' parameter. Describe what capability you need."
	}

	if h.registry == nil {
		return "Tool registry not available."
	}

	// Collect all available tools from the GUI registry.
	allTools := h.registry.ListAvailable()
	if len(allTools) == 0 {
		return "No tools available in registry."
	}

	// Build BM25 index over all tools (including those not currently routed).
	idx := bm25.New()
	docs := make([]bm25.Doc, 0, len(allTools))
	toolMap := make(map[string]RegisteredTool, len(allTools))

	for _, t := range allTools {
		// Skip core tools that are already always included.
		if tool.CoreToolNames[t.Name] {
			continue
		}
		text := t.Name + " " + t.Description
		for _, tag := range t.Tags {
			text += " " + tag
		}
		// Use enrichment if available via corelib store.
		docs = append(docs, bm25.Doc{ID: t.Name, Text: text})
		toolMap[t.Name] = t
	}

	if len(docs) == 0 {
		return "No additional tools found beyond the core set."
	}

	idx.RebuildIfChanged(docs)
	scores := idx.Score(need)

	// Rank by score, take top 5.
	type scored struct {
		name  string
		score float64
	}
	var ranked []scored
	for name, s := range scores {
		if s > 0 {
			ranked = append(ranked, scored{name: name, score: s})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > 5 {
		ranked = ranked[:5]
	}

	if len(ranked) == 0 {
		return fmt.Sprintf("No matching tools found for: %q. Try rephrasing your need or use craft_tool to create a custom script.", need)
	}

	// Activate matched tools in the session so they appear in subsequent turns.
	if h.toolRouter != nil {
		for _, r := range ranked {
			h.toolRouter.ActivateSessionTool(r.name)
		}
	}

	// Format result.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching tools (now activated for this session):\n", len(ranked)))
	for i, r := range ranked {
		t := toolMap[r.name]
		desc := t.Description
		if runes := []rune(desc); len(runes) > 120 {
			desc = string(runes[:120]) + "..."
		}
		b.WriteString(fmt.Sprintf("%d. **%s** — %s\n", i+1, r.name, desc))
	}
	b.WriteString("\nThese tools are now available. Call them directly in your next action.")
	return b.String()
}
