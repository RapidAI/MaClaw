package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MCPToolProvider is the interface required by the agent executor to discover
// and invoke MCP tools at runtime. It is satisfied by *MCPToolBridge which
// wraps the Service's MCP runtime.
//
// This interface decouples the executor from the Service struct, allowing
// the executor to be tested independently and avoiding circular dependencies.
type MCPToolProvider interface {
	// ListAvailableTools returns all MCP tools currently available for the
	// given principal. Only tools from healthy remote servers and running
	// local servers are included. This is called on every Execute() to pick
	// up newly installed MCP servers without restart.
	ListAvailableTools(ctx context.Context, p Principal) []MCPToolEntry

	// CallTool invokes a specific MCP tool and returns the result as a string.
	CallTool(ctx context.Context, p Principal, serverID, toolName string, arguments map[string]interface{}) (string, error)
}

// MCPToolEntry represents a single MCP tool available for the agent.
type MCPToolEntry struct {
	ServerID    string
	ServerName  string
	ToolName    string
	Description string
	InputSchema map[string]interface{}
}

// MCPToolBridge implements MCPToolProvider by delegating to the Service's
// existing MCP runtime infrastructure. It reads the user's config and runtime
// state on every call, ensuring newly installed MCP servers are immediately
// visible to the agent.
//
// The bridge owns an MCPReadinessManager that guarantees MCP servers are in a
// ready state before tools are listed. This eliminates the lifecycle gap where
// servers are configured but not started/probed.
type MCPToolBridge struct {
	svc       *Service
	client    *http.Client
	readiness *MCPReadinessManager
}

// NewMCPToolBridge creates a bridge that connects the CoreAgentExecutor to
// the Service's MCP runtime.
func NewMCPToolBridge(svc *Service) *MCPToolBridge {
	return &MCPToolBridge{
		svc:       svc,
		client:    &http.Client{Timeout: 30 * time.Second},
		readiness: NewMCPReadinessManager(svc),
	}
}

