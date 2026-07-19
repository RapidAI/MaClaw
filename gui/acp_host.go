package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/acpagent"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// Mode B: GUI hosts industry ACP (Agent Client Protocol) on loopback so
// VS Code / maclaw-acp-bridge can use the desktop AI assistant as a
// programming agent (session cwd → project_path).
//
// Transport: NDJSON JSON-RPC over TCP (127.0.0.1 only).

const (
	acpHostDefaultBind     = "127.0.0.1"
	acpHostPreferredPort   = 18789 // default preferred when config port is 0 but we want sticky
	acpHostTokenBytes      = 24
	acpHostAgentName       = "maclaw-gui-ai-assistant"
)

// acpHostEndpoint is written to <MaclawBaseDir>/acp/endpoint.json for discovery.
type acpHostEndpoint struct {
	URL       string `json:"url"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Protocol  string `json:"protocol"` // "acp-ndjson-tcp"
	Agent     string `json:"agent"`
	UpdatedAt string `json:"updatedAt"`
}

type acpHost struct {
	app *App

	mu       sync.Mutex
	listener net.Listener
	token    string
	port     int
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	// Active client connections — closed on Stop so serveConn does not hang.
	conns map[net.Conn]struct{}
}

func (a *App) ensureACPHost() {
	if a == nil {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[acp-host] load config: %v", err)
		return
	}
	if !cfg.IsAcpHostEnabled() {
		a.stopACPHost()
		return
	}
	want := cfg.PreferredAcpHostPort()

	a.acpHostMu.Lock()
	if a.acpHost != nil {
		// Restart only when a sticky preferred port is set and differs.
		if want > 0 && a.acpHost.port != want {
			old := a.acpHost
			a.acpHost = nil
			a.acpHostMu.Unlock()
			old.Stop()
			a.acpHostMu.Lock()
			// Another goroutine may have started a host while we were stopped.
			if a.acpHost != nil {
				a.acpHostMu.Unlock()
				return
			}
		} else {
			a.acpHostMu.Unlock()
			return
		}
	}
	h := &acpHost{app: a, conns: make(map[net.Conn]struct{})}
	if err := h.Start(want); err != nil {
		a.acpHostMu.Unlock()
		log.Printf("[acp-host] start failed: %v", err)
		return
	}
	a.acpHost = h
	addr := h.Addr()
	a.acpHostMu.Unlock()
	log.Printf("[acp-host] Mode B listening on %s (AI assistant programming agent)", addr)
}

func (a *App) syncACPHostFromConfig() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.IsAcpHostEnabled() {
		a.stopACPHost()
		clearACPHostDiscovery()
		log.Printf("[acp-host] Mode B disabled by config")
		return
	}
	// Force recreate to pick up port changes.
	a.stopACPHost()
	a.ensureACPHost()
}

func (a *App) stopACPHost() {
	a.acpHostMu.Lock()
	h := a.acpHost
	a.acpHost = nil
	a.acpHostMu.Unlock()
	if h != nil {
		h.Stop()
	}
}

// GetACPHostStatus returns Mode B readiness for utilities/settings UI.
func (a *App) GetACPHostStatus() map[string]any {
	cfg, err := a.LoadConfig()
	enabled := true
	portPref := 0
	mirror := true
	if err == nil {
		enabled = cfg.IsAcpHostEnabled()
		portPref = cfg.PreferredAcpHostPort()
		mirror = cfg.IsAcpHostMirrorUI()
	}
	host, port, _, discoveryOK := ReadACPHostEndpoint()
	addr := ""
	a.acpHostMu.Lock()
	runningLocal := a.acpHost != nil
	if a.acpHost != nil {
		addr = a.acpHost.Addr()
		if port == 0 {
			port = a.acpHost.port
		}
		if host == "" {
			host = "127.0.0.1"
		}
	}
	a.acpHostMu.Unlock()
	return map[string]any{
		"enabled":       enabled,
		// Process-local host is authoritative; discovery files may lag on first start.
		"running":       runningLocal || discoveryOK,
		"address":       addr,
		"host":          host,
		"port":          port,
		"preferredPort": portPref,
		"mirrorUI":      mirror,
		"agent":         acpHostAgentName,
		"discoveryDir":  acpDiscoveryDir(),
		"discoveryOK":   discoveryOK,
		"protocol":      "acp-ndjson-tcp",
		"description":   "VS Code programming agent uses MaClaw GUI AI assistant",
	}
}

// RestartACPHost restarts Mode B (settings UI).
func (a *App) RestartACPHost() map[string]any {
	a.syncACPHostFromConfig()
	return a.GetACPHostStatus()
}

func (h *acpHost) Addr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		return ""
	}
	return h.listener.Addr().String()
}

func (h *acpHost) Start(preferredPort int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener != nil {
		return nil
	}
	token, err := randomACPToken()
	if err != nil {
		return err
	}

	// Bind strategy:
	//   preferredPort > 0 → STRICT that port only (no silent fallback)
	//   preferredPort == 0 → try 18789, then ephemeral :0
	var ln net.Listener
	tryPorts := []int{}
	strict := preferredPort > 0
	if strict {
		tryPorts = append(tryPorts, preferredPort)
	} else {
		tryPorts = append(tryPorts, acpHostPreferredPort, 0)
	}

	var lastErr error
	var tcpAddr *net.TCPAddr
	for _, p := range tryPorts {
		addr := fmt.Sprintf("%s:%d", acpHostDefaultBind, p)
		if p == 0 {
			addr = acpHostDefaultBind + ":0"
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			lastErr = err
			log.Printf("[acp-host] bind %s failed: %v", addr, err)
			continue
		}
		ta, ok := l.Addr().(*net.TCPAddr)
		if !ok || ta.Port <= 0 {
			_ = l.Close()
			lastErr = fmt.Errorf("invalid bind address")
			continue
		}
		ln = l
		tcpAddr = ta
		break
	}
	if ln == nil || tcpAddr == nil {
		if strict && lastErr != nil {
			return fmt.Errorf("acp host strict port %d unavailable: %w (set acp_host_port=0 for auto)", preferredPort, lastErr)
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("failed to bind acp host")
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.listener = ln
	h.token = token
	h.port = tcpAddr.Port
	h.cancel = cancel

	if err := writeACPHostDiscovery(tcpAddr.Port, token); err != nil {
		cancel()
		_ = ln.Close()
		h.listener = nil
		return err
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.acceptLoop(ctx, ln)
	}()
	return nil
}

func (h *acpHost) Stop() {
	h.mu.Lock()
	cancel := h.cancel
	ln := h.listener
	conns := h.conns
	h.listener = nil
	h.cancel = nil
	h.conns = nil
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	for c := range conns {
		_ = c.Close()
	}
	h.wg.Wait()
	clearACPHostDiscovery()
}

func (h *acpHost) trackConn(c net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns == nil {
		h.conns = map[net.Conn]struct{}{}
	}
	h.conns[c] = struct{}{}
}

func (h *acpHost) untrackConn(c net.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns != nil {
		delete(h.conns, c)
	}
}

func (h *acpHost) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					time.Sleep(50 * time.Millisecond)
					continue
				}
				log.Printf("[acp-host] accept: %v", err)
				return
			}
		}
		if !isLoopbackAddr(conn.RemoteAddr()) {
			log.Printf("[acp-host] reject non-loopback %v", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		h.trackConn(conn)
		h.wg.Add(1)
		go func(c net.Conn) {
			defer h.wg.Done()
			defer h.untrackConn(c)
			defer c.Close()
			h.serveConn(ctx, c)
		}(conn)
	}
}

func (h *acpHost) serveConn(ctx context.Context, c net.Conn) {
	// Stop() closes tracked connections so ReadMessage unblocks with EOF/error.
	conn := acpagent.NewConn(c, c)
	h.mu.Lock()
	tok := h.token
	h.mu.Unlock()
	sess := newACPHostSession(h.app, tok, conn)
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := conn.ReadMessage()
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "EOF") || ctx.Err() != nil {
				return
			}
			if strings.Contains(err.Error(), "empty line") {
				continue
			}
			// net.OpError after Close during Stop is expected.
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			log.Printf("[acp-host] read: %v", err)
			return
		}
		if msg.Response != nil {
			sess.deliverClientResponse(*msg.Response)
			continue
		}
		if msg.Request == nil {
			continue
		}
		if err := sess.handle(*msg.Request); err != nil {
			log.Printf("[acp-host] handle %s: %v", msg.Request.Method, err)
		}
	}
}

// --- session / methods ---

type acpHostSession struct {
	app    *App
	token  string
	conn   *acpagent.Conn
	authed bool

	mu       sync.Mutex
	sessions map[string]*acpHostAgentSession
	cancels  map[string]context.CancelFunc
	promptMu map[string]*sync.Mutex

	// reverse RPC (agent → client): permission etc.
	nextClientID atomic.Int64
	pendingMu    sync.Mutex
	pending      map[string]chan acpagent.Response

	// allow_always: sessionId → toolName → true (TCP connection lifetime)
	allowAlwaysMu sync.Mutex
	allowAlways   map[string]map[string]bool
}

type acpHostAgentSession struct {
	ID      string
	Cwd     string
	UserID  string
	Created time.Time
}

func newACPHostSession(app *App, token string, conn *acpagent.Conn) *acpHostSession {
	return &acpHostSession{
		app:      app,
		token:    token,
		conn:     conn,
		sessions: map[string]*acpHostAgentSession{},
		cancels:  map[string]context.CancelFunc{},
		promptMu: map[string]*sync.Mutex{},
		pending:  map[string]chan acpagent.Response{},
	}
}

func (s *acpHostSession) deliverClientResponse(resp acpagent.Response) {
	key := string(resp.ID)
	s.pendingMu.Lock()
	ch := s.pending[key]
	s.pendingMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

// callClient sends a JSON-RPC request to the ACP client (via bridge → VS Code).
func (s *acpHostSession) callClient(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s == nil || s.conn == nil {
		return nil, fmt.Errorf("no connection")
	}
	idNum := s.nextClientID.Add(1)
	idRaw, _ := json.Marshal(idNum)
	ch := make(chan acpagent.Response, 1)
	key := string(idRaw)
	s.pendingMu.Lock()
	s.pending[key] = ch
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, key)
		s.pendingMu.Unlock()
	}()
	if err := s.conn.WriteRequest(idRaw, method, params); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc %d: %s", resp.Error.Code, resp.Error.Message)
		}
		switch r := resp.Result.(type) {
		case json.RawMessage:
			return r, nil
		case nil:
			return json.RawMessage(`{}`), nil
		default:
			b, err := json.Marshal(r)
			if err != nil {
				return nil, err
			}
			return b, nil
		}
	}
}

func (s *acpHostSession) handle(req acpagent.Request) error {
	isNotif := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "initialize":
		result, rpcErr := s.onInitialize(req.Params)
		return s.reply(req, result, rpcErr)
	case "session/new":
		if !s.authed {
			return s.reply(req, nil, acpErr(acpagent.CodeInvalidRequest, "not authenticated: include authToken matching acp/token"))
		}
		result, rpcErr := s.onSessionNew(req.Params)
		return s.reply(req, result, rpcErr)
	case "session/prompt":
		if !s.authed {
			return s.reply(req, nil, acpErr(acpagent.CodeInvalidRequest, "not authenticated"))
		}
		reqCopy := req
		go func() {
			result, rpcErr := s.onSessionPrompt(reqCopy.Params)
			_ = s.reply(reqCopy, result, rpcErr)
		}()
		return nil
	case "session/cancel":
		if !s.authed {
			if !isNotif {
				return s.reply(req, nil, acpErr(acpagent.CodeInvalidRequest, "not authenticated"))
			}
			return nil
		}
		s.onSessionCancel(req.Params)
		if !isNotif {
			return s.reply(req, map[string]any{}, nil)
		}
		return nil
	case "session/steer":
		if !s.authed {
			return s.reply(req, nil, acpErr(acpagent.CodeInvalidRequest, "not authenticated"))
		}
		result, rpcErr := s.onSessionSteer(req.Params)
		return s.reply(req, result, rpcErr)
	case "authenticated", "initialized", "notifications/initialized":
		if !isNotif {
			return s.reply(req, map[string]any{}, nil)
		}
		return nil
	default:
		if isNotif {
			return nil
		}
		return s.reply(req, nil, acpErr(acpagent.CodeMethodNotFound, "method not found: "+req.Method))
	}
}

func (s *acpHostSession) reply(req acpagent.Request, result any, rpcErr *acpagent.RPCError) error {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}
	return s.conn.WriteResponse([]byte(req.ID), result, rpcErr)
}

func acpErr(code int, msg string) *acpagent.RPCError {
	return &acpagent.RPCError{Code: code, Message: msg}
}

func (s *acpHostSession) onInitialize(raw json.RawMessage) (any, *acpagent.RPCError) {
	var params struct {
		acpagent.InitializeParams
		AuthToken string `json:"authToken"`
		Token     string `json:"token"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	tok := strings.TrimSpace(params.AuthToken)
	if tok == "" {
		tok = strings.TrimSpace(params.Token)
	}
	if tok == "" && len(raw) > 0 {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			if v, ok := m["authToken"].(string); ok {
				tok = strings.TrimSpace(v)
			}
			if tok == "" {
				if v, ok := m["token"].(string); ok {
					tok = strings.TrimSpace(v)
				}
			}
		}
	}
	if !secureTokenEqual(tok, s.token) {
		return nil, acpErr(acpagent.CodeInvalidRequest, "invalid authToken")
	}
	s.authed = true
	return acpagent.InitializeResult{
		ProtocolVersion:   acpagent.ProtocolVersion,
		AgentCapabilities: acpagent.DefaultAgentCapabilities(),
		AgentInfo: acpagent.ImplementationInfo{
			Name:    acpHostAgentName,
			Title:   "MaClaw GUI AI Assistant (Programming)",
			Version: "mode-b-1",
		},
	}, nil
}

