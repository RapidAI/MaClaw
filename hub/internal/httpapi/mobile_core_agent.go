package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileCoreAgent is the shared full-agent executor for Mobile official path.
// Reuses corelib/agentservice (skills/MCP/web tools) — same stack as MaClawSrv.
var (
	mobileCoreAgentOnce   sync.Once
	mobileCoreAgent       *agentservice.CoreAgentExecutor
	mobileAgentService    *agentservice.Service
	mobileMCPBridge       *agentservice.MCPToolBridge
	mobileAgentServiceErr error
	mobileAgentDataRoot   string
	mobileAgentRootMu     sync.RWMutex
)

// InitMobileCoreAgent sets the Hub runtime data root used for mobile agent
// state (skills, MCP config, memory). Safe to call multiple times; first
// non-empty root wins before the agent is first used.
func InitMobileCoreAgent(runtimeDataDir string) {
	root := strings.TrimSpace(runtimeDataDir)
	if root == "" {
		return
	}
	mobileAgentRootMu.Lock()
	if mobileAgentDataRoot == "" {
		mobileAgentDataRoot = filepath.Join(root, "mobile-agent")
	}
	mobileAgentRootMu.Unlock()
}

// resetMobileCoreAgentForTest tears down process-level mobile agent state so
// tests can re-init against a fresh data root without cross-test leakage or
// Windows file-lock failures on TempDir cleanup (knowledge.db / records.db).
func resetMobileCoreAgentForTest() {
	resetMobileDynamicSemanticRoutingForTest()
	mobileResetKnowledgePurgeStateForTest()
	if mobileKnowledgeStore != nil {
		_ = mobileKnowledgeStore.Close()
	}
	mobileKnowledgeStore = nil
	mobileKnowledgeErr = nil
	mobileKnowledgeMode = ""
	mobileKnowledgeOnce = sync.Once{}

	if mobileAgentService != nil {
		_ = mobileAgentService.Close()
	}
	mobileCoreAgent = nil
	mobileAgentService = nil
	mobileMCPBridge = nil
	mobileAgentServiceErr = nil
	mobileCoreAgentOnce = sync.Once{}

	mobileAgentRootMu.Lock()
	mobileAgentDataRoot = ""
	mobileAgentRootMu.Unlock()
}

func mobileCoreAgentDataRoot() string {
	mobileAgentRootMu.RLock()
	root := mobileAgentDataRoot
	mobileAgentRootMu.RUnlock()
	if root != "" {
		return root
	}
	if env := strings.TrimSpace(os.Getenv("MACLAW_HUB_MOBILE_AGENT_DATA")); env != "" {
		return env
	}
	return filepath.Join("data", "mobile-agent")
}

// purgeMobileAgentUserData removes the Mobile full-agent control plane, user
// configuration, installed skills, workspace and structured records. The
// tombstone is set by the caller before this runs, so an old authenticated
// request cannot recreate its agent principal during cleanup.
func purgeMobileAgentUserData(ctx context.Context, tenantID string, ownerIDs map[string]struct{}) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		return err
	}
	for ownerID := range ownerIDs {
		if err := svc.PurgeUserData(ctx, tenantID, ownerID); err != nil {
			return err
		}
	}
	return nil
}

func mobileCoreAgentTokenSecret() string {
	if s := strings.TrimSpace(os.Getenv("MACLAW_HUB_MOBILE_AGENT_TOKEN_SECRET")); s != "" {
		return s
	}
	// Local service token material; not used for viewer Hub auth.
	return "maclaw-hub-mobile-agent-token-secret-v1"
}

