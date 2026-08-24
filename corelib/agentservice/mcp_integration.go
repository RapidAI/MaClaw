package agentservice

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
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
	// Contract is control-plane supplied. An inventory item without one is
	// ready for management/diagnostics but quarantined from Agent execution.
	Contract DynamicCapabilityContract
}

// MCPDynamicContractResolver is the only extension point that translates a
// control-plane capability registration into an executable tool contract. It
// receives no model input and must return false for registrations that do not
// describe this concrete server/tool binding.
type MCPDynamicContractResolver interface {
	ResolveMCPDynamicContract(ctx context.Context, p Principal, serverID, toolName string) (DynamicCapabilityContract, bool)
}

// MCPToolBinding is the immutable target selected from one readiness snapshot.
// It deliberately carries the server/tool identity out of the model parameter
// surface: callers invoke it with business arguments only.
type MCPToolBinding struct {
	ServerID       string
	ToolName       string
	SchemaDigest   string
	ContractDigest string
}

func (b MCPToolBinding) StableID() string {
	return strings.Join([]string{strings.TrimSpace(b.ServerID), strings.TrimSpace(b.ToolName), strings.TrimSpace(b.SchemaDigest), strings.TrimSpace(b.ContractDigest)}, ":")
}

// boundMCPCallSurface is the per-callback materialization of one immutable MCP
// inventory. Function names are opaque adapter IDs, never server or tool
// names. It intentionally has no lookup-by-provider API: an LLM call can only
// resolve an adapter that was rendered from this exact inventory snapshot.
type boundMCPCallSurface struct {
	mu       sync.RWMutex
	adapters map[string]boundMCPAdapter
}

type boundMCPAdapter struct {
	Binding    MCPToolBinding
	Parameters map[string]interface{}
}

func newBoundMCPCallSurface() *boundMCPCallSurface {
	return &boundMCPCallSurface{adapters: make(map[string]boundMCPAdapter)}
}

func (s *boundMCPCallSurface) replace(adapters map[string]boundMCPAdapter) {
	if s == nil {
		return
	}
	clone := make(map[string]boundMCPAdapter, len(adapters))
	for name, adapter := range adapters {
		clone[name] = boundMCPAdapter{Binding: adapter.Binding, Parameters: cloneMCPJSONValue(adapter.Parameters).(map[string]interface{})}
	}
	s.mu.Lock()
	s.adapters = clone
	s.mu.Unlock()
}

func (s *boundMCPCallSurface) adapter(adapterName string) (boundMCPAdapter, bool) {
	if s == nil {
		return boundMCPAdapter{}, false
	}
	s.mu.RLock()
	adapter, ok := s.adapters[strings.TrimSpace(adapterName)]
	s.mu.RUnlock()
	return adapter, ok
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
	contracts MCPDynamicContractResolver
}

// DynamicCatalogLifecycle gives semantic routing a conservative completeness
// signal for the MCP family. EnsureReady may have started an async probe, so
// no healthy entries yet is not equivalent to "there is no MCP capability".
// Only a completed readiness pass can state Complete; discovery identity and
// metadata remain private to the bridge and are not exposed to the model.
func (b *MCPToolBridge) DynamicCatalogLifecycle(ctx context.Context, p Principal) DynamicCatalogLifecycle {
	_, lifecycle := b.DynamicMCPInventory(ctx, p)
	return lifecycle
}