func (s *acpHostSession) onSessionNew(raw json.RawMessage) (any, *acpagent.RPCError) {
	var params acpagent.SessionNewParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
		}
	}
	cwd := strings.TrimSpace(params.Cwd)
	if cwd == "" {
		cwd = corelib.EffectiveWorkspaceDir()
	}
	id := "acp_gui_" + randomHexID(10)
	userID := desktopAIAssistantUserIDForProjectPath(normalizeProjectSessionPath(cwd))
	s.mu.Lock()
	s.sessions[id] = &acpHostAgentSession{
		ID:      id,
		Cwd:     cwd,
		UserID:  userID,
		Created: time.Now(),
	}
	s.promptMu[id] = &sync.Mutex{}
	s.mu.Unlock()
	return acpagent.SessionNewResult{SessionID: id}, nil
}

func (s *acpHostSession) onSessionCancel(raw json.RawMessage) {
	var params acpagent.SessionCancelParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return
	}
	s.mu.Lock()
	cancel := s.cancels[sid]
	sess := s.sessions[sid]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if sess != nil && s.app != nil {
		_, _ = s.app.CancelAIAssistantSessionForSession(sess.UserID)
		if sess.Cwd != "" {
			s.app.cancelProjectTaskLoop(sess.Cwd)
		}
	}
}