func mobileEnsureCoreAgent() (*agentservice.CoreAgentExecutor, *agentservice.Service, error) {
	mobileCoreAgentOnce.Do(func() {
		dataRoot := mobileCoreAgentDataRoot()
		if err := os.MkdirAll(dataRoot, 0o755); err != nil {
			mobileAgentServiceErr = fmt.Errorf("mobile agent data root: %w", err)
			return
		}
		executor := &agentservice.CoreAgentExecutor{
			// Multi-tenant Hub: no host shell / direct SSH unless explicitly enabled later.
			AllowLocalBash:       false,
			AllowDirectSSH:       false,
			AllowSSHFileTransfer: false,
		}
		svc, err := agentservice.NewService(agentservice.Config{
			DataRoot:    dataRoot,
			TokenSecret: mobileCoreAgentTokenSecret(),
			TokenTTL:    12 * time.Hour,
		}, nil, executor)
		if err != nil {
			mobileAgentServiceErr = fmt.Errorf("create mobile agent service: %w", err)
			return
		}
		// Reviewed dynamic capability family (semantic routing). Mirrors the
		// MaClawSrv bootstrap: any failure is fail-closed — the mobile agent is
		// unavailable rather than silently serving only the legacy bulk tool
		// surface. ConfigureDynamicSemanticRouting also wires the routing onto
		// the executor via its SetDynamicSemanticRouting support.
		if err := configureMobileDynamicSemanticRouting(svc); err != nil {
			log.Printf("[mobile-core-agent] dynamic semantic routing unavailable (fail-closed): %v", err)
			_ = svc.Close()
			mobileAgentServiceErr = fmt.Errorf("configure mobile dynamic semantic routing: %w", err)
			return
		}
		// Generic dynamic-effect receipt reconciliation loop. No trusted
		// receipt source is registered yet; the framework is in place.
		if err := startMobileDynamicEffectReceiptWorker(svc); err != nil {
			_ = svc.Close()
			mobileAgentServiceErr = fmt.Errorf("start mobile dynamic effect receipt worker: %w", err)
			return
		}
		// Same wiring as MaClawSrv: skill + MCP + knowledge onto CoreAgentExecutor.
		// The legacy bridges stay for request families the resolver has not
		// migrated; a managed (Managed=true) request is served solely by the
		// grant-bound semantic surface.
		skillBridge := agentservice.NewSkillToolBridge(svc)
		mcpBridge := agentservice.NewMCPToolBridge(svc)
		executor.SetSkillToolProvider(skillBridge)
		executor.SetMCPToolProvider(mcpBridge)
		mobileInitKnowledgeStore(dataRoot, executor)
		wireMobileReviewedHostSpeechTranscriber(executor)
		mobileCoreAgent = executor
		mobileAgentService = svc
		mobileMCPBridge = mcpBridge
		log.Printf("[mobile-core-agent] initialized data_root=%s skills=on mcp=on knowledge=on semantic_routing=on effect_receipt_worker=on", dataRoot)
	})
	if mobileAgentServiceErr != nil {
		return nil, nil, mobileAgentServiceErr
	}
	if mobileCoreAgent == nil || mobileAgentService == nil {
		return nil, nil, fmt.Errorf("mobile core agent is not initialized")
	}
	return mobileCoreAgent, mobileAgentService, nil
}

func mobileCoreAgentExecutor() *agentservice.CoreAgentExecutor {
	exec, _, err := mobileEnsureCoreAgent()
	if err != nil {
		log.Printf("[mobile-core-agent] ensure failed: %v", err)
		// Still return a bare executor so callers can fall back cleanly.
		return &agentservice.CoreAgentExecutor{
			AllowLocalBash: false, AllowDirectSSH: false, AllowSSHFileTransfer: false,
		}
	}
	return exec
}

