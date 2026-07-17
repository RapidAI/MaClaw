// Package lansenger implements a client-side gateway for the Lansenger (蓝信)
// messaging platform. It connects via WebSocket to receive messages and uses
// REST APIs to send messages, following the Lansenger Open Platform protocol.
package lansenger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	lansengerReadWait     = 180 * time.Second
	lansengerPingInterval = 15 * time.Second
	lansengerPingWait     = 15 * time.Second
	lansengerPongWait     = 45 * time.Second
	lansengerMaxConnAge   = 12 * time.Hour
	lansengerMaxBackoff   = 30 * time.Second
	lansengerAPIMaxRetry  = 3
)

// Config holds the credentials and endpoints for a Lansenger bot.
type Config struct {
	AppID            string // Application ID (from token field 1)
	AppSecret        string // Application secret (from token field 2)
	ApiGatewayURL    string // API gateway base URL (from token field 3)
	WebSocketBaseURL string // optional WebSocket gateway base URL override
}

// ParseToken parses a composite token string "AppID:AppSecret:ApiGatewayURL".
// The URL contains "://" so we cannot simply split on ":". Instead we find
// the first two colons to separate AppID and AppSecret, then treat the
// remainder as the gateway URL.
func ParseToken(token string) (Config, error) {
	first := strings.Index(token, ":")
	if first < 0 {
		return Config{}, fmt.Errorf("lansenger: invalid token format, expected AppID:AppSecret:ApiGatewayURL")
	}
	rest := token[first+1:]
	second := strings.Index(rest, ":")
	if second < 0 {
		return Config{}, fmt.Errorf("lansenger: invalid token format, expected AppID:AppSecret:ApiGatewayURL")
	}
	appID := token[:first]
	appSecret := rest[:second]
	apiGatewayURL := rest[second+1:]
	if appID == "" || appSecret == "" || apiGatewayURL == "" {
		return Config{}, fmt.Errorf("lansenger: invalid token format, all three fields must be non-empty")
	}
	return Config{
		AppID:         appID,
		AppSecret:     appSecret,
		ApiGatewayURL: apiGatewayURL,
	}, nil
}

// IncomingMessage represents a message received from Lansenger.
type IncomingMessage struct {
	FromUserID      string           // staffId of the sender
	Text            string           // text content
	MessageID       string           // platform message ID
	MessageType     string           // "text", "formatText", "image", "file", etc.
	ChatType        string           // "p2p" or "group"
	GroupID         string           // group ID if group message
	MediaType       string           // "image", "video", "file", "voice" or ""
	MediaData       []byte           // downloaded media bytes (if any)
	MediaName       string           // media file name
	SenderName      string           // display name of the sender, when supplied by Lansenger
	GroupName       string           // display name of the group, when supplied by Lansenger
	ReferenceText   string           // quoted/referenced message rendered as plain text
	MentionedStaffs []MentionedStaff // staff @mentioned in this message
	MentionedBots   []MentionedBot   // bots @mentioned in this message
	IsAtMe          bool             // true when Lansenger marks this bot as explicitly @mentioned
	IsAtAll         bool             // true when the message @mentions all members
}

// MentionedStaff and MentionedBot preserve the mention metadata sent by the
// current Lansenger gateway. They let downstream agents understand group
// context without having to infer mentions from rendered text.
type MentionedStaff struct {
	ID   string `json:"staffId"`
	Name string `json:"staffName"`
}

type MentionedBot struct {
	ID   string `json:"botId"`
	Name string `json:"botName"`
}

// OutgoingReminder is the Lansenger "reminder" payload used to @mention members.
// The platform auto-prepends @姓名; message text does not need to include names.
// Matches OpenClaw / 蓝信 open API: { all, userIds, botIds }.
type OutgoingReminder struct {
	All     bool     `json:"all,omitempty"`
	UserIDs []string `json:"userIds,omitempty"`
	BotIDs  []string `json:"botIds,omitempty"`
}

// OutgoingText is a plain text message to send.
type OutgoingText struct {
	ToUserID string
	Text     string
	IsGroup  bool
	// Reminder optionally @mentions users/bots (or @all) when sending.
	Reminder *OutgoingReminder
	// RefMsgID attaches a native quote/reply to an inbound platform message ID.
	RefMsgID string
}

// OutgoingMedia is a media message to send.
type OutgoingMedia struct {
	ToUserID  string
	FileData  []byte
	FileName  string
	MediaType string // "image", "file", "video"
	IsGroup   bool
}

// MessageHandler is the callback for incoming messages.
type MessageHandler func(msg IncomingMessage)

// StatusCallback is called when the connection status changes.
type StatusCallback func(status string)

// ---------------------------------------------------------------------------
// Token management
// ---------------------------------------------------------------------------

type tokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (tc *tokenCache) get() (string, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.token != "" && time.Now().Before(tc.expiresAt) {
		return tc.token, true
	}
	return "", false
}

func (tc *tokenCache) set(token string, expiresIn int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.token = token
	// Refresh 5 minutes before expiry, but at least 30 seconds from now.
	margin := 5 * time.Minute
	ttl := time.Duration(expiresIn) * time.Second
	if ttl <= margin {
		margin = ttl / 2
	}
	if margin < 30*time.Second {
		margin = 30 * time.Second
	}
	refreshIn := ttl - margin
	// A malformed/non-positive expiresIn must never create an immediately
	// expired cache entry (which otherwise causes every media operation to
	// re-authenticate). Keep it short-lived, but usable.
	if refreshIn <= 0 {
		refreshIn = 30 * time.Second
	}
	tc.expiresAt = time.Now().Add(refreshIn)
}

func (tc *tokenCache) clear() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.token = ""
	tc.expiresAt = time.Time{}
}

// ---------------------------------------------------------------------------
// Gateway
// ---------------------------------------------------------------------------

// Gateway manages the WebSocket connection and REST API calls to Lansenger.
type Gateway struct {
	config   Config
	handler  MessageHandler
	statusMu sync.RWMutex
	statusCb StatusCallback
	client   *http.Client
	// mediaClient allows large uploads/downloads to use the same hardened
	// transport and redirect policy without being constrained by the short API
	// request timeout used for authentication and message sends.
	mediaClient *http.Client
	tokens      tokenCache
	// tokenFetchSem prevents a burst of concurrent sends/media downloads from
	// stampeding the app-token endpoint. A channel semaphore lets callers stop
	// waiting promptly when their own context is cancelled.
	tokenFetchSem chan struct{}

	mu sync.Mutex
	// lifecycleMu serializes Start and Stop. In particular, a new Start must
	// not overlap Stop waiting for the prior connection goroutine to finish.
	lifecycleMu sync.Mutex
	ws          *websocket.Conn
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	running     bool
	runID       uint64
}