// onSessionSteer injects guide-launch text into the session's running agent
// loop — the same semantics as the GUI input-buffer 引导发射: the text lands
// as a user-role supplement the loop drains between iterations, without
// cancelling the current turn. accepted=false tells the client to fall back
// to queueing for the next turn.
func (s *acpHostSession) onSessionSteer(raw json.RawMessage) (any, *acpagent.RPCError) {
	var params acpagent.SessionSteerParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
	}
	sid := strings.TrimSpace(params.SessionID)
	text := strings.TrimSpace(params.Text)
	if sid == "" || text == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "sessionId and text required")
	}
	s.mu.Lock()
	sess := s.sessions[sid]
	s.mu.Unlock()
	if sess == nil {
		return nil, acpErr(acpagent.CodeInvalidParams, "unknown sessionId")
	}
	if s.app == nil {
		return map[string]any{"accepted": false}, nil
	}
	s.app.ensureInteractionInfra()
	hubClient := s.app.ensureHubClient()
	if hubClient == nil {
		return map[string]any{"accepted": false}, nil
	}
	accepted := hubClient.ensureIMHandler().InjectGuideReference(sess.UserID, text)
	return map[string]any{"accepted": accepted}, nil
}

func (s *acpHostSession) onSessionPrompt(raw json.RawMessage) (any, *acpagent.RPCError) {
	var params acpagent.SessionPromptParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, acpErr(acpagent.CodeInvalidParams, err.Error())
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "sessionId required")
	}
	text := acpagent.PromptText(params.Prompt)
	if text == "" {
		return nil, acpErr(acpagent.CodeInvalidParams, "empty prompt")
	}

	s.mu.Lock()
	sess := s.sessions[sid]
	pm := s.promptMu[sid]
	s.mu.Unlock()
	if sess == nil {
		return nil, acpErr(acpagent.CodeInvalidParams, "unknown sessionId")
	}
	if pm == nil {
		pm = &sync.Mutex{}
		s.mu.Lock()
		s.promptMu[sid] = pm
		s.mu.Unlock()
	}
	if !pm.TryLock() {
		return nil, acpErr(acpagent.CodeInvalidParams, "session busy")
	}
	defer pm.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s.mu.Lock()
	if prev := s.cancels[sid]; prev != nil {
		prev()
	}
	s.cancels[sid] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, sid)
		s.mu.Unlock()
	}()

	requestID := "acp-" + sid + "-" + randomHexID(6)
	mirrorUI := true
	if cfg, err := s.app.LoadConfig(); err == nil {
		mirrorUI = cfg.IsAcpHostMirrorUI()
	}

	// Mirror user prompt into GUI AI assistant surface (optional).
	if mirrorUI {
		s.app.mirrorACPToGUI("user", text, requestID, sess.UserID, sess.Cwd)
	}

	// VS Code ACP clients only render assistant text from session/update
	// agent_message_chunk notifications — end_turn alone leaves an empty bubble.
	//
	// tokenChunks: model stream only (drives final-text flush decision).
	// Diff cards / tool side-channel use writeContent and must NOT suppress the
	// final assistant summary flush when the model never streamed tokens.
	var tokenChunks atomic.Int32
	writeContent := func(chunk string, countAsAnswer bool) {
		if strings.TrimSpace(chunk) == "" {
			return
		}
		if err := s.conn.WriteNotification("session/update", acpagent.SessionUpdateParams{
			SessionID: sid,
			Update: map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": chunk},
			},
		}); err != nil {
			log.Printf("[acp-host] session/update content write failed session=%s: %v", sid, err)
			return
		}
		if countAsAnswer {
			tokenChunks.Add(1)
		}
	}
	emitThought := func(chunk string) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			return
		}
		// Progress as thought — must NOT use agent_message_chunk or clients
		// treat status lines as the whole assistant answer.
		if err := s.conn.WriteNotification("session/update", acpagent.SessionUpdateParams{
			SessionID: sid,
			Update: map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]any{"type": "text", "text": chunk + "\n"},
			},
		}); err != nil {
			log.Printf("[acp-host] session/update thought write failed session=%s: %v", sid, err)
		}
	}

	onToken := func(delta string) {
		if strings.TrimSpace(delta) == "" {
			return
		}
		// Stream deltas with original whitespace (model tokens).
		if err := s.conn.WriteNotification("session/update", acpagent.SessionUpdateParams{
			SessionID: sid,
			Update: map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": delta},
			},
		}); err != nil {
			log.Printf("[acp-host] session/update token write failed session=%s: %v", sid, err)
		} else {
			tokenChunks.Add(1)
		}
		if mirrorUI {
			s.app.mirrorACPToken(requestID, sess.UserID, delta)
		}
	}
	onProgress := func(progressText string) {
		progressText = strings.TrimSpace(progressText)
		if progressText == "" || progressText == imHeartbeatMsg {
			return
		}
		if !isVisibleAIAssistantProgressText(progressText) {
			return
		}
		emitThought(progressText)
		if mirrorUI {
			s.app.mirrorACPProgress(requestID, sess.UserID, progressText)
		}
	}
	onToolEvent := func(ev ACPToolEvent) {
		// Capture pre-image for write tools before start completes on client UI.
		if strings.EqualFold(ev.Phase, "start") {
			globalACPWriteSnaps.capture(requestID, ev.Name, ev.ArgsJSON, sess.Cwd)
		}
		update := acpToolEventToUpdate(ev)
		// Attach diff content for edit tools when ending (Cursor-like review in chat).
		if strings.EqualFold(ev.Phase, "end") && nameIsWriteTool(ev.Name) && ev.OK {
			if card := buildACPWriteDiffSummary(requestID, ev.Name, ev.ArgsJSON, sess.Cwd); card != "" {
				update["content"] = []map[string]any{
					{"type": "text", "text": card},
				}
				// Side-channel markdown (does not suppress final assistant flush).
				writeContent("\n"+card, false)
			}
		}
		if err := s.conn.WriteNotification("session/update", acpagent.SessionUpdateParams{
			SessionID: sid,
			Update:    update,
		}); err != nil {
			log.Printf("[acp-host] tool_call write failed session=%s tool=%s: %v", sid, ev.Name, err)
		}
	}
	// Cursor-like permission: VS Code QuickPick via session/request_permission.
	// Capture the prompt's session id/cwd so allow_always is scoped correctly.
	promptSid, promptCwd := sid, sess.Cwd
	clearPerm := globalACPPermission.set(requestID, func(pctx context.Context, toolName, argsJSON string) (bool, string) {
		return s.requestClientPermission(pctx, promptSid, toolName, argsJSON, promptCwd)
	})
	defer clearPerm()
	defer globalACPWriteSnaps.clearRequest(requestID)

	resp, err := s.app.RunAIAssistantProgrammingPrompt(ctx, AIAssistantSendRequest{
		Text:        text,
		ProjectPath: sess.Cwd,
		RequestID:   requestID,
		Lang:        s.app.CurrentLanguage,
	}, AIAssistantExternalCallbacks{
		OnToken:     onToken,
		OnProgress:  onProgress,
		OnToolEvent: onToolEvent,
	})

	if ctx.Err() != nil {
		return acpagent.SessionPromptResult{StopReason: acpagent.StopCancelled}, nil
	}
	if err != nil {
		// Still try to surface the error text to the client.
		writeContent(err.Error(), true)
		return nil, acpErr(acpagent.CodeInternalError, err.Error())
	}
	if resp != nil {
		finalText := strings.TrimSpace(resp.Text)
		if finalText == "" {
			finalText = strings.TrimSpace(resp.Error)
		}
		// Non-streaming paths (chit-chat, slash, confirmations, some handlers)
		// never call onToken — must flush or VS Code shows empty assistant.
		// Diff cards do not increment tokenChunks, so they won't suppress this.
		if finalText != "" && tokenChunks.Load() == 0 {
			writeContent(finalText, true)
			log.Printf("[acp-host] flushed final content session=%s request=%s chars=%d",
				sid, requestID, len([]rune(finalText)))
		}
		if mirrorUI {
			s.app.mirrorACPFinal(requestID, sess.UserID, resp)
		}
	} else if tokenChunks.Load() == 0 {
		writeContent("(empty response)", true)
	}
	return acpagent.SessionPromptResult{StopReason: acpagent.StopEndTurn}, nil
}