// mobileRunCoreAgent runs the full corelib agentservice agent for a mobile user.
// LLM traffic is proxied through the Hub official (or delegated) chat handler so
// credits / auth stay on the existing Mobile billing path.
func mobileRunCoreAgent(
	ctx context.Context,
	r *http.Request,
	principal *auth.ViewerPrincipal,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	baseMessages []map[string]string,
	emit mobileAgentEventWriter,
	attachments []agent.MessageAttachment,
) (string, string, error) {
	if principal == nil {
		return "", "", fmt.Errorf("principal is required for core agent")
	}
	if officialLLM == nil && !useDelegated {
		return "", "", fmt.Errorf("no LLM backend for core agent")
	}

	userText, history, systemHint := mobileSplitAgentMessages(baseMessages)
	if strings.TrimSpace(userText) == "" {
		return "", "", fmt.Errorf("user message is required")
	}
	// Same-turn convenience: "查状态 host user password" → vault + enable ssh tool.
	// Password is stripped before the LLM sees the message (even if register fails).
	if redacted, label, pass, registered := mobileMaybeAutoRegisterSSHFromUserText(principal, userText); pass != "" || registered {
		userText = redacted
		if registered {
			log.Printf("[mobile-core-agent] auto-registered ssh label=%s for user=%s", label, principal.UserID)
		}
		// Redact password fragments in prior user turns so they never reach the LLM.
		if pass != "" {
			for i := range history {
				if history[i].Role == agentservice.MessageRoleUser {
					history[i].Content = mobileRedactPasswordInText(history[i].Content, pass)
				}
			}
		}
	}

	tenantID := strings.TrimSpace(principal.TenantID)
	if tenantID == "" {
		tenantID = "default"
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		userID = strings.TrimSpace(principal.Email)
	}
	if userID == "" {
		return "", "", fmt.Errorf("user identity is required")
	}

	executor, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		return "", "", err
	}
	p := agentservice.Principal{TenantID: tenantID, UserID: userID, Roles: []string{"mobile"}}
	if err := svc.EnsurePrincipal(ctx, p, principal.Email, userID); err != nil {
		return "", "", fmt.Errorf("ensure agent principal: %w", err)
	}
	// Best-effort: seed Hub marketplace/package skills into this user's skill dir.
	mobileSeedUserSkills(svc, p)

	// Prefer agentservice user data root (skills/MCP live here) when available.
	dataDir := filepath.Join(svc.DataRoot(), "tenants", sanitizePathSegment(tenantID), "users", sanitizePathSegment(userID), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create agent data dir: %w", err)
	}
	workspace := filepath.Join(dataDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return "", "", fmt.Errorf("create workspace: %w", err)
	}

	// Merge user-saved AppConfig (MCP servers, skill hubs, etc.) with Hub LLM proxy config.
	hubBase := mobileRequestBaseURL(r)
	appCfg := mobileCoreAgentAppConfig(systemHint, hubBase)
	if uc, err := svc.GetUserConfig(ctx, p); err == nil && uc != nil {
		appCfg = mobileMergeUserAgentConfig(uc.AppConfig, appCfg)
	}
	// Inject Linux server profiles + Hub vault secrets as label-based SSH hosts
	// so the assistant can operate remote machines without AllowDirectSSH.
	// Always attach a status hint so the model does not invent "no ssh tools"
	// without explaining setup when hosts are empty. Live sessions come from
	// the same per-user SSHSessionManager as desktop GUI reuse.
	mobileEnsureStateLoaded()
	keyMaterialDir := filepath.Join(dataDir, "ssh-vault-keys")
	sshHosts := mobileAgentSSHHosts(principal, keyMaterialDir)
	if len(sshHosts) > 0 {
		appCfg.SSHHosts = mobileMergeSSHHosts(appCfg.SSHHosts, sshHosts)
	}
	liveSSH := executor.ListSSHSessionsForUser(tenantID, userID)
	if sshHint := mobileAgentSSHSystemHint(sshHosts, liveSSH); sshHint != "" {
		if strings.TrimSpace(appCfg.MaclawRoleDescription) == "" {
			appCfg.MaclawRoleDescription = sshHint
		} else {
			appCfg.MaclawRoleDescription = appCfg.MaclawRoleDescription + "\n\n" + sshHint
		}
	}

	transport := &mobileHubLLMTransport{
		base:         r,
		officialLLM:  officialLLM,
		delegated:    delegated,
		useDelegated: useDelegated,
	}
	llmClient := &http.Client{
		Timeout:   6 * time.Minute,
		Transport: transport,
	}
	// Per-request client so auth headers follow the viewer/delegated path.
	prev := executor.HTTPClient
	executor.HTTPClient = llmClient
	defer func() { executor.HTTPClient = prev }()

	// Give the semantic intent classifier the same Hub-proxied, per-principal
	// LLM path (viewer or delegated billing) as the agent loop itself. Without
	// this slot the classifier fails closed rather than using other credentials.
	if llmCfg, cfgErr := agentservice.ResolveLLMConfig(appCfg); cfgErr == nil {
		ctx = mobileWithSemanticClassifierLLM(ctx, mobileSemanticClassifierLLM{config: llmCfg, client: llmClient})
	} else {
		log.Printf("[mobile-core-agent] semantic classifier llm config unavailable: %v", cfgErr)
	}

	now := time.Now().UTC()
	sessionID := fmt.Sprintf("mobile-%s-%d", userID, now.UnixNano())
	req := agentservice.ExecuteRequest{
		Principal: p,
		Tenant:    agentservice.Tenant{ID: tenantID, Name: tenantID, Status: agentservice.TenantStatusActive, CreatedAt: now, UpdatedAt: now},
		User: agentservice.User{
			ID: userID, TenantID: tenantID, Name: userID, Email: principal.Email,
			Status: agentservice.UserStatusActive, CreatedAt: now, UpdatedAt: now,
		},
		Instance: agentservice.Instance{
			ID: "mobile-default", TenantID: tenantID, UserID: userID,
			Name: "mobile", DataDir: dataDir, RuntimeDir: filepath.Join(dataDir, "runtime"),
			Workspace: workspace, Status: agentservice.InstanceStatusReady, Ready: true,
			Description: "MaClaw Mobile official full agent",
			CreatedAt:   now, UpdatedAt: now,
		},
		Session: agentservice.Session{
			ID: sessionID, TenantID: tenantID, UserID: userID, InstanceID: "mobile-default",
			AgentID: "mobile-assistant", Title: "Mobile Assistant",
			CreatedAt: now, UpdatedAt: now,
		},
		Message: agentservice.Message{
			ID: fmt.Sprintf("msg-%d", now.UnixNano()), SessionID: sessionID,
			Role: agentservice.MessageRoleUser, Content: userText, CreatedAt: now,
			Attachments: append([]agent.MessageAttachment(nil), attachments...),
		},
		History: history,
		DataDir: dataDir,
		Config:  appCfg,
		OnToken: func(delta string) {
			if emit == nil || strings.TrimSpace(delta) == "" {
				return
			}
			emit("delta", map[string]any{"text": delta})
		},
		OnToolCall: func(name string) {
			if emit == nil {
				return
			}
			emit("tool_call", map[string]any{"name": name, "id": name})
		},
		OnToolResult: func(name, result string) {
			if emit == nil {
				return
			}
			emit("tool_result", map[string]any{
				"name":   name,
				"id":     name,
				"result": mobileClipRunes(result, 800),
			})
		},
	}

	result, err := executor.Execute(ctx, req)
	if err != nil {
		return "", "", err
	}
	requestID := transport.lastRequestID()
	if result != nil && result.Metadata != nil {
		if rid := strings.TrimSpace(result.Metadata["request_id"]); rid != "" {
			requestID = rid
		}
	}
	if requestID == "" {
		requestID = fmt.Sprintf("mobile-core-%d", now.UnixNano())
	}
	content := ""
	recordingMarker := ""
	if result != nil {
		content = strings.TrimSpace(result.Content)
		if result.Metadata != nil && result.Metadata["record_audio"] == "true" {
			payload, _ := json.Marshal(map[string]string{
				"title":   result.Metadata["recording_title"],
				"purpose": result.Metadata["recording_purpose"],
				"hint":    result.Metadata["recording_hint"],
			})
			recordingMarker = "__RECORD_AUDIO__" + string(payload)
		}
	}
	if recordingMarker != "" {
		if emit != nil {
			emit("record_audio", map[string]any{"request": recordingMarker})
		}
		content = recordingMarker
	}
	return content, requestID, nil
}