// DynamicMCPInventory obtains both the ready/quarantined MCP entries and the
// corresponding lifecycle watermark from one runtime observation. It is the
// semantic-routing path; ListAvailableTools remains a compatibility projection
// for legacy callers that do not consume coverage metadata.
func (b *MCPToolBridge) DynamicMCPInventory(ctx context.Context, p Principal) ([]MCPToolEntry, DynamicCatalogLifecycle) {
	if b == nil || b.svc == nil || b.readiness == nil {
		return nil, IncompleteDynamicCatalogLifecycle("catalog_incomplete")
	}
	appCfg, ok := b.readiness.EnsureReady(ctx, p)
	if !ok {
		return nil, IncompleteDynamicCatalogLifecycle("catalog_incomplete")
	}
	runtime := runtimeForService(b.svc).user(composite(p.TenantID, p.UserID))
	snapshot := runtime.inventorySnapshot()
	contracts, err := b.contractSnapshot(p)
	if err != nil {
		return nil, IncompleteDynamicCatalogLifecycle("contract_registry_unavailable")
	}
	entries := b.availableToolsFromSnapshotWithContracts(ctx, p, appCfg, snapshot, contracts)
	for _, server := range appCfg.MCPServers {
		state, ok := snapshot.remote[server.ID]
		if !ok || state.healthStatus != MCPHealthHealthy {
			return entries, IncompleteDynamicCatalogLifecycle("provider_not_ready")
		}
	}
	for _, server := range appCfg.LocalMCPServers {
		if server.Disabled {
			continue
		}
		state, ok := snapshot.local[server.ID]
		if !ok || !state.running {
			return entries, IncompleteDynamicCatalogLifecycle("provider_not_ready")
		}
	}
	return entries, CompleteDynamicCatalogLifecycle()
}

// SetMCPDynamicContractResolver installs the control-plane contract lookup.
// Without it, discovered MCP tools remain quarantined from Agent execution.
func (b *MCPToolBridge) SetMCPDynamicContractResolver(resolver MCPDynamicContractResolver) {
	if b == nil {
		return
	}
	b.contracts = resolver
}

// NewMCPToolBridge creates a bridge that connects the CoreAgentExecutor to
// the Service's MCP runtime.
func NewMCPToolBridge(svc *Service) *MCPToolBridge {
	bridge := &MCPToolBridge{
		svc:       svc,
		client:    &http.Client{Timeout: 30 * time.Second},
		readiness: NewMCPReadinessManager(svc),
	}
	if svc != nil {
		bridge.contracts = svc.DynamicCapabilityContracts()
	}
	return bridge
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
	return b.availableToolsFromSnapshot(ctx, p, appCfg, runtime.inventorySnapshot())
}

// availableToolsFromSnapshot is intentionally called only with a single
// config/runtime observation. Do not move readiness checks into the loops:
// that would reintroduce a mixed-generation inventory to semantic routing.
func (b *MCPToolBridge) availableToolsFromSnapshot(ctx context.Context, p Principal, appCfg corelib.AppConfig, snapshot mcpRuntimeInventorySnapshot) []MCPToolEntry {
	return b.availableToolsFromSnapshotWithContracts(ctx, p, appCfg, snapshot, b.contracts)
}

