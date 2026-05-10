package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ToolDefinitionGenerator dynamically generates the Agent's tool definition
// list by merging builtin tool definitions with tools from healthy MCP Servers
// and running local (stdio) MCP Servers.
type ToolDefinitionGenerator struct {
	registry        *MCPRegistry
	localMCPManager *LocalMCPManager
	builtinDefs     []map[string]interface{} // the 12 builtin tool definitions
	deferredTools   map[string]bool          // tool names excluded from Generate(), discoverable via SearchDeferred()
}

// NewToolDefinitionGenerator creates a new generator.
// builtinDefs are the static tool definitions (e.g. from buildToolDefinitions).
func NewToolDefinitionGenerator(registry *MCPRegistry, builtinDefs []map[string]interface{}) *ToolDefinitionGenerator {
	return &ToolDefinitionGenerator{
		registry:      registry,
		builtinDefs:   builtinDefs,
		deferredTools: make(map[string]bool),
	}
}

// SetDeferredTools marks tool names that should be excluded from Generate()
// output. These tools are still available via SearchDeferred().
func (g *ToolDefinitionGenerator) SetDeferredTools(names []string) {
	g.deferredTools = make(map[string]bool, len(names))
	for _, n := range names {
		g.deferredTools[n] = true
	}
}

// SearchDeferred returns deferred tool definitions matching the query.
func (g *ToolDefinitionGenerator) SearchDeferred(query string, maxResults int) []map[string]interface{} {
	all := g.GenerateDeferred()
	if query == "" {
		if maxResults > 0 && len(all) > maxResults {
			return all[:maxResults]
		}
		return all
	}
	queryLower := strings.ToLower(query)
	var matches []map[string]interface{}
	for _, def := range all {
		name := strings.ToLower(extractToolName(def))
		desc := ""
		if fn, ok := def["function"].(map[string]interface{}); ok {
			desc, _ = fn["description"].(string)
		}
		descLower := strings.ToLower(desc)
		if strings.Contains(name, queryLower) || strings.Contains(descLower, queryLower) {
			matches = append(matches, def)
			if maxResults > 0 && len(matches) >= maxResults {
				break
			}
		}
	}
	return matches
}

// GenerateDeferred returns only the deferred tool definitions.
func (g *ToolDefinitionGenerator) GenerateDeferred() []map[string]interface{} {
	var result []map[string]interface{}
	for _, def := range g.builtinDefs {
		name := extractToolName(def)
		if name != "" && g.deferredTools[name] {
			result = append(result, def)
		}
	}
	return result
}

// SetLocalMCPManager sets the local MCP manager for stdio-based tool discovery.
func (g *ToolDefinitionGenerator) SetLocalMCPManager(mgr *LocalMCPManager) {
	g.localMCPManager = mgr
}

// Generate produces the complete tool definition list: builtin + dynamic MCP tools.
// Dynamic tool names that conflict with builtin names get a server_id prefix.
// Only tools from healthy remote MCP Servers and running local MCP Servers are included.
func (g *ToolDefinitionGenerator) Generate() []map[string]interface{} {
	// Start with a copy of builtin definitions, excluding deferred tools.
	result := make([]map[string]interface{}, 0, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		name := extractToolName(def)
		if name != "" && g.deferredTools[name] {
			continue
		}
		result = append(result, def)
	}

	// Build a set of builtin tool names for conflict detection.
	builtinNames := make(map[string]bool, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		if name := extractToolName(def); name != "" {
			builtinNames[name] = true
		}
	}

	// Track dynamic tool names across all servers for conflict detection.
	// Maps tool name → server ID of the first server that registered it.
	dynamicNames := make(map[string]string)
	type pendingTool struct {
		serverID string
		tool     MCPToolView
	}
	var pending []pendingTool

	// Collect tools from healthy remote MCP Servers.
	if g.registry != nil {
		servers := g.registry.ListServers()
		for _, srv := range servers {
			if normalizeMCPHealthStatus(srv.HealthStatus) != mcpHealthStatusHealthy {
				continue
			}
			tools := g.registry.GetServerTools(srv.ID)
			for _, t := range tools {
				pending = append(pending, pendingTool{serverID: srv.ID, tool: t})
				if _, exists := dynamicNames[t.Name]; !exists {
					dynamicNames[t.Name] = srv.ID
				} else {
					dynamicNames[t.Name] = ""
				}
			}
		}
	}

	// Collect tools from running local (stdio) MCP Servers.
	if g.localMCPManager != nil {
		for _, ts := range g.localMCPManager.GetAllTools() {
			for _, t := range ts.Tools {
				pending = append(pending, pendingTool{serverID: ts.ServerID, tool: t})
				if _, exists := dynamicNames[t.Name]; !exists {
					dynamicNames[t.Name] = ts.ServerID
				} else {
					dynamicNames[t.Name] = ""
				}
			}
		}
	}

	// Generate definitions for each dynamic tool.
	for _, p := range pending {
		name := p.tool.Name
		needsPrefix := builtinNames[name]
		if !needsPrefix {
			if ownerID := dynamicNames[name]; ownerID == "" {
				needsPrefix = true
			}
		}
		finalName := name
		if needsPrefix {
			finalName = fmt.Sprintf("%s_%s", p.serverID, name)
		}
		def := mcpToolToDefinition(finalName, p.tool)
		result = append(result, def)
	}

	return result
}

