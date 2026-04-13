package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MCPServerSource, MCPServerEntry — see corelib_aliases.go

// MCPToolView is a tool exposed by an MCP Server.
type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// MCPServerView is the Wails-facing view of an MCP Server including runtime state.
type MCPServerView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	EndpointURL  string          `json:"endpoint_url"`
	AuthType     string          `json:"auth_type"`
	AuthSecret   string          `json:"auth_secret"`
	Source       MCPServerSource `json:"source"`
	Tools        []MCPToolView   `json:"tools"`
	HealthStatus string          `json:"health_status"` // "healthy", "slow", "unavailable", "unknown"
	FailCount    int             `json:"fail_count"`
	LastCheckAt  time.Time       `json:"last_check_at"`
	CreatedAt    time.Time       `json:"created_at"`
}

// MCPRegistry manages locally-registered MCP Servers on the MaClaw client.
type MCPRegistry struct {
	app    *App
	mu     sync.RWMutex
	client *http.Client // shared HTTP client for MCP calls
	// Runtime health tracking (not persisted).
	health map[string]*mcpHealthState
}

type mcpHealthState struct {
	Status    string // "healthy", "slow", "unavailable", "unknown"
	FailCount int
	LastCheck time.Time
}

// NewMCPRegistry creates a new client-side MCP registry.
func NewMCPRegistry(app *App) *MCPRegistry {
	return &MCPRegistry{
		app:    app,
		client: &http.Client{Timeout: 30 * time.Second},
		health: make(map[string]*mcpHealthState),
	}
}

// loadServers reads MCP server entries from config.
func (r *MCPRegistry) loadServers() []MCPServerEntry {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.MCPServers
}

// saveServers persists MCP server entries to config.
func (r *MCPRegistry) saveServers(servers []MCPServerEntry) error {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return err
	}
	cfg.MCPServers = servers
	return r.app.SaveConfig(cfg)
}

// sanitizeMCPID converts a name to a safe ID slug.
func sanitizeMCPID(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var buf strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf.WriteRune(r)
			prevDash = false
		} else if !prevDash && buf.Len() > 0 {
			buf.WriteByte('-')
			prevDash = true
		}
	}
	result := strings.TrimRight(buf.String(), "-")
	if result == "" {
		return "mcp"
	}
	return result
}

// Register adds a new MCP Server.
func (r *MCPRegistry) Register(entry MCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.Name == "" || entry.EndpointURL == "" {
		return fmt.Errorf("name and endpoint_url are required")
	}
	// Auto-generate ID from name if not provided
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s-%d", sanitizeMCPID(entry.Name), time.Now().UnixMilli())
	}
	servers := r.loadServers()
	for _, s := range servers {
		if s.ID == entry.ID {
			return fmt.Errorf("MCP server with id %q already exists", entry.ID)
		}
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if entry.Source == "" {
		entry.Source = MCPSourceManual
	}
	servers = append(servers, entry)
	return r.saveServers(servers)
}

// Update modifies an existing MCP Server.
func (r *MCPRegistry) Update(entry MCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	servers := r.loadServers()
	for i, s := range servers {
		if s.ID == entry.ID {
			if entry.Name != "" {
				servers[i].Name = entry.Name
			}
			if entry.EndpointURL != "" {
				servers[i].EndpointURL = entry.EndpointURL
			}
			servers[i].AuthType = entry.AuthType
			servers[i].AuthSecret = entry.AuthSecret
			return r.saveServers(servers)
		}
	}
	return fmt.Errorf("MCP server %q not found", entry.ID)
}

// Unregister removes an MCP Server by ID.
func (r *MCPRegistry) Unregister(serverID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	servers := r.loadServers()
	for i, s := range servers {
		if s.ID == serverID {
			servers = append(servers[:i], servers[i+1:]...)
			delete(r.health, serverID)
			return r.saveServers(servers)
		}
	}
	return fmt.Errorf("MCP server %q not found", serverID)
}

// ListServers returns all registered servers with runtime health info.
func (r *MCPRegistry) ListServers() []MCPServerView {
	r.mu.RLock()
	defer r.mu.RUnlock()

	servers := r.loadServers()
	views := make([]MCPServerView, 0, len(servers))
	for _, s := range servers {
		v := MCPServerView{
			ID:           s.ID,
			Name:         s.Name,
			EndpointURL:  s.EndpointURL,
			AuthType:     s.AuthType,
			AuthSecret:   s.AuthSecret,
			Source:       s.Source,
			HealthStatus: "unknown",
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			v.CreatedAt = t
		}
		if h, ok := r.health[s.ID]; ok {
			v.HealthStatus = h.Status
			v.FailCount = h.FailCount
			v.LastCheckAt = h.LastCheck
		}
		views = append(views, v)
	}
	return views
}