func (b *MCPToolBridge) availableToolsFromSnapshotWithContracts(ctx context.Context, p Principal, appCfg corelib.AppConfig, snapshot mcpRuntimeInventorySnapshot, contracts MCPDynamicContractResolver) []MCPToolEntry {
	var entries []MCPToolEntry

	// Remote MCP servers: only include healthy ones with cached tools.
	for _, srv := range appCfg.MCPServers {
		state, ok := snapshot.remote[srv.ID]
		if !ok || state.healthStatus != MCPHealthHealthy {
			continue
		}
		for _, t := range state.tools {
			if coretool.IsDisabledExternalCodingSessionTool(t.Name) {
				continue
			}
			entry := MCPToolEntry{
				ServerID:    srv.ID,
				ServerName:  srv.Name,
				ToolName:    t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
			if contracts != nil {
				entry.Contract, _ = contracts.ResolveMCPDynamicContract(ctx, p, srv.ID, t.Name)
				if !dynamicMCPContractMatchesEntry(entry.Contract, entry) {
					entry.Contract = DynamicCapabilityContract{}
				}
			}
			entries = append(entries, entry)
		}
	}

	// Local MCP servers: only include running ones.
	for _, srv := range appCfg.LocalMCPServers {
		if srv.Disabled {
			continue
		}
		state, ok := snapshot.local[srv.ID]
		if !ok || !state.running {
			continue
		}
		for _, t := range state.tools {
			if coretool.IsDisabledExternalCodingSessionTool(t.Name) {
				continue
			}
			entry := MCPToolEntry{
				ServerID:    srv.ID,
				ServerName:  srv.Name,
				ToolName:    t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
			if contracts != nil {
				entry.Contract, _ = contracts.ResolveMCPDynamicContract(ctx, p, srv.ID, t.Name)
				if !dynamicMCPContractMatchesEntry(entry.Contract, entry) {
					entry.Contract = DynamicCapabilityContract{}
				}
			}
			entries = append(entries, entry)
		}
	}

	return entries
}

func dynamicMCPObservedBindingDigest(serverID, toolName string, schema map[string]interface{}) string {
	return coretool.SchemaDigest([]byte(strings.Join([]string{
		"mcp",
		strings.TrimSpace(serverID),
		strings.TrimSpace(toolName),
		coretool.SchemaDigest(canonicalMCPSchema(schema)),
	}, "\x00")))
}

// DynamicMCPObservedBindingDigest is the control-plane identity for one
// concrete MCP implementation. Hosts that own a reviewed contract registry
// use it when comparing a freshly observed server/tool/schema tuple with a
// previously approved declaration; discovery metadata cannot provide it.
func DynamicMCPObservedBindingDigest(serverID, toolName string, schema map[string]interface{}) string {
	return dynamicMCPObservedBindingDigest(serverID, toolName, schema)
}

func dynamicMCPContractMatchesEntry(contract DynamicCapabilityContract, entry MCPToolEntry) bool {
	want := strings.TrimSpace(contract.ObservedBindingDigest)
	return want != "" && want == dynamicMCPObservedBindingDigest(entry.ServerID, entry.ToolName, entry.InputSchema)
}

func (b *MCPToolBridge) contractSnapshot(p Principal) (MCPDynamicContractResolver, error) {
	if b == nil || b.contracts == nil {
		return nil, nil
	}
	provider, ok := b.contracts.(dynamicCapabilityContractSnapshotProvider)
	if !ok {
		return b.contracts, nil
	}
	snapshot, err := provider.SnapshotDynamicCapabilityContracts(p)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// CallTool invokes an MCP tool on the appropriate server (remote or local).
func (b *MCPToolBridge) CallTool(ctx context.Context, p Principal, serverID, toolName string, arguments map[string]interface{}) (string, error) {
	if coretool.IsDisabledExternalCodingSessionTool(toolName) {
		return "", fmt.Errorf("external coding-session tool %q is disabled", toolName)
	}
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

// BindTool resolves exactly one MCP endpoint against the currently ready,
// principal-scoped inventory. It is the control/routing boundary used before a
// model-facing adapter is created; a subsequent invocation cannot silently
// retarget the call by supplying a different server or tool name.
func BindMCPTool(entries []MCPToolEntry, serverID, toolName string) (MCPToolBinding, error) {
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)
	if serverID == "" || toolName == "" {
		return MCPToolBinding{}, fmt.Errorf("MCP binding requires server and tool identity")
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ServerID) != serverID || strings.TrimSpace(entry.ToolName) != toolName {
			continue
		}
		if err := entry.Contract.validate(); err != nil {
			return MCPToolBinding{}, fmt.Errorf("MCP tool %q/%q is quarantined: %w", serverID, toolName, err)
		}
		return MCPToolBinding{ServerID: serverID, ToolName: toolName, SchemaDigest: coretool.SchemaDigest(canonicalMCPSchema(entry.InputSchema)), ContractDigest: entry.Contract.Digest()}, nil
	}
	return MCPToolBinding{}, fmt.Errorf("MCP tool %q/%q is not ready", serverID, toolName)
}

// CallBoundTool validates the immutable binding against a fresh ready list
// before delegating to the legacy transport carrier. This prevents stale or
// same-name replacement from being treated as an equivalent provider.
func (b *MCPToolBridge) CallBoundTool(ctx context.Context, p Principal, binding MCPToolBinding, arguments map[string]interface{}) (string, error) {
	entries := b.ListAvailableTools(ctx, p)
	fresh, err := BindMCPTool(entries, binding.ServerID, binding.ToolName)
	if err != nil {
		return "", err
	}
	if fresh.StableID() != binding.StableID() {
		return "", fmt.Errorf("mcp_binding_stale")
	}
	return b.CallTool(ctx, p, binding.ServerID, binding.ToolName, arguments)
}

func canonicalMCPSchema(schema map[string]interface{}) []byte {
	if schema == nil {
		schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return []byte("invalid-schema")
	}
	return data
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

// mcpToolDefs materializes one immutable ready inventory into opaque adapter
// names. Provider descriptions never enter the model prompt and execution can
// only resolve an adapter from this rendering, rather than re-discovering a
// provider by a model-controlled name.
func (c *coreAgentCallbacks) mcpToolDefs() []map[string]interface{} {
	if c.mcpProvider == nil {
		return nil
	}
	entries := c.mcpProvider.ListAvailableTools(c.ctx, c.principal)
	if len(entries) == 0 {
		c.boundMCPCalls().replace(nil)
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ServerID != entries[j].ServerID {
			return entries[i].ServerID < entries[j].ServerID
		}
		return entries[i].ToolName < entries[j].ToolName
	})
	adapters := make(map[string]boundMCPAdapter, len(entries))
	defs := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		if err := entry.Contract.validate(); err != nil {
			// Healthy discovery is not sufficient authorization. In particular,
			// do not infer a capability from the tool/server name or description.
			log.Printf("[MCP] quarantine undeclared tool %s/%s: %v", entry.ServerID, entry.ToolName, err)
			continue
		}
		binding, err := BindMCPTool(entries, entry.ServerID, entry.ToolName)
		if err != nil {
			log.Printf("[MCP] skip unbindable tool %s/%s: %v", entry.ServerID, entry.ToolName, err)
			continue
		}
		adapterName, err := newMCPAdapterName(adapters)
		if err != nil {
			log.Printf("[MCP] skip adapter for %s/%s: %v", entry.ServerID, entry.ToolName, err)
			continue
		}
		// A capability-level description is deliberately generic until this
		// legacy surface is replaced by CatalogRenderer. Do not project MCP
		// descriptions, server names, or tool names into the model prompt.
		parameters := safeMCPInvocationSchema(entry.InputSchema)
		defs = append(defs, functionToolDefinition(adapterName, "Perform the approved MCP capability.", parameters))
		adapters[adapterName] = boundMCPAdapter{Binding: binding, Parameters: parameters}
	}
	c.boundMCPCalls().replace(adapters)
	return defs
}

func newMCPAdapterName(existing map[string]boundMCPAdapter) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, 12)
		if _, err := cryptorand.Read(buf); err != nil {
			return "", fmt.Errorf("generate MCP adapter identity: %w", err)
		}
		name := "invoke_mcp_" + base64.RawURLEncoding.EncodeToString(buf)
		if _, exists := existing[name]; !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("generate unique MCP adapter identity")
}

