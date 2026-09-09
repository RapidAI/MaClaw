package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// MCPToolView is a tool exposed by an MCP Server.
// The JSON tag uses snake_case ("input_schema") for internal serialization and
// Wails bindings. MCP wire format uses camelCase ("inputSchema"); use
// mcpWireToolView for deserializing tools/list responses.
type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// mcpWireToolView mirrors MCPToolView but uses the MCP protocol's camelCase
// field names for JSON deserialization of tools/list responses.
type mcpWireToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// toView converts a wire-format tool to the internal MCPToolView.
func (w mcpWireToolView) toView() MCPToolView {
	return MCPToolView{
		Name:        w.Name,
		Description: w.Description,
		InputSchema: w.InputSchema,
	}
}

// mcpWireToolsToViews converts a slice of wire-format tools to MCPToolViews.
func mcpWireToolsToViews(wire []mcpWireToolView) []MCPToolView {
	views := make([]MCPToolView, len(wire))
	for i := range wire {
		views[i] = wire[i].toView()
	}
	return views
}

// MCPServerView is the Wails-facing view of an MCP Server including runtime state.
type MCPServerView struct {
	ID                     string                          `json:"id"`
	Name                   string                          `json:"name"`
	EndpointURL            string                          `json:"endpoint_url"`
	AuthType               string                          `json:"auth_type"`
	AuthSecret             string                          `json:"auth_secret"`
	Headers                map[string]string               `json:"headers,omitempty"`
	Source                 corelib.MCPServerSource         `json:"source"`
	Capability             *corelib.MCPServerCapabilityRef `json:"capability,omitempty"`
	Tools                  []MCPToolView                   `json:"tools"`
	HealthStatus           mcpHealthStatus                 `json:"health_status"`
	FailCount              int                             `json:"fail_count"`
	LastCheckAt            time.Time                       `json:"last_check_at"`
	CreatedAt              time.Time                       `json:"created_at"`
	Managed                bool                            `json:"managed"`
	RuntimeSyncStatus      string                          `json:"runtime_sync_status,omitempty"`
	RuntimeSyncAttempts    int                             `json:"runtime_sync_attempts,omitempty"`
	RuntimeSyncLastError   string                          `json:"runtime_sync_last_error,omitempty"`
	RuntimeSyncNextRetryAt string                          `json:"runtime_sync_next_retry_at,omitempty"`
}

// LocalMCPServerView is the Wails-facing view of a local MCP server including managed status.
type LocalMCPServerView struct {
	ID                     string                          `json:"id"`
	Name                   string                          `json:"name"`
	Command                string                          `json:"command"`
	Args                   []string                        `json:"args,omitempty"`
	Env                    map[string]string               `json:"env,omitempty"`
	Disabled               bool                            `json:"disabled,omitempty"`
	AutoStart              bool                            `json:"auto_start,omitempty"`
	CreatedAt              string                          `json:"created_at"`
	Source                 corelib.MCPServerSource         `json:"source,omitempty"`
	Capability             *corelib.MCPServerCapabilityRef `json:"capability,omitempty"`
	Managed                bool                            `json:"managed"`
	RuntimeSyncStatus      string                          `json:"runtime_sync_status,omitempty"`
	RuntimeSyncAttempts    int                             `json:"runtime_sync_attempts,omitempty"`
	RuntimeSyncLastError   string                          `json:"runtime_sync_last_error,omitempty"`
	RuntimeSyncNextRetryAt string                          `json:"runtime_sync_next_retry_at,omitempty"`
}

// MCPRegistry manages locally-registered MCP Servers on the MaClaw client.
type MCPRegistry struct {
	app    *App
	mu     sync.RWMutex
	client *http.Client // shared HTTP client for MCP calls
	// Runtime health tracking (not persisted).
	health map[string]*mcpHealthState
	// Cached tool lists from the last successful tools/list call.
	toolsCache map[string][]MCPToolView
	// MCP session tracking (per-server Streamable HTTP sessions).
	sessionMu        sync.RWMutex
	sessions         map[string]*mcpSession
	sessionInitMu    sync.Mutex
	sessionInitLocks map[string]*sync.Mutex
}

// mcpSession tracks an active MCP Streamable HTTP session for a server.
type mcpSession struct {
	SessionID string
	CreatedAt time.Time
}

func mcpSessionKey(serverID, ownerID string) string {
	serverID = strings.TrimSpace(serverID)
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return serverID
	}
	return serverID + "\x00" + ownerID
}

func (r *MCPRegistry) getSession(serverID string) (mcpSession, bool) {
	return r.getSessionForOwner(serverID, "")
}

func (r *MCPRegistry) getSessionForOwner(serverID, ownerID string) (mcpSession, bool) {
	r.sessionMu.RLock()
	defer r.sessionMu.RUnlock()
	if r.sessions == nil {
		return mcpSession{}, false
	}
	sess, ok := r.sessions[mcpSessionKey(serverID, ownerID)]
	if !ok || sess == nil {
		return mcpSession{}, false
	}
	return *sess, true
}

func (r *MCPRegistry) setSession(serverID, sessionID string) {
	r.setSessionForOwner(serverID, "", sessionID)
}

func (r *MCPRegistry) setSessionForOwner(serverID, ownerID, sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if r.sessions == nil {
		r.sessions = make(map[string]*mcpSession)
	}
	r.sessions[mcpSessionKey(serverID, ownerID)] = &mcpSession{SessionID: sessionID, CreatedAt: time.Now()}
}

func (r *MCPRegistry) deleteSession(serverID string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	serverID = strings.TrimSpace(serverID)
	for key := range r.sessions {
		if key == serverID || strings.HasPrefix(key, serverID+"\x00") {
			delete(r.sessions, key)
		}
	}
}

func (r *MCPRegistry) deleteSessionForOwner(serverID, ownerID string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	delete(r.sessions, mcpSessionKey(serverID, ownerID))
}

type mcpHealthState struct {
	Status    mcpHealthStatus
	FailCount int
	LastCheck time.Time
}

// mcpBackgroundProbeTimeout bounds one remote readiness/health request. Keep
// this separate from the longer HTTP client timeout so one slow server cannot
// stall an entire background health round.
const mcpBackgroundProbeTimeout = 15 * time.Second

// NewMCPRegistry creates a new client-side MCP registry.
func NewMCPRegistry(app *App) *MCPRegistry {
	return &MCPRegistry{
		app:        app,
		client:     &http.Client{Timeout: 30 * time.Second},
		health:     make(map[string]*mcpHealthState),
		toolsCache: make(map[string][]MCPToolView),
		sessions:   make(map[string]*mcpSession),
	}
}

func (r *MCPRegistry) sessionInitLockForOwner(serverID, ownerID string) *sync.Mutex {
	if r == nil {
		return &sync.Mutex{}
	}
	key := mcpSessionKey(serverID, ownerID)
	r.sessionInitMu.Lock()
	defer r.sessionInitMu.Unlock()
	if r.sessionInitLocks == nil {
		r.sessionInitLocks = make(map[string]*sync.Mutex)
	}
	lock := r.sessionInitLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.sessionInitLocks[key] = lock
	}
	return lock
}

// loadServers reads MCP server entries from config.
func (r *MCPRegistry) loadServers() []corelib.MCPServerEntry {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.MCPServers
}

// saveServers persists MCP server entries to config.
func (r *MCPRegistry) saveServers(servers []corelib.MCPServerEntry) error {
	return r.app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MCPServers = servers
	})
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
func (r *MCPRegistry) Register(entry corelib.MCPServerEntry) error {
	_, err := r.register(entry, true)
	return err
}