// NewGateway creates a new Lansenger gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
	// Media transfers can legitimately take longer than ordinary API calls
	// before the gateway starts returning a response body. Do not share the API
	// transport here: its 30-second ResponseHeaderTimeout would otherwise defeat
	// mediaClient's five-minute timeout.
	mediaTransport := transport.Clone()
	mediaTransport.ResponseHeaderTimeout = 5 * time.Minute
	noRedirect := func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Gateway{
		config:        config,
		handler:       handler,
		tokenFetchSem: make(chan struct{}, 1),
		client: &http.Client{
			Timeout: 30 * time.Second,
			// App tokens are sent as query parameters by the Lansenger API. Never
			// follow a server-provided redirect, which could leak them to another
			// origin. Callers receive the redirect response as an API error instead.
			CheckRedirect: noRedirect,
			Transport:     transport,
		},
		// The reference channel uses a five-minute window for media transfer.
		// It is still bounded by the caller's context and the 50 MiB inbound cap.
		mediaClient: &http.Client{
			Timeout:       5 * time.Minute,
			CheckRedirect: noRedirect,
			Transport:     mediaTransport,
		},
	}
}

// SetStatusCallback sets the status change callback.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.statusMu.Lock()
	defer g.statusMu.Unlock()
	g.statusCb = cb
}

func (g *Gateway) emitStatus(status string) {
	g.statusMu.RLock()
	cb := g.statusCb
	g.statusMu.RUnlock()
	if cb != nil {
		cb(status)
	}
}

// Start connects to Lansenger via WebSocket and begins receiving messages.
func (g *Gateway) Start(ctx context.Context) error {
	g.lifecycleMu.Lock()
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		g.lifecycleMu.Unlock()
		log.Printf("[lansenger] Start called but already running")
		return nil
	}
	ctx2, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.runID++
	runID := g.runID
	g.mu.Unlock()
	g.lifecycleMu.Unlock()

	log.Printf("[lansenger] starting gateway: appID=%s gateway=%s", g.config.AppID, g.config.ApiGatewayURL)

	// Validate credentials by fetching an app token.
	if _, err := g.getAppToken(ctx2); err != nil {
		g.lifecycleMu.Lock()
		g.mu.Lock()
		stillCurrent := g.running && g.runID == runID
		if stillCurrent {
			g.running = false
			g.cancel = nil
		}
		g.mu.Unlock()
		g.lifecycleMu.Unlock()
		cancel()
		// Cancellation is a normal lifecycle transition, not an authentication
		// failure. In particular, Stop may cancel an in-flight Start.
		if ctx2.Err() != nil {
			return ctx2.Err()
		}
		g.emitStatus("error")
		return fmt.Errorf("lansenger: auth failed: %w", err)
	}

	g.lifecycleMu.Lock()
	g.mu.Lock()
	stillCurrent := g.running && g.runID == runID
	g.mu.Unlock()
	if !stillCurrent {
		g.lifecycleMu.Unlock()
		cancel()
		return ctx2.Err()
	}
	g.wg.Add(1)
	go g.connectLoop(ctx2, runID)
	g.lifecycleMu.Unlock()
	return nil
}

// Stop gracefully shuts down the gateway.
func (g *Gateway) Stop() error {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()

	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = false
	g.runID++
	if g.cancel != nil {
		g.cancel()
	}
	ws := g.ws
	g.ws = nil
	g.cancel = nil
	g.mu.Unlock()

	log.Printf("[lansenger] stopping gateway")
	if ws != nil {
		_ = ws.Close()
	}
	g.wg.Wait()
	g.emitStatus("disconnected")
	log.Printf("[lansenger] gateway stopped")
	return nil
}

// IsRunning returns whether the gateway is active.
func (g *Gateway) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// ---------------------------------------------------------------------------
// WebSocket connection loop
// ---------------------------------------------------------------------------

