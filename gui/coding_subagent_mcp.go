package main

// coding_subagent_mcp.go implements task-aware MCP tool selection for the
// CodingSubAgent. When executing a coding task, the SubAgent can optionally
// call MCP tools (e.g. playwright for UI testing) via call_mcp_tool.
//
// Selection mechanism mirrors coding_subagent_skills.go:
//   1. Scan connected local and reachable remote MCP servers and their tools
//   2. Score each tool's (name + description) against the task description
//      using BM25 + bigram Jaccard + embedding cosine (three-signal fusion)
//   3. Take top-K (K = codingSubAgentMaxMCPTools, default 5) with score >= threshold
//   4. Inject matched tools into system prompt + add call_mcp_tool definition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

const (
	// codingSubAgentMaxMCPTools is the maximum number of MCP tools injected
	// into the SubAgent's context per task.
	codingSubAgentMaxMCPTools = 5

	// codingSubAgentMCPScoreThreshold is the minimum relevance score for an
	// MCP tool to be considered relevant to the current task.
	codingSubAgentMCPScoreThreshold = 0.15
)

// codingSubAgentMCPToolMatch is an MCP tool that matched the current task.
type codingSubAgentMCPToolMatch struct {
	ServerID      string
	ServerName    string
	ToolName      string
	Description   string
	Score         float64
	RequiredArgs  []string // extracted from InputSchema.required
	ArgumentHints []string // compact required parameter descriptions from InputSchema.properties
}

// selectRelevantMCPToolsForTask returns all MCP tools in a full coding
// environment and the relevant top-K tools in a lean subagent environment.
// Returns nil if no tools match or the MCP manager is unavailable.
func (c *codingSubAgentCallbacks) selectRelevantMCPToolsForTask(taskDescription string) []codingSubAgentMCPToolMatch {
	if c == nil || c.subagent == nil || c.subagent.handler == nil {
		return nil
	}
	taskForScore := strings.TrimSpace(taskDescription)
	fullEnv := c.subagent.isFullEnvironment()
	if taskForScore == "" && !fullEnv {
		return nil
	}

	// Keep the discovery source aligned with call_mcp_tool: it supports both
	// running local servers and reachable remote servers.
	candidates := c.connectedMCPToolCandidates()
	if len(candidates) == 0 {
		return nil
	}
	if fullEnv {
		// A coding task may need any connected MCP capability. Do not score or
		// cap this list: a top-K subset makes valid tools undiscoverable and is
		// the direct cause of the assistant reporting a truncated tool list.
		results := buildCodingSubAgentMCPToolMatches(candidates, nil)
		logCodingSubAgentMCPToolSelection(taskDescription, true, len(candidates), results)
		return results
	}

	// Build document strings for scoring.
	docs := make([]string, len(candidates))
	for i, cand := range candidates {
		docs[i] = cand.doc
	}

	// Three-signal scoring via shared infrastructure.
	emb := getSubAgentEmbedder(c.subagent.handler)
	scored := scoreAndSelectTopK(taskForScore, docs, emb, codingSubAgentMaxMCPTools, codingSubAgentMCPScoreThreshold)

	if len(scored) == 0 {
		logCodingSubAgentMCPToolSelection(taskDescription, false, len(candidates), nil)
		return nil
	}
	results := buildCodingSubAgentMCPToolMatches(candidates, scored)
	logCodingSubAgentMCPToolSelection(taskDescription, false, len(candidates), results)
	return results
}