func (r *MCPRegistry) register(entry corelib.MCPServerEntry, asyncHealthCheck bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.Name == "" || entry.EndpointURL == "" {
		return "", fmt.Errorf("name and endpoint_url are required")
	}
	// Auto-generate ID from name if not provided
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s-%d", sanitizeMCPID(entry.Name), time.Now().UnixMilli())
	}
	servers := r.loadServers()
	for _, s := range servers {
		if s.ID == entry.ID {
			return "", fmt.Errorf("MCP server with id %q already exists", entry.ID)
		}
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if entry.Source == "" {
		entry.Source = corelib.MCPSourceManual
	}
	servers = append(servers, entry)
	if err := r.saveServers(servers); err != nil {
		return "", err
	}
	if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil && r.app != nil {
		if err := r.app.resetMCPRuntimeSyncPending(entry.ID, "http"); err != nil {
			return "", fmt.Errorf("MCP config saved but runtime state initialization failed: %w", err)
		}
	}
	if asyncHealthCheck {
		// Trigger async health check for the newly registered server.
		go func() {
			var err error
			if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil {
				err = r.HealthCheckStrictContext(context.Background(), entry.ID)
				if err != nil {
					if stateErr := r.app.recordMCPRuntimeSyncFailure(entry.ID, "http", "", err); stateErr != nil {
						log.Printf("[MCPRegistry] persist initial runtime failure for %s: %v", entry.ID, stateErr)
					}
				}
			} else {
				err = r.HealthCheck(entry.ID)
			}
			if err != nil {
				log.Printf("[MCPRegistry] initial health check for %s failed: %v", entry.ID, err)
			}
		}()
	}
	return entry.ID, nil
}

// Update modifies an existing MCP Server.
func (r *MCPRegistry) Update(entry corelib.MCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	servers := r.loadServers()
	for i, s := range servers {
		if s.ID == entry.ID {
			_, hadRuntimeState := loadMCPRuntimeSyncState(entry.ID)
			managedRuntime := s.Source == corelib.MCPSourceMarket && s.Capability != nil
			configChanged := false
			endpointChanged := entry.EndpointURL != "" && entry.EndpointURL != s.EndpointURL
			if entry.Name != "" {
				configChanged = configChanged || entry.Name != s.Name
				servers[i].Name = entry.Name
			}
			if entry.EndpointURL != "" {
				configChanged = configChanged || entry.EndpointURL != s.EndpointURL
				servers[i].EndpointURL = entry.EndpointURL
			}
			authChanged := false
			// Treat omitted optional fields as unchanged. An explicit auth type
			// (including "none") or non-empty secret still replaces the existing
			// contract; this keeps partial Wails updates from erasing credentials.
			if entry.AuthType != "" || entry.AuthSecret != "" {
				authChanged = entry.AuthType != s.AuthType || entry.AuthSecret != s.AuthSecret
				configChanged = configChanged || authChanged
				if entry.AuthType != "" {
					servers[i].AuthType = entry.AuthType
				}
				servers[i].AuthSecret = entry.AuthSecret
			}
			if entry.Headers != nil {
				headerChanged := !reflect.DeepEqual(entry.Headers, s.Headers)
				authChanged = authChanged || headerChanged
				configChanged = configChanged || headerChanged
				servers[i].Headers = entry.Headers
			}
			if entry.Source != "" {
				configChanged = configChanged || entry.Source != s.Source
				servers[i].Source = entry.Source
			}
			if entry.Capability != nil {
				configChanged = configChanged || !reflect.DeepEqual(entry.Capability, s.Capability)
				servers[i].Capability = entry.Capability
			} else if entry.Source != "" && entry.Source != corelib.MCPSourceMarket && s.Capability != nil {
				// Allow an explicit managed-to-manual conversion to remove the
				// marketplace capability identity and its durable runtime blocker.
				configChanged = true
				servers[i].Capability = nil
			}
			// Invalidate cached tools and health when endpoint or auth changes —
			// the old cache is from a different server configuration.
			// Custom headers are part of the remote authentication/transport
			// contract as well. Reusing a session or tool cache after a header
			// change can send requests with stale credentials (or expose a tool
			// inventory from the previous tenant), even though endpoint/authType
			// remained unchanged.
			if endpointChanged || authChanged {
				delete(r.toolsCache, entry.ID)
				delete(r.health, entry.ID)
				r.deleteSession(entry.ID)
			}
			if err := r.saveServers(servers); err != nil {
				return err
			}
			newManagedRuntime := servers[i].Source == corelib.MCPSourceMarket && servers[i].Capability != nil
			if configChanged && r.app != nil {
				switch {
				case newManagedRuntime:
					if err := r.app.resetMCPRuntimeSyncPending(entry.ID, "http"); err != nil {
						return fmt.Errorf("persist MCP runtime sync pending state: %w", err)
					}
					go func(serverID string) {
						if err := r.HealthCheckStrictContext(context.Background(), serverID); err != nil {
							if stateErr := r.app.recordMCPRuntimeSyncFailure(serverID, "http", "", err); stateErr != nil {
								log.Printf("[MCPRegistry] persist update runtime failure for %s: %v", serverID, stateErr)
							}
							return
						}
						if err := r.app.markMCPRuntimeSyncReady(serverID); err != nil {
							log.Printf("[MCPRegistry] clear update runtime state for %s: %v", serverID, err)
						}
					}(entry.ID)
				case hadRuntimeState || managedRuntime:
					// A managed-to-manual conversion removes the marketplace-only
					// blocker. Running a strict marketplace probe here would be both
					// surprising and unsafe because the manual entry no longer has a
					// durable runtime-readiness contract.
					if err := clearMCPRuntimeSyncState(entry.ID); err != nil {
						return fmt.Errorf("clear MCP runtime sync state: %w", err)
					}
				}
			}
			return nil
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
			delete(r.toolsCache, serverID)
			r.deleteSession(serverID)
			if err := r.saveServers(servers); err != nil {
				return err
			}
			if r.app != nil {
				if err := clearMCPRuntimeSyncState(serverID); err != nil {
					return fmt.Errorf("clear MCP runtime sync state: %w", err)
				}
			}
			return nil
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
			Headers:      s.Headers,
			Source:       s.Source,
			Capability:   s.Capability,
			HealthStatus: mcpHealthStatusUnknown,
			Managed:      r.isManagedCapability(&s),
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			v.CreatedAt = t
		}
		runtimeBlocked := false
		if h, ok := r.health[s.ID]; ok {
			v.HealthStatus = h.Status
			v.FailCount = h.FailCount
			v.LastCheckAt = h.LastCheck
		}
		if syncState, ok := loadMCPRuntimeSyncState(s.ID); ok {
			if syncState.Status != "ready" {
				runtimeBlocked = true
				v.RuntimeSyncStatus = syncState.Status
				v.RuntimeSyncAttempts = syncState.Attempts
				v.RuntimeSyncLastError = syncState.LastError
				v.RuntimeSyncNextRetryAt = syncState.NextRetryAt
				// A durable runtime-sync failure supersedes a stale in-memory
				// health marker. This keeps semantic routing fail-closed across
				// process restarts until a checked probe succeeds.
				v.HealthStatus = mcpHealthStatusUnavailable
			}
		} else if r.app != nil && r.app.mcpRuntimeSyncRequired(s.ID) {
			// A managed entry without a durable observation is still pending.
			// Surface the same fail-closed state used by call admission instead of
			// showing a stale/optimistic healthy status after restart.
			v.RuntimeSyncStatus = "pending"
			v.RuntimeSyncLastError = "runtime synchronization pending"
			v.HealthStatus = mcpHealthStatusUnavailable
			runtimeBlocked = true
		}
		if !runtimeBlocked {
			if tools, ok := r.toolsCache[s.ID]; ok {
				v.Tools = tools
			}
		}
		views = append(views, v)
	}
	return views
}

