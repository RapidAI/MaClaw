// Package lansenger implements a client-side gateway for the Lansenger (蓝信)
// messaging platform. It connects via WebSocket to receive messages and uses
// REST APIs to send messages, following the Lansenger Open Platform protocol.
package lansenger

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	lansengerReadWait     = 180 * time.Second
	lansengerPingInterval = 15 * time.Second
	lansengerPingWait     = 15 * time.Second
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
	FromUserID  string // staffId of the sender
	Text        string // text content
	MessageID   string // platform message ID
	MessageType string // "text", "formatText", "image", "file", etc.
	ChatType    string // "p2p" or "group"
	GroupID     string // group ID if group message
	MediaType   string // "image", "video", "file", "voice" or ""
	MediaData   []byte // downloaded media bytes (if any)
	MediaName   string // media file name
}

// OutgoingText is a plain text message to send.
type OutgoingText struct {
	ToUserID string
	Text     string
	IsGroup  bool
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
	tc.expiresAt = time.Now().Add(ttl - margin)
}

// ---------------------------------------------------------------------------
// Gateway
// ---------------------------------------------------------------------------

// Gateway manages the WebSocket connection and REST API calls to Lansenger.
type Gateway struct {
	config   Config
	handler  MessageHandler
	statusCb StatusCallback
	client   *http.Client
	tokens   tokenCache

	mu      sync.Mutex
	ws      *websocket.Conn
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewGateway creates a new Lansenger gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &Gateway{
		config:  config,
		handler: handler,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           dialer.DialContext,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
			},
		},
	}
}

// SetStatusCallback sets the status change callback.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.statusCb = cb
}

func (g *Gateway) emitStatus(status string) {
	if g.statusCb != nil {
		g.statusCb(status)
	}
}

// Start connects to Lansenger via WebSocket and begins receiving messages.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		log.Printf("[lansenger] Start called but already running")
		return nil
	}
	ctx2, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.mu.Unlock()

	log.Printf("[lansenger] starting gateway: appID=%s gateway=%s", g.config.AppID, g.config.ApiGatewayURL)

	// Validate credentials by fetching an app token.
	if _, err := g.getAppToken(ctx2); err != nil {
		g.emitStatus("error")
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		cancel()
		return fmt.Errorf("lansenger: auth failed: %w", err)
	}

	g.wg.Add(1)
	go g.connectLoop(ctx2)
	return nil
}

// Stop gracefully shuts down the gateway.
func (g *Gateway) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = false
	if g.cancel != nil {
		g.cancel()
	}
	ws := g.ws
	g.ws = nil
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

func (g *Gateway) connectLoop(ctx context.Context) {
	defer g.finishConnectLoop(ctx)
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

		log.Printf("[lansenger] dialing WebSocket (attempt %d): %s", attempt+1, wsURL)
		g.emitStatus("connecting")

		dialStart := time.Now()
		dialer := websocket.Dialer{
			TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
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

		g.mu.Lock()
		g.ws = conn
		g.mu.Unlock()

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
		g.ws = nil
		g.mu.Unlock()

		g.emitStatus("reconnecting")
		log.Printf("[lansenger] warning: connection lost after %v uptime, reconnecting (attempt %d)",
			uptime.Truncate(time.Second), attempt+1)
		attempt++
		g.backoff(ctx, attempt)
	}
}