// connectedMCPToolCandidates returns every MCP tool that the host can invoke.
// Local servers must be running; remote servers must have a cached tool list
// and have passed a health check.
func (c *codingSubAgentCallbacks) connectedMCPToolCandidates() []codingSubAgentMCPCandidate {
	if c == nil || c.subagent == nil || c.subagent.handler == nil {
		return nil
	}
	h := c.subagent.handler
	var candidates []codingSubAgentMCPCandidate
	appendTools := func(serverID, serverName string, tools []MCPToolView) {
		serverID = strings.TrimSpace(serverID)
		serverName = strings.TrimSpace(serverName)
		if serverID == "" || serverName == "" {
			return
		}
		for _, t := range tools {
			toolName := strings.TrimSpace(t.Name)
			if toolName == "" || isDisabledExternalCodingSessionTool(toolName) {
				continue
			}
			doc := serverName + " " + toolName + " " + t.Description
			candidates = append(candidates, codingSubAgentMCPCandidate{
				serverID:      serverID,
				serverName:    serverName,
				toolName:      toolName,
				description:   t.Description,
				doc:           doc,
				requiredArgs:  extractMCPToolRequiredArgs(t.InputSchema),
				argumentHints: extractMCPToolRequiredArgumentHints(t.InputSchema),
			})
		}
	}

	if mgr := h.getLocalMCPManager(); mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			appendTools(ts.ServerID, ts.ServerName, ts.Tools)
		}
	}
	if registry := h.getMCPRegistry(); registry != nil {
		for _, server := range registry.ListServers() {
			if !isCodingSubAgentMCPServerReachable(server.HealthStatus) {
				continue
			}
			// ListServers carries the cache populated by HealthCheck. Use it rather
			// than calling GetServerTools here: prompt construction must not make a
			// second network round-trip when a cache is absent or has just expired.
			appendTools(server.ID, server.Name, server.Tools)
		}
	}
	// GetAllTools traverses a map, so sort before rendering the prompt. This
	// keeps a cached CodingSubAgent prompt reproducible across process runs.
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if strings.EqualFold(left.serverID, right.serverID) {
			if strings.EqualFold(left.serverName, right.serverName) {
				return strings.ToLower(left.toolName) < strings.ToLower(right.toolName)
			}
			return strings.ToLower(left.serverName) < strings.ToLower(right.serverName)
		}
		return strings.ToLower(left.serverID) < strings.ToLower(right.serverID)
	})
	return deduplicateCodingSubAgentMCPCandidates(candidates)
}

