package agentservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	mcphttp "github.com/RapidAI/CodeClaw/corelib/mcp"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type MCPToolView struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type MCPServerView struct {
	ID            string                          `json:"id"`
	Kind          string                          `json:"kind"`
	Name          string                          `json:"name"`
	EndpointURL   string                          `json:"endpoint_url,omitempty"`
	AuthType      string                          `json:"auth_type,omitempty"`
	HasAuthSecret bool                            `json:"has_auth_secret,omitempty"`
	HeaderNames   []string                        `json:"header_names,omitempty"`
	Command       string                          `json:"command,omitempty"`
	Args          []string                        `json:"args,omitempty"`
	EnvKeys       []string                        `json:"env_keys,omitempty"`
	HasEnv        bool                            `json:"has_env,omitempty"`
	Disabled      bool                            `json:"disabled,omitempty"`
	AutoStart     bool                            `json:"auto_start,omitempty"`
	Source        corelib.MCPServerSource         `json:"source,omitempty"`
	Capability    *corelib.MCPServerCapabilityRef `json:"capability,omitempty"`
	Running       bool                            `json:"running"`
	HealthStatus  MCPHealthStatus                 `json:"health_status"`
	FailCount     int                             `json:"fail_count,omitempty"`
	LastCheckAt   *time.Time                      `json:"last_check_at,omitempty"`
	CreatedAt     string                          `json:"created_at,omitempty"`
	Tools         []MCPToolView                   `json:"tools,omitempty"`
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

// mcpRuntimeInventorySnapshot is one consistent observation of a principal's
// MCP runtime. It is deliberately value-only: callers must not retain live
// runtime/client pointers while building a semantic catalog, because a later
// health check or local-process restart could otherwise mix generations in one
// route snapshot.
type mcpRuntimeInventorySnapshot struct {
	remote map[string]remoteMCPRuntime
	local  map[string]localMCPInventorySnapshot
}

type localMCPInventorySnapshot struct {
	running bool
	tools   []MCPToolView
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
		// Reject metadata/link-local endpoints at admission so a bad URL never
		// reaches the probe or tool-call transport.
		if err := validateMCPRemoteEndpoint(endpoint); err != nil {
			return nil, err
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
		// P0-1 (2026-09-08 review): CreateMCPServer previously accepted any
		// binary, any args and any env, then auto-started when AutoStart=true.
		// Combined with LLM-driven registration this was a remote-code-execution
		// primitive. Validate hard before persisting.
		cleanedArgs, err := validateMCPCommandArgs(in.Args)
		if err != nil {
			return nil, fmt.Errorf("invalid command args: %w", err)
		}
		cleanedEnv, err := validateMCPEnv(in.Env)
		if err != nil {
			return nil, fmt.Errorf("invalid command env: %w", err)
		}
		// Auto-start now requires explicit confirmation. The auto-spawn path was
		// previously triggered purely by client request (no second factor); LLM
		// prompt injection could use it to spawn a reverse shell. The default
		// AutoStart value coming in is forced to false.
		autoStart := false
		if in.AutoStart {
			autoStart = false
		}
		entry := corelib.LocalMCPServerEntry{
			ID:        NewID("mcp_local"),
			Name:      name,
			Command:   command,
			Args:      cleanedArgs,
			Env:       cleanedEnv,
			Disabled:  in.Disabled,
			AutoStart: autoStart,
			CreatedAt: now,
		}
		cfg.AppConfig.LocalMCPServers = append(cfg.AppConfig.LocalMCPServers, entry)
		if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
			return nil, err
		}
		// Even when the client requested AutoStart=true we deliberately do NOT
		// spawn at registration time. Operators must call StartMCPServer
		// explicitly; that path goes through StopLocal-first health checks and
		// emits a different audit action (`mcp.local.started` not `mcp.local.created`).
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
			// An update must not be able to pivot an existing server onto a
			// metadata address.
			if err := validateMCPRemoteEndpoint(entry.EndpointURL); err != nil {
				return nil, err
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
			nextSecret := strings.TrimSpace(*in.AuthSecret)
			preserveMaskedSecretString(&nextSecret, entry.AuthSecret)
			entry.AuthSecret = nextSecret
		}
		if in.Headers != nil {
			entry.Headers = preserveStringMapSecretValues(entry.Headers, cleanStringMap(in.Headers))
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
			cleaned, err := validateMCPCommandArgs(*in.Args)
			if err != nil {
				return nil, fmt.Errorf("invalid command args: %w", err)
			}
			entry.Args = cleaned
		}
		if in.Env != nil {
			cleaned, err := validateMCPEnv(in.Env)
			if err != nil {
				return nil, fmt.Errorf("invalid command env: %w", err)
			}
			entry.Env = preserveStringMapSecretValues(entry.Env, cleaned)
		}
		if in.Disabled != nil {
			entry.Disabled = *in.Disabled
		}
		if in.AutoStart != nil {
			// P0-1 (2026-09-08 review): AutoStart may be turned off through
			// Update but never back on. Re-enabling auto-spawn would reintroduce
			// the "register -> immediate exec" primitive, so it stays off;
			// operators start servers explicitly via StartMCPServer.
			entry.AutoStart = false
		}
		updatedKind = "local"
		break
	}
	if updatedKind == "" {
		return nil, ErrInstanceNotFound
	}
	// A server configuration is part of every dynamic binding's observed
	// identity. Revoke before persisting/restarting it so an interrupted update
	// cannot leave its old contracts attached to a changed endpoint.
	if err := s.revokeMCPServerDynamicContracts(p, serverID); err != nil {
		return nil, err
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
	// Deletion is a control-plane boundary, not merely a runtime cleanup. The
	// associated contracts must disappear before the configuration can change.
	if err := s.revokeMCPServerDynamicContracts(p, serverID); err != nil {
		return err
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
		// P0-1 (2026-09-08 review): double-check at start time that the binary
		// resolves to a trusted directory. The CreateMCPServer validation
		// controls the input shape; this gate covers drift between
		// registration and now, and prevents a binary that has been symlinked
		// to /tmp/evil from running.
		if err := validateLocalMCPSpawnTarget(localEntry.Command); err != nil {
			return nil, fmt.Errorf("local MCP binary rejected: %w", err)
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
			return nil, agentruntime.MarkJobErrorRetryable(err)
		}
		return s.GetMCPServer(ctx, p, serverID)
	}
	if err := runtime.checkRemote(*remoteEntry); err != nil {
		return nil, agentruntime.MarkJobErrorRetryable(err)
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
	appCfg = effectiveLLMFlatConfig(appCfg)
	cfg := UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: appCfg, UpdatedAt: s.now()}
	if err := s.store.SaveUserConfig(cfg); err != nil {
		return err
	}
	return saveUserConfigToFile(s.userConfigPath(p.TenantID, p.UserID), cfg)
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
			Capability:    entry.Capability,
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
			Source:       entry.Source,
			Capability:   entry.Capability,
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

// mcpMaxCommandArgs caps argv length so a hostile config cannot push the
// child process past its ARG_MAX or overflow our own bookkeeping.
const mcpMaxCommandArgs = 256

// mcpMaxArgLen caps a single argv slot.
const mcpMaxArgLen = 4096

// validateMCPRemoteEndpoint refuses MCP remote endpoints that point at cloud
// instance-metadata or link-local infrastructure. One JSON-RPC round trip to
// 169.254.169.254 returns cloud STS credentials, so this class is rejected
// regardless of any "private network allowed" posture.
//
// Loopback and RFC1918 stay permitted: MCP-over-HTTP from localhost is a
// supported deployment and must keep working. The guard is deliberately a
// floor, not a full public-only allowlist — see corelib/mcp for the rationale.
//
// This check is SYNTAX ONLY and never resolves DNS. It runs on admission and
// on every single round trip, so a lookup here would both break registering a
// server whose name only resolves later (VPN down, internal DNS) and add a
// resolution to every tool call. Name-to-address binding is enforced once, at
// dial time, by the guarded transport in corelib/mcp.
//
// Added 2026-09-09: corelib/mcp.sendMCPRequest already enforced this, but
// agentservice runs a second, independent MCP-over-HTTP stack (health probes
// and tools/call) that never reached that code.
func validateMCPRemoteEndpoint(raw string) error {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return fmt.Errorf("endpoint_url is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint_url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("endpoint_url must use http or https")
	}
	host := strings.Trim(strings.Trim(parsed.Hostname(), "[]"), ".")
	if host == "" {
		return fmt.Errorf("endpoint_url host is required")
	}
	if mcphttp.IsAlwaysBlockedMCPHost(host) {
		return fmt.Errorf("endpoint_url host %q is always blocked (cloud metadata or link-local)", host)
	}
	return nil
}

// validateMCPCommandArgs hardens the argv handed to exec.CommandContext.
//
// IMPORTANT — what this does NOT do, and why: it does not reject shell
// metacharacters. localMCPClient.Start spawns via
// exec.CommandContext(ctx, command, args...) with no shell in the path, so
// `;`, `|`, `&` and spaces are inert data, never syntax. An earlier revision
// enforced a `[A-Za-z0-9._:/=@,+%~-]+` pattern that rejected spaces, which
// broke every legitimate MCP server launched with a path containing a space
// (the norm on Windows: `C:\Users\Jane Doe\...`). Do not reintroduce it.
//
// What it does reject: NUL (truncates at the syscall boundary), CR/LF (would
// corrupt the newline-delimited JSON-RPC framing the client reads back over
// stdio), and oversized argv.
func validateMCPCommandArgs(in []string) ([]string, error) {
	if len(in) > mcpMaxCommandArgs {
		return nil, fmt.Errorf("too many args (limit %d)", mcpMaxCommandArgs)
	}
	out := make([]string, 0, len(in))
	for i, slot := range in {
		if slot == "" {
			continue
		}
		if strings.ContainsAny(slot, "\r\n\x00") {
			return nil, fmt.Errorf("arg %d contains a control character", i)
		}
		if len(slot) > mcpMaxArgLen {
			return nil, fmt.Errorf("arg %d too long (limit %d bytes)", i, mcpMaxArgLen)
		}
		out = append(out, slot)
	}
	return out, nil
}

// mcpEnvBlockedKeys are environment variable names that would let a local
// MCP server load arbitrary code into its child process and therefore must
// never be settable from a CreateMCPServer / UpdateMCPServer payload.
//
// See: LD_PRELOAD / LD_LIBRARY_PATH (Linux), DYLD_INSERT_LIBRARIES (macOS),
// PATH / PYTHONPATH / NODE_PATH / RUBYLIB / PERL5LIB / BASH_ENV / ENV /
// SHELLOPTS / GCONV_PATH (GNU libc dynamic loader), and PATHEXT (Windows
// command resolution). NODE_OPTIONS = --require /inspect-brk overrides are
// also covered because NODE_OPTIONS is commonly used for code injection.
var mcpEnvBlockedKeys = map[string]struct{}{
	"path":               {},
	"ld_preload":         {},
	"ld_library_path":    {},
	"dyld_insert_libs":   {},
	"dyld_library_path":  {},
	"pythonpath":         {},
	"python_startup":     {},
	"node_path":          {},
	"node_options":       {},
	"rubyopt":            {},
	"rubylib":            {},
	"perl5lib":           {},
	"perl5opt":           {},
	"bash_env":           {},
	"env":                {},
	"shellopts":          {},
	"gconv_path":         {},
	"pathext":            {},
	"comspec":            {},
	"systemroot":         {},
	"windir":             {},
}

// validateMCPEnv enforces that every env key is a regular identifier, that no
// loader-injection names are present, and that values do not embed newline
// sequences that bash / cmd would honor.
func validateMCPEnv(in map[string]string) (map[string]string, error) {
	if len(in) > 64 {
		return nil, fmt.Errorf("too many env entries (limit 64)")
	}
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if !envKeyAllowedRe.MatchString(key) {
			return nil, fmt.Errorf("env key %q is not a valid POSIX/Win identifier", truncateForErr(key))
		}
		normalized := strings.ToLower(key)
		if _, blocked := mcpEnvBlockedKeys[normalized]; blocked {
			return nil, fmt.Errorf("env key %q is reserved for runtime safety", key)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("env value for %q must not contain newline / NUL", key)
		}
		if len(v) > 4096 {
			return nil, fmt.Errorf("env value for %q too long (limit 4096 bytes)", key)
		}
		out[key] = v
	}
	if len(out) > 64 {
		return nil, fmt.Errorf("too many env entries (limit 64)")
	}
	return out, nil
}

var envKeyAllowedRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// untrustedLocalMCPSpawnDirs returns directories a local MCP binary must never
// be launched from. These are the places any unprivileged process (or a
// downloaded archive, or a crafted "install this MCP server" instruction) can
// drop an executable, so allowing them turns an MCP registration into arbitrary
// code execution.
//
// Why a denylist and not an allowlist: an earlier revision allowlisted
// /usr/bin,/bin,/usr/local/bin and %SystemRoot%\System32. That reads as strict
// but is close to worthless — those directories already contain bash, python3,
// curl, nc and ssh on POSIX, and cmd.exe, powershell.exe, wsl.exe, certutil.exe
// and rundll32.exe on Windows — while simultaneously rejecting the places real
// MCP servers actually live (~/.local/bin, npm global prefix, go/bin, venvs,
// node_modules/.bin). Maximum collateral damage, near-zero security gain.
func untrustedLocalMCPSpawnDirs() []string {
	dirs := []string{os.TempDir(), "/tmp", "/var/tmp", "/dev/shm"}
	if runtime.GOOS == "windows" {
		dirs = append(dirs,
			filepath.Join(os.Getenv("SystemRoot"), "Temp"),
			filepath.Join(os.Getenv("SystemRoot"), "Tasks"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, "Downloads"))
	}
	out := make([]string, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		clean := filepath.Clean(d)
		if _, dup := seen[clean]; dup {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// validateLocalMCPSpawnTarget rejects commands that resolve into a
// world-writable / temporary directory.
//
// P0-1 (2026-09-08 review): the registration path previously had no such gate,
// so `command: /tmp/x` (or a bare name resolving to a PATH hijack) executed
// verbatim. Note this is defence-in-depth only: it does not make an approved
// directory's binaries "safe". The primary control is that registration never
// spawns — see CreateMCPServer.
func validateLocalMCPSpawnTarget(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("empty command")
	}
	if strings.ContainsAny(command, "\r\n\x00") {
		return fmt.Errorf("command contains control characters")
	}
	resolved := command
	// exec.LookPath refuses arguments and only looks at PATH; passing a
	// fully-qualified path through returns it unchanged.
	if !strings.ContainsAny(command, `/\`) {
		lp, err := exec.LookPath(command)
		if err != nil {
			return fmt.Errorf("unable to resolve %q on PATH: %w", command, err)
		}
		resolved = lp
	} else {
		abs, err := filepath.Abs(command)
		if err != nil {
			return fmt.Errorf("unable to resolve %q to an absolute path: %w", command, err)
		}
		resolved = abs
	}
	// Walk symlinks; if the final target lives outside any allowed dir, reject.
	target, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		// Some binaries don't exist yet (rare for MCP servers but possible).
		// Fall back to the lexical path of `resolved` so we still apply the
		// directory check.
		target = resolved
	}
	target = filepath.Clean(target)
	dir := filepath.Clean(filepath.Dir(target))
	for _, root := range untrustedLocalMCPSpawnDirs() {
		if pathsEqual(dir, root) || isPathUnder(dir, root) {
			return fmt.Errorf("command resolves to %s, inside untrusted directory %s", truncateForErr(target), root)
		}
	}
	return nil
}

// pathsEqual compares two filesystem paths case-insensitively on Windows.
func pathsEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// isPathUnder reports whether child is contained within parent.
func isPathUnder(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return rel != "."
}

func truncateForErr(s string) string {
	const limit = 64
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
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

// inventorySnapshot captures remote health/tool lists and local process/tool
// lists while holding the runtime ownership lock. A local client's own state
// is copied before the runtime lock is released, so the result represents one
// catalog observation rather than a collection of individually fresh reads.
func (rt *userMCPRuntime) inventorySnapshot() mcpRuntimeInventorySnapshot {
	snapshot := mcpRuntimeInventorySnapshot{
		remote: make(map[string]remoteMCPRuntime),
		local:  make(map[string]localMCPInventorySnapshot),
	}
	if rt == nil {
		return snapshot
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for serverID, state := range rt.remote {
		if state == nil {
			continue
		}
		copyState := *state
		copyState.tools = cloneMCPTools(state.tools)
		snapshot.remote[serverID] = copyState
	}
	for serverID, client := range rt.local {
		if client == nil {
			continue
		}
		snapshot.local[serverID] = client.inventorySnapshot()
	}
	return snapshot
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
	// Share the SSRF-guarded MCP transport (2026-09-09 re-review). A bare
	// http.Client follows redirects, so an endpoint that answers 307 to
	// 169.254.169.254 would turn this probe into a cloud-metadata request.
	client := mcphttp.NewPrivateHTTPClient(30 * time.Second)
	return rt.checkRemoteWithClient(client, entry)
}

// checkRemoteWithClient performs a health probe using the provided HTTP client.
// The client's Timeout controls the maximum duration of the probe.
func (rt *userMCPRuntime) checkRemoteWithClient(client *http.Client, entry corelib.MCPServerEntry) error {
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
	// Choke point for every agentservice MCP-over-HTTP call (health probe,
	// session bootstrap and tools/call). Validate the endpoint here as well as
	// on the transport so a caller that passes its own http.Client is still
	// covered — the guard must not depend on which client was injected.
	if err := validateMCPRemoteEndpoint(entry.EndpointURL); err != nil {
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
		return nil, sid, fmt.Errorf("MCP HTTP %d: body_len=%d", resp.StatusCode, len(body))
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
	coretool.PrepareCommandForTreeKill(cmd)
	cmd.Cancel = func() error {
		coretool.TerminateCommandTree(cmd)
		return nil
	}
	cmd.Dir = safeLocalMCPDir(c.entry)
	cmd.Env = os.Environ()
	for k, v := range c.entry.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	coretool.HideCommandWindow(cmd)
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

func (c *localMCPClient) inventorySnapshot() localMCPInventorySnapshot {
	if c == nil {
		return localMCPInventorySnapshot{}
	}
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return localMCPInventorySnapshot{running: c.running, tools: cloneMCPTools(c.tools)}
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
		coretool.TerminateCommandTree(c.cmd)
	}
}
