package agentservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type MCPServerView struct {
	ID            string                  `json:"id"`
	Kind          string                  `json:"kind"`
	Name          string                  `json:"name"`
	EndpointURL   string                  `json:"endpoint_url,omitempty"`
	AuthType      string                  `json:"auth_type,omitempty"`
	HasAuthSecret bool                    `json:"has_auth_secret,omitempty"`
	HeaderNames   []string                `json:"header_names,omitempty"`
	Command       string                  `json:"command,omitempty"`
	Args          []string                `json:"args,omitempty"`
	EnvKeys       []string                `json:"env_keys,omitempty"`
	HasEnv        bool                    `json:"has_env,omitempty"`
	Disabled      bool                    `json:"disabled,omitempty"`
	AutoStart     bool                    `json:"auto_start,omitempty"`
	Source        corelib.MCPServerSource `json:"source,omitempty"`
	Running       bool                    `json:"running"`
	HealthStatus  MCPHealthStatus         `json:"health_status"`
	FailCount     int                     `json:"fail_count,omitempty"`
	LastCheckAt   *time.Time              `json:"last_check_at,omitempty"`
	CreatedAt     string                  `json:"created_at,omitempty"`
	Tools         []MCPToolView           `json:"tools,omitempty"`
}

type MCPServerCreateInput struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	EndpointURL string            `json:"endpoint_url,omitempty"`
	AuthType    string            `json:"auth_type,omitempty"`
	AuthSecret  string            `json:"auth_secret,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	AutoStart   bool              `json:"auto_start,omitempty"`
}

type MCPServerUpdateInput struct {
	Name        *string           `json:"name,omitempty"`
	EndpointURL *string           `json:"endpoint_url,omitempty"`
	AuthType    *string           `json:"auth_type,omitempty"`
	AuthSecret  *string           `json:"auth_secret,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     *string           `json:"command,omitempty"`
	Args        *[]string         `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Disabled    *bool             `json:"disabled,omitempty"`
	AutoStart   *bool             `json:"auto_start,omitempty"`
}

type mcpServiceRuntime struct {
	mu    sync.Mutex
	users map[string]*userMCPRuntime
}

type userMCPRuntime struct {
	mu     sync.Mutex
	remote map[string]*remoteMCPRuntime
	local  map[string]*localMCPClient
}

type remoteMCPRuntime struct {
	healthStatus MCPHealthStatus
	failCount    int
	lastCheckAt  time.Time
	sessionID    string
	sessionAt    time.Time
	tools        []MCPToolView
}

var globalMCPRuntimes sync.Map

func runtimeForService(s *Service) *mcpServiceRuntime {
	if s == nil {
		return &mcpServiceRuntime{users: map[string]*userMCPRuntime{}}
	}
	if v, ok := globalMCPRuntimes.Load(s); ok {
		return v.(*mcpServiceRuntime)
	}
	rt := &mcpServiceRuntime{users: map[string]*userMCPRuntime{}}
	actual, _ := globalMCPRuntimes.LoadOrStore(s, rt)
	return actual.(*mcpServiceRuntime)
}

func (rt *mcpServiceRuntime) user(key string) *userMCPRuntime {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if item, ok := rt.users[key]; ok {
		return item
	}
	item := &userMCPRuntime{remote: map[string]*remoteMCPRuntime{}, local: map[string]*localMCPClient{}}
	rt.users[key] = item
	return item
}

func (s *Service) ListMCPServers(ctx context.Context, p Principal) ([]MCPServerView, error) {
	_ = ctx
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	return buildMCPViews(cfg.AppConfig, runtime), nil
}

func (s *Service) GetMCPServer(ctx context.Context, p Principal, serverID string) (*MCPServerView, error) {
	_ = ctx
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	view, _, _, err := s.lookupMCPServer(p, cfg.AppConfig, serverID)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (s *Service) CreateMCPServer(ctx context.Context, p Principal, in MCPServerCreateInput) (*MCPServerView, error) {
	_ = ctx
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := s.now().UTC().Format(time.RFC3339)
	switch kind {
	case "remote":
		endpoint := strings.TrimSpace(in.EndpointURL)
		if endpoint == "" {
			return nil, fmt.Errorf("endpoint_url is required")
		}
		authType := normalizeMCPAuthType(in.AuthType)
		if authType == "" {
			return nil, fmt.Errorf("invalid auth_type")
		}
		entry := corelib.MCPServerEntry{
			ID:          NewID("mcp_remote"),
			Name:        name,
			EndpointURL: endpoint,
			AuthType:    authType,
			AuthSecret:  strings.TrimSpace(in.AuthSecret),
			Headers:     cleanStringMap(in.Headers),
			CreatedAt:   now,
			Source:      corelib.MCPSourceManual,
		}
		cfg.AppConfig.MCPServers = append(cfg.AppConfig.MCPServers, entry)
		if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
			return nil, err
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.remote.created", ResourceType: "mcp_server", ResourceID: entry.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
		return s.GetMCPServer(ctx, p, entry.ID)
	case "local":
		command := strings.TrimSpace(in.Command)
		if command == "" {
			return nil, fmt.Errorf("command is required")
		}
		entry := corelib.LocalMCPServerEntry{
			ID:        NewID("mcp_local"),
			Name:      name,
			Command:   command,
			Args:      cloneStringSlice(in.Args),
			Env:       cleanStringMap(in.Env),
			Disabled:  in.Disabled,
			AutoStart: in.AutoStart,
			CreatedAt: now,
		}
		cfg.AppConfig.LocalMCPServers = append(cfg.AppConfig.LocalMCPServers, entry)
		if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
			return nil, err
		}
		if entry.AutoStart && !entry.Disabled {
			_, _ = s.StartMCPServer(ctx, p, entry.ID)
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.local.created", ResourceType: "mcp_server", ResourceID: entry.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
		return s.GetMCPServer(ctx, p, entry.ID)
	default:
		return nil, fmt.Errorf("kind must be remote or local")
	}
}

func (s *Service) UpdateMCPServer(ctx context.Context, p Principal, serverID string, in MCPServerUpdateInput) (*MCPServerView, error) {
	_ = ctx
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	var updatedKind string
	for i := range cfg.AppConfig.MCPServers {
		entry := &cfg.AppConfig.MCPServers[i]
		if entry.ID != serverID {
			continue
		}
		if in.Name != nil {
			entry.Name = strings.TrimSpace(*in.Name)
			if entry.Name == "" {
				return nil, fmt.Errorf("name is required")
			}
		}
		if in.EndpointURL != nil {
			entry.EndpointURL = strings.TrimSpace(*in.EndpointURL)
			if entry.EndpointURL == "" {
				return nil, fmt.Errorf("endpoint_url is required")
			}
		}
		if in.AuthType != nil {
			authType := normalizeMCPAuthType(*in.AuthType)
			if authType == "" {
				return nil, fmt.Errorf("invalid auth_type")
			}
			entry.AuthType = authType
		}
		if in.AuthSecret != nil {
			entry.AuthSecret = strings.TrimSpace(*in.AuthSecret)
		}
		if in.Headers != nil {
			entry.Headers = cleanStringMap(in.Headers)
		}
		updatedKind = "remote"
		break
	}
	for i := range cfg.AppConfig.LocalMCPServers {
		entry := &cfg.AppConfig.LocalMCPServers[i]
		if entry.ID != serverID {
			continue
		}
		if in.Name != nil {
			entry.Name = strings.TrimSpace(*in.Name)
			if entry.Name == "" {
				return nil, fmt.Errorf("name is required")
			}
		}
		if in.Command != nil {
			entry.Command = strings.TrimSpace(*in.Command)
			if entry.Command == "" {
				return nil, fmt.Errorf("command is required")
			}
		}
		if in.Args != nil {
			entry.Args = cloneStringSlice(*in.Args)
		}
		if in.Env != nil {
			entry.Env = cleanStringMap(in.Env)
		}
		if in.Disabled != nil {
			entry.Disabled = *in.Disabled
		}
		if in.AutoStart != nil {
			entry.AutoStart = *in.AutoStart
		}
		updatedKind = "local"
		break
	}
	if updatedKind == "" {
		return nil, ErrInstanceNotFound
	}
	if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
		return nil, err
	}
	if updatedKind == "local" {
		runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
		runtime.stopLocal(serverID)
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.updated", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return s.GetMCPServer(ctx, p, serverID)
}

func (s *Service) DeleteMCPServer(ctx context.Context, p Principal, serverID string) error {
	_ = ctx
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return err
	}
	found := false
	remote := make([]corelib.MCPServerEntry, 0, len(cfg.AppConfig.MCPServers))
	for _, entry := range cfg.AppConfig.MCPServers {
		if entry.ID == serverID {
			found = true
			continue
		}
		remote = append(remote, entry)
	}
	local := make([]corelib.LocalMCPServerEntry, 0, len(cfg.AppConfig.LocalMCPServers))
	for _, entry := range cfg.AppConfig.LocalMCPServers {
		if entry.ID == serverID {
			found = true
			continue
		}
		local = append(local, entry)
	}
	if !found {
		return ErrInstanceNotFound
	}
	cfg.AppConfig.MCPServers = remote
	cfg.AppConfig.LocalMCPServers = local
	if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
		return err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	runtime.stopLocal(serverID)
	runtime.clearRemote(serverID)
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.deleted", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return nil
}

func (s *Service) StartMCPServer(ctx context.Context, p Principal, serverID string) (*MCPServerView, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	_, remoteEntry, localEntry, err := s.lookupMCPServer(p, cfg.AppConfig, serverID)
	if err != nil {
		return nil, err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	if localEntry != nil {
		if localEntry.Disabled {
			return nil, fmt.Errorf("local MCP server is disabled")
		}
		if err := runtime.startLocal(ctx, *localEntry); err != nil {
			return nil, err
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.local.started", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
		return s.GetMCPServer(ctx, p, serverID)
	}
	if err := runtime.checkRemote(*remoteEntry); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.remote.session_started", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
	return s.GetMCPServer(ctx, p, serverID)
}

func (s *Service) StopMCPServer(ctx context.Context, p Principal, serverID string) (*MCPServerView, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	view, remoteEntry, localEntry, err := s.lookupMCPServer(p, cfg.AppConfig, serverID)
	if err != nil {
		return nil, err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	if localEntry != nil {
		runtime.stopLocal(serverID)
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.local.stopped", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
		return s.GetMCPServer(ctx, p, serverID)
	}
	if remoteEntry != nil {
		runtime.clearRemote(serverID)
		view.HealthStatus = MCPHealthUnknown
		view.Running = false
		view.LastCheckAt = nil
		view.Tools = nil
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "mcp.remote.session_stopped", ResourceType: "mcp_server", ResourceID: serverID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID})
		return view, nil
	}
	return nil, ErrInstanceNotFound
}

func (s *Service) CheckMCPServer(ctx context.Context, p Principal, serverID string) (*MCPServerView, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	_, remoteEntry, localEntry, err := s.lookupMCPServer(p, cfg.AppConfig, serverID)
	if err != nil {
		return nil, err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	if localEntry != nil {
		if !runtime.isLocalRunning(serverID) {
			return s.GetMCPServer(ctx, p, serverID)
		}
		if _, err := runtime.localTools(serverID); err != nil {
			return nil, err
		}
		return s.GetMCPServer(ctx, p, serverID)
	}
	if err := runtime.checkRemote(*remoteEntry); err != nil {
		return nil, err
	}
	return s.GetMCPServer(ctx, p, serverID)
}

func (s *Service) GetMCPServerTools(ctx context.Context, p Principal, serverID string) ([]MCPToolView, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	_, remoteEntry, localEntry, err := s.lookupMCPServer(p, cfg.AppConfig, serverID)
	if err != nil {
		return nil, err
	}
	runtime := runtimeForService(s).user(composite(p.TenantID, p.UserID))
	if localEntry != nil {
		return runtime.localTools(serverID)
	}
	if err := runtime.checkRemote(*remoteEntry); err != nil {
		return nil, err
	}
	view, err := s.GetMCPServer(ctx, p, serverID)
	if err != nil {
		return nil, err
	}
	return view.Tools, nil
}

func (s *Service) requireUserConfigForMCP(p Principal) (UserConfig, error) {
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return UserConfig{}, err
	}
	cfg, err := s.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err == nil {
		return cfg, nil
	}
	if err != ErrUserConfigNotFound {
		return UserConfig{}, err
	}
	return UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: corelib.AppConfig{}}, nil
}

func (s *Service) saveRawUserConfig(p Principal, appCfg corelib.AppConfig) error {
	cfg := UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: appCfg, UpdatedAt: s.now()}
	if err := s.store.SaveUserConfig(cfg); err != nil {
		return err
	}
	if err := saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg); err != nil {
		return err
	}
	return writeRuntimeConfig(s.userDataRoot(p.TenantID, p.UserID), cfg.AppConfig)
}

func (s *Service) lookupMCPServer(p Principal, cfg corelib.AppConfig, serverID string) (*MCPServerView, *corelib.MCPServerEntry, *corelib.LocalMCPServerEntry, error) {
	views := buildMCPViews(cfg, runtimeForService(s).user(composite(p.TenantID, p.UserID)))
	for i := range views {
		if views[i].ID == serverID {
			for j := range cfg.MCPServers {
				if cfg.MCPServers[j].ID == serverID {
					entry := cfg.MCPServers[j]
					return &views[i], &entry, nil, nil
				}
			}
			for j := range cfg.LocalMCPServers {
				if cfg.LocalMCPServers[j].ID == serverID {
					entry := cfg.LocalMCPServers[j]
					return &views[i], nil, &entry, nil
				}
			}
		}
	}
	return nil, nil, nil, ErrInstanceNotFound
}

func buildMCPViews(cfg corelib.AppConfig, runtime *userMCPRuntime) []MCPServerView {
	items := make([]MCPServerView, 0, len(cfg.MCPServers)+len(cfg.LocalMCPServers))
	if runtime == nil {
		runtime = &userMCPRuntime{}
	}
	for _, entry := range cfg.MCPServers {
		state := runtime.remoteState(entry.ID)
		view := MCPServerView{
			ID:            entry.ID,
			Kind:          "remote",
			Name:          entry.Name,
			EndpointURL:   entry.EndpointURL,
			AuthType:      entry.AuthType,
			HasAuthSecret: strings.TrimSpace(entry.AuthSecret) != "",
			HeaderNames:   sortedKeys(entry.Headers),
			Source:        entry.Source,
			CreatedAt:     entry.CreatedAt,
			HealthStatus:  MCPHealthUnknown,
		}
		if state != nil {
			view.HealthStatus = normalizeMCPHealthStatus(state.healthStatus)
			view.FailCount = state.failCount
			if !state.lastCheckAt.IsZero() {
				ts := state.lastCheckAt
				view.LastCheckAt = &ts
			}
			view.Running = state.sessionID != ""
			view.Tools = cloneMCPTools(state.tools)
		}
		items = append(items, view)
	}
	for _, entry := range cfg.LocalMCPServers {
		client := runtime.localClient(entry.ID)
		view := MCPServerView{
			ID:           entry.ID,
			Kind:         "local",
			Name:         entry.Name,
			Command:      entry.Command,
			Args:         cloneStringSlice(entry.Args),
			EnvKeys:      sortedKeys(entry.Env),
			HasEnv:       len(entry.Env) > 0,
			Disabled:     entry.Disabled,
			AutoStart:    entry.AutoStart,
			CreatedAt:    entry.CreatedAt,
			HealthStatus: MCPHealthStopped,
		}
		if client != nil && client.IsRunning() {
			view.Running = true
			view.HealthStatus = MCPHealthRunning
			view.Tools = cloneMCPTools(client.GetTools())
		} else if entry.Disabled {
			view.HealthStatus = MCPHealthDisabled
		}
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		return items[i].Kind < items[j].Kind
	})
	return items
}

func normalizeMCPAuthType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none":
		return "none"
	case "bearer":
		return "bearer"
	case "api_key":
		return "api_key"
	default:
		return ""
	}
}

func cleanStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func sortedKeys(in map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cloneMCPTools(in []MCPToolView) []MCPToolView {
	if len(in) == 0 {
		return nil
	}
	out := make([]MCPToolView, len(in))
	copy(out, in)
	return out
}

func (rt *userMCPRuntime) remoteState(serverID string) *remoteMCPRuntime {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if state, ok := rt.remote[serverID]; ok {
		copyState := *state
		copyState.tools = cloneMCPTools(state.tools)
		return &copyState
	}
	return nil
}

func (rt *userMCPRuntime) localClient(serverID string) *localMCPClient {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.local[serverID]
}

func (rt *userMCPRuntime) isLocalRunning(serverID string) bool {
	client := rt.localClient(serverID)
	return client != nil && client.IsRunning()
}

func (rt *userMCPRuntime) stopLocal(serverID string) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	client := rt.local[serverID]
	delete(rt.local, serverID)
	rt.mu.Unlock()
	if client != nil {
		client.Stop()
	}
}

func (rt *userMCPRuntime) clearRemote(serverID string) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	delete(rt.remote, serverID)
	rt.mu.Unlock()
}

func (rt *userMCPRuntime) startLocal(ctx context.Context, entry corelib.LocalMCPServerEntry) error {
	rt.mu.Lock()
	client := rt.local[entry.ID]
	if client != nil && client.IsRunning() {
		rt.mu.Unlock()
		return nil
	}
	client = newLocalMCPClient(entry)
	rt.local[entry.ID] = client
	rt.mu.Unlock()
	if err := client.Start(ctx); err != nil {
		rt.mu.Lock()
		delete(rt.local, entry.ID)
		rt.mu.Unlock()
		return err
	}
	_, err := client.DiscoverTools()
	return err
}

func (rt *userMCPRuntime) localTools(serverID string) ([]MCPToolView, error) {
	client := rt.localClient(serverID)
	if client == nil || !client.IsRunning() {
		return nil, fmt.Errorf("local MCP server is not running")
	}
	if tools := client.GetTools(); len(tools) > 0 {
		return tools, nil
	}
	return client.DiscoverTools()
}

func (rt *userMCPRuntime) checkRemote(entry corelib.MCPServerEntry) error {
	client := &http.Client{Timeout: 30 * time.Second}
	if err := rt.ensureRemoteSession(client, entry); err != nil {
		return err
	}
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}
	start := time.Now()
	payload, sid, err := doRemoteMCPRoundTrip(client, entry, rt.sessionID(entry.ID), reqBody)
	if err != nil {
		rt.recordRemoteFailure(entry.ID)
		return err
	}
	var parsed struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		rt.recordRemoteFailure(entry.ID)
		return fmt.Errorf("parse MCP tools/list response: %w", err)
	}
	tools := make([]MCPToolView, 0, len(parsed.Result.Tools))
	for _, item := range parsed.Result.Tools {
		tools = append(tools, MCPToolView{Name: item.Name, Description: item.Description, InputSchema: item.InputSchema})
	}
	rt.mu.Lock()
	state := rt.remote[entry.ID]
	if state == nil {
		state = &remoteMCPRuntime{}
		if rt.remote == nil {
			rt.remote = map[string]*remoteMCPRuntime{}
		}
		rt.remote[entry.ID] = state
	}
	state.healthStatus = remoteMCPHealthStatus(time.Since(start))
	state.failCount = 0
	state.lastCheckAt = time.Now()
	state.tools = tools
	if sid != "" {
		state.sessionID = sid
		state.sessionAt = time.Now()
	}
	rt.mu.Unlock()
	return nil
}

func (rt *userMCPRuntime) recordRemoteFailure(serverID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.remote == nil {
		rt.remote = map[string]*remoteMCPRuntime{}
	}
	state := rt.remote[serverID]
	if state == nil {
		state = &remoteMCPRuntime{}
		rt.remote[serverID] = state
	}
	state.failCount++
	state.lastCheckAt = time.Now()
	if state.failCount >= 3 {
		state.healthStatus = MCPHealthUnavailable
	} else {
		state.healthStatus = MCPHealthSlow
	}
}

func (rt *userMCPRuntime) sessionID(serverID string) string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if state := rt.remote[serverID]; state != nil {
		if state.sessionID != "" && time.Since(state.sessionAt) < 30*time.Minute {
			return state.sessionID
		}
	}
	return ""
}

func (rt *userMCPRuntime) ensureRemoteSession(client *http.Client, entry corelib.MCPServerEntry) error {
	if sid := rt.sessionID(entry.ID); sid != "" {
		return nil
	}
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "maclawsrv",
				"version": "1.0.0",
			},
		},
	}
	_, sid, err := doRemoteMCPRoundTrip(client, entry, "", reqBody)
	if err != nil {
		return nil
	}
	if sid != "" {
		rt.mu.Lock()
		if rt.remote == nil {
			rt.remote = map[string]*remoteMCPRuntime{}
		}
		state := rt.remote[entry.ID]
		if state == nil {
			state = &remoteMCPRuntime{}
			rt.remote[entry.ID] = state
		}
		state.sessionID = sid
		state.sessionAt = time.Now()
		rt.mu.Unlock()
	}
	return nil
}

func doRemoteMCPRoundTrip(client *http.Client, entry corelib.MCPServerEntry, sessionID string, reqBody map[string]interface{}) ([]byte, string, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", err
	}
	url := strings.TrimRight(entry.EndpointURL, "/")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range entry.Headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		lower := strings.ToLower(k)
		if lower == "content-type" || lower == "accept" {
			continue
		}
		req.Header.Set(k, v)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	switch entry.AuthType {
	case "bearer":
		if entry.AuthSecret != "" {
			req.Header.Set("Authorization", "Bearer "+entry.AuthSecret)
		}
	case "api_key":
		if entry.AuthSecret != "" {
			req.Header.Set("X-API-Key", entry.AuthSecret)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("MCP HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	sid := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, sid, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	parsed, err := corelib.ParseMCPResponse(resp.Body, resp.Header.Get("Content-Type"), 256*1024)
	if err != nil {
		return nil, sid, err
	}
	return parsed, sid, nil
}

type localMCPClient struct {
	entry   corelib.LocalMCPServerEntry
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex
	stateMu sync.RWMutex
	nextID  atomic.Int64
	tools   []MCPToolView
	running bool
	cancel  context.CancelFunc
}

type localJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type localJSONRPCResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      int64              `json:"id"`
	Result  json.RawMessage    `json:"result,omitempty"`
	Error   *localJSONRPCError `json:"error,omitempty"`
}

type localJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newLocalMCPClient(entry corelib.LocalMCPServerEntry) *localMCPClient {
	return &localMCPClient{entry: entry}
}

func (c *localMCPClient) Start(ctx context.Context) error {
	c.stateMu.Lock()
	if c.running {
		c.stateMu.Unlock()
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, c.entry.Command, c.entry.Args...)
	cmd.Dir = safeLocalMCPDir(c.entry)
	cmd.Env = os.Environ()
	for k, v := range c.entry.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.stateMu.Unlock()
		cancel()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.stateMu.Unlock()
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		c.stateMu.Unlock()
		cancel()
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReaderSize(stdout, 256*1024)
	c.cancel = cancel
	c.running = true
	c.stateMu.Unlock()
	go c.watch()
	if err := c.initialize(); err != nil {
		c.Stop()
		return err
	}
	return nil
}

func safeLocalMCPDir(entry corelib.LocalMCPServerEntry) string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return filepath.Dir(entry.Command)
}

func (c *localMCPClient) watch() {
	if c.cmd == nil {
		return
	}
	_ = c.cmd.Wait()
	c.stateMu.Lock()
	c.running = false
	c.stateMu.Unlock()
}

func (c *localMCPClient) initialize() error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "maclawsrv",
			"version": "1.0.0",
		},
	}
	if _, err := c.sendRequest("initialize", params); err != nil {
		return err
	}
	notice, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "method": "notifications/initialized"})
	notice = append(notice, '\n')
	c.mu.Lock()
	_, err := c.stdin.Write(notice)
	c.mu.Unlock()
	return err
}

func (c *localMCPClient) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	c.stateMu.RLock()
	if !c.running {
		c.stateMu.RUnlock()
		return nil, fmt.Errorf("client not running")
	}
	c.stateMu.RUnlock()
	id := c.nextID.Add(1)
	req := localJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.stdin.Write(data); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp localJSONRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
	return nil, fmt.Errorf("timeout waiting for %s response", method)
}

func (c *localMCPClient) DiscoverTools() ([]MCPToolView, error) {
	result, err := c.sendRequest("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, err
	}
	tools := make([]MCPToolView, 0, len(parsed.Tools))
	for _, item := range parsed.Tools {
		tools = append(tools, MCPToolView{Name: item.Name, Description: item.Description, InputSchema: item.InputSchema})
	}
	c.stateMu.Lock()
	c.tools = tools
	c.stateMu.Unlock()
	return tools, nil
}

func (c *localMCPClient) GetTools() []MCPToolView {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return cloneMCPTools(c.tools)
}

func (c *localMCPClient) IsRunning() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.running
}

func (c *localMCPClient) Stop() {
	c.stateMu.Lock()
	wasRunning := c.running
	c.running = false
	c.stateMu.Unlock()
	if !wasRunning {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}