// Deduplicate malformed or stale tool snapshots by canonical server ID and
// tool name. The same external target must appear at most once in the prompt
// and matched-tool policy, even if a server accidentally reports it twice.
func deduplicateCodingSubAgentMCPCandidates(candidates []codingSubAgentMCPCandidate) []codingSubAgentMCPCandidate {
	seen := make(map[string]struct{}, len(candidates))
	unique := candidates[:0]
	for _, candidate := range candidates {
		key := strings.ToLower(candidate.serverID) + "\x00" + strings.ToLower(candidate.toolName)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

// A slow remote MCP server has completed a health check successfully and is
// still callable. Only unknown and unavailable servers are hidden.
func isCodingSubAgentMCPServerReachable(status mcpHealthStatus) bool {
	switch normalizeMCPHealthStatus(status) {
	case mcpHealthStatusHealthy, mcpHealthStatusSlow:
		return true
	default:
		return false
	}
}

type codingSubAgentMCPCandidate struct {
	serverID      string
	serverName    string
	toolName      string
	description   string
	doc           string
	requiredArgs  []string
	argumentHints []string
}

func buildCodingSubAgentMCPToolMatches(candidates []codingSubAgentMCPCandidate, scored []scoredCandidate) []codingSubAgentMCPToolMatch {
	if len(candidates) == 0 {
		return nil
	}
	if len(scored) == 0 {
		scored = make([]scoredCandidate, len(candidates))
		for i := range candidates {
			scored[i] = scoredCandidate{Idx: i}
		}
	}

	results := make([]codingSubAgentMCPToolMatch, len(scored))
	for i, s := range scored {
		cand := candidates[s.Idx]
		desc := cand.description
		if len([]rune(desc)) > 80 {
			desc = string([]rune(desc)[:80]) + "..."
		}
		results[i] = codingSubAgentMCPToolMatch{
			ServerID:      cand.serverID,
			ServerName:    cand.serverName,
			ToolName:      cand.toolName,
			Description:   desc,
			Score:         s.Score,
			RequiredArgs:  cand.requiredArgs,
			ArgumentHints: cand.argumentHints,
		}
	}

	return results
}

func (c *codingSubAgentCallbacks) buildCodingSubAgentMCPSection() string {
	if c == nil {
		return ""
	}
	c.ensureMatchedMCPToolsSelected()
	return buildCodingSubAgentMCPSection(c.matchedMCPTools)
}

func logCodingSubAgentMCPToolSelection(taskDescription string, fullEnv bool, candidateCount int, results []codingSubAgentMCPToolMatch) {
	const logPreviewLimit = 12
	shown := len(results)
	if shown > logPreviewLimit {
		shown = logPreviewLimit
	}
	names := make([]string, shown)
	for i, r := range results[:shown] {
		names[i] = fmt.Sprintf("%s/%s(%.2f)", r.ServerName, r.ToolName, r.Score)
	}
	matched := strings.Join(names, ", ")
	if len(results) > shown {
		matched += fmt.Sprintf(", ... +%d more", len(results)-shown)
	}
	log.Printf("[coding-subagent] MCP tool selection: task=%q full_env=%v candidates=%d matched=%s",
		truncateLogText(taskDescription, 60), fullEnv, candidateCount, matched)
}

// buildCodingSubAgentMCPSection builds the system prompt section listing
// available MCP tools. Returns empty string if no tools matched.
func buildCodingSubAgentMCPSection(tools []codingSubAgentMCPToolMatch) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 可用 MCP 工具\n")
	b.WriteString("以下 MCP 工具可通过 call_mcp_tool(server_id=\"...\", tool_name=\"...\", arguments={...}) 调用。优先使用 server_id；名称重复时仅 server_id 可消歧：\n")
	for _, t := range tools {
		serverRef := t.ServerName
		if strings.TrimSpace(t.ServerID) != "" {
			serverRef = fmt.Sprintf("%s [%s]", t.ServerName, t.ServerID)
		}
		if len(t.RequiredArgs) > 0 {
			argumentText := compactCodingSubAgentMCPArgumentHints(t.RequiredArgs, t.ArgumentHints)
			b.WriteString(fmt.Sprintf("- **%s** (server: %s): %s（必需参数: %s）\n", t.ToolName, serverRef, t.Description, argumentText))
		} else {
			b.WriteString(fmt.Sprintf("- **%s** (server: %s): %s\n", t.ToolName, serverRef, t.Description))
		}
	}
	b.WriteString("\n调用规则：\n")
	b.WriteString("- 每次调用必须指定 server_id 和 tool_name\n")
	b.WriteString("- arguments 必须是完整的 JSON 对象，符合工具的参数要求\n")
	b.WriteString("- MCP 工具执行失败时不要反复重试，改用 bash 手动完成\n")
	return b.String()
}

func compactCodingSubAgentMCPArgumentHints(requiredArgs, argumentHints []string) string {
	if len(argumentHints) == 0 {
		return compactCodingSubAgentRequiredArgs(requiredArgs)
	}
	const maxHints = codingSubAgentDynamicRequiredArgsMax
	shown := len(argumentHints)
	if shown > maxHints {
		shown = maxHints
	}
	result := append([]string(nil), argumentHints[:shown]...)
	if remaining := len(argumentHints) - shown; remaining > 0 {
		result = append(result, fmt.Sprintf("还有 %d 项未展开", remaining))
	}
	return strings.Join(result, "; ")
}

// buildCallMCPToolDefinition returns the call_mcp_tool definition for SubAgent.
func buildCallMCPToolDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "call_mcp_tool",
			"description": "调用 MCP Server 上的工具（如 playwright 浏览器测试、puppeteer 截图等）。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"server_id": map[string]interface{}{
						"type":        "string",
						"description": "MCP Server ID 或 Name",
					},
					"tool_name": map[string]interface{}{
						"type":        "string",
						"description": "工具名称",
					},
					"arguments": map[string]interface{}{
						"type":        "object",
						"description": "工具参数（JSON 对象，按工具要求传入）",
					},
				},
				"required": []string{"server_id", "tool_name"},
			},
		},
	}
}