// ListAvailableTools reads the user's MCP config and runtime state, returning
// tools from all healthy/running servers.
//
// Before reading state, it calls EnsureReady to guarantee that:
// - Local servers with AutoStart=true are running (started or restarted)
// - Remote servers have an async health probe in flight (non-blocking)
//
// This eliminates the lifecycle gap where servers are configured but the
// runtime has no state (e.g. after process restart).
func (b *MCPToolBridge) ListAvailableTools(ctx context.Context, p Principal) []MCPToolEntry {
	// Reconcile runtime state. Returns the config for reuse (avoids double-load).
	appCfg, ok := b.readiness.EnsureReady(ctx, p)
	if !ok {
		return nil
	}

	runtime := runtimeForService(b.svc).user(composite(p.TenantID, p.UserID))
	var entries []MCPToolEntry

	// Remote MCP servers: only include healthy ones with cached tools.
	// Remote MCP servers: only include healthy ones with cached tools.
	for _, srv := range appCfg.MCPServers {
		state := runtime.remoteState(srv.ID)
		if state == nil || state.healthStatus != MCPHealthHealthy {
			continue
		}
		for _, t := range state.tools {
			entries = append(entries, MCPToolEntry{
				ServerID:    srv.ID,
				ServerName:  srv.Name,
				ToolName:    t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}

	// Local MCP servers: only include running ones.
	for _, srv := range appCfg.LocalMCPServers {
		if srv.Disabled {
			continue
		}
		client := runtime.localClient(srv.ID)
		if client == nil || !client.IsRunning() {
			continue
		}
		for _, t := range client.GetTools() {
			entries = append(entries, MCPToolEntry{
				ServerID:    srv.ID,
				ServerName:  srv.Name,
				ToolName:    t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}

	return entries
}

// CallTool invokes an MCP tool on the appropriate server (remote or local).
func (b *MCPToolBridge) CallTool(ctx context.Context, p Principal, serverID, toolName string, arguments map[string]interface{}) (string, error) {
	cfg, err := b.svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return "", fmt.Errorf("load user config: %w", err)
	}
	runtime := runtimeForService(b.svc).user(composite(p.TenantID, p.UserID))

	// Try local server first.
	for _, srv := range cfg.AppConfig.LocalMCPServers {
		if srv.ID == serverID && !srv.Disabled {
			client := runtime.localClient(serverID)
			if client == nil || !client.IsRunning() {
				return "", fmt.Errorf("local MCP server %q is not running", serverID)
			}
			return b.callLocalTool(client, toolName, arguments)
		}
	}

	// Try remote server.
	for _, srv := range cfg.AppConfig.MCPServers {
		if srv.ID == serverID {
			return b.callRemoteTool(runtime, srv, toolName, arguments)
		}
	}

	return "", fmt.Errorf("MCP server %q not found or disabled", serverID)
}

func (b *MCPToolBridge) callLocalTool(client *localMCPClient, toolName string, arguments map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": arguments,
	}
	result, err := client.sendRequest("tools/call", params)
	if err != nil {
		return "", fmt.Errorf("MCP tools/call failed: %w", err)
	}
	return parseMCPToolCallResult(result)
}

func (b *MCPToolBridge) callRemoteTool(runtime *userMCPRuntime, entry corelib.MCPServerEntry, toolName string, arguments map[string]interface{}) (string, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	sessionID := runtime.sessionID(entry.ID)
	payload, _, err := doRemoteMCPRoundTrip(b.client, entry, sessionID, reqBody)
	if err != nil {
		return "", fmt.Errorf("MCP tools/call failed: %w", err)
	}
	return parseMCPToolCallResult(payload)
}

// parseMCPToolCallResult extracts text content from a tools/call response.
func parseMCPToolCallResult(raw json.RawMessage) (string, error) {
	// MCP tools/call result format:
	// {"content": [{"type": "text", "text": "..."}, ...], "isError": false}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		// Fallback: return raw JSON as string.
		return string(raw), nil
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) == 0 {
		// No text content — return raw JSON.
		return string(raw), nil
	}
	combined := strings.Join(texts, "\n")
	if result.IsError {
		return "Error: " + combined, nil
	}
	return combined, nil
}

// SetMCPToolProvider wires the MCP tool provider into the executor.
// Must be called after Service initialization to enable MCP tools in the agent loop.
func (e *CoreAgentExecutor) SetMCPToolProvider(provider MCPToolProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mcpProvider = provider
}

// --- Integration into coreAgentCallbacks ---

// mcpResolvedName computes the final tool name for an MCP tool entry,
// applying server-ID prefix when there's a conflict with core tools or
// cross-server name collision. This is the single source of truth for
// MCP tool name resolution — used by both mcpToolDefs and executeMCPTool.
func mcpResolvedName(entry MCPToolEntry, coreNames map[string]bool, nameOwner map[string]string) string {
	finalName := entry.ToolName
	needsPrefix := coreNames[entry.ToolName]
	if !needsPrefix && nameOwner[entry.ToolName] == "" {
		needsPrefix = true // cross-server conflict
	}
	if needsPrefix {
		finalName = entry.ServerID + "_" + entry.ToolName
	}
	return finalName
}

// buildMCPNameResolution builds the lookup tables needed for MCP tool name
// resolution. Returns (coreNames, nameOwner).
func (c *coreAgentCallbacks) buildMCPNameResolution(entries []MCPToolEntry) (map[string]bool, map[string]string) {
	coreNames := make(map[string]bool)
	for _, spec := range c.coreToolSpecs() {
		if spec.Enabled {
			coreNames[spec.Name] = true
		}
	}
	nameOwner := make(map[string]string, len(entries))
	for _, e := range entries {
		if owner, exists := nameOwner[e.ToolName]; !exists {
			nameOwner[e.ToolName] = e.ServerID
		} else if owner != e.ServerID {
			nameOwner[e.ToolName] = "" // conflict
		}
	}
	return coreNames, nameOwner
}

// mcpToolDefs returns tool definitions for all available MCP tools.
// Called by BuildTools on every agent loop iteration to pick up newly
// installed servers.
func (c *coreAgentCallbacks) mcpToolDefs() []map[string]interface{} {
	if c.mcpProvider == nil {
		return nil
	}
	entries := c.mcpProvider.ListAvailableTools(c.ctx, c.principal)
	if len(entries) == 0 {
		return nil
	}

	coreNames, nameOwner := c.buildMCPNameResolution(entries)

	var defs []map[string]interface{}
	for _, e := range entries {
		finalName := mcpResolvedName(e, coreNames, nameOwner)

		params := e.InputSchema
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}

		desc := e.Description
		if desc == "" {
			desc = fmt.Sprintf("MCP tool %s from server %s", e.ToolName, e.ServerName)
		}

		defs = append(defs, functionToolDefinition(finalName, desc, params))
	}
	return defs
}

// executeMCPTool attempts to dispatch a tool call to an MCP server.
// Returns (result, true) if the tool was handled, ("", false) if not an MCP tool.
func (c *coreAgentCallbacks) executeMCPTool(name string, args map[string]interface{}) (string, bool) {
	if c.mcpProvider == nil {
		return "", false
	}
	entries := c.mcpProvider.ListAvailableTools(c.ctx, c.principal)
	if len(entries) == 0 {
		return "", false
	}

	coreNames, nameOwner := c.buildMCPNameResolution(entries)

	// Find the entry matching the resolved name.
	for _, e := range entries {
		if mcpResolvedName(e, coreNames, nameOwner) == name {
			ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
			defer cancel()
			result, err := c.mcpProvider.CallTool(ctx, c.principal, e.ServerID, e.ToolName, args)
			if err != nil {
				log.Printf("[MCP] tool %s/%s call failed: %v", e.ServerID, e.ToolName, err)
				return fmt.Sprintf("Error: MCP tool call failed: %v", err), true
			}
			return result, true
		}
	}
	return "", false
}
