package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ModeBEndpoint is the GUI-hosted ACP discovery record.
type ModeBEndpoint struct {
	Host  string
	Port  int
	Token string
	URL   string
	Agent string
}

// DiscoverModeB reads <MaclawBaseDir>/acp/endpoint.json + token.
// Returns false when discovery files are missing, token empty, or recorded PID is dead (stale).
func DiscoverModeB() (ModeBEndpoint, bool) {
	dir := filepath.Join(corelib.MaclawBaseDir(), "acp")
	if d := strings.TrimSpace(os.Getenv("MACLAW_DATA_DIR")); d != "" {
		dir = filepath.Join(d, "acp")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "endpoint.json"))
	if err != nil {
		return ModeBEndpoint{}, false
	}
	var ep struct {
		URL   string `json:"url"`
		Host  string `json:"host"`
		Port  int    `json:"port"`
		PID   int    `json:"pid"`
		Agent string `json:"agent"`
	}
	if json.Unmarshal(raw, &ep) != nil || ep.Port <= 0 {
		return ModeBEndpoint{}, false
	}
	// Stale discovery after GUI crash: clear files so tools don't keep reporting
	// a dead Mode B endpoint, and refuse so the bridge fails/falls back cleanly.
	if ep.PID > 0 && !processAlive(ep.PID) {
		_ = os.Remove(filepath.Join(dir, "endpoint.json"))
		_ = os.Remove(filepath.Join(dir, "token"))
		return ModeBEndpoint{}, false
	}
	tokb, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil {
		return ModeBEndpoint{}, false
	}
	tok := strings.TrimSpace(string(tokb))
	if tok == "" {
		return ModeBEndpoint{}, false
	}
	host := strings.TrimSpace(ep.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	return ModeBEndpoint{
		Host:  host,
		Port:  ep.Port,
		Token: tok,
		URL:   strings.TrimSpace(ep.URL),
		Agent: strings.TrimSpace(ep.Agent),
	}, true
}

// ReverseRequestHandler handles agent→client JSON-RPC requests (e.g.
// session/request_permission). Return result or RPC error for the response.
type ReverseRequestHandler func(req Request) (result any, rpcErr *RPCError)

// ModeBClient is a thin ACP client over TCP NDJSON for GUI Mode B.
type ModeBClient struct {
	ep      ModeBEndpoint
	conn    net.Conn
	rw      *bufio.Reader
	writeMu sync.Mutex
	nextID  atomic.Int64
	// pending responses by id string (client→agent calls)
	pendingMu sync.Mutex
	pending   map[string]chan json.RawMessage
	// notifications fan-out
	onUpdate func(SessionUpdateParams)
	// reverse: agent→client requests (permission, etc.)
	onReverse ReverseRequestHandler
	closed    atomic.Bool
}

func DialModeB(ctx context.Context, ep ModeBEndpoint) (*ModeBClient, error) {
	if strings.TrimSpace(ep.Host) == "" {
		ep.Host = "127.0.0.1"
	}
	if ep.Port <= 0 {
		return nil, fmt.Errorf("invalid Mode B port")
	}
	if strings.TrimSpace(ep.Token) == "" {
		return nil, fmt.Errorf("empty Mode B token")
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	addr := net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	cli := &ModeBClient{
		ep:      ep,
		conn:    c,
		rw:      bufio.NewReaderSize(c, 1024*1024),
		pending: map[string]chan json.RawMessage{},
	}
	go cli.readLoop()
	// initialize + auth
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = cli.call(initCtx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientInfo":      map[string]any{"name": "maclaw-acp-bridge", "version": "0.2.0"},
		"authToken":       ep.Token,
	})
	if err != nil {
		cli.closed.Store(true)
		_ = c.Close()
		return nil, fmt.Errorf("mode-b initialize: %w", err)
	}
	return cli, nil
}

func (c *ModeBClient) SetUpdateHandler(fn func(SessionUpdateParams)) {
	c.onUpdate = fn
}

// SetReverseHandler handles agent-initiated requests (session/request_permission).
func (c *ModeBClient) SetReverseHandler(fn ReverseRequestHandler) {
	c.onReverse = fn
}

func (c *ModeBClient) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	return c.conn.Close()
}