// executeCallMCPTool handles call_mcp_tool from the SubAgent with tool name
// validation against matched MCP tools.
func (c *codingSubAgentCallbacks) executeCallMCPTool(args map[string]interface{}) codingToolExecutionResult {
	if len(c.matchedMCPTools) == 0 {
		msg := "call_mcp_tool is not available for this task (no relevant MCP tools found)"
		return codingToolExecutionResult{
			Text:    c.rejectToolCall("call_mcp_tool", args, msg),
			Outcome: codingToolOutcomeBlocked,
		}
	}

	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)

	if serverID == "" {
		return missingCodingSubAgentRequiredArgumentResult("call_mcp_tool", "server_id")
	}
	if toolName == "" {
		return missingCodingSubAgentRequiredArgumentResult("call_mcp_tool", "tool_name")
	}

	// Validate tool is in the matched set.
	matchedTool, matched := c.matchedMCPTool(serverID, toolName)
	if !matched {
		allowed := codingSubAgentMCPToolReferences(c.matchedMCPTools, 12)
		log.Printf("[coding-subagent] call_mcp_tool blocked: %s/%s not in matched set %v", serverID, toolName, allowed)
		msg := fmt.Sprintf("MCP tool %s/%s is not available for this task (available: %s)", serverID, toolName, strings.Join(allowed, ", "))
		return codingToolExecutionResult{
			Text:    c.rejectToolCall("call_mcp_tool", args, msg),
			Outcome: codingToolOutcomeBlocked,
		}
	}
	if result, rejected := rejectMissingCodingSubAgentMCPRequiredArguments(matchedTool, args); rejected {
		return result
	}

	// Delegate to host handler's call_mcp_tool execution.
	h := c.subagent.handler
	if h == nil {
		msg := "call_mcp_tool: host handler unavailable"
		return codingToolExecutionResult{
			Text:    c.rejectToolCall("call_mcp_tool", args, msg),
			Outcome: codingToolOutcomeFailed,
		}
	}

	log.Printf("[coding-subagent] call_mcp_tool: server=%s tool=%s", serverID, toolName)
	result := h.toolCallMCPTool(args)

	outcome := codingToolOutcomeSuccess
	if isCodingSubAgentDynamicToolFailure(result) ||
		strings.HasPrefix(result, "MCP tool error") ||
		strings.HasPrefix(result, "local MCP server") ||
		// Legacy failure rows may start with U+274C (cross mark).
		(len(result) > 0 && []rune(result)[0] == 0x274C) {
		outcome = codingToolOutcomeFailed
	}
	c.trackDynamicToolResult("call_mcp_tool", serverID+"/"+toolName, result, outcome == codingToolOutcomeSuccess)
	return codingToolExecutionResult{Text: result, Outcome: outcome}
}

func codingSubAgentMCPToolReferences(tools []codingSubAgentMCPToolMatch, max int) []string {
	if max <= 0 || len(tools) == 0 {
		return nil
	}
	shown := len(tools)
	if shown > max {
		shown = max
	}
	refs := make([]string, 0, shown+1)
	for _, tool := range tools[:shown] {
		serverID := strings.TrimSpace(tool.ServerID)
		if serverID == "" {
			serverID = strings.TrimSpace(tool.ServerName)
		}
		refs = append(refs, serverID+"/"+tool.ToolName)
	}
	if remaining := len(tools) - shown; remaining > 0 {
		refs = append(refs, fmt.Sprintf("... +%d more", remaining))
	}
	return refs
}

// isMatchedMCPTool checks if a server/tool combination was selected for this task.
func (c *codingSubAgentCallbacks) isMatchedMCPTool(serverRef, toolName string) bool {
	_, ok := c.matchedMCPTool(serverRef, toolName)
	return ok
}

func (c *codingSubAgentCallbacks) matchedMCPTool(serverRef, toolName string) (codingSubAgentMCPToolMatch, bool) {
	lowerServer := strings.ToLower(strings.TrimSpace(serverRef))
	lowerTool := strings.ToLower(strings.TrimSpace(toolName))
	for _, t := range c.matchedMCPTools {
		if (strings.EqualFold(strings.TrimSpace(t.ServerID), lowerServer) || strings.EqualFold(strings.TrimSpace(t.ServerName), lowerServer)) &&
			strings.EqualFold(strings.TrimSpace(t.ToolName), lowerTool) {
			return t, true
		}
	}
	return codingSubAgentMCPToolMatch{}, false
}

func rejectMissingCodingSubAgentMCPRequiredArguments(tool codingSubAgentMCPToolMatch, args map[string]interface{}) (codingToolExecutionResult, bool) {
	if len(tool.RequiredArgs) == 0 {
		return codingToolExecutionResult{}, false
	}
	arguments, _ := args["arguments"].(map[string]interface{})
	if arguments == nil {
		arguments = make(map[string]interface{})
		args["arguments"] = arguments
	}
	for _, field := range tool.RequiredArgs {
		value, ok := arguments[field]
		if !ok {
			if topLevelValue, topLevelOK := args[field]; topLevelOK {
				value = topLevelValue
				ok = true
				arguments[field] = topLevelValue
			}
		}
		if !ok || value == nil {
			return missingCodingSubAgentMCPRequiredArgumentResult(tool, field), true
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			return missingCodingSubAgentMCPRequiredArgumentResult(tool, field), true
		}
	}
	return codingToolExecutionResult{}, false
}

