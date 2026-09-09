package guiapp

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// legacyDiscoverToolMaxPerTurn bounds discover_tool on one loop. Discovery is
// the path from an unlisted name to a loop-scoped grant; once that grant is
// issued (or the name is already on the surface), repeating the search cannot
// change the surface and is the 2026-08-31 copy-file spiral.
const legacyDiscoverToolMaxPerTurn = 4

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
	need = strings.TrimSpace(need)
	if need == "" {
		return "Missing 'need' parameter. Describe what capability you need."
	}

	loopCtx := h.runtimeLoopContextForOwner(ownerID)
	if loopCtx != nil {
		if n := loopCtx.noteDiscoverToolCall(); n > legacyDiscoverToolMaxPerTurn {
			return fmt.Sprintf("discover_tool reached its limit of %d calls for this turn. Discovery cannot change this turn's tool surface further; call a name already listed or granted, and state plainly what remains unfinished.", legacyDiscoverToolMaxPerTurn)
		}
	}

	var allTools []RegisteredTool
	if h.registry != nil {
		allTools = h.registry.ListAvailable()
	}

	idx := bm25.New()
	docs := make([]bm25.Doc, 0, len(allTools))
	toolMap := make(map[string]RegisteredTool, len(allTools))

	addDiscoveryDoc := func(name, desc string, tags []string) {
		name = strings.TrimSpace(name)
		if name == "" || shouldHideToolFromDiscovery(name) {
			return
		}
		if toolMap[name].Name != "" {
			return
		}
		if strings.TrimSpace(desc) == "" {
			desc = name
		}
		text := name + " " + desc
		for _, tag := range tags {
			text += " " + tag
		}
		docs = append(docs, bm25.Doc{ID: name, Text: text})
		toolMap[name] = RegisteredTool{Name: name, Description: desc, Tags: tags}
	}

	for _, t := range allTools {
		addDiscoveryDoc(t.Name, t.Description, t.Tags)
	}

	if h.toolDefGen != nil {
		for _, def := range h.toolDefGen.Generate() {
			addDiscoveryDoc(extractToolName(def), extractToolDescription(def), nil)
		}
		for _, def := range h.toolDefGen.GenerateDeferred() {
			addDiscoveryDoc(extractToolName(def), extractToolDescription(def), nil)
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
	mentioned := discoveryMentionedNameSet(need, toolMap)
	ranked = pinMentionedDiscoveryTools(mentioned, ranked)
	ranked = h.filterDiscoveryRanking(ownerID, loopCtx, ranked, mcpMatches)
	ranked = finalizeDiscoveryRanking(mentioned, ranked)

	if len(ranked) == 0 {
		return fmt.Sprintf("No matching tools found for: %q. Try rephrasing your need or use craft_tool to create a custom script.", need)
	}

	onSurface := map[string]bool{}
	if loopCtx != nil {
		if names := loopCtx.exposedToolNameSet(); names != nil {
			onSurface = names
		}
	}

	activated := make(map[string]bool)
	if h.toolDefGen != nil {
		for _, item := range ranked {
			if h.toolDefGen.ActivateDeferredTool(item.name) {
				activated[item.name] = true
			}
		}
	}
	// Grant only names the query actually mentioned so a broad BM25 hit cannot
	// unlock screenshot/browser alongside a named host tool. Names already on
	// this turn's surface do not need a grant. The grant is loop-scoped and is
	// rendered on the next closed surface.
	for _, item := range ranked {
		if _, isMCP := mcpMatches[item.name]; isMCP || onSurface[item.name] {
			continue
		}
		if mentioned[item.name] && h.grantDiscoveredSurfaceTool(ownerID, item.name) {
			activated[item.name] = true
		}
	}
	if len(activated) > 0 {
		h.toolsMu.Lock()
		h.cachedTools = nil
		h.toolsCacheTime = time.Time{}
		h.toolsMu.Unlock()
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching tools:\n", len(ranked)))
	anyGranted := false
	anyOnSurface := false
	anyBlocked := false
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
		blocked := h.surfaceNameRejected(item.name)
		if blocked {
			anyBlocked = true
		}
		if onSurface[item.name] {
			anyOnSurface = true
		}
		if activated[item.name] && !onSurface[item.name] && !blocked {
			anyGranted = true
		}
		b.WriteString(discoverToolStatusLine(i+1, item.name, desc, onSurface[item.name], activated[item.name], blocked))
	}
	switch {
	case anyGranted:
		b.WriteString("\nGranted names will be on the next model request in this turn. Call them then; do not rediscover the same name.")
	case anyOnSurface:
		b.WriteString("\nA listed name is already on this turn's tool surface. Call it directly; do not keep calling discover_tool for the same name.")
	case anyBlocked:
		b.WriteString("\nA blocked name cannot be added to this turn. Use another listed tool; do not keep calling discover_tool for the same name.")
	default:
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
	if mgr := h.peekLocalMCPManager(); mgr != nil {
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
	return b == '_' || b == '-' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

const discoveryMentionScore = 1e9

func discoveryMentionedNameSet(need string, toolMap map[string]RegisteredTool) map[string]bool {
	out := make(map[string]bool)
	if strings.TrimSpace(need) == "" || len(toolMap) == 0 {
		return out
	}
	for name := range toolMap {
		if discoveryNeedMentionsTool(need, name) {
			out[name] = true
		}
	}
	return out
}

func pinMentionedDiscoveryTools(mentioned map[string]bool, ranked []discoveredToolScore) []discoveredToolScore {
	if len(mentioned) == 0 {
		return ranked
	}
	seen := make(map[string]bool, len(ranked)+len(mentioned))
	for _, item := range ranked {
		seen[item.name] = true
	}
	for name := range mentioned {
		if seen[name] {
			continue
		}
		ranked = append(ranked, discoveredToolScore{name: name, score: discoveryMentionScore})
		seen[name] = true
	}
	return ranked
}

func (h *IMMessageHandler) filterDiscoveryRanking(ownerID string, loopCtx *LoopContext, ranked []discoveredToolScore, mcpMatches map[string]discoverableMCPTool) []discoveredToolScore {
	if ownerID == "" {
		return ranked
	}
	if def := expertDefForUserID(ownerID); def != nil && len(def.Tools) > 0 {
		ranked = filterDiscoveredToolsForExpert(ranked, mcpMatches, def)
	}
	if loopCtx != nil && loopCtx.LansengerGroupPermissions != nil {
		ranked = filterDiscoveredToolsForLansengerGroup(ranked, mcpMatches, *loopCtx.LansengerGroupPermissions)
	}
	return ranked
}

// finalizeDiscoveryRanking keeps every catalog name the query mentioned, then
// fills remaining slots from BM25. The cap exists to bound retrieval noise; an
// explicit identifier is not noise and must not be dropped by the cap.
func finalizeDiscoveryRanking(mentioned map[string]bool, ranked []discoveredToolScore) []discoveredToolScore {
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].name < ranked[j].name
	})
	kept := make([]discoveredToolScore, 0, len(ranked))
	rest := make([]discoveredToolScore, 0, len(ranked))
	for _, item := range ranked {
		if mentioned[item.name] {
			kept = append(kept, item)
		} else {
			rest = append(rest, item)
		}
	}
	out := kept
	for _, item := range rest {
		if len(out) >= 5 {
			break
		}
		out = append(out, item)
	}
	return out
}

func (h *IMMessageHandler) grantDiscoveredConditionalTool(ownerID, name string) bool {
	return h.grantDiscoveredSurfaceTool(ownerID, name)
}

func (h *IMMessageHandler) grantDiscoveredSurfaceTool(ownerID, name string) bool {
	name = strings.TrimSpace(name)
	if h == nil || name == "" || h.surfaceNameRejected(name) {
		return false
	}
	loopCtx := h.runtimeLoopContextForOwner(ownerID)
	if loopCtx == nil {
		return false
	}
	return loopCtx.rememberDiscoveredConditionalTool(name)
}

func (h *IMMessageHandler) surfaceNameRejected(name string) bool {
	if h == nil || strings.TrimSpace(name) == "" {
		return false
	}
	probe := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name": name,
			},
		},
	}
	return len(h.filterPolicyRejectedSurfaceTools(probe)) == 0
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

func discoverToolStatusLine(index int, name, desc string, onSurface, activated, blocked bool) string {
	switch {
	case blocked:
		return fmt.Sprintf("%d. **%s** (blocked by host policy — do not rediscover) - %s\n", index, name, desc)
	case onSurface:
		return fmt.Sprintf("%d. **%s** (already in this turn's tools — call it now) - %s\n", index, name, desc)
	case activated:
		return fmt.Sprintf("%d. **%s** (granted for the next request) - %s\n", index, name, desc)
	default:
		return fmt.Sprintf("%d. **%s** (matched) - %s\n", index, name, desc)
	}
}
