package acpagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// Metadata keys written by MaClaw GUI third-party gateway for reliable turn ends.
const (
	MetaACPTurn      = "acp_turn"
	MetaACPTurnFinal = "final"
)

// BridgeOptions configures the VS Code ↔ GUI ACP bridge.
type BridgeOptions struct {
	AgentInfo ImplementationInfo
	Gateway   *GatewayClient
	// Logger writes to stderr only (never stdout — ACP purity).
	Logger *log.Logger
	// MaxPromptWait is the max time to wait for a GUI turn to finish.
	MaxPromptWait time.Duration
	// IdleAfterContent is a fallback end when gateway does not mark final (legacy).
	IdleAfterContent time.Duration
	// PollTimeoutSec is the gateway long-poll timeout per request.
	PollTimeoutSec int
	// SkipReadyCheck disables Health+Handshake gating (tests only).
	SkipReadyCheck bool
}

// Bridge is an ACP Agent that attaches to a running MaClaw GUI via IM Gateway.
type Bridge struct {
	opts BridgeOptions
	conn *Conn
	log  *log.Logger

	mu       sync.Mutex
	sessions map[string]*bridgeSession
	// in-flight prompt cancel channels keyed by sessionId
	cancels map[string]context.CancelFunc
	// per-session prompt serialization
	promptMu map[string]*sync.Mutex
	ready    bool
	readyErr error
}

type bridgeSession struct {
	ID              string
	Cwd             string
	ConversationID  string
	Cursor          string
	LastPromptMsgID string
	CreatedAt       time.Time
}

// NewBridge creates a bridge agent. Gateway must be non-nil.
func NewBridge(opts BridgeOptions) (*Bridge, error) {
	if opts.Gateway == nil {
		return nil, fmt.Errorf("acpagent: Gateway is required")
	}
	if opts.AgentInfo.Name == "" {
		opts.AgentInfo = ImplementationInfo{
			Name:    "maclaw-gui-bridge",
			Title:   "MaClaw GUI Bridge",
			Version: "0.2.0",
		}
	}
	if opts.MaxPromptWait <= 0 {
		opts.MaxPromptWait = 30 * time.Minute
	}
	if opts.IdleAfterContent <= 0 {
		opts.IdleAfterContent = 8 * time.Second
	}
	if opts.PollTimeoutSec <= 0 {
		opts.PollTimeoutSec = coreim.ThirdPartyPollTimeoutSec
	}
	lg := opts.Logger
	if lg == nil {
		lg = log.New(io.Discard, "", 0)
	}
	return &Bridge{
		opts:     opts,
		log:      lg,
		sessions: map[string]*bridgeSession{},
		cancels:  map[string]context.CancelFunc{},
		promptMu: map[string]*sync.Mutex{},
	}, nil
}

// ServeStdio runs the ACP agent loop until stdin EOF or fatal error.
// session/prompt runs asynchronously so session/cancel can be processed mid-turn.
func (b *Bridge) ServeStdio(r io.Reader, w io.Writer) error {
	b.conn = NewConn(r, w)

	if !b.opts.SkipReadyCheck {
		if err := b.ensureReady(context.Background()); err != nil {
			b.log.Printf("gateway not ready at start: %v (will retry on session/new)", err)
			b.mu.Lock()
			b.readyErr = err
			b.mu.Unlock()
		}
	}

	for {
		req, err := b.conn.ReadRequest()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			if strings.Contains(err.Error(), "empty line") {
				continue
			}
			if strings.Contains(err.Error(), "EOF") {
				return nil
			}
			b.log.Printf("read error: %v", err)
			continue
		}

		// Notifications / methods that must not block the read loop.
		switch req.Method {
		case "session/prompt":
			reqCopy := req
			go func() {
				result, rpcErr := b.onSessionPrompt(reqCopy.Params)
				if err := b.reply(reqCopy, result, rpcErr); err != nil {
					b.log.Printf("reply session/prompt: %v", err)
				}
			}()
		case "session/cancel":
			b.onSessionCancel(req.Params)
			if hasRequestID(req.ID) {
				_ = b.conn.WriteResponse(req.ID, map[string]any{}, nil)
			}
		default:
			if err := b.handle(req); err != nil {
				b.log.Printf("handle %s: %v", req.Method, err)
			}
		}
	}
}

