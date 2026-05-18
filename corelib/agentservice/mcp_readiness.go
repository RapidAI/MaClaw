package agentservice

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MCPReadinessManager is the state reconciler for MCP server runtime.
// It ensures the runtime state (local processes running, remote sessions
// established) converges to the declared configuration state.
//
// Responsibilities:
//  1. Start local MCP servers with AutoStart=true that are not running.
//  2. Probe remote MCP servers that have no health state or stale state.
//  3. Respect cooldowns to avoid restart storms for crashy servers.
//
// All reconciliation is synchronous with bounded timeouts:
//   - Local server start: bounded by process fork + MCP initialize (~2s typical)
//   - Remote server probe: bounded by remoteProbeTimeout (5s)
//
// This ensures MCP tools are available on the FIRST agent loop iteration,
// not the second. The tradeoff is up to 5s latency on the first call after
// process restart — acceptable because it only happens once per user per
// process lifetime (subsequent calls hit the cooldown fast path).
type MCPReadinessManager struct {
	svc *Service

	mu    sync.Mutex
	users map[string]*userReadinessState
}

type userReadinessState struct {
	mu sync.Mutex

	// localAttempts tracks the last start attempt time per local server ID.
	localAttempts map[string]time.Time

	// remoteAttempts tracks the last probe attempt time per remote server ID.
	remoteAttempts map[string]time.Time
}

const (
	// remoteProbeInterval is how often a remote server is re-probed.
	remoteProbeInterval = 60 * time.Second

	// remoteProbeTimeout bounds the HTTP round-trip for a single remote probe.
	// Short enough to not block the agent loop excessively, long enough to
	// succeed on a healthy server with moderate latency.
	remoteProbeTimeout = 5 * time.Second

	// localRestartCooldown prevents rapid restart attempts for a crashed
	// local server.
	localRestartCooldown = 30 * time.Second
)

func NewMCPReadinessManager(svc *Service) *MCPReadinessManager {
	return &MCPReadinessManager{
		svc:   svc,
		users: make(map[string]*userReadinessState),
	}
}

// EnsureReady reconciles the MCP runtime state for the given principal.
// Returns the user's AppConfig for reuse by the caller (avoids double-load).
//
// All operations are synchronous with bounded timeouts. On the first call
// after process restart, this may take up to ~5s (remote probe timeout).
// Subsequent calls are fast (<1ms) due to cooldown tracking.
func (m *MCPReadinessManager) EnsureReady(ctx context.Context, p Principal) (corelib.AppConfig, bool) {
	cfg, err := m.svc.getOrLoadUserConfig(p.TenantID, p.UserID)
	if err != nil {
		return corelib.AppConfig{}, false
	}
	if len(cfg.AppConfig.LocalMCPServers) == 0 && len(cfg.AppConfig.MCPServers) == 0 {
		return cfg.AppConfig, true
	}

	userKey := composite(p.TenantID, p.UserID)
	state := m.getOrCreateUserState(userKey)
	runtime := runtimeForService(m.svc).user(userKey)

	// Reconcile local servers.
	for i := range cfg.AppConfig.LocalMCPServers {
		srv := &cfg.AppConfig.LocalMCPServers[i]
		if srv.Disabled || !srv.AutoStart {
			continue
		}
		m.reconcileLocal(ctx, runtime, state, *srv)
	}

	// Reconcile remote servers.
	for i := range cfg.AppConfig.MCPServers {
		m.reconcileRemote(runtime, state, &cfg.AppConfig.MCPServers[i])
	}

	return cfg.AppConfig, true
}

func (m *MCPReadinessManager) getOrCreateUserState(userKey string) *userReadinessState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.users[userKey]; ok {
		return s
	}
	s := &userReadinessState{
		localAttempts:  make(map[string]time.Time),
		remoteAttempts: make(map[string]time.Time),
	}
	m.users[userKey] = s
	return s
}

func (m *MCPReadinessManager) reconcileLocal(ctx context.Context, runtime *userMCPRuntime, state *userReadinessState, entry corelib.LocalMCPServerEntry) {
	if runtime.isLocalRunning(entry.ID) {
		return
	}

	state.mu.Lock()
	lastAttempt, attempted := state.localAttempts[entry.ID]
	if attempted && time.Since(lastAttempt) < localRestartCooldown {
		state.mu.Unlock()
		return
	}
	state.localAttempts[entry.ID] = time.Now()
	state.mu.Unlock()

	if err := runtime.startLocal(ctx, entry); err != nil {
		log.Printf("[MCPReadiness] failed to start local server %s (%s): %v", entry.Name, entry.Command, err)
		return
	}
	log.Printf("[MCPReadiness] started local server %s (%s)", entry.Name, entry.ID)
}

func (m *MCPReadinessManager) reconcileRemote(runtime *userMCPRuntime, state *userReadinessState, entry *corelib.MCPServerEntry) {
	// Skip if healthy and fresh.
	if rs := runtime.remoteState(entry.ID); rs != nil {
		if rs.healthStatus == MCPHealthHealthy && time.Since(rs.lastCheckAt) < remoteProbeInterval {
			return
		}
	}

	// Skip if probed recently (even if failed).
	state.mu.Lock()
	lastProbe, probed := state.remoteAttempts[entry.ID]
	if probed && time.Since(lastProbe) < remoteProbeInterval {
		state.mu.Unlock()
		return
	}
	state.remoteAttempts[entry.ID] = time.Now()
	state.mu.Unlock()

	// Synchronous probe with short timeout. Uses a dedicated HTTP client
	// with remoteProbeTimeout instead of the default 30s, ensuring the
	// agent loop is never blocked for more than 5s per remote server.
	if err := checkRemoteWithTimeout(runtime, *entry, remoteProbeTimeout); err != nil {
		log.Printf("[MCPReadiness] remote probe %s (%s) failed: %v", entry.Name, entry.ID, err)
		return
	}
	log.Printf("[MCPReadiness] remote probe %s (%s) ok", entry.Name, entry.ID)
}

// checkRemoteWithTimeout is like userMCPRuntime.checkRemote but uses a
// dedicated HTTP client with a short timeout, preventing slow/dead remote
// servers from blocking the agent loop for 30s.
func checkRemoteWithTimeout(runtime *userMCPRuntime, entry corelib.MCPServerEntry, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	return runtime.checkRemoteWithClient(client, entry)
}

// Reset clears readiness state for a user, forcing re-reconciliation on
// the next EnsureReady call.
func (m *MCPReadinessManager) Reset(tenantID, userID string) {
	m.mu.Lock()
	delete(m.users, composite(tenantID, userID))
	m.mu.Unlock()
}