// --- GUI mirror helpers ---
//
// Frontend useAIAssistant only attaches tokens/responses to an in-flight round.
// Emitting ai-assistant-foreground-round-started (same path as goal continuation)
// creates user + assistant placeholders keyed by request_id so subsequent
// token/progress/response events land in the same chat bubbles.

// acpMirrorUISessionKey returns the UI session key for Mode B traffic.
// Prefer project-scoped desktop-user:<path> so the programming agent lands in
// the matching project tab; fall back to main assistant when no cwd.
func acpMirrorUISessionKey(projectPath, backendSessionKey string) string {
	if sk := strings.TrimSpace(backendSessionKey); sk != "" {
		return sk
	}
	if p := normalizeProjectSessionPath(projectPath); p != "" {
		return desktopAIAssistantUserIDForProjectPath(p)
	}
	return "desktop-user"
}

func (a *App) mirrorACPToGUI(role, text, requestID, sessionKey, projectPath string) {
	if a == nil {
		return
	}
	uiSession := acpMirrorUISessionKey(projectPath, sessionKey)
	payload, err := json.Marshal(map[string]any{
		"source":       "acp-mode-b",
		"role":         role,
		"text":         text,
		"request_id":   requestID,
		"session_key":  uiSession,
		"project_path": projectPath,
	})
	if err != nil {
		return
	}
	// Panel opens/activates project tab before (or as) the round starts.
	a.emitEvent("acp-mode-b-message", string(payload))

	if role != "user" {
		return
	}
	display := "〔VS Code / ACP〕" + text
	if projectPath != "" {
		display = "〔VS Code / ACP · " + filepath.Base(projectPath) + "〕" + text
	}
	// Create a real foreground round (request_id-matched tokens/response).
	// Emit immediately: useAIAssistant accepts acp- rounds without active-tab
	// match, so a delay only raced early tokens/progress against an empty
	// in-flight map. Project-tab open still runs via acp-mode-b-message above.
	roundEvt, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:   requestID,
		Text:        text,
		SessionKey:  uiSession,
		DisplayText: display,
	})
	if err == nil {
		a.emitEvent("ai-assistant-foreground-round-started", string(roundEvt))
	}
	a.appendACPRoundToUIState(display, requestID, uiSession)
}

