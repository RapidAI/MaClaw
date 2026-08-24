package tool

import (
	"encoding/json"
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

// DefinitionGenerator renders the static legacy host catalog. Dynamic MCP
// inventory may still be retained by its providers for lifecycle/semantic
// routing, but it is never converted into an unbound legacy model function.
// Supports deferred loading for static entries only.
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
		builtinDefs:   filterAgentVisibleToolDefinitions(builtinDefs),
		deferredTools: make(map[string]bool),
	}
}

func filterAgentVisibleToolDefinitions(defs []map[string]interface{}) []map[string]interface{} {
	filtered := FilterDisabledExternalCodingSessionToolDefs(defs)
	out := make([]map[string]interface{}, 0, len(filtered))
	for _, def := range filtered {
		if isInternalBrowserDispatchToolName(ExtractToolName(def)) {
			continue
		}
		out = append(out, def)
	}
	return out
}

// SetDeferredTools marks tool names that should be excluded from Generate()
// output. These tools are still available via SearchDeferred().
func (g *DefinitionGenerator) SetDeferredTools(names []string) {
	g.deferredTools = make(map[string]bool, len(names))
	for _, n := range names {
		if IsDisabledExternalCodingSessionTool(n) || isInternalBrowserDispatchToolName(n) {
			continue
		}
		g.deferredTools[n] = true
	}
}

// IsDeferredTool returns true if the tool name is in the deferred set.
func (g *DefinitionGenerator) IsDeferredTool(name string) bool {
	if IsDisabledExternalCodingSessionTool(name) || isInternalBrowserDispatchToolName(name) {
		return false
	}
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

// Generate produces static legacy host definitions. Dynamic MCP calls require
// a managed surface that binds provider identity, observed schema and contract
// outside model arguments.
func (g *DefinitionGenerator) Generate() []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(g.builtinDefs))
	for _, def := range g.builtinDefs {
		name := ExtractToolName(def)
		if IsDisabledExternalCodingSessionTool(name) || isInternalBrowserDispatchToolName(name) {
			continue
		}
		if name != "" && g.deferredTools[name] {
			continue
		}
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

// GenerateDeferred returns only deferred static tool definitions. Dynamic MCP
// inventory is intentionally absent because a legacy discovery result cannot
// create a provider binding or model authorization.
func (g *DefinitionGenerator) GenerateDeferred() []map[string]interface{} {
	var result []map[string]interface{}
	for _, def := range g.builtinDefs {
		name := ExtractToolName(def)
		if IsDisabledExternalCodingSessionTool(name) || isInternalBrowserDispatchToolName(name) {
			continue
		}
		if name != "" && g.deferredTools[name] {
			result = append(result, def)
		}
	}

	return result
}
