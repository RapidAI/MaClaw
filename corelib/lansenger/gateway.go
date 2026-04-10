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
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config holds the credentials and endpoints for a Lansenger bot.
type Config struct {
	AppID         string // Application ID (from token field 1)
	AppSecret     string // Application secret (from token field 2)
	ApiGatewayURL string // API gateway base URL (from token field 3)
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
	config  Config
	handler MessageHandler
	statusCb StatusCallback
	client  *http.Client
	tokens  tokenCache

	mu       sync.Mutex
	ws       *websocket.Conn
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  bool
}

// NewGateway creates a new Lansenger gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	return &Gateway{
		config:  config,
		handler: handler,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
		return nil
	}
	ctx2, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.mu.Unlock()

	// Validate credentials by fetching an app token.
	if _, err := g.getAppToken(); err != nil {
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

	if ws != nil {
		_ = ws.Close()
	}
	g.wg.Wait()
	g.emitStatus("disconnected")
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
	defer g.wg.Done()
	attempt := 0
	maxAttempts := 50

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if attempt >= maxAttempts {
			log.Printf("[lansenger] max reconnect attempts reached, stopping")
			g.mu.Lock()
			g.running = false
			g.mu.Unlock()
			g.emitStatus("error")
			return
		}

		wsURL, err := g.getWebSocketURL()
		if err != nil {
			log.Printf("[lansenger] get WS URL failed (attempt %d): %v", attempt, err)
			attempt++
			g.backoff(ctx, attempt)
			continue
		}

		log.Printf("[lansenger] connecting to WebSocket: %s", wsURL)
		g.emitStatus("connecting")

		dialer := websocket.Dialer{
			TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
			HandshakeTimeout: 10 * time.Second,
		}
		conn, _, err := dialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			log.Printf("[lansenger] WS dial failed (attempt %d): %v", attempt, err)
			attempt++
			g.backoff(ctx, attempt)
			continue
		}

		g.mu.Lock()
		g.ws = conn
		g.mu.Unlock()

		attempt = 0
		g.emitStatus("connected")
		log.Printf("[lansenger] WebSocket connected")

		// Read loop — blocks until error or close.
		g.readLoop(ctx, conn)

		g.mu.Lock()
		g.ws = nil
		g.mu.Unlock()

		g.emitStatus("reconnecting")
		attempt++
		g.backoff(ctx, attempt)
	}
}

func (g *Gateway) readLoop(ctx context.Context, conn *websocket.Conn) {
	// Start heartbeat.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go g.heartbeatLoop(hbCtx, conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.Printf("[lansenger] WS read error: %v", err)
			}
			return
		}

		g.handleWSMessage(message)
	}
}

func (g *Gateway) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(55 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.mu.Lock()
			ws := g.ws
			g.mu.Unlock()
			if ws != conn {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				log.Printf("[lansenger] heartbeat ping failed: %v", err)
				return
			}
		}
	}
}

func (g *Gateway) backoff(ctx context.Context, attempt int) {
	delay := time.Duration(attempt) * 3 * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
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

func (g *Gateway) getAppToken() (string, error) {
	if tok, ok := g.tokens.get(); ok {
		return tok, nil
	}

	url := fmt.Sprintf("%s/v1/apptoken/create?grant_type=client_credential&appid=%s&secret=%s",
		strings.TrimRight(g.config.ApiGatewayURL, "/"),
		g.config.AppID,
		g.config.AppSecret,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("token HTTP %d: %s", resp.StatusCode, string(body))
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

	g.tokens.set(result.Data.AppToken, result.Data.ExpiresIn)
	return result.Data.AppToken, nil
}

// getWebSocketURL creates a WebSocket endpoint via the API.
func (g *Gateway) getWebSocketURL() (string, error) {
	apiBase := strings.TrimRight(g.config.ApiGatewayURL, "/")
	url := apiBase + "/v1/ws/endpoint/create"

	body, _ := json.Marshal(map[string]string{
		"appId":  g.config.AppID,
		"secret": g.config.AppSecret,
	})

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ws endpoint HTTP %d: %s", resp.StatusCode, string(respBody))
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
		return "", fmt.Errorf("ws endpoint error %d: %s", result.ErrCode, result.ErrMsg)
	}
	if result.Data.WsEndpoint != "" {
		return result.Data.WsEndpoint, nil
	}

	// Fallback: construct from gateway URL.
	wsBase := strings.Replace(apiBase, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	return fmt.Sprintf("%s/open-apis/im/v1/ws/%s", wsBase, g.config.AppID), nil
}

// ---------------------------------------------------------------------------
// REST API — sending messages
// ---------------------------------------------------------------------------

func (g *Gateway) apiURL(path string) string {
	return strings.TrimRight(g.config.ApiGatewayURL, "/") + path
}

// SendText sends a text message to a user or group.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	token, err := g.getAppToken()
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

	token, err := g.getAppToken()
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
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("lansenger API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil // response parsed OK, ignore decode issues
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("lansenger API error %d: %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}