// isManagedCapability checks if a server entry is a managed (forced) deployment
// that cannot be deleted by the user.
func (r *MCPRegistry) isManagedCapability(entry *corelib.MCPServerEntry) bool {
	if entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
		return false
	}
	if r.app == nil {
		return false
	}
	return r.app.isCapabilityManagedDeployment(entry.Capability.CapabilityID)
}

// findServer looks up a server by ID under RLock and returns a copy.
func (r *MCPRegistry) findServer(serverID string) (*corelib.MCPServerEntry, error) {
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
// It first applies any custom headers from the entry, then applies
// AuthType/AuthSecret (which take precedence over custom headers).
// Content-Type and Accept are protocol-level headers set by newMCPJSONRequest
// and are not overridable via custom headers.
func setAuthHeader(req *http.Request, target *corelib.MCPServerEntry) {
	// Apply custom headers first (lower precedence).
	for k, v := range target.Headers {
		if k == "" || v == "" {
			continue
		}
		// Protect protocol-level headers from being overridden.
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "accept" {
			continue
		}
		req.Header.Set(k, v)
	}
	// Apply AuthType/AuthSecret (higher precedence, overwrites custom Authorization if both set).
	if target.AuthSecret == "" {
		return
	}
	switch target.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+target.AuthSecret)
	case "api_key":
		req.Header.Set("X-API-Key", target.AuthSecret)
	}
}

// newMCPJSONRequest creates a JSON-RPC request to the given MCP server endpoint.
// If a session ID is known for this server, it is included via the Mcp-Session-Id header.
func (r *MCPRegistry) newMCPJSONRequest(target *corelib.MCPServerEntry, body []byte, ownerID ...string) (*http.Request, error) {
	return r.newMCPJSONRequestContext(context.Background(), target, body, ownerID...)
}

func (r *MCPRegistry) newMCPJSONRequestContext(ctx context.Context, target *corelib.MCPServerEntry, body []byte, ownerID ...string) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	url := strings.TrimRight(target.EndpointURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	setAuthHeader(req, target)
	// Attach MCP session ID if available (required by Streamable HTTP servers).
	owner := ""
	if len(ownerID) > 0 {
		owner = ownerID[0]
	}
	owner = strings.TrimSpace(owner)
	if sess, ok := r.getSessionForOwner(target.ID, owner); ok && sess.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", sess.SessionID)
	}
	return req, nil
}

// doMCPRoundTrip executes an MCP JSON-RPC request and extracts the session ID
// from the response. Returns the parsed JSON-RPC payload.
func (r *MCPRegistry) doMCPRoundTrip(target *corelib.MCPServerEntry, reqBody map[string]interface{}, ownerID ...string) ([]byte, error) {
	return r.doMCPRoundTripContext(context.Background(), target, reqBody, ownerID...)
}

func (r *MCPRegistry) doMCPRoundTripContext(ctx context.Context, target *corelib.MCPServerEntry, reqBody map[string]interface{}, ownerID ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	data, _ := json.Marshal(reqBody)
	owner := ""
	if len(ownerID) > 0 {
		owner = ownerID[0]
	}
	req, err := r.newMCPJSONRequestContext(ctx, target, data, owner)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MCP HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Capture session ID from response header (Streamable HTTP servers).
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		r.setSessionForOwner(target.ID, owner, sid)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, truncateMCPBody(errBody))
	}

	ct := resp.Header.Get("Content-Type")
	parsed, err := corelib.ParseMCPResponse(resp.Body, ct, 256*1024)
	if err != nil {
		return nil, fmt.Errorf("parse MCP response: %w", err)
	}
	return parsed, nil
}

// ensureSession sends an MCP "initialize" handshake if no session exists for
// the given server. Streamable HTTP servers (e.g. 智谱 BigModel) require this
// handshake before tools/call will accept the API key.
func (r *MCPRegistry) ensureSession(target *corelib.MCPServerEntry, ownerID ...string) error {
	return r.ensureSessionContext(context.Background(), target, ownerID...)
}

func (r *MCPRegistry) ensureSessionContext(ctx context.Context, target *corelib.MCPServerEntry, ownerID ...string) error {
	return r.ensureSessionContextMode(ctx, target, false, ownerID...)
}

// ensureSessionContextMode performs the initialize handshake. Legacy tool
// calls keep the permissive behavior for simple REST-compatible servers that
// do not implement initialize, while checked readiness probes can require a
// successful MCP handshake before declaring a managed runtime ready.
func (r *MCPRegistry) ensureSessionContextMode(ctx context.Context, target *corelib.MCPServerEntry, strict bool, ownerID ...string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	startedAt := time.Now()
	owner := ""
	if len(ownerID) > 0 {
		owner = ownerID[0]
	}
	owner = strings.TrimSpace(owner)
	if !strict {
		if sess, ok := r.getSessionForOwner(target.ID, owner); ok && sess.SessionID != "" {
			// Session already established; check if it's stale (>30 min).
			if time.Since(sess.CreatedAt) < 30*time.Minute {
				return nil
			}
			// Stale session — re-initialize.
			r.deleteSessionForOwner(target.ID, owner)
		}
	} else {
		// Strict readiness probes must prove initialize on this checked run;
		// an old session alone is not durable evidence of a successful handshake.
		r.deleteSessionForOwner(target.ID, owner)
	}

	initLock := r.sessionInitLockForOwner(target.ID, owner)
	lockWaitStart := time.Now()
	initLock.Lock()
	if waited := time.Since(lockWaitStart); waited > 100*time.Millisecond {
		log.Printf("[MCPRegistry] ensure_session lock_wait server=%s owner=%q waited=%s", target.ID, owner, waited.Round(time.Millisecond))
	}
	defer initLock.Unlock()
	if !strict {
		if sess, ok := r.getSessionForOwner(target.ID, owner); ok && sess.SessionID != "" {
			if time.Since(sess.CreatedAt) < 30*time.Minute {
				return nil
			}
			r.deleteSessionForOwner(target.ID, owner)
		}
	} else {
		r.deleteSessionForOwner(target.ID, owner)
	}

	log.Printf("[MCPRegistry] ensure_session start server=%s owner=%q", target.ID, owner)
	initBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "maclaw",
				"version": "1.0.0",
			},
		},
	}

	initPayload, err := r.doMCPRoundTripContext(ctx, target, initBody, owner)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if strict {
			return fmt.Errorf("initialize handshake failed: %w", err)
		}
		// Some servers don't support initialize (e.g. simple REST-based MCP).
		// Log and continue — the session will be empty and requests will
		// proceed without Mcp-Session-Id (backward compatible).
		log.Printf("[MCPRegistry] initialize handshake failed for %s owner=%q elapsed=%s: %v (proceeding without session)", target.ID, strings.TrimSpace(owner), time.Since(startedAt).Round(time.Millisecond), err)
		return nil
	}
	if strict {
		if err := validateMCPJSONRPCSuccess(initPayload, "initialize"); err != nil {
			return err
		}
	}
	log.Printf("[MCPRegistry] ensure_session done server=%s owner=%q elapsed=%s", target.ID, strings.TrimSpace(owner), time.Since(startedAt).Round(time.Millisecond))
	return nil
}

// CallTool calls a tool on the specified MCP Server with a 30-second timeout.
func (r *MCPRegistry) CallTool(serverID, toolName string, args map[string]interface{}) (string, error) {
	return r.CallToolForOwner("", serverID, toolName, args)
}

