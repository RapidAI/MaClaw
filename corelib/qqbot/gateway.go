// Package qqbot implements a client-side QQ Bot WebSocket gateway.
//
// The gateway connects to QQ's official WebSocket endpoint using the user's
// own AppID/AppSecret, receives C2C messages, and forwards them to the Hub
// via the existing machine WebSocket connection. Outbound messages (replies
// from the Hub agent) are sent back to QQ via REST API.
//
// This runs entirely on the client machine — the Hub never touches QQ tokens.
package qqbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/gorilla/websocket"
	silk "github.com/wdvxdr1123/go-silk"
)

const (
	qqAPIBase         = "https://api.sgroup.qq.com"
	tokenEndpoint     = "https://bots.qq.com/app/getAppAccessToken"
	qqVoiceSampleRate = 24000
	qqVoiceBitRate    = 24000

	intentsGroupAndC2C = 1 << 25

	reconnectBaseDelay = 3 * time.Second
	reconnectMaxDelay  = 60 * time.Second
	maxReconnects      = 50
)

// Config holds the user's QQ Bot credentials.
type Config struct {
	AppID     string
	AppSecret string
}

// IncomingMessage is a QQ C2C message received from the gateway.
type IncomingMessage struct {
	OpenID    string
	Text      string
	RawData   json.RawMessage
	Timestamp time.Time
	// Media fields (populated when the message contains attachments).
	MediaType string // "image", "file", "voice", "video", or ""
	MediaData []byte // raw file bytes (downloaded)
	MediaName string // original file name
	MimeType  string // MIME type
}

// OutgoingText is a text message to send to a QQ user.
type OutgoingText struct {
	OpenID string
	Text   string
}

// OutgoingMedia is a rich media message to send to a QQ user.
type OutgoingMedia struct {
	OpenID   string
	FileType int    // 1=image, 2=video, 3=voice, 4=file
	FileData string // base64
	FileName string
	MimeType string
	Caption  string
	FileURL  string // used for large files instead of inline base64
}

// MessageHandler is called when a C2C message arrives from QQ.
type MessageHandler func(msg IncomingMessage)

// StatusCallback is called when the gateway connection status changes.
type StatusCallback func(status string)

// Gateway manages the QQ Bot WebSocket connection on the client side.
type Gateway struct {
	config   Config
	handler  MessageHandler
	onStatus StatusCallback
	client   *http.Client

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool

	// token cache
	tokenMu      sync.Mutex
	accessToken  string
	tokenExpires time.Time

	// ws write serialisation
	wsMu sync.Mutex

	// sequence tracking
	seqMu   sync.Mutex
	lastSeq *int

	// Per-user message processing locks — ensures messages from the same
	// user are handled sequentially while different users run concurrently.
	userLocks   map[string]*sync.Mutex
	userLocksMu sync.Mutex

	// handlerWg tracks in-flight handler goroutines so Stop() can wait
	// for them to finish before returning.
	handlerWg sync.WaitGroup

	// interruptHandler is called when a new message arrives while the
	// per-user lock is held. See corelib/progress.InterruptHandler.
	interruptHandler progress.InterruptHandler
}