func (g *Gateway) connectLoop(ctx context.Context, runID uint64) {
	defer g.finishConnectLoop(ctx, runID)
	attempt := 0
	var lastConnectedAt time.Time

	for {
		select {
		case <-ctx.Done():
			log.Printf("[lansenger] connectLoop exiting: context cancelled, err=%v", ctx.Err())
			return
		default:
		}

		wsURL, err := g.getWebSocketURL(ctx)
		if err != nil {
			log.Printf("[lansenger] error: get WS URL failed (attempt %d): %v", attempt+1, err)
			attempt++
			g.backoff(ctx, attempt)
			continue
		}

		log.Printf("[lansenger] dialing WebSocket (attempt %d): %s", attempt+1, redactWebSocketURL(wsURL))
		g.emitStatus("connecting")

		dialStart := time.Now()
		dialer := websocket.Dialer{
			HandshakeTimeout: 30 * time.Second,
			NetDialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
		conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
		dialDuration := time.Since(dialStart)
		if err != nil {
			extra := ""
			if resp != nil {
				extra = fmt.Sprintf(", HTTP status=%d", resp.StatusCode)
			}
			log.Printf("[lansenger] error: WS dial failed (attempt %d, took %v%s): %v",
				attempt+1, dialDuration, extra, err)
			attempt++
			g.backoff(ctx, attempt)
			continue
		}

		// Stop may cancel the context just after DialContext succeeds. Never
		// publish a late connection into the gateway after that point.
		g.mu.Lock()
		acceptConn := ctx.Err() == nil && g.running
		if acceptConn {
			g.ws = conn
		}
		g.mu.Unlock()
		if !acceptConn {
			_ = conn.Close()
			return
		}

		lastConnectedAt = time.Now()
		attempt = 0
		g.emitStatus("connected")
		log.Printf("[lansenger] WebSocket connected (dial took %v), remote=%v",
			dialDuration, conn.RemoteAddr())

		// Read loop blocks until error or close.
		g.readLoop(ctx, conn)
		if ctx.Err() != nil {
			log.Printf("[lansenger] connection loop stopped after readLoop: %v", ctx.Err())
			return
		}

		uptime := time.Since(lastConnectedAt)
		g.mu.Lock()
		if g.ws == conn {
			g.ws = nil
		}
		g.mu.Unlock()

		g.emitStatus("reconnecting")
		log.Printf("[lansenger] warning: connection lost after %v uptime, reconnecting (attempt %d)",
			uptime.Truncate(time.Second), attempt+1)
		attempt++
		g.backoff(ctx, attempt)
	}
}

func (g *Gateway) finishConnectLoop(ctx context.Context, runID uint64) {
	g.mu.Lock()
	current := g.runID == runID
	wasRunning := current && g.running
	var ws *websocket.Conn
	if current {
		g.running = false
		ws = g.ws
		g.ws = nil
		g.cancel = nil
	}
	g.mu.Unlock()

	if ws != nil {
		_ = ws.Close()
	}
	if wasRunning && ctx.Err() != nil {
		g.emitStatus("disconnected")
	}
	g.wg.Done()
}

func (g *Gateway) readLoop(ctx context.Context, conn *websocket.Conn) {
	loopStart := time.Now()
	// Start heartbeat.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	var lastPongUnixNano atomic.Int64
	lastPongUnixNano.Store(time.Now().UnixNano())
	go g.heartbeatLoop(hbCtx, conn, &lastPongUnixNano, loopStart)
	contextClosed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-contextClosed:
		}
	}()
	defer close(contextClosed)

	msgCount := 0
	lastMsgAt := time.Now()

	// Set Pong handler to reset read deadline when server responds to our Ping.
	conn.SetPongHandler(func(appData string) error {
		lastPongUnixNano.Store(time.Now().UnixNano())
		_ = conn.SetReadDeadline(time.Now().Add(lansengerReadWait))
		return nil
	})

	// Set CloseHandler to log server-initiated close with code and reason.
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("[lansenger] warning: server sent close frame: code=%d reason=%q (after %d msgs, uptime %v)",
			code, text, msgCount, time.Since(loopStart).Truncate(time.Second))
		// Return a CloseError so ReadMessage surfaces it properly.
		return &websocket.CloseError{Code: code, Text: text}
	})

	for {
		select {
		case <-ctx.Done():
			log.Printf("[lansenger] readLoop exiting: context cancelled, err=%v (received %d msgs)", ctx.Err(), msgCount)
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(lansengerReadWait))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.Printf("[lansenger] warning: server closed connection normally (received %d msgs, uptime %v)",
					msgCount, time.Since(loopStart).Truncate(time.Second))
			} else {
				idleDuration := time.Since(lastMsgAt)
				errType := "unknown"
				errStr := err.Error()
				switch {
				case strings.Contains(errStr, "use of closed network connection"):
					errType = "connection_closed"
				case strings.Contains(errStr, "i/o timeout"):
					errType = "read_timeout"
				case strings.Contains(errStr, "connection reset"):
					errType = "connection_reset"
				case strings.Contains(errStr, "EOF"):
					errType = "eof"
				case strings.Contains(errStr, "tls:"):
					errType = "tls_error"
				case websocket.IsUnexpectedCloseError(err):
					errType = "unexpected_close"
				}
				log.Printf("[lansenger] error: WS read failed: type=%s err=%v (idle=%v, msgs=%d, uptime=%v)",
					errType, err, idleDuration.Truncate(time.Second),
					msgCount, time.Since(loopStart).Truncate(time.Second))
			}
			return
		}

		msgCount++
		lastMsgAt = time.Now()
		g.handleWSMessage(message)
	}
}

func (g *Gateway) heartbeatLoop(ctx context.Context, conn *websocket.Conn, lastPongUnixNano *atomic.Int64, connectedAt time.Time) {
	ticker := time.NewTicker(lansengerPingInterval)
	defer ticker.Stop()
	pingCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("[lansenger] heartbeat stopped: context cancelled, err=%v (sent %d pings)", ctx.Err(), pingCount)
			return
		case <-ticker.C:
			g.mu.Lock()
			ws := g.ws
			g.mu.Unlock()
			if ws != conn {
				log.Printf("[lansenger] warning: heartbeat stopped: connection replaced (sent %d pings)", pingCount)
				return
			}
			lastPong := time.Unix(0, lastPongUnixNano.Load())
			if !lastPong.IsZero() && time.Since(lastPong) > lansengerPongWait {
				log.Printf("[lansenger] error: heartbeat watchdog closing stale connection: no pong for %v (sent %d pings)",
					time.Since(lastPong).Truncate(time.Second), pingCount)
				_ = conn.Close()
				return
			}
			if lansengerConnectionAgeExceeded(connectedAt, time.Now()) {
				log.Printf("[lansenger] warning: refreshing long-lived connection after %v (sent %d pings)",
					time.Since(connectedAt).Truncate(time.Second), pingCount)
				_ = conn.Close()
				return
			}
			pingStart := time.Now()
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(lansengerPingWait)); err != nil {
				log.Printf("[lansenger] error: heartbeat ping #%d failed (took %v): %v",
					pingCount+1, time.Since(pingStart), err)
				_ = conn.Close()
				return
			}
			pingCount++
			// Log every 40th ping (~10 min) as a liveness indicator.
			if pingCount%40 == 0 {
				log.Printf("[lansenger] heartbeat alive: sent %d pings, ping latency=%v [warn-level-for-visibility]",
					pingCount, time.Since(pingStart))
			}
		}
	}
}

func (g *Gateway) backoff(ctx context.Context, attempt int) {
	delay := reconnectBackoffDelay(attempt)
	log.Printf("[lansenger] warning: backoff waiting %v before retry (attempt %d)", delay, attempt)
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func reconnectBackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	backoffStep := attempt
	if backoffStep > int(lansengerMaxBackoff/(3*time.Second)) {
		backoffStep = int(lansengerMaxBackoff / (3 * time.Second))
	}
	delay := time.Duration(backoffStep) * 3 * time.Second
	if delay > lansengerMaxBackoff {
		delay = lansengerMaxBackoff
	}
	return delay
}

func lansengerConnectionAgeExceeded(connectedAt, now time.Time) bool {
	if connectedAt.IsZero() || now.Before(connectedAt) {
		return false
	}
	return now.Sub(connectedAt) >= lansengerMaxConnAge
}