func (b *Bridge) ensureReady(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	if _, err := b.opts.Gateway.Health(ctx); err != nil {
		return fmt.Errorf("gateway health: %w", err)
	}
	hsCtx, hsCancel := context.WithTimeout(parent, 10*time.Second)
	defer hsCancel()
	if _, err := b.opts.Gateway.Handshake(hsCtx); err != nil {
		return fmt.Errorf("gateway handshake: %w", err)
	}
	b.mu.Lock()
	b.ready = true
	b.readyErr = nil
	b.mu.Unlock()
	return nil
}

func (b *Bridge) handle(req Request) error {
	isNotif := !hasRequestID(req.ID)

	switch req.Method {
	case "initialize":
		result, rpcErr := b.onInitialize(req.Params)
		return b.reply(req, result, rpcErr)
	case "session/new":
		result, rpcErr := b.onSessionNew(req.Params)
		return b.reply(req, result, rpcErr)
	case "authenticated", "notifications/initialized", "initialized":
		if !isNotif {
			return b.conn.WriteResponse(req.ID, map[string]any{}, nil)
		}
		return nil
	default:
		if isNotif {
			b.log.Printf("ignore notification %q", req.Method)
			return nil
		}
		return b.conn.WriteResponse(req.ID, nil, newRPCError(CodeMethodNotFound, "method not found: "+req.Method))
	}
}

func (b *Bridge) reply(req Request, result any, rpcErr *RPCError) error {
	if !hasRequestID(req.ID) {
		return nil
	}
	return b.conn.WriteResponse(req.ID, result, rpcErr)
}

func (b *Bridge) onInitialize(raw json.RawMessage) (any, *RPCError) {
	var params InitializeParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, newRPCError(CodeInvalidParams, "invalid initialize params: "+err.Error())
		}
	}
	_ = params.ProtocolVersion
	return InitializeResult{
		ProtocolVersion:   ProtocolVersion,
		AgentCapabilities: DefaultAgentCapabilities(),
		AgentInfo:         b.opts.AgentInfo,
		AuthMethods:       nil,
	}, nil
}

func (b *Bridge) onSessionNew(raw json.RawMessage) (any, *RPCError) {
	var params SessionNewParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, newRPCError(CodeInvalidParams, "invalid session/new params: "+err.Error())
		}
	}
	if !b.opts.SkipReadyCheck {
		if err := b.ensureReady(context.Background()); err != nil {
			return nil, newRPCError(CodeInternalError, "MaClaw GUI gateway not ready: "+err.Error()+". Keep MaClaw GUI running with Third-party Gateway enabled.")
		}
	}

	id := "acp_" + randomID(12)
	conv := id
	b.mu.Lock()
	b.sessions[id] = &bridgeSession{
		ID:             id,
		Cwd:            strings.TrimSpace(params.Cwd),
		ConversationID: conv,
		Cursor:         "0",
		CreatedAt:      time.Now(),
	}
	b.promptMu[id] = &sync.Mutex{}
	b.mu.Unlock()

	return SessionNewResult{SessionID: id}, nil
}

func (b *Bridge) onSessionCancel(raw json.RawMessage) {
	var params SessionCancelParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return
	}
	b.mu.Lock()
	cancel := b.cancels[sid]
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Bridge) sessionPromptLock(sid string) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.promptMu[sid]
	if m == nil {
		m = &sync.Mutex{}
		b.promptMu[sid] = m
	}
	return m
}