// wsPayload is the QQ Bot WebSocket payload structure.
type wsPayload struct {
	ID string          `json:"id,omitempty"`
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int            `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// NewGateway creates a new QQ Bot gateway.
func NewGateway(config Config, handler MessageHandler) *Gateway {
	return &Gateway{
		config:    config,
		handler:   handler,
		client:    &http.Client{Timeout: 60 * time.Second},
		userLocks: make(map[string]*sync.Mutex),
	}
}

// SetStatusCallback sets a callback for connection status changes.
func (g *Gateway) SetStatusCallback(cb StatusCallback) {
	g.onStatus = cb
}

// SetInterruptHandler sets the handler for interrupt signals.
func (g *Gateway) SetInterruptHandler(ih progress.InterruptHandler) {
	g.interruptHandler = ih
}

// Start launches the WebSocket gateway in the background.
func (g *Gateway) Start(ctx context.Context) error {
	if g.config.AppID == "" || g.config.AppSecret == "" {
		return fmt.Errorf("qqbot: AppID and AppSecret are required")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return nil
	}

	wsCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.running = true
	g.wg.Add(1)
	go g.runGateway(wsCtx)
	log.Printf("[qqbot/gw] started")
	g.emitStatus("connecting")
	return nil
}

// Stop shuts down the gateway.
func (g *Gateway) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.running = false
	g.cancel = nil
	g.mu.Unlock()

	g.wg.Wait()        // wait for runGateway to exit
	g.handlerWg.Wait() // wait for in-flight handler goroutines

	g.seqMu.Lock()
	g.lastSeq = nil
	g.seqMu.Unlock()

	log.Printf("[qqbot/gw] stopped")
	g.emitStatus("disconnected")
	return nil
}

// IsRunning returns whether the gateway is currently running.
func (g *Gateway) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

func (g *Gateway) emitStatus(status string) {
	if g.onStatus != nil {
		g.onStatus(status)
	}
}

// userLock returns a per-user mutex, creating one if it doesn't exist yet.
func (g *Gateway) userLock(userKey string) *sync.Mutex {
	g.userLocksMu.Lock()
	defer g.userLocksMu.Unlock()
	ul, ok := g.userLocks[userKey]
	if !ok {
		ul = &sync.Mutex{}
		g.userLocks[userKey] = ul
	}
	return ul
}

// UpdateConfig updates the gateway config. Caller should Stop/Start to apply.
func (g *Gateway) UpdateConfig(config Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.config = config
}

// ---------------------------------------------------------------------------
// AccessToken management
// ---------------------------------------------------------------------------

func (g *Gateway) getAccessToken(ctx context.Context) (string, error) {
	g.tokenMu.Lock()
	defer g.tokenMu.Unlock()

	if g.accessToken != "" && time.Now().Before(g.tokenExpires.Add(-60*time.Second)) {
		return g.accessToken, nil
	}

	body, _ := json.Marshal(map[string]string{
		"appId":        g.config.AppID,
		"clientSecret": g.config.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("qqbot: token request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("qqbot: token decode failed: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("qqbot: empty access_token returned")
	}

	expSec, _ := result.ExpiresIn.Int64()
	if expSec <= 0 {
		expSec = 7200
	}
	g.accessToken = result.AccessToken
	g.tokenExpires = time.Now().Add(time.Duration(expSec) * time.Second)
	log.Printf("[qqbot/gw] access token refreshed, expires in %ds", expSec)
	return g.accessToken, nil
}

// ---------------------------------------------------------------------------
// WebSocket Gateway — reconnect loop + connect/identify/heartbeat
// ---------------------------------------------------------------------------

func (g *Gateway) runGateway(ctx context.Context) {
	defer g.wg.Done()

	var sessionID string
	reconnects := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		gotReady, err := g.connectAndRun(ctx, &sessionID)
		if ctx.Err() != nil {
			return
		}

		if gotReady {
			reconnects = 0
		}

		reconnects++
		if reconnects > maxReconnects {
			log.Printf("[qqbot/gw] max reconnect attempts (%d) reached, giving up", maxReconnects)
			g.emitStatus("error")
			return
		}

		shift := reconnects - 1
		if shift > 4 {
			shift = 4
		}
		delay := reconnectBaseDelay * time.Duration(1<<shift)
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
		if err != nil {
			log.Printf("[qqbot/gw] connection error: %v, reconnecting in %v", err, delay)
		}
		g.emitStatus("reconnecting")

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (g *Gateway) connectAndRun(ctx context.Context, sessionID *string) (gotReady bool, err error) {
	token, err := g.getAccessToken(ctx)
	if err != nil {
		return false, fmt.Errorf("get token: %w", err)
	}

	gatewayURL, err := g.getGatewayURL(ctx, token)
	if err != nil {
		return false, fmt.Errorf("get gateway: %w", err)
	}

	log.Printf("[qqbot/gw] connecting to %s", gatewayURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	wsWrite := func(v any) error {
		g.wsMu.Lock()
		defer g.wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Read Hello (op=10)
	var hello wsPayload
	if err := conn.ReadJSON(&hello); err != nil {
		return false, fmt.Errorf("read hello: %w", err)
	}
	if hello.Op != 10 {
		return false, fmt.Errorf("expected op=10 Hello, got op=%d", hello.Op)
	}

	var helloData struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &helloData)
	heartbeatMs := helloData.HeartbeatInterval
	if heartbeatMs <= 0 {
		heartbeatMs = 45000
	}

	// Send Identify (op=2) or Resume (op=6)
	g.seqMu.Lock()
	lastSeq := g.lastSeq
	g.seqMu.Unlock()

	if *sessionID != "" && lastSeq != nil {
		resume := map[string]any{
			"op": 6,
			"d": map[string]any{
				"token":      "QQBot " + token,
				"session_id": *sessionID,
				"seq":        *lastSeq,
			},
		}
		if err := wsWrite(resume); err != nil {
			return false, fmt.Errorf("send resume: %w", err)
		}
	} else {
		identify := map[string]any{
			"op": 2,
			"d": map[string]any{
				"token":   "QQBot " + token,
				"intents": intentsGroupAndC2C,
				"shard":   []int{0, 1},
			},
		}
		if err := wsWrite(identify); err != nil {
			return false, fmt.Errorf("send identify: %w", err)
		}
	}

	// Start heartbeat goroutine
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go func() {
		ticker := time.NewTicker(time.Duration(heartbeatMs) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				g.seqMu.Lock()
				seq := g.lastSeq
				g.seqMu.Unlock()
				if err := wsWrite(map[string]any{"op": 1, "d": seq}); err != nil {
					log.Printf("[qqbot/gw] heartbeat send error: %v", err)
					return
				}
			}
		}
	}()

	// Read loop
	for {
		select {
		case <-ctx.Done():
			return gotReady, nil
		default:
		}

		var payload wsPayload
		if err := conn.ReadJSON(&payload); err != nil {
			return gotReady, fmt.Errorf("read: %w", err)
		}

		if payload.S != nil {
			g.seqMu.Lock()
			g.lastSeq = payload.S
			g.seqMu.Unlock()
		}

		switch payload.Op {
		case 0: // Dispatch
			if payload.T == "READY" || payload.T == "RESUMED" {
				gotReady = true
				g.emitStatus("connected")
			}
			g.handleDispatch(payload.T, payload.D, sessionID)
		case 1: // Heartbeat request from server
			g.seqMu.Lock()
			seq := g.lastSeq
			g.seqMu.Unlock()
			_ = wsWrite(map[string]any{"op": 1, "d": seq})
		case 7: // Reconnect
			log.Printf("[qqbot/gw] server requested reconnect (op=7)")
			return gotReady, nil
		case 9: // Invalid Session
			canResume := false
			_ = json.Unmarshal(payload.D, &canResume)
			if !canResume {
				*sessionID = ""
				g.seqMu.Lock()
				g.lastSeq = nil
				g.seqMu.Unlock()
			}
			log.Printf("[qqbot/gw] invalid session, canResume=%v", canResume)
			return gotReady, nil
		case 11: // Heartbeat ACK
			// ok
		}
	}
}

// ---------------------------------------------------------------------------
// Dispatch handling
// ---------------------------------------------------------------------------

func (g *Gateway) handleDispatch(eventType string, data json.RawMessage, sessionID *string) {
	switch eventType {
	case "READY":
		var ready struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(data, &ready)
		*sessionID = ready.SessionID
		log.Printf("[qqbot/gw] READY, session=%s", ready.SessionID)
	case "RESUMED":
		log.Printf("[qqbot/gw] session resumed")
	case "C2C_MESSAGE_CREATE":
		go g.handleC2CMessage(data)
	case "GROUP_AT_MESSAGE_CREATE":
		log.Printf("[qqbot/gw] group @message received (not yet supported)")
	default:
		log.Printf("[qqbot/gw] unhandled event: %s", eventType)
	}
}

func (g *Gateway) handleC2CMessage(data json.RawMessage) {
	var event struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			UserOpenID string `json:"user_openid"`
		} `json:"author"`
		Timestamp   string `json:"timestamp"`
		Attachments []struct {
			ContentType string `json:"content_type"`
			Filename    string `json:"filename"`
			URL         string `json:"url"`
			Size        int64  `json:"size"`
		} `json:"attachments,omitempty"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("[qqbot/gw] parse C2C message failed: %v", err)
		return
	}

	openID := event.Author.UserOpenID
	text := strings.TrimSpace(event.Content)
	hasAttachment := len(event.Attachments) > 0

	if openID == "" || (text == "" && !hasAttachment) {
		return
	}

	log.Printf("[qqbot/gw] C2C from %s: %s attachments=%d", openID, truncate(text, 80), len(event.Attachments))

	if g.handler != nil {
		incoming := IncomingMessage{
			OpenID:    openID,
			Text:      text,
			RawData:   data,
			Timestamp: time.Now(),
		}

		// Download the first attachment if present
		if hasAttachment {
			att := event.Attachments[0]
			mediaType := "file"
			if strings.HasPrefix(att.ContentType, "image/") {
				mediaType = "image"
			} else if strings.HasPrefix(att.ContentType, "video/") {
				mediaType = "video"
			} else if strings.HasPrefix(att.ContentType, "audio/") {
				mediaType = "voice"
			}
			if att.URL != "" {
				dlData, err := g.downloadURL(att.URL)
				if err != nil {
					log.Printf("[qqbot/gw] download attachment failed: %v", err)
				} else {
					incoming.MediaType = mediaType
					incoming.MediaData = dlData
					incoming.MediaName = att.Filename
					incoming.MimeType = att.ContentType
				}
			}
		}

		ul := g.userLock(openID)
		g.handlerWg.Add(1)
		go func() {
			defer g.handlerWg.Done()
			locked := ul.TryLock()
			if !locked {
				// Try interrupt handler before blocking.
				if g.interruptHandler != nil && incoming.Text != "" {
					result := g.interruptHandler.TryInterrupt(openID, incoming.Text)
					if result.Handled {
						replyText := result.Reply
						if len(result.Corrections) > 0 && replyText != "" {
							replyText = progress.FormatCorrectionsText(replyText, result.Corrections)
						}
						if replyText != "" {
							_ = g.SendText(context.Background(), OutgoingText{
								OpenID: openID,
								Text:   replyText,
							})
						}
						return // Cancel/Merge/StatusQuery — fully handled
					}
					if result.Queued && result.Reply != "" {
						replyText := result.Reply
						if len(result.Corrections) > 0 {
							replyText = progress.FormatCorrectionsText(replyText, result.Corrections)
						}
						_ = g.SendText(context.Background(), OutgoingText{
							OpenID: openID,
							Text:   replyText,
						})
					}
				}
				ul.Lock()
			}
			defer ul.Unlock()
			g.handler(incoming)
		}()
	}
}