func mobileCoreAgentAppConfig(systemHint, hubBaseURL string) corelib.AppConfig {
	// Sentinel URL handled by mobileHubLLMTransport. Key is non-empty so
	// ResolveLLMConfig succeeds; real auth is injected by the transport.
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "http://hub-mobile-llm.internal/v1",
		MaclawLLMKey:             "hub-mobile-viewer",
		MaclawLLMModel:           "auto",
		MaclawLLMProtocol:        "openai",
		MaclawAgentMaxIterations: 40,
		MaclawRoleName:           "MaClaw",
		MaclawRoleDescription: strings.TrimSpace(
			"MaClaw Mobile official assistant on Hub. Full agent tools (web, skills, MCP, knowledge when available). " +
				"When the user needs Linux server ops, use the built-in ssh tool with preconfigured host labels " +
				"(Hub vault — credentials never leave Hub). Reuse live session_id like desktop GUI; do not reconnect in a loop. " +
				"Answer in the user's language. Prefer Markdown.",
		),
	}
	// Point skill search/install at known HubCenter endpoints when available.
	// Private Hub base URL is recorded for local capability APIs.
	if centers := mobileOfficialHubCenterCandidates; len(centers) > 0 {
		cfg.RemoteHubCenterURL = centers[0]
		if len(centers) > 1 {
			cfg.RemoteHubCenterURLs = append([]string(nil), centers[1:]...)
		}
	}
	if hub := strings.TrimRight(strings.TrimSpace(hubBaseURL), "/"); hub != "" {
		cfg.SkillHubURLs = []corelib.SkillHubEntry{
			{Label: "hub", URL: hub, Type: "standard"},
		}
	}
	if hint := strings.TrimSpace(systemHint); hint != "" {
		cfg.MaclawRoleDescription = cfg.MaclawRoleDescription + "\n\n" + hint
	}
	return cfg
}