// findServer looks up a server by ID under RLock and returns a copy.
func (r *MCPRegistry) findServer(serverID string) (*MCPServerEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.loadServers() {
		if s.ID == serverID {
			cp := s
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("MCP server %q not found", serverID)
}

// ResolveServerID resolves an MCP server reference by exact ID first, then exact name.
func (r *MCPRegistry) ResolveServerID(serverRef string) (string, error) {
	serverRef = strings.TrimSpace(serverRef)
	if serverRef == "" {
		return "", fmt.Errorf("MCP server reference is required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	servers := r.loadServers()
	for _, s := range servers {
		if s.ID == serverRef {
			return s.ID, nil
		}
	}

	matches := make([]string, 0, 1)
	for _, s := range servers {
		if strings.TrimSpace(s.Name) == serverRef {
			matches = append(matches, s.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("MCP server name %q is ambiguous; please use server id", serverRef)
	}
	return "", fmt.Errorf("MCP server %q not found", serverRef)
}

// setAuthHeader sets the appropriate auth header on the request.
func setAuthHeader(req *http.Request, authType, authSecret string) {
	if authSecret == "" {
		return
	}
	switch authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+authSecret)
	case "api_key":
		req.Header.Set("X-API-Key", authSecret)
	}
}

// newMCPJSONRequest creates a JSON-RPC request to the given MCP server endpoint.
func (r *MCPRegistry) newMCPJSONRequest(target *MCPServerEntry, body []byte) (*http.Request, error) {
	url := strings.TrimRight(target.EndpointURL, "/")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, target.AuthType, target.AuthSecret)
	return req, nil
}

// CallTool calls a tool on the specified MCP Server with a 30-second timeout.
func (r *MCPRegistry) CallTool(serverID, toolName string, args map[string]interface{}) (string, error) {
	target, err := r.findServer(serverID)
	if err != nil {
		return "", err
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}
	data, _ := json.Marshal(reqBody)

	req, err := r.newMCPJSONRequest(target, data)
	if err != nil {
		return "", err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		r.recordFailure(serverID)
		return "", fmt.Errorf("MCP call failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		r.recordFailure(serverID)
		return "", fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(body))
	}

	r.recordSuccess(serverID)
	return string(body), nil
}

// HealthCheck pings the MCP Server and updates health state.
func (r *MCPRegistry) HealthCheck(serverID string) error {
	target, err := r.findServer(serverID)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}
	data, _ := json.Marshal(reqBody)

	req, err := r.newMCPJSONRequest(target, data)
	if err != nil {
		return err
	}

	healthClient := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := healthClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		r.recordFailure(serverID)
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != http.StatusOK {
		r.recordFailure(serverID)
		return fmt.Errorf("health check HTTP %d", resp.StatusCode)
	}

	r.mu.Lock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount = 0
	h.LastCheck = time.Now()
	if elapsed > 5*time.Second {
		h.Status = "slow"
	} else {
		h.Status = "healthy"
	}
	r.mu.Unlock()
	return nil
}

func (r *MCPRegistry) recordFailure(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount++
	h.LastCheck = time.Now()
	if h.FailCount >= 3 {
		h.Status = "unavailable"
	} else {
		h.Status = "slow"
	}
}

func (r *MCPRegistry) recordSuccess(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount = 0
	h.LastCheck = time.Now()
	h.Status = "healthy"
}

func (r *MCPRegistry) getOrCreateHealth(serverID string) *mcpHealthState {
	h, ok := r.health[serverID]
	if !ok {
		h = &mcpHealthState{Status: "unknown"}
		r.health[serverID] = h
	}
	return h
}

// RegisterAutoDiscovered registers an auto-discovered MCP Server.
// If a manually registered server with the same ID already exists, the
// auto-discovered entry is silently ignored to preserve manual configuration.
func (r *MCPRegistry) RegisterAutoDiscovered(entry MCPServerEntry, source MCPServerSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.ID == "" || entry.Name == "" || entry.EndpointURL == "" {
		return fmt.Errorf("id, name, and endpoint_url are required")
	}

	servers := r.loadServers()
	for _, s := range servers {
		if s.ID == entry.ID {
			// Conflict with an existing entry — if it was manually registered,
			// silently ignore the auto-discovered one (requirement 1.5).
			if s.Source == MCPSourceManual || s.Source == "" {
				return nil
			}
			// Already registered from auto-discovery; skip duplicate.
			return nil
		}
	}

	entry.Source = source
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	servers = append(servers, entry)
	return r.saveServers(servers)
}

// StartHealthLoop starts a background goroutine that performs a health check
// on every registered MCP Server every 60 seconds. It also calls
// RemoveUnhealthy after each round to prune auto-discovered servers that have
// failed 3 consecutive checks. The loop stops when ctx is cancelled.
func (r *MCPRegistry) StartHealthLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.mu.RLock()
				servers := r.loadServers()
				r.mu.RUnlock()

				for _, s := range servers {
					if err := r.HealthCheck(s.ID); err != nil {
						log.Printf("[MCPRegistry] health check failed for %s: %v", s.ID, err)
					}
				}

				r.RemoveUnhealthy()
			}
		}
	}()
}