func (c *coreAgentCallbacks) boundMCPCalls() *boundMCPCallSurface {
	c.mcpSurfaceMu.Lock()
	defer c.mcpSurfaceMu.Unlock()
	if c.mcpSurface == nil {
		c.mcpSurface = newBoundMCPCallSurface()
	}
	return c.mcpSurface
}

// executeBoundMCPTool only accepts adapter identities emitted by mcpToolDefs.
// The binding is revalidated by the provider before transport, so a fresh
// inventory cannot silently replace the selected MCP implementation.
func (c *coreAgentCallbacks) executeBoundMCPTool(adapterName string, args map[string]interface{}) (string, bool) {
	adapter, ok := c.boundMCPCalls().adapter(adapterName)
	if !ok {
		return "", false
	}
	adapterRecord, execute, err := c.admitDynamicAdapterInvocation("mcp", adapterName, adapter.Binding.StableID())
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if !execute {
		return dynamicOperationReplayResult(adapterRecord), true
	}
	// Adapter admission only protects the short-lived rendered function name.
	// It has no external effect, so completing it before validation makes a
	// retry with changed arguments a stable replay rather than a second chance.
	if _, err := c.completeDynamicOperation(adapterRecord, DynamicOperationSucceeded, ""); err != nil {
		return "Error: " + err.Error(), true
	}
	if err := validateMCPInvocationArguments(adapter.Parameters, args); err != nil {
		return "Error: " + err.Error(), true
	}
	record, execute, err := c.admitDynamicOperation("mcp", adapter.Binding.StableID(), args)
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if !execute {
		return dynamicOperationReplayResult(record), true
	}
	if c.mcpProvider == nil {
		_, _ = c.completeDynamicOperation(record, DynamicOperationFailed, "mcp_provider_unavailable")
		return "Error: MCP provider is unavailable", true
	}
	if strings.TrimSpace(adapter.Binding.SchemaDigest) == "" {
		_, _ = c.completeDynamicOperation(record, DynamicOperationFailed, "mcp_binding_stale")
		return "Error: mcp_binding_stale", true
	}
	if adapter.Binding.ServerID == "" || adapter.Binding.ToolName == "" {
		_, _ = c.completeDynamicOperation(record, DynamicOperationFailed, "mcp_binding_stale")
		return "Error: mcp_binding_stale", true
	}
	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()
	result, err := callBoundMCPTool(ctx, c.mcpProvider, c.principal, adapter.Binding, args)
	if err != nil {
		log.Printf("[MCP] bound call %s failed: %v", adapter.Binding.StableID(), err)
		// The carrier cannot tell whether a remote effect was accepted before
		// the error. Do not permit automatic replay on this identity.
		_, _ = c.completeDynamicOperation(record, DynamicOperationUnknown, "mcp_transport_unknown")
		return fmt.Sprintf("Error: MCP tool call failed: %v", err), true
	}
	_, _ = c.completeDynamicOperation(record, DynamicOperationSucceeded, "")
	return result, true
}

