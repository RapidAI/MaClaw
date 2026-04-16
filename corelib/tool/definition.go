package tool

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// MCPToolView is a tool exposed by an MCP Server.
type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MCPServerView is the runtime view of an MCP Server.
type MCPServerView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	HealthStatus string `json:"health_status"`
}

// MCPToolSet groups tools from a single MCP server.
type MCPToolSet struct {
	ServerID   string
	ServerName string
	Tools      []MCPToolView
}

// MCPServerProvider abstracts access to remote MCP servers (decouples from MCPRegistry).
type MCPServerProvider interface {
	ListServers() []MCPServerView
	GetServerTools(serverID string) []MCPToolView
	CallTool(serverID, toolName string, args map[string]interface{}) (string, error)
}

// LocalMCPToolProvider abstracts access to local (stdio) MCP servers (decouples from LocalMCPManager).
type LocalMCPToolProvider interface {
	GetAllTools() []MCPToolSet
	CallTool(serverID, toolName string, args map[string]interface{}) (string, error)
}

// DefinitionGenerator dynamically generates the Agent's tool definition
// list by merging builtin tool definitions with tools from healthy MCP Servers
// and running local (stdio) MCP Servers.
// Supports deferred loading: tools in DeferredTools are excluded from the
// initial prompt and can be discovered via SearchDeferred (inspired by
// Claude Code's ToolSearchTool pattern).
type DefinitionGenerator struct {
	mcpProvider      MCPServerProvider
	localMCPProvider LocalMCPToolProvider
	builtinDefs      []map[string]interface{}
	deferredTools    map[string]bool // tool names to defer (not included in Generate output)
}

// NewDefinitionGenerator creates a new generator.
// builtinDefs are the static tool definitions (e.g. from buildToolDefinitions).
func NewDefinitionGenerator(mcpProvider MCPServerProvider, builtinDefs []map[string]interface{}) *DefinitionGenerator {
	return &DefinitionGenerator{
		mcpProvider:   mcpProvider,
		builtinDefs:   builtinDefs,
		deferredTools: make(map[string]bool),
	}
}

// SetDeferredTools marks tool names that should be excluded from Generate()
// output. These tools are still available via SearchDeferred().
func (g *DefinitionGenerator) SetDeferredTools(names []string) {
	g.deferredTools = make(map[string]bool, len(names))
	for _, n := range names {
		g.deferredTools[n] = true
	}
}

// IsDeferredTool returns true if the tool name is in the deferred set.
func (g *DefinitionGenerator) IsDeferredTool(name string) bool {
	return g.deferredTools[name]
}

// SetLocalMCPProvider sets the local MCP provider for stdio-based tool discovery.
func (g *DefinitionGenerator) SetLocalMCPProvider(provider LocalMCPToolProvider) {
	g.localMCPProvider = provider
}

// MCPProvider returns the remote MCP server provider (may be nil).
func (g *DefinitionGenerator) MCPProvider() MCPServerProvider { return g.mcpProvider }

// LocalMCPProvider returns the local MCP tool provider (may be nil).
func (g *DefinitionGenerator) LocalMCPProvider() LocalMCPToolProvider { return g.localMCPProvider }

// Generate produces the complete tool definition list: builtin + dynamic MCP tools.
// Dynamic tool names that conflict with builtin names get a server_id prefix.
// Only tools from healthy remote MCP Servers and running local MCP Servers are included.
// Tools in the deferred set are excluded (use SearchDeferred to find them).
func (g *DefinitionGenerator) Generate() []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		name := ExtractToolName(def)
		if name != "" && g.deferredTools[name] {
			continue
		}
		result = append(result, def)
	}

	builtinNames := make(map[string]bool, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		if name := ExtractToolName(def); name != "" {
			builtinNames[name] = true
		}
	}

	dynamicNames := make(map[string]string)
	type pendingTool struct {
		serverID string
		tool     MCPToolView
	}
	var pending []pendingTool

	if g.mcpProvider != nil {
		servers := g.mcpProvider.ListServers()
		for _, srv := range servers {
			if srv.HealthStatus != "healthy" {
				continue
			}
			tools := g.mcpProvider.GetServerTools(srv.ID)
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

	if g.localMCPProvider != nil {
		for _, ts := range g.localMCPProvider.GetAllTools() {
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

	for _, p := range pending {
		name := p.tool.Name
		if g.deferredTools[name] {
			continue
		}
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
		def := MCPToolToDefinition(finalName, p.tool)
		result = append(result, def)
	}

	return result
}

// ExtractToolName extracts the tool name from an OpenAI function calling definition.
func ExtractToolName(def map[string]interface{}) string {
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

// MCPToolToDefinition converts an MCPToolView into an OpenAI function calling
// tool definition (map format matching toolDef output).
func MCPToolToDefinition(name string, tool MCPToolView) map[string]interface{} {
	params := BuildParametersFromSchema(tool.InputSchema)
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": tool.Description,
			"parameters":  params,
		},
	}
}

// BuildParametersFromSchema converts an MCP tool's InputSchema into the
// OpenAI function calling parameters format.
func BuildParametersFromSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil || len(schema) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

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

	if LooksLikePropertiesMap(schema) {
		return map[string]interface{}{
			"type":       "object",
			"properties": schema,
		}
	}

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

// LooksLikePropertiesMap heuristically checks if a map looks like a JSON Schema
// properties map (each value is a map with a "type" key).
func LooksLikePropertiesMap(m map[string]interface{}) bool {
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

// SearchDeferred returns deferred tool definitions matching the query.
// Searches tool names and descriptions using substring matching.
// Returns up to maxResults definitions. If query is empty, returns all deferred tools.
func (g *DefinitionGenerator) SearchDeferred(query string, maxResults int) []map[string]interface{} {
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
		name := strings.ToLower(ExtractToolName(def))
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

// GenerateDeferred returns only the deferred tool definitions (the complement
// of Generate). Used by SearchDeferred and for tool discovery prompts.
func (g *DefinitionGenerator) GenerateDeferred() []map[string]interface{} {
	var result []map[string]interface{}
	for _, def := range g.builtinDefs {
		name := ExtractToolName(def)
		if name != "" && g.deferredTools[name] {
			result = append(result, def)
		}
	}

	// Also include deferred MCP tools.
	if g.mcpProvider != nil {
		for _, srv := range g.mcpProvider.ListServers() {
			if srv.HealthStatus != "healthy" {
				continue
			}
			for _, t := range g.mcpProvider.GetServerTools(srv.ID) {
				if g.deferredTools[t.Name] {
					result = append(result, MCPToolToDefinition(t.Name, t))
				}
			}
		}
	}
	if g.localMCPProvider != nil {
		for _, ts := range g.localMCPProvider.GetAllTools() {
			for _, t := range ts.Tools {
				if g.deferredTools[t.Name] {
					result = append(result, MCPToolToDefinition(t.Name, t))
				}
			}
		}
	}
	return result
}