func (b *Bridge) onSessionPrompt(raw json.RawMessage) (any, *RPCError) {
	var params SessionPromptParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, newRPCError(CodeInvalidParams, "invalid session/prompt params: "+err.Error())
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return nil, newRPCError(CodeInvalidParams, "sessionId is required")
	}
	text := PromptText(params.Prompt)
	if text == "" {
		return nil, newRPCError(CodeInvalidParams, "prompt has no text content")
	}

	lock := b.sessionPromptLock(sid)
	if !lock.TryLock() {
		return nil, newRPCError(CodeInvalidParams, "session busy: another prompt is in progress")
	}
	defer lock.Unlock()

	b.mu.Lock()
	sess := b.sessions[sid]
	b.mu.Unlock()
	if sess == nil {
		return nil, newRPCError(CodeInvalidParams, "unknown sessionId")
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.opts.MaxPromptWait)
	defer cancel()
	b.mu.Lock()
	if prev := b.cancels[sid]; prev != nil {
		prev()
	}
	b.cancels[sid] = cancel
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.cancels, sid)
		b.mu.Unlock()
	}()

	b.mu.Lock()
	cursor := sess.Cursor
	conv := sess.ConversationID
	cwd := sess.Cwd
	b.mu.Unlock()

	if cwd != "" {
		text = text + "\n\n[workspace cwd: " + cwd + "]"
	}

	msgID := "msg_" + randomID(10)
	evtID := "evt_" + msgID
	b.mu.Lock()
	if s := b.sessions[sid]; s != nil {
		s.LastPromptMsgID = msgID
	}
	b.mu.Unlock()

	sendCtx, sendCancel := context.WithTimeout(ctx, 60*time.Second)
	_, err := b.opts.Gateway.SendText(sendCtx, conv, evtID, msgID, text)
	sendCancel()
	if err != nil {
		if ctx.Err() != nil {
			return SessionPromptResult{StopReason: StopCancelled}, nil
		}
		return nil, newRPCError(CodeInternalError, "gateway send failed: "+err.Error())
	}

	stop, err := b.pumpOutgoing(ctx, sid, conv, msgID, &cursor)
	b.mu.Lock()
	if s := b.sessions[sid]; s != nil {
		s.Cursor = cursor
	}
	b.mu.Unlock()

	if err != nil && stop != StopCancelled {
		return nil, newRPCError(CodeInternalError, err.Error())
	}
	if stop == "" {
		stop = StopEndTurn
	}
	return SessionPromptResult{StopReason: stop}, nil
}

func (b *Bridge) pumpOutgoing(ctx context.Context, sessionID, conv, promptMsgID string, cursor *string) (stopReason string, err error) {
	var (
		gotFinal      bool
		gotContent    bool
		lastActivity  time.Time
		emptyStreak   int
		progressOnly  bool
	)
	lastActivity = time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return StopCancelled, nil
		}

		timeoutSec := b.opts.PollTimeoutSec
		if gotContent || gotFinal {
			timeoutSec = 3
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, time.Duration(timeoutSec+10)*time.Second)
		out, pollErr := b.opts.Gateway.Poll(pollCtx, *cursor, timeoutSec, coreim.ThirdPartyMaxBatchSize)
		pollCancel()
		if pollErr != nil {
			if ctx.Err() != nil {
				return StopCancelled, nil
			}
			return "", fmt.Errorf("gateway poll: %w", pollErr)
		}
		if out.NextCursor != "" {
			*cursor = out.NextCursor
		}

		var ackIDs []string
		for _, msg := range out.Messages {
			if msg.ConversationID != "" && conv != "" && msg.ConversationID != conv {
				if msg.ID != "" {
					ackIDs = append(ackIDs, msg.ID)
				}
				continue
			}
			if err := b.emitOutgoing(sessionID, msg); err != nil {
				b.log.Printf("emit: %v", err)
			}
			if msg.ID != "" {
				ackIDs = append(ackIDs, msg.ID)
			}
			lastActivity = time.Now()

			if msg.Progress {
				progressOnly = true
				continue
			}
			if messageHasUserVisibleContent(msg) {
				gotContent = true
				progressOnly = false
			}
			if isTurnFinal(msg, promptMsgID) {
				gotFinal = true
			}
		}
		if len(ackIDs) > 0 {
			ackCtx, ackCancel := context.WithTimeout(ctx, 10*time.Second)
			if ackErr := b.opts.Gateway.Ack(ackCtx, ackIDs); ackErr != nil {
				b.log.Printf("ack: %v", ackErr)
			}
			ackCancel()
		}

		if gotFinal {
			// Drain a short moment for trailing media/tool messages then end.
			if len(out.Messages) == 0 || !out.HasMore {
				return StopEndTurn, nil
			}
			// one more poll cycle allowed if hasMore
			continue
		}

		if !gotContent {
			// Still waiting for agent; only cancel/timeout ends.
			continue
		}

		// Fallback end for gateways without acp_turn=final (legacy).
		if len(out.Messages) == 0 {
			emptyStreak++
		} else {
			emptyStreak = 0
		}
		if !progressOnly && emptyStreak >= 2 && time.Since(lastActivity) >= b.opts.IdleAfterContent {
			return StopEndTurn, nil
		}
		if !progressOnly && time.Since(lastActivity) >= b.opts.IdleAfterContent && (len(out.Messages) == 0 || !out.HasMore) {
			return StopEndTurn, nil
		}
	}
}