// ---------------------------------------------------------------------------
// WebSocket message parsing
// ---------------------------------------------------------------------------

type wsEnvelope struct {
	Events    []wsEvent       `json:"events"`
	Type      string          `json:"type"`
	EventType string          `json:"eventType"`
	Data      json.RawMessage `json:"data"`
}

type wsEvent struct {
	ID        string          `json:"id"`
	EventType string          `json:"eventType"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

type wsEventData struct {
	EventType string          `json:"eventType"`
	Type      string          `json:"type"`
	From      string          `json:"from"`
	MessageID string          `json:"messageId"`
	ID        string          `json:"id"`
	MsgType   string          `json:"msgType"`
	MsgData   json.RawMessage `json:"msgData"`
	ChatType  string          `json:"chatType"`
	GroupID   string          `json:"groupId"`
	// ConversationID is the group/chat identifier used by the current
	// Lansenger WebSocket payload.  Older payloads expose the same value as
	// groupId, so keep both and prefer groupId when it is present.
	ConversationID    string          `json:"conversationId"`
	ConversationTitle string          `json:"conversationTitle"`
	GroupName         string          `json:"groupName"`
	SenderName        string          `json:"senderName"`
	FromType          int             `json:"fromType"`
	Reminder          wsReminder      `json:"reminder"`
	ReferenceMsg      *wsReferenceMsg `json:"referenceMsg"`
	Data              *wsEventData    `json:"data"` // nested data
}

type wsReminder struct {
	Staffs  []MentionedStaff `json:"staffs"`
	Bots    []MentionedBot   `json:"bots"`
	IsAtMe  bool             `json:"isAtMe"`
	IsAtAll bool             `json:"isAtAll"`
}

type wsReferenceMsg struct {
	From         string          `json:"from"`
	SenderName   string          `json:"senderName"`
	MsgType      string          `json:"msgType"`
	MsgData      json.RawMessage `json:"msgData"`
	ReferenceMsg *wsReferenceMsg `json:"referenceMsg"`
}

func (g *Gateway) handleWSMessage(raw []byte) {
	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("[lansenger] WS message parse error: %v", err)
		return
	}

	events := env.Events
	// Some gateway callbacks are delivered as a single top-level event rather
	// than an events array. Normalize that shape before processing.
	if len(events) == 0 && len(env.Data) > 0 {
		events = []wsEvent{{EventType: env.EventType, Type: env.Type, Data: env.Data}}
	}
	for _, evt := range events {
		g.processEvent(evt)
	}
}

func (g *Gateway) processEvent(evt wsEvent) {
	var data wsEventData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		log.Printf("[lansenger] event data parse error: %v", err)
		return
	}

	// Handle nested data structure.
	actual := unwrapEventData(&data)

	evtType := evt.EventType
	if evtType == "" {
		evtType = evt.Type
	}
	if evtType == "" {
		evtType = actual.EventType
		if evtType == "" {
			evtType = actual.Type
		}
	}

	// Skip non-message events.
	switch evtType {
	case "account_message", "bot_private_message":
		// private message
	case "bot_group_message":
		// group message
	case "staff_info":
		log.Printf("[lansenger] received staff_info event, skipping")
		return
	default:
		log.Printf("[lansenger] unhandled event type: %s", evtType)
		return
	}

	from := actual.From
	if from == "" {
		return
	}

	msgType := actual.MsgType
	if msgType == "" {
		msgType = "text"
	}

	text := extractText(actual.MsgData, msgType)
	chatType := NormalizeChatType(actual.ChatType)
	if chatType == "" {
		if evtType == "bot_group_message" {
			chatType = "group"
		} else {
			chatType = "p2p"
		}
	}

	msgID := evt.ID
	if msgID == "" {
		msgID = actual.MessageID
		if msgID == "" {
			msgID = actual.ID
		}
	}

	msg := IncomingMessage{
		FromUserID:      from,
		Text:            text,
		MessageID:       msgID,
		MessageType:     msgType,
		ChatType:        chatType,
		GroupID:         firstNonEmpty(actual.GroupID, actual.ConversationID),
		SenderName:      actual.SenderName,
		GroupName:       firstNonEmpty(actual.GroupName, actual.ConversationTitle),
		MentionedStaffs: actual.Reminder.Staffs,
		MentionedBots:   actual.Reminder.Bots,
		IsAtMe:          actual.Reminder.IsAtMe,
		IsAtAll:         actual.Reminder.IsAtAll,
	}
	msg.ReferenceText = extractReferenceText(actual.ReferenceMsg)
	mediaCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	msg.MediaType, msg.MediaName, msg.MediaData = g.extractMedia(mediaCtx, actual.MsgData, msgType)
	if msg.ReferenceText != "" {
		msg.Text = joinMessageContext(msg.Text, msg.ReferenceText)
	}

	g.dispatchIncomingMessage(msg)
}

// dispatchIncomingMessage keeps a faulty application callback from taking down
// the WebSocket reader. Delivery remains synchronous so message order and the
// existing per-conversation handling semantics are preserved.
func (g *Gateway) dispatchIncomingMessage(msg IncomingMessage) {
	if g.handler == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[lansenger] message handler panic recovered: %v\n%s", recovered, debug.Stack())
		}
	}()
	g.handler(msg)
}

func unwrapEventData(data *wsEventData) *wsEventData {
	for data != nil && data.Data != nil && strings.TrimSpace(data.From) == "" {
		inheritEventMetadata(data.Data, data)
		data = data.Data
	}
	return data
}

// inheritEventMetadata preserves envelope-level fields when the actual message
// payload is nested under data. Current Lansenger callbacks use both layouts,
// and group metadata commonly lives on the outer envelope.
func inheritEventMetadata(dst, src *wsEventData) {
	if dst == nil || src == nil {
		return
	}
	if dst.EventType == "" {
		dst.EventType = src.EventType
	}
	if dst.Type == "" {
		dst.Type = src.Type
	}
	if dst.MessageID == "" {
		dst.MessageID = src.MessageID
	}
	if dst.ID == "" {
		dst.ID = src.ID
	}
	if dst.MsgType == "" {
		dst.MsgType = src.MsgType
	}
	if len(dst.MsgData) == 0 {
		dst.MsgData = src.MsgData
	}
	if dst.ChatType == "" {
		dst.ChatType = src.ChatType
	}
	if dst.GroupID == "" {
		dst.GroupID = src.GroupID
	}
	if dst.ConversationID == "" {
		dst.ConversationID = src.ConversationID
	}
	if dst.ConversationTitle == "" {
		dst.ConversationTitle = src.ConversationTitle
	}
	if dst.GroupName == "" {
		dst.GroupName = src.GroupName
	}
	if dst.SenderName == "" {
		dst.SenderName = src.SenderName
	}
	if len(dst.Reminder.Staffs) == 0 {
		dst.Reminder.Staffs = src.Reminder.Staffs
	}
	if len(dst.Reminder.Bots) == 0 {
		dst.Reminder.Bots = src.Reminder.Bots
	}
	if !dst.Reminder.IsAtMe {
		dst.Reminder.IsAtMe = src.Reminder.IsAtMe
	}
	if !dst.Reminder.IsAtAll {
		dst.Reminder.IsAtAll = src.Reminder.IsAtAll
	}
	if dst.ReferenceMsg == nil {
		dst.ReferenceMsg = src.ReferenceMsg
	}
}

func extractReferenceText(ref *wsReferenceMsg) string {
	if ref == nil {
		return ""
	}
	text := extractText(ref.MsgData, firstNonEmpty(ref.MsgType, "text"))
	label := firstNonEmpty(ref.SenderName, ref.From)
	if label != "" && text != "" {
		text = "[引用 " + label + "] " + text
	}
	if nested := extractReferenceText(ref.ReferenceMsg); nested != "" {
		if text != "" {
			return text + "\n" + nested
		}
		return nested
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func extractText(raw json.RawMessage, msgType string) string {
	if len(raw) == 0 {
		return ""
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return ""
	}

	switch msgType {
	case "text":
		// Current Lansenger clients also use text + mediaType/mediaIds for
		// attachments. Keep the caption, and expose a useful label if absent.
		var text struct {
			Content   string   `json:"content"`
			MediaType int      `json:"mediaType"`
			MediaIDs  []string `json:"mediaIds"`
		}
		if textObj, ok := wrapper["text"]; ok && json.Unmarshal(textObj, &text) == nil {
			if text.Content != "" {
				return text.Content
			}
			if len(text.MediaIDs) > 0 {
				return mediaLabel(mediaKindFromType(text.MediaType), text.MediaIDs, "")
			}
		}
	case "formatText":
		if ftObj, ok := wrapper["formatText"]; ok {
			var t struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ftObj, &t) == nil {
				return t.Text
			}
		}
	case "format":
		if formatObj, ok := wrapper["format"]; ok {
			var format struct {
				Text    string `json:"text"`
				Content string `json:"content"`
			}
			if json.Unmarshal(formatObj, &format) == nil {
				return firstNonEmpty(format.Text, format.Content)
			}
		}
	case "image", "video", "file", "voice":
		var media struct {
			Content  string   `json:"content"`
			MediaIDs []string `json:"mediaIds"`
		}
		if mediaObj, ok := wrapper[msgType]; ok && json.Unmarshal(mediaObj, &media) == nil {
			return mediaLabel(msgType, media.MediaIDs, media.Content)
		}
		return mediaLabel(msgType, nil, "")
	case "position":
		var position struct {
			Name      string `json:"name"`
			Address   string `json:"address"`
			Latitude  any    `json:"latitude"`
			Longitude any    `json:"longitude"`
		}
		if value, ok := wrapper["position"]; ok && json.Unmarshal(value, &position) == nil {
			return strings.TrimSpace("[位置] " + strings.Join([]string{position.Name, position.Address}, " "))
		}
		return "[位置]"
	}
	return "[" + msgType + "]"
}

func joinMessageContext(text, context string) string {
	text = strings.TrimSpace(text)
	context = strings.TrimSpace(context)
	if context == "" {
		return text
	}
	if text == "" {
		return context
	}
	return text + "\n\n" + context
}

func mediaKindFromType(mediaType int) string {
	switch mediaType {
	case 1:
		return "video"
	case 2:
		return "image"
	case 3:
		return "file"
	default:
		return "attachment"
	}
}

// mediaLabel preserves the media ID supplied by Lansenger.  It makes an
// attachment actionable for downstream code even if its eager download failed
// or only the first item was loaded (for example, a video cover image).
func mediaLabel(kind string, mediaIDs []string, caption string) string {
	label := "[" + kind
	if len(mediaIDs) == 1 {
		label += ": " + mediaIDs[0]
	} else if len(mediaIDs) > 1 {
		label += ": " + strings.Join(mediaIDs, ", ")
	}
	label += "]"
	if caption = strings.TrimSpace(caption); caption != "" {
		label += " " + caption
	}
	return label
}

// extractMedia follows the current Lansenger convention: attachments may use
// their own msgType or a text payload containing mediaType and mediaIds.
func (g *Gateway) extractMedia(ctx context.Context, raw json.RawMessage, msgType string) (string, string, []byte) {
	var wrapper map[string]json.RawMessage
	if json.Unmarshal(raw, &wrapper) != nil {
		return "", "", nil
	}
	mediaKind := msgType
	var payload struct {
		MediaType int      `json:"mediaType"`
		MediaIDs  []string `json:"mediaIds"`
	}
	value := wrapper[msgType]
	if msgType == "text" {
		value = wrapper["text"]
	}
	if len(value) == 0 || json.Unmarshal(value, &payload) != nil || len(payload.MediaIDs) == 0 {
		return "", "", nil
	}
	if msgType == "text" {
		switch payload.MediaType {
		case 1:
			mediaKind = "video"
		case 2:
			mediaKind = "image"
		default:
			mediaKind = "file"
		}
	}
	data, name, err := g.downloadMedia(ctx, payload.MediaIDs[0])
	if err != nil {
		log.Printf("[lansenger] media download failed: mediaId=%s: %v", payload.MediaIDs[0], err)
		return mediaKind, "", nil
	}
	return mediaKind, name, data
}

// ---------------------------------------------------------------------------
// REST API — authentication
// ---------------------------------------------------------------------------

func (g *Gateway) getAppToken(ctx context.Context) (string, error) {
	if tok, ok := g.tokens.get(); ok {
		return tok, nil
	}
	select {
	case g.tokenFetchSem <- struct{}{}:
		defer func() { <-g.tokenFetchSem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// Another caller may have refreshed the token while this caller waited.
	if tok, ok := g.tokens.get(); ok {
		return tok, nil
	}

	log.Printf("[lansenger] refreshing app token from %s", g.config.ApiGatewayURL)
	url := fmt.Sprintf("%s/v1/apptoken/create?grant_type=client_credential&appid=%s&secret=%s",
		strings.TrimRight(g.config.ApiGatewayURL, "/"),
		url.QueryEscape(g.config.AppID),
		url.QueryEscape(g.config.AppSecret),
	)

	var lastErr error
	for attempt := 1; attempt <= lansengerAPIMaxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		reqStart := time.Now()
		resp, err := g.client.Do(req)
		reqDuration := time.Since(reqStart)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lastErr = fmt.Errorf("token request failed (took %v): %w", reqDuration, err)
			log.Printf("[lansenger] error: token request failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, reqDuration, err)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("token body read failed (took %v): %w", reqDuration, readErr)
			log.Printf("[lansenger] error: token body read failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, reqDuration, readErr)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("token HTTP error %d (took %v): %s", resp.StatusCode, reqDuration, string(body))
			log.Printf("[lansenger] error: token HTTP %d (attempt %d/%d, took %v): %s", resp.StatusCode, attempt, lansengerAPIMaxRetry, reqDuration, string(body))
			if isRetryableHTTPStatus(resp.StatusCode) && attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		var result struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				AppToken  string `json:"appToken"`
				ExpiresIn int    `json:"expiresIn"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("token decode error: %w", err)
		}
		if result.ErrCode != 0 {
			return "", fmt.Errorf("token API error %d: %s", result.ErrCode, result.ErrMsg)
		}
		if result.Data.AppToken == "" {
			return "", fmt.Errorf("token API returned empty appToken")
		}

		log.Printf("[lansenger] app token refreshed (took %v, expires_in=%ds)", reqDuration, result.Data.ExpiresIn)
		g.tokens.set(result.Data.AppToken, result.Data.ExpiresIn)
		return result.Data.AppToken, nil
	}
	return "", lastErr
}