func (c *ModeBClient) SessionNew(ctx context.Context, cwd string) (string, error) {
	raw, err := c.call(ctx, "session/new", map[string]any{"cwd": cwd})
	if err != nil {
		return "", err
	}
	var res SessionNewResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	return res.SessionID, nil
}

func (c *ModeBClient) SessionPrompt(ctx context.Context, sessionID string, prompt []ContentBlock) (string, error) {
	raw, err := c.call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    prompt,
	})
	if err != nil {
		return "", err
	}
	var res SessionPromptResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	return res.StopReason, nil
}

func (c *ModeBClient) SessionCancel(sessionID string) error {
	return c.notify("session/cancel", map[string]any{"sessionId": sessionID})
}

// SessionSteer forwards a guide-launch injection to the GUI host: the running
// agent loop drains the text between iterations without cancelling the turn.
func (c *ModeBClient) SessionSteer(ctx context.Context, sessionID string, text string) (bool, error) {
	raw, err := c.call(ctx, "session/steer", map[string]any{"sessionId": sessionID, "text": text})
	if err != nil {
		return false, err
	}
	var res SessionSteerResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, err
	}
	return res.Accepted, nil
}

func (c *ModeBClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	idRaw, _ := json.Marshal(id)
	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[string(idRaw)] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, string(idRaw))
		c.pendingMu.Unlock()
	}()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.write(msg); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw := <-ch:
		var wrap map[string]json.RawMessage
		if json.Unmarshal(raw, &wrap) == nil {
			if errRaw, ok := wrap["__rpc_error__"]; ok {
				var re RPCError
				if json.Unmarshal(errRaw, &re) == nil && re.Message != "" {
					return nil, fmt.Errorf("%s", re.Message)
				}
				return nil, fmt.Errorf("rpc error: %s", string(errRaw))
			}
		}
		return raw, nil
	}
}