func missingCodingSubAgentMCPRequiredArgumentResult(tool codingSubAgentMCPToolMatch, field string) codingToolExecutionResult {
	target := tool.ServerName + "/" + tool.ToolName
	example := codingSubAgentMCPToolArgumentExample(tool, field)
	return codingToolExecutionResult{
		Text:                          fmt.Sprintf("Error: call_mcp_tool target %q is missing required MCP argument %q in arguments. The MCP tool was not executed. Regenerate call_mcp_tool with arguments.%s set. Example valid arguments: %s.", target, field, field, example),
		Outcome:                       codingToolOutcomeFailed,
		SkipRejectedDynamicToolRecord: true,
	}
}

func codingSubAgentMCPToolArgumentExample(tool codingSubAgentMCPToolMatch, missingField string) string {
	serverID := strings.TrimSpace(tool.ServerID)
	if serverID == "" {
		serverID = strings.TrimSpace(tool.ServerName)
	}
	toolName := strings.TrimSpace(tool.ToolName)
	arguments := make(map[string]interface{}, len(tool.RequiredArgs)+1)
	for _, field := range tool.RequiredArgs {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		arguments[field] = fmt.Sprintf("<%s>", field)
	}
	if strings.TrimSpace(missingField) != "" {
		arguments[strings.TrimSpace(missingField)] = fmt.Sprintf("<%s>", strings.TrimSpace(missingField))
	}
	example := map[string]interface{}{
		"server_id": serverID,
		"tool_name": toolName,
		"arguments": arguments,
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(example); err != nil {
		return `{"server_id":"server","tool_name":"tool","arguments":{}}`
	}
	return strings.TrimSpace(buf.String())
}

// getSubAgentEmbedderSafe is a nil-safe wrapper (avoids import cycle issues
// if embedding package is needed here). Uses the shared helper from
// coding_subagent_skills.go.

// extractMCPToolRequiredArgs extracts the "required" field from an MCP tool's
// InputSchema. Returns nil if the schema or required field is absent.
func extractMCPToolRequiredArgs(schema map[string]interface{}) []string {
	if schema == nil {
		return nil
	}
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		// Try []string directly (some implementations).
		if strArr, ok := raw.([]string); ok {
			return normalizeCodingSubAgentMCPRequiredArgs(strArr)
		}
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return normalizeCodingSubAgentMCPRequiredArgs(result)
}

func normalizeCodingSubAgentMCPRequiredArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(args))
	result := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		key := strings.ToLower(arg)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, arg)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// extractMCPToolRequiredArgumentHints returns compact, schema-derived guidance
// for the required arguments shown in the CodingSubAgent prompt. It preserves
// the required-field order and never exposes optional fields.
func extractMCPToolRequiredArgumentHints(schema map[string]interface{}) []string {
	required := extractMCPToolRequiredArgs(schema)
	if len(required) == 0 {
		return nil
	}
	properties, _ := schema["properties"].(map[string]interface{})
	hints := make([]string, 0, len(required))
	for _, name := range required {
		property, _ := properties[name].(map[string]interface{})
		typeName, _ := property["type"].(string)
		description, _ := property["description"].(string)
		if typeName == "" && description == "" {
			hints = append(hints, name)
			continue
		}
		hint := name
		if typeName != "" {
			hint += " (" + typeName + ")"
		}
		if description = compactCodingSubAgentMCPArgumentDescription(description); description != "" {
			hint += ": " + description
		}
		hints = append(hints, hint)
	}
	return hints
}

func compactCodingSubAgentMCPArgumentDescription(description string) string {
	const maxRunes = 80
	description = strings.Join(strings.Fields(strings.TrimSpace(description)), " ")
	runes := []rune(description)
	if len(runes) <= maxRunes {
		return description
	}
	return string(runes[:maxRunes]) + "..."
}