func (a *App) mirrorACPToken(requestID, sessionKey, delta string) {
	uiSession := acpMirrorUISessionKey("", sessionKey)
	payload, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:  requestID,
		Text:       delta,
		SessionKey: uiSession,
	})
	if err != nil {
		return
	}
	a.emitEvent("ai-assistant-token", string(payload))
}

func (a *App) mirrorACPProgress(requestID, sessionKey, text string) {
	uiSession := acpMirrorUISessionKey("", sessionKey)
	payload, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:  requestID,
		Text:       text,
		SessionKey: uiSession,
	})
	if err != nil {
		return
	}
	a.emitEvent("ai-assistant-progress", string(payload))
}

func (a *App) mirrorACPFinal(requestID, sessionKey string, resp *IMAgentResponse) {
	if resp == nil {
		return
	}
	uiSession := acpMirrorUISessionKey("", sessionKey)
	resp.RequestID = requestID
	resp.SessionKey = uiSession
	_ = a.emitAIAssistantResponse(requestID, resp)

	meta, _ := json.Marshal(map[string]any{
		"source":       "acp-mode-b",
		"request_id":   requestID,
		"session_key":  uiSession,
		"has_text":     strings.TrimSpace(resp.Text) != "",
		"has_error":    strings.TrimSpace(resp.Error) != "",
	})
	a.emitEvent("acp-mode-b-done", string(meta))

	finalText := strings.TrimSpace(resp.Text)
	if finalText == "" {
		finalText = strings.TrimSpace(resp.Error)
	}
	if finalText != "" {
		a.patchACPAssistantUIState(requestID, finalText)
	}
}