// CallToolForOwner calls a tool with an owner-scoped Streamable HTTP session.
// Independent agent loops must not share one remote MCP session.
func (r *MCPRegistry) CallToolForOwner(ownerID, serverID, toolName string, args map[string]interface{}) (string, error) {
	startedAt := time.Now()
	if coretool.IsDisabledExternalCodingSessionTool(toolName) {
		return "", fmt.Errorf("external coding-session tool %q is disabled", toolName)
	}
	ownerID = strings.TrimSpace(ownerID)
	if r.app != nil && r.app.mcpRuntimeSyncPending(serverID) {
		return "", fmt.Errorf("MCP runtime for %q is not ready; runtime synchronization requires reconciliation", strings.TrimSpace(serverID))
	}
	defer func() {
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			log.Printf("[MCPRegistry] call_tool slow server=%s owner=%q tool=%s elapsed=%s", serverID, ownerID, toolName, elapsed.Round(time.Millisecond))
		}
	}()
	target, err := r.findServer(serverID)
	if err != nil {
		return "", err
	}

	if args == nil {
		args = map[string]interface{}{}
	}

	// Ensure MCP session is established (Streamable HTTP handshake).
	if err := r.ensureSession(target, ownerID); err != nil {
		return "", fmt.Errorf("MCP session init failed: %w", err)
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

	parsed, err := r.doMCPRoundTrip(target, reqBody, ownerID)
	if err != nil {
		// If we get an auth error and have a session, the session may be stale.
		// Clear it and retry once with a fresh session.
		if _, hasSession := r.getSessionForOwner(target.ID, ownerID); hasSession && isMCPAuthError(err) {
			log.Printf("[MCPRegistry] auth error with session for %s, retrying with fresh session", serverID)
			r.deleteSessionForOwner(target.ID, ownerID)
			if initErr := r.ensureSession(target, ownerID); initErr == nil {
				parsed, err = r.doMCPRoundTrip(target, reqBody, ownerID)
			}
		}
		if err != nil {
			r.recordFailure(serverID)
			return "", err
		}
	}

	r.recordSuccess(serverID)
	return string(parsed), nil
}

// HealthCheck pings the MCP Server and updates health state.
func (r *MCPRegistry) HealthCheck(serverID string) error {
	return r.HealthCheckContext(context.Background(), serverID)
}

// HealthCheckContext performs a checked remote MCP tools/list probe. Unlike
// the legacy background health loop, cancellation and request failures are
// returned to the caller so a managed capability install cannot report a
// healthy runtime without discovery evidence.
func (r *MCPRegistry) HealthCheckContext(ctx context.Context, serverID string) error {
	return r.healthCheckContextMode(ctx, serverID, false)
}

// HealthCheckStrictContext requires both initialize and tools/list. Managed
// marketplace installs use this stronger contract; ordinary/manual MCP health
// checks retain compatibility with REST-like servers that only implement the
// tools/list request.
func (r *MCPRegistry) HealthCheckStrictContext(ctx context.Context, serverID string) error {
	return r.healthCheckContextMode(ctx, serverID, true)
}