func (g *Gateway) finishConnectLoop(ctx context.Context) {
	g.mu.Lock()
	wasRunning := g.running
	g.running = false
	ws := g.ws
	g.ws = nil
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
	// Start heartbeat.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go g.heartbeatLoop(hbCtx, conn)
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
	loopStart := time.Now()

	// Set Pong handler to reset read deadline when server responds to our Ping.
	conn.SetPongHandler(func(appData string) error {
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

func (g *Gateway) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
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

// ---------------------------------------------------------------------------
// WebSocket message parsing
// ---------------------------------------------------------------------------

type wsEnvelope struct {
	Events []wsEvent `json:"events"`
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
	Data      *wsEventData    `json:"data"` // nested data
}

func (g *Gateway) handleWSMessage(raw []byte) {
	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("[lansenger] WS message parse error: %v", err)
		return
	}

	for _, evt := range env.Events {
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
	actual := &data
	if actual.Data != nil && actual.From == "" {
		actual = actual.Data
	}

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
	chatType := actual.ChatType
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
		FromUserID:  from,
		Text:        text,
		MessageID:   msgID,
		MessageType: msgType,
		ChatType:    chatType,
		GroupID:     actual.GroupID,
	}

	if g.handler != nil {
		g.handler(msg)
	}
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
		if textObj, ok := wrapper["text"]; ok {
			var t struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(textObj, &t) == nil {
				return t.Content
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
	case "image":
		return "[图片]"
	case "video":
		return "[视频]"
	case "file":
		return "[文件]"
	case "voice":
		return "[语音]"
	}
	return ""
}

// ---------------------------------------------------------------------------
// REST API — authentication
// ---------------------------------------------------------------------------

func (g *Gateway) getAppToken(ctx context.Context) (string, error) {
	if tok, ok := g.tokens.get(); ok {
		return tok, nil
	}

	log.Printf("[lansenger] refreshing app token from %s", g.config.ApiGatewayURL)
	url := fmt.Sprintf("%s/v1/apptoken/create?grant_type=client_credential&appid=%s&secret=%s",
		strings.TrimRight(g.config.ApiGatewayURL, "/"),
		g.config.AppID,
		g.config.AppSecret,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	reqStart := time.Now()
	resp, err := g.client.Do(req)
	reqDuration := time.Since(reqStart)
	if err != nil {
		return "", fmt.Errorf("token request failed (took %v): %w", reqDuration, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("token HTTP error %d (took %v): %s", resp.StatusCode, reqDuration, string(body))
	}

	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			AppToken  string `json:"appToken"`
			ExpiresIn int    `json:"expiresIn"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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

// getWebSocketURL creates a WebSocket endpoint via the API.
func (g *Gateway) getWebSocketURL(ctx context.Context) (string, error) {
	apiBase := strings.TrimRight(g.config.ApiGatewayURL, "/")
	url := apiBase + "/v1/ws/endpoint/create"

	body, _ := json.Marshal(map[string]string{
		"appId":  g.config.AppID,
		"secret": g.config.AppSecret,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	reqStart := time.Now()
	resp, err := g.client.Do(req)
	reqDuration := time.Since(reqStart)
	if err != nil {
		return "", fmt.Errorf("ws endpoint request failed (took %v): %w", reqDuration, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ws endpoint HTTP error %d (took %v): %s", resp.StatusCode, reqDuration, string(respBody))
	}

	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			WsEndpoint string `json:"wsEndpoint"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("ws endpoint error %d (took %v): %s", result.ErrCode, reqDuration, result.ErrMsg)
	}
	if result.Data.WsEndpoint != "" {
		log.Printf("[lansenger] got WS endpoint (took %v): %s", reqDuration, result.Data.WsEndpoint)
		return result.Data.WsEndpoint, nil
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
	fallbackURL := fmt.Sprintf("%s/open-apis/im/v1/ws/%s", wsBase, g.config.AppID)
	log.Printf("[lansenger] warning: WS endpoint API returned empty, using fallback: %s", fallbackURL)
	return fallbackURL, nil
}

// ---------------------------------------------------------------------------
// REST API — sending messages
// ---------------------------------------------------------------------------

func (g *Gateway) apiURL(path string) string {
	return strings.TrimRight(g.config.ApiGatewayURL, "/") + path
}

// SendText sends a text message to a user or group.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	token, err := g.getAppToken(ctx)
	if err != nil {
		return err
	}

	if msg.IsGroup {
		return g.sendGroupMessage(ctx, token, msg.ToUserID, "formatText", map[string]any{
			"formatText": map[string]any{"formatType": 1, "text": msg.Text},
		})
	}
	return g.sendPrivateMessage(ctx, token, msg.ToUserID, "formatText", map[string]any{
		"formatText": map[string]any{"formatType": 1, "text": msg.Text},
	})
}

// SendMedia uploads media to Lansenger and sends it as a text message with
// mediaType + mediaIds attachment. Per the Lansenger API docs, media is sent
// via msgType="text" with mediaType (1=video, 2=image, 3=file) and mediaIds.
func (g *Gateway) SendMedia(ctx context.Context, msg OutgoingMedia) error {
	if len(msg.FileData) == 0 {
		return fmt.Errorf("lansenger: empty file data")
	}

	token, err := g.getAppToken(ctx)
	if err != nil {
		return err
	}

	fileName := msg.FileName
	if fileName == "" {
		switch msg.MediaType {
		case "image":
			fileName = "image.png"
		case "video":
			fileName = "video.mp4"
		default:
			fileName = "file.bin"
		}
	}

	// Upload media to get mediaId.
	mediaID, err := g.uploadMedia(ctx, token, msg.FileData, fileName, msg.MediaType)
	if err != nil {
		log.Printf("[lansenger] media upload failed: %v, sending text fallback", err)
		return g.SendText(ctx, OutgoingText{
			ToUserID: msg.ToUserID,
			Text:     fmt.Sprintf("[%s: %s — 上传失败]", msg.MediaType, fileName),
			IsGroup:  msg.IsGroup,
		})
	}

	// Map media type to Lansenger's mediaType int: 1=video, 2=image, 3=file
	mediaTypeInt := 3 // file
	switch msg.MediaType {
	case "image":
		mediaTypeInt = 2
	case "video":
		mediaTypeInt = 1
	}

	// Send as msgType="text" with mediaType + mediaIds per official docs.
	msgData := map[string]any{
		"text": map[string]any{
			"content":   fileName,
			"mediaType": mediaTypeInt,
			"mediaIds":  []string{mediaID},
		},
	}

	if msg.IsGroup {
		return g.sendGroupMessage(ctx, token, msg.ToUserID, "text", msgData)
	}
	return g.sendPrivateMessage(ctx, token, msg.ToUserID, "text", msgData)
}

// uploadMedia uploads a file to Lansenger's media storage and returns the mediaId.
func (g *Gateway) uploadMedia(ctx context.Context, appToken string, data []byte, fileName, mediaType string) (string, error) {
	// Map media type to Lansenger's type parameter: 2=image, 1=video, 3=audio, default=2
	typeParam := "2" // image
	switch mediaType {
	case "video":
		typeParam = "1"
	case "audio", "voice":
		typeParam = "3"
	case "file":
		typeParam = "2" // files also use type=2 in the API
	}

	url := fmt.Sprintf("%s?type=%s&app_token=%s",
		g.apiURL("/v1/medias/create"), typeParam, appToken)

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

	resp, err := g.client.Do(req)
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
		return "", fmt.Errorf("upload API error %d: %s", result.ErrCode, result.ErrMsg)
	}
	if result.Data.MediaID == "" {
		return "", fmt.Errorf("upload returned empty mediaId")
	}

	log.Printf("[lansenger] media uploaded: mediaId=%s type=%s name=%s size=%d",
		result.Data.MediaID, mediaType, fileName, len(data))
	return result.Data.MediaID, nil
}

func (g *Gateway) sendPrivateMessage(ctx context.Context, token, userID, msgType string, msgData any) error {
	url := fmt.Sprintf("%s?app_token=%s", g.apiURL("/v1/bot/messages/create"), token)
	body, _ := json.Marshal(map[string]any{
		"userIdList": []string{userID},
		"msgType":    msgType,
		"msgData":    msgData,
	})
	return g.doPost(ctx, url, body)
}

func (g *Gateway) sendGroupMessage(ctx context.Context, token, groupID, msgType string, msgData any) error {
	url := fmt.Sprintf("%s?app_token=%s", g.apiURL("/v1/messages/group/create"), token)
	body, _ := json.Marshal(map[string]any{
		"groupId": groupID,
		"msgType": msgType,
		"msgData": msgData,
	})
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
			return fmt.Errorf("lansenger API error %d: %s", result.ErrCode, result.ErrMsg)
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