func (a *App) appendACPRoundToUIState(userDisplay, requestID, uiSession string) {
	if a == nil || strings.TrimSpace(userDisplay) == "" || strings.TrimSpace(requestID) == "" {
		return
	}
	if uiSession == "" {
		uiSession = "desktop-user"
	}
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()
	path := a.aiAssistantUIStatePath()
	state := AIAssistantUIState{Messages: []map[string]interface{}{}, Prompts: []string{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	now := float64(time.Now().UnixMilli())
	state.Messages = append(state.Messages,
		map[string]interface{}{
			"id":         "acp-user-" + requestID,
			"role":       "user",
			"content":    userDisplay,
			"sessionKey": uiSession,
			"timestamp":  now,
			"source":     "acp-mode-b",
		},
		map[string]interface{}{
			"id":         "acp-asst-" + requestID,
			"role":       "assistant",
			"content":    "",
			"requestId":  requestID,
			"sessionKey": uiSession,
			"timestamp":  now + 1,
			"source":     "acp-mode-b",
		},
	)
	normalizeAIAssistantUIState(&state)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = writeAIAssistantUIStateUnlocked(path, state)
}

func (a *App) patchACPAssistantUIState(requestID, assistantText string) {
	if a == nil || requestID == "" || assistantText == "" {
		return
	}
	aiAssistantUIStateMu.Lock()
	defer aiAssistantUIStateMu.Unlock()
	path := a.aiAssistantUIStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state AIAssistantUIState
	if json.Unmarshal(data, &state) != nil {
		return
	}
	changed := false
	for i := range state.Messages {
		msg := state.Messages[i]
		if msg == nil {
			continue
		}
		rid, _ := msg["requestId"].(string)
		if rid == "" {
			rid, _ = msg["request_id"].(string)
		}
		role, _ := msg["role"].(string)
		if rid == requestID && role == "assistant" {
			msg["content"] = assistantText
			changed = true
		}
	}
	if !changed {
		return
	}
	normalizeAIAssistantUIState(&state)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = writeAIAssistantUIStateUnlocked(path, state)
}

func writeAIAssistantUIStateUnlocked(path string, state AIAssistantUIState) error {
	state.StoragePath = ""
	return configfile.AtomicWriteJSON(path, state)
}

// --- discovery files ---

func acpDiscoveryDir() string {
	return filepath.Join(corelib.MaclawBaseDir(), "acp")
}

func writeACPHostDiscovery(port int, token string) error {
	dir := acpDiscoveryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ep := acpHostEndpoint{
		URL:       fmt.Sprintf("tcp://127.0.0.1:%d", port),
		Host:      "127.0.0.1",
		Port:      port,
		PID:       os.Getpid(),
		Protocol:  "acp-ndjson-tcp",
		Agent:     acpHostAgentName,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(ep, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "endpoint.json"), data, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return nil
}

func clearACPHostDiscovery() {
	dir := acpDiscoveryDir()
	_ = os.Remove(filepath.Join(dir, "endpoint.json"))
	_ = os.Remove(filepath.Join(dir, "token"))
}

func randomACPToken() (string, error) {
	buf := make([]byte, acpHostTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomHexID(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func secureTokenEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	// ConstantTimeCompare returns 0 on length mismatch (no panic); keep the
	// empty checks above so blank tokens never authenticate.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isLoopbackAddr(addr net.Addr) bool {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ReadACPHostEndpoint loads discovery for bridge/tools.
func ReadACPHostEndpoint() (host string, port int, token string, ok bool) {
	dir := acpDiscoveryDir()
	raw, err := os.ReadFile(filepath.Join(dir, "endpoint.json"))
	if err != nil {
		return "", 0, "", false
	}
	var ep acpHostEndpoint
	if json.Unmarshal(raw, &ep) != nil || ep.Port <= 0 {
		return "", 0, "", false
	}
	tokb, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil {
		return "", 0, "", false
	}
	tok := strings.TrimSpace(string(tokb))
	if tok == "" {
		return "", 0, "", false
	}
	if ep.PID > 0 && !processAlive(ep.PID) {
		clearACPHostDiscovery()
		return "", 0, "", false
	}
	h := ep.Host
	if h == "" {
		h = "127.0.0.1"
	}
	return h, ep.Port, tok, true
}
