package main

import (
	"encoding/json"
	"fmt"
	"log"
)

// ToolDefinitionGenerator dynamically generates the Agent's tool definition
// list by merging builtin tool definitions with tools from healthy MCP Servers.
type ToolDefinitionGenerator struct {
	registry    *MCPRegistry
	builtinDefs []map[string]interface{} // the 12 builtin tool definitions
}

// NewToolDefinitionGenerator creates a new generator.
// builtinDefs are the static tool definitions (e.g. from buildToolDefinitions).
func NewToolDefinitionGenerator(registry *MCPRegistry, builtinDefs []map[string]interface{}) *ToolDefinitionGenerator {
	return &ToolDefinitionGenerator{
		registry:    registry,
		builtinDefs: builtinDefs,
	}
}

// Generate produces the complete tool definition list: builtin + dynamic MCP tools.
// Dynamic tool names that conflict with builtin names get a server_id prefix.
// Only tools from healthy MCP Servers are included.
func (g *ToolDefinitionGenerator) Generate() []map[string]interface{} {
	// Start with a copy of builtin definitions.
	result := make([]map[string]interface{}, len(g.builtinDefs))
	copy(result, g.builtinDefs)

	// Build a set of builtin tool names for conflict detection.
	builtinNames := make(map[string]bool, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		if name := extractToolName(def); name != "" {
			builtinNames[name] = true
		}
	}

	if g.registry == nil {
		return result
	}

	// Collect tools from all healthy MCP Servers.
	servers := g.registry.ListServers()

	// Track dynamic tool names across servers for inter-server conflict detection.
	// Maps tool name → server ID of the first server that registered it.
	dynamicNames := make(map[string]string)
	// Deferred definitions that need conflict resolution between dynamic tools.
	type pendingTool struct {
		serverID string
		tool     MCPToolView
	}
	var pending []pendingTool

	for _, srv := range servers {
		if srv.HealthStatus != "healthy" {
			continue
		}

		tools := g.registry.GetServerTools(srv.ID)
		for _, t := range tools {
			pending = append(pending, pendingTool{serverID: srv.ID, tool: t})
			if _, exists := dynamicNames[t.Name]; !exists {
				dynamicNames[t.Name] = srv.ID
			} else {
				// Mark as conflicting by setting to empty string.
				dynamicNames[t.Name] = ""
			}
		}
	}

	// Generate definitions for each dynamic tool.
	for _, p := range pending {
		name := p.tool.Name

		// Check conflict with builtin tools — always prefix.
		needsPrefix := builtinNames[name]

		// Check conflict between dynamic tools from different servers.
		if !needsPrefix {
			if ownerID := dynamicNames[name]; ownerID == "" {
				// Multiple servers have the same tool name — prefix all.
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
		return map[string]interface{}{
			"type":       "object",
			"properties": schema,
		}
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
// properties map (each value is a map with a "type" key).
func looksLikePropertiesMap(m map[string]interface{}) bool {
	if len(m) == 0 {
		return false
	}
	for _, v := range m {
		vm, ok := v.(map[string]interface{})
		if !ok {
			return false
		}
		if _, hasType := vm["type"]; !hasType {
			return false
		}
	}
	return true
}
