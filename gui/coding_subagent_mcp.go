package main

// coding_subagent_mcp.go implements task-aware MCP tool selection for the
// CodingSubAgent. When executing a coding task, the SubAgent can optionally
// call MCP tools (e.g. playwright for UI testing) via call_mcp_tool.
//
// Selection mechanism mirrors coding_subagent_skills.go:
//   1. Scan all running local MCP servers and their tools
//   2. Score each tool's (name + description) against the task description
//      using BM25 + bigram Jaccard + embedding cosine (three-signal fusion)
//   3. Take top-K (K = codingSubAgentMaxMCPTools, default 5) with score >= threshold
//   4. Inject matched tools into system prompt + add call_mcp_tool definition

import (
	"fmt"
	"log"
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
	ServerID     string
	ServerName   string
	ToolName     string
	Description  string
	Score        float64
	RequiredArgs []string // extracted from InputSchema.required
}

// selectRelevantMCPToolsForTask returns up to codingSubAgentMaxMCPTools MCP
// tools whose name/description are relevant to the given task description.
// Returns nil if no tools match or the MCP manager is unavailable.
func (c *codingSubAgentCallbacks) selectRelevantMCPToolsForTask(taskDescription string) []codingSubAgentMCPToolMatch {
	if c.subagent == nil || c.subagent.handler == nil {
		return nil
	}
	mgr := c.subagent.handler.getLocalMCPManager()
	if mgr == nil {
		return nil
	}

	allToolSets := mgr.GetAllTools()
	if len(allToolSets) == 0 {
		return nil
	}

	if len(strings.TrimSpace(taskDescription)) == 0 {
		return nil
	}

	// Flatten all MCP tools into candidates.
	type candidate struct {
		serverID     string
		serverName   string
		toolName     string
		description  string
		doc          string   // scoring input: serverName + toolName + description
		requiredArgs []string // from InputSchema.required
	}
	var candidates []candidate
	for _, ts := range allToolSets {
		for _, t := range ts.Tools {
			doc := ts.ServerName + " " + t.Name + " " + t.Description
			candidates = append(candidates, candidate{
				serverID:     ts.ServerID,
				serverName:   ts.ServerName,
				toolName:     t.Name,
				description:  t.Description,
				doc:          doc,
				requiredArgs: extractMCPToolRequiredArgs(t.InputSchema),
			})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Build document strings for scoring.
	docs := make([]string, len(candidates))
	for i, cand := range candidates {
		docs[i] = cand.doc
	}

	// Three-signal scoring via shared infrastructure.
	emb := getSubAgentEmbedder(c.subagent.handler)
	scored := scoreAndSelectTopK(taskDescription, docs, emb, codingSubAgentMaxMCPTools, codingSubAgentMCPScoreThreshold)

	if len(scored) == 0 {
		return nil
	}

	results := make([]codingSubAgentMCPToolMatch, len(scored))
	for i, s := range scored {
		cand := candidates[s.Idx]
		desc := cand.description
		if len([]rune(desc)) > 80 {
			desc = string([]rune(desc)[:80]) + "..."
		}
		results[i] = codingSubAgentMCPToolMatch{
			ServerID:     cand.serverID,
			ServerName:   cand.serverName,
			ToolName:     cand.toolName,
			Description:  desc,
			Score:        s.Score,
			RequiredArgs: cand.requiredArgs,
		}
	}

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = fmt.Sprintf("%s/%s(%.2f)", r.ServerName, r.ToolName, r.Score)
	}
	log.Printf("[coding-subagent] MCP tool selection: task=%q candidates=%d matched=%s",
		truncateLogText(taskDescription, 60), len(candidates), strings.Join(names, ", "))

	return results
}

// buildCodingSubAgentMCPSection builds the system prompt section listing
// available MCP tools. Returns empty string if no tools matched.
func buildCodingSubAgentMCPSection(tools []codingSubAgentMCPToolMatch) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 可用 MCP 工具\n")
	b.WriteString("以下 MCP 工具可通过 call_mcp_tool(server_id=\"...\", tool_name=\"...\", arguments={...}) 调用：\n")
	for _, t := range tools {
		if len(t.RequiredArgs) > 0 {
			b.WriteString(fmt.Sprintf("- **%s** (server: %s): %s（必需参数: %s）\n", t.ToolName, t.ServerName, t.Description, strings.Join(t.RequiredArgs, ", ")))
		} else {
			b.WriteString(fmt.Sprintf("- **%s** (server: %s): %s\n", t.ToolName, t.ServerName, t.Description))
		}
	}
	b.WriteString("\n调用规则：\n")
	b.WriteString("- 每次调用必须指定 server_id 和 tool_name\n")
	b.WriteString("- arguments 必须是完整的 JSON 对象，符合工具的参数要求\n")
	b.WriteString("- MCP 工具执行失败时不要反复重试，改用 bash 手动完成\n")
	return b.String()
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
		return codingToolExecutionResult{
			Text:    "call_mcp_tool is not available for this task (no relevant MCP tools found)",
			Outcome: codingToolOutcomeBlocked,
		}
	}

	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)

	if serverID == "" || toolName == "" {
		return codingToolExecutionResult{
			Text:    "call_mcp_tool requires both server_id and tool_name parameters",
			Outcome: codingToolOutcomeFailed,
		}
	}

	// Validate tool is in the matched set.
	if !c.isMatchedMCPTool(serverID, toolName) {
		allowed := make([]string, len(c.matchedMCPTools))
		for i, t := range c.matchedMCPTools {
			allowed[i] = t.ServerName + "/" + t.ToolName
		}
		log.Printf("[coding-subagent] call_mcp_tool blocked: %s/%s not in matched set %v", serverID, toolName, allowed)
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("MCP tool %s/%s is not available for this task (available: %s)", serverID, toolName, strings.Join(allowed, ", ")),
			Outcome: codingToolOutcomeBlocked,
		}
	}

	// Delegate to host handler's call_mcp_tool execution.
	h := c.subagent.handler
	if h == nil {
		return codingToolExecutionResult{
			Text:    "call_mcp_tool: host handler unavailable",
			Outcome: codingToolOutcomeFailed,
		}
	}

	log.Printf("[coding-subagent] call_mcp_tool: server=%s tool=%s", serverID, toolName)
	result := h.toolCallMCPTool(args)

	outcome := codingToolOutcomeSuccess
	if result == "" {
		outcome = codingToolOutcomeFailed
	} else if strings.HasPrefix(result, "❌") ||
		strings.HasPrefix(result, "MCP tool error") ||
		strings.HasPrefix(result, "local MCP server") {
		outcome = codingToolOutcomeFailed
	}
	return codingToolExecutionResult{Text: result, Outcome: outcome}
}

// isMatchedMCPTool checks if a server/tool combination was selected for this task.
func (c *codingSubAgentCallbacks) isMatchedMCPTool(serverRef, toolName string) bool {
	lowerServer := strings.ToLower(serverRef)
	lowerTool := strings.ToLower(toolName)
	for _, t := range c.matchedMCPTools {
		if (strings.ToLower(t.ServerID) == lowerServer || strings.ToLower(t.ServerName) == lowerServer) &&
			strings.ToLower(t.ToolName) == lowerTool {
			return true
		}
	}
	return false
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
			return strArr
		}
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