// downloadURL downloads content from a URL with a size limit.
func (g *Gateway) downloadURL(rawURL string) ([]byte, error) {
	// Validate URL scheme to prevent SSRF
	if !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "http://") {
		return nil, fmt.Errorf("invalid URL scheme: %s", rawURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
}

// ---------------------------------------------------------------------------
// Gateway URL
// ---------------------------------------------------------------------------

func (g *Gateway) getGatewayURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qqAPIBase+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("gateway API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.URL == "" {
		return "", fmt.Errorf("empty gateway URL")
	}
	return result.URL, nil
}

// ---------------------------------------------------------------------------
// Send C2C message via REST API
// ---------------------------------------------------------------------------

// SendText sends a text message to a QQ user via REST API.
func (g *Gateway) SendText(ctx context.Context, msg OutgoingText) error {
	token, err := g.getAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/v2/users/%s/messages", qqAPIBase, msg.OpenID)
	body, _ := json.Marshal(map[string]any{
		"content":  msg.Text,
		"msg_type": 0,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("qqbot: send message failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("qqbot: send message HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// SendMedia sends a rich media message to a QQ user via REST API.
func (g *Gateway) SendMedia(ctx context.Context, msg OutgoingMedia) error {
	token, err := g.getAccessToken(ctx)
	if err != nil {
		return err
	}
	if msg.FileType == 3 {
		voiceData, err := prepareQQVoiceData(msg.FileName, msg.FileData)
		if err != nil {
			return err
		}
		msg.FileData = voiceData
		msg.FileName = "voice.silk"
		msg.MimeType = "audio/silk"
	}

	// Step 1: Upload media
	uploadURL := fmt.Sprintf("%s/v2/users/%s/files", qqAPIBase, msg.OpenID)
	uploadPayload := map[string]any{
		"file_type":    msg.FileType,
		"srv_send_msg": false,
	}
	if msg.FileURL != "" {
		uploadPayload["url"] = msg.FileURL
	} else {
		uploadPayload["file_data"] = msg.FileData
	}
	if msg.FileType == 4 && msg.FileName != "" {
		uploadPayload["file_name"] = msg.FileName
	}

	uploadBody, _ := json.Marshal(uploadPayload)
	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(uploadBody))
	if err != nil {
		return fmt.Errorf("qqbot: create upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", "application/json")
	uploadReq.Header.Set("Authorization", "QQBot "+token)

	uploadResp, err := g.client.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("qqbot: upload media failed: %w", err)
	}
	defer uploadResp.Body.Close()

	uploadRespBody, _ := io.ReadAll(io.LimitReader(uploadResp.Body, 8192))
	if uploadResp.StatusCode >= 300 {
		return fmt.Errorf("qqbot: upload media HTTP %d: %s", uploadResp.StatusCode, string(uploadRespBody))
	}

	var uploadResult struct {
		FileInfo string `json:"file_info"`
	}
	if err := json.Unmarshal(uploadRespBody, &uploadResult); err != nil {
		return fmt.Errorf("qqbot: parse upload response: %w", err)
	}
	if uploadResult.FileInfo == "" {
		return fmt.Errorf("qqbot: upload returned empty file_info")
	}

	// Step 2: Send rich media message
	msgURL := fmt.Sprintf("%s/v2/users/%s/messages", qqAPIBase, msg.OpenID)
	msgBody, _ := json.Marshal(map[string]any{
		"msg_type": 7,
		"media":    map[string]any{"file_info": uploadResult.FileInfo},
	})

	msgReq, err := http.NewRequestWithContext(ctx, http.MethodPost, msgURL, bytes.NewReader(msgBody))
	if err != nil {
		return fmt.Errorf("qqbot: create message request: %w", err)
	}
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Authorization", "QQBot "+token)

	msgResp, err := g.client.Do(msgReq)
	if err != nil {
		return fmt.Errorf("qqbot: send media message failed: %w", err)
	}
	defer msgResp.Body.Close()

	if msgResp.StatusCode >= 300 {
		msgRespBody, _ := io.ReadAll(io.LimitReader(msgResp.Body, 4096))
		return fmt.Errorf("qqbot: send media message HTTP %d: %s", msgResp.StatusCode, string(msgRespBody))
	}

	if msg.Caption != "" {
		_ = g.SendText(ctx, OutgoingText{OpenID: msg.OpenID, Text: msg.Caption})
	}
	return nil
}

type qqVoiceMetadata struct {
	sampleRate    int
	channels      int
	bitsPerSample int
	dataStart     int
	dataSize      int
}

func prepareQQVoiceData(fileName, b64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return "", fmt.Errorf("qqbot: voice base64 decode failed: %w", err)
		}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("qqbot: empty voice data")
	}
	if isTencentSilk(data) {
		return base64.StdEncoding.EncodeToString(data), nil
	}
	if strings.EqualFold(filepath.Ext(fileName), ".silk") || strings.EqualFold(filepath.Ext(fileName), ".slk") {
		return "", fmt.Errorf("qqbot: invalid SILK voice payload")
	}
	pcm, meta, ok := qqWAVVoicePayload(data)
	if !ok {
		return "", fmt.Errorf("qqbot: voice payload must be Tencent SILK or PCM WAV, got %s", filepath.Ext(fileName))
	}
	silkData, err := encodeQQWAVVoicePayload(pcm, meta)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(silkData), nil
}

func encodeQQWAVVoicePayload(pcm []byte, meta qqVoiceMetadata) ([]byte, error) {
	if meta.bitsPerSample != 16 {
		return nil, fmt.Errorf("qqbot: unsupported WAV bit depth %d for SILK voice", meta.bitsPerSample)
	}
	mono := pcm
	if meta.channels == 2 {
		mono = qqDownmixStereoS16ToMono(pcm)
	} else if meta.channels != 1 {
		return nil, fmt.Errorf("qqbot: unsupported WAV channel count %d for SILK voice", meta.channels)
	}
	if meta.sampleRate != qqVoiceSampleRate {
		mono = qqResampleS16Mono(mono, meta.sampleRate, qqVoiceSampleRate)
	}
	mono, err := qqPadPCMForSilk(mono, qqVoiceSampleRate)
	if err != nil {
		return nil, err
	}
	out, err := silk.EncodePcmBuffToSilk(mono, qqVoiceSampleRate, qqVoiceBitRate, true)
	if err != nil {
		return nil, fmt.Errorf("qqbot: SILK encode failed: %w", err)
	}
	if !isTencentSilk(out) {
		return nil, fmt.Errorf("qqbot: SILK encode produced invalid payload")
	}
	return out, nil
}

func isTencentSilk(data []byte) bool {
	return bytes.HasPrefix(data, []byte("\x02#!SILK_V3")) || bytes.HasPrefix(data, []byte("#!SILK_V3"))
}

func qqWAVVoicePayload(data []byte) ([]byte, qqVoiceMetadata, bool) {
	meta, ok := parseQQWAVVoiceMetadata(data)
	if !ok {
		return nil, qqVoiceMetadata{}, false
	}
	return data[meta.dataStart : meta.dataStart+meta.dataSize], meta, true
}

func parseQQWAVVoiceMetadata(data []byte) (qqVoiceMetadata, bool) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return qqVoiceMetadata{}, false
	}
	var audioFormat, channels, bits uint16
	var sampleRate uint32
	meta := qqVoiceMetadata{}
	for off := 12; off+8 <= len(data); {
		chunkID := string(data[off : off+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		chunkStart := off + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd > len(data) {
			break
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				audioFormat = binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2])
				channels = binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4])
				sampleRate = binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8])
				bits = binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16])
			}
		case "data":
			meta.dataStart = chunkStart
			meta.dataSize = chunkSize
		}
		if sampleRate > 0 && channels > 0 && bits > 0 && meta.dataSize > 0 {
			break
		}
		off = chunkEnd
		if off%2 == 1 {
			off++
		}
	}
	if audioFormat != 1 || sampleRate == 0 || channels == 0 || bits == 0 || meta.dataSize == 0 || meta.dataStart == 0 {
		return qqVoiceMetadata{}, false
	}
	meta.sampleRate = int(sampleRate)
	meta.channels = int(channels)
	meta.bitsPerSample = int(bits)
	return meta, true
}

