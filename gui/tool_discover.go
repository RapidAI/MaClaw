package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// toolDiscoverTool searches for matching tools and unlocks deferred tools that
// are explicitly selected through discovery.
func (h *IMMessageHandler) toolDiscoverTool(args map[string]interface{}) string {
	// Runtime execution injects the owner into args for this tool. Consume it
	// before discovery so a shared handler never consults the last-started loop
	// when two assistant sessions are active concurrently. Direct callers that
	// predate runtime-owner propagation retain the legacy fallback below.
	if ownerID, hasOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args); hasOwner {
		return h.toolDiscoverToolForOwner(ownerID, args)
	}
	ownerID, _ := h.currentRuntimePolicyOwnerState()
	return h.toolDiscoverToolForOwner(ownerID, args)
}

// toolDiscoverToolForOwner performs discovery for one explicitly identified
// assistant owner. An empty owner is only for legacy/direct callers; live
// agent-loop calls are required to pass the owner through tool runtime args.
func (h *IMMessageHandler) toolDiscoverToolForOwner(ownerID string, args map[string]interface{}) string {
	if h == nil {
		return "Tool registry not available."
	}
	ownerID = strings.TrimSpace(ownerID)
	need, _ := args["need"].(string)
	if need == "" {
		return "Missing 'need' parameter. Describe what capability you need."
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

	mcpMatches := h.discoverableMCPToolDocs()
	for id, item := range mcpMatches {
		text := strings.Join([]string{
			"mcp",
			"remote",
			"external",
			"tool",
			"search",
			"query",
			"lookup",
			"retrieve",
			"查找",
			"搜索",
			"查询",
			"检索",
			"内容",
			item.serverID,
			item.serverName,
			item.toolName,
			item.description,
		}, " ")
		docs = append(docs, bm25.Doc{ID: id, Text: text})
	}

	if len(docs) == 0 {
		return "No additional tools found beyond the core set."
	}

	idx.RebuildIfChanged(docs)
	scores := idx.Score(need)

	var ranked []discoveredToolScore
	for name, score := range scores {
		if score > 0 {
			ranked = append(ranked, discoveredToolScore{name: name, score: score})
		}
	}
	// Expert session: drop matches outside the expert allow-list so discovery
	// neither leaks unauthorized tool names nor activates them below.
	if ownerID != "" {
		if def := expertDefForUserID(ownerID); def != nil && len(def.Tools) > 0 {
			ranked = filterDiscoveredToolsForExpert(ranked, mcpMatches, def)
		}
	}
	// Group discovery is subject to the same policy as execution. Otherwise the
	// result can disclose local capabilities or session-pin a tool that should
	// never be offered in a group conversation.
	if ownerID != "" {
		if loopCtx := h.runtimeLoopContextForOwner(ownerID); loopCtx != nil && loopCtx.LansengerGroupPermissions != nil {
			ranked = filterDiscoveredToolsForLansengerGroup(ranked, mcpMatches, *loopCtx.LansengerGroupPermissions)
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

	activated := make(map[string]bool)
	if h.toolDefGen != nil {
		for _, item := range ranked {
			if h.toolDefGen.ActivateDeferredTool(item.name) {
				activated[item.name] = true
			}
		}
	}
	// Fail-closed names stay hidden until a plan or a loop-scoped discovery grant
	// exposes them. Grant only names the query actually mentioned so a broad
	// BM25 hit cannot unlock screenshot/browser alongside ssh.
	for _, item := range ranked {
		if discoveryNeedMentionsTool(need, item.name) && h.grantDiscoveredConditionalTool(ownerID, item.name) {
			activated[item.name] = true
		}
	}
	if len(activated) > 0 {
		h.toolsMu.Lock()
		h.cachedTools = nil
		h.toolsCacheTime = time.Time{}
		h.toolsMu.Unlock()
	}

	anyActivated := len(activated) > 0

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching tools:\n", len(ranked)))
	for i, item := range ranked {
		if mcp, ok := mcpMatches[item.name]; ok {
			desc := mcp.description
			if runes := []rune(desc); len(runes) > 120 {
				desc = string(runes[:120]) + "..."
			}
			b.WriteString(fmt.Sprintf("%d. **MCP capability pending managed replan** (%s/%s) - %s\n", i+1, mcp.serverID, mcp.toolName, desc))
			continue
		}
		t := toolMap[item.name]
		desc := t.Description
		if runes := []rune(desc); len(runes) > 120 {
			desc = string(runes[:120]) + "..."
		}
		isActivated := activated[item.name]
		b.WriteString(discoverToolStatusLine(i+1, item.name, desc, tool.CoreToolNames[item.name], isActivated))
	}
	if anyActivated {
		b.WriteString("\nActivated catalog entries are available for a subsequent planned step.")
	} else {
		b.WriteString("\nThe matched name is not in the current tool list yet. If it stays unmatched after this search, use another listed tool; do not keep calling discover_tool for the same name.")
	}
	if containsMCPDiscoveryMatch(ranked, mcpMatches) {
		b.WriteString("\nMCP matches are not executable on this legacy surface. Request a managed semantic replan; the host must bind the provider and tool before it is exposed.")
	}
	return b.String()
}

func shouldHideToolFromDiscovery(name string) bool {
	name = strings.TrimSpace(name)
	return name != MergedBrowserToolName && strings.HasPrefix(name, "browser_")
}

// filterDiscoveredToolsForExpert removes discovery matches outside the expert's
// tool allow-list. MCP matches map to the call_mcp_tool gateway tool: they are
// kept only when call_mcp_tool itself is allowed.
func filterDiscoveredToolsForExpert(ranked []discoveredToolScore, mcpMatches map[string]discoverableMCPTool, def *ExpertDefinition) []discoveredToolScore {
	if def == nil || len(def.Tools) == 0 {
		return ranked
	}
	allow := expertToolAllowSet(def)
	out := make([]discoveredToolScore, 0, len(ranked))
	for _, item := range ranked {
		if _, isMCP := mcpMatches[item.name]; isMCP {
			if allow["call_mcp_tool"] {
				out = append(out, item)
			}
			continue
		}
		if allow[item.name] {
			out = append(out, item)
		}
	}
	return out
}

func filterDiscoveredToolsForLansengerGroup(ranked []discoveredToolScore, mcpMatches map[string]discoverableMCPTool, policy lansengerGroupPermissionPolicy) []discoveredToolScore {
	out := make([]discoveredToolScore, 0, len(ranked))
	for _, item := range ranked {
		// MCP matches execute through call_mcp_tool, which intentionally has no
		// group-safe path/source contract.
		if _, isMCP := mcpMatches[item.name]; isMCP || !policy.allowsTool(item.name) {
			continue
		}
		out = append(out, item)
	}
	return out
}

type discoverableMCPTool struct {
	serverID    string
	serverName  string
	toolName    string
	description string
}

type discoveredToolScore struct {
	name  string
	score float64
}

func (h *IMMessageHandler) discoverableMCPToolDocs() map[string]discoverableMCPTool {
	out := make(map[string]discoverableMCPTool)
	if h == nil {
		return out
	}
	if mgr := h.getLocalMCPManager(); mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			for _, t := range ts.Tools {
				id := "mcp:local:" + ts.ServerID + ":" + t.Name
				out[id] = discoverableMCPTool{
					serverID:    ts.ServerID,
					serverName:  ts.ServerName,
					toolName:    t.Name,
					description: t.Description,
				}
			}
		}
	}
	if registry := h.getMCPRegistry(); registry != nil {
		for _, s := range registry.ListServers() {
			for _, t := range s.Tools {
				id := "mcp:remote:" + s.ID + ":" + t.Name
				out[id] = discoverableMCPTool{
					serverID:    s.ID,
					serverName:  s.Name,
					toolName:    t.Name,
					description: t.Description,
				}
			}
		}
	}
	return out
}

func containsMCPDiscoveryMatch(ranked []discoveredToolScore, mcpMatches map[string]discoverableMCPTool) bool {
	for _, item := range ranked {
		if _, ok := mcpMatches[item.name]; ok {
			return true
		}
	}
	return false
}

func discoveryNeedMentionsTool(need, name string) bool {
	need = strings.ToLower(strings.TrimSpace(need))
	name = strings.ToLower(strings.TrimSpace(name))
	if need == "" || name == "" {
		return false
	}
	if need == name {
		return true
	}
	for i := 0; i <= len(need)-len(name); i++ {
		if need[i:i+len(name)] != name {
			continue
		}
		leftOK := i == 0 || !isDiscoveryNameByte(need[i-1])
		rightOK := i+len(name) == len(need) || !isDiscoveryNameByte(need[i+len(name)])
		if leftOK && rightOK {
			return true
		}
	}
	return false
}

func isDiscoveryNameByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func (h *IMMessageHandler) grantDiscoveredConditionalTool(ownerID, name string) bool {
	if h == nil || !tool.IsFailClosedConditionalTool(name) {
		return false
	}
	loopCtx := h.runtimeLoopContextForOwner(ownerID)
	if loopCtx == nil {
		return false
	}
	return loopCtx.rememberDiscoveredConditionalTool(name)
}

func applyLoopDiscoveredConditionalTools(tools, baseTools, allTools []map[string]interface{}, ctx *LoopContext) ([]map[string]interface{}, []map[string]interface{}) {
	discovered := ctx.discoveredConditionalToolNames()
	if len(discovered) == 0 {
		return tools, baseTools
	}
	before := agentLoopToolNameSet(tools)
	tools = ensureNamedToolsPresent(tools, allTools, discovered)
	baseTools = ensureNamedToolsPresent(baseTools, allTools, discovered)
	if added := surfaceRecoveryAddedNames(before, tools); len(added) > 0 {
		log.Printf("[tool-surface] inject reason=loop-discovery tools=%s", strings.Join(added, ","))
	}
	return tools, baseTools
}

func agentLoopToolNameSet(tools []map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, def := range tools {
		if name := extractToolName(def); name != "" {
			out[name] = true
		}
	}
	return out
}

func surfaceRecoveryAddedNames(before map[string]bool, tools []map[string]interface{}) []string {
	var added []string
	for _, def := range tools {
		name := extractToolName(def)
		if name != "" && !before[name] {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	return added
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