func (r *MCPRegistry) healthCheckContextMode(ctx context.Context, serverID string, strict bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := r.findServer(serverID)
	if err != nil {
		return err
	}

	// Ensure session for Streamable HTTP servers.
	if err := r.ensureSessionContextMode(ctx, target, strict); err != nil {
		return fmt.Errorf("MCP session init failed: %w", err)
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	start := time.Now()
	parsed, err := r.doMCPRoundTripContext(ctx, target, reqBody)
	elapsed := time.Since(start)

	if err != nil {
		r.recordFailure(serverID)
		return fmt.Errorf("health check failed: %w", err)
	}
	if strict {
		if err := validateMCPJSONRPCSuccess(parsed, "tools/list"); err != nil {
			r.recordFailure(serverID)
			return fmt.Errorf("health check failed: %w", err)
		}
		if err := validateMCPToolsListResult(parsed); err != nil {
			r.recordFailure(serverID)
			return fmt.Errorf("health check failed: %w", err)
		}
	}

	// Parse and cache the tool list from the response (tools/list returns
	// the same data GetServerTools needs, so we cache it here to avoid a
	// redundant round-trip when the management panel displays tool counts).
	// NOTE: MCP protocol uses camelCase "inputSchema" in the wire format,
	// but MCPToolView uses snake_case "input_schema" for internal/Wails
	// serialization. We use mcpWireToolView to bridge the mismatch.
	var toolsResult struct {
		Result struct {
			Tools []mcpWireToolView `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(parsed, &toolsResult); err == nil {
		if strict && r.app != nil {
			// A configuration update can race an in-flight probe. Never publish
			// tools or mark the new configuration ready based on a response from
			// the old endpoint/auth contract; the update path already reset the
			// durable runtime state to pending. Treat the stale response as an
			// obsolete probe and let the next checked cycle evaluate the new copy.
			current, findErr := r.findServer(serverID)
			if findErr != nil || current == nil || !reflect.DeepEqual(*current, *target) {
				return nil
			}
		}
		r.mu.Lock()
		r.toolsCache[serverID] = mcpWireToolsToViews(toolsResult.Result.Tools)
		r.mu.Unlock()
	}

	r.mu.Lock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount = 0
	h.LastCheck = time.Now()
	if elapsed > 5*time.Second {
		h.Status = mcpHealthStatusSlow
	} else {
		h.Status = mcpHealthStatusHealthy
	}
	r.mu.Unlock()
	if strict && r.app != nil {
		// A successful checked probe is the authoritative recovery event for a
		// previously persisted marketplace runtime failure. Clearing this state
		// also lets manual/background health checks repair a failed install.
		if stateErr := r.app.markMCPRuntimeSyncReady(serverID); stateErr != nil {
			return fmt.Errorf("MCP runtime readiness state cleanup failed: %w", stateErr)
		}
	}
	return nil
}

// validateMCPJSONRPCSuccess rejects protocol-level errors and malformed
// responses. HTTP 200 alone is not evidence that an MCP method succeeded:
// JSON-RPC servers may return an error object with status 200.
func validateMCPJSONRPCSuccess(payload []byte, method string) error {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("MCP %s returned invalid JSON: %w", method, err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return fmt.Errorf("MCP %s returned a JSON-RPC error", method)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("MCP %s response is missing result", method)
	}
	return nil
}

func validateMCPToolsListResult(payload []byte) error {
	var envelope struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("MCP tools/list returned invalid JSON: %w", err)
	}
	if len(envelope.Result.Tools) == 0 || string(envelope.Result.Tools) == "null" {
		return fmt.Errorf("MCP tools/list response is missing tools")
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(envelope.Result.Tools, &tools); err != nil {
		return fmt.Errorf("MCP tools/list tools is not an array: %w", err)
	}
	return nil
}

func (r *MCPRegistry) recordFailure(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount++
	h.LastCheck = time.Now()
	if h.FailCount >= 3 {
		h.Status = mcpHealthStatusUnavailable
	} else {
		h.Status = mcpHealthStatusSlow
	}
}

func (r *MCPRegistry) recordSuccess(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.getOrCreateHealth(serverID)
	h.FailCount = 0
	h.LastCheck = time.Now()
	h.Status = mcpHealthStatusHealthy
}

func (r *MCPRegistry) getOrCreateHealth(serverID string) *mcpHealthState {
	h, ok := r.health[serverID]
	if !ok {
		h = &mcpHealthState{Status: mcpHealthStatusUnknown}
		r.health[serverID] = h
	}
	return h
}

// truncateMCPBody returns a short preview of a response body for error messages.
func truncateMCPBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// RegisterAutoDiscovered registers an auto-discovered MCP Server.
// If a manually registered server with the same ID already exists, the
// auto-discovered entry is silently ignored to preserve manual configuration.
func (r *MCPRegistry) RegisterAutoDiscovered(entry corelib.MCPServerEntry, source corelib.MCPServerSource) error {
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
			if s.Source == corelib.MCPSourceManual || s.Source == "" {
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

// ProbeAllUnknownAsync kicks off background health checks for every registered
// server whose health status is still "unknown". Each check runs in its own
// goroutine with a 15-second timeout. The method returns immediately — callers
// should poll ListServers to pick up results.
func (r *MCPRegistry) ProbeAllUnknownAsync() {
	r.mu.RLock()
	servers := r.loadServers()
	type probeTarget struct {
		id     string
		strict bool
	}
	var toCheck []probeTarget
	for _, s := range servers {
		h, ok := r.health[s.ID]
		if !ok || normalizeMCPHealthStatus(h.Status) == mcpHealthStatusUnknown {
			toCheck = append(toCheck, probeTarget{id: s.ID, strict: s.Source == corelib.MCPSourceMarket && s.Capability != nil})
		}
	}
	r.mu.RUnlock()

	if len(toCheck) == 0 {
		return
	}

	for _, target := range toCheck {
		go func(target probeTarget) {
			probeCtx, cancel := context.WithTimeout(context.Background(), mcpBackgroundProbeTimeout)
			defer cancel()
			var err error
			if target.strict {
				err = r.HealthCheckStrictContext(probeCtx, target.id)
			} else {
				err = r.HealthCheckContext(probeCtx, target.id)
			}
			if err != nil {
				log.Printf("[MCPRegistry] probe failed for %s: %v", target.id, err)
				if target.strict && r.app != nil {
					if stateErr := r.app.recordMCPRuntimeSyncFailure(target.id, "http", "", err); stateErr != nil {
						log.Printf("[MCPRegistry] persist probe failure for %s: %v", target.id, stateErr)
					}
				}
			}
		}(target)
	}
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
					managed := s.Source == corelib.MCPSourceMarket && s.Capability != nil
					// Bound every background probe independently. Reusing the loop
					// context without a per-request deadline lets a slow remote MCP
					// hold the health round open for the full HTTP client timeout and
					// can delay later readiness repairs indefinitely.
					probeCtx, cancel := context.WithTimeout(ctx, mcpBackgroundProbeTimeout)
					var err error
					if managed {
						err = r.HealthCheckStrictContext(probeCtx, s.ID)
					} else {
						err = r.HealthCheckContext(probeCtx, s.ID)
					}
					cancel()
					if err != nil {
						log.Printf("[MCPRegistry] health check failed for %s: %v", s.ID, err)
						if managed && r.app != nil {
							if stateErr := r.app.recordMCPRuntimeSyncFailure(s.ID, "http", "", err); stateErr != nil {
								log.Printf("[MCPRegistry] persist health failure for %s: %v", s.ID, stateErr)
							}
						}
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
	var kept []corelib.MCPServerEntry
	for _, s := range servers {
		h, ok := r.health[s.ID]
		if ok && h.FailCount >= 3 && s.Source != corelib.MCPSourceManual && s.Source != "" {
			// Auto-discovered server with >= 3 consecutive failures — remove it.
			delete(r.health, s.ID)
			delete(r.toolsCache, s.ID)
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
// Returns cached tools if available from a prior health check; otherwise
// fetches fresh and updates the cache.
func (r *MCPRegistry) GetServerTools(serverID string) []MCPToolView {
	if r == nil || r.app == nil {
		return nil
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	// A marketplace-managed server is executable only after a durable strict
	// initialize + tools/list probe. Do this check before consulting the cache:
	// a stale cache must not make a newly committed or failed runtime appear
	// ready through the Wails/IM inspection path.
	target, err := r.findServer(serverID)
	if err != nil || target == nil {
		// Do not expose a cache entry when the authoritative configuration cannot
		// be read or the server no longer exists.
		return nil
	}
	if target.Source == corelib.MCPSourceMarket && target.Capability != nil &&
		r.app != nil && r.app.mcpRuntimeSyncPending(serverID) {
		return nil
	}
	// Check cache first.
	r.mu.RLock()
	if cached, ok := r.toolsCache[serverID]; ok && len(cached) > 0 {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Ensure session for Streamable HTTP servers.
	_ = r.ensureSession(target)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	parsed, err := r.doMCPRoundTrip(target, reqBody)
	if err != nil {
		log.Printf("[MCPRegistry] GetServerTools failed for %s: %v", serverID, err)
		return nil
	}

	// Use mcpWireToolView with camelCase "inputSchema" to match MCP wire format.
	var result struct {
		Result struct {
			Tools []mcpWireToolView `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(parsed, &result); err != nil {
		log.Printf("[MCPRegistry] GetServerTools JSON unmarshal error for %s: %v", serverID, err)
		return nil
	}

	views := mcpWireToolsToViews(result.Result.Tools)

	// Update cache.
	r.mu.Lock()
	r.toolsCache[serverID] = views
	r.mu.Unlock()

	return views
}

// CachedServerTools returns the last successful tools/list observation without
// performing network I/O. Semantic routing uses this to publish one lifecycle
// snapshot; discovery/connection belongs to the MCP lifecycle worker, not to
// an individual user request. Callers receive a copy so a catalog projection
// cannot mutate registry state.
func (r *MCPRegistry) CachedServerTools(serverID string) ([]MCPToolView, bool) {
	if r == nil {
		return nil, false
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, false
	}
	r.mu.RLock()
	tools, ok := r.toolsCache[serverID]
	r.mu.RUnlock()
	// An observed server is allowed to expose zero tools. Presence of the cache
	// entry, not a non-zero length, distinguishes that complete observation from
	// an unobserved server whose lifecycle discovery is still pending.
	if !ok {
		return nil, false
	}
	return append([]MCPToolView(nil), tools...), true
}

func (r *MCPRegistry) warmServerTools(serverID string, wait time.Duration, onDone func(error)) error {
	done := make(chan error, 1)
	go func() {
		err := r.HealthCheck(serverID)
		if onDone != nil {
			onDone(err)
		}
		done <- err
	}()
	if wait <= 0 {
		return <-done
	}
	select {
	case err := <-done:
		return err
	case <-time.After(wait):
		return errMCPToolSyncPending{wait: wait}
	}
}

func (r *MCPRegistry) warmServerToolsAsync(serverID string, onDone func(error)) {
	go func() {
		err := r.HealthCheck(serverID)
		if onDone != nil {
			onDone(err)
		}
	}()
}

type errMCPToolSyncPending struct {
	wait time.Duration
}

func (e errMCPToolSyncPending) Error() string {
	return fmt.Sprintf("tool sync still running after %s", e.wait)
}

func logMCPToolSyncResult(action, serverID string, err error) {
	if err == nil {
		return
	}
	if _, pending := err.(errMCPToolSyncPending); pending {
		log.Printf("[MCPRegistry] %s for %s pending: %v", action, serverID, err)
		return
	}
	log.Printf("[MCPRegistry] %s for %s failed: %v", action, serverID, err)
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
func (a *App) RegisterMCPServer(server corelib.MCPServerEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("call_mcp_tool", map[string]interface{}{"action": "register_server", "server_id": server.ID, "endpoint_url": server.EndpointURL}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": server.EndpointURL}); !ok {
		return fmt.Errorf("%s", reason)
	}
	serverID, err := a.mcpRegistry.register(server, false)
	if err != nil {
		return err
	}
	a.warmMCPServerToolsAndRefresh(serverID, "immediate tool sync")
	return nil
}

// UpdateMCPServer updates an existing MCP Server (Wails binding).
func (a *App) UpdateMCPServer(server corelib.MCPServerEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("call_mcp_tool", map[string]interface{}{"action": "update_server", "server_id": server.ID, "endpoint_url": server.EndpointURL}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": server.EndpointURL}); !ok {
		return fmt.Errorf("%s", reason)
	}
	if err := a.mcpRegistry.Update(server); err != nil {
		return err
	}
	a.recordMarketplaceMCPSecretBinding(server)
	// Managed updates start their strict probe inside registry.Update after the
	// durable pending marker is written. Avoid launching a second concurrent
	// probe here; ordinary/manual servers retain the historical warm path.
	if updated, findErr := a.mcpRegistry.findServer(server.ID); findErr == nil && updated != nil && updated.Source == corelib.MCPSourceMarket && updated.Capability != nil {
		a.invalidateIMToolCaches()
	} else {
		a.warmMCPServerToolsAndRefresh(server.ID, "immediate tool sync after update")
	}
	return nil
}

// UnregisterMCPServer removes an MCP Server by ID (Wails binding).
func (a *App) UnregisterMCPServer(serverID string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("call_mcp_tool", map[string]interface{}{"action": "unregister_server", "server_id": serverID}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	// Check managed deployment protection: forced-delivery MCP cannot be deleted.
	if entry, err := a.mcpRegistry.findServer(serverID); err == nil && entry != nil {
		if a.mcpRegistry.isManagedCapability(entry) {
			return fmt.Errorf("此 MCP 为企业强制下发，不可删除")
		}
	}
	if err := a.mcpRegistry.Unregister(serverID); err != nil {
		return err
	}
	a.invalidateIMToolCaches()
	return nil
}

func (a *App) warmMCPServerToolsAndRefresh(serverID, action string) {
	a.invalidateIMToolCaches()
	if a == nil || a.mcpRegistry == nil || strings.TrimSpace(serverID) == "" {
		return
	}
	strict := false
	if entry, findErr := a.mcpRegistry.findServer(serverID); findErr == nil && entry != nil {
		strict = entry.Source == corelib.MCPSourceMarket && entry.Capability != nil
	}
	probe := func() error {
		if strict {
			return a.mcpRegistry.HealthCheckStrictContext(context.Background(), serverID)
		}
		return a.mcpRegistry.HealthCheck(serverID)
	}
	done := make(chan error, 1)
	go func() {
		err := probe()
		if strict {
			if err != nil {
				if stateErr := a.recordMCPRuntimeSyncFailure(serverID, "http", "", err); stateErr != nil {
					log.Printf("[MCPRegistry] persist runtime failure for %s: %v", serverID, stateErr)
				}
			} else if stateErr := a.markMCPRuntimeSyncReady(serverID); stateErr != nil {
				log.Printf("[MCPRegistry] clear runtime state for %s: %v", serverID, stateErr)
			}
		}
		a.invalidateIMToolCaches()
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		err = errMCPToolSyncPending{wait: 3 * time.Second}
	}
	if err != nil {
		logMCPToolSyncResult(action, serverID, err)
	}
}

func (a *App) invalidateIMToolCaches() {
	if a == nil {
		return
	}
	clearHandler := func(h *IMMessageHandler) {
		if h == nil {
			return
		}
		h.toolsMu.Lock()
		h.cachedTools = nil
		h.cachedToolDefGen = nil
		h.toolsCacheTime = time.Time{}
		h.toolsMu.Unlock()
	}
	clearHandler(a.imHandler)
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		a.remoteSessions.hubClient.imHandlerMu.Lock()
		clearHandler(a.remoteSessions.hubClient.imHandler)
		a.remoteSessions.hubClient.imHandlerMu.Unlock()
	}
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
	entry, findErr := a.mcpRegistry.findServer(serverID)
	if findErr != nil {
		return findErr
	}
	if entry != nil {
		if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": entry.EndpointURL}); !ok {
			return fmt.Errorf("%s", reason)
		}
		managed := entry.Source == corelib.MCPSourceMarket && entry.Capability != nil
		if managed {
			// A user-initiated check is an explicit operator action. It may
			// reopen a terminal needs_review record, but the probe itself must
			// still satisfy the strict initialize + tools/list contract before
			// readiness is restored.
			if err := a.resetMCPRuntimeSyncPending(serverID, "http"); err != nil {
				return fmt.Errorf("reset MCP runtime sync state: %w", err)
			}
			if err := a.mcpRegistry.HealthCheckStrictContext(context.Background(), serverID); err != nil {
				if stateErr := a.recordMCPRuntimeSyncFailure(serverID, "http", "", err); stateErr != nil {
					return fmt.Errorf("MCP health check failed: %v; persist runtime state: %w", err, stateErr)
				}
				return err
			}
			return nil
		}
	}
	return a.mcpRegistry.HealthCheck(serverID)
}

// RetryMCPRuntimeSync explicitly re-arms and probes one marketplace-managed
// MCP runtime. Runtime blockers are intentionally not cleared by a generic
// refresh: an operator must confirm the exact server ID, and readiness is
// restored only after the strict initialize + tools/list (or local stdio
// start + discovery) contract succeeds.
func (a *App) RetryMCPRuntimeSync(serverID string, confirm bool) error {
	serverID = strings.TrimSpace(serverID)
	if !confirm {
		return fmt.Errorf("MCP runtime retry requires explicit confirmation")
	}
	if serverID == "" {
		return fmt.Errorf("MCP server ID is required")
	}
	if a == nil || a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{
		"action": "retry_mcp_runtime_sync", "server_id": serverID,
	}); err != nil {
		return err
	}
	if entry, err := a.mcpRegistry.findServer(serverID); err == nil && entry != nil {
		if entry.Source != corelib.MCPSourceMarket || entry.Capability == nil {
			return fmt.Errorf("MCP runtime retry is only available for managed marketplace servers")
		}
		if err := a.resetMCPRuntimeSyncPending(serverID, "http"); err != nil {
			log.Printf("[MCPRegistry] reset manual runtime retry state for %s: %v", serverID, err)
			return fmt.Errorf("MCP runtime retry could not initialize runtime state")
		}
		if err := a.mcpRegistry.HealthCheckStrictContext(context.Background(), serverID); err != nil {
			if stateErr := a.recordMCPRuntimeSyncFailure(serverID, "http", "", err); stateErr != nil {
				log.Printf("[MCPRegistry] persist manual runtime retry failure for %s: %v", serverID, stateErr)
				return fmt.Errorf("MCP runtime retry failed; runtime state could not be persisted")
			}
			return fmt.Errorf("MCP runtime retry failed: %s", summarizeMCPRuntimeSyncError(err))
		}
		return nil
	} else if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		return err
	}
	local := a.mcpRegistry.findLocalServer(serverID)
	if local == nil || local.Source != corelib.MCPSourceMarket || local.Capability == nil {
		return fmt.Errorf("managed MCP server %q not found", serverID)
	}
	if local.Disabled {
		return fmt.Errorf("local MCP server %q is disabled", serverID)
	}
	if err := a.resetMCPRuntimeSyncPending(serverID, "stdio"); err != nil {
		log.Printf("[MCPRegistry] reset manual runtime retry state for %s: %v", serverID, err)
		return fmt.Errorf("MCP runtime retry could not initialize runtime state")
	}
	a.ensureLocalMCPManager()
	if a.localMCPManager == nil {
		return fmt.Errorf("local MCP manager not initialized")
	}
	// Force a fresh process/handshake so an already-running stale client cannot
	// satisfy the newly re-armed readiness cycle without checked discovery.
	a.localMCPManager.StopServer(serverID)
	syncErr := a.localMCPManager.SyncFromConfigChecked()
	if a.localMCPManager.IsRunning(serverID) {
		if state, ok := loadMCPRuntimeSyncState(serverID); ok && state.Status == "ready" {
			return nil
		}
	}
	if syncErr != nil {
		return fmt.Errorf("MCP runtime retry failed: %s", summarizeMCPRuntimeSyncError(syncErr))
	}
	return fmt.Errorf("local MCP runtime %q is not ready after retry", serverID)
}

// MCPEndpointTestResult holds the result of probing an arbitrary MCP endpoint.
type MCPEndpointTestResult struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Tools   []MCPToolView `json:"tools"`
	Latency int64         `json:"latency_ms"`
}