func qqPadPCMForSilk(data []byte, sampleRate int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("qqbot: empty WAV voice payload")
	}
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("qqbot: malformed 16-bit PCM payload size %d", len(data))
	}
	frameBytes := sampleRate / 1000 * 40
	if frameBytes <= 0 {
		return nil, fmt.Errorf("qqbot: invalid SILK sample rate %d", sampleRate)
	}
	rem := len(data) % frameBytes
	if rem == 0 {
		return data, nil
	}
	out := make([]byte, len(data)+frameBytes-rem)
	copy(out, data)
	return out, nil
}

func qqDownmixStereoS16ToMono(data []byte) []byte {
	frames := len(data) / 4
	out := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		l := int(int16(binary.LittleEndian.Uint16(data[i*4:])))
		r := int(int16(binary.LittleEndian.Uint16(data[i*4+2:])))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16((l+r)/2)))
	}
	return out
}

func qqResampleS16Mono(data []byte, fromRate, toRate int) []byte {
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate || len(data) < 2 {
		return data
	}
	inSamples := len(data) / 2
	outSamples := int((int64(inSamples)*int64(toRate) + int64(fromRate) - 1) / int64(fromRate))
	if outSamples <= 0 {
		return nil
	}
	out := make([]byte, outSamples*2)
	for i := 0; i < outSamples; i++ {
		posNum := int64(i) * int64(fromRate)
		idx := int(posNum / int64(toRate))
		if idx >= inSamples-1 {
			copy(out[i*2:], data[(inSamples-1)*2:inSamples*2])
			continue
		}
		frac := float64(posNum%int64(toRate)) / float64(toRate)
		a := float64(int16(binary.LittleEndian.Uint16(data[idx*2:])))
		b := float64(int16(binary.LittleEndian.Uint16(data[(idx+1)*2:])))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(a+(b-a)*frac)))
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
