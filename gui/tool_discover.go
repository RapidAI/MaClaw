package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// toolDiscoverTool searches for matching tools and unlocks deferred tools that
// are explicitly selected through discovery.
func (h *IMMessageHandler) toolDiscoverTool(args map[string]interface{}) string {
	need, _ := args["need"].(string)
	if need == "" {
		return "Missing 'need' parameter. Describe what capability you need."
	}

	if h.registry == nil && h.toolDefGen == nil {
		return "Tool registry not available."
	}

	var allTools []RegisteredTool
	if h.registry != nil {
		allTools = h.registry.ListAvailable()
	}

	idx := bm25.New()
	docs := make([]bm25.Doc, 0, len(allTools))
	toolMap := make(map[string]RegisteredTool, len(allTools))

	for _, t := range allTools {
		if shouldHideToolFromDiscovery(t.Name) {
			continue
		}
		if strings.TrimSpace(t.Description) == "" {
			continue
		}
		text := t.Name + " " + t.Description
		for _, tag := range t.Tags {
			text += " " + tag
		}
		docs = append(docs, bm25.Doc{ID: t.Name, Text: text})
		toolMap[t.Name] = t
	}

	if h.toolDefGen != nil {
		for _, def := range h.toolDefGen.GenerateDeferred() {
			name := extractToolName(def)
			if shouldHideToolFromDiscovery(name) {
				continue
			}
			if name == "" || toolMap[name].Name != "" {
				continue
			}
			desc := extractToolDescription(def)
			docs = append(docs, bm25.Doc{ID: name, Text: name + " " + desc})
			toolMap[name] = RegisteredTool{Name: name, Description: desc}
		}
	}

	if len(docs) == 0 {
		return "No additional tools found beyond the core set."
	}

	idx.RebuildIfChanged(docs)
	scores := idx.Score(need)

	type scored struct {
		name  string
		score float64
	}
	var ranked []scored
	for name, score := range scores {
		if score > 0 {
			ranked = append(ranked, scored{name: name, score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].name < ranked[j].name
	})
	if len(ranked) > 5 {
		ranked = ranked[:5]
	}

	if len(ranked) == 0 {
		return fmt.Sprintf("No matching tools found for: %q. Try rephrasing your need or use craft_tool to create a custom script.", need)
	}

	activatedDeferred := make(map[string]bool)
	activatedConditional := make(map[string]bool)
	if h.toolDefGen != nil {
		for _, item := range ranked {
			if h.toolDefGen.ActivateDeferredTool(item.name) {
				activatedDeferred[item.name] = true
			}
		}
		if len(activatedDeferred) > 0 {
			h.toolsMu.Lock()
			h.cachedTools = nil
			h.toolsCacheTime = time.Time{}
			h.toolsMu.Unlock()
		}
	}

	// Session-pin discovered conditional tools so they appear in subsequent
	// Route() calls within the same agent loop. Without this, conditional
	// tools (ssh, web_search, etc.) found by discover_tool remain invisible
	// to the LLM because Route() only activates them via UIC/keyword
	// matching on the original user message. Session-pinning makes them
	// persist for the rest of the session regardless of message content.
	if h.toolRouter != nil {
		for _, item := range ranked {
			if tool.CoreToolNames[item.name] || activatedDeferred[item.name] {
				continue
			}
			if tool.ShouldPinConditionalTool(item.name) {
				h.toolRouter.ActivateSessionTool(item.name)
				activatedConditional[item.name] = true
			}
		}
		if len(activatedConditional) > 0 {
			// Invalidate the tool cache so the next getTools() + routeTools()
			// call (at the start of the next iteration) will include these
			// newly session-pinned tools in the LLM tool list.
			h.toolsMu.Lock()
			h.cachedTools = nil
			h.toolsCacheTime = time.Time{}
			h.toolsMu.Unlock()
		}
	}

	anyActivated := len(activatedDeferred) > 0 || len(activatedConditional) > 0

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching tools:\n", len(ranked)))
	for i, item := range ranked {
		t := toolMap[item.name]
		desc := t.Description
		if runes := []rune(desc); len(runes) > 120 {
			desc = string(runes[:120]) + "..."
		}
		isActivated := activatedDeferred[item.name] || activatedConditional[item.name]
		b.WriteString(discoverToolStatusLine(i+1, item.name, desc, tool.CoreToolNames[item.name], isActivated))
	}
	if anyActivated {
		b.WriteString("\nActivated tools are now available. Call them directly in your next step.")
	} else {
		b.WriteString("\nUse the matched tool name when the next step needs that capability.")
	}
	return b.String()
}

func shouldHideToolFromDiscovery(name string) bool {
	name = strings.TrimSpace(name)
	return name != MergedBrowserToolName && strings.HasPrefix(name, "browser_")
}

func discoverToolStatusLine(index int, name, desc string, coreTool, activated bool) string {
	switch {
	case coreTool:
		return fmt.Sprintf("%d. **%s** (core, already available) - %s\n", index, name, desc)
	case activated:
		return fmt.Sprintf("%d. **%s** (activated) - %s\n", index, name, desc)
	default:
		return fmt.Sprintf("%d. **%s** (matched) - %s\n", index, name, desc)
	}
}