// TestMCPEndpoint probes an arbitrary MCP endpoint (without requiring registration)
// to verify connectivity and list available tools. Used by the edit form's "Test Connection" button.
func (a *App) TestMCPEndpoint(endpointURL string, authType string, authSecret string, headers map[string]string) MCPEndpointTestResult {
	if a.mcpRegistry == nil {
		return MCPEndpointTestResult{Message: "MCP registry not initialized"}
	}
	if endpointURL == "" {
		return MCPEndpointTestResult{Message: "Endpoint URL is required"}
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": endpointURL}); !ok {
		return MCPEndpointTestResult{Message: reason}
	}

	// Build a temporary MCPServerEntry for the probe.
	tempEntry := &corelib.MCPServerEntry{
		ID:          "__test_probe__",
		Name:        "test-probe",
		EndpointURL: endpointURL,
		AuthType:    authType,
		AuthSecret:  authSecret,
		Headers:     headers,
	}

	return a.mcpRegistry.ProbeEndpoint(tempEntry)
}

// ProbeEndpoint tests connectivity to an arbitrary MCP server entry and returns
// the tool list. Does not modify any registry state.
func (r *MCPRegistry) ProbeEndpoint(target *corelib.MCPServerEntry) MCPEndpointTestResult {
	// Use the same bounded probe budget as background readiness checks.
	ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundProbeTimeout)
	defer cancel()

	endpointURL := strings.TrimRight(target.EndpointURL, "/")
	start := time.Now()

	// Step 1: Try initialize handshake (some servers require it).
	initBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "maclaw",
				"version": "1.0.0",
			},
		},
	}

	initData, _ := json.Marshal(initBody)
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(initData))
	if err != nil {
		return MCPEndpointTestResult{Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	setAuthHeader(initReq, target)

	var sessionID string
	initResp, err := r.client.Do(initReq)
	if err == nil {
		if sid := initResp.Header.Get("Mcp-Session-Id"); sid != "" {
			sessionID = sid
		}
		initResp.Body.Close()
	}
	// Initialize failure is non-fatal — some servers don't support it.

	// Step 2: Call tools/list to verify connectivity and get tool list.
	toolsBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	toolsData, _ := json.Marshal(toolsBody)
	toolsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(toolsData))
	if err != nil {
		return MCPEndpointTestResult{Message: fmt.Sprintf("Failed to create request: %v", err), Latency: time.Since(start).Milliseconds()}
	}
	toolsReq.Header.Set("Content-Type", "application/json")
	toolsReq.Header.Set("Accept", "application/json, text/event-stream")
	setAuthHeader(toolsReq, target)
	if sessionID != "" {
		toolsReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	toolsResp, err := r.client.Do(toolsReq)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		msg := fmt.Sprintf("Connection failed: %v", err)
		if ctx.Err() == context.DeadlineExceeded {
			msg = "Connection timed out (15s)"
		}
		return MCPEndpointTestResult{Message: msg, Latency: latency}
	}
	defer toolsResp.Body.Close()

	if toolsResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(toolsResp.Body, 4*1024))
		return MCPEndpointTestResult{
			Message: fmt.Sprintf("HTTP %d: %s", toolsResp.StatusCode, truncateMCPBody(errBody)),
			Latency: latency,
		}
	}

	ct := toolsResp.Header.Get("Content-Type")
	parsed, err := corelib.ParseMCPResponse(toolsResp.Body, ct, 256*1024)
	if err != nil {
		return MCPEndpointTestResult{Message: fmt.Sprintf("Failed to parse response: %v", err), Latency: latency}
	}

	// Parse tool list from response.
	var toolsResult struct {
		Result struct {
			Tools []mcpWireToolView `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(parsed, &toolsResult); err != nil {
		return MCPEndpointTestResult{
			Success: true,
			Message: "Connected successfully but could not parse tool list",
			Latency: latency,
		}
	}

	tools := mcpWireToolsToViews(toolsResult.Result.Tools)
	msg := fmt.Sprintf("Connected successfully. %d tool(s) available.", len(tools))
	return MCPEndpointTestResult{
		Success: true,
		Message: msg,
		Tools:   tools,
		Latency: latency,
	}
}

// ProbeMCPServers kicks off background health probes for all remote MCP
// servers that have "unknown" status, then immediately returns the current
// server list (status may still be "unknown" at this point). The frontend
// should poll ListMCPServers to pick up the results as they arrive.
func (a *App) ProbeMCPServers() []MCPServerView {
	if a.mcpRegistry == nil {
		return nil
	}
	a.mcpRegistry.ProbeAllUnknownAsync()
	return a.mcpRegistry.ListServers()
}

// ─── Local (stdio) MCP Server support ───────────────────────────────────────

// loadLocalServers reads local MCP server entries from config.
func (r *MCPRegistry) loadLocalServers() []corelib.LocalMCPServerEntry {
	cfg, err := r.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.LocalMCPServers
}

// saveLocalServers persists local MCP server entries to config.
func (r *MCPRegistry) saveLocalServers(servers []corelib.LocalMCPServerEntry) error {
	return r.app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.LocalMCPServers = servers
	})
}

// RegisterLocal adds a new local MCP server entry.
func (r *MCPRegistry) RegisterLocal(entry corelib.LocalMCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	for _, existing := range servers {
		if existing.ID == entry.ID {
			return fmt.Errorf("local MCP server with id %q already exists", entry.ID)
		}
	}
	servers = append(servers, entry)
	if err := r.saveLocalServers(servers); err != nil {
		return err
	}
	if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil && r.app != nil {
		if err := r.app.resetMCPRuntimeSyncPending(entry.ID, "stdio"); err != nil {
			return fmt.Errorf("MCP config saved but runtime state initialization failed: %w", err)
		}
	}
	return nil
}

// UpdateLocal updates an existing local MCP server entry.
func (r *MCPRegistry) UpdateLocal(entry corelib.LocalMCPServerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	servers := r.loadLocalServers()
	for i, s := range servers {
		if s.ID == entry.ID {
			_, hadRuntimeState := loadMCPRuntimeSyncState(entry.ID)
			managedRuntime := s.Source == corelib.MCPSourceMarket && s.Capability != nil
			entry.CreatedAt = s.CreatedAt
			configChanged := !reflect.DeepEqual(s, entry)
			servers[i] = entry
			if err := r.saveLocalServers(servers); err != nil {
				return err
			}
			newManagedRuntime := entry.Source == corelib.MCPSourceMarket && entry.Capability != nil
			if configChanged && r.app != nil {
				switch {
				case newManagedRuntime:
					// A changed managed configuration must establish a fresh,
					// checked-readiness cycle before execution is admitted.
					if err := r.app.resetMCPRuntimeSyncPending(entry.ID, "stdio"); err != nil {
						return fmt.Errorf("persist MCP runtime sync pending state: %w", err)
					}
				case hadRuntimeState || managedRuntime:
					// Converting a managed entry to a manual one must remove the
					// marketplace-only blocker; otherwise a failed old probe would
					// unexpectedly block the manual server after the update.
					if err := clearMCPRuntimeSyncState(entry.ID); err != nil {
						return fmt.Errorf("clear MCP runtime sync state: %w", err)
					}
				}
			}
			return nil
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
			if err := r.saveLocalServers(servers); err != nil {
				return err
			}
			if r.app != nil {
				if err := clearMCPRuntimeSyncState(serverID); err != nil {
					return fmt.Errorf("clear MCP runtime sync state: %w", err)
				}
			}
			return nil
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
func (r *MCPRegistry) ListLocalServers() []corelib.LocalMCPServerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loadLocalServers()
}

// findLocalServer looks up a local MCP server by ID under RLock and returns a pointer (nil if not found).
func (r *MCPRegistry) findLocalServer(serverID string) *corelib.LocalMCPServerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.loadLocalServers() {
		if s.ID == serverID {
			cp := s
			return &cp
		}
	}
	return nil
}

// ─── Wails bindings for Local MCP Servers ───────────────────────────────────

// ListLocalMCPServers returns all local (stdio) MCP server configs (Wails binding).
func (a *App) ListLocalMCPServers() []LocalMCPServerView {
	if a.mcpRegistry == nil {
		return nil
	}
	entries := a.mcpRegistry.ListLocalServers()
	// Local and remote views share the same durable runtime-sync blocker. Local
	// entries are projected here so a managed Stdio failure remains visible
	// after restart and the management UI cannot imply readiness from config
	// alone.
	views := make([]LocalMCPServerView, 0, len(entries))
	for _, e := range entries {
		managed := false
		if e.Source == corelib.MCPSourceMarket && e.Capability != nil {
			managed = a.isCapabilityManagedDeployment(e.Capability.CapabilityID)
		}
		view := LocalMCPServerView{
			ID:         e.ID,
			Name:       e.Name,
			Command:    e.Command,
			Args:       e.Args,
			Env:        e.Env,
			Disabled:   e.Disabled,
			AutoStart:  e.AutoStart,
			CreatedAt:  e.CreatedAt,
			Source:     e.Source,
			Capability: e.Capability,
			Managed:    managed,
		}
		if syncState, ok := loadMCPRuntimeSyncState(e.ID); ok {
			if syncState.Status != "ready" {
				view.RuntimeSyncStatus = syncState.Status
				view.RuntimeSyncAttempts = syncState.Attempts
				view.RuntimeSyncLastError = syncState.LastError
				view.RuntimeSyncNextRetryAt = syncState.NextRetryAt
			}
		} else if managed && a.mcpRuntimeSyncRequired(e.ID) {
			view.RuntimeSyncStatus = "pending"
			view.RuntimeSyncLastError = "runtime synchronization pending"
		}
		views = append(views, view)
	}
	return views
}

// RegisterLocalMCPServer adds a new local MCP server config (Wails binding).
func (a *App) RegisterLocalMCPServer(server corelib.LocalMCPServerEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.Join(append([]string{server.Command}, server.Args...), " "), "action": "register_local_mcp", "server_id": server.ID}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("bash", map[string]interface{}{"command": strings.Join(append([]string{server.Command}, server.Args...), " ")}); !ok {
		return fmt.Errorf("%s", reason)
	}
	return a.mcpRegistry.RegisterLocal(server)
}

// UpdateLocalMCPServer updates an existing local MCP server config (Wails binding).
func (a *App) UpdateLocalMCPServer(server corelib.LocalMCPServerEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.Join(append([]string{server.Command}, server.Args...), " "), "action": "update_local_mcp", "server_id": server.ID}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("bash", map[string]interface{}{"command": strings.Join(append([]string{server.Command}, server.Args...), " ")}); !ok {
		return fmt.Errorf("%s", reason)
	}
	return a.mcpRegistry.UpdateLocal(server)
}

// UnregisterLocalMCPServer removes a local MCP server config by ID (Wails binding).
func (a *App) UnregisterLocalMCPServer(serverID string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "unregister local mcp " + strings.TrimSpace(serverID), "action": "unregister_local_mcp", "server_id": serverID}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	// Check managed deployment protection for local MCP servers.
	if entry := a.mcpRegistry.findLocalServer(serverID); entry != nil {
		if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil && a.isCapabilityManagedDeployment(entry.Capability.CapabilityID) {
			return fmt.Errorf("此 MCP 为企业强制下发，不可删除")
		}
	}
	return a.mcpRegistry.UnregisterLocal(serverID)
}

// SyncLocalMCPServers triggers the local MCP manager to re-read config
// and start/stop processes accordingly (Wails binding).
func (a *App) SyncLocalMCPServers() error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "sync local mcp servers", "action": "sync_local_mcp"}); err != nil {
		return err
	}
	a.ensureLocalMCPManager()
	if a.localMCPManager == nil {
		return fmt.Errorf("local MCP manager not initialized")
	}
	// This is a foreground Wails operation: surface startup/discovery failure
	// instead of reporting success while the configured runtime is unavailable.
	if err := a.localMCPManager.SyncFromConfigChecked(); err != nil {
		return err
	}
	// This is an explicit checked foreground operation. Only after the whole
	// configured local runtime has reconciled successfully may it clear durable
	// marketplace runtime blockers; background best-effort sync deliberately
	// does not have this authority.
	var blocked []string
	for _, entry := range a.mcpRegistry.ListLocalServers() {
		if entry.Disabled {
			continue
		}
		// A checked sync may legitimately leave a managed runtime in
		// backoff/needs_review. Do not turn a successful no-op sync into a
		// readiness marker for a server that was intentionally not started.
		if state, ok := loadMCPRuntimeSyncState(entry.ID); ok && state.Status != "ready" {
			if entry.Source == corelib.MCPSourceMarket && entry.Capability != nil {
				blocked = append(blocked, entry.ID)
			}
			continue
		}
		if err := a.markMCPRuntimeSyncReady(entry.ID); err != nil {
			return fmt.Errorf("persist local MCP runtime readiness for %s: %w", entry.ID, err)
		}
	}
	if len(blocked) > 0 {
		return fmt.Errorf("local MCP runtime synchronization remains blocked for: %s", strings.Join(blocked, ", "))
	}
	return nil
}

// SetLocalMCPAutoStart sets the AutoStart flag for a local MCP server and
// triggers a sync. When enabled=true the server starts immediately and will
// auto-start on future app launches. When enabled=false the server stays
// governed by Disabled for the current run, but will not auto-start on the
// next app launch.
func (a *App) SetLocalMCPAutoStart(serverID string, enabled bool) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "set local mcp autostart " + strings.TrimSpace(serverID), "action": "set_local_mcp_autostart", "server_id": serverID, "enabled": enabled}); err != nil {
		return err
	}
	if a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if err := a.mcpRegistry.SetLocalAutoStart(serverID, enabled); err != nil {
		return err
	}
	// Sync immediately so the server starts/stops now.
	a.ensureLocalMCPManager()
	if a.localMCPManager != nil {
		if err := a.localMCPManager.SyncFromConfigChecked(); err != nil {
			return fmt.Errorf("local MCP runtime sync failed: %w", err)
		}
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

	a.ensureLocalMCPManager()
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