func validateMCPInvocationArguments(schema map[string]interface{}, args map[string]interface{}) error {
	if args == nil {
		args = map[string]interface{}{}
	}
	properties, _ := schema["properties"].(map[string]interface{})
	for field := range args {
		if _, allowed := properties[field]; !allowed {
			return fmt.Errorf("mcp_argument_not_authorized: %s", field)
		}
	}
	if required, ok := schema["required"].([]interface{}); ok {
		for _, raw := range required {
			field, _ := raw.(string)
			if field != "" {
				if _, present := args[field]; !present {
					return fmt.Errorf("mcp_required_argument_missing: %s", field)
				}
			}
		}
	}
	if required, ok := schema["required"].([]string); ok {
		for _, field := range required {
			if _, present := args[field]; !present {
				return fmt.Errorf("mcp_required_argument_missing: %s", field)
			}
		}
	}
	return nil
}

type boundMCPToolCaller interface {
	CallBoundTool(ctx context.Context, p Principal, binding MCPToolBinding, arguments map[string]interface{}) (string, error)
}

func callBoundMCPTool(ctx context.Context, provider MCPToolProvider, principal Principal, binding MCPToolBinding, arguments map[string]interface{}) (string, error) {
	if bound, ok := provider.(boundMCPToolCaller); ok {
		return bound.CallBoundTool(ctx, principal, binding, arguments)
	}
	return "", fmt.Errorf("mcp bound execution is unavailable")
}

func safeMCPInvocationSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	}
	cloned, ok := cloneMCPJSONValue(schema).(map[string]interface{})
	if !ok || strings.TrimSpace(fmt.Sprint(cloned["type"])) != "object" {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	}
	for _, reserved := range []string{"server_id", "server", "tool_name", "tool", "provider", "provider_id", "selection_id", "artifact_id", "artifact_ref", "credential", "credentials"} {
		if properties, ok := cloned["properties"].(map[string]interface{}); ok {
			delete(properties, reserved)
		}
	}
	// Required fields must be a subset of model-writable properties. A dynamic
	// provider may require server/tool identity in its transport schema, but
	// those values are server-bound and must never become impossible model
	// requirements after projection.
	if properties, ok := cloned["properties"].(map[string]interface{}); ok {
		cloned["required"] = permittedMCPRequiredFields(cloned["required"], properties)
	}
	cloned["additionalProperties"] = false
	return cloned
}

func permittedMCPRequiredFields(raw interface{}, properties map[string]interface{}) []string {
	var values []string
	switch required := raw.(type) {
	case []interface{}:
		for _, value := range required {
			if field, ok := value.(string); ok {
				values = append(values, field)
			}
		}
	case []string:
		values = append(values, required...)
	}
	allowed := make([]string, 0, len(values))
	for _, field := range values {
		if _, ok := properties[field]; ok {
			allowed = append(allowed, field)
		}
	}
	return allowed
}

func cloneMCPJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			out[key] = cloneMCPJSONValue(child)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for index, child := range typed {
			out[index] = cloneMCPJSONValue(child)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