// RemoveUnhealthy removes auto-discovered servers that have failed 3 or more
// consecutive health checks. Manually registered servers are never removed
// automatically (requirement 1.4).
func (r *MCPRegistry) RemoveUnhealthy() {
	r.mu.Lock()
	defer r.mu.Unlock()

	servers := r.loadServers()
	var kept []MCPServerEntry
	for _, s := range servers {
		h, ok := r.health[s.ID]
		if ok && h.FailCount >= 3 && s.Source != MCPSourceManual && s.Source != "" {
			// Auto-discovered server with >= 3 consecutive failures — remove it.
			delete(r.health, s.ID)
			log.Printf("[MCPRegistry] removed unhealthy auto-discovered server %s (%s)", s.ID, s.Source)
			continue
		}
		kept = append(kept, s)
	}

	if len(kept) != len(servers) {
		_ = r.saveServers(kept)
	}
}

// GetServerTools fetches the tool list from an MCP Server.
func (r *MCPRegistry) GetServerTools(serverID string) []MCPToolView {
	target, err := r.findServer(serverID)
	if err != nil {
		return nil
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}
	data, _ := json.Marshal(reqBody)

	req, err := r.newMCPJSONRequest(target, data)
	if err != nil {
		return nil
	}

	healthClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := healthClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result struct {
		Result struct {
			Tools []MCPToolView `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return result.Result.Tools
}

// --- Wails binding functions ---

// ListMCPServers returns all registered MCP Servers (Wails binding).
func (a *App) ListMCPServers() []MCPServerView {
	if a.mcpRegistry == nil {
		return nil
	}
	return a.mcpRegistry.ListServers()
}

// RegisterMCPServer registers a new MCP Server (Wails binding).
func (a *App) RegisterMCPServer(server MCPServerEntry) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.Register(server)
}

// UpdateMCPServer updates an existing MCP Server (Wails binding).
func (a *App) UpdateMCPServer(server MCPServerEntry) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.Update(server)
}

// UnregisterMCPServer removes an MCP Server by ID (Wails binding).
func (a *App) UnregisterMCPServer(serverID string) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.Unregister(serverID)
}

// GetMCPServerTools returns the tool list for a specific MCP Server (Wails binding).
func (a *App) GetMCPServerTools(serverID string) []MCPToolView {
	if a.mcpRegistry == nil {
		return nil
	}
	return a.mcpRegistry.GetServerTools(serverID)
}

// CheckMCPServerHealth triggers a health check for the specified MCP Server (Wails binding).
func (a *App) CheckMCPServerHealth(serverID string) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.HealthCheck(serverID)
}

// ─── Local (stdio) MCP Server support ───────────────────────────────────────

// LocalMCPServerEntry — see corelib_aliases.go

// loadLocalServers reads local MCP server entries from config.
func (r *MCPRegistry) loadLocalServers() []LocalMCPServerEntry {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.LocalMCPServers
}

// saveLocalServers persists local MCP server entries to config.
func (r *MCPRegistry) saveLocalServers(servers []LocalMCPServerEntry) error {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return err
	}
	cfg.LocalMCPServers = servers
	return r.app.SaveConfig(cfg)
}

// RegisterLocal adds a new local MCP server entry.
func (r *MCPRegistry) RegisterLocal(entry LocalMCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	servers = append(servers, entry)
	return r.saveLocalServers(servers)
}

// UpdateLocal updates an existing local MCP server entry.
func (r *MCPRegistry) UpdateLocal(entry LocalMCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	for i, s := range servers {
		if s.ID == entry.ID {
			entry.CreatedAt = s.CreatedAt
			servers[i] = entry
			return r.saveLocalServers(servers)
		}
	}
	return fmt.Errorf("local MCP server %s not found", entry.ID)
}

// UnregisterLocal removes a local MCP server entry by ID.
func (r *MCPRegistry) UnregisterLocal(serverID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	for i, s := range servers {
		if s.ID == serverID {
			servers = append(servers[:i], servers[i+1:]...)
			return r.saveLocalServers(servers)
		}
	}
	return fmt.Errorf("local MCP server %s not found", serverID)
}

// SetLocalAutoStart updates the AutoStart flag for a local MCP server entry.
func (r *MCPRegistry) SetLocalAutoStart(serverID string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	for i := range servers {
		if servers[i].ID == serverID {
			servers[i].AutoStart = enabled
			return r.saveLocalServers(servers)
		}
	}
	return fmt.Errorf("local MCP server %s not found", serverID)
}

// ListLocalServers returns all local MCP server entries.
func (r *MCPRegistry) ListLocalServers() []LocalMCPServerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loadLocalServers()
}

// ─── Wails bindings for Local MCP Servers ───────────────────────────────────

// ListLocalMCPServers returns all local (stdio) MCP server configs (Wails binding).
func (a *App) ListLocalMCPServers() []LocalMCPServerEntry {
	if a.mcpRegistry == nil {
		return nil
	}
	return a.mcpRegistry.ListLocalServers()
}

// RegisterLocalMCPServer adds a new local MCP server config (Wails binding).
func (a *App) RegisterLocalMCPServer(server LocalMCPServerEntry) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.RegisterLocal(server)
}

// UpdateLocalMCPServer updates an existing local MCP server config (Wails binding).
func (a *App) UpdateLocalMCPServer(server LocalMCPServerEntry) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.UpdateLocal(server)
}

// UnregisterLocalMCPServer removes a local MCP server config by ID (Wails binding).
func (a *App) UnregisterLocalMCPServer(serverID string) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	return a.mcpRegistry.UnregisterLocal(serverID)
}

// SyncLocalMCPServers triggers the local MCP manager to re-read config
// and start/stop processes accordingly (Wails binding).
func (a *App) SyncLocalMCPServers() error {
	a.ensureLocalMCPManager()
	if a.localMCPManager == nil {
		return fmt.Errorf("local MCP manager not initialized")
	}
	a.localMCPManager.SyncFromConfig()
	return nil
}

// SetLocalMCPAutoStart sets the AutoStart flag for a local MCP server and
// triggers a sync. When enabled=true the server starts immediately and will
// auto-start on future app launches. When enabled=false the server stays
// governed by Disabled for the current run, but will not auto-start on the
// next app launch.
func (a *App) SetLocalMCPAutoStart(serverID string, enabled bool) error {
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if err := a.mcpRegistry.SetLocalAutoStart(serverID, enabled); err != nil {
		return err
	}
	// Sync immediately so the server starts/stops now.
	a.ensureLocalMCPManager()
	if a.localMCPManager != nil {
		a.localMCPManager.SyncFromConfig()
	}
	return nil
}

// LocalMCPServerStatus represents the runtime status of a local MCP server.
type LocalMCPServerStatus struct {
	ID      string `json:"id"`
	Running bool   `json:"running"`
}

func (a *App) resolveMCPServerRef(serverRef string) (resolvedID string, isLocal bool, err error) {
	serverRef = strings.TrimSpace(serverRef)
	if serverRef == "" {
		return "", false, fmt.Errorf("missing server_id parameter")
	}

	if a.localMCPManager != nil {
		if id, localErr := a.localMCPManager.ResolveServerID(serverRef); localErr == nil {
			return id, true, nil
		} else if strings.Contains(localErr.Error(), "ambiguous") {
			return "", false, localErr
		}
	}

	if a.mcpRegistry == nil {
		return "", false, fmt.Errorf("MCP registry not initialized")
	}
	id, err := a.mcpRegistry.ResolveServerID(serverRef)
	if err != nil {
		return "", false, err
	}
	return id, false, nil
}

// GetLocalMCPServerStatuses returns the running status of all configured
// local MCP servers (Wails binding).
func (a *App) GetLocalMCPServerStatuses() []LocalMCPServerStatus {
	if a.mcpRegistry == nil {
		return nil
	}
	entries := a.mcpRegistry.ListLocalServers()
	result := make([]LocalMCPServerStatus, len(entries))
	for i, e := range entries {
		running := false
		if a.localMCPManager != nil {
			running = a.localMCPManager.IsRunning(e.ID)
		}
		result[i] = LocalMCPServerStatus{ID: e.ID, Running: running}
	}
	return result
}