// getWebSocketURL creates a WebSocket endpoint via the API.
func (g *Gateway) getWebSocketURL(ctx context.Context) (string, error) {
	apiBase := strings.TrimRight(g.config.ApiGatewayURL, "/")
	endpointURL := apiBase + "/v1/ws/endpoint/create"

	body, _ := json.Marshal(map[string]string{
		"appId":  g.config.AppID,
		"secret": g.config.AppSecret,
	})

	var lastErr error
	for attempt := 1; attempt <= lansengerAPIMaxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		reqStart := time.Now()
		resp, err := g.client.Do(req)
		reqDuration := time.Since(reqStart)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lastErr = fmt.Errorf("ws endpoint request failed (took %v): %w", reqDuration, err)
			log.Printf("[lansenger] error: ws endpoint request failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, reqDuration, err)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("ws endpoint body read failed (took %v): %w", reqDuration, readErr)
			log.Printf("[lansenger] error: ws endpoint body read failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, reqDuration, readErr)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("ws endpoint HTTP error %d (took %v): %s", resp.StatusCode, reqDuration, string(respBody))
			log.Printf("[lansenger] error: ws endpoint HTTP %d (attempt %d/%d, took %v): %s", resp.StatusCode, attempt, lansengerAPIMaxRetry, reqDuration, string(respBody))
			if isRetryableHTTPStatus(resp.StatusCode) && attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return "", lastErr
		}

		var result struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				WsEndpoint string `json:"wsEndpoint"`
			} `json:"data"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", err
		}
		if result.ErrCode != 0 {
			return "", fmt.Errorf("ws endpoint error %d (took %v): %s", result.ErrCode, reqDuration, result.ErrMsg)
		}
		if result.Data.WsEndpoint != "" {
			wsURL, err := validateWebSocketURL(result.Data.WsEndpoint)
			if err != nil {
				return "", fmt.Errorf("invalid WS endpoint: %w", err)
			}
			log.Printf("[lansenger] got WS endpoint (took %v): %s", reqDuration, redactWebSocketURL(wsURL))
			return wsURL, nil
		}
		break
	}

	// Fallback: construct from a dedicated WebSocket gateway when configured,
	// otherwise derive it from the API gateway.
	wsBase := strings.TrimSpace(g.config.WebSocketBaseURL)
	if wsBase == "" {
		wsBase = apiBase
	}
	wsBase = strings.TrimRight(wsBase, "/")
	wsBase = strings.Replace(wsBase, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	fallbackRawURL := fmt.Sprintf("%s/open-apis/im/v1/ws/%s", wsBase, url.PathEscape(g.config.AppID))
	fallbackURL, err := validateWebSocketURL(fallbackRawURL)
	if err != nil {
		return "", fmt.Errorf("invalid WS fallback URL: %w", err)
	}
	log.Printf("[lansenger] warning: WS endpoint API returned empty, using fallback: %s", redactWebSocketURL(fallbackURL))
	return fallbackURL, nil
}

// validateWebSocketURL rejects malformed endpoint data before it reaches the
// reconnect loop. The endpoint is gateway-controlled but still external input;
// userinfo in particular must never be accepted or logged as part of a URL.
func validateWebSocketURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	if u.User != nil {
		return "", fmt.Errorf("userinfo is not allowed")
	}
	return u.String(), nil
}

// redactWebSocketURL removes query and fragment data before diagnostics. Some
// Lansenger deployments return one-time connection credentials in the endpoint
// query string, which must never be written to logs.
func redactWebSocketURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid websocket URL]"
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// ---------------------------------------------------------------------------
// REST API — sending messages
// ---------------------------------------------------------------------------

func (g *Gateway) apiURL(path string) string {
	return strings.TrimRight(g.config.ApiGatewayURL, "/") + path
}

// SendText sends a text message to a user or group.
// Optional Reminder (@mention) and RefMsgID (native quote) follow the蓝信 open
// API / OpenClaw Lansenger channel contract.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	if strings.TrimSpace(msg.ToUserID) == "" {
		return fmt.Errorf("lansenger: recipient is required")
	}
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}
	token, err := g.getAppToken(ctx)
	if err != nil {
		return err
	}
	return g.sendTextRetrying(ctx, token, msg)
}

// sendTextRetrying sends once, refreshes an expired token, then degrades optional
// decorations on structured API rejections only (never on network/context errors).
// Order prefers keeping @mention when possible (invalid refMsgId is a common reject):
//  1. drop refMsgId (native quote)
//  2. drop reminder (@mention), keep refMsgId
//  3. drop both
func (g *Gateway) sendTextRetrying(ctx context.Context, token string, msg OutgoingText) error {
	err := g.sendTextWithToken(ctx, token, msg, true)
	if err == nil {
		return nil
	}
	if isLansengerTokenExpiredError(err) {
		log.Printf("[lansenger] token expired while sending text, refreshing and retrying once")
		g.tokens.clear()
		freshToken, tokenErr := g.getAppToken(ctx)
		if tokenErr != nil {
			return tokenErr
		}
		err = g.sendTextWithToken(ctx, freshToken, msg, true)
		if err == nil {
			return nil
		}
		token = freshToken
	}
	if !isLansengerAPIError(err) {
		return err
	}

	type degrade struct {
		label   string
		withRem bool
		refID   string
	}
	var attempts []degrade
	hasRef := strings.TrimSpace(msg.RefMsgID) != ""
	hasRem := msg.Reminder != nil
	if hasRef {
		attempts = append(attempts, degrade{"without refMsgId", true, ""})
	}
	if hasRem {
		attempts = append(attempts, degrade{"without reminder", false, msg.RefMsgID})
	}
	if hasRem && hasRef {
		attempts = append(attempts, degrade{"without reminder+refMsgId", false, ""})
	}
	for _, a := range attempts {
		next := msg
		next.RefMsgID = a.refID
		log.Printf("[lansenger] SendText decoration failed (%v), retrying %s", err, a.label)
		if retryErr := g.sendTextWithToken(ctx, token, next, a.withRem); retryErr == nil {
			return nil
		} else {
			err = retryErr
			if !isLansengerAPIError(err) {
				return err
			}
		}
	}
	return err
}

func isLansengerAPIError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr)
}

func (g *Gateway) sendTextWithToken(ctx context.Context, token string, msg OutgoingText, withReminder bool) error {
	fmtData := map[string]any{"formatType": 1, "text": msg.Text}
	if withReminder {
		if rem := reminderPayload(msg.Reminder); rem != nil {
			fmtData["reminder"] = rem
		}
	}
	msgData := map[string]any{"formatText": fmtData}
	var extra map[string]any
	if id := strings.TrimSpace(msg.RefMsgID); id != "" {
		extra = map[string]any{"refMsgId": id}
	}
	if msg.IsGroup {
		return g.sendGroupMessage(ctx, token, msg.ToUserID, "formatText", msgData, extra)
	}
	return g.sendPrivateMessage(ctx, token, msg.ToUserID, "formatText", msgData, extra)
}

func reminderPayload(r *OutgoingReminder) map[string]any {
	if r == nil {
		return nil
	}
	out := map[string]any{}
	if r.All {
		out["all"] = true
	}
	userIDs := compactNonEmpty(r.UserIDs)
	botIDs := compactNonEmpty(r.BotIDs)
	if len(userIDs) > 0 {
		out["userIds"] = userIDs
	}
	if len(botIDs) > 0 {
		out["botIds"] = botIDs
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactNonEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// APIError is a structured Lansenger REST error (errCode/errMsg).
type APIError struct {
	Code int
	Msg  string
}

// Deprecated name kept as an unexported alias so internal call sites stay short.
type lansengerAPIError = APIError

func (e *APIError) Error() string {
	return fmt.Sprintf("lansenger API error %d: %s", e.Code, e.Msg)
}

func isLansengerTokenExpiredError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		msg := strings.ToLower(apiErr.Msg)
		if strings.Contains(msg, "token") && (strings.Contains(msg, "expired") || strings.Contains(msg, "invalid") || strings.Contains(msg, "expire")) {
			return true
		}
		switch apiErr.Code {
		case 40001, 40014, 42001:
			return true
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "token") && (strings.Contains(lower, "expired") || strings.Contains(lower, "invalid") || strings.Contains(lower, "expire"))
}

// SendMedia uploads media to Lansenger and sends it as a text message with
// mediaType + mediaIds attachment. Per the Lansenger API docs, media is sent
// via msgType="text" with mediaType (1=video, 2=image, 3=file) and mediaIds.
func (g *Gateway) SendMedia(ctx context.Context, msg OutgoingMedia) error {
	if strings.TrimSpace(msg.ToUserID) == "" {
		return fmt.Errorf("lansenger: recipient is required")
	}
	if len(msg.FileData) == 0 {
		return fmt.Errorf("lansenger: empty file data")
	}

	token, err := g.getAppToken(ctx)
	if err != nil {
		return err
	}

	if msg.FileName == "" {
		switch msg.MediaType {
		case "image":
			msg.FileName = "image.png"
		case "video":
			msg.FileName = "video.mp4"
		default:
			msg.FileName = "file.bin"
		}
	}

	if err := g.sendMediaWithToken(ctx, token, msg); err != nil {
		if isLansengerTokenExpiredError(err) {
			log.Printf("[lansenger] token expired while sending media, refreshing and retrying once")
			g.tokens.clear()
			freshToken, tokenErr := g.getAppToken(ctx)
			if tokenErr != nil {
				return tokenErr
			}
			return g.sendMediaWithToken(ctx, freshToken, msg)
		}
		return err
	}
	return nil
}

func (g *Gateway) sendMediaWithToken(ctx context.Context, token string, msg OutgoingMedia) error {
	mediaID, err := g.uploadMedia(ctx, token, msg.FileData, msg.FileName, msg.MediaType)
	if err != nil {
		log.Printf("[lansenger] media upload failed: %v, sending text fallback", err)
		if isLansengerTokenExpiredError(err) {
			return err
		}
		return g.SendText(ctx, OutgoingText{
			ToUserID: msg.ToUserID,
			Text:     fmt.Sprintf("[%s: %s upload failed]", msg.MediaType, msg.FileName),
			IsGroup:  msg.IsGroup,
		})
	}

	mediaTypeInt := 3
	switch msg.MediaType {
	case "image":
		mediaTypeInt = 2
	case "video":
		mediaTypeInt = 1
	}

	msgData := map[string]any{
		"text": map[string]any{
			"content":   msg.FileName,
			"mediaType": mediaTypeInt,
			"mediaIds":  []string{mediaID},
		},
	}

	if msg.IsGroup {
		return g.sendGroupMessage(ctx, token, msg.ToUserID, "text", msgData, nil)
	}
	return g.sendPrivateMessage(ctx, token, msg.ToUserID, "text", msgData, nil)
}

// uploadMedia uploads a file to Lansenger's media storage and returns the mediaId.
func (g *Gateway) uploadMedia(ctx context.Context, appToken string, data []byte, fileName, mediaType string) (string, error) {
	// The current /v1/app/medias/create API expects a descriptive upload type,
	// rather than the numeric mediaType used later in a text message payload.
	// Keep voice as an alias because callers use both names for audio files.
	typeParam := "file"
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image", "video", "file":
		typeParam = strings.ToLower(strings.TrimSpace(mediaType))
	case "audio", "voice":
		typeParam = "audio"
	}

	url := fmt.Sprintf("%s?type=%s&app_token=%s",
		g.apiURL("/v1/app/medias/create"), typeParam, url.QueryEscape(appToken))

	// Build multipart form.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if fileName == "" {
		fileName = "file"
		switch mediaType {
		case "image":
			fileName = "image.png"
		case "video":
			fileName = "video.mp4"
		case "file":
			fileName = "file.bin"
		}
	}

	part, err := writer.CreateFormFile("media", fileName)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write form data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := g.mediaClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			MediaID string `json:"mediaId"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("upload decode: %w", err)
	}
	if result.ErrCode != 0 {
		return "", &lansengerAPIError{Code: result.ErrCode, Msg: result.ErrMsg}
	}
	if result.Data.MediaID == "" {
		return "", fmt.Errorf("upload returned empty mediaId")
	}

	log.Printf("[lansenger] media uploaded: mediaId=%s type=%s name=%s size=%d",
		result.Data.MediaID, mediaType, fileName, len(data))
	return result.Data.MediaID, nil
}