// mobileMergeUserAgentConfig keeps user MCP/skill/search settings while forcing
// Hub-proxied LLM credentials for the mobile official billing path.
func mobileMergeUserAgentConfig(userCfg, hubLLM corelib.AppConfig) corelib.AppConfig {
	out := userCfg
	out.MaclawLLMUrl = hubLLM.MaclawLLMUrl
	out.MaclawLLMKey = hubLLM.MaclawLLMKey
	out.MaclawLLMModel = hubLLM.MaclawLLMModel
	out.MaclawLLMProtocol = hubLLM.MaclawLLMProtocol
	if out.MaclawAgentMaxIterations <= 0 {
		out.MaclawAgentMaxIterations = hubLLM.MaclawAgentMaxIterations
	}
	if strings.TrimSpace(out.MaclawRoleName) == "" {
		out.MaclawRoleName = hubLLM.MaclawRoleName
	}
	if strings.TrimSpace(out.MaclawRoleDescription) == "" {
		out.MaclawRoleDescription = hubLLM.MaclawRoleDescription
	} else if strings.TrimSpace(hubLLM.MaclawRoleDescription) != "" {
		out.MaclawRoleDescription = out.MaclawRoleDescription + "\n\n" + hubLLM.MaclawRoleDescription
	}
	return out
}

// mobileMergeSSHHosts prefers vault-injected hosts for duplicate labels.
func mobileMergeSSHHosts(existing, injected []corelib.SSHHostEntry) []corelib.SSHHostEntry {
	if len(injected) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return append([]corelib.SSHHostEntry(nil), injected...)
	}
	seen := make(map[string]struct{}, len(injected)+len(existing))
	out := make([]corelib.SSHHostEntry, 0, len(injected)+len(existing))
	for _, h := range injected {
		key := strings.ToLower(strings.TrimSpace(h.Label))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	for _, h := range existing {
		key := strings.ToLower(strings.TrimSpace(h.Label))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	return out
}

func mobileAgentUserDataDir(tenantID, userID string) (string, error) {
	root := mobileCoreAgentDataRoot()
	// Sanitize path segments (no traversal).
	tenantID = sanitizePathSegment(tenantID)
	userID = sanitizePathSegment(userID)
	dir := filepath.Join(root, "tenants", tenantID, "users", userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create agent data dir: %w", err)
	}
	return dir, nil
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "_unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == '@' || r == ':':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "." || out == ".." || out == "" {
		return "_unknown"
	}
	return out
}

func mobileSplitAgentMessages(base []map[string]string) (userText string, history []agentservice.Message, systemHint string) {
	now := time.Now().UTC()
	var users []string
	for i, m := range base {
		role := strings.ToLower(strings.TrimSpace(m["role"]))
		content := strings.TrimSpace(m["content"])
		if content == "" {
			continue
		}
		switch role {
		case "system":
			if systemHint == "" {
				systemHint = content
			} else {
				systemHint += "\n\n" + content
			}
		case "assistant":
			history = append(history, agentservice.Message{
				ID: fmt.Sprintf("hist-a-%d", i), Role: agentservice.MessageRoleAssistant,
				Content: content, CreatedAt: now,
			})
		case "user":
			users = append(users, content)
			history = append(history, agentservice.Message{
				ID: fmt.Sprintf("hist-u-%d", i), Role: agentservice.MessageRoleUser,
				Content: content, CreatedAt: now,
			})
		}
	}
	if len(users) == 0 {
		return "", history, systemHint
	}
	// Last user turn is the current message; drop it from history for RunLoop.
	userText = users[len(users)-1]
	if len(history) > 0 {
		last := history[len(history)-1]
		if last.Role == agentservice.MessageRoleUser && last.Content == userText {
			history = history[:len(history)-1]
		}
	}
	return userText, history, systemHint
}