func isTurnFinal(msg coreim.ThirdPartyOutgoingMessage, promptMsgID string) bool {
	if msg.Progress {
		return false
	}
	// Prefer explicit terminal marker from GUI (Mode B / Gateway production path).
	if msg.Metadata != nil {
		if v := strings.TrimSpace(msg.Metadata[MetaACPTurn]); strings.EqualFold(v, MetaACPTurnFinal) || strings.EqualFold(v, "done") {
			return true
		}
	}
	// Explicit error terminal (with or without replyTo).
	if strings.EqualFold(msg.Type, "error") || strings.TrimSpace(msg.Error) != "" {
		if promptMsgID == "" || msg.ReplyToMessageID == "" || msg.ReplyToMessageID == promptMsgID {
			return true
		}
	}
	// Do NOT treat bare text+replyTo as final: intermediate tool-plan text can
	// share replyTo and would end the turn too early. Rely on acp_turn=final
	// or idle fallback in pumpOutgoing.
	return false
}

func messageHasUserVisibleContent(msg coreim.ThirdPartyOutgoingMessage) bool {
	if strings.TrimSpace(msg.Error) != "" {
		return true
	}
	if msg.ToolCall != nil || msg.ToolPlan != nil {
		return true
	}
	if strings.TrimSpace(msg.Text) != "" {
		return true
	}
	if strings.TrimSpace(msg.Caption) != "" {
		return true
	}
	return false
}

func (b *Bridge) emitOutgoing(sessionID string, msg coreim.ThirdPartyOutgoingMessage) error {
	if msg.ToolCall != nil {
		status := "pending"
		if msg.ToolCall.RequiresApproval {
			status = "pending"
		}
		update := map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    msg.ToolCall.ID,
			"title":         msg.ToolCall.Name,
			"kind":          "other",
			"status":        status,
			"rawInput":      msg.ToolCall.Arguments,
		}
		return b.conn.WriteNotification("session/update", SessionUpdateParams{
			SessionID: sessionID,
			Update:    update,
		})
	}
	if msg.ToolPlan != nil {
		entries := make([]map[string]any, 0, len(msg.ToolPlan.Steps))
		for _, step := range msg.ToolPlan.Steps {
			entries = append(entries, map[string]any{
				"content":  step.Tool,
				"priority": "medium",
				"status":   "pending",
			})
		}
		return b.conn.WriteNotification("session/update", SessionUpdateParams{
			SessionID: sessionID,
			Update: map[string]any{
				"sessionUpdate": "plan",
				"entries":       entries,
			},
		})
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" && strings.TrimSpace(msg.Error) != "" {
		text = msg.Error
	}
	if text == "" && strings.TrimSpace(msg.Caption) != "" {
		text = msg.Caption
	}
	if text == "" {
		// Media-only: surface a short notice.
		switch strings.ToLower(msg.Type) {
		case "image", "file", "voice", "video":
			name := strings.TrimSpace(msg.FileName)
			if name == "" {
				name = msg.Type
			}
			text = "[" + msg.Type + ": " + name + "]"
		default:
			return nil
		}
	}

	return b.conn.WriteNotification("session/update", SessionUpdateParams{
		SessionID: sessionID,
		Update: map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content": map[string]any{
				"type": "text",
				"text": text,
			},
		},
	})
}

func randomID(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// Doctor checks gateway reachability and configuration. Returns multi-line human report.
func Doctor(ep GatewayEndpoint) string {
	var b strings.Builder
	fmt.Fprintf(&b, "config: %s\n", ep.ConfigPath)
	fmt.Fprintf(&b, "gateway: %s\n", ep.BaseURL)
	if ep.Token == "" {
		b.WriteString("token: MISSING (enable third-party gateway in MaClaw GUI or set MACLAW_GATEWAY_TOKEN)\n")
	} else {
		b.WriteString("token: present\n")
	}
	if !ep.OK {
		b.WriteString("discovery: incomplete (gateway disabled or no token in config)\n")
	} else {
		b.WriteString("discovery: ok\n")
	}
	g := NewGatewayClient(ep)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := g.Health(ctx)
	if err != nil {
		fmt.Fprintf(&b, "health: FAIL (%v)\n", err)
		b.WriteString("hint: start MaClaw GUI and enable Third-party Gateway (default 127.0.0.1:18777)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "health: ok status=%s protocol=%s\n", h.Status, h.ProtocolVersion)
	hsCtx, hsCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer hsCancel()
	if _, err := g.Handshake(hsCtx); err != nil {
		fmt.Fprintf(&b, "handshake: FAIL (%v)\n", err)
	} else {
		b.WriteString("handshake: ok\n")
	}
	return b.String()
}