func (c *ModeBClient) notify(method string, params any) error {
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (c *ModeBClient) write(v any) error {
	if c.closed.Load() {
		return fmt.Errorf("mode-b connection closed")
	}
	line, err := encodeLine(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.conn.Write(line)
	return err
}

func (c *ModeBClient) writeResponse(id json.RawMessage, result any, rpcErr *RPCError) error {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	if rpcErr != nil {
		resp.Result = nil
	}
	return c.write(resp)
}

func (c *ModeBClient) readLoop() {
	defer func() {
		// Unblock any waiters when the connection dies.
		c.pendingMu.Lock()
		for key, ch := range c.pending {
			select {
			case ch <- json.RawMessage(`{"__rpc_error__":{"code":-32000,"message":"connection closed"}}`):
			default:
			}
			delete(c.pending, key)
		}
		c.pendingMu.Unlock()
		c.closed.Store(true)
	}()
	for {
		line, err := c.rw.ReadBytes('\n')
		if err != nil {
			return
		}
		for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			continue
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(line, &envelope) != nil {
			continue
		}
		if methodRaw, ok := envelope["method"]; ok {
			var method string
			_ = json.Unmarshal(methodRaw, &method)
			idRaw, hasID := envelope["id"]
			// Agent → client reverse request (has method + id).
			// session/update is a notification (no id); if a peer mis-sends id, still fan-out update.
			if method == "session/update" {
				if c.onUpdate != nil {
					var params SessionUpdateParams
					if p, ok := envelope["params"]; ok {
						_ = json.Unmarshal(p, &params)
						c.onUpdate(params)
					}
				}
				continue
			}
			if hasID && hasRequestID(idRaw) && c.onReverse != nil && method != "" {
				req := Request{
					JSONRPC: "2.0",
					ID:      idRaw,
					Method:  method,
				}
				if p, ok := envelope["params"]; ok {
					req.Params = p
				}
				go func(r Request) {
					result, rpcErr := c.onReverse(r)
					_ = c.writeResponse(r.ID, result, rpcErr)
				}(req)
				continue
			}
			continue
		}
		if idRaw, ok := envelope["id"]; ok {
			key := string(idRaw)
			c.pendingMu.Lock()
			ch := c.pending[key]
			c.pendingMu.Unlock()
			if ch == nil {
				continue
			}
			if errRaw, ok := envelope["error"]; ok && len(errRaw) > 0 && string(errRaw) != "null" {
				// Encode error for call() to unwrap.
				wrapped, _ := json.Marshal(map[string]json.RawMessage{"__rpc_error__": errRaw})
				select {
				case ch <- wrapped:
				default:
				}
				continue
			}
			result := json.RawMessage(`{}`)
			if r, ok := envelope["result"]; ok {
				result = r
			}
			select {
			case ch <- result:
			default:
			}
		}
	}
}

// ServeStdioModeBProxy runs ACP stdio agent that proxies to GUI Mode B TCP.
// This is what VS Code should spawn: bridge attaches to live GUI AI assistant.
func ServeStdioModeBProxy(r io.Reader, w io.Writer, logger interface {
	Printf(string, ...any)
}) error {
	ep, ok := DiscoverModeB()
	if !ok {
		return fmt.Errorf("Mode B not available: start MaClaw GUI (acp/endpoint.json missing)")
	}
	// One retry: GUI may still be writing endpoint during startup.
	var client *ModeBClient
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		client, err = DialModeB(ctx, ep)
		cancel()
		if err == nil {
			break
		}
		if attempt == 0 {
			if logger != nil {
				logger.Printf("Mode B dial failed (%v); retrying once…", err)
			}
			time.Sleep(400 * time.Millisecond)
			if ep2, ok2 := DiscoverModeB(); ok2 {
				ep = ep2
			}
		}
	}
	if err != nil {
		return fmt.Errorf("dial Mode B: %w", err)
	}
	defer client.Close()

	stdio := NewConn(r, w)
	// map session id: bridge session == host session (transparent)
	// Forward updates from host to stdio client.
	client.SetUpdateHandler(func(p SessionUpdateParams) {
		_ = stdio.WriteNotification("session/update", p)
	})

	// Reverse RPC: host → bridge → VS Code (e.g. session/request_permission).
	// Responses arrive on stdio and must be demuxed by id (not treated as requests).
	var reverseMu sync.Mutex
	reversePending := map[string]chan Response{}

	client.SetReverseHandler(func(req Request) (any, *RPCError) {
		idKey := string(req.ID)
		ch := make(chan Response, 1)
		reverseMu.Lock()
		reversePending[idKey] = ch
		reverseMu.Unlock()
		defer func() {
			reverseMu.Lock()
			delete(reversePending, idKey)
			reverseMu.Unlock()
		}()
		var params any
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		if err := stdio.WriteRequest(req.ID, req.Method, params); err != nil {
			return nil, newRPCError(CodeInternalError, "forward reverse request: "+err.Error())
		}
		select {
		case resp := <-ch:
			if resp.Error != nil {
				return nil, resp.Error
			}
			return resp.Result, nil
		case <-time.After(5 * time.Minute):
			return nil, newRPCError(CodeInternalError, "client permission/request timeout")
		}
	})

	// local session bookkeeping: cancel + per-session prompt serialization
	var mu sync.Mutex
	cancels := map[string]context.CancelFunc{}
	promptMu := map[string]*sync.Mutex{}

	sessionPromptLock := func(sid string) *sync.Mutex {
		mu.Lock()
		defer mu.Unlock()
		m := promptMu[sid]
		if m == nil {
			m = &sync.Mutex{}
			promptMu[sid] = m
		}
		return m
	}

	for {
		msg, err := stdio.ReadMessage()
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "EOF") {
				return nil
			}
			if strings.Contains(err.Error(), "empty line") {
				continue
			}
			return err
		}
		if msg.Response != nil {
			key := string(msg.Response.ID)
			reverseMu.Lock()
			ch := reversePending[key]
			reverseMu.Unlock()
			if ch != nil {
				select {
				case ch <- *msg.Response:
				default:
				}
			}
			continue
		}
		if msg.Request == nil {
			continue
		}
		req := *msg.Request
		switch req.Method {
		case "initialize":
			// Local answer matching host agent identity (already authed to host).
			res := InitializeResult{
				ProtocolVersion:   ProtocolVersion,
				AgentCapabilities: DefaultAgentCapabilities(),
				AgentInfo: ImplementationInfo{
					Name:    "maclaw-gui-ai-assistant",
					Title:   "MaClaw GUI AI Assistant (Programming)",
					Version: "mode-b-proxy",
				},
			}
			_ = stdio.WriteResponse(req.ID, res, nil)
		case "session/new":
			var params SessionNewParams
			_ = json.Unmarshal(req.Params, &params)
			callCtx, callCancel := context.WithTimeout(context.Background(), 15*time.Second)
			hostID, err := client.SessionNew(callCtx, params.Cwd)
			callCancel()
			if err != nil {
				_ = stdio.WriteResponse(req.ID, nil, newRPCError(CodeInternalError, err.Error()))
				continue
			}
			_ = stdio.WriteResponse(req.ID, SessionNewResult{SessionID: hostID}, nil)
		case "session/prompt":
			reqCopy := req
			go func() {
				var params SessionPromptParams
				if err := json.Unmarshal(reqCopy.Params, &params); err != nil {
					_ = stdio.WriteResponse(reqCopy.ID, nil, newRPCError(CodeInvalidParams, err.Error()))
					return
				}
				sid := strings.TrimSpace(params.SessionID)
				lock := sessionPromptLock(sid)
				if !lock.TryLock() {
					_ = stdio.WriteResponse(reqCopy.ID, nil, newRPCError(CodeInvalidParams, "session busy: another prompt is in progress"))
					return
				}
				defer lock.Unlock()

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
				mu.Lock()
				if prev := cancels[sid]; prev != nil {
					prev()
				}
				cancels[sid] = cancel
				mu.Unlock()
				defer func() {
					cancel()
					mu.Lock()
					delete(cancels, sid)
					mu.Unlock()
				}()
				stop, err := client.SessionPrompt(ctx, sid, params.Prompt)
				// Prefer cancelled stopReason when client aborted (even if host returns an error).
				// Always notify the host so a timed-out/cancelled client does not leave a
				// programming-agent turn running in the GUI.
				if ctx.Err() != nil {
					_ = client.SessionCancel(sid)
					_ = stdio.WriteResponse(reqCopy.ID, SessionPromptResult{StopReason: StopCancelled}, nil)
					return
				}
				if err != nil {
					msg := err.Error()
					if strings.Contains(msg, "connection closed") {
						_ = stdio.WriteResponse(reqCopy.ID, nil, newRPCError(CodeInternalError, "Mode B connection closed — is MaClaw GUI still running?"))
						return
					}
					_ = stdio.WriteResponse(reqCopy.ID, nil, newRPCError(CodeInternalError, msg))
					return
				}
				if stop == "" {
					stop = StopEndTurn
				}
				_ = stdio.WriteResponse(reqCopy.ID, SessionPromptResult{StopReason: stop}, nil)
			}()
		case "session/cancel":
			var params SessionCancelParams
			_ = json.Unmarshal(req.Params, &params)
			sid := strings.TrimSpace(params.SessionID)
			mu.Lock()
			if cfn := cancels[sid]; cfn != nil {
				cfn()
			}
			mu.Unlock()
			_ = client.SessionCancel(sid)
			if hasRequestID(req.ID) {
				_ = stdio.WriteResponse(req.ID, map[string]any{}, nil)
			}
		case "session/steer":
			// Guide-launch injection into the running GUI loop; deliberately not
			// gated on the prompt lock — steering only makes sense mid-turn.
			var params SessionSteerParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				_ = stdio.WriteResponse(req.ID, nil, newRPCError(CodeInvalidParams, err.Error()))
				continue
			}
			callCtx, callCancel := context.WithTimeout(context.Background(), 15*time.Second)
			accepted, err := client.SessionSteer(callCtx, strings.TrimSpace(params.SessionID), params.Text)
			callCancel()
			if err != nil {
				_ = stdio.WriteResponse(req.ID, nil, newRPCError(CodeInternalError, err.Error()))
				continue
			}
			_ = stdio.WriteResponse(req.ID, SessionSteerResult{Accepted: accepted}, nil)
		default:
			if hasRequestID(req.ID) {
				_ = stdio.WriteResponse(req.ID, nil, newRPCError(CodeMethodNotFound, "method not found: "+req.Method))
			}
		}
	}
}