// downloadMedia retrieves an inbound attachment. The gateway returns the
// original filename in Content-Disposition when available.
func (g *Gateway) downloadMedia(ctx context.Context, mediaID string) ([]byte, string, error) {
	if strings.TrimSpace(mediaID) == "" {
		return nil, "", fmt.Errorf("empty mediaId")
	}
	token, err := g.getAppToken(ctx)
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf("%s/%s/fetch?app_token=%s", g.apiURL("/v1/medias"), url.PathEscape(mediaID), url.QueryEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := g.mediaClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("media download HTTP %d: %s", resp.StatusCode, string(body))
	}
	const maxInboundMediaBytes = 50 << 20
	if resp.ContentLength > maxInboundMediaBytes {
		return nil, "", fmt.Errorf("media download exceeds %d byte limit", maxInboundMediaBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundMediaBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxInboundMediaBytes {
		return nil, "", fmt.Errorf("media download exceeds %d byte limit", maxInboundMediaBytes)
	}
	name := mediaFilename(resp.Header.Get("Content-Disposition"))
	return data, name, nil
}

func mediaFilename(disposition string) string {
	// RFC 5987 filename* takes precedence over the legacy filename parameter.
	for _, part := range strings.Split(disposition, ";") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "filename*=") {
			name := strings.TrimSpace(part[len("filename*="):])
			if pieces := strings.SplitN(name, "''", 2); len(pieces) == 2 {
				if decoded, err := url.PathUnescape(pieces[1]); err == nil {
					return safeMediaFilename(decoded)
				}
			}
		}
	}
	for _, part := range strings.Split(disposition, ";") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "filename=") {
			name := strings.Trim(strings.TrimSpace(part[len("filename="):]), "\"")
			return safeMediaFilename(name)
		}
	}
	return ""
}