// mobileHubLLMTransport sends OpenAI-compatible chat requests through the Hub
// official LLM handler (viewer auth) or delegated third-party credentials.
type mobileHubLLMTransport struct {
	base         *http.Request
	officialLLM  http.Handler
	delegated    mobileLlmAuthorizationRecord
	useDelegated bool

	mu        sync.Mutex
	lastReqID string
}

func (t *mobileHubLLMTransport) lastRequestID() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastReqID
}

func (t *mobileHubLLMTransport) rememberRequestID(resp *http.Response) {
	if t == nil || resp == nil {
		return
	}
	id := strings.TrimSpace(resp.Header.Get("X-MaClaw-Request-ID"))
	if id == "" {
		id = strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	}
	if id == "" {
		return
	}
	t.mu.Lock()
	t.lastReqID = id
	t.mu.Unlock()
}

func (t *mobileHubLLMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil {
		return nil, fmt.Errorf("nil transport")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	if t.useDelegated {
		resp, err := t.roundTripDelegated(req.Context(), body)
		if err == nil {
			t.rememberRequestID(resp)
		}
		return resp, err
	}
	if t.officialLLM == nil {
		return nil, fmt.Errorf("official LLM handler is not configured")
	}
	if t.base == nil {
		return nil, fmt.Errorf("base request is required for viewer LLM auth")
	}

	clone := t.base.Clone(req.Context())
	clone.Method = http.MethodPost
	urlCopy := *clone.URL
	clone.URL = &urlCopy
	clone.URL.Path = "/api/llm/v1/chat/completions"
	clone.RequestURI = clone.URL.RequestURI()
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	clone.Header = t.base.Header.Clone()
	clone.Header.Set("Content-Type", "application/json")
	// Agent may set stream:true; Hub handler supports it.
	rec := httptest.NewRecorder()
	t.officialLLM.ServeHTTP(rec, clone)
	resp := rec.Result()
	// Ensure body is readable for llm client.
	resp.Body = io.NopCloser(bytes.NewReader(rec.Body.Bytes()))
	// Propagate request id from handler to agent client consumers.
	if id := rec.Header().Get("X-MaClaw-Request-ID"); id != "" {
		resp.Header.Set("X-MaClaw-Request-ID", id)
	}
	t.rememberRequestID(resp)
	return resp, nil
}

func (t *mobileHubLLMTransport) roundTripDelegated(ctx context.Context, body []byte) (*http.Response, error) {
	// Reuse existing mobile delegated chat path by POSTing to provider URL.
	url := strings.TrimSpace(t.delegated.ProviderURL)
	if url == "" {
		return nil, fmt.Errorf("delegated provider URL is empty")
	}
	// Normalize to chat/completions if needed.
	if !strings.Contains(url, "/chat/completions") {
		url = strings.TrimRight(url, "/") + "/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(t.delegated.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 6 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// mobileTryCoreAgent prefers the shared corelib agentservice path. On failure
// it logs and returns ok=false so callers may fall back to the legacy mini-loop.
func mobileTryCoreAgent(
	ctx context.Context,
	r *http.Request,
	principal *auth.ViewerPrincipal,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	baseMessages []map[string]string,
	emit mobileAgentEventWriter,
	attachments []agent.MessageAttachment,
) (answer, requestID string, ok bool) {
	answer, requestID, err := mobileRunCoreAgent(ctx, r, principal, officialLLM, delegated, useDelegated, baseMessages, emit, attachments)
	if err != nil {
		log.Printf("[mobile-core-agent] fallback to legacy loop: %v", err)
		return "", "", false
	}
	return answer, requestID, true
}