// extractToolName extracts the tool name from an OpenAI function calling definition.
func extractToolName(def map[string]interface{}) string {
	fn, ok := def["function"]
	if !ok {
		return ""
	}
	fnMap, ok := fn.(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := fnMap["name"].(string)
	return name
}

// mcpToolToDefinition converts an MCPToolView into an OpenAI function calling
// tool definition (map format matching toolDef output).
func mcpToolToDefinition(name string, tool MCPToolView) map[string]interface{} {
	params := buildParametersFromSchema(tool.InputSchema)
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": tool.Description,
			"parameters":  params,
		},
	}
}

// buildParametersFromSchema converts an MCP tool's InputSchema into the
// OpenAI function calling parameters format.
// If the schema is already a valid JSON Schema object, it is used directly.
// Otherwise a minimal {"type":"object","properties":{}} is returned.
func buildParametersFromSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil || len(schema) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// If the schema already has "type":"object", use it as-is but ensure
	// "properties" key exists.
	if t, ok := schema["type"]; ok {
		if ts, ok := t.(string); ok && ts == "object" {
			result := make(map[string]interface{}, len(schema))
			for k, v := range schema {
				result[k] = v
			}
			if _, hasProp := result["properties"]; !hasProp {
				result["properties"] = map[string]interface{}{}
			}
			return result
		}
	}

	// The schema might be a raw properties map or something else.
	// Wrap it in a standard object schema.
	// Try to detect if it looks like a properties map (keys are property names
	// with object values describing types).
	if looksLikePropertiesMap(schema) {
		// Separate property definitions (maps with "type" key) from
		// non-property top-level keys (e.g., "required" array,
		// "additionalProperties" bool, "description" string).
		properties := make(map[string]interface{}, len(schema))
		result := map[string]interface{}{
			"type": "object",
		}
		for k, v := range schema {
			if vm, isObj := v.(map[string]interface{}); isObj {
				if _, hasType := vm["type"]; hasType {
					properties[k] = v
					continue
				}
			}
			// Non-property entry — promote to result map directly.
			result[k] = v
		}
		result["properties"] = properties
		return result
	}

	// Fallback: marshal and re-parse to ensure clean copy, then use as-is.
	data, err := json.Marshal(schema)
	if err != nil {
		log.Printf("[ToolDefGen] failed to marshal schema: %v", err)
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return result
}

// looksLikePropertiesMap heuristically checks if a map looks like a JSON Schema
// properties map (at least one value is a map with a "type" key).
// It tolerates non-object entries (e.g., "required" array, "additionalProperties"
// bool) that may be mixed in alongside property definitions.
func looksLikePropertiesMap(m map[string]interface{}) bool {
	if len(m) == 0 {
		return false
	}
	propertyCount := 0
	for _, v := range m {
		vm, ok := v.(map[string]interface{})
		if !ok {
			// Non-object value — could be "required" array, "additionalProperties" bool, etc.
			continue
		}
		if _, hasType := vm["type"]; hasType {
			propertyCount++
		}
	}
	return propertyCount > 0
}