func safeMediaFilename(name string) string {
	// Content-Disposition is remote input. Normalize both path separator styles
	// before taking the base name, then reject the special directory names.
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func (g *Gateway) sendPrivateMessage(ctx context.Context, token, userID, msgType string, msgData any, extra map[string]any) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("lansenger: private recipient is required")
	}
	url := fmt.Sprintf("%s?app_token=%s", g.apiURL("/v1/bot/messages/create"), url.QueryEscape(token))
	payload := map[string]any{
		"userIdList": []string{userID},
		"msgType":    msgType,
		"msgData":    msgData,
	}
	for k, v := range extra {
		if k == "" || v == nil {
			continue
		}
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	return g.doPost(ctx, url, body)
}

func (g *Gateway) sendGroupMessage(ctx context.Context, token, groupID, msgType string, msgData any, extra map[string]any) error {
	if strings.TrimSpace(groupID) == "" {
		return fmt.Errorf("lansenger: group recipient is required")
	}
	url := fmt.Sprintf("%s?app_token=%s", g.apiURL("/v1/messages/group/create"), url.QueryEscape(token))
	payload := map[string]any{
		"groupId": groupID,
		"msgType": msgType,
		"msgData": msgData,
	}
	for k, v := range extra {
		if k == "" || v == nil {
			continue
		}
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	return g.doPost(ctx, url, body)
}

func (g *Gateway) doPost(ctx context.Context, url string, body []byte) error {
	var lastErr error
	for attempt := 1; attempt <= lansengerAPIMaxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		reqStart := time.Now()
		resp, err := g.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			log.Printf("[lansenger] error: API POST failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, time.Since(reqStart), err)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return err
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			log.Printf("[lansenger] error: API POST body read failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, time.Since(reqStart), readErr)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return readErr
		}

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("lansenger API HTTP %d: %s", resp.StatusCode, string(respBody))
			lastErr = err
			log.Printf("[lansenger] error: API POST HTTP %d (attempt %d/%d, took %v): %s", resp.StatusCode, attempt, lansengerAPIMaxRetry, time.Since(reqStart), string(respBody))
			if isRetryableHTTPStatus(resp.StatusCode) && attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return err
		}

		var result struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil // response parsed OK, ignore decode issues
		}
		if result.ErrCode != 0 {
			log.Printf("[lansenger] error: API POST errCode=%d: %s (took %v)", result.ErrCode, result.ErrMsg, time.Since(reqStart))
			return &lansengerAPIError{Code: result.ErrCode, Msg: result.ErrMsg}
		}
		return nil
	}
	return lastErr
}

func apiRetryBackoff(ctx context.Context, attempt int) {
	delay := time.Duration(attempt) * 500 * time.Millisecond
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func isRetryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
